package observability

import (
	"context"
	"io"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns a structured slog logger.  JSON output is used when the
// environment name indicates a non-local deployment (anything other than
// "development", "dev", "local", or "test") or when LOG_FORMAT=json is set.
// LOG_LEVEL=debug enables debug output regardless of environment.
func NewLogger(env, serviceName, serviceVersion string) *slog.Logger {
	return newLogger(os.Stderr, env, serviceName, serviceVersion)
}

func newLogger(w io.Writer, env, serviceName, serviceVersion string) *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	commonAttrs := []slog.Attr{
		slog.String("service.name", serviceName),
		slog.String("service.version", serviceVersion),
		slog.String("deployment.environment", env),
	}

	var baseHandler slog.Handler
	if useJSONLogging(env) {
		baseHandler = slog.NewJSONHandler(w, opts)
	} else {
		baseHandler = slog.NewTextHandler(w, opts)
	}
	baseHandler = baseHandler.WithAttrs(commonAttrs)

	return slog.New(&traceHandler{next: baseHandler})
}

func useJSONLogging(env string) bool {
	if os.Getenv("LOG_FORMAT") == "json" {
		return true
	}
	switch env {
	case "", "development", "dev", "local", "test":
		return false
	}
	return true
}

// WithTrace returns a child logger with trace_id and span_id fields added when
// the context carries an active, valid OTel span.  Returns the original logger
// unchanged when no valid span is present.
func WithTrace(ctx context.Context, logger *slog.Logger) *slog.Logger {
	span := trace.SpanFromContext(ctx)
	if !span.SpanContext().IsValid() {
		return logger
	}
	sc := span.SpanContext()
	return logger.With(
		slog.String("trace_id", sc.TraceID().String()),
		slog.String("span_id", sc.SpanID().String()),
	)
}

type traceHandler struct {
	next slog.Handler
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, record slog.Record) error {
	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		sc := span.SpanContext()
		record.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, record)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{next: h.next.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{next: h.next.WithGroup(name)}
}
