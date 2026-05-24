# Validation Patterns

Input validation rules and patterns for each resource type.

## How validation works

Two patterns:

1. **Modal validation** -- called during `handleViewSubmission()`. Returns `map[string]string` (blockID -> error message). Empty map = valid. Errors are returned to Slack via `ViewSubmissionResponse.ResponseActionErrors` which highlights the invalid fields in the modal.

2. **Cross-record validation** -- called after modal validation passes. Returns a single error string. If non-empty, posted as a thread reply and modal is closed without proceeding.

## Validation rules by resource

### Repository (repo_validation.go)

| Field | Rule |
|---|---|
| Name | Required, <= 100 chars, matches `^[a-zA-Z0-9][a-zA-Z0-9._-]*$`, cannot end with `.` |
| Description | Required |
| Default branch | Required, no `..`, space, `~`, `^`, `:`, `\`, `?`, `*`, `[`; cannot start with `-`/`.`; cannot end with `.`/`/` |
| Required reviews | If present, must be integer 1-5 |

Cross-record: `checkRepoAlreadyExists()` (case-insensitive) for create, `checkRepoStillExists()` (case-sensitive) for delete/edit.

Settings validations mirror create but skip name (read-only in edit).

### DNS (dns_validation.go)

| Field | Rule |
|---|---|
| Name | Required |
| Content | Required; A: valid IPv4; AAAA: valid IPv6; CNAME/MX: hostname (not IP), matches `^([a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?\.)*[a-zA-Z0-9]([a-zA-Z0-9-]*[a-zA-Z0-9])?$`; TXT: any non-empty |
| Priority | Required for MX, must be positive integer |
| Proxied | Error if enabled for MX or TXT |

Cross-record: `checkDnsConflict()` -- CNAME exclusivity: CNAME cannot coexist with any other record on the same name, and no record can be added alongside an existing CNAME.

Record key generation: `generateDnsRecordKey()` derives terraform key from name + type + random hex suffix. Pattern: `{normalized-name}-{type}-{hex}`.

### Organization settings (org_validation.go)

| Field | Rule |
|---|---|
| Name | Required (trimmed) |
| Billing email | Required, must contain `@` |
| Blog | If present, must start with `http://` or `https://` |
| Description | Required (trimmed) |

### Team members (member_validation.go)

| Field | Rule |
|---|---|
| Team | Required |
| Username | Required |
| Role | Required, must be `member` or `maintainer` |

Add/change_role validate all three. Remove validates team + username only.

## Adding validation for a new resource type

1. Create `internal/slack/{resource}_validation.go`
2. Implement `validate{Resource}Fields(values map[string]map[string]slack.BlockAction) map[string]string` for modal fields
3. Implement cross-record validation function if needed (returns `string`)
4. Call modal validation in `handleViewSubmission()` before proceeding
5. Call cross-record validation after modal validation passes
6. Add tests in `{resource}_validation_test.go`

## Error return patterns

Modal errors (map):

```go
errs := make(map[string]string)
if name == "" {
    errs[BlockName] = "Name is required."
}
return errs
```

Cross-record errors (string):

```go
func checkConflict(...) string {
    // return "" if valid, error message if invalid
}
```
