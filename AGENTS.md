# AGENTS.md — jae-labs/conCIerge

## Overview

Platform consisting of two repositories, with a hard file-path contract between the bot and the Terraform IaC repository:

1. **conCierge Slack Bot** (`src/`) — Go bot in this monorepo that opens PRs mutating terraform locals files in the `iac` repository and posts request summaries to `#concierge`.
2. **Ansible Host Config** (`ansible/`) — manual-first post-provision configuration for the OCI instance in this monorepo, using OCI dynamic inventory and focused host roles.
3. **Terraform IaC** (external `iac` repository) — manages GitHub org, Cloudflare DNS, Doppler secrets, and OCI infrastructure.

## Cross-system contract

The bot reads and writes terraform locals files in the `iac` repository directly via the GitHub API. Any rename or restructure of those files breaks the bot unless path constants in `src/internal/slack/handler.go` are updated in the same change.

| Bot operation            | Terraform file (in `iac` repo)                    | HCL editor function |
|--------------------------|---------------------------------------------------|---------------------|
| Add/delete/update repo   | `github/locals_repos.tf`                          | `AddRepo`, `RemoveRepo`, `UpdateRepo` |
| Read team names          | `github/locals_members.tf`                        | `ExtractTeamNames` |
| Read/update org settings | `github/locals_org.tf`                            | `ExtractOrgSettings`, `UpdateOrgSettings` |
| Add/delete/update DNS    | `cloudflare/locals_dns.tf`                        | `AddDnsRecord`, `RemoveDnsRecord`, `UpdateDnsRecord` |
| Add/delete/update project | `doppler/locals_projects.tf`                      | `AddProject`, `RemoveProject`, `UpdateProject` |

## Component guidelines

- `src/AGENTS.md` — bot architecture, HCL parsing, PR creation flow, test patterns.
- `ansible/README.md` — ansible layout, OCI inventory usage, and local run commands.

## CI

Workflows live in `.github/workflows/`. Triggering is path-based:

- `src/**`, `ansible/**`, `.github/workflows/ci.yml`, and `.github/workflows/release.yml` trigger repository CI validation (`ci.yml`).
- `src/**` (on push to `main` only) additionally triggers bot releases (`release.yml`).
- Note: Terraform apply workflows are managed entirely within the `iac` repository's CI pipelines.

## Agent rules

- MUST update the five path constants in `src/internal/slack/handler.go` whenever a terraform locals file is renamed or moved.
- MUST run `go test ./...` from `src/` after any bot changes.
- MUST run Lefthook pre-commit checks (`lefthook run pre-commit` at repository root) after any changes and ensure all checks pass.
- Test data in `src/internal/hcl/testdata/` mirrors the structure of the terraform locals files; keep it in sync when terraform file structure changes.

## Documentation maintenance

Documentation MUST be updated in the same PR as the code change it relates to.

| Change type | Update required |
|---|---|
| New/modified bot resource type | `src/docs/adding-a-resource-type.md` checklist summary, `src/docs/architecture.md` config structs |
| New/modified validation rule | `src/docs/validation-patterns.md` |
| New/modified Block Kit modal | `src/docs/modals-and-blocks.md` existing modals table |
| New bot-terraform file coupling | Cross-system contract table (this file), `src/AGENTS.md` |
| New/modified ansible operational flow | `ansible/README.md`, root `README.md`, and OCI docs if host configuration responsibilities change |

Format standard: tables over prose, no emojis, concise.
