# AGENTS.md — jae-labs/conCIerge

## Overview

Three-repo platform:

1. `conCIerge` — Go Slack bot that opens Terraform PRs.
2. `ansible` — host config for the OCI instance that runs it.
3. `terraform` — source of truth for infra data and `concierge-schema.yaml`.

Production deployment lives in `deployments/ansible/` and is applied by `release.yml`.

## Cross-system contract

Bot reads everything except the schema filename from `concierge-schema.yaml`. Each schema-backed resource declares its own `file` (e.g. `github/locals.tf`); the only path hardcoded in Go is `concierge-schema.yaml`.

Renaming the schema file requires a matching change in `internal/slack/handler.go` (`pathConciergeSchema`). Renaming any other Terraform file just requires updating the schema; no Go change needed.

## Architecture

Flow is YAML-driven end to end:

1. Load `concierge-schema.yaml` from the Terraform repo.
2. Render categories / resources / actions from schema.
3. Render dynamic Slack modals from schema steps/fields.
4. Read/write Terraform locals via the generic HCL engine.
5. Open a PR with the modified file.

No resource-specific Slack handlers, modal builders, or validation files for schema-backed resources.

## Package map

| Package | Role |
|---|---|
| `internal/config` | env loading and validation |
| `internal/conversation` | thread state, nonce, flow tracking |
| `internal/github` | GitHub file/branch/PR operations + commit author |
| `internal/hcl` | generic HCL read/write engine, membership helpers |
| `internal/schema` | YAML schema parse + validation |
| `internal/slack` | event routing, dynamic modal flow, PR orchestration |
| `internal/observability` | tracing, metrics, logging setup |

## Slack package layout

`internal/slack/` is split by concern; touch the file that matches the change:

| File | Responsibility |
|---|---|
| `handler.go` | `Handler` struct, lifecycle, replies, span helpers |
| `events.go` | inbound Slack events (DMs, App Home, assistant threads) |
| `interactive.go` | block-action handlers, dynamic modal launch |
| `submission.go` | view submission dispatch, ack helper, step validation |
| `pr.go` | Terraform mutation + PR orchestration |
| `summary.go` | reply helpers, request summary, message locking |
| `schema.go` | runtime schema fetch + cache |
| `ids.go` | block/element/callback ID constants and parsing |

## HCL constraints

- Prefer the generic engine in `internal/hcl/dynamic_editor.go`.
- `singleton` and `map_entry` share `applyUpdates` with different base indents (`mapEntryIndent`, `singletonIndent`).
- `membership` uses `internal/hcl/membership_editor.go`.
- Test fixtures in `internal/hcl/testdata/` must mirror the real Terraform layout.

## Slack constraints

- Categories, resources, actions come from YAML.
- Modal fields and steps come from YAML.
- Stale-callback protection uses `threadTS:nonce` in `PrivateMetadata`.
- Authorization is `isAuthorized()` only.
- Block IDs, element IDs, and callback IDs come from `ids.go`; never inline string literals.

## Commands

- build: `go build ./...`
- install hooks: `lefthook install`
- test: `go test ./...`
- gate: `lefthook run pre-commit`

## Agent rules

- Update docs in the same PR as behavior changes.
- Update `pathConciergeSchema` only when the schema file itself moves.
- Keep flow schema-driven; do not add resource-specific branching keyed off resource IDs.
- Run `go test ./...` and `lefthook run pre-commit` before pushing.
