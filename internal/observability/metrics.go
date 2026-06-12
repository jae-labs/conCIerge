package observability

import (
	"context"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	promexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
)

func buildMeterProvider(ctx context.Context, res *resource.Resource, cfg Config) (*sdkmetric.MeterProvider, http.Handler, error) {
	readers := make([]sdkmetric.Reader, 0, 2)
	handler := noContentHandler()

	if cfg.MetricsEnabled {
		registry := prometheus.NewRegistry()
		exporter, err := promexporter.New(promexporter.WithRegisterer(registry))
		if err != nil {
			return nil, nil, err
		}
		readers = append(readers, exporter)
		handler = promhttp.HandlerFor(registry, promhttp.HandlerOpts{})

		if cfg.MetricsEndpoint != "" {
			var (
				metricExporter sdkmetric.Exporter
				err            error
			)
			switch cfg.MetricsProtocol {
			case "http/protobuf", "http/json", "http":
				if endpointHasScheme(cfg.MetricsEndpoint) {
					metricExporter, err = otlpmetrichttp.New(ctx,
						otlpmetrichttp.WithEndpointURL(cfg.MetricsEndpoint),
					)
				} else {
					metricExporter, err = otlpmetrichttp.New(ctx,
						otlpmetrichttp.WithEndpoint(cfg.MetricsEndpoint),
						otlpmetrichttp.WithInsecure(),
					)
				}
			default:
				if endpointHasScheme(cfg.MetricsEndpoint) {
					metricExporter, err = otlpmetricgrpc.New(ctx,
						otlpmetricgrpc.WithEndpointURL(cfg.MetricsEndpoint),
					)
				} else {
					metricExporter, err = otlpmetricgrpc.New(ctx,
						otlpmetricgrpc.WithEndpoint(cfg.MetricsEndpoint),
						otlpmetricgrpc.WithInsecure(),
					)
				}
			}
			if err != nil {
				return nil, nil, err
			}
			readers = append(readers, sdkmetric.NewPeriodicReader(metricExporter))
		}
	}

	options := []sdkmetric.Option{
		sdkmetric.WithResource(res),
	}
	for _, reader := range readers {
		options = append(options, sdkmetric.WithReader(reader))
	}

	return sdkmetric.NewMeterProvider(options...), handler, nil
}
