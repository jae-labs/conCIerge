# Agent Instructions — conCierge Slack Bot

## Architecture overview

conCierge is a self-service GitOps platform: Slack modal -> Bot manipulates HCL -> GitHub PR -> CI/CD applies Terraform. Uses Socket Mode (WebSocket) for development, HTTP event subscriptions for production. The bot reads live Terraform files from GitHub to populate dropdowns and validate input, then writes modified HCL back via PR. This creates a tight coupling between `src/` and `terraform/` — changes to either side must consider the other.

## Module and commands

- Go module root: `src/` (go.mod lives here, NOT at repo root)
- Build: `pushd src && go build ./...`
- Test: `pushd src && go test ./...`
- MUST run from `src/` directory — `cd` alone may not work in all shells, use `pushd`

## Package map

| Package | Role |
|---|---|
| `internal/config` | Env var loading and validation; Slack user/manager/admin role parsing |
| `internal/conversation` | Thread-keyed state machine (`State` struct); nonce-protected flow tracking; RepoConfig, DnsConfig, OrgConfig data structs |
| `internal/github` | GitHub App authenticated client: `GetFileContent`, `CreateBranchFromMain`, `UpdateFile`, `CreatePR`, `UpdatePRBody`, `CommentOnPR`; PR template builders |
| `internal/hcl` | HCL editors for terraform locals files; `editor.go` (repos), `dns_editor.go` (DNS), `org_editor.go` (org settings); template-based rendering, AST parsing, double-validation |
| `internal/slack` | Event handler (Socket Mode for dev, HTTP for prod), interaction routing, Block Kit modal definitions, input validation (repo/dns/org), `#concierge` summaries, PR approval via reactions |

## Bot <-> Terraform coupling

The bot fetches Terraform files at runtime via GitHub API and parses them to populate Slack modals. This means:

### Path constants (`handler.go`)
```
pathGitHubRepos   = "terraform/github/locals_repos.tf"
pathGitHubMembers = "terraform/github/locals_members.tf"
pathGitHubOrg     = "terraform/github/locals_org.tf"
pathCloudflareDNS = "terraform/cloudflare/locals_dns.tf"
```

### What the bot reads from Terraform
| Terraform file | Bot reads | Used for |
|---|---|---|
| `locals_members.tf` | `teams` map keys | Team dropdown options in repo modals |
| `locals_repos.tf` | repo names, full repo configs | Duplicate detection, edit pre-population, delete targets |
| `locals_org.tf` | org settings | Org settings edit pre-population |
| `locals_dns.tf` | DNS record keys and configs | DNS record dropdowns, edit pre-population |

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

### Impact
- Renaming/restructuring Terraform files breaks the bot at runtime
- Adding a new field to a Terraform resource requires changes in: HCL editor template, Block Kit modal, handler parsing, conversation state struct, confirmation blocks
- Removing a team from `locals_members.tf` affects team dropdown options in the bot

## Data fetching and fallbacks

| Function | Source file | Parses with | Fallback on error |
|---|---|---|---|
| `fetchTeamNames()` | `locals_members.tf` | `hcleditor.ExtractTeamNames()` | `["Maintainers"]` (hardcoded) |
| `fetchMemberNames()` | `locals_members.tf` | `hcleditor.ExtractMemberNames()` | `nil` (no fallback) |
| `fetchRepoNames()` | `locals_repos.tf` | `hcleditor.ExistingRepoNames()` | `nil` (no fallback) |

If the bot logs show only "Maintainers" in team dropdowns, check whether `fetchTeamNames()` is hitting an API error and falling back.

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

## Block Kit constants (`blocks.go`)

All Block/Elem IDs are paired constants at the top of `blocks.go`. `Block*` is the container ID, `Elem*` is the form control ID. Both are needed when reading submission values: `values[BlockFoo][ElemFoo]`.

## HCL editing (`internal/hcl/`)

- **Read**: Uses `hcl/v2` AST parsing (safe, structured)
- **Write**: Uses Go templates + string insertion (preserves formatting)
- HCL template for repos is in `editor.go` (`renderRepoEntry`); uses dynamic padding for alignment
- `team_access` is rendered sorted by team name for deterministic output
- All editors double-validate: parse input HCL, modify, parse output HCL
- Test fixtures in `internal/hcl/testdata/` mirror production Terraform structure

## Conversation state (`internal/conversation/`)

- `State` struct holds: Phase, Category, ResourceType, ActionType, Priority, RepoConfig, DnsConfig, OrgConfig
- `RepoConfig.TeamAccess` is `map[string]string` (key=team name, value=permission level)
- Valid permission levels: `admin`, `maintain`, `push`, `triage`, `pull`
- `Store` is concurrency-safe in-memory map keyed by thread timestamp
- State is deleted after PR creation or cancel

## Access control

Access is role-based:

- **User**: can initiate flows
- **Manager/Admin**: can initiate flows and approve requests via `+1`/`:thumbsup:` reaction

Authorization is checked in `handler.go` via `isAuthorized()` (any role) and `isApprover()` (manager or admin only).

## PR completion workflow

After PR creation, the handler:
1. Posts a summary message to `#concierge` (channel ID from `SLACK_REQUESTS_CHANNEL_ID`)
2. Monitors `reaction_added` events for `+1`/`thumbsup` on that top-level message only
3. `handlePRApproval()` validates the reactor is an approver, extracts PR URL from the message, and comments the approval on the PR
4. Replies in the original request thread with a link to the posted request

## Nonce-based stale callback protection

Each flow gets a unique nonce (unix-nano + atomic counter, base36-encoded) stored in `State.Nonce`. The nonce is embedded in Block Kit `PrivateMetadata` and block action IDs. Handlers validate the nonce before processing callbacks, preventing stale interactions from affecting a superseded flow.

## Parallel flows: DNS and org settings

DNS add/remove/update and org settings update follow the same pattern as repo flows. When modifying a flow for one resource type, check if the same change applies to others.

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

## Key constraints

**HCL field names**: changing a field name requires updating the HCL editor template, Block Kit modal, handler parser, and confirmation blocks. Missing any will silently produce malformed Terraform or broken UI.

**Test data**: `internal/hcl/testdata/` mirrors production Terraform files. Update fixtures when adding new HCL editor features.

**Confirmation blocks**: create (`ConfirmationBlocks`) takes ~18 positional parameters. Edit (`SettingsConfirmationBlocks`) takes old/new config and diffs them. Adding a new field to `RepoConfig` requires updating both.

## Documentation

Reference docs live in `docs/`:

| Document | Description |
|---|---|
| [Architecture](docs/architecture.md) | Package map, state machine, request lifecycle, config structs, IaC coupling |
| [Adding a Resource Type](docs/adding-a-resource-type.md) | 11-step guide for adding new terraform resource support |
| [Validation Patterns](docs/validation-patterns.md) | Input validation rules per resource, error patterns |
| [Modals and Blocks](docs/modals-and-blocks.md) | Block Kit patterns, wizard flows, ID pairing, existing modals reference |

### Documentation maintenance

Documentation MUST be updated in the same PR as the code change.

| Change type | Update required |
|---|---|
| New resource type | `adding-a-resource-type.md` checklist, `architecture.md` config structs, root `AGENTS.md` contract table |
| New/modified validation | `validation-patterns.md` rules table |
| New/modified modal | `modals-and-blocks.md` existing modals table |
| New config struct field | `architecture.md` config struct table |
| New bot-terraform coupling | Path constants section (this file), `terraform/AGENTS.md`, root `AGENTS.md` |

Format: tables over prose, no emojis, exact file paths and function names.
