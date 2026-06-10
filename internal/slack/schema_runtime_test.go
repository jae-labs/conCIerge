package slack

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
)

type fakeSchemaFileReader struct {
	content []byte
	err     error
	path    string
}

func (f *fakeSchemaFileReader) GetFileContent(_ context.Context, path string) ([]byte, string, error) {
	f.path = path
	if f.err != nil {
		return nil, "", f.err
	}
	return f.content, "sha", nil
}

func TestFetchRuntimeSchemaFromReader(t *testing.T) {
	reader := &fakeSchemaFileReader{content: []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
resources:
  - id: repo
    category: github
    kind: map_entry
    label: Repositories
    file: github/locals.tf
    root_path: repos
    key_label: Repository Name
    actions: [add]
    steps:
      - id: basics
        title: Basics
        fields:
          - path: description
            type: string
            label: Description
            required: true
`)}

	runtimeSchema, err := fetchRuntimeSchemaFromReader(context.Background(), reader)
	if err != nil {
		t.Fatalf("fetchRuntimeSchemaFromReader: %v", err)
	}
	if reader.path != pathConciergeSchema {
		t.Fatalf("path=%q want %q", reader.path, pathConciergeSchema)
	}
	if len(runtimeSchema.Categories) != 1 || len(runtimeSchema.Resources) != 1 {
		t.Fatalf("unexpected schema sizes: %+v", runtimeSchema)
	}
}

func TestFetchRuntimeSchemaFromReaderError(t *testing.T) {
	reader := &fakeSchemaFileReader{err: errors.New("boom")}
	if _, err := fetchRuntimeSchemaFromReader(context.Background(), reader); err == nil {
		t.Fatal("expected error")
	}
}

func TestFetchRuntimeSchemaFromReaderInvalidYAML(t *testing.T) {
	reader := &fakeSchemaFileReader{content: []byte(`not: valid: yaml`)}
	if _, err := fetchRuntimeSchemaFromReader(context.Background(), reader); err == nil {
		t.Fatal("expected parse error")
	}
}

func TestHandlerSchemaReloadAndCaching(t *testing.T) {
	reader := &fakeSchemaFileReader{content: []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
`)}

	handler := NewHandler(
		nil,
		nil,
		"C12345",
		map[string]bool{"U123": true},
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	handler.schemaReader = reader

	// 1. Initial fetch
	s1, err := handler.fetchRuntimeSchema(context.Background())
	if err != nil {
		t.Fatalf("fetchRuntimeSchema error: %v", err)
	}
	if len(s1.Categories) != 1 || s1.Categories[0].ID != "github" {
		t.Fatalf("unexpected initial schema categories: %+v", s1.Categories)
	}

	// 2. Modify schema in reader
	reader.content = []byte(`
version: 1
categories:
  - id: github
    label: GitHub
    order: 10
  - id: cloudflare
    label: Cloudflare
    order: 20
`)

	// 3. fetchRuntimeSchema should still return cached initial schema
	s2, err := handler.fetchRuntimeSchema(context.Background())
	if err != nil {
		t.Fatalf("fetchRuntimeSchema error: %v", err)
	}
	if len(s2.Categories) != 1 || s2.Categories[0].ID != "github" {
		t.Fatalf("expected cached schema, got: %+v", s2.Categories)
	}

	// 4. refreshRuntimeSchema should reload and update the cache
	s3, err := handler.refreshRuntimeSchema(context.Background())
	if err != nil {
		t.Fatalf("refreshRuntimeSchema error: %v", err)
	}
	if len(s3.Categories) != 2 || s3.Categories[1].ID != "cloudflare" {
		t.Fatalf("expected refreshed schema, got: %+v", s3.Categories)
	}

	// 5. fetchRuntimeSchema should now return the updated cached schema
	s4, err := handler.fetchRuntimeSchema(context.Background())
	if err != nil {
		t.Fatalf("fetchRuntimeSchema error: %v", err)
	}
	if len(s4.Categories) != 2 || s4.Categories[1].ID != "cloudflare" {
		t.Fatalf("expected updated cache, got: %+v", s4.Categories)
	}

	// 6. welcomeBlocksCtx with error in reader should fall back to cached schema
	reader.err = errors.New("github API error")
	blocks := handler.welcomeBlocksCtx(context.Background(), "U123")
	// Verify it returned the welcome blocks with the 2 categories (github, cloudflare)
	if len(blocks) != 2 {
		t.Fatalf("expected 2 welcome blocks, got %d", len(blocks))
	}
}
