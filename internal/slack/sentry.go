package slack

import (
	"context"

	"github.com/getsentry/sentry-go"
	"github.com/jae-labs/conCIerge/internal/conversation"
	"go.opentelemetry.io/otel/trace"
)

func captureWorkflowError(ctx context.Context, state *conversation.State, step string, err error) {
	if err == nil {
		return
	}

	hub := sentry.CurrentHub()
	if hub.Client() == nil {
		return
	}

	hub.WithScope(func(scope *sentry.Scope) {
		scope.SetTag("component", "slack")
		scope.SetTag("workflow.step", step)

		spanContext := trace.SpanContextFromContext(ctx)
		if spanContext.IsValid() {
			scope.SetTag("trace_id", spanContext.TraceID().String())
			scope.SetTag("span_id", spanContext.SpanID().String())
		}

		if state != nil {
			scope.SetTag("category", state.Category)
			scope.SetTag("resource_type", state.ResourceType)
			scope.SetTag("action_type", state.ActionType)
			scope.SetContext("concierge", sentry.Context{
				"channel_id": state.ChannelID,
				"thread_ts":  state.ThreadTS,
			})
		}

		hub.CaptureException(err)
	})
}
