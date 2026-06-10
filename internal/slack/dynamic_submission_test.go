package slack

import (
	"context"
	"io"
	"log/slog"
	"testing"

	"github.com/jae-labs/concierge/internal/schema"
	goslack "github.com/slack-go/slack"
)

func TestHandleViewSubmissionDynamicCreateStep1UsesPreparedState(t *testing.T) {
	handler := NewHandler(
		nil,
		nil,
		"C12345",
		map[string]bool{"U123": true},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.runtimeSchema = &schema.Schema{
		Version: 1,
		Categories: []schema.Category{
			{ID: "github", Label: "GitHub", Order: 10},
		},
		Resources: []schema.Resource{{
			ID:       "repo",
			Category: "github",
			Kind:     schema.KindMapEntry,
			Label:    "GitHub Repositories",
			File:     "github/locals.tf",
			RootPath: "repos",
			KeyLabel: "Repository Name",
			Steps: []schema.Step{
				{
					ID:    "basics",
					Title: "Basic Information",
					Fields: []schema.Field{
						{Path: "description", Type: schema.TypeString, Label: "Description", Required: true},
					},
				},
				{
					ID:    "access",
					Title: "Team Access",
					Fields: []schema.Field{
						{
							Path:  "team_access",
							Type:  schema.TypeMapString,
							Label: "Team Access",
							KeySource: &schema.KeySource{
								File:     "github/locals.tf",
								RootPath: "teams",
							},
							ValueOptions: []string{"admin", "maintain"},
						},
					},
				},
			},
		}},
	}

	state := handler.store.Create("thread-1", "C123", "U123")
	state.ResourceType = "repo"
	state.DynamicKeys = map[string][]string{
		"team_access": {"Maintainers", "Collaborators"},
	}

	callback := goslack.InteractionCallback{
		Type: goslack.InteractionTypeViewSubmission,
		User: goslack.User{ID: "U123"},
		View: goslack.View{
			CallbackID:      dynamicCallback{Mode: flowCreate, Step: 1}.String(),
			PrivateMetadata: "thread-1:" + state.Nonce,
			State: &goslack.ViewState{Values: map[string]map[string]goslack.BlockAction{
				BlockResourceKey: {
					ElemResourceKey: {Value: "test-repo"},
				},
				fieldBlockID("description"): {
					fieldElemID("description"): {Value: "repo description"},
				},
			}},
		},
	}

	var ackPayload []any
	handler.handleViewSubmission(context.Background(), callback, interactionResponderFunc(func(payload ...any) error {
		ackPayload = payload
		return nil
	}))

	if len(ackPayload) != 1 {
		t.Fatalf("ack payload count=%d want 1", len(ackPayload))
	}
	resp, ok := ackPayload[0].(map[string]interface{})
	if !ok {
		t.Fatalf("ack payload type=%T want map", ackPayload[0])
	}
	if got := resp["response_action"]; got != "update" {
		t.Fatalf("response_action=%v want update", got)
	}
	view, ok := resp["view"].(goslack.ModalViewRequest)
	if !ok {
		t.Fatalf("view type=%T want slack.ModalViewRequest", resp["view"])
	}
	if want := (dynamicCallback{Mode: flowCreate, Step: 2}.String()); view.CallbackID != want {
		t.Fatalf("callback_id=%q want %q", view.CallbackID, want)
	}
	if got := state.TargetRepo; got != "test-repo" {
		t.Fatalf("target_repo=%q want test-repo", got)
	}
}
