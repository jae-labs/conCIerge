# AGENTS.md — jae-labs/conCIerge

## Overview

Platform consisting of two repositories, with a hard file-path contract between the bot and the Terraform IaC repository:

1. **conCierge Slack Bot** (repository root) — Go bot that opens PRs mutating terraform locals files in the `iac` repository and posts request summaries to `#concierge`.
2. **Ansible Host Config** (external `jae-labs/ansible` repository) — post-provision configuration for the OCI instance, using OCI dynamic inventory and focused host roles.
3. **Terraform IaC** (external `iac` repository) — manages GitHub org, Cloudflare DNS, Doppler secrets, and OCI infrastructure.

---

## Cross-system contract

The bot reads and writes terraform locals files in the `iac` repository directly via the GitHub API. Any rename or restructure of those files breaks the bot unless path constants in `internal/slack/handler.go` are updated in the same change.

| Bot operation            | Terraform file (in `iac` repo)                    | HCL editor function |
|--------------------------|---------------------------------------------------|---------------------|
| Add/delete/update repo   | `github/locals_repos.tf`                          | `AddRepo`, `RemoveRepo`, `UpdateRepo` |
| Read team names          | `github/locals_members.tf`                        | `ExtractTeamNames` |
| Read/update org settings | `github/locals_org.tf`                            | `ExtractOrgSettings`, `UpdateOrgSettings` |
| Add/delete/update DNS    | `cloudflare/locals_dns.tf`                        | `AddDnsRecord`, `RemoveDnsRecord`, `UpdateDnsRecord` |
| Add/delete/update project | `doppler/locals_projects.tf`                      | `AddProject`, `RemoveProject`, `UpdateProject` |

---

## Architecture overview

conCierge is a self-service GitOps platform: Slack modal -> Bot manipulates HCL -> GitHub PR -> CI/CD applies Terraform. Uses Socket Mode (WebSocket) for development, HTTP event subscriptions for production. The bot reads live Terraform files from the external `iac` repository on GitHub to populate dropdowns and validate input, then writes modified HCL back via PR. This creates a tight coupling between the bot and the `iac` repository — changes to either side must consider the other.

---

## Module and commands

- Go module root: repository root (`go.mod` lives at the root)
- Build: `go build ./...`
- Test: `go test ./...`
- Pre-commit checks: MUST run Lefthook checks (`lefthook run pre-commit` from the repository root) and ensure they pass.

---

## Package map

| Package | Role |
|---|---|
| `internal/config` | Env var loading and validation; Slack user/manager/admin role parsing |
| `internal/conversation` | Thread-keyed state machine (`State` struct); nonce-protected flow tracking; RepoConfig, DnsConfig, OrgConfig data structs |
| `internal/github` | GitHub App authenticated client: `GetFileContent`, `CreateBranchFromMain`, `UpdateFile`, `CreatePR`, `UpdatePRBody`, `CommentOnPR`; PR template builders |
| `internal/hcl` | HCL editors for terraform locals files; `editor.go` (repos), `dns_editor.go` (DNS), `org_editor.go` (org settings); template-based rendering, AST parsing, double-validation |
| `internal/slack` | Event handler (Socket Mode for dev, HTTP for prod), interaction routing, Block Kit modal definitions, input validation (repo/dns/org), `#concierge` summaries, PR approval via reactions |

---

## Bot <-> Terraform coupling

The bot fetches Terraform files at runtime via GitHub API and parses them to populate Slack modals. This means:

### Path constants (`internal/slack/handler.go`)
```go
pathGitHubRepos      = "github/locals_repos.tf"
pathGitHubMembers    = "github/locals_members.tf"
pathGitHubOrg        = "github/locals_org.tf"
pathCloudflareDNS    = "cloudflare/locals_dns.tf"
pathDopplerProjects  = "doppler/locals_projects.tf"
```

### What the bot reads from Terraform
| Terraform file | Bot reads | Used for |
|---|---|---|
| `locals_members.tf` | `teams` map keys | Team dropdown options in repo modals |
| `locals_repos.tf` | repo names, full repo configs | Duplicate detection, edit pre-population, delete targets |
| `locals_org.tf` | org settings | Org settings edit pre-population |
| `locals_dns.tf` | DNS record keys and configs | DNS record dropdowns, edit pre-population |
| `locals_projects.tf` | project names, project configs | Project dropdown options, edit pre-population |

### What the bot writes to Terraform
| Action | Terraform file | HCL function |
|---|---|---|
| Add repo | `locals_repos.tf` | `hcleditor.AddRepo()` |
| Delete repo | `locals_repos.tf` | `hcleditor.RemoveRepo()` |
| Edit repo | `locals_repos.tf` | `hcleditor.UpdateRepo()` |
| Add DNS | `locals_dns.tf` | `hcleditor.AddDnsRecord()` |
| Delete DNS | `locals_dns.tf` | `hcleditor.RemoveDnsRecord()` |
| Edit DNS | `locals_dns.tf` | `hcleditor.UpdateDnsRecord()` |
| Edit org | `locals_org.tf` | `hcleditor.UpdateOrgSettings()` |
| Add team member | `locals_members.tf` | `hcleditor.AddTeamMember()` |
| Remove team member | `locals_members.tf` | `hcleditor.RemoveTeamMember()` |
| Change member role | `locals_members.tf` | `hcleditor.UpdateTeamMemberRole()` |
| Add project | `locals_projects.tf` | `hcleditor.AddProject()` |
| Delete project | `locals_projects.tf` | `hcleditor.RemoveProject()` |
| Edit project | `locals_projects.tf` | `hcleditor.UpdateProject()` |

### Impact
- Renaming/restructuring Terraform files breaks the bot at runtime.
- Adding a new field to a Terraform resource requires changes in: HCL editor template, Block Kit modal, handler parsing, conversation state struct, confirmation blocks.
- Removing a team from `locals_members.tf` affects team dropdown options in the bot.

---

## Data fetching and fallbacks

| Function | Source file | Parses with | Fallback on error |
|---|---|---|---|
| `fetchTeamNames()` | `locals_members.tf` | `hcleditor.ExtractTeamNames()` | `["Maintainers"]` (hardcoded) |
| `fetchMemberNames()` | `locals_members.tf` | `hcleditor.ExtractMemberNames()` | `nil` (no fallback) |
| `fetchRepoNames()` | `locals_repos.tf` | `hcleditor.ExistingRepoNames()` | `nil` (no fallback) |

If the bot logs show only "Maintainers" in team dropdowns, check whether `fetchTeamNames()` is hitting an API error and falling back.

---

## Parallel flows: create vs edit repos

Create and edit share the same `RepoConfig` struct and similar 3-step modals, but differ in critical ways. When modifying one flow, check if the other needs the same change.

| Aspect | Create flow | Edit flow |
|---|---|---|
| Callbacks | `CallbackRepoStep1/2/3` | `CallbackSelectRepo`, `CallbackSettingsStep1/2/3` |
| Modal builders | `RepoStep1Modal`, `RepoStep2Modal`, `RepoStep3Modal` | `SelectRepoModal`, `SettingsStep1Modal`, `SettingsStep2Modal`, `SettingsStep3Modal` |
| Step 1 | Collects name + desc + visibility + justification | Collects desc + visibility + justification (no name) |
| Step 2 team access | Multi-select, all default to `"admin"` | Multi-select, preserves existing permission levels |
| Step 2 pre-population | Empty | Pre-populated from existing config |
| Step 3 pre-population | Defaults (protection off, reviews=1) | Pre-populated from existing config |
| Confirmation | `ConfirmationBlocks()` — shows summary | `SettingsConfirmationBlocks()` — shows old vs new diff |
| PR creation | `createPR()` -> `hcleditor.AddRepo()` | `createSettingsPR()` -> `hcleditor.UpdateRepo()` |
| Repo validation | `checkRepoAlreadyExists()` | `checkRepoStillExists()` |

### Common mistake: create/edit parity drift

The create and edit flows are implemented as separate code paths in both `blocks.go` and `handler.go`. When changing behavior (e.g., switching single-select to multi-select for team access), both flows must be updated:
- Modal builder function in `blocks.go`
- Submission parser in `handler.go` (`.SelectedOption.Value` vs `.SelectedOptions`)
- Confirmation display in `blocks.go`

---

## Block Kit constants (`internal/slack/blocks.go`)

All Block/Elem IDs are paired constants at the top of `blocks.go`. `Block*` is the container ID, `Elem*` is the form control ID. Both are needed when reading submission values: `values[BlockFoo][ElemFoo]`.

---

## HCL editing (`internal/hcl/`)

- **Read**: Uses `hcl/v2` AST parsing (safe, structured)
- **Write**: Uses Go templates + string insertion (preserves formatting)
- HCL template for repos is in `editor.go` (`renderRepoEntry`); uses dynamic padding for alignment
- `team_access` is rendered sorted by team name for deterministic output
- All editors double-validate: parse input HCL, modify, parse output HCL
- Test fixtures in `internal/hcl/testdata/` mirror production Terraform structure

---

## Conversation state (`internal/conversation/`)

- `State` struct holds: Phase, Category, ResourceType, ActionType, Priority, RepoConfig, DnsConfig, OrgConfig
- `RepoConfig.TeamAccess` is `map[string]string` (key=team name, value=permission level)
- Valid permission levels: `admin`, `maintain`, `push`, `triage`, `pull`
- `Store` is concurrency-safe in-memory map keyed by thread timestamp
- State is deleted after PR creation or cancel

Access is role-based:

- **User / Manager / Admin**: can initiate flows

Authorization is checked in `handler.go` via `isAuthorized()` (any role).

After PR creation, the handler:
1. Posts a summary message to `#concierge` (channel ID from `SLACK_REQUESTS_CHANNEL_ID`)
2. Replies in the original request thread with a link to the posted request

---

## Nonce-based stale callback protection

Each flow gets a unique nonce (unix-nano + atomic counter, base36-encoded) stored in `State.Nonce`. The nonce is embedded in Block Kit `PrivateMetadata` and block action IDs. Handlers validate the nonce before processing callbacks, preventing stale interactions from affecting a superseded flow.

---

## Parallel flows: DNS, org settings, Doppler projects

DNS add/remove/update, org settings update, and Doppler projects follow the same pattern as repo flows. When modifying a flow for one resource type, check if the same change applies to others.

| Resource | Modal callbacks | HCL editor | PR builder |
|---|---|---|---|
| Repo (add) | `CallbackRepoStep1/2/3` | `AddRepo()` | `BranchName()`, `BuildPRDescription()` |
| Repo (delete) | `CallbackDeleteRepo` | `RemoveRepo()` | `DeleteBranchName()`, `BuildDeletePRDescription()` |
| Repo (update) | `CallbackSettingsStep1/2/3` | `UpdateRepo()` | `SettingsBranchName()`, `BuildSettingsPRDescription()` |
| DNS (add) | `CallbackDnsAdd` | `AddDnsRecord()` | `DnsBranchName("add", ...)` |
| DNS (delete) | `CallbackDnsRemove` | `RemoveDnsRecord()` | `DnsBranchName("delete", ...)` |
| DNS (update) | `CallbackDnsUpdate` | `UpdateDnsRecord()` | `DnsBranchName("settings", ...)` |
| Org settings | `CallbackOrgSettings` | `UpdateOrgSettings()` | `OrgSettingsBranchName()` |
| Member (add) | `CallbackTeamMemberAdd` | `AddTeamMember()` | `MemberBranchName("add", ...)` |
| Member (remove) | `CallbackTeamMemberRemove` | `RemoveTeamMember()` | `MemberBranchName("delete", ...)` |
| Member (change role) | `CallbackTeamMemberChangeRole` | `UpdateTeamMemberRole()` | `MemberBranchName("change_role", ...)` |
| Doppler project (add) | `CallbackDopplerAdd` | `AddProject()` | `DopplerBranchName("add", ...)` |
| Doppler project (delete) | `CallbackDopplerRemove` | `RemoveProject()` | `DopplerBranchName("delete", ...)` |
| Doppler project (update) | `CallbackDopplerUpdate` | `UpdateProject()` | `DopplerBranchName("settings", ...)` |

---

## Key constraints

**HCL field names**: changing a field name requires updating the HCL editor template, Block Kit modal, handler parser, and confirmation blocks. Missing any will silently produce malformed Terraform or broken UI.

**Test data**: `internal/hcl/testdata/` mirrors production Terraform files. Update fixtures when adding new HCL editor features.

**Confirmation blocks**: create (`ConfirmationBlocks`) takes ~18 positional parameters. Edit (`SettingsConfirmationBlocks`) takes old/new config and diffs them. Adding a new field to `RepoConfig` requires updating both.

---

## Documentation reference

Reference docs live in `docs/`:

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Package map, state machine, request lifecycle, config structs, IaC coupling |
| [Adding a Resource Type](docs/adding-a-resource-type.md) | 11-step guide for adding new terraform resource support |
| [Validation Patterns](docs/validation-patterns.md) | Input validation rules per resource, error patterns |
| [Modals and Blocks](docs/modals-and-blocks.md) | Block Kit patterns, wizard flows, ID pairing, existing modals reference |

---

## CI/CD triggers

Workflows live in `.github/workflows/`. Triggering is path-based:

- Go codebase changes (`cmd/**`, `internal/**`, `docs/**`, `Dockerfile`, `go.mod`, `.air.toml`, etc.) and `.github/workflows/ci.yml` / `.github/workflows/release.yml` trigger repository CI validation (`ci.yml`).
- On push to `main` only, the same Go codebase changes additionally trigger bot releases (`release.yml`).
- Note: Terraform apply workflows are managed entirely within the `iac` repository's CI pipelines.

---

## Agent rules

- MUST update the five path constants in `internal/slack/handler.go` whenever a terraform locals file is renamed or moved.
- MUST run `go test ./...` from the repository root after any bot changes.
- MUST run Lefthook pre-commit checks (`lefthook run pre-commit` at repository root) after any changes and ensure all checks pass.
- Test data in `internal/hcl/testdata/` mirrors the structure of the terraform locals files; keep it in sync when terraform file structure changes.

---

## Documentation maintenance

Documentation MUST be updated in the same PR as the code change it relates to.

| Change type | Update required |
|---|---|
| New/modified bot resource type | `docs/adding-a-resource-type.md` checklist summary, `docs/architecture.md` config structs |
| New/modified validation rule | `docs/validation-patterns.md` |
| New/modified Block Kit modal | `docs/modals-and-blocks.md` existing modals table |
| New bot-terraform file coupling | Cross-system contract table (this file), `AGENTS.md` |

Format standard: tables over prose, no emojis, concise.
