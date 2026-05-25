package observability

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// NewLogger returns a structured slog logger.  JSON output is used when the
// environment name indicates a non-local deployment (anything other than
// "development", "dev", "local", or "test") or when LOG_FORMAT=json is set.
// LOG_LEVEL=debug enables debug output regardless of environment.
func NewLogger(env string) *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("LOG_LEVEL") == "debug" {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	if useJSONLogging(env) {
		return slog.New(&traceHandler{next: slog.NewJSONHandler(os.Stderr, opts)})
	}
	return slog.New(&traceHandler{next: slog.NewTextHandler(os.Stderr, opts)})
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
