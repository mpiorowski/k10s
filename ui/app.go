package ui

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"k8s.io/client-go/util/homedir"

	"k10s/pkg/config"
	"k10s/pkg/k8s"
)

type viewState int

const (
	stateDashboard viewState = iota
	stateSelection
	stateLogSelection
	stateLogKeyParse
)

type tickMsg time.Time

type statusMsg struct {
	ctxName string
	status  k8s.ClusterStatus
}

type initDashboardMsg struct{}
type deploymentsMsg []string

type App struct {
	state viewState

	// Dashboard state
	contexts   []string
	manager    *k8s.ClientManager
	statuses   map[string]k8s.ClusterStatus
	width      int
	height     int
	ready      bool
	focusedIdx int // -1 means show all
	initErr    error

	showLogs       bool
	logsOnlyErrors bool

	// Selection state
	allContexts []string
	cursor      int
	selected    map[string]struct{}

	// Log Selection state
	allDeployments      []string
	logCursor           int
	selectedDeployments map[string]struct{}
	activeLogFilters    []string

	// Log Key Parse state
	availableLogKeys []string
	logKeyCursor     int
	selectedLogKeys  map[string]struct{}
	activeLogKeys    map[string][]string
}

func NewApp(initialContexts []string) *App {
	kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
	if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
		kubeconfig = envKubeconfig
	}

	allCtx, _ := k8s.GetAllContexts(kubeconfig)
	sort.Strings(allCtx)

	selectedMap := make(map[string]struct{})
	for _, c := range initialContexts {
		selectedMap[c] = struct{}{}
	}

	app := &App{
		allContexts:         allCtx,
		selected:            selectedMap,
		selectedDeployments: make(map[string]struct{}),
		selectedLogKeys:     make(map[string]struct{}),
		activeLogKeys:       make(map[string][]string),
		width:               80,
		height:              24,
	}

	// Load previously saved log configuration
	cfg, err := config.LoadConfig()
	if err == nil {
		app.showLogs = cfg.ShowLogs
		app.logsOnlyErrors = cfg.LogsOnlyErrors
		app.activeLogFilters = cfg.SelectedLogFilters
		if cfg.SelectedLogKeys != nil {
			app.activeLogKeys = cfg.SelectedLogKeys
		}
		for _, f := range cfg.SelectedLogFilters {
			app.selectedDeployments[f] = struct{}{}
		}
	}

	if len(initialContexts) > 0 {
		app.contexts = initialContexts
		app.state = stateDashboard
		app.initManager(kubeconfig)
	} else {
		app.state = stateSelection
	}

	return app
}

func (a *App) initManager(kubeconfig string) {
	manager, err := k8s.NewClientManager(kubeconfig, a.contexts)
	a.manager = manager
	a.initErr = err
	a.statuses = make(map[string]k8s.ClusterStatus)
	a.focusedIdx = -1
}

func (a *App) Init() tea.Cmd {
	var cmds []tea.Cmd
	cmds = append(cmds, tick())

	if a.state == stateDashboard {
		if a.initErr != nil {
			return tea.Quit
		}
		cmds = append(cmds, func() tea.Msg { return initDashboardMsg{} })
	}
	return tea.Batch(cmds...)
}

func fetchClusterStatus(manager *k8s.ClientManager, ctxName string, logFilters []string, jsonKeys []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		status := manager.FetchStatus(ctx, ctxName, logFilters, jsonKeys)
		return statusMsg{ctxName: ctxName, status: status}
	}
}

func fetchDeployments(manager *k8s.ClientManager, contexts []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		deps, _ := manager.GetDeployments(ctx, contexts)
		
		sort.Slice(deps, func(i, j int) bool {
			// Extract context and namespace
			// format: [ctx] ns/name
			parse := func(s string) (string, string, string) {
				parts := strings.SplitN(s, "] ", 2)
				if len(parts) != 2 {
					return "", "", s
				}
				c := strings.TrimPrefix(parts[0], "[")
				nsName := strings.SplitN(parts[1], "/", 2)
				if len(nsName) != 2 {
					return c, "", parts[1]
				}
				return c, nsName[0], nsName[1]
			}
			
			c1, ns1, n1 := parse(deps[i])
			c2, ns2, n2 := parse(deps[j])
			
			if c1 != c2 {
				return c1 < c2
			}
			if ns1 == "default" && ns2 != "default" {
				return true
			}
			if ns1 != "default" && ns2 == "default" {
				return false
			}
			if ns1 != ns2 {
				return ns1 < ns2
			}
			return n1 < n2
		})
		
		return deploymentsMsg(deps)
	}
}

func tick() tea.Cmd {
	return tea.Tick(time.Second*5, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (a *App) saveConfig() {
	cfg := config.Config{
		SelectedContexts:   a.contexts,
		SelectedLogFilters: a.activeLogFilters,
		SelectedLogKeys:    a.activeLogKeys,
		ShowLogs:           a.showLogs,
		LogsOnlyErrors:     a.logsOnlyErrors,
	}
	_ = config.SaveConfig(cfg)
}

func (a *App) updateSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.cursor > 0 {
			a.cursor--
		}
	case "down", "j":
		if a.cursor < len(a.allContexts)-1 {
			a.cursor++
		}
	case " ":
		if len(a.allContexts) == 0 {
			return a, nil
		}
		ctxName := a.allContexts[a.cursor]
		if _, ok := a.selected[ctxName]; ok {
			delete(a.selected, ctxName)
		} else {
			a.selected[ctxName] = struct{}{}
		}
	case "enter":
		var newCtxs []string
		for _, c := range a.allContexts { // Preserve alphabetical order
			if _, ok := a.selected[c]; ok {
				newCtxs = append(newCtxs, c)
			}
		}
		if len(newCtxs) == 0 {
			return a, nil // Prevent starting with 0 clusters
		}

		a.contexts = newCtxs
		a.state = stateDashboard
		a.saveConfig()

		kubeconfig := filepath.Join(homedir.HomeDir(), ".kube", "config")
		if envKubeconfig := os.Getenv("KUBECONFIG"); envKubeconfig != "" {
			kubeconfig = envKubeconfig
		}
		a.initManager(kubeconfig)

		return a, func() tea.Msg { return initDashboardMsg{} }
	case "esc":
		if len(a.contexts) > 0 {
			a.state = stateDashboard
			// Restore selection to match what was previously active
			a.selected = make(map[string]struct{})
			for _, c := range a.contexts {
				a.selected[c] = struct{}{}
			}
		}
	}
	return a, nil
}

func (a *App) updateLogSelection(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.logCursor > 0 {
			a.logCursor--
		}
	case "down", "j":
		if a.logCursor < len(a.allDeployments)-1 {
			a.logCursor++
		}
	case " ":
		if len(a.allDeployments) == 0 {
			return a, nil
		}
		dep := a.allDeployments[a.logCursor]
		if _, ok := a.selectedDeployments[dep]; ok {
			delete(a.selectedDeployments, dep)
		} else {
			a.selectedDeployments[dep] = struct{}{}
		}
	case "enter":
		var filters []string
		// Preserve alphabetical order
		for _, dep := range a.allDeployments {
			if _, ok := a.selectedDeployments[dep]; ok {
				filters = append(filters, dep)
			}
		}
		
		if len(filters) == 0 {
			// Don't show logs if nothing is selected
			a.showLogs = false
			a.state = stateDashboard
			a.saveConfig()
			return a, nil
		}
		
		a.activeLogFilters = filters
		a.showLogs = true
		a.state = stateDashboard
		a.saveConfig()
		
		// trigger a refresh
		var cmds []tea.Cmd
		for _, ctx := range a.contexts {
			cmds = append(cmds, fetchClusterStatus(a.manager, ctx, a.activeLogFilters, a.activeLogKeys[ctx]))
		}
		return a, tea.Batch(cmds...)
	case "esc":
		a.state = stateDashboard
		return a, nil
	}
	return a, nil
}

func (a *App) updateLogKeyParse(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "up", "k":
		if a.logKeyCursor > 0 {
			a.logKeyCursor--
		}
	case "down", "j":
		if a.logKeyCursor < len(a.availableLogKeys)-1 {
			a.logKeyCursor++
		}
	case " ":
		if len(a.availableLogKeys) == 0 {
			return a, nil
		}
		key := a.availableLogKeys[a.logKeyCursor]
		if _, ok := a.selectedLogKeys[key]; ok {
			delete(a.selectedLogKeys, key)
		} else {
			a.selectedLogKeys[key] = struct{}{}
		}
	case "enter":
		var filters []string
		for _, key := range a.availableLogKeys {
			if _, ok := a.selectedLogKeys[key]; ok {
				filters = append(filters, key)
			}
		}
		
		if a.focusedIdx != -1 {
			ctxName := a.contexts[a.focusedIdx]
			a.activeLogKeys[ctxName] = filters
		}

		a.state = stateDashboard
		a.saveConfig()
		
		var cmds []tea.Cmd
		for _, ctx := range a.contexts {
			cmds = append(cmds, fetchClusterStatus(a.manager, ctx, a.activeLogFilters, a.activeLogKeys[ctx]))
		}
		return a, tea.Batch(cmds...)
	case "esc":
		a.state = stateDashboard
		return a, nil
	}
	return a, nil
}

func (a *App) updateDashboard(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch msg.String() {
	case "1", "2", "3", "4", "5", "6", "7", "8", "9":
		idx := int(msg.String()[0] - '1')
		if idx < len(a.contexts) {
			if a.focusedIdx == idx {
				a.focusedIdx = -1
			} else {
				a.focusedIdx = idx
			}
		}
	case "s", "c":
		a.state = stateSelection
		return a, nil
	case "l":
		if a.showLogs {
			a.showLogs = false
			a.saveConfig()
			return a, nil
		}
		a.state = stateLogSelection
		a.allDeployments = nil
		a.logCursor = 0
		return a, fetchDeployments(a.manager, a.contexts)
	case "e":
		a.logsOnlyErrors = !a.logsOnlyErrors
		a.saveConfig()
		return a, nil
	case "p":
		if a.focusedIdx == -1 {
			return a, nil
		}

		a.state = stateLogKeyParse
		a.logKeyCursor = 0
		a.selectedLogKeys = make(map[string]struct{})

		ctxName := a.contexts[a.focusedIdx]
		if currentKeys, ok := a.activeLogKeys[ctxName]; ok {
			for _, k := range currentKeys {
				a.selectedLogKeys[k] = struct{}{}
			}
		}

		// Extract all unique JSON keys from current logs for the focused cluster
		keySet := make(map[string]struct{})
		if status, ok := a.statuses[ctxName]; ok {
			for _, log := range status.RecentLogs {
				line := strings.TrimSpace(log.RawMessage)
				if strings.HasPrefix(line, "{") && strings.HasSuffix(line, "}") {
					var jsonLog map[string]interface{}
					if err := json.Unmarshal([]byte(line), &jsonLog); err == nil {
						for k := range jsonLog {
							keySet[k] = struct{}{}
						}
					}
				}
			}
		}
		var keys []string
		for k := range keySet {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		a.availableLogKeys = keys
		return a, nil
	}
	return a, nil
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return a, tea.Quit
		}

		if a.state == stateSelection {
			return a.updateSelection(msg)
		} else if a.state == stateLogSelection {
			return a.updateLogSelection(msg)
		} else if a.state == stateLogKeyParse {
			return a.updateLogKeyParse(msg)
		} else {
			return a.updateDashboard(msg)
		}

	case tea.WindowSizeMsg:
		a.width = msg.Width
		a.height = msg.Height
		a.ready = true

	case deploymentsMsg:
		a.allDeployments = msg
		return a, nil

	case initDashboardMsg:
		var cmds []tea.Cmd
		for _, ctx := range a.contexts {
			cmds = append(cmds, fetchClusterStatus(a.manager, ctx, a.activeLogFilters, a.activeLogKeys[ctx]))
		}
		return a, tea.Batch(cmds...)

	case statusMsg:
		a.statuses[msg.ctxName] = msg.status
		return a, nil

	case tickMsg:
		var cmds []tea.Cmd
		if a.state == stateDashboard {
			for _, ctx := range a.contexts {
				cmds = append(cmds, fetchClusterStatus(a.manager, ctx, a.activeLogFilters, a.activeLogKeys[ctx]))
			}
		}
		cmds = append(cmds, tick())
		return a, tea.Batch(cmds...)
	}

	return a, nil
}

func (a *App) viewSelection() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Select Kubernetes Clusters to Monitor")
	subHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Use UP/DOWN to navigate, SPACE to select, ENTER to confirm. (ESC to cancel)")

	var rows []string
	for i, ctx := range a.allContexts {
		cursor := " " // no cursor
		if a.cursor == i {
			cursor = ">" // cursor!
		}

		checked := " " // not selected
		if _, ok := a.selected[ctx]; ok {
			checked = "x" // selected!
		}

		row := fmt.Sprintf("%s [%s] %s", cursor, checked, ctx)
		if a.cursor == i {
			rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Render(row))
		} else if checked == "x" {
			rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(row))
		} else {
			rows = append(rows, row)
		}
	}

	if len(a.allContexts) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("No clusters found in kubeconfig."))
	}

	body := lipgloss.NewStyle().PaddingLeft(2).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	return fmt.Sprintf("\n  %s\n  %s\n\n%s\n", header, subHeader, body)
}

func (a *App) viewLogSelection() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Select Deployments to Watch Logs For")
	subHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Use UP/DOWN to navigate, SPACE to select, ENTER to confirm. (ESC to cancel)")

	var rows []string
	
	if a.allDeployments == nil {
		rows = append(rows, "Fetching deployments from clusters...")
	} else if len(a.allDeployments) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("No deployments found in selected clusters."))
	} else {
		for i, dep := range a.allDeployments {
			cursor := " " // no cursor
			if a.logCursor == i {
				cursor = ">" // cursor!
			}

			checked := " " // not selected
			if _, ok := a.selectedDeployments[dep]; ok {
				checked = "x" // selected!
			}

			// Format: [ctx] ns/name
			var formattedDep string
			parts := strings.SplitN(dep, "] ", 2)
			if len(parts) == 2 {
				ctxStr := parts[0] + "]"
				nsName := strings.SplitN(parts[1], "/", 2)
				if len(nsName) == 2 {
					ns := nsName[0]
					name := nsName[1]
					
					ctxStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("62"))
					nsStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
					nameStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))
					
					if ns == "default" {
						nsStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("35")) // Distinct color for default
					}
					
					if a.logCursor == i || checked == "x" {
						// Let the row highlight override if needed, or keep these base colors
					}
					
					formattedDep = ctxStyle.Render(ctxStr+" ") + nsStyle.Render(ns+"/") + nameStyle.Render(name)
				} else {
					formattedDep = dep
				}
			} else {
				formattedDep = dep
			}

			row := fmt.Sprintf("%s [%s] %s", cursor, checked, formattedDep)
			if a.logCursor == i {
				// Highlight entire row but preserve custom formatting by replacing just the prefix
				prefix := lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Render(fmt.Sprintf("%s [%s] ", cursor, checked))
				rows = append(rows, prefix + formattedDep)
			} else if checked == "x" {
				prefix := lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(fmt.Sprintf("%s [%s] ", cursor, checked))
				rows = append(rows, prefix + formattedDep)
			} else {
				rows = append(rows, row)
			}
		}
	}

	body := lipgloss.NewStyle().PaddingLeft(2).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	return fmt.Sprintf("\n  %s\n  %s\n\n%s\n", header, subHeader, body)
}

func (a *App) viewLogKeyParse() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("Select JSON Keys to Display in Logs")
	subHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Use UP/DOWN to navigate, SPACE to select, ENTER to confirm. (ESC to cancel)")

	var rows []string
	
	if len(a.availableLogKeys) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("9")).Render("No JSON logs found in current buffer to extract keys from."))
	} else {
		for i, key := range a.availableLogKeys {
			cursor := " " // no cursor
			if a.logKeyCursor == i {
				cursor = ">" // cursor!
			}

			checked := " " // not selected
			if _, ok := a.selectedLogKeys[key]; ok {
				checked = "x" // selected!
			}

			row := fmt.Sprintf("%s [%s] %s", cursor, checked, key)
			if a.logKeyCursor == i {
				rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("212")).Render(row))
			} else if checked == "x" {
				rows = append(rows, lipgloss.NewStyle().Foreground(lipgloss.Color("10")).Render(row))
			} else {
				rows = append(rows, row)
			}
		}
	}

	body := lipgloss.NewStyle().PaddingLeft(2).Render(lipgloss.JoinVertical(lipgloss.Left, rows...))
	return fmt.Sprintf("\n  %s\n  %s\n\n%s\n", header, subHeader, body)
}

func (a *App) viewDashboard() string {
	if a.initErr != nil {
		return fmt.Sprintf("Error initializing K8s clients:\n%v\n\nPress 'q' to quit", a.initErr)
	}

	headerStr := fmt.Sprintf("Monitoring %d Clusters | 1-%d focus | 's' cls | 'l' logs | 'e' err | 'q' quit", len(a.contexts), len(a.contexts))
	if a.focusedIdx != -1 {
		headerStr = fmt.Sprintf("Focused on: %s | %d un-focus | 's' cls | 'l' logs | 'e' err | 'p' parse | 'q' quit", a.contexts[a.focusedIdx], a.focusedIdx+1)
	}
	header := lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230")).Render(headerStr)

	panels := []string{}

	var contextsToRender []string
	if a.focusedIdx != -1 {
		contextsToRender = []string{a.contexts[a.focusedIdx]}
	} else {
		contextsToRender = a.contexts
	}

	panelWidth := (a.width / len(contextsToRender))
	if panelWidth < 15 {
		panelWidth = 15 // min width
	}

	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(panelWidth - 2). // Account for borders
		Height(a.height - 3)   // Account for header, footer and borders

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9")) // Red
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))   // Green
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11")) // Yellow
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))

	for _, ctx := range contextsToRender {
		var content string
		status, exists := a.statuses[ctx]

		content += titleStyle.Render(fmt.Sprintf("Cluster: %s", ctx)) + "\n\n"

		if !exists {
			content += "Fetching data..."
		} else if status.Error != nil {
			content += errorStyle.Render(fmt.Sprintf("Error:\n%v", status.Error))
		} else {
			content += fmt.Sprintf("Version: %s\n", status.Version)
			content += fmt.Sprintf("Last Update: %s\n\n", status.LastUpdate.Format("15:04:05"))

			nodeStr := fmt.Sprintf("Nodes: %d / %d Ready", status.NodesReady, status.NodesTotal)
			if status.NodesReady < status.NodesTotal {
				content += warnStyle.Render(nodeStr) + "\n"
			} else {
				content += okStyle.Render(nodeStr) + "\n"
			}

			if status.CpuCapacity > 0 {
				cpuPct := float64(status.CpuUsage) / float64(status.CpuCapacity) * 100
				cpuStr := fmt.Sprintf("CPU:   %.1f%% (%d/%dm)", cpuPct, status.CpuUsage, status.CpuCapacity)
				if cpuPct > 90 {
					content += errorStyle.Render(cpuStr) + "\n"
				} else if cpuPct > 75 {
					content += warnStyle.Render(cpuStr) + "\n"
				} else {
					content += okStyle.Render(cpuStr) + "\n"
				}
			}
			if status.MemCapacity > 0 {
				memPct := float64(status.MemUsage) / float64(status.MemCapacity) * 100
				memUsageGi := float64(status.MemUsage) / (1024 * 1024 * 1024)
				memCapGi := float64(status.MemCapacity) / (1024 * 1024 * 1024)
				memStr := fmt.Sprintf("Mem:   %.1f%% (%.1f/%.1fGi)", memPct, memUsageGi, memCapGi)
				if memPct > 90 {
					content += errorStyle.Render(memStr) + "\n"
				} else if memPct > 75 {
					content += warnStyle.Render(memStr) + "\n"
				} else {
					content += okStyle.Render(memStr) + "\n"
				}
			}

			content += "\n" + titleStyle.Render("Pods") + "\n"
			content += fmt.Sprintf("Total:   %d\n", status.PodsTotal)

			if status.PodsRunning > 0 {
				content += okStyle.Render(fmt.Sprintf("Running: %d", status.PodsRunning)) + "\n"
			} else {
				content += fmt.Sprintf("Running: %d\n", status.PodsRunning)
			}

			if status.PodsPending > 0 {
				content += warnStyle.Render(fmt.Sprintf("Pending: %d", status.PodsPending)) + "\n"
			} else {
				content += fmt.Sprintf("Pending: %d\n", status.PodsPending)
			}

			if status.PodsFailed > 0 {
				content += errorStyle.Render(fmt.Sprintf("Failed/CrashLoop: %d", status.PodsFailed)) + "\n"
			} else {
				content += fmt.Sprintf("Failed:  %d\n", status.PodsFailed)
			}

			if a.showLogs {
				content += "\n" + titleStyle.Render("Recent Logs")
				if a.logsOnlyErrors {
					content += titleStyle.Render(" (Errors Only)")
				}
				content += "\n"

				var renderedLogs []string
				for _, log := range status.RecentLogs {
					if a.logsOnlyErrors && !log.IsError {
						continue
					}
					
					logStr := fmt.Sprintf("[%s] %s", log.PodName, log.Message)
					// Truncate to avoid wrapping breaking the layout too badly
					maxLen := panelWidth - 4
					if len(logStr) > maxLen && maxLen > 0 {
						logStr = logStr[:maxLen-3] + "..."
					}
					
					if log.IsError {
						renderedLogs = append(renderedLogs, errorStyle.Render(logStr))
					} else {
						renderedLogs = append(renderedLogs, lipgloss.NewStyle().Foreground(lipgloss.Color("246")).Render(logStr))
					}
				}

				if len(renderedLogs) == 0 {
					content += "No matching logs found.\n"
				} else {
					usedLines := strings.Count(content, "\n")
					availableLines := (a.height - 4) - usedLines
					
					if availableLines > 0 {
						if len(renderedLogs) > availableLines {
							renderedLogs = renderedLogs[len(renderedLogs)-availableLines:]
						}
						for _, rl := range renderedLogs {
							content += rl + "\n"
						}
					}
				}
			}
		}

		panels = append(panels, panelStyle.Render(content))
	}

	body := lipgloss.JoinHorizontal(lipgloss.Top, panels...)

	return lipgloss.JoinVertical(lipgloss.Left, header, body)
}

func (a *App) View() string {
	if !a.ready {
		return "Initializing...\n"
	}
	if a.state == stateSelection {
		return a.viewSelection()
	}
	if a.state == stateLogSelection {
		return a.viewLogSelection()
	}
	if a.state == stateLogKeyParse {
		return a.viewLogKeyParse()
	}
	return a.viewDashboard()
}
