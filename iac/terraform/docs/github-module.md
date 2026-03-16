# GitHub Module

Manages the `jae-labs` GitHub organization: members, teams, repositories, environments, and branch protection.

## Resources managed

| Resource | Key | Description |
|---|---|---|
| `github_organization_settings` | `org` | Org-level settings |
| `github_membership` | `members[username]` | Org members |
| `github_team` | `teams[name]` | Teams |
| `github_team_members` | `teams[name]` | Team membership (members + maintainers) |
| `github_organization_role_team` | `teams[name:role]` | Team org role assignments |
| `github_repository` | `repos[name]` | Repositories |
| `github_team_repository` | `repos[repo:team]` | Team-repo access |
| `github_repository_environment` | `envs[repo:env]` | Deployment environments |
| `github_branch_protection` | `repos[name]` | Branch protection rules |

## Variables

| Name | Type | Description |
|---|---|---|
| `org` | `string` | GitHub organization name |
| `members` | `map(object({role, full_name}))` | Org members keyed by username |
| `teams` | `map(object({description, privacy, members, maintainers, org_roles?}))` | Teams keyed by slug |
| `repos` | `map(object({...}))` | Repositories with 20+ fields (see `variables.tf`) |
| `org_settings` | `object({...})` | Org settings: billing, permissions, dependabot flags (15+ fields) |

## Locals files

Root module `github/` splits configuration into three locals files:

| File | Content |
|---|---|
| `locals_org.tf` | `org`, `org_settings` |
| `locals_members.tf` | `members`, `teams` |
| `locals_repos.tf` | `repos` |

## Flattening pattern

The module flattens nested maps into composite keys for `for_each`:

| Local | Source | Key format |
|---|---|---|
| `team_org_roles` | teams x org_roles | `"team:role"` |
| `repo_team_access` | repos x team_access | `"repo:team"` |
| `repo_environments` | repos x environments | `"repo:env"` |

## Bot integration

**Status**: Fully integrated.

The Opsy bot reads and writes all three locals files via path constants in `bot/slack/internal/slack/handler.go`:

| Constant | Target file |
|---|---|
| `pathGitHubRepos` | `locals_repos.tf` |
| `pathGitHubMembers` | `locals_members.tf` |
| `pathGitHubOrg` | `locals_org.tf` |

Bot operations: add/delete/update repos, extract team names, read/update org settings, add/remove/change team members.

## Auth

The GitHub provider reads `GITHUB_TOKEN` automatically. No variable needed.

Fine-grained PAT permissions:

| Scope | Permission | Access |
|---|---|---|
| Organization | Administration | Read and write |
| Organization | Members | Read and write |
| Repository | Administration | Read and write |
| Repository | Environments | Read and write |
| Repository | Metadata | Read-only |

## Configuration examples

### Adding a member

```hcl
"username" = {
  role      = "member"
  full_name = "Full Name"
}
```

### Adding a team

```hcl
"Engineering" = {
  description = "Engineering team"
  privacy     = "closed"
  members     = ["user1", "user2"]
  maintainers = ["lead1"]
  org_roles   = {}
}
```

### Adding a repository

```hcl
"my-repo" = {
  description    = "My new repo"
  visibility     = "public"
  has_issues     = true
  default_branch = "main"
  team_access    = { "Maintainers" = "admin" }
  branch_protection = null
}
```

### Adding branch protection

```hcl
branch_protection = {
  required_reviews                = 1
  dismiss_stale_reviews           = true
  require_linear_history          = true
  require_conversation_resolution = true
  force_push_bypassers            = ["/username"]
}
```

### Adding an environment

```hcl
environments = {
  "production" = {
    deployment_branch_policy = {
      protected_branches     = true
      custom_branch_policies = false
    }
  }
}
```
