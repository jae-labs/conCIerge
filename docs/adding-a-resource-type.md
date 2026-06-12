# Adding a Resource Type

Schema-backed resources are added in Terraform first, not by adding Go flow code.

## Checklist

1. Add/update resource in `terraform/concierge-schema.yaml`.
2. Point `file:` to an existing locals file (e.g. `github/locals.tf`).
3. Set `kind`:
   - `map_entry`
   - `singleton`
   - `membership`
4. Define actions.
5. Define steps and fields.
6. Define dynamic option sources (`key_source`) where needed.
7. Add/update HCL fixture in `internal/hcl/testdata/`.
8. Add tests for schema parse and generic HCL behavior.
9. Update docs if the contract changed.

## When Go changes are valid

Only change Go when the schema needs a new generic capability, for example:

- new field type
- new resource kind
- new generic validation rule
- new generic HCL mutation primitive

Do not add:

- resource-specific callbacks
- resource-specific modal builders
- resource-specific validation files
- resource-specific PR creation paths

## Files commonly touched

| Area | File |
|---|---|
| schema model | `internal/schema/schema.go` |
| Slack rendering/parsing | `internal/slack/dynamic_blocks.go`, `internal/slack/dynamic_validation.go`, `internal/slack/handler.go` |
| Slack callback IDs / block IDs | `internal/slack/ids.go` |
| HCL engine | `internal/hcl/dynamic_editor.go` |
| fixtures/tests | `internal/hcl/testdata/`, package tests |

## Decision rule

If adding a resource needs bespoke Go branching on resource ID, the design is wrong. Extend the generic model instead.
