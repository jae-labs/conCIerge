// Package observability initialises the OpenTelemetry tracer and meter
// providers and exposes a Prometheus-compatible /metrics endpoint for local
// Alloy scraping.  All telemetry listeners default to loopback-only addresses;
// OTLP trace export is optional and is disabled when no endpoint is configured.
package observability

import (
	"context"
	"fmt"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds observability settings derived from the application Config.
type Config struct {
	ServiceName    string
	Environment    string
	TracesEndpoint string // empty → no OTLP export; tracing uses a noop provider
	TracesProtocol string // "grpc" (default), "http/protobuf", "http"
	MetricsEnabled bool
}

// Provider bundles the shutdown hook and the Prometheus metrics HTTP handler
// returned by Setup.
type Provider struct {
	// MetricsHandler serves Prometheus metrics on the configured listen address.
	// When MetricsEnabled is false it returns HTTP 204.
	MetricsHandler http.Handler
	// Shutdown flushes and stops all SDK pipelines.  Call it on process exit.
	Shutdown func(context.Context) error
}

// Setup initialises the global OTel tracer and meter providers and returns a
// Provider. It is safe to call with an empty TracesEndpoint: tracing still uses
// an SDK tracer provider so spans and log correlation fields are available
// locally, but nothing is exported. The caller owns the returned Provider and
// must call Shutdown before exit.
func Setup(ctx context.Context, cfg Config) (*Provider, error) {
	res, err := buildResource(ctx, cfg.ServiceName, cfg.Environment)
	if err != nil {
		// Non-fatal: fall back to default resource rather than refusing to start.
		res = resource.Default()
	}

	var shutdowns []func(context.Context) error

	tp, err := buildTracerProvider(ctx, res, cfg.TracesEndpoint, cfg.TracesProtocol)
	if err != nil {
		return nil, fmt.Errorf("create tracer provider: %w", err)
	}
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	shutdowns = append(shutdowns, tp.Shutdown)

	// Metrics — Prometheus exporter with an isolated registry.
	reg := prometheus.NewRegistry()
	mp, err := buildMeterProvider(res, reg, cfg.MetricsEnabled)
	if err != nil {
		return nil, fmt.Errorf("create meter provider: %w", err)
	}
	otel.SetMeterProvider(mp)
	shutdowns = append(shutdowns, mp.Shutdown)

	var metricsHandler http.Handler
	if cfg.MetricsEnabled {
		metricsHandler = promhttp.HandlerFor(reg, promhttp.HandlerOpts{})
	} else {
		metricsHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNoContent)
		})
	}

	return &Provider{
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

func buildResource(ctx context.Context, name, env string) (*resource.Resource, error) {
	return resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceNameKey.String(name),
			semconv.DeploymentEnvironmentKey.String(env),
		),
		resource.WithProcessPID(),
		resource.WithHost(),
	)
}

func buildTracerProvider(ctx context.Context, res *resource.Resource, endpoint, protocol string) (*sdktrace.TracerProvider, error) {
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
	}
	if endpoint == "" {
		return sdktrace.NewTracerProvider(options...), nil
	}

	var (
		exp sdktrace.SpanExporter
		err error
	)
	switch protocol {
	case "http/protobuf", "http/json", "http":
		exp, err = otlptracehttp.New(ctx,
			otlptracehttp.WithEndpoint(endpoint),
			otlptracehttp.WithInsecure(),
		)
	default: // "grpc"
		exp, err = otlptracegrpc.New(ctx,
			otlptracegrpc.WithEndpoint(endpoint),
			otlptracegrpc.WithInsecure(),
		)
	}
	if err != nil {
		return nil, err
	}
	options = append(options, sdktrace.WithBatcher(exp))
	return sdktrace.NewTracerProvider(options...), nil
}

func buildMeterProvider(res *resource.Resource, reg *prometheus.Registry, enabled bool) (*sdkmetric.MeterProvider, error) {
	if !enabled {
		// Noop-like: SDK provider with no readers — instruments record nothing.
		return sdkmetric.NewMeterProvider(sdkmetric.WithResource(res)), nil
	}
	exp, err := promexporter.New(promexporter.WithRegisterer(reg))
	if err != nil {
		return nil, err
	}
	return sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(exp),
		sdkmetric.WithResource(res),
	), nil
}
