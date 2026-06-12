package slack

import (
	"testing"

	"github.com/jae-labs/concierge/internal/schema"
)

func TestWelcomeBlocks(t *testing.T) {
	blocks := WelcomeBlocks("U123")
	if len(blocks) == 0 {
		t.Fatal("expected non-empty blocks")
	}
}

func TestWelcomeBlocksFromCategories(t *testing.T) {
	blocks := WelcomeBlocksFromCategories("U123", []CategoryOption{{Value: "github", Label: "GitHub"}})
	if len(blocks) == 0 {
		t.Fatal("expected non-empty blocks")
	}
}

func TestResourceBlocksFromOptions(t *testing.T) {
	blocks := ResourceBlocksFromOptions("github", []CategoryOption{{Value: "repo", Label: "Repo"}})
	if len(blocks) == 0 {
		t.Fatal("expected non-empty blocks")
	}
}

func TestComingSoonBlocks(t *testing.T) {
	blocks := ComingSoonBlocks("github")
	if len(blocks) == 0 {
		t.Fatal("expected coming soon blocks")
	}
}

func TestActionOptionsFromSchema(t *testing.T) {
	resource := schema.Resource{Actions: []string{schema.ActionAdd, schema.ActionSettings, schema.ActionDelete, schema.ActionChangeRole}}
	opts := ActionOptionsFromSchema(resource)
	if len(opts) != 4 {
		t.Fatalf("expected 4 options, got %d", len(opts))
	}
}
