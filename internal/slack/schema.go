package slack

import (
	"context"
	"fmt"

	"github.com/jae-labs/concierge/internal/schema"
)

// fetchRuntimeSchemaFromReader fetches and parses the concierge schema file.
// Exposed at package scope so HTTP/socket-mode tests can reuse it directly.
func fetchRuntimeSchemaFromReader(ctx context.Context, reader schemaFileReader) (*schema.Schema, error) {
	src, _, err := reader.GetFileContent(ctx, pathConciergeSchema)
	if err != nil {
		return nil, fmt.Errorf("fetch schema file: %w", err)
	}
	parsed, err := schema.Parse(src)
	if err != nil {
		return nil, fmt.Errorf("parse yaml schema: %w", err)
	}
	return parsed, nil
}

// refreshRuntimeSchema always re-fetches the schema and overwrites the cache.
func (h *Handler) refreshRuntimeSchema(ctx context.Context) (*schema.Schema, error) {
	if h.schemaReader == nil {
		return nil, fmt.Errorf("schema reader is not configured")
	}
	runtimeSchema, err := fetchRuntimeSchemaFromReader(ctx, h.schemaReader)
	if err != nil {
		return nil, err
	}
	h.schemaMu.Lock()
	h.runtimeSchema = runtimeSchema
	h.schemaMu.Unlock()
	return runtimeSchema, nil
}

// fetchRuntimeSchema returns the cached schema or refreshes it on miss.
func (h *Handler) fetchRuntimeSchema(ctx context.Context) (*schema.Schema, error) {
	h.schemaMu.RLock()
	cached := h.runtimeSchema
	h.schemaMu.RUnlock()
	if cached != nil {
		return cached, nil
	}
	return h.refreshRuntimeSchema(ctx)
}

// ValidateRuntimeSchema is called at boot to fail fast on schema problems.
func (h *Handler) ValidateRuntimeSchema(ctx context.Context) error {
	_, err := h.fetchRuntimeSchema(ctx)
	return err
}

func (h *Handler) schemaResourcesByCategory(ctx context.Context, category string) []schema.Resource {
	runtimeSchema, err := h.fetchRuntimeSchema(ctx)
	if err != nil {
		return nil
	}
	return runtimeSchema.ResourcesByCategory(category)
}

func (h *Handler) fetchSchema(ctx context.Context, resourceID string) (*schema.Resource, error) {
	runtimeSchema, err := h.fetchRuntimeSchema(ctx)
	if err != nil {
		return nil, err
	}
	resource, ok := runtimeSchema.ResourceByID(resourceID)
	if !ok {
		return nil, fmt.Errorf("resource %q not found in schema", resourceID)
	}
	return resource, nil
}
