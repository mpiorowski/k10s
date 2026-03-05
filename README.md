# k10s

A fast, concurrent, and highly functional multi-cluster Kubernetes terminal dashboard built with Go and Bubble Tea.

`k10s` is the spiritual successor to single-cluster tools like `k9s`. It was born out of the need to view, aggregate, and actively monitor critical information across **multiple** Kubernetes clusters simultaneously within a single terminal window, without needing to run multiple `tmux` panes.

<img width="2541" height="1539" alt="image" src="https://github.com/user-attachments/assets/97ff04df-c74d-4fbf-ab11-f275c9713569" />
<img width="2548" height="1536" alt="image" src="https://github.com/user-attachments/assets/664127a4-4fca-4323-aca8-39b9bbb89dc9" />

## Features

- **Multi-Cluster Support out of the box:** Select any number of contexts from your `~/.kube/config` and monitor them side-by-side.
- **Dynamic Grid Layout:** Automatically calculates an optimal multi-column grid layout based on the number of selected clusters and your terminal dimensions.
- **Focus Mode:** Jump into a specific cluster (full screen) using the number keys (1-9) for a detailed view.
- **Aggregated Health Metrics & Diagnostics:**
  - Node readiness, CPU, and Memory resource usage (via `metrics-server`).
  - Workload readiness for Deployments and StatefulSets (instantly flags degraded apps).
  - Pod lifecycle summary (Running, Pending, Failed).
  - **Proactive Alerts:** Automatically detects and surfaces `OOMKilled` pods, high container `Restarts`, and recent cluster-level `Warning` events (e.g. scheduling failures) directly on the dashboard.
- **Strict Visual Sections:** The dashboard is divided into fixed-height semantic sections (Health, Warnings, Logs) to prevent layout shifting and maximize predictability during an incident.
- **Smart Concurrent Polling:** Uses Go routines to hit the Kubernetes API concurrently and asynchronously; a slow cluster will never block the UI for a fast cluster.
- **Advanced Log Engine:**
  - Filter logs to specific Deployments across your clusters to minimize noise.
  - Unified, chronologically sorted tail across all selected pods.
  - **Error highlighting:** Instantly spot failure lines across any pod in any cluster based on naive string matching.
- **Persistent State:** `k10s` remembers exactly which clusters and deployments you've selected and restores them the next time you open the app.

## Installation

### Via Go Install (Recommended)
If you have Go 1.25+ installed, you can install the latest version directly to your `$GOPATH/bin`:

```bash
go install github.com/mpiorowski/k10s@latest
```

### From Source
```bash
git clone https://github.com/mpiorowski/k10s.git
cd k10s
go build -o k10s .
```

You can move the `k10s` binary to your `$PATH` (e.g., `sudo mv k10s /usr/local/bin/`).

## Usage

Start the interactive dashboard by simply running:

```bash
k10s
```

If it's your first run, you will be prompted to select which Kubernetes clusters you want to monitor.

Alternatively, you can bypass the interactive selection and pass your clusters directly via flags:
```bash
k10s --contexts dev-cluster,prod-cluster
```

## Keyboard Shortcuts

### Global Navigation
*   **`s` or `c`**: Re-open the cluster selection menu.
*   **`1-9`**: Focus (full screen) on a specific cluster panel. Press the same number again to un-focus and return to the split multi-cluster view.
*   **`i`**: Open the interactive Legend/Info screen to see what the condensed metrics and acronyms mean.
*   **`q` or `ctrl+c`**: Quit the application.

### Log Management
*   **`l` (Logs)**: Toggle the Recent Logs view.
    *   *Note: Pressing `l` for the first time will open the Deployment Selection screen. You must check which deployments you want to watch before logs will appear.*
*   **`e` (Errors Only)**: Toggle the log filter. When active, it will strictly hide all log lines unless they contain an error-related word (e.g., `error`, `err`, `fail`, `exception`).
*   **`w` (Warns Only)**: Toggle the log filter. When active, it will strictly hide all log lines unless they contain a warning-related word (e.g., `warn`, `warning`).
    *   *Note: You can activate both `e` and `w` simultaneously to see only errors and warnings, filtering out all standard info/debug logs.*

## Configuration Directory

`k10s` saves your UI preferences to your operating system's standard user configuration directory:
- **Linux:** `~/.config/k10s/config.json`
- **macOS:** `~/Library/Application Support/k10s/config.json`
- **Windows:** `%AppData%\k10s\config.json`

*(Note: The `k10s` engine still exclusively reads your standard `~/.kube/config` for cluster authentication.)*

<img width="2547" height="1539" alt="image" src="https://github.com/user-attachments/assets/c7a8a786-7d09-408c-af31-36021d0735a8" />
<img width="2556" height="1534" alt="image" src="https://github.com/user-attachments/assets/e4563f49-c991-4b53-a464-dd7c64e2c44d" />
<img width="1278" height="972" alt="image" src="https://github.com/user-attachments/assets/b7d7d5b6-22eb-4b95-9456-0dd298a6e47f" />
<img width="799" height="584" alt="image" src="https://github.com/user-attachments/assets/915d2c4a-ccd3-425b-b5c8-ad164bd180fc" />

