package slack

import (
	"context"
	"log/slog"

	"github.com/jae-labs/concierge/internal/conversation"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

func captureWorkflowError(ctx context.Context, state *conversation.State, step string, err error) {
	if err == nil {
		return
	}

	span := trace.SpanFromContext(ctx)
	if span.SpanContext().IsValid() {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
	}

	attrs := []any{
		slog.String("component", "slack"),
		slog.String("workflow.step", step),
		slog.Any("error", err),
	}

	if state != nil {
		attrs = append(attrs,
			slog.String("category", state.Category),
			slog.String("resource_type", state.ResourceType),
			slog.String("action_type", state.ActionType),
			slog.String("channel_id", state.ChannelID),
			slog.String("thread_ts", state.ThreadTS),
		)
	}

	slog.ErrorContext(ctx, "workflow step failed", attrs...)
}
