# AGENTS.md — jae-labs/conCIerge

## Overview

Monorepo for three systems, with a hard file-path contract between the bot and Terraform:

1. **Terraform IaC** (`terraform/`) — manages GitHub org, Cloudflare DNS, Doppler secrets, and OCI infrastructure via four root modules. Reusable modules exist for GitHub, Cloudflare, and Doppler; OCI is a flat root module.
2. **conCierge Slack Bot** (`src/`) — Go bot that opens PRs mutating terraform locals files, posts request summaries to `#concierge`, and records manager/admin approvals via reactions.
3. **Ansible Host Config** (`ansible/`) — manual-first post-provision configuration for the OCI instance, using OCI dynamic inventory and focused host roles.

## Cross-system contract

The bot reads and writes terraform locals files directly via the GitHub API. Any rename or restructure of those files breaks the bot unless path constants in `src/internal/slack/handler.go` are updated in the same change.

| Bot operation            | Terraform file                                    | HCL editor function |
|--------------------------|---------------------------------------------------|---------------------|
| Add/delete/update repo   | `terraform/github/locals_repos.tf`            | `AddRepo`, `RemoveRepo`, `UpdateRepo` |
| Read team names          | `terraform/github/locals_members.tf`          | `ExtractTeamNames` |
| Read/update org settings | `terraform/github/locals_org.tf`              | `ExtractOrgSettings`, `UpdateOrgSettings` |
| Add/delete/update DNS    | `terraform/cloudflare/locals_dns.tf`          | `AddDnsRecord`, `RemoveDnsRecord`, `UpdateDnsRecord` |

## Component guidelines

- `terraform/AGENTS.md` — terraform module conventions, variable naming, state backend.
- `src/AGENTS.md` — bot architecture, HCL parsing, PR creation flow, test patterns.
- `ansible/README.md` — ansible layout, OCI inventory usage, and local run commands.

## CI

Workflows live in `.github/workflows/`. Triggering is path-based:

- `src/**`, `.github/workflows/ci.yml`, and `.github/workflows/release.yml` trigger bot CI and releases (`ci.yml`, `release.yml`).
- `terraform/github/**` or `terraform/modules/github/**` triggers `github-apply.yml`.
- `terraform/cloudflare/**` or `terraform/modules/cloudflare/**` triggers `cloudflare-apply.yml`.
- `terraform/doppler/**` or `terraform/modules/doppler/**` triggers `doppler-apply.yml`.
- `terraform/oci/**` triggers `oci-apply.yml`.
- `ansible/**` has no CI workflow yet; it is run manually.

## Agent rules

- MUST update the four path constants in `src/internal/slack/handler.go` whenever a terraform locals file is renamed or moved.
- MUST run `go test ./...` from `src/` after any bot changes.
- MUST NOT modify terraform files and bot files in the same PR — they have different review concerns and CI pipelines.
- Test data in `src/internal/hcl/testdata/` mirrors the structure of the terraform locals files; keep it in sync when terraform file structure changes.

## Documentation maintenance

Documentation MUST be updated in the same PR as the code change it relates to.

| Change type | Update required |
|---|---|
| New/modified terraform module variable | Module doc in `terraform/docs/{module}-module.md` |
| New/modified bot resource type | `src/docs/adding-a-resource-type.md` checklist summary, `src/docs/architecture.md` config structs |
| New/modified validation rule | `src/docs/validation-patterns.md` |
| New/modified Block Kit modal | `src/docs/modals-and-blocks.md` existing modals table |
| New bot-terraform file coupling | Cross-system contract table (this file), `src/AGENTS.md`, `terraform/AGENTS.md` |
| New/modified ansible operational flow | `ansible/README.md`, root `README.md`, and OCI docs if host configuration responsibilities change |
| New CI workflow or secret | `terraform/docs/ci-cd.md` |

Format standard: tables over prose, no emojis, concise. Module docs follow the format in `terraform/docs/github-module.md`.
