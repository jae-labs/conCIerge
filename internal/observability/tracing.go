package observability

import (
	"context"

	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func buildTracerProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdktrace.TracerProvider, error) {
	options := []sdktrace.TracerProviderOption{
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	}

	if cfg.TracesEndpoint != "" {
		var (
			exporter sdktrace.SpanExporter
			err      error
		)
		switch cfg.TracesProtocol {
		case "http/protobuf", "http/json", "http":
			if endpointHasScheme(cfg.TracesEndpoint) {
				exporter, err = otlptracehttp.New(ctx,
					otlptracehttp.WithEndpointURL(cfg.TracesEndpoint),
				)
			} else {
				exporter, err = otlptracehttp.New(ctx,
					otlptracehttp.WithEndpoint(cfg.TracesEndpoint),
					otlptracehttp.WithInsecure(),
				)
			}
		default:
			if endpointHasScheme(cfg.TracesEndpoint) {
				exporter, err = otlptracegrpc.New(ctx,
					otlptracegrpc.WithEndpointURL(cfg.TracesEndpoint),
				)
			} else {
				exporter, err = otlptracegrpc.New(ctx,
					otlptracegrpc.WithEndpoint(cfg.TracesEndpoint),
					otlptracegrpc.WithInsecure(),
				)
			}
		}
		if err != nil {
			return nil, err
		}
		options = append(options, sdktrace.WithBatcher(exporter))
	}

	return sdktrace.NewTracerProvider(options...), nil
}
