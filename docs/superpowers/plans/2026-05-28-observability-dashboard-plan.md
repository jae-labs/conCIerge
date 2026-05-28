# Implementation Plan: Observability Dashboard & Telemetry Enhancements

This plan describes the step-by-step implementation of the telemetry collector (Alloy) updates and Grafana dashboard query modifications to resolve existing errors and deploy comprehensive application/container monitoring.

---

## Todo List

- [ ] Task 1: Update Grafana Alloy configuration template (`config.alloy.j2` in Ansible) <!-- id: 1 -->
- [ ] Task 2: Fix LogQL query label mismatches and dynamic network interface device matching in `dashboard.json` <!-- id: 2 -->
- [ ] Task 3: Split Disk Throughput/IOPS and append new Docker Container and conCIerge Prometheus panels in `dashboard.json` <!-- id: 3 -->
- [ ] Task 4: Push the modified dashboard to Grafana Cloud via `gcx` and run validation checks <!-- id: 4 -->

---

## Task Details

### Task 1: Update Grafana Alloy configuration template (`config.alloy.j2` in Ansible)
* **Goal**: Update the Alloy agent config template to support the native Nginx exporter, enable systemd unit collection, and activate embedded container cAdvisor metrics.
* **Steps**:
  1. Modify `ansible/roles/grafana_alloy/templates/config.alloy.j2` using the new blocks outlined in the design spec.
  2. Enable the `systemd` collector inside the `prometheus.exporter.unix "host"` configuration.
  3. Validate the syntax of the rendered configuration via `ansible-lint`.
  4. Run repository validation checks via git hooks or standard `go test` to confirm codebase hygiene is unaffected.

---

### Task 2: Fix LogQL query label mismatches and dynamic network interface device matching in `dashboard.json`
* **Goal**: Correct existing Loki queries in `dashboard.json` and make the network device regex dynamic.
* **Steps**:
  1. Edit `/Users/luiz1361/.gemini/antigravity-cli/brain/c360ad4f-5e9e-40ec-9ecd-be458594effb/scratch/dashboard.json` (the locally saved copy of the dashboard).
  2. Replace all instances of `{service_name="nginx"}` with `{job="nginx"}` in Loki queries.
  3. Replace `{service_name="loki.source.journal.journald"}` with `{instance="$instance"}` in the journald warnings/errors panel query.
  4. Replace `{device="ens3"}` in the network traffic panel with `{device!~"lo|docker.*|tailscale.*|br-.*"}`.

---

### Task 3: Split Disk Throughput/IOPS and append new Docker Container and conCIerge Prometheus panels in `dashboard.json`
* **Goal**: Modify the JSON structure of `dashboard.json` to insert Disk IOPS, SSH security logs, and the collapsible rows for Docker Container Performance and conCIerge Application Metrics.
* **Steps**:
  1. Rename the existing "Disk I/O" panel to "Disk Throughput (Bytes/s)".
  2. Inject a new timeseries panel next to it for "Disk IOPS (Ops/s)" tracking reads/writes completed rates.
  3. Add the two new Loki-based panels in the `Logs` row for "SSH Successful Logins" and "SSH Failed Logins".
  4. Construct the collapsible rows and insert the PromQL queries for cAdvisor (Docker CPU, Memory, Network) and conCIerge custom Go telemetry (Slack events, workflows, GitHub operations, API latencies).

---

### Task 4: Push the modified dashboard to Grafana Cloud via `gcx` and run validation checks
* **Goal**: Apply the corrected dashboard configuration live to the Grafana stack.
* **Steps**:
  1. Run `gcx dashboards update --config <path>` or direct API execution to push the updated `dashboard.json` manifest.
  2. Run `lefthook run pre-commit` to ensure all staged files and repository conventions conform perfectly to the rules.
  3. Perform a final verification of the workspace status.
