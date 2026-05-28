# Design Spec: conCIerge Observability Dashboard & Telemetry Enhancements

This design document outlines the end-to-end fixes and enhancements for monitoring the conCIerge OCI deployment, focusing on resolving silent failures, correcting metric mismatches, and adding high-precision application and container telemetry.

## 1. Problem Statements & Telemetry Gaps

1. **Broken Nginx Log Panels**: LogQL queries for `Nginx Request Rate by Status` (panel-69), `Nginx 4xx/5xx Errors` (panel-70), and `Nginx Access Logs` (panel-71) query `{service_name="nginx"}` in Loki. However, the Alloy logs relabeling block maps Nginx logs to `job="nginx"`. This mismatch results in "No Data" on the dashboard.
2. **Broken Journald Logs Panel**: The `Journal Logs — Warnings & Errors` panel queries `{service_name="loki.source.journal.journald"}` which does not exist, leaving the panel completely blank.
3. **Broken Nginx Scraper in Alloy**: The Nginx scrapers in `config.alloy.j2` pull metrics from port `8081` via standard `prometheus.scrape "nginx"`. Since `stub_status` exposes plain text rather than Prometheus exposition format, Alloy fails to parse the page, leading to silent scraper failures.
4. **Missing conCIerge Prometheus Telemetry**: Custom Go application metrics exposed on port `9090` (Slack events, Slack Web API operations/latency, GitHub API operations/latency, workflows, PR metrics) are completely omitted from the dashboard.
5. **CPU Usage 100% NaN Bug**: The CPU Usage Stat panel uses `$__rate_interval` with a direct division that evaluates to `NaN` when the scraping interval is too close to the query window, resulting in an incorrect `100%` display.
6. **No Individual Container Monitoring**: Docker containers (like `concierge`, `n8n`) are not monitored individually, only the host system-level resources.
7. **SSH Login Attempts Untracked**: The system journal contains failed and successful SSH auth attempts, but they are not tracked or visualized on the dashboard.
8. **Ambiguous Disk IO / Missing IOPS**: The "Disk I/O" panel tracks read/write throughput in MB/s but is named "Disk I/O" and lacks operational rate (IOPS) metrics.
9. **Hardcoded Network Interface**: The network traffic panel is hardcoded to `device="ens3"`, which breaks if the interface name changes.

---

## 2. Proposed Changes

### A. Telemetry Collection Configuration (Alloy)
Update `ansible/roles/grafana_alloy/templates/config.alloy.j2` to:
1. Use Alloy's native Nginx exporter to translate `stub_status` plain text output into Prometheus metrics.
2. Explicitly enable the `systemd` collector in the Unix exporter to track system service health.
3. Enable the native embedded `cadvisor` exporter inside Alloy to scrape individual Docker container metrics directly from the Docker socket.

```alloy
// 1. Host (Unix) system metrics with systemd collector enabled
prometheus.exporter.unix "host" {
  enabled_collectors = ["cpu", "meminfo", "diskstats", "netdev", "systemd", "filesystem", "loadavg", "tcpstat"]
}

// 3. Nginx Status metrics
prometheus.exporter.nginx "nginx" {
  address = "http://127.0.0.1:8081/metrics"
}

prometheus.scrape "nginx" {
  targets    = prometheus.exporter.nginx.nginx.targets
  forward_to = [prometheus.remote_write.grafana_cloud_metrics.receiver]
}

// 5. Individual Docker Container metrics
prometheus.exporter.cadvisor "containers" {
  docker_only = true
}

prometheus.scrape "containers" {
  targets    = prometheus.exporter.cadvisor.containers.targets
  forward_to = [prometheus.remote_write.grafana_cloud_metrics.receiver]
}
```

### B. Dashboard LogQL Queries (Loki)
Correct the query labels to align with the deployed telemetry labels:
1. **Nginx Request Rate by Status**: `{job="nginx"}` instead of `{service_name="nginx"}`.
2. **Nginx 4xx/5xx Errors**: `{job="nginx"}` instead of `{service_name="nginx"}`.
3. **Nginx Access Logs**: `{job="nginx"}` instead of `{service_name="nginx"}`.
4. **Journal Logs — Warnings & Errors**: `{instance="$instance"}` instead of `{service_name="loki.source.journal.journald"}`.

### C. New Dashboard Prometheus Panels

1. **CPU Usage (Stat) [Fixed]**: Update to the industry-standard PromQL formula and set the panel's Min Interval to `1m` to prevent the rate NaN/empty bug:
   ```promql
   100 * (1 - sum(rate(node_cpu_seconds_total{mode="idle", instance="$instance"}[$__rate_interval])) / sum(rate(node_cpu_seconds_total{instance="$instance"}[$__rate_interval])))
   ```
2. **Dynamic Network Traffic**: Change the device selector from `{device="ens3"}` to `{device!~"lo|docker.*|tailscale.*|br-.*"}`.
3. **Disk Telemetry Split**:
   * **Disk Throughput (Bytes/s)**: (Existing panel updated) tracking `rate(node_disk_read_bytes_total)` and `rate(node_disk_written_bytes_total)` (Unit: Bps).
   * **Disk IOPS (Ops/s) [NEW]**: tracking `rate(node_disk_reads_completed_total)` and `rate(node_disk_writes_completed_total)` (Unit: iops).
4. **SSH Authentication & Security [NEW]** (inside the `Logs` row):
   * **SSH Successful Logins**: Log list querying `{instance="$instance", job=~"systemd-journal|ssh.service"} |~ "sshd" |~ "Accepted"`.
   * **SSH Failed Logins**: Stat tracker + log list querying `{instance="$instance", job=~"systemd-journal|ssh.service"} |~ "sshd" |~ "Failed password|invalid user|Connection closed by authenticating"`.
5. **Docker Container Performance row [NEW]** (collapsible):
   * **Container CPU Usage (%)**: `sum by(name) (rate(container_cpu_usage_seconds_total{instance="$instance", container_label_com_docker_compose_service=~".+"}[$__rate_interval])) * 100` (Unit: percent).
   * **Container Memory Usage (MB)**: `container_memory_working_set_bytes{instance="$instance", container_label_com_docker_compose_service=~".+"}` (Unit: decbytes).
   * **Container Network Traffic (Bytes/s)**: `sum by(name) (rate(container_network_receive_bytes_total{instance="$instance"}[$__rate_interval]))` (Rx) and `sum by(name) (rate(container_network_transmit_bytes_total{instance="$instance"}[$__rate_interval]))` (Tx).
6. **conCIerge Bot Application Metrics row [NEW]** (collapsible):
   * **Slack Events Rate**: `sum by(event_type) (rate(concierge_slack_events_total{instance="$instance"}[$__rate_interval]))`
   * **Pull Requests Created**: `sum by(resource_type, action) (increase(concierge_slack_pr_created_total{instance="$instance"}[$__rate_interval]))`
   * **Slack API Operations**: `sum by(method, outcome) (rate(concierge_slack_api_calls_total{instance="$instance"}[$__rate_interval]))`
   * **Slack API Call Latency (p95/p50)**: Heatmaps/graphs utilizing `concierge_slack_api_duration_seconds_bucket`.
   * **GitHub API Operations**: `sum by(github_operation, status) (rate(concierge_github_api_calls_total{instance="$instance"}[$__rate_interval]))`
   * **GitHub API Latency (p95/p50)**: Graphs utilizing `concierge_github_api_duration_seconds_bucket`.
   * **Bot Workflows Completed**: `sum by(workflow, outcome) (rate(concierge_slack_workflow_total{instance="$instance"}[$__rate_interval]))`

---

## 3. Verification Plan

1. **Syntax Check & Linting**: Run `ansible-lint` and verify the template syntax remains valid.
2. **Go Test Execution**: Run `go test ./...` in the `src/` directory to ensure no application changes break expectations.
3. **Dashboard Serialization & Validation**: Test the modified Grafana Dashboard JSON using `gcx` commands to ensure it is structurally sound.
4. **Lefthook Verification**: Execute `lefthook run pre-commit` to guarantee all repository-level hooks pass.
