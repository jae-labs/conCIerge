# Terraform CI/CD

GitHub Actions workflows at `.github/workflows/` in the repo root.

## Workflows

| Workflow | Trigger paths | Provider secret |
|---|---|---|
| `github-apply.yml` | `iac/terraform/github/**`, `iac/terraform/modules/github/**` | `GH_PAT` |
| `cloudflare-apply.yml` | `iac/terraform/cloudflare/**`, `iac/terraform/modules/cloudflare/**` | `CLOUDFLARE_API_TOKEN` |
| `doppler-apply.yml` | `iac/terraform/doppler/**`, `iac/terraform/modules/doppler/**` | `DOPPLER_TOKEN` |
| `oci-apply.yml` | `iac/terraform/oci/**` | OCI auth and stack secrets |

## Reusable workflow

GitHub, Cloudflare, and Doppler call `terraform-reusable.yml` with inputs:

- `module-path` (string) -- path to root module
- `provider-token-name` (string) -- env var name for provider token

The reusable workflow:

1. Checks out code (`actions/checkout`, SHA-ratcheted)
2. Sets up Terraform (`hashicorp/setup-terraform`, ~> 1.5)
3. Writes GCP credentials to temp file from `GCP_SA_KEY` secret
4. Runs `terraform init` with `GOOGLE_APPLICATION_CREDENTIALS`
5. Serializes applies per root module with a GitHub Actions concurrency group keyed by repository and `module-path`
6. Runs `terraform apply -auto-approve` with provider token injected
7. Cleans up credentials (always runs)

## Dedicated OCI workflow

`oci-apply.yml` is separate because OCI needs multiple provider environment variables plus stack inputs.

The OCI workflow:

1. Checks out code (`actions/checkout`, SHA-ratcheted)
2. Sets up Terraform (`hashicorp/setup-terraform`, ~> 1.5)
3. Writes GCP backend credentials to a temp file from `GCP_SA_KEY`
4. Writes the OCI API private key to a temp PEM file from `OCI_PRIVATE_KEY`
5. Exports OCI provider env vars and `TF_VAR_*` stack inputs
6. Runs `terraform init`
7. Runs `terraform apply -auto-approve`
8. Cleans up temporary credential files
9. Serializes applies for `iac/terraform/oci`

## Trigger

Push to `main` affecting module-specific paths (see table).

## Secrets

Stored in the `production` environment on the `conCIerge` repo.

| Secret | Value |
|---|---|
| `GCP_SA_KEY` | Raw JSON contents of GCP service account key (GCS state backend) |
| `GH_PAT` | Fine-grained PAT with org admin permissions |
| `CLOUDFLARE_API_TOKEN` | Cloudflare API token with zone/DNS edit |
| `DOPPLER_TOKEN` | Doppler personal token |
| `OCI_TENANCY_OCID` | OCI tenancy OCID used by the provider |
| `OCI_USER_OCID` | OCI user OCID used by the provider |
| `OCI_FINGERPRINT` | Fingerprint for the OCI API signing key |
| `OCI_REGION` | OCI region for provider operations |
| `OCI_PRIVATE_KEY` | PEM contents of the OCI API signing key |
| `OCI_COMPARTMENT_OCID` | Compartment OCID for the OCI stack |
| `OCI_AVAILABILITY_DOMAIN` | Exact tenancy-prefixed OCI availability-domain name for the compute instance (for example `tjxx:eu-amsterdam-1-AD-1`) |
| `OCI_SSH_AUTHORIZED_KEYS` | SSH authorized keys content injected into the instance |
| `OCI_SSH_INGRESS_CIDR` | CIDR allowed to reach the instance over SSH |

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
