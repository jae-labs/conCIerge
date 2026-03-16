# AGENTS.md — jae-labs/opsy

## Overview

Monorepo for two systems with a hard file-path contract between them:

1. **Terraform IaC** (`iac/terraform/`) — manages GitHub org, Cloudflare DNS, Doppler secrets via three root modules and reusable modules (github, cloudflare, doppler).
2. **Opsy Slack Bot** (`bot/slack/`) — Go bot that opens PRs mutating terraform locals files. Creates Linear tracking issues and monitors a Slack requests channel for reaction-based approvals.

## Cross-system contract

The bot reads and writes terraform locals files directly via the GitHub API. Any rename or restructure of those files breaks the bot unless path constants in `bot/slack/internal/slack/handler.go` are updated in the same change.

| Bot operation            | Terraform file                                    | HCL editor function |
|--------------------------|---------------------------------------------------|---------------------|
| Add/delete/update repo   | `iac/terraform/github/locals_repos.tf`            | `AddRepo`, `RemoveRepo`, `UpdateRepo` |
| Read team names          | `iac/terraform/github/locals_members.tf`          | `ExtractTeamNames` |
| Read/update org settings | `iac/terraform/github/locals_org.tf`              | `ExtractOrgSettings`, `UpdateOrgSettings` |
| Add/delete/update DNS    | `iac/terraform/cloudflare/locals_dns.tf`          | `AddDnsRecord`, `RemoveDnsRecord`, `UpdateDnsRecord` |

## Component guidelines

- `iac/terraform/AGENTS.md` — terraform module conventions, variable naming, state backend.
- `bot/slack/AGENTS.md` — bot architecture, HCL parsing, PR creation flow, test patterns.

## CI

Workflows live in `.github/workflows/`. Triggering is path-based:

- `bot/slack/**` triggers Go build and test (`opsy-build.yml`).
- `iac/terraform/github/**` or `iac/terraform/modules/github/**` triggers `github-apply.yml`.
- `iac/terraform/cloudflare/**` or `iac/terraform/modules/cloudflare/**` triggers `cloudflare-apply.yml`.
- `iac/terraform/doppler/**` or `iac/terraform/modules/doppler/**` triggers `doppler-apply.yml`.

## Agent rules

- MUST update the four path constants in `bot/slack/internal/slack/handler.go` whenever a terraform locals file is renamed or moved.
- MUST run `go test ./...` from `bot/slack/` after any bot changes.
- MUST NOT modify terraform files and bot files in the same PR — they have different review concerns and CI pipelines.
- Test data in `bot/slack/internal/hcl/testdata/` mirrors the structure of the terraform locals files; keep it in sync when terraform file structure changes.

## Documentation maintenance

Documentation MUST be updated in the same PR as the code change it relates to.

| Change type | Update required |
|---|---|
| New/modified terraform module variable | Module doc in `iac/terraform/docs/{module}-module.md` |
| New/modified bot resource type | `bot/slack/docs/adding-a-resource-type.md` checklist summary, `bot/slack/docs/architecture.md` config structs |
| New/modified validation rule | `bot/slack/docs/validation-patterns.md` |
| New/modified Block Kit modal | `bot/slack/docs/modals-and-blocks.md` existing modals table |
| New bot-terraform file coupling | Cross-system contract table (this file), `bot/slack/AGENTS.md`, `iac/terraform/AGENTS.md` |
| New CI workflow or secret | `iac/terraform/docs/ci-cd.md` |

Format standard: tables over prose, no emojis, concise. Module docs follow the format in `iac/terraform/docs/github-module.md`.
