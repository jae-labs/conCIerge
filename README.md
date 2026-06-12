<p align="center">
<img src="assets/logo.png" alt="conCierge Logo" width="120"/>
</p>

<p align="center">
<a href="https://codecov.io/gh/jae-labs/concierge"><img src="https://codecov.io/gh/jae-labs/concierge/branch/main/graph/badge.svg" alt="codecov"></a>
<a href="https://github.com/jae-labs/concierge/actions/workflows/ci.yml"><img src="https://github.com/jae-labs/concierge/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
<a href="https://github.com/jae-labs/concierge/actions/workflows/release.yml"><img src="https://github.com/jae-labs/concierge/actions/workflows/release.yml/badge.svg" alt="Release"></a>
<a href="https://goreportcard.com/report/github.com/jae-labs/concierge"><img src="https://goreportcard.com/badge/github.com/jae-labs/concierge" alt="Go Report Card"></a>
<a href="LICENSE"><img src="https://img.shields.io/github/license/jae-labs/concierge" alt="License"></a>
<a href="https://github.com/jae-labs/concierge/releases"><img src="https://img.shields.io/github/v/release/jae-labs/concierge" alt="Release"></a>
<a href="go.mod"><img src="https://img.shields.io/github/go-mod/go-version/jae-labs/concierge" alt="Go Version"></a>
<a href="https://buymeacoffee.com/luiz1361"><img src="https://img.shields.io/badge/Buy%20Me%20A%20Coffee-donate-orange.svg?logo=buymeacoffee" alt="Buy Me A Coffee"></a>
</p>

Slack-native GitOps for GitHub, Cloudflare, and Doppler.

`conCIerge` turns structured Slack requests into reviewed Terraform pull requests. It reads live Terraform locals plus a YAML schema from the external [`jae-labs/terraform`](https://github.com/jae-labs/terraform) repo, drives a dynamic Slack modal flow, edits HCL, opens a PR, and posts the request summary back to `#concierge`. It never mutates infra directly; the apply boundary stays in the Terraform repo's normal review + CI/CD pipeline.

## Highlights

- Schema-driven flow: categories, resources, actions, modal fields all live in `concierge-schema.yaml`.
- Generic HCL engine: one editor handles `map_entry`, `singleton`, and `membership` resources.
- Nonce-keyed thread state rejects stale modal submissions.
- GitHub App auth, no long-lived personal tokens.
- `slog` + OpenTelemetry + Sentry; logs/traces flow through Grafana Alloy in production.

## Quick start

Prerequisites: Go 1.25+, Slack app credentials, GitHub App credentials with access to the Terraform repo. Doppler is optional for secret injection.

Required environment:

| Var | Purpose |
|---|---|
| `SLACK_BOT_TOKEN` | Bot OAuth token |
| `SLACK_REQUESTS_CHANNEL_ID` | Where request summaries are posted |
| `SLACK_APP_TOKEN` *or* `SLACK_SIGNING_SECRET` | Socket Mode app token / HTTP signing secret |
| `GITHUB_APP_ID`, `GITHUB_APP_INSTALLATION_ID`, `GITHUB_APP_PRIVATE_KEY` | GitHub App credentials |
| `GITHUB_OWNER`, `GITHUB_REPO` | Target Terraform repo (not this repo) |

Optional:

| Var | Default |
|---|---|
| `SLACK_MODE` | `socket` (also `http`) |
| `SLACK_USER_IDS` | comma-separated allow-list |
| `GITHUB_COMMIT_AUTHOR_NAME` | `conCierge Bot` |
| `GITHUB_COMMIT_AUTHOR_EMAIL` | `239121271+luiz1361@users.noreply.github.com` |

Local:

```sh
doppler run -- go run ./cmd/concierge   # or: go run ./cmd/concierge
air                                      # live reload
```

Production runs the same binary with `SLACK_MODE=http` behind nginx and exposes `GET /healthz`.

## What it manages

| Domain | Resource | Actions |
|---|---|---|
| GitHub | Repository | Add, Remove, Update |
| GitHub | Org Settings | Update |
| GitHub | Team Membership | Add, Remove, Change Role |
| Cloudflare | DNS Records | Add, Remove, Update |
| Doppler | Projects | Add, Remove, Update |

The exact list is whatever lives in `concierge-schema.yaml` at runtime, not in code.

## Observability

App code emits structured logs via `slog` and traces/metrics via OpenTelemetry. In production, Grafana Alloy tails the Nomad allocation log files into Loki, collects traces and can fan them out to Tempo and optionally Sentry, and metrics are exposed via Prometheus scrape + optional OTLP export. Exceptions and errors are sent to Sentry via Alloy logs forwarding rather than direct Sentry SDK exceptions capture.

Relevant env vars (all optional):

- `OTEL_SERVICE_NAME`, `OTEL_ENVIRONMENT`, `SERVICE_VERSION`
- `OTEL_EXPORTER_OTLP_ENDPOINT` (default `127.0.0.1:4317`; also accepts full URLs such as `http://127.0.0.1:4317`), `OTEL_EXPORTER_OTLP_PROTOCOL` (`grpc`)
- `OTEL_EXPORTER_OTLP_METRICS_ENDPOINT` (falls back to traces endpoint), `OTEL_EXPORTER_OTLP_METRICS_PROTOCOL`
- `METRICS_ENABLED`, `METRICS_LISTEN_ADDR` (loopback-only)
- `SENTRY_DSN`, `SENTRY_ENVIRONMENT`, `SENTRY_RELEASE`
- `CONCIERGE_TRACE_SAMPLE_PERCENTAGE` (default `100`)
- `CONCIERGE_TRACE_SLOW_THRESHOLD_MS` (default `1000`)

## CI and releases

| Workflow | Trigger | Behavior |
|---|---|---|
| `ci.yml` | push to `main`, PRs | format, lint, test+coverage, multi-arch build, security scan |
| `release.yml` | push to `main` | release artifacts, GHCR image, deploy to OCI via `jae-labs/ansible`, cap CI-side Nomad wait time |

Release assets cover `linux/{amd64,arm64}` and `darwin/{amd64,arm64}`.
Production observability settings used by Ansible are injected from `release.yml` runner secrets at deploy time; if Honeycomb, Grafana Profiles, or Sentry OTLP secrets are omitted there, the rendered Alloy config excludes those pipelines.

## Related repositories

| Repo | Purpose |
|---|---|
| [`jae-labs/terraform`](https://github.com/jae-labs/terraform) | Schema + Terraform source of truth |
| [`jae-labs/ansible`](https://github.com/jae-labs/ansible) | OCI host config + production deploy |

## Documentation

| Doc | What |
|---|---|
| [Architecture](docs/architecture.md) | Runtime design, packages, request lifecycle |
| [Adding a Resource Type](docs/adding-a-resource-type.md) | Schema-first checklist |
| [Validation Patterns](docs/validation-patterns.md) | Where/how input is validated |
| [Modals and Blocks](docs/modals-and-blocks.md) | Block Kit and callback ID conventions |

## Test

```sh
go test ./...
```

## Contributing

Read `AGENTS.md` and the docs in `docs/` before changing flow behavior or Terraform file paths. This bot has a hard contract with `jae-labs/terraform`; trimming README text does not relax it.

## License

See [LICENSE](LICENSE).
