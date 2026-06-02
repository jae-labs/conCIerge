package slack

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/jae-labs/conCIerge/internal/conversation"
	"go.opentelemetry.io/otel/trace"
)

type sentryTestTransport struct {
	events []*sentry.Event
}

func (t *sentryTestTransport) Configure(sentry.ClientOptions) {}
func (t *sentryTestTransport) Flush(time.Duration) bool       { return true }
func (t *sentryTestTransport) FlushWithContext(context.Context) bool {
	return true
}
func (t *sentryTestTransport) Close() {}
func (t *sentryTestTransport) SendEvent(event *sentry.Event) {
	t.events = append(t.events, event)
}

func TestCaptureWorkflowErrorSendsSentryEventWithWorkflowContext(t *testing.T) {
	transport := &sentryTestTransport{}
	client, err := sentry.NewClient(sentry.ClientOptions{
		Dsn:       "https://public@example.invalid/1",
		Transport: transport,
	})
	if err != nil {
		t.Fatalf("create sentry client: %v", err)
	}
	hub := sentry.CurrentHub()
	previousClient := hub.Client()
	hub.BindClient(client)
	defer hub.BindClient(previousClient)

	traceID, err := trace.TraceIDFromHex("11111111111111111111111111111111")
	if err != nil {
		t.Fatalf("trace id: %v", err)
	}
	spanID, err := trace.SpanIDFromHex("2222222222222222")
	if err != nil {
		t.Fatalf("span id: %v", err)
	}
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))
	state := &conversation.State{
		ChannelID:    "C123",
		ThreadTS:     "1710000000.000100",
		UserID:       "U123",
		Category:     "github",
		ResourceType: "user_management",
		ActionType:   "change_role",
	}

	captureWorkflowError(ctx, state, "fetch members HCL", errors.New("get contents: context canceled"))

	if len(transport.events) != 1 {
		t.Fatalf("captured events=%d, want 1", len(transport.events))
	}
	event := transport.events[0]
	if got := event.Tags["workflow.step"]; got != "fetch members HCL" {
		t.Fatalf("workflow.step=%q", got)
	}
	if got := event.Tags["trace_id"]; got != traceID.String() {
		t.Fatalf("trace_id=%q", got)
	}
	if got := event.Tags["span_id"]; got != spanID.String() {
		t.Fatalf("span_id=%q", got)
	}
	if got := event.Tags["resource_type"]; got != "user_management" {
		t.Fatalf("resource_type=%q", got)
	}
	if got := event.Contexts["concierge"]["thread_ts"]; got != "1710000000.000100" {
		t.Fatalf("thread_ts=%v", got)
	}
}
