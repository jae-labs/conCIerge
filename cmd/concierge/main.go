package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/pprof"
	"os"
	"os/signal"
	"runtime/debug"
	"syscall"
	"time"

	"github.com/jae-labs/concierge/internal/config"
	ghclient "github.com/jae-labs/concierge/internal/github"
	"github.com/jae-labs/concierge/internal/observability"
	slackhandler "github.com/jae-labs/concierge/internal/slack"
	slacklib "github.com/slack-go/slack"
	"github.com/slack-go/slack/socketmode"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		// Pre-observability: use plain text logger for startup errors.
		slog.New(slog.NewTextHandler(os.Stderr, nil)).Error("failed to load config", "error", err)
		os.Exit(1)
	}

	obsCfg := observability.Config{
		ServiceName:     cfg.OtelServiceName,
		Environment:     cfg.OtelEnvironment,
		ServiceVersion:  cfg.ServiceVersion,
		TracesEndpoint:  cfg.OtelTracesEndpoint,
		TracesProtocol:  cfg.OtelTracesProtocol,
		MetricsEnabled:  cfg.MetricsEnabled,
		MetricsEndpoint: cfg.OtelMetricsEndpoint,
		MetricsProtocol: cfg.OtelMetricsProtocol,
	}
	obs, err := observability.Setup(context.Background(), obsCfg)
	if err != nil {
		slog.Error("failed to initialise observability", "error", err)
		os.Exit(1)
	}
	logger := obs.Logger
	slog.SetDefault(logger)

	var metricsServer *http.Server
	defer func() {
		if metricsServer != nil {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			if err := metricsServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics server shutdown error", "error", err)
			}
			cancel()
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if err := obs.Shutdown(ctx); err != nil {
			slog.Error("observability shutdown error", "error", err)
		}
		cancel()
	}()

	// Start the Prometheus metrics server on the loopback metrics address.
	if cfg.MetricsEnabled {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", obs.MetricsHandler)

		// Register pprof handlers for continuous profiling
		metricsMux.HandleFunc("/debug/pprof/", pprof.Index)
		metricsMux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
		metricsMux.HandleFunc("/debug/pprof/profile", pprof.Profile)
		metricsMux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
		metricsMux.HandleFunc("/debug/pprof/trace", pprof.Trace)

		metricsServer = &http.Server{
			Addr:              cfg.MetricsListenAddr,
			Handler:           metricsMux,
			ReadHeaderTimeout: 5 * time.Second,
		}
		go func() {
			slog.Info("metrics endpoint listening", "addr", cfg.MetricsListenAddr)
			if err := metricsServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
				slog.Error("metrics server error", "error", err)
			}
		}()
	}

	slackOptions := []slacklib.Option{}
	if cfg.SlackAppToken != "" {
		slackOptions = append(slackOptions, slacklib.OptionAppLevelToken(cfg.SlackAppToken))
	}
	slackOptions = append(slackOptions, slacklib.OptionHTTPClient(&http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}))
	api := slacklib.New(cfg.SlackBotToken, slackOptions...)

	gh, err := ghclient.NewClient(
		cfg.GitHubAppID, cfg.GitHubAppInstallationID, cfg.GitHubAppPrivateKey,
		cfg.GitHubOwner, cfg.GitHubRepo,
		ghclient.Author{Name: cfg.GitHubCommitAuthorName, Email: cfg.GitHubCommitAuthorEmail},
	)
	if err != nil {
		slog.Error("failed to create github client", "error", err)
		os.Exit(1)
	}

	handler := slackhandler.NewHandler(
		api,
		gh,
		cfg.SlackRequestsChannelID,
		cfg.SlackUserIDs,
		logger,
	)
	if err := handler.ValidateRuntimeSchema(context.Background()); err != nil {
		slog.Error("failed to load runtime schema",
			"error", err,
			"github_owner", cfg.GitHubOwner,
			"github_repo", cfg.GitHubRepo,
			"schema_path", "concierge-schema.yaml",
		)
		os.Exit(1)
	}

	runCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()

	switch cfg.SlackMode {
	case config.SlackModeHTTP:
		slog.Info("concierge starting in http mode", "listen_addr", cfg.SlackHTTPListenAddr)
		httpHandler := recoverMiddleware(handler.EventsHTTPHandler(cfg.SlackSigningSecret))
		server := &http.Server{
			Addr:              cfg.SlackHTTPListenAddr,
			Handler:           httpHandler,
			ReadHeaderTimeout: slackhandler.ReadHeaderTimeout,
		}
		go func() {
			<-runCtx.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			_ = server.Shutdown(ctx)
			if metricsServer != nil {
				_ = metricsServer.Shutdown(ctx)
			}
		}()
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("http server stopped", "error", err)
			os.Exit(1)
		}
	default:
		slog.Info("concierge starting in socket mode")
		sm := socketmode.New(api)
		//nolint:staticcheck // SA4023 comparison is necessary to check if the error is due to cancellation
		if err := handler.RunSocketMode(runCtx, sm); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("socket mode client stopped", "error", err)
			os.Exit(1)
		}
	}
}

func recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				err, ok := rec.(error)
				if !ok {
					err = fmt.Errorf("%v", rec)
				}
				slog.ErrorContext(r.Context(), "panic recovered in HTTP handler",
					slog.Any("error", err),
					slog.String("stack", string(debug.Stack())),
				)
				http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
			}
		}()
		next.ServeHTTP(w, r)
	})
}
