package slack

import (
	"strings"
	"testing"

	"github.com/jae-labs/concierge/internal/conversation"
	"github.com/jae-labs/concierge/internal/schema"
)

func TestBuildPRArtifactsSingleton(t *testing.T) {
	resource := &schema.Resource{
		ID:       "org_settings",
		Kind:     schema.KindSingleton,
		RootPath: "org_settings",
		Steps: []schema.Step{{Fields: []schema.Field{
			{Path: "name", Type: schema.TypeString, Label: "Name"},
		}}},
	}
	state := &conversation.State{
		ResourceType:  "org_settings",
		ActionType:    schema.ActionSettings,
		TargetRepo:    "org_settings",
		DynamicConfig: map[string]any{"name": "JAE Labs Updated"},
	}
	src := []byte("locals {\n  org_settings = {\n    name = \"JAE Labs\"\n  }\n}\n")

	artifacts, err := buildPRArtifacts(resource, state, src)
	if err != nil {
		t.Fatalf("buildPRArtifacts: %v", err)
	}
	if !strings.HasPrefix(artifacts.Branch, "concierge/update-org_settings-org_settings-") {
		t.Fatalf("branch=%q", artifacts.Branch)
	}
	if artifacts.Title != "concierge: Update org_settings" {
		t.Fatalf("title=%q", artifacts.Title)
	}
	if !strings.Contains(string(artifacts.Modified), "JAE Labs Updated") {
		t.Fatalf("missing updated value in modified output: %s", string(artifacts.Modified))
	}
}

func TestBuildPRArtifactsMapEntryActions(t *testing.T) {
	resource := &schema.Resource{
		ID:       "repo",
		Kind:     schema.KindMapEntry,
		RootPath: "repos",
		Steps: []schema.Step{{Fields: []schema.Field{
			{Path: "description", Type: schema.TypeString, Label: "Description"},
		}}},
	}
	src := []byte("locals {\n  repos = {\n    \"alpha\" = {\n      description = \"first\"\n    }\n  }\n}\n")

	tests := []struct {
		name       string
		action     string
		target     string
		config     map[string]any
		wantPrefix string
		wantTitle  string
	}{
		{
			name:       "add",
			action:     schema.ActionAdd,
			target:     "beta",
			config:     map[string]any{"description": "new repo"},
			wantPrefix: "concierge/add-repo-beta-",
			wantTitle:  "concierge: Add repo beta",
		},
		{
			name:       "delete",
			action:     schema.ActionDelete,
			target:     "alpha",
			wantPrefix: "concierge/delete-repo-alpha-",
			wantTitle:  "concierge: Remove repo alpha",
		},
		{
			name:       "settings",
			action:     schema.ActionSettings,
			target:     "alpha",
			config:     map[string]any{"description": "updated"},
			wantPrefix: "concierge/update-repo-alpha-",
			wantTitle:  "concierge: Update repo alpha",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			state := &conversation.State{
				ResourceType:  "repo",
				ActionType:    tc.action,
				TargetRepo:    tc.target,
				DynamicConfig: tc.config,
			}
			artifacts, err := buildPRArtifacts(resource, state, src)
			if err != nil {
				t.Fatalf("buildPRArtifacts: %v", err)
			}
			if !strings.HasPrefix(artifacts.Branch, tc.wantPrefix) {
				t.Fatalf("branch=%q, want prefix %q", artifacts.Branch, tc.wantPrefix)
			}
			if artifacts.Title != tc.wantTitle {
				t.Fatalf("title=%q, want %q", artifacts.Title, tc.wantTitle)
			}
		})
	}
}

func TestBuildPRArtifactsUnknownAction(t *testing.T) {
	resource := &schema.Resource{
		ID:       "repo",
		Kind:     schema.KindMapEntry,
		RootPath: "repos",
		Steps:    []schema.Step{{Fields: []schema.Field{{Path: "description", Type: schema.TypeString, Label: "Description"}}}},
	}
	state := &conversation.State{ResourceType: "repo", ActionType: "weird", TargetRepo: "alpha"}
	src := []byte("locals {\n  repos = {\n    \"alpha\" = {}\n  }\n}\n")

	if _, err := buildPRArtifacts(resource, state, src); err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestBuildClosingText(t *testing.T) {
	withLink := buildClosingText("https://slack/permalink", "C123")
	if !strings.Contains(withLink, "<https://slack/permalink|View your request>") {
		t.Fatalf("withLink=%q", withLink)
	}
	withoutLink := buildClosingText("", "C123")
	if !strings.Contains(withoutLink, "<#C123>") {
		t.Fatalf("withoutLink=%q", withoutLink)
	}
}

func TestPRLabel(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"https://github.com/owner/repo/pull/42", "#42"},
		{"https://github.com/owner/repo", "View PR"},
		{"", "View PR"},
	}
	for _, tc := range tests {
		if got := prLabel(tc.in); got != tc.want {
			t.Fatalf("prLabel(%q)=%q want %q", tc.in, got, tc.want)
		}
	}
}
