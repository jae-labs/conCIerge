# Implementation Plan: Overview KPI Panels Stat-Card Redesign

This plan describes the step-by-step implementation to redesign `panel-15`, `panel-16`, and `panel-17` of the Grafana dashboard to use high-impact stat cards with background sparklines.

---

## Todo List

- [ ] Task 1: Redesign panel-15, panel-16, and panel-17 inside `dashboard.json` <!-- id: 1 -->
- [ ] Task 2: Push changes to Grafana Cloud and verify <!-- id: 2 -->

---

## Task Details

### Task 1: Redesign panel-15, panel-16, and panel-17 inside `dashboard.json`
* **Goal**: Update the visualization type of CPU, Memory, and Disk Overview panels from circular gauges to sleek stat cards.
* **Steps**:
  1. Write a Python script to load `/Users/luiz1361/.gemini/antigravity-cli/brain/c360ad4f-5e9e-40ec-9ecd-be458594effb/scratch/dashboard.json`.
  2. For `panel-15`, `panel-16`, and `panel-17`, update the `vizConfig` object:
     - Change `"group"` to `"stat"`.
     - Set `"spec.options"` to:
       ```json
       {
         "colorMode": "value",
         "graphMode": "area",
         "justifyMode": "center",
         "orientation": "auto",
         "percentChangeColorMode": "standard",
         "reduceOptions": {
           "calcs": ["lastNotNull"],
           "fields": "",
           "values": false
         },
         "showPercentChange": false,
         "textMode": "value_and_name",
         "wideLayout": true
       }
       ```
  3. Write the updated JSON back to `/Users/luiz1361/.gemini/antigravity-cli/brain/c360ad4f-5e9e-40ec-9ecd-be458594effb/scratch/dashboard.json` with pretty indent of 2 spaces.

---

### Task 2: Push changes to Grafana Cloud and verify
* **Goal**: Deploy the modified dashboard to the live Grafana stack and run sanity checks.
* **Steps**:
  1. Retrieve the latest `resourceVersion` dynamically from Grafana Cloud to avoid any concurrent edit conflicts (409).
  2. Sync the live `resourceVersion` into `/Users/luiz1361/.gemini/antigravity-cli/brain/c360ad4f-5e9e-40ec-9ecd-be458594effb/scratch/dashboard.json`.
  3. Run the deployment command:
     `gcx dashboards update luxtx8z --config /Users/luiz1361/.gemini/antigravity-cli/brain/c360ad4f-5e9e-40ec-9ecd-be458594effb/scratch/dashboard.json`
  4. Run `lefthook run pre-commit` to ensure code/repo styling rules are perfectly preserved.
  5. Verify git status and confirm there are no unstaged/dirty changes.
