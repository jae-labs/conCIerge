package slack

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/jae-labs/concierge/internal/conversation"
	goslack "github.com/slack-go/slack"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestHandleInteractiveCallbackViewSubmissionUsesParentContext(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	handler := NewHandler(
		nil,
		nil,
		"C12345",
		map[string]bool{"U123": true},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.tracer = tp.Tracer("concierge/slack-test")

	state := handler.store.Create("thread-1", "C123", "U123")
	state.ResourceType = "repo"
	callback := goslack.InteractionCallback{
		Type: goslack.InteractionTypeViewSubmission,
		User: goslack.User{ID: "U123"},
		View: goslack.View{
			CallbackID:      dynamicCallback{Mode: flowCreate, Step: 1}.String(),
			PrivateMetadata: "thread-1:" + state.Nonce,
			State:           &goslack.ViewState{Values: map[string]map[string]goslack.BlockAction{}},
		},
	}

	parentCtx, parentSpan := tp.Tracer("parent").Start(context.Background(), "interactive.request")
	handler.handleInteractiveCallback(parentCtx, callback, interactionResponderFunc(func(payload ...any) error { return nil }))
	parentSpan.End()

	viewSpan := findSpanByName(t, recorder.Ended(), "slack.view_submission")
	if got, want := viewSpan.Parent().SpanID(), parentSpan.SpanContext().SpanID(); got != want {
		t.Fatalf("view submission span parent = %s, want %s", got, want)
	}
	if got, want := viewSpan.SpanContext().TraceID(), parentSpan.SpanContext().TraceID(); got != want {
		t.Fatalf("view submission trace = %s, want %s", got, want)
	}
}

func TestEventsHTTPHandlerInteractiveBlockActionPreservesTraceContext(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
		sdktrace.WithSpanProcessor(recorder),
	)
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	}()

	slackAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/chat.postMessage":
			_, _ = io.WriteString(w, `{"ok":true,"channel":"C123","ts":"1710000000.000200","message":{"text":"ok"}}`)
		default:
			t.Fatalf("unexpected Slack API path %s", r.URL.Path)
		}
	}))
	defer slackAPI.Close()

	handler := NewHandler(
		goslack.New("token", goslack.OptionAPIURL(slackAPI.URL+"/")),
		nil,
		"C12345",
		map[string]bool{"U123": true},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.tracer = tp.Tracer("concierge/slack-test")

	state := handler.store.Create("thread-1", "C123", "U123")
	state.TrackMessage("1710000000.000100", conversation.MsgCategory, "")

	payload, err := json.Marshal(goslack.InteractionCallback{
		Type:      goslack.InteractionTypeBlockActions,
		TriggerID: "trigger-1",
		User:      goslack.User{ID: "U123"},
		Channel: goslack.Channel{
			GroupConversation: goslack.GroupConversation{
				Conversation: goslack.Conversation{ID: "C123"},
			},
		},
		Message: goslack.Message{
			Msg: goslack.Msg{
				Timestamp:       "1710000000.000100",
				ThreadTimestamp: "thread-1",
			},
		},
		ActionCallback: goslack.ActionCallbacks{
			BlockActions: []*goslack.BlockAction{{
				ActionID: ActionCategorySelect,
				SelectedOption: goslack.OptionBlockObject{
					Value: "github",
				},
			}},
		},
	})
	if err != nil {
		t.Fatalf("marshal payload: %v", err)
	}

	req := signedSlackFormRequest(t, "signing-secret", string(payload))
	rr := httptest.NewRecorder()

	handler.EventsHTTPHandler("signing-secret").ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status=%d, want %d", rr.Code, http.StatusOK)
	}

	httpSpan := findSpanByName(t, recorder.Ended(), "POST /slack/events")
	postSpan := findSpanByName(t, recorder.Ended(), "slack.api.post_message")

	for _, span := range []sdktrace.ReadOnlySpan{postSpan} {
		if got, want := span.Parent().SpanID(), httpSpan.SpanContext().SpanID(); got != want {
			t.Fatalf("%s parent = %s, want %s", span.Name(), got, want)
		}
		if got, want := span.SpanContext().TraceID(), httpSpan.SpanContext().TraceID(); got != want {
			t.Fatalf("%s trace = %s, want %s", span.Name(), got, want)
		}
	}
}

func signedSlackFormRequest(t *testing.T, secret, payload string) *http.Request {
	t.Helper()

	form := url.Values{}
	form.Set("payload", payload)
	req := signedSlackRequest(t, secret, form.Encode())
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req
}

func findSpanByName(t *testing.T, spans []sdktrace.ReadOnlySpan, name string) sdktrace.ReadOnlySpan {
	t.Helper()

	for _, span := range spans {
		if span.Name() == name {
			return span
		}
	}

	var names []string
	for _, span := range spans {
		names = append(names, span.Name())
	}
	t.Fatalf("span %q not found in %s", name, strings.Join(names, ", "))
	return nil
}
