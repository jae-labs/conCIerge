# Design Spec: Overview KPI Panels Stat-Card Redesign

Re-engineer the top-row Overview KPI panels (CPU Usage, Memory Usage, Disk Usage) of the conCIerge dashboard from circular gauges to high-impact stat cards.

---

## 1. Problem Statement
The current circular gauge visualizations in `panel-15`, `panel-16`, and `panel-17` are confined within small `4x4` grid layout cells. Because the circular gauge arc consumes a large amount of horizontal and vertical whitespace, the numeric value text inside is scaled down significantly by Grafana. This makes the numbers tiny, cramped, and difficult to read at a glance, resulting in a suboptimal user experience.

---

## 2. Proposed Solution (Option A: High-Impact Stat Cards)
We will convert `panel-15`, `panel-16`, and `panel-17` to Grafana `stat` panels using a clean, minimalist design system:
- **Big Typography**: The numeric values will scale up to the maximum available size inside the grid cell.
- **Dynamic Value Colors**: The numeric values will be colored based on resource thresholds (Green for normal, Yellow for warning, Red for high load).
- **Subtle Trend Sparklines**: A low-opacity, background area graph/sparkline will display the historical trend of the metric over the active time range (e.g., the last 1 hour), giving valuable context on whether resource usage is rising or falling.
- **Clean Background**: The background of the panel will remain neutral dark/light (matching the dashboard theme) rather than using solid color blocks, ensuring a sleek, professional layout.

---

## 3. Implementation Details

### File Changes
We will modify the locally tracked dashboard configuration at:
`/Users/luiz1361/.gemini/antigravity-cli/brain/c360ad4f-5e9e-40ec-9ecd-be458594effb/scratch/dashboard.json`

### Panel Specific Changes
For `panel-15` (CPU), `panel-16` (Memory), and `panel-17` (Disk), we will update the `vizConfig` section to:
- Change `"group"` from `"gauge"` to `"stat"`.
- Set the following options under `"spec.options"`:
  - `"colorMode"`: `"value"` (colors only the numeric value text)
  - `"graphMode"`: `"area"` (displays background sparkline area)
  - `"justifyMode"`: `"center"` (centers the value and text)
  - `"textMode"`: `"value_and_name"` (shows both metric name and value)
  - `"orientation"`: `"auto"`

---

## 4. Verification and Deployment
1. **JSON Validation**: Verify the syntax of the modified `dashboard.json` using a Python linter/parser.
2. **Grafana Cloud Update**: Deploy the changes to Grafana Cloud via the `gcx` CLI tool:
   `gcx dashboards update --config /Users/luiz1361/.gemini/antigravity-cli/brain/c360ad4f-5e9e-40ec-9ecd-be458594effb/scratch/dashboard.json`
3. **Repository Sanity Check**: Run pre-commit checks using `lefthook run pre-commit` to ensure compliance with monorepo rules.
