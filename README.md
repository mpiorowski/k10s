# k10s

A fast, concurrent, and highly functional multi-cluster Kubernetes terminal dashboard built with Go and Bubble Tea.

`k10s` is the spiritual successor to single-cluster tools like `k9s`. It was born out of the need to view, aggregate, and actively monitor critical information across **multiple** Kubernetes clusters simultaneously within a single terminal window, without needing to run multiple `tmux` panes.

## Features

- **Multi-Cluster Support out of the box:** Select any number of contexts from your `~/.kube/config` and monitor them side-by-side.
- **Dynamic Split-Pane Layout:** Automatically adjusts and splits your terminal screen depending on how many clusters you are monitoring.
- **Focus Mode:** Jump into a specific cluster (full screen) using the number keys (1-9) for a detailed view.
- **Aggregated Health Metrics:**
  - Node readiness and counts.
  - Pod lifecycle states (Running, Pending, Failed/CrashLoopBackOff).
  - High-level CPU & Memory resource usage (if `metrics-server` is deployed on the cluster).
- **Smart Concurrent Polling:** Uses Go routines to hit the Kubernetes API concurrently and asynchronously; a slow cluster will never block the UI for a fast cluster.
- **Advanced Log Engine:**
  - Filter logs to specific Deployments across your clusters to minimize noise.
  - Unified, chronologically sorted tail across all selected pods.
  - **Dynamic JSON Parsing:** Scans the logs for JSON structures, allows you to dynamically pick which JSON keys you care about, and automatically reformats them into clean `key=value` strings!
  - **Error highlighting:** Instantly spot failure lines across any pod in any cluster.
- **Persistent State:** `k10s` remembers exactly which clusters, deployments, and JSON log keys you've selected and restores them the next time you open the app.

## Installation

### From Source
Ensure you have Go 1.25+ installed.

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
*   **`q` or `ctrl+c`**: Quit the application.

### Log Management
*   **`l` (Logs)**: Toggle the Recent Logs view.
    *   *Note: Pressing `l` for the first time will open the Deployment Selection screen. You must check which deployments you want to watch before logs will appear.*
*   **`e` (Errors Only)**: Toggle the log filter. When active, it will strictly hide all log lines unless they contain an error-related word (e.g., `error`, `err`, `fail`, `exception`) or have a JSON log level of `error`/`fatal`.
*   **`p` (Parse JSON)**: *Available only when a specific cluster is Focused (e.g., by pressing `1`).* Opens the JSON key extraction menu for that specific cluster.

## Configuration Directory

`k10s` saves your UI preferences to your operating system's standard user configuration directory:
- **Linux:** `~/.config/k10s/config.json`
- **macOS:** `~/Library/Application Support/k10s/config.json`
- **Windows:** `%AppData%\k10s\config.json`

*(Note: The `k10s` engine still exclusively reads your standard `~/.kube/config` for cluster authentication.)*