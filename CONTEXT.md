# k10s Architecture Context

## Overview
`k10s` is a high-performance, multi-cluster Kubernetes TUI (Terminal User Interface) dashboard written in Go. It enables developers and operators to monitor critical health metrics and aggregated, structured logs across multiple Kubernetes contexts simultaneously in a dynamically split terminal layout.

## Core Technologies
- **Go 1.25+**: The core language, chosen for its native Kubernetes ecosystem integration and concurrency primitives.
- **k8s.io/client-go**: The official Kubernetes client. Used to parse `~/.kube/config`, handle authentication (AWS IAM, GCP, OIDC), and interact with the core Kubernetes API (`CoreV1`, `AppsV1`).
- **k8s.io/metrics**: The official Kubernetes metrics client. Used to fetch node-level CPU and Memory utilization if the `metrics-server` is deployed on the target clusters.
- **Bubble Tea (`github.com/charmbracelet/bubbletea`)**: The Elm-inspired TUI framework that manages the application state, event loop, and view rendering.
- **Lipgloss (`github.com/charmbracelet/lipgloss`)**: A styling and layout library used to calculate dynamic terminal dimensions, render borders, and apply semantic color coding to metrics and logs.
- **Cobra (`github.com/spf13/cobra`)**: The CLI framework for parsing command-line flags (e.g., `-c ctx1,ctx2`).

## Project Structure
- `main.go`: The entry point that executes the Cobra root command.
- `cmd/root.go`: Defines the CLI flags and initializes the Bubble Tea application (`ui.NewApp()`).
- `pkg/config/config.go`: Handles saving and loading the user's persistent preferences (selected clusters, log filters, JSON keys) to `~/.k10s.json`.
- `pkg/k8s/client.go`: The core Kubernetes interaction engine. Contains the `ClientManager` which handles concurrent API fetching, pod log streaming, and dynamic JSON log parsing.
- `ui/app.go`: The Bubble Tea application logic. Manages the state machine (Selection Menu vs Dashboard vs Log Menus), keyboard event handlers, and the dynamic rendering of the Lipgloss split-pane layout.

## Application States & Workflow
The TUI operates a state machine with 4 distinct views (`viewState`):

1. **`stateSelection` (Cluster Selection):**
   - Triggered on first run or by pressing `s`.
   - Parses all available contexts from `~/.kube/config`.
   - Renders an interactive checklist. Selections are saved to `~/.k10s.json`.
2. **`stateDashboard` (Main Multi-Cluster View):**
   - The primary monitoring layout.
   - Calculates available terminal width/height and dynamically splits the screen into `N` vertical panels based on the number of selected clusters.
   - Users can press `1-9` to "Focus" a specific cluster, expanding its panel to fill the entire terminal screen.
3. **`stateLogSelection` (Deployment Log Filtering):**
   - Triggered by pressing `l`.
   - Fetches and deduplicates all `Deployments` running across the active clusters.
   - Allows users to select specific deployments to watch logs for, preventing the UI from being overwhelmed by noisy system pods.
4. **`stateLogKeyParse` (Dynamic JSON Formatting):**
   - Triggered by pressing `p` (only available when a specific cluster is "Focused").
   - Scans the currently buffered log lines for that specific cluster, discovers all available JSON keys, and allows the user to select which keys to render in a `key=value` format.
   - Selections are stored on a *per-cluster* basis.

## Data Fetching Engine (`pkg/k8s`)
The `ClientManager` is designed to be highly concurrent and respectful of the Kubernetes API to avoid rate-limiting or causing high load:

- **Polling:** The Bubble Tea `tick()` command triggers a background refresh for all active clusters every 5 seconds.
- **Metrics Fetching:** Uses parallel Go routines to fetch Node Readiness, Pod Phase aggregations (Running/Pending/Failed), and CPU/Memory capacities and usages.
- **Log Fetching (Optimized):**
  - Only fetches logs for pods whose names match the user's selected deployment filters.
  - Limits API calls to a maximum of 20 pods per refresh cycle.
  - Uses `TailLines: 40` to quickly grab the end of the log stream.
  - Maintains an in-memory rolling buffer of the last 100 log lines per cluster.
- **Smart JSON Parser:**
  - If a log line is raw text, it passes it through.
  - If a log line is structured JSON, it intercepts it, parses the object, and dynamically re-formats the output string based on the user's selected keys (configured via the `p` menu).
  - Automatically upgrades a log line to an "Error" state (highlighted in red) if the word "error" appears, or if the parsed JSON contains `"level": "error"`.

## Styling & Layout Mechanics
- **Semantic Colors:**
  - **Green:** Healthy nodes, Running pods, CPU/Mem < 75%.
  - **Yellow:** Pending pods, CPU/Mem > 75% (Warning).
  - **Red:** Failed/CrashLoop pods, Error logs, CPU/Mem > 90% (Critical).
- **Dynamic Log Rendering:** To prevent layout breakage, the dashboard calculates how much vertical space is consumed by the Header and Pod Stats, and mathematically trims the `RecentLogs` buffer to render exactly the number of lines needed to perfectly fill the bottom of the panel without overflowing.
