# Architecture

How the Opsy bot is structured, key files, data flow, and IaC coupling.

## Package map

| Package | Key files | Responsibility |
|---|---|---|
| `cmd/opsy` | `main.go` | Entry point: loads config, initializes clients (Slack, GitHub, Linear), starts handler |
| `internal/config` | `config.go` | Env var loading; RBAC role sets (user/manager/admin) parsed from comma-separated IDs |
| `internal/conversation` | `state.go`, `repo.go`, `dns.go`, `org.go`, `team_member.go` | Thread-keyed state machine with nonce protection; config structs for each resource type |
| `internal/github` | `client.go`, `pr.go` | GitHub App auth client: file ops, branch/commit/PR creation; PR title/description/branch name builders |
| `internal/hcl` | `editor.go`, `dns_editor.go`, `org_editor.go`, `member_editor.go` | HCL text editors: reads via AST parsing (hcl/v2), writes via Go templates + string insertion; double-validates output |
| `internal/linear` | `client.go` | Linear GraphQL client: creates tracking issues with priority mapping (high=2, medium=3, low=4) |
| `internal/slack` | `handler.go`, `blocks.go`, `*_validation.go` | Event routing, Block Kit modals, interaction handling, input validation, PR creation flows |

## State machine

```mermaid
stateDiagram-v2
    PhaseIdle --> PhaseCategorySelected: user selects category
    PhaseCategorySelected --> PhaseResourceSelected: user selects resource
    PhaseResourceSelected --> PhaseActionSelected: user selects action
    PhaseActionSelected --> PhaseWizardStep1: opens modal
    PhaseWizardStep1 --> PhaseWizardStep2: step 1 submitted
    PhaseWizardStep2 --> PhaseWizardStep3: step 2 submitted (repos only)
    PhaseWizardStep3 --> PhaseConfirming: step 3 submitted
    PhaseWizardStep2 --> PhaseConfirming: step submitted (DNS/org/members)
    PhaseConfirming --> PhaseCreatingPR: user confirms
    PhaseCreatingPR --> [*]: PR created, state cleaned up
```

## Request lifecycle

```mermaid
sequenceDiagram
    participant U as User
    participant S as Slack
    participant B as Bot
    participant GH as GitHub
    participant L as Linear
    participant Ch as #opsy-requests

    U->>S: opens assistant thread
    S->>B: assistant_thread_started event
    B->>B: creates State, stores in map[threadTS]
    B->>S: posts welcome message + category buttons

    U->>S: selects category
    S->>B: block_action callback
    B->>B: locks previous buttons, advances phase
    B->>S: posts resource buttons

    U->>S: selects resource
    S->>B: block_action callback
    B->>S: posts action buttons

    U->>S: selects action
    S->>B: block_action callback
    B->>S: opens Block Kit modal (wizard step 1)

    loop wizard steps (1-3 depending on resource)
        U->>S: submits modal step
        S->>B: view_submission callback
        B->>B: validates input
        B->>S: pushes next modal step or confirmation
    end

    U->>S: confirms
    S->>B: block_action callback (confirm button)
    B->>GH: fetches terraform file via GitHub API
    B->>B: runs HCL editor (AST parse + template write)
    B->>GH: creates branch, commits modified HCL, creates PR
    B->>L: creates Linear tracking issue
    B->>Ch: posts summary to #opsy-requests
    B->>S: posts PR link in thread
    B->>B: deletes State from store

    Note over Ch: manager reacts with +1
    Ch->>B: reaction_added event
    B->>GH: comments approval on PR
```

## Config structs

### RepoConfig

| Field | Type |
|---|---|
| Name | string |
| Description | string |
| Visibility | string |
| Topics | []string |
| TeamAccess | map[string]string |
| DefaultBranch | string |
| HasIssues | bool |
| HasWiki | bool |
| HasProjects | bool |
| HasDiscussions | bool |
| HomepageURL | string |
| AllowAutoMerge | bool |
| AllowUpdateBranch | bool |
| DeleteBranchOnMerge | bool |
| EnableBranchProtection | bool |
| RequiredReviews | int |
| DismissStaleReviews | bool |
| RequireLinearHistory | bool |
| RequireConversationResolution | bool |

### DnsConfig

| Field | Type | Notes |
|---|---|---|
| RecordKey | string | |
| Type | string | A, AAAA, CNAME, MX, TXT |
| Name | string | |
| Content | string | |
| Proxied | bool | A/AAAA/CNAME only |
| Priority | int | MX only |
| Comment | string | |

### OrgConfig

| Field | Type |
|---|---|
| Name | string |
| BillingEmail | string |
| Blog | string |
| Description | string |
| Location | string |
| MembersCanCreateRepos | bool |
| DefaultRepoPermission | string (read/write/admin/none) |
| WebCommitSignoffRequired | bool |
| DependabotAlerts | bool |
| DependabotSecurityUpdates | bool |
| DependencyGraph | bool |

### TeamMemberConfig

| Field | Type | Notes |
|---|---|---|
| Team | string | |
| Username | string | |
| Role | string | member or maintainer |

## IaC coupling

The bot fetches terraform files at runtime via GitHub API to populate modals and validate input, then writes modified HCL back via PR.

Path constants (defined in `handler.go`):

```
pathGitHubRepos   = "iac/terraform/github/locals_repos.tf"
pathGitHubMembers = "iac/terraform/github/locals_members.tf"
pathGitHubOrg     = "iac/terraform/github/locals_org.tf"
pathCloudflareDNS = "iac/terraform/cloudflare/locals_dns.tf"
```

Impact rules:

| Change | Required updates |
|---|---|
| Rename/restructure terraform files | Bot breaks at runtime (path constants are hardcoded) |
| Add field to terraform resource | HCL editor, Block Kit modal, handler parser, conversation state struct, confirmation blocks |
| Modify HCL block structure | HCL editor AST parsing, test fixtures in `internal/hcl/testdata/` |

Test fixtures in `internal/hcl/testdata/` must mirror production terraform file structure.

## Nonce protection

Each flow gets a unique nonce: unix-nano + atomic counter, base36-encoded. Embedded in modal `PrivateMetadata` as `threadTS:nonce`. Handlers validate nonce before processing -- prevents stale callbacks from superseded flows.

## RBAC

Three tiers from comma-separated env vars:

| Role | Env var | Permissions |
|---|---|---|
| User | `SLACK_USER_IDS` | Initiate flows |
| Manager | `SLACK_MANAGER_IDS` | Initiate + approve |
| Admin | `SLACK_ADMIN_IDS` | Initiate + approve |

Checked via `isAuthorized()` (any role) and `isApprover()` (manager/admin only).

## Store

In-memory `map[string]*State` keyed by thread timestamp. Thread-safe via `sync.Mutex`. State created on flow start, deleted after PR creation or cancel. No persistence -- restart loses in-flight flows.
