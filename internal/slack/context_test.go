package slack

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestWorkflowContextDetachesCancellationAndPreservesTrace(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("11111111111111111111111111111111")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("2222222222222222")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}
	parent, cancel := context.WithCancel(trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})))

	got := workflowContext(parent)
	cancel()

	select {
	case <-got.Done():
		t.Fatal("workflow context was canceled with request context")
	default:
	}

	if gotTraceID := trace.SpanContextFromContext(got).TraceID(); gotTraceID != traceID {
		t.Fatalf("trace id=%s, want %s", gotTraceID, traceID)
	}
}
