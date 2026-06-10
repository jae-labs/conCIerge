package slack

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/slack-go/slack"
	"github.com/slack-go/slack/slackevents"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/baggage"
	"go.opentelemetry.io/otel/trace"
)

// ReadHeaderTimeout caps HTTP header read in the events server.
const ReadHeaderTimeout = 5 * time.Second

// EventsHTTPHandler returns the mux that serves /slack/events and /healthz.
// The mux pattern strings include method matchers; the wrapping otelhttp
// handler creates root spans for each inbound request.
func (h *Handler) EventsHTTPHandler(signingSecret string) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", handleHealth)
	mux.HandleFunc("POST /slack/events", func(w http.ResponseWriter, r *http.Request) {
		h.handleEventsHTTPRequest(w, r, signingSecret)
	})
	return otelhttp.NewHandler(mux, "slack.events",
		otelhttp.WithMessageEvents(otelhttp.ReadEvents, otelhttp.WriteEvents),
	)
}

func handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *Handler) handleEventsHTTPRequest(w http.ResponseWriter, r *http.Request, signingSecret string) {
	body, err := verifySlackRequest(r, signingSecret)
	if err != nil {
		h.logger.WarnContext(r.Context(), "invalid slack request signature", "error", err)
		http.Error(w, "invalid request signature", http.StatusUnauthorized)
		return
	}

	if payload := r.PostFormValue("payload"); payload != "" {
		h.serveInteractive(w, r)
		return
	}
	h.serveEvent(w, r, body)
}

func (h *Handler) serveInteractive(w http.ResponseWriter, r *http.Request) {
	callback, err := slack.InteractionCallbackParse(r)
	if err != nil {
		h.logger.WarnContext(r.Context(), "failed to parse interactive payload", "error", err)
		http.Error(w, "invalid interactive payload", http.StatusBadRequest)
		return
	}
	h.handleInteractiveCallback(r.Context(), callback, newHTTPResponder(w))
}

func (h *Handler) serveEvent(w http.ResponseWriter, r *http.Request, body []byte) {
	eventsAPI, err := slackevents.ParseEvent(json.RawMessage(body), slackevents.OptionNoVerifyToken())
	if err != nil {
		h.logger.WarnContext(r.Context(), "failed to parse events payload", "error", err)
		http.Error(w, "invalid events payload", http.StatusBadRequest)
		return
	}

	if eventsAPI.Type == slackevents.URLVerification {
		verification, ok := eventsAPI.Data.(*slackevents.EventsAPIURLVerificationEvent)
		if !ok {
			h.logger.WarnContext(r.Context(), "unexpected url verification payload type")
			http.Error(w, "invalid url verification payload", http.StatusBadRequest)
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"challenge": verification.Challenge})
		return
	}

	w.WriteHeader(http.StatusOK)
	// Detach from the HTTP request lifecycle to keep the worker alive past
	// Slack's 3s ack budget, while propagating the trace/baggage context.
	ctx := trace.ContextWithSpanContext(context.Background(), trace.SpanContextFromContext(r.Context()))
	ctx = baggage.ContextWithBaggage(ctx, baggage.FromContext(r.Context()))
	go h.dispatchEventsAPIEvent(ctx, eventsAPI)
}

func verifySlackRequest(r *http.Request, signingSecret string) ([]byte, error) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, err
	}
	verifier, err := slack.NewSecretsVerifier(r.Header, signingSecret)
	if err != nil {
		return nil, err
	}
	if _, err := verifier.Write(body); err != nil {
		return nil, err
	}
	if err := verifier.Ensure(); err != nil {
		return nil, err
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	return body, nil
}

type httpResponder struct {
	w         http.ResponseWriter
	responded bool
}

func newHTTPResponder(w http.ResponseWriter) *httpResponder { return &httpResponder{w: w} }

func (r *httpResponder) Ack(payload ...any) error {
	if r.responded {
		return nil
	}
	r.responded = true

	switch len(payload) {
	case 0:
		r.w.WriteHeader(http.StatusOK)
		return nil
	case 1:
		writeJSON(r.w, http.StatusOK, payload[0])
		return nil
	default:
		return errors.New("http responder accepts at most one payload")
	}
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
