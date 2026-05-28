package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/getsentry/sentry-go"
	sentryhttp "github.com/getsentry/sentry-go/http"
	"github.com/jae-labs/conCIerge/internal/config"
	ghclient "github.com/jae-labs/conCIerge/internal/github"
	"github.com/jae-labs/conCIerge/internal/observability"
	slackhandler "github.com/jae-labs/conCIerge/internal/slack"
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

	logger := observability.NewLogger(cfg.OtelEnvironment)
	slog.SetDefault(logger)

	sentryEnabled := cfg.SentryDSN != ""
	if sentryEnabled {
		if err := sentry.Init(sentry.ClientOptions{
			Dsn:         cfg.SentryDSN,
			Environment: cfg.SentryEnvironment,
			Release:     cfg.SentryRelease,
		}); err != nil {
			slog.Error("failed to initialise sentry", "error", err)
			os.Exit(1)
		}
		defer sentry.RecoverWithContext(context.Background())
		defer sentry.Flush(2 * time.Second)
	}

	obsCfg := observability.Config{
		ServiceName:    cfg.OtelServiceName,
		Environment:    cfg.OtelEnvironment,
		TracesEndpoint: cfg.OtelTracesEndpoint,
		TracesProtocol: cfg.OtelTracesProtocol,
		MetricsEnabled: cfg.MetricsEnabled,
	}
	obs, err := observability.Setup(context.Background(), obsCfg)
	if err != nil {
		slog.Error("failed to initialise observability", "error", err)
		os.Exit(1)
	}
	var metricsServer *http.Server
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if metricsServer != nil {
			if err := metricsServer.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
				slog.Error("metrics server shutdown error", "error", err)
			}
		}
		if err := obs.Shutdown(ctx); err != nil {
			slog.Error("observability shutdown error", "error", err)
		}
		if sentryEnabled {
			sentry.Flush(2 * time.Second)
		}
	}()

	// Start the Prometheus metrics server on the loopback metrics address.
	if cfg.MetricsEnabled {
		metricsMux := http.NewServeMux()
		metricsMux.Handle("/metrics", obs.MetricsHandler)
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

	runCtx, stopSignals := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stopSignals()

	switch cfg.SlackMode {
	case config.SlackModeHTTP:
		slog.Info("concierge starting in http mode", "listen_addr", cfg.SlackHTTPListenAddr)
		httpHandler := handler.EventsHTTPHandler(cfg.SlackSigningSecret)
		if sentryEnabled {
			httpHandler = sentryhttp.New(sentryhttp.Options{}).Handle(httpHandler)
		}
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
		if err := handler.RunSocketMode(runCtx, sm); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("socket mode client stopped", "error", err)
			os.Exit(1)
		}
	}
}
