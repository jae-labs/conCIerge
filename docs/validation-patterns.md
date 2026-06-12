# Validation Patterns

Validation is generic and schema-driven. There are no resource-specific validation files.

## Layers

1. schema parse validation
2. modal submission validation
3. HCL parse before mutation
4. HCL parse after mutation

## Modal validation

`internal/slack/dynamic_validation.go` validates by schema field metadata:

- required fields
- integer / number parsing
- select presence
- `map_string` option validity
- dynamic key-source selections

Returns `map[string]string` keyed by block ID for Slack inline errors.

## Cross-record validation

Current generic checks (in `internal/slack/submission.go`):

- duplicate key rejection for `map_entry` create (uses `state.DynamicResourceKeys`)
- target existence checks for update/delete
- key-pattern regex match (when `key_pattern` is set)

## Schema validation

`internal/schema.Parse(...)` validates:

- category IDs and orders
- resource IDs
- valid kind
- valid action names
- step/field presence
- field type compatibility
- `key_source` shape and `map_string`/`select` linkage

## Rule

Do not add per-resource validation files for schema-backed resources. If a new rule is needed, make it expressible in schema or implement it generically.
