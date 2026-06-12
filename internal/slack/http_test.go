package slack

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/slack-go/slack/slackevents"
)

func TestEventsHTTPHandlerURLVerification(t *testing.T) {
	const secret = "signing-secret"

	handler := NewHandler(
		nil,
		nil,
		"C12345",
		map[string]bool{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	body := `{"type":"url_verification","token":"token","challenge":"challenge-token"}`
	req := signedSlackRequest(t, secret, body)
	rr := httptest.NewRecorder()

	handler.EventsHTTPHandler(secret).ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status=%d, want %d", rr.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"challenge":"challenge-token"}` {
		t.Fatalf("got body=%q, want challenge response", got)
	}
}

func TestEventsHTTPHandlerHealthEndpoint(t *testing.T) {
	handler := NewHandler(
		nil,
		nil,
		"C12345",
		map[string]bool{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rr := httptest.NewRecorder()

	handler.EventsHTTPHandler("signing-secret").ServeHTTP(rr, req)

	if rr.Code != http.StatusOK {
		t.Fatalf("got status=%d, want %d", rr.Code, http.StatusOK)
	}
	if got := strings.TrimSpace(rr.Body.String()); got != `{"status":"ok"}` {
		t.Fatalf("got body=%q, want health response", got)
	}
}

func TestEventsHTTPHandlerRejectsInvalidSignature(t *testing.T) {
	handler := NewHandler(
		nil,
		nil,
		"C12345",
		map[string]bool{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(`{}`))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()

	handler.EventsHTTPHandler("signing-secret").ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("got status=%d, want %d", rr.Code, http.StatusUnauthorized)
	}
}

func TestEventsHTTPHandlerEventCallbackReturnsBeforeProcessingCompletes(t *testing.T) {
	const secret = "signing-secret"

	handler := NewHandler(
		nil,
		nil,
		"C12345",
		map[string]bool{},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	started := make(chan struct{})
	release := make(chan struct{})
	handler.eventsAPIProcessor = func(_ slackevents.EventsAPIEvent) {
		close(started)
		<-release
	}

	body := `{"type":"event_callback","token":"token","team_id":"T123","api_app_id":"A123","event":{"type":"app_home_opened","user":"U123","channel":"D123","tab":"home","event_ts":"1716540000.000100"},"event_id":"Ev123","event_time":1716540000}`
	req := signedSlackRequest(t, secret, body)
	rr := httptest.NewRecorder()

	finished := make(chan struct{})
	go func() {
		handler.EventsHTTPHandler(secret).ServeHTTP(rr, req)
		close(finished)
	}()

	select {
	case <-finished:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("HTTP handler did not return before background event processing completed")
	}

	if rr.Code != http.StatusOK {
		t.Fatalf("got status=%d, want %d", rr.Code, http.StatusOK)
	}

	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("background event processing did not start")
	}

	close(release)
}

func signedSlackRequest(t *testing.T, secret, body string) *http.Request {
	t.Helper()

	req := httptest.NewRequest(http.MethodPost, "/slack/events", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")

	req.Header.Set("X-Slack-Request-Timestamp", strconv.FormatInt(time.Now().UTC().Unix(), 10))

	base := "v0:" + req.Header.Get("X-Slack-Request-Timestamp") + ":" + body
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(base))
	req.Header.Set("X-Slack-Signature", "v0="+hex.EncodeToString(mac.Sum(nil)))

	return req
}
