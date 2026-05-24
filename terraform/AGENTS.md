# Terraform IaC — jae-labs infrastructure

Root modules: `github/`, `cloudflare/`, `doppler/`, `oci/`. Reusable modules: `modules/github/`, `modules/cloudflare/`, `modules/doppler/`. OCI is a flat root module with no reusable submodule yet. State in GCS bucket `gh-jae-labs-terraform`; each root module has its own state prefix. Terraform >= 1.5. CI auto-applies on merge to main, path-filtered per module. Action SHAs are ratchet-pinned in workflows.

## Structure

- `github/` — org settings, members, teams, repos, branch protection, environments
- `cloudflare/` — DNS records, zone members
- `doppler/` — secrets management (projects, environments, groups)
- `oci/` — flat OCI root module (VCN, subnet, security rules, compute instance)
- `modules/github/` — reusable GitHub module (org settings, membership, teams, repos, branch protection, environments)
- `modules/cloudflare/` — reusable Cloudflare module (zones, DNS records, account members)
- `modules/doppler/` — reusable Doppler module (projects, environments, groups)
- `scripts/` — GCS backend bootstrap
- `docs/` — module docs (github, cloudflare, doppler), ci-cd, bootstrap

## Bot-Consumed Files

The conCierge bot reads and writes these files via the GitHub API:

| File | Bot Operation |
|---|---|
| `github/locals_repos.tf` | Add/delete/update repos |
| `github/locals_members.tf` | Extract team names for dropdowns |
| `github/locals_org.tf` | Read/update org settings |
| `cloudflare/locals_dns.tf` | Add/delete/update DNS records |

## Critical Constraints

- MUST NOT rename or restructure any bot-consumed locals file without updating path constants in `src/internal/slack/handler.go`
- MUST NOT change HCL key names or nesting in those files without updating the HCL editor at `src/internal/hcl/`
- These files are the contract between IaC and the bot; treat them as a public API

## Documentation

Module docs live in `docs/`:

| Document | Description |
|---|---|
| [GitHub Module](docs/github-module.md) | Org members, teams, repos, branch protection, environments |
| [Cloudflare Module](docs/cloudflare-module.md) | DNS zones, records, account members |
| [Doppler Module](docs/doppler-module.md) | Projects, environments, groups, access grants |
| [OCI Module](docs/oci-module.md) | VCN, subnet, security rules, compute instance |
| [CI/CD](docs/ci-cd.md) | GitHub Actions workflows, secrets, SHA ratcheting |
| [Bootstrap](docs/bootstrap.md) | One-time GCS backend setup |

### Documentation maintenance

Documentation MUST be updated in the same PR as the code change.

| Change type | Update required |
|---|---|
| New/modified variable | Module doc in `docs/{module}-module.md` variables table |
| New/modified resource | Module doc resources table |
| New/modified locals file | Module doc locals files table |
| New bot integration | Module doc bot integration section, `src/AGENTS.md`, root `AGENTS.md` |
| New CI workflow or secret | `docs/ci-cd.md` |

Module docs follow the format in `docs/github-module.md`: title, resources, variables, locals, flattening, bot integration, auth, examples.
