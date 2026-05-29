# Terraform

IaC for managing the [jae-labs](https://github.com/jae-labs) GitHub organization, Cloudflare DNS, Doppler secrets, and OCI infrastructure.

## Architecture

```mermaid
flowchart TD
    subgraph Root Modules
        GH[github/]
        CF[cloudflare/]
        DP[doppler/]
        OCI[oci/]
    end
    subgraph Reusable Modules
        MGH[modules/github/]
        MCF[modules/cloudflare/]
        MDP[modules/doppler/]
    end
    subgraph State
        GCS[(GCS bucket: gh-jae-labs-terraform)]
    end
    GH --> MGH
    CF --> MCF
    DP --> MDP
    GH -.->|prefix: github/| GCS
    CF -.->|prefix: cloudflare/| GCS
    DP -.->|prefix: doppler/| GCS
    OCI -.->|prefix: oci/| GCS
```

Each root module has independent state stored in GCS with per-module prefix. No cross-module dependencies. The OCI root module is intentionally flat and self-contained; it does not use `modules/oci/`.

## Documentation

| Document | Description |
|---|---|
| [GitHub Module](docs/github-module.md) | Org members, teams, repos, branch protection, environments |
| [Cloudflare Module](docs/cloudflare-module.md) | DNS zones, records, account members |
| [Doppler Module](docs/doppler-module.md) | Projects, environments, groups, access grants |
| [OCI Module](docs/oci-module.md) | VCN, public subnet, security rules, and OCI compute instances |
| [CI/CD](docs/ci-cd.md) | GitHub Actions workflows, secrets, SHA ratcheting |
| [Bootstrap](docs/bootstrap.md) | One-time GCS backend setup |

## Prerequisites

- Terraform >= 1.5
- `GITHUB_TOKEN` env var (fine-grained PAT with org admin + repo admin + members read/write)
- `CLOUDFLARE_API_TOKEN` env var (API token with zone/DNS edit permissions)
- `DOPPLER_TOKEN` env var (personal token from Doppler account settings)
- OCI provider env vars: `OCI_TENANCY_OCID`, `OCI_USER_OCID`, `OCI_FINGERPRINT`, `OCI_REGION`, `OCI_PRIVATE_KEY_PATH`
- OCI stack vars: `TF_VAR_compartment_id`, `TF_VAR_availability_domain`, `TF_VAR_ssh_authorized_keys`, `TF_VAR_ssh_ingress_cidr` (`TF_VAR_availability_domain` must be the exact tenancy-prefixed OCI AD name)
- `GOOGLE_APPLICATION_CREDENTIALS` pointing to a GCP service account key for GCS backend

## Usage

```bash
# first time only
bash scripts/bootstrap.sh

# github
cd github
terraform init
terraform plan
terraform apply

# cloudflare
cd cloudflare
terraform init
terraform plan
terraform apply

# doppler
cd doppler
terraform init
terraform plan
terraform apply

# oci
cd oci
terraform init
terraform plan
terraform apply
```

## Adding a repo

The conCierge bot automates repo creation via Slack. For manual additions, edit `github/locals_repos.tf` directly under `repos`, then push to `main` — the GitHub Action applies automatically.

## CI/CD

Merges to `main` trigger path-filtered Terraform runs via GitHub Actions. GitHub, Cloudflare, and Doppler use the shared `terraform-reusable.yml`; OCI uses a dedicated `oci-apply.yml` because it needs multiple OCI auth and stack-input secrets. In GitHub Actions, the workflows write a temporary local tfplan and suppress Terraform plan/apply stdout and stderr; local `terraform plan` and `terraform apply` behavior is unchanged. Workflows live in `.github/workflows/` at the repo root and use a `production` environment with protected secrets.
