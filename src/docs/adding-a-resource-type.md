# Adding a New Resource Type

Step-by-step guide for adding support for a new terraform resource (e.g., Doppler) to the conCierge bot.

## Before you start

Evaluate scope:

1. Which terraform locals files will the bot read/write?
2. What actions are needed? (add, delete, update, or subset)
3. What fields does each action collect from the user?
4. What validation rules apply?
5. Are there multi-step wizards or single-step modals?

Study existing patterns: repo flows for multi-step (3 wizard steps), DNS flows for single-step, org settings for update-only, team members for add/remove/change_role.

## Step-by-step checklist

### Step 1: Add path constants

File: `internal/slack/handler.go` (top of file, lines ~22-25)

Add path constant for each terraform file the bot will read/write:

```go
pathDopplerProjects = "terraform/doppler/locals_projects.tf"
pathDopplerAccess   = "terraform/doppler/locals_access.tf"
```

### Step 2: Add config struct

File: `internal/conversation/` -- create new file (e.g., `doppler.go`)

Define a struct holding all fields collected during the wizard:

```go
type DopplerProjectConfig struct {
    Name         string
    Description  string
    Environments map[string]DopplerEnvConfig
}
```

Follow patterns from `repo.go`, `dns.go`, `org.go`, `team_member.go`.

### Step 3: Add to State struct

File: `internal/conversation/state.go`

Add config field to State:

```go
DopplerProjectConfig DopplerProjectConfig
```

Add any target fields needed for update/delete flows (e.g., `TargetProject string`).

### Step 4: Create HCL editor

File: `internal/hcl/` -- create new file (e.g., `doppler_editor.go`)

Implement functions following existing patterns:

- Read functions (AST-based): `ExistingProjectNames()`, `ExtractProjectConfig()`
- Write functions (template-based): `AddProject()`, `RemoveProject()`, `UpdateProject()`

Key patterns:

- Read uses `hclsyntax` for safe AST parsing
- Write uses Go templates + byte insertion to preserve formatting
- All editors must double-validate: parse input, modify, parse output
- Sorted output for determinism (see `team_access` in `editor.go`)

### Step 5: Add test fixtures

File: `internal/hcl/testdata/` -- create fixture file (e.g., `locals_projects.tf`)

Mirror the production terraform file structure. Write tests in `doppler_editor_test.go`.

### Step 6: Create validation

File: `internal/slack/` -- create new file (e.g., `doppler_validation.go`)

Validation function signatures:

- Multi-field modals: `func validateDopplerFields(values map[string]map[string]slack.BlockAction) map[string]string`
- Returns map of blockID -> error message (empty map = valid)
- Simple validations: `func validateDopplerRemove(name string) string` (empty = valid)

Refer to existing patterns in `repo_validation.go`, `dns_validation.go`, `org_validation.go`, `member_validation.go`.

### Step 7: Add Block Kit constants and modals

File: `internal/slack/blocks.go`

1. Add callback constants:

```go
CallbackDopplerAdd    = "doppler_add"
CallbackDopplerRemove = "doppler_remove"
```

2. Add Block/Elem ID pairs:

```go
BlockDopplerName = "block_doppler_name"
ElemDopplerName  = "elem_doppler_name"
```

Convention: `Block*` is container ID, `Elem*` is form control ID. Both needed to read values: `values[BlockID][ElemID]`.

3. Add modal builder functions returning `slack.ModalViewRequest`:

- Set `CallbackID` to the callback constant
- Set `PrivateMetadata` to `threadTS + ":" + nonce`
- Use `slack.NewSectionBlock`, `slack.NewInputBlock`, etc.

4. Add summary builder functions returning formatted strings for confirmation messages.

Study `DnsAddModal()` for single-step pattern, `RepoStep1Modal()`/`RepoStep2Modal()`/`RepoStep3Modal()` for multi-step.

### Step 8: Add handler routing

File: `internal/slack/handler.go`

1. Add category/resource/action routing in `handleBlockAction()`:
   - When category "doppler" selected, show resource options
   - When resource selected, show action options
   - When action selected, open modal

2. Add view submission routing in `handleViewSubmission()`:
   - Match on CallbackID
   - Extract threadTS and nonce from PrivateMetadata
   - Validate nonce matches state
   - Parse form values from `view.State.Values`
   - Call validation function
   - If errors, return `slack.ViewSubmissionResponse` with `ResponseActionErrors`
   - If valid, update state and either push next modal step or trigger PR creation

3. Add data fetching function (follows pattern of `fetchTeamNames()`, `fetchRepoNames()`):

```go
func (h *Handler) fetchProjectNames() []string {
    ctx := context.Background()
    src, _, err := h.gh.GetFileContent(ctx, pathDopplerProjects)
    // parse and return, with fallback on error
}
```

### Step 9: Add PR builder functions

File: `internal/github/pr.go`

Add branch name and PR description builders:

```go
func DopplerBranchName(action, name string) string { ... }
func BuildDopplerPRDescription(action, name, requester, justification string) string { ... }
```

Follow naming pattern: `concierge/{action}-doppler-{sanitized}-{timestamp}`.

### Step 10: Wire up PR creation

File: `internal/slack/handler.go`

Create async PR creation function (follows pattern of `createDnsAddPR()`, `createPR()`):

1. Post "creating PR..." progress message
2. Fetch terraform file via GitHub API (content + SHA)
3. Run HCL editor function
4. Create branch from main
5. Commit modified file
6. Create PR
7. Call `replyPR()` which posts the request summary as a top-level message in `#concierge` and replies in the original request thread with the request link

### Step 11: Update documentation

- Add bot integration status to terraform module doc (`terraform/docs/doppler-module.md`)
- Update `src/AGENTS.md` cross-system contract tables
- Update root `AGENTS.md` cross-system contract table

## Create vs edit parity

If implementing both create and edit flows, they share the same config struct but differ in:

- Callbacks (separate constants)
- Modal builders (separate functions)
- Pre-population (edit populates from existing config)
- Confirmation (create shows summary, edit shows diff)

When changing one flow, always check if the other needs the same change. See repo create vs settings flows in `blocks.go` and `handler.go`.

## Checklist summary

| Step | File(s) | What |
|---|---|---|
| 1 | `handler.go` | Path constants |
| 2 | `conversation/*.go` | Config struct |
| 3 | `conversation/state.go` | State fields |
| 4 | `hcl/*_editor.go` | HCL read/write |
| 5 | `hcl/testdata/` | Test fixtures + tests |
| 6 | `slack/*_validation.go` | Input validation |
| 7 | `slack/blocks.go` | Modals, constants, summaries |
| 8 | `slack/handler.go` | Event routing, form parsing |
| 9 | `github/pr.go` | Branch names, PR descriptions |
| 10 | `slack/handler.go` | Async PR creation |
| 11 | docs, AGENTS.md | Documentation |
