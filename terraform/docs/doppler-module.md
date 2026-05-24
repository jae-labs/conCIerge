# Doppler Module

Manages secrets infrastructure: projects, environments, groups, and access grants.

## Resources managed

| Resource | Key | Description |
|---|---|---|
| `doppler_project` | `projects[name]` | Doppler projects |
| `doppler_environment` | `envs[project:env_slug]` | Project environments |
| `doppler_group` | `groups[name]` | Access groups |
| `doppler_project_member_group` | `access[project:group]` | Group-to-project access grants |

## Variables

Defined in `modules/doppler/variables.tf`:

| Variable | Type | Description |
|---|---|---|
| `projects` | `map(object({description, environments}))` | Projects with nested environments. Environment key is the slug, `name` is display value. `personal_configs` defaults to `false`. |
| `groups` | `map(object({description}))` | Doppler groups |
| `project_access` | `list(object({project, group, role, environments?}))` | Group-to-project access grants. Validation rejects duplicate `project:group` pairs. |

## Configuration

All metadata lives in split locals files under `doppler/`:

| File | Content |
|---|---|
| `locals_projects.tf` | `projects` (with nested environments) |
| `locals_access.tf` | `groups`, `project_access` |

## Flattening

Two flattening locals in the module (`modules/doppler/main.tf`):

| Local | Pattern | Key format |
|---|---|---|
| `project_environments` | `projects x environments` | `"project:env_slug"` |
| `project_access_map` | list-to-map conversion | `"project:group"` |

`project_access_map` duplicates fail fast via variable-level validation.

## Configuration examples

### Adding a project with environments

```hcl
"my-service" = {
  description = "Backend API service"
  environments = {
    "dev" = {
      name             = "Development"
      personal_configs = true
    }
    "stg" = {
      name = "Staging"
    }
    "prd" = {
      name = "Production"
    }
  }
}
```

### Adding a group

```hcl
"backend-team" = {
  description = "Backend engineering team"
}
```

### Adding a project access grant

```hcl
project_access = [
  {
    project = "my-service"
    group   = "backend-team"
    role    = "admin"
  },
  {
    project      = "my-service"
    group        = "frontend-team"
    role         = "viewer"
    environments = ["dev", "stg"]
  },
]
```

## Bot integration

**Status: Not yet implemented.**

When implemented, the bot will need:
- Path constants for `locals_projects.tf` and `locals_access.tf`
- HCL editors for project/environment/group/access CRUD
- Block Kit modals and validation

See `src/docs/adding-a-resource-type.md` for the implementation guide.

## Auth

The Doppler provider reads `DOPPLER_TOKEN` automatically. No variable needed.

Required: a personal token from Doppler account settings with access to managed projects.
