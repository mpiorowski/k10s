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
- `pkg/config/config.go`: Handles saving and loading the user's persistent preferences (selected clusters, log filters) to the OS standard user config directory (e.g., `~/.config/k10s/config.json`).
- `pkg/k8s/client.go`: The core Kubernetes interaction engine. Contains the `ClientManager` which handles concurrent API fetching, pod log streaming, and log level identification.
- `ui/app.go`: The Bubble Tea application logic. Manages the state machine (Selection Menu vs Dashboard vs Log Menus), keyboard event handlers, and the dynamic rendering of the Lipgloss split-pane grid layout.

## Application States & Workflow
The TUI operates a state machine with 4 distinct views (`viewState`):

1. **`stateSelection` (Cluster Selection):**
   - Triggered on first run or by pressing `s`.
   - Parses all available contexts from `~/.kube/config`.
   - Renders an interactive checklist. Selections are saved to the OS standard user config directory.
2. **`stateDashboard` (Main Multi-Cluster View):**
   - The primary monitoring layout.
   - Calculates available terminal width/height and dynamically computes a grid layout of panels based on the number of selected clusters.
   - Users can press `1-9` to "Focus" a specific cluster, expanding its panel to fill the entire terminal screen.
3. **`stateLogSelection` (Deployment Log Filtering):**
   - Triggered by pressing `l`.
   - Fetches and deduplicates all `Deployments` running across the active clusters.
   - Allows users to select specific deployments to watch logs for, preventing the UI from being overwhelmed by noisy system pods.
4. **`stateInfo` (Legend & Help):**
   - Triggered by pressing `i`.
   - Displays a static overlay explaining the highly condensed dashboard acronyms (Deps, STS, R/P/F pod codes) and diagnostic triggers (OOMKilled, Restarts, Warnings) alongside a keyboard shortcut reference.

## Data Fetching Engine (`pkg/k8s`)
The `ClientManager` is designed to be highly concurrent and respectful of the Kubernetes API to avoid rate-limiting or causing high load:

- **Polling:** The Bubble Tea `tick()` command triggers a background refresh for all active clusters every 5 seconds.
- **Metrics Fetching:** Uses parallel Go routines to fetch Node Readiness, Pod Phase aggregations (Running/Pending/Failed), and CPU/Memory capacities and usages.
- **Diagnostic Fetching:** Inspects `ContainerStatuses` for `OOMKilled` termination codes and total `RestartCount`.
- **Workload Fetching:** Compares expected `Replicas` against `ReadyReplicas` for `Deployments` and `StatefulSets` to identify and list degraded applications.
- **Event Fetching:** Filters the `CoreV1().Events` stream for `type=Warning` within the last hour to surface critical cluster alarms (e.g. FailedScheduling).
- **Log Fetching (Optimized):**
  - Only fetches logs for pods whose names match the user's selected deployment filters.
  - Limits API calls to a maximum of 20 pods per refresh cycle.
  - Uses `TailLines: 40` to quickly grab the end of the log stream.
  - **Chronological Aggregation:** Explicitly requests API timestamps, parses them, and sorts all interleaved pod logs chronologically before rendering.
  - Maintains an in-memory rolling buffer of the last 100 log lines per cluster.
- **Log Parsing:**
  - Evaluates raw log lines using fast string matching to automatically upgrade a log line to an "Error" state (highlighted in red) or "Warn" state (highlighted in yellow) based on matching common level strings like "error", "fail", "exception" or "warn".

## Styling & Layout Mechanics
- **Strict Grid Layout:** The dashboard creates a multi-column matrix depending on the number of selected clusters, keeping boxes close to a golden ratio.
- **Semantic Sections:** Each cluster panel uses rigid, fixed-height internal blocks (Health, Active Warnings, Recent Logs) that are clearly delineated by visual borders. This guarantees UI predictability and zero layout shift.
- **Reactive Content:** Instead of collapsing empty panels, "✅ Healthy" placeholders fill empty diagnostic zones, maintaining grid stability and providing reassuring feedback.
- **Semantic Colors:**
  - **Green:** Healthy nodes, Running pods, CPU/Mem < 75%.
  - **Yellow:** Pending pods, CPU/Mem > 75% (Warning), Warn logs.
  - **Red:** Failed/CrashLoop pods, Error logs, CPU/Mem > 90% (Critical).
- **Dynamic Log Rendering:** To prevent layout breakage, the `RecentLogs` buffer calculates exact available space and mathematically trims rows to perfectly fill the bottom of the panel without overflowing. Users can press `r` to toggle between truncated (with `...` ellipsis) and wrapped log lines. The wrap setting is persisted in the user config.
