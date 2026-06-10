package slack

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	goslack "github.com/slack-go/slack"
)

func TestResolveRequesterNameUsesRealName(t *testing.T) {
	slackAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/users.info":
			_, _ = io.WriteString(w, `{"ok":true,"user":{"id":"U123","real_name":"Luiz F. C. Martins","profile":{"real_name":"Luiz F. C. Martins"}}}`)
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

	if got := handler.resolveRequesterName(context.Background(), "U123"); got != "Luiz F. C. Martins" {
		t.Fatalf("requester=%q", got)
	}
}

func TestResolveRequesterNameFallsBackToUserID(t *testing.T) {
	slackAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":false,"error":"user_not_found"}`)
	}))
	defer slackAPI.Close()

	handler := NewHandler(
		goslack.New("token", goslack.OptionAPIURL(slackAPI.URL+"/")),
		nil,
		"C12345",
		map[string]bool{"U123": true},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)

	if got := handler.resolveRequesterName(context.Background(), "U123"); got != "U123" {
		t.Fatalf("requester=%q", got)
	}
}
