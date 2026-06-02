# Ansible OCI Host Configuration

Manual-first Ansible setup for post-provision configuration of the OCI instance managed by `terraform/oci`.

## What it configures

- OCI dynamic inventory via the Oracle OCI Ansible collection
- host-level iptables rules
- SSH daemon hardening via a managed `sshd_config.d` drop-in
- fail2ban jails for SSH and repeated access to non-allowed HTTPS paths (anything outside `/slack/events` and `/n8n*`)
- managed 2 GiB swapfile on the existing OCI root disk
- swap tuning via `vm.swappiness` (default `10`) to prefer physical RAM use before swap on low-memory hosts
- Docker CE (including docker-compose-plugin) and group configuration
- nginx, including repo-managed logrotate policy for `/var/log/nginx/*.log` and localhost `/metrics` status endpoint for scraping
- certbot in webroot mode with a systemd renewal timer
- concierge bot build, HTTP-mode systemd deployment, and nginx reverse proxying for `/slack/events`
- Grafana Alloy, acting as a local loopback-bound collector that forwards host/system metrics, application metrics, nginx status, journald/nginx logs, and OTLP traces to Grafana Cloud

The current default certificate email is `luiz@justanother.engineer`, matching the public `abox` example layout. The nginx and certbot roles are intentionally modeled after the working `abox` nginx and `nginx_certbot` behavior.

Out of scope:

- containerctl
- partitioning

## Layout

```text
ansible/
  ansible.cfg
  inventory/oci.oci.yml
  inventory/group_vars/all.yml
  playbooks/site.yml
  roles/
  requirements.yml
```

## Prerequisites

- Ansible installed locally
- OCI Python SDK installed locally
- OCI credentials configured via the normal OCI config/environment flow
- optional `TF_VAR_ssh_ingress_cidr` if you want the host firewall SSH allowlist to match Terraform; when unset, the Ansible host firewall defaults SSH ingress to `0.0.0.0/0`
- DNS for `oci.justanother.engineer` pointing at the OCI instance before certbot runs
- SSH access to the instance with the matching private key already available locally
- a trusted SSH host key for `oci.justanother.engineer`; Ansible connects to the OCI public IP but reuses the FQDN as the host-key alias

## Install collections

```sh
cd ansible
bash bootstrap.sh
```

That script:

- detects the Python interpreter used by `ansible-playbook`
- reuses `pip` from that interpreter when already available, otherwise bootstraps it if needed
- installs `requirements.txt` into the same interpreter
- installs the Ansible collection from `requirements.yml`

If you want to run the steps manually, use the interpreter behind `ansible-playbook`, not a generic `python3`:

```sh
cd ansible
ANSIBLE_PYTHON="$(head -n 1 "$(command -v ansible-playbook)" | sed 's/^#!//')"
if ! "$ANSIBLE_PYTHON" -m pip --version >/dev/null 2>&1; then "$ANSIBLE_PYTHON" -m ensurepip --upgrade; fi
"$ANSIBLE_PYTHON" -m pip install -r requirements.txt
ansible-galaxy collection install -r requirements.yml
```

## Inspect inventory

```sh
cd ansible
ansible-inventory -i inventory/oci.oci.yml --list
```

## Run the playbook

```sh
cd ansible
ansible-playbook -i inventory/oci.oci.yml playbooks/site.yml
```

## Tag Filtering

Every role in the playbook is tagged with its name, as well as a high-level category tag. This allows you to run only specific parts of the configuration to save time.

### Available Tags & Categories

| Role / Task | Role Tag | Category Tag | Description |
|---|---|---|---|
| `common` | `common` | `baseline` | Core OS updates, packages, and setup |
| `oci_iptables` | `oci_iptables`, `firewall` | `baseline` | Host-level iptables rules |
| `ssh_hardening` | `ssh_hardening`, `security` | `baseline` | Hardened SSH configuration |
| `fail2ban` | `fail2ban`, `security` | `baseline` | Fail2ban service and jails |
| `tailscale` | `tailscale` | `baseline` | Tailscale VPN client configuration |
| `swapfile` | `swapfile` | `baseline` | Virtual memory swap file setup |
| `docker` | `docker` | `baseline` | Docker CE engine and group setup |
| `nginx` | `nginx` | `web` | Nginx reverse proxy configuration |
| `n8n` | `n8n` | `web` | Deploy and run n8n service |
| `certbot` | `certbot`, `security` | `web` | Certbot certificate provisioning |
| `concierge` | `concierge`, `deploy` | `deploy` | Deploy/upgrade concierge bot |
| `grafana_alloy` | `grafana_alloy` | `monitoring` | Deploy Grafana Alloy telemetry collector |

### Local Tag Filtering Examples

Run only the Slack bot deployment:
```sh
ansible-playbook -i inventory/oci.oci.yml playbooks/site.yml --tags concierge
```

Run only core baseline security configuration:
```sh
ansible-playbook -i inventory/oci.oci.yml playbooks/site.yml --tags baseline
```

Exclude Grafana Alloy monitoring configuration:
```sh
ansible-playbook -i inventory/oci.oci.yml playbooks/site.yml --skip-tags monitoring
```

## Running Ad-hoc via GitHub Actions

Infrequent host and service configuration (like OS updates, nginx setup, and monitoring) should be run ad-hoc via the manual **Ansible Ad-hoc Configuration** workflow in GitHub Actions.

1. Go to the **Actions** tab in GitHub.
2. Select the **Ansible Ad-hoc Configuration** workflow.
3. Click **Run workflow**, choose your branch, select a configuration category (e.g., `baseline`, `monitoring`), and optionally configure custom tags or skip-tags.
4. Click **Run workflow** to execute.

## Deploying the concierge bot

The `concierge` role deploys the conCIerge bot in a Docker container using Docker Compose and manages it with a systemd service unit. It pulls the image specified by `concierge_image` and `concierge_image_tag` (which default to `ghcr.io/jae-labs/concierge:latest`), renders `/etc/concierge/concierge.env` from Ansible variables, and runs the service in `network_mode: host` to share the host's networking.

Provide bot runtime configuration through the `concierge_env` Ansible variable. On the OCI host, inventory defaults set `SLACK_MODE=http` and `SLACK_HTTP_LISTEN_ADDR=127.0.0.1:8080`; nginx proxies only `/slack/events` to that loopback listener and returns `404` for other application paths. Example:

```yaml
concierge_env:
  SLACK_BOT_TOKEN: xoxb-...
  SLACK_SIGNING_SECRET: ...
  SLACK_REQUESTS_CHANNEL_ID: C12345678
  SLACK_USER_IDS: U111,U222
  GITHUB_APP_ID: "12345"
  GITHUB_APP_INSTALLATION_ID: "67890"
  GITHUB_APP_PRIVATE_KEY: "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----"
  GITHUB_OWNER: jae-labs
  GITHUB_REPO: conCIerge
```

If you need to run the deployed service in Socket Mode instead, override `SLACK_MODE=socket` in `concierge_env` and provide `SLACK_APP_TOKEN`.

When passing `GITHUB_APP_PRIVATE_KEY` through the generated env file, use `\n` escapes between PEM lines. The Go config loader normalizes those escapes back into real newlines at runtime.

## Check mode

```sh
cd ansible
ansible-playbook -i inventory/oci.oci.yml playbooks/site.yml --check
```

## Observability & Grafana Alloy

Grafana Alloy is deployed as a local telemetry collector on the OCI host. All scrape and collection endpoints are securely bound only to localhost:

* **Traces**: Recieves gRPC and HTTP OTLP traces from the concierge process at `127.0.0.1:4317` / `127.0.0.1:4318`.
* **Concierge Metrics**: Scrapes concierge application metrics on `127.0.0.1:9090/metrics`.
* **Nginx Metrics**: Scrapes web server active connections on `127.0.0.1:9113` via `prometheus-nginx-exporter` (which in turn scrapes Nginx's status page on `127.0.0.1:8081/metrics`).
* **Alloy Metrics**: Scrapes Alloy runtime and collection metrics on `127.0.0.1:12345/metrics`.
* **System Logs**: Tail logs from `journald` (filtered for service names and units).
* **Nginx Logs**: Collects nginx access and error logs from `/var/log/nginx/*.log`. Access logs use JSON and include `request_time`, `upstream_response_time`, `upstream_status`, and `upstream_addr` for latency and upstream error SLIs.

### Credentials Injection

Grafana Cloud write endpoints and tokens are injected dynamically from your local environment during deployment (e.g. from Doppler or environment variables). Ensure the following environment variables are present in your shell when running `ansible-playbook`:

* `GRAFANA_CLOUD_PROMETHEUS_URL` & `GRAFANA_CLOUD_PROMETHEUS_USERNAME` & `GRAFANA_CLOUD_PROMETHEUS_TOKEN`
* `GRAFANA_CLOUD_LOKI_URL` & `GRAFANA_CLOUD_LOKI_USERNAME` & `GRAFANA_CLOUD_LOKI_TOKEN`
* `GRAFANA_CLOUD_TEMPO_URL` & `GRAFANA_CLOUD_TEMPO_USERNAME` & `GRAFANA_CLOUD_TEMPO_TOKEN`
* `TAILSCALE_AUTH_KEY`
* optional `SENTRY_DSN`, `SENTRY_ENVIRONMENT`, and `SENTRY_RELEASE`
