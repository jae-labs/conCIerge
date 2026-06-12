// Package observability initialises the OpenTelemetry tracer and meter
// providers, wires Sentry, and exposes a Prometheus-compatible /metrics
// endpoint for local Alloy scraping. All telemetry listeners default to
// loopback-only addresses; OTLP export defaults to the local Alloy receiver and
// can still be disabled explicitly by overriding endpoints at config load time.
package observability

import (
	"context"
	"fmt"
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds observability settings derived from the application Config.
type Config struct {
	ServiceName     string
	Environment     string
	ServiceVersion  string
	TracesEndpoint  string // defaults to the local Alloy OTLP receiver
	TracesProtocol  string // "grpc" (default), "http/protobuf", "http"
	MetricsEnabled  bool
	MetricsEndpoint string // defaults to TracesEndpoint
	MetricsProtocol string // "grpc" (default), "http/protobuf", "http"
}

func Setup(ctx context.Context, cfg Config) (*Runtime, error) {
	logger := NewLogger(cfg.Environment, cfg.ServiceName, cfg.ServiceVersion)

	res, err := buildResource(ctx, cfg.ServiceName, cfg.ServiceVersion, cfg.Environment)
	if err != nil {
		// Non-fatal: fall back to default resource rather than refusing to start.
		res = resource.Default()
	}

	var shutdowns []func(context.Context) error

	tp, err := buildTracerProvider(ctx, res, cfg)
	if err != nil {
		return nil, fmt.Errorf("create tracer provider: %w", err)
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	shutdowns = append(shutdowns, tp.Shutdown)

	mp, metricsHandler, err := buildMeterProvider(ctx, res, cfg)
	if err != nil {
		return nil, fmt.Errorf("create meter provider: %w", err)
	}
	otel.SetMeterProvider(mp)
	shutdowns = append(shutdowns, mp.Shutdown)

	return &Runtime{
		Logger:         logger,
		MetricsHandler: metricsHandler,
		Shutdown: func(ctx context.Context) error {
			var errs []error
			for _, fn := range shutdowns {
				if err := fn(ctx); err != nil {
					errs = append(errs, err)
				}
			}
			if len(errs) > 0 {
				return fmt.Errorf("otel shutdown errors: %v", errs)
			}
			return nil
		},
	}, nil
}

func buildResource(ctx context.Context, name, version, env string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(name),
			semconv.ServiceVersionKey.String(version),
			semconv.DeploymentEnvironmentKey.String(env),
			attribute.String("service.instance.id", name),
		),
		resource.WithProcessPID(),
		resource.WithHost(),
	)
}

func noContentHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
}
