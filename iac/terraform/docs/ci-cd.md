# Terraform CI/CD

GitHub Actions workflows at `.github/workflows/` in the repo root.

## Workflows

| Workflow | Trigger paths | Provider secret |
|---|---|---|
| `github-apply.yml` | `iac/terraform/github/**`, `iac/terraform/modules/github/**` | `GH_PAT` |
| `cloudflare-apply.yml` | `iac/terraform/cloudflare/**`, `iac/terraform/modules/cloudflare/**` | `CLOUDFLARE_API_TOKEN` |
| `doppler-apply.yml` | `iac/terraform/doppler/**`, `iac/terraform/modules/doppler/**` | `DOPPLER_TOKEN` |

## Reusable workflow

All three call `terraform-reusable.yml` with inputs:

- `module-path` (string) -- path to root module
- `provider-token-name` (string) -- env var name for provider token

The reusable workflow:

1. Checks out code (`actions/checkout`, SHA-ratcheted)
2. Sets up Terraform (`hashicorp/setup-terraform`, ~> 1.5)
3. Writes GCP credentials to temp file from `GCP_SA_KEY` secret
4. Runs `terraform init` with `GOOGLE_APPLICATION_CREDENTIALS`
5. Runs `terraform apply -auto-approve` with provider token injected
6. Cleans up credentials (always runs)

## Trigger

Push to `main` affecting module-specific paths (see table).

## Secrets

Stored in the `production` environment on the `opsy` repo.

| Secret | Value |
|---|---|
| `GCP_SA_KEY` | Raw JSON contents of GCP service account key (GCS state backend) |
| `GH_PAT` | Fine-grained PAT with org admin permissions |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token with zone/DNS edit |
| `DOPPLER_TOKEN` | Doppler personal token |

## Security

- Secrets scoped to `production` environment only
- Environment restricted to protected branches (`main`)
- Branch protection requires PR with 1 approval before merge
- No direct pushes to `main`
- Action SHAs are ratchet-pinned in workflows

## SHA ratcheting

Action versions are pinned by SHA (not tag) with ratchet comments for auditability:

```yaml
- uses: actions/checkout@de0fac2e... # ratchet:actions/checkout@v6
```
