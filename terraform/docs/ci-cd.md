# Repository CI/CD

GitHub Actions workflows at `.github/workflows/` in the repo root.

## Workflows

| Workflow | Trigger paths | Required secret |
|---|---|---|
| `ci.yml` | `src/**`, `terraform/**`, `ansible/**`, `.github/workflows/ci.yml`, `.github/workflows/release.yml` | None |
| `release.yml` | `src/**`, `.github/workflows/ci.yml`, `.github/workflows/release.yml` (push to `main` only) | `GITHUB_TOKEN` plus bot deploy and OCI/SSH secrets in the `production` environment |
| `github-apply.yml` | `terraform/github/**`, `terraform/modules/github/**` | `GH_PAT` |
| `cloudflare-apply.yml` | `terraform/cloudflare/**`, `terraform/modules/cloudflare/**` | `CLOUDFLARE_API_TOKEN` |
| `doppler-apply.yml` | `terraform/doppler/**`, `terraform/modules/doppler/**` | `DOPPLER_TOKEN` |
| `oci-apply.yml` | `terraform/oci/**` | OCI auth and stack secrets |
| `ansible-adhoc.yml` | Manual (`workflow_dispatch`) | OCI auth and stack secrets in the `production` environment |


## Bot workflows

### `ci.yml`

The bot CI workflow mirrors the structure used in `jae-labs/flashcards` while targeting the nested Go module in `src/`.

1. Checks out code (`actions/checkout`, SHA-ratcheted)
2. Sets up Go from `src/go.mod`
3. Runs `golangci-lint`
4. Runs `go test -v -race -coverprofile=coverage.out ./...`
5. Uploads coverage to Codecov on a best-effort basis
6. Builds `cmd/concierge` across the Linux/macOS matrix used by the reference repo
7. Runs `gosec` and `trivy` against the bot subtree

### `release.yml`

The bot release workflow also mirrors `flashcards`, with monorepo path adjustments and no Homebrew publishing step.

1. Builds `cmd/concierge` artifacts for Linux and macOS
2. Packages tarballs and raw binaries
3. Uploads build artifacts between jobs
4. Computes the next patch release tag from the latest `v*.*.*` tag on `main`
5. Creates a versioned GitHub release plus a refreshed `latest` release
6. Downloads the Linux amd64 release artifact and deploys it to the OCI host through Ansible in the `production` environment

The release-creation steps use the default `GITHUB_TOKEN`. The deploy job additionally needs OCI auth, SSH access, and concierge runtime secrets from the `production` environment.

### `ansible-adhoc.yml`

The manual Ansible ad-hoc workflow allows operators to run specific tags or categories of configuration on the OCI instance via `workflow_dispatch`.

1. Prompts for tag `category` (`all`, `baseline`, `web`, `monitoring`, `concierge`, `custom`).
2. Accepts optional `custom_tags` and `custom_skip_tags`.
3. Sets up OCI auth and SSH access to the production host.
4. Prepares dynamic Ansible deployment variables.
5. Runs Ansible syntax check followed by playbook execution with the requested tags and optional dry-run check mode.


## Reusable workflow

GitHub, Cloudflare, and Doppler call `terraform-reusable.yml` with inputs:

- `module-path` (string) -- path to root module
- `provider-token-name` (string) -- env var name for provider token

The reusable workflow:

1. Checks out code (`actions/checkout`, SHA-ratcheted)
2. Sets up Terraform (`hashicorp/setup-terraform`, ~> 1.5)
3. Writes GCP credentials to temp file from `GCP_SA_KEY` secret
4. Runs `terraform init` with `GOOGLE_APPLICATION_CREDENTIALS`
5. Runs `terraform plan` into a temporary local tfplan with stdout/stderr suppressed in GitHub Actions
6. Runs `terraform apply` from that temporary tfplan with stdout/stderr suppressed in GitHub Actions
7. Deletes the temporary tfplan, serializes applies per root module with a GitHub Actions concurrency group keyed by repository and `module-path`, and cleans up credentials (always runs)

## Dedicated OCI workflow

`oci-apply.yml` is separate because OCI needs multiple provider environment variables plus stack inputs.

The OCI workflow:

1. Checks out code (`actions/checkout`, SHA-ratcheted)
2. Sets up Terraform (`hashicorp/setup-terraform`, ~> 1.5)
3. Writes GCP backend credentials to a temp file from `GCP_SA_KEY`
4. Writes the OCI API private key to a temp PEM file from `OCI_PRIVATE_KEY`
5. Exports OCI provider env vars and `TF_VAR_*` stack inputs
6. Runs `terraform init`
7. Runs `terraform plan` into a temporary local tfplan with stdout/stderr suppressed in GitHub Actions
8. Runs `terraform apply` from that temporary tfplan with stdout/stderr suppressed in GitHub Actions
9. Deletes the temporary tfplan, cleans up temporary credential files, and serializes applies for `terraform/oci`

## Verification workflows

### Unified `ci.yml`

The main CI workflow (`ci.yml`) runs on all pull requests and pushes to `main` affecting `src/**`, `terraform/**`, or `ansible/**`. It verifies and validates all three codebases in parallel:

#### Go Bot Checks
1. **Lint Check**: Runs `golangci-lint` on the Go code inside `src/`.
2. **Unit Tests**: Runs the full suite of Go tests with race-detection enabled.
3. **Build matrix**: Compiles `cmd/concierge` for Linux and macOS.
4. **Security scan**: Runs `gosec` and `trivy` scans on the Go codebase.

#### Terraform Checks
1. **Format Check**: Runs `terraform fmt -check -recursive terraform` to verify styling.
2. **Lint Check**: Installs TFLint with `terraform-linters/setup-tflint`, then runs `tflint` recursively using a unified configuration (`terraform/.tflint.hcl`) which suppresses subjective warnings (like `terraform_unused_declarations`).
3. **Offline Validation**: Run a matrix job across all root modules (`github/`, `cloudflare/`, `doppler/`, `oci/`) which:
   - Sets up a dummy GCP service account key structure in `/tmp/dummy-gcp-key.json` to satisfy the backend library requirements.
   - Runs `terraform init -backend=false`.
   - Runs `terraform validate`.

#### Ansible Checks
1. **Python Setup**: Initializes Python with `actions/setup-python`.
2. **Install Ansible and Linters**: Installs `ansible-core`, `ansible-lint`, and the necessary dependencies (`oci` Python library).
3. **Install Ansible Collections**: Installs `oracle.oci` and `community.crypto` from `ansible/requirements.yml` to ensure module resolution works correctly during linting.
4. **Lint Check**: Runs `ansible-lint` using `.ansible-lint` at the repository root with `ANSIBLE_ROLES_PATH=ansible/roles` configured to check all syntax and structure.

## Trigger

Bot CI runs on path-scoped pushes to `main` and pull requests. Bot releases run on path-scoped pushes to `main`. Terraform applies run on pushes to `main` affecting module-specific paths (see table).

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
| `OCI_PRIVATE_KEY_PASSPHRASE` | Optional passphrase for `OCI_PRIVATE_KEY` when the OCI API signing key is encrypted |
| `OCI_SSH_PRIVATE_KEY_B64` | Single-line base64-encoded private SSH key used by the bot deploy workflow to connect to `ubuntu@oci.justanother.engineer` |
| `SLACK_BOT_TOKEN` | Slack bot token rendered into `/etc/concierge/concierge.env` during CI deploy |
| `SLACK_SIGNING_SECRET` | Slack signing secret rendered into `/etc/concierge/concierge.env` during CI deploy |
| `SLACK_REQUESTS_CHANNEL_ID` | Slack channel ID for concierge request summaries |
| `SLACK_USER_IDS` | Comma-separated Slack user IDs allowed to use the bot |
| `SLACK_MANAGER_IDS` | Comma-separated Slack manager IDs allowed to approve requests |
| `SLACK_ADMIN_IDS` | Comma-separated Slack admin IDs allowed to approve requests |
| `CONCIERGE_GH_APP_ID` | GitHub App ID rendered into the concierge service env file |
| `CONCIERGE_GH_APP_INSTALLATION_ID` | GitHub App installation ID rendered into the concierge service env file |
| `CONCIERGE_GH_APP_PRIVATE_KEY` | GitHub App private key rendered into the concierge service env file |
| `CONCIERGE_GH_OWNER` | GitHub owner/org rendered into the concierge service env file |
| `CONCIERGE_GH_REPO` | GitHub repo rendered into the concierge service env file |
| `TAILSCALE_AUTH_KEY` | Tailscale auth key used by Ansible to register the OCI host as an exit node with Tailscale SSH |
| `SENTRY_DSN` | Optional Sentry DSN rendered into the concierge service env file |
| `SENTRY_ENVIRONMENT` | Optional Sentry environment override |

Encode the deploy SSH private key before saving it as `OCI_SSH_PRIVATE_KEY_B64`, for example with `base64 < ~/.ssh/your_deploy_key | tr -d '\n'`.

## External repository synced secrets

Doppler also syncs project `pages` configs `prd` and `rev` into the `pages` repository `production` and `review` GitHub environments. The `pages` CI workflow consumes these secrets and writes runtime-only values into Cloudflare Pages before deploy.

| Repository | Environment | Secret | Purpose |
|---|---|---|---|
| `pages` | `production`, `review` | `NVIDIA_NIM_TOKEN` | NVIDIA NIM token used by the lui.z clone chat endpoint; CI writes it to Cloudflare Pages runtime secrets |

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
