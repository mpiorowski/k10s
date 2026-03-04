# Project Plan: Multi-Cluster Kubernetes TUI Dashboard (Go + Bubble Tea)

## Goal
Build a condensed, high-performance CLI tool that monitors multiple Kubernetes clusters simultaneously. The focus is on dense information aggregation (CPU/Memory, error counts, health checks, logs) rather than a complex, highly customizable UI.

## Core Principles
- **Fast & Concurrent:** Fetch data from multiple clusters asynchronously without blocking the main UI thread.
- **Dense Data:** Show the most critical information at a glance (node health, pod crash loops, resource usage).
- **Dynamic Layout:** Automatically split the terminal into equally sized panels based on the number of selected clusters.
- **Focus Mode:** Allow expanding a single cluster's panel to full screen (e.g., using number keys 1-6).
- **Native Kubernetes Compatibility:** Utilize `k8s.io/client-go` for seamless authentication and API interaction across all standard Kubeconfig setups.

## Phase 1: Foundation & Kubernetes Connection
1. **Initialize Go Project:** Scaffold a new Go module.
2. **CLI Framework:** Integrate `cobra` to handle commands and flags (e.g., `k10s monitor --contexts ctx1,ctx2`).
3. **Kubeconfig Parsing:** Implement logic to read `~/.kube/config` and parse available contexts.
4. **Client-Go Setup:** Create a robust connection manager that instantiates a Kubernetes clientset for each selected context.
5. **Connectivity Check:** Build a simple test command to verify connections to all selected clusters concurrently and print their API server versions.

## Phase 2: Data Aggregation Engine
1. **Concurrent Fetcher:** Design an asynchronous fetching system (using goroutines and channels) that polls the Kubernetes APIs.
2. **Key Metrics:** Implement fetchers for:
   - Node status (Ready/NotReady).
   - Pod states (Running, CrashLoopBackOff, Pending, Evicted).
   - Deployment/StatefulSet rollout status.
   - (Optional but planned) Metrics API integration for CPU/Memory utilization.
3. **State Management:** Create a centralized state object that holds the latest fetched data for each cluster, ready to be consumed by the UI.

## Phase 3: The TUI (Bubble Tea)
1. **Bubble Tea Setup:** Initialize the Bubble Tea application and define the core `Model`, `Update`, and `View` functions.
2. **Dynamic Layout Engine:**
   - Use `lipgloss` to calculate terminal dimensions and split them evenly into vertical or horizontal panels based on the number of clusters.
   - Handle terminal resize events gracefully.
3. **Cluster Panels:** Create a reusable view component for a single cluster that renders the aggregated data clearly.
4. **Focus Mechanics:** Implement keyboard handlers (e.g., `1`, `2`, `3`, or `f` to focus/unfocus) that switch the layout between the multi-pane view and a single full-screen pane for a specific cluster.

## Phase 4: Refinement & Polish
1. **Error Handling:** Gracefully display connection errors or RBAC issues within a cluster's panel without crashing the whole application.
2. **Styling:** Apply minimal, semantic coloring (e.g., red for CrashLoops, green for healthy) using `lipgloss`.
3. **Optimization:** Tune polling intervals and optimize API calls to minimize load on the API servers.

## Future Enhancements
- Aggregated log viewing across clusters.
- Actionable commands (e.g., restarting deployments across all clusters simultaneously).
