package ui

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"k8s.io/client-go/util/homedir"

	"github.com/mpiorowski/k10s/pkg/config"
	"github.com/mpiorowski/k10s/pkg/k8s"
)

type viewState int

const (
	stateDashboard viewState = iota
	stateSelection
	stateLogSelection
	stateInfo
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
	logsOnlyWarns  bool
	wrapLogs       bool

	// Selection state
	allContexts []string
	cursor      int
	selected    map[string]struct{}

	// Log Selection state
	allDeployments      []string
	logCursor           int
	selectedDeployments map[string]struct{}
	activeLogFilters    []string
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
		width:               80,
		height:              24,
		logsOnlyErrors:      true,
		logsOnlyWarns:       true,
	}

	// Load previously saved log configuration
	cfg, err := config.LoadConfig()
	if err == nil {
		app.showLogs = cfg.ShowLogs
		app.logsOnlyErrors = cfg.LogsOnlyErrors
		app.logsOnlyWarns = cfg.LogsOnlyWarns
		app.wrapLogs = cfg.WrapLogs
		app.activeLogFilters = cfg.SelectedLogFilters
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

func fetchClusterStatus(manager *k8s.ClientManager, ctxName string, logFilters []string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		status := manager.FetchStatus(ctx, ctxName, logFilters)
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
		ShowLogs:           a.showLogs,
		LogsOnlyErrors:     a.logsOnlyErrors,
		LogsOnlyWarns:      a.logsOnlyWarns,
		WrapLogs:           a.wrapLogs,
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
			cmds = append(cmds, fetchClusterStatus(a.manager, ctx, a.activeLogFilters))
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
	case "w":
		a.logsOnlyWarns = !a.logsOnlyWarns
		a.saveConfig()
		return a, nil
	case "r":
		a.wrapLogs = !a.wrapLogs
		a.saveConfig()
		return a, nil
	case "i":
		a.state = stateInfo
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
		} else if a.state == stateInfo {
			if msg.String() == "i" || msg.String() == "esc" || msg.String() == "enter" {
				a.state = stateDashboard
			}
			return a, nil
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
			cmds = append(cmds, fetchClusterStatus(a.manager, ctx, a.activeLogFilters))
		}
		return a, tea.Batch(cmds...)

	case statusMsg:
		a.statuses[msg.ctxName] = msg.status
		return a, nil

	case tickMsg:
		var cmds []tea.Cmd
		if a.state == stateDashboard {
			for _, ctx := range a.contexts {
				cmds = append(cmds, fetchClusterStatus(a.manager, ctx, a.activeLogFilters))
			}
		}
		cmds = append(cmds, tick())
		return a, tea.Batch(cmds...)
	}

	return a, nil
}

func paginateRows(rows []string, cursor int, maxHeight int) []string {
	if len(rows) <= maxHeight || maxHeight <= 0 {
		return rows
	}

	startIdx := cursor - maxHeight/2
	if startIdx < 0 {
		startIdx = 0
	}
	if startIdx+maxHeight > len(rows) {
		startIdx = len(rows) - maxHeight
	}

	visibleRows := make([]string, maxHeight)
	copy(visibleRows, rows[startIdx:startIdx+maxHeight])

	if startIdx > 0 {
		visibleRows[0] = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  ↑ more...")
	}
	if startIdx+maxHeight < len(rows) {
		visibleRows[len(visibleRows)-1] = lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("  ↓ more...")
	}

	return visibleRows
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

	visibleRows := paginateRows(rows, a.cursor, a.height-8)
	body := lipgloss.NewStyle().PaddingLeft(2).Render(lipgloss.JoinVertical(lipgloss.Left, visibleRows...))
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

	visibleRows := paginateRows(rows, a.logCursor, a.height-8)
	body := lipgloss.NewStyle().PaddingLeft(2).Render(lipgloss.JoinVertical(lipgloss.Left, visibleRows...))
	return fmt.Sprintf("\n  %s\n  %s\n\n%s\n", header, subHeader, body)
}

func (a *App) viewInfo() string {
	header := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86")).Render("k10s Legend & Help")
	subHeader := lipgloss.NewStyle().Foreground(lipgloss.Color("241")).Render("Press 'i', 'ESC', or 'ENTER' to return to dashboard.")

	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	keyStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("212"))
	descStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("250"))

	sections := []string{
		fmt.Sprintf("%s\n%s %s\n%s %s\n%s %s",
			titleStyle.Render("Workload Metrics:"),
			keyStyle.Render("Deps:"), descStyle.Render("Deployments (Ready / Total)"),
			keyStyle.Render("STS: "), descStyle.Render("StatefulSets (Ready / Total)"),
			keyStyle.Render("Pods:"), descStyle.Render("Total Pods (R:Running P:Pending F:Failed)"),
		),
		fmt.Sprintf("%s\n%s %s\n%s %s\n%s %s",
			titleStyle.Render("Diagnostic Alerts:"),
			keyStyle.Render("OOMKilled:"), descStyle.Render("Pods killed due to memory limits (Red alert)"),
			keyStyle.Render("Restarts: "), descStyle.Render("Total container restarts (Yellow alert)"),
			keyStyle.Render("Warnings: "), descStyle.Render("Recent cluster-level error events (last 1hr)"),
		),
		fmt.Sprintf("%s\n%s %s\n%s %s\n%s %s\n%s %s\n%s %s",
			titleStyle.Render("Keyboard Shortcuts:"),
			keyStyle.Render("1-9:"), descStyle.Render("Focus (full-screen) a specific cluster"),
			keyStyle.Render("l:  "), descStyle.Render("Toggle logs / Select deployment filters"),
			keyStyle.Render("e:  "), descStyle.Render("Toggle 'Errors Only' log filter"),
			keyStyle.Render("w:  "), descStyle.Render("Toggle 'Warns Only' log filter"),
			keyStyle.Render("r:  "), descStyle.Render("Toggle log line wrapping (vs truncation)"),
		),
	}

	body := lipgloss.NewStyle().PaddingLeft(2).Render(lipgloss.JoinVertical(lipgloss.Left, sections...))
	return fmt.Sprintf("\n  %s\n  %s\n\n%s\n", header, subHeader, body)
}

func (a *App) viewDashboard() string {
	if a.initErr != nil {
		return fmt.Sprintf("Error initializing K8s clients:\n%v\n\nPress 'q' to quit", a.initErr)
	}

	headerStr := fmt.Sprintf("Monitoring %d Clusters | 1-%d focus | 's' cls | 'l' logs | 'e' err | 'w' warn | 'r' wrap | 'i' info | 'q' quit", len(a.contexts), len(a.contexts))
	if a.focusedIdx != -1 {
		headerStr = fmt.Sprintf("Focused on: %s | %d un-focus | 's' cls | 'l' logs | 'e' err | 'w' warn | 'r' wrap | 'i' info | 'q' quit", a.contexts[a.focusedIdx], a.focusedIdx+1)
	}
	header := lipgloss.NewStyle().Bold(true).Padding(0, 1).Background(lipgloss.Color("62")).Foreground(lipgloss.Color("230")).Render(headerStr)

	var contextsToRender []string
	if a.focusedIdx != -1 {
		contextsToRender = []string{a.contexts[a.focusedIdx]}
	} else {
		contextsToRender = a.contexts
	}

	numContexts := len(contextsToRender)
	cols := int(math.Ceil(math.Sqrt(float64(numContexts))))
	if cols == 0 {
		cols = 1
	}
	rows := int(math.Ceil(float64(numContexts) / float64(cols)))
	if rows == 0 {
		rows = 1
	}

	panelWidth := a.width / cols
	if panelWidth < 20 {
		panelWidth = 20
	}
	// Account for the single header line
	availableHeight := a.height - 1
	panelHeight := availableHeight / rows
	if panelHeight < 15 {
		panelHeight = 15
	}
	panelStyle := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(lipgloss.Color("62")).
		Width(panelWidth - 2). // Account for outer borders
		Height(panelHeight - 2) // Account for outer borders

	errorStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("9"))
	okStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("10"))
	warnStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("11"))
	titleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("86"))
	sectionTitleStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("62"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("241"))

	innerAvailableHeight := panelHeight - 2
	healthHeight := 5
	warningsHeight := 5
	// Subtract 2 to account for the two sectionBorderStyle bottom borders
	logsHeight := innerAvailableHeight - healthHeight - warningsHeight - 2
	if logsHeight < 2 {
		logsHeight = 2
	}
	sectionBorderStyle := lipgloss.NewStyle().
		Border(lipgloss.NormalBorder(), false, false, true, false).
		BorderForeground(lipgloss.Color("238")).
		Width(panelWidth - 4) // Inner width

	truncateStr := func(s string, max int) string {
		s = strings.ReplaceAll(s, "\n", " ")
		s = strings.ReplaceAll(s, "\r", "")
		runes := []rune(s)
		if len(runes) > max {
			if max > 3 {
				return string(runes[:max-3]) + "..."
			}
			return string(runes[:max])
		}
		return string(runes)
	}

	wrapStr := func(s string, max int) []string {
		s = strings.ReplaceAll(s, "\n", " ")
		s = strings.ReplaceAll(s, "\r", "")
		if max <= 0 {
			return []string{s}
		}
		runes := []rune(s)
		if len(runes) <= max {
			return []string{string(runes)}
		}
		var lines []string
		for len(runes) > max {
			lines = append(lines, string(runes[:max]))
			runes = runes[max:]
		}
		if len(runes) > 0 {
			lines = append(lines, string(runes))
		}
		return lines
	}

	var gridRows []string
	
	for r := 0; r < rows; r++ {
		var rowPanels []string
		for c := 0; c < cols; c++ {
			idx := r*cols + c
			if idx >= numContexts {
				rowPanels = append(rowPanels, panelStyle.Render(""))
				continue
			}

			ctx := contextsToRender[idx]
			status, exists := a.statuses[ctx]

			shortcutIdx := idx
			if a.focusedIdx != -1 {
				shortcutIdx = a.focusedIdx
			}

			var content string
			clusterTitle := titleStyle.Render(fmt.Sprintf("[%d] Cluster: %s", shortcutIdx+1, ctx))

			if !exists {
				content = clusterTitle + "\n\nFetching data..."
				rowPanels = append(rowPanels, panelStyle.Render(content))
				continue
			}
			
			if status.Error != nil {
				errStr := fmt.Sprintf("Error:\n%v", status.Error)
				content = clusterTitle + "\n\n" + errorStyle.Render(errStr)
				rowPanels = append(rowPanels, panelStyle.Render(content))
				continue
			}

			// --- Section 1: Health ---
			var healthLines []string
			
			// Apply title style to the cluster part if we want, but for simplicity let's just use the original with truncate
			clusterTitleTrimmed := truncateStr(fmt.Sprintf("[%d] Cluster: %s", shortcutIdx+1, ctx), panelWidth-20)
			if len(clusterTitleTrimmed) < 1 { clusterTitleTrimmed = "Cluster" }
			healthLines = append(healthLines, titleStyle.Render(clusterTitleTrimmed)+fmt.Sprintf(" | Ver: %s", status.Version))
			
			nodeStr := fmt.Sprintf("Nodes: %d/%d", status.NodesReady, status.NodesTotal)
			if status.NodesReady < status.NodesTotal { nodeStr = warnStyle.Render(nodeStr) } else { nodeStr = okStyle.Render(nodeStr) }
			
			resStr := ""
			if status.CpuCapacity > 0 {
				cpuPct := float64(status.CpuUsage) / float64(status.CpuCapacity) * 100
				cpuStr := fmt.Sprintf("CPU: %.0f%%", cpuPct)
				if cpuPct > 90 { cpuStr = errorStyle.Render(cpuStr) } else if cpuPct > 75 { cpuStr = warnStyle.Render(cpuStr) } else { cpuStr = okStyle.Render(cpuStr) }
				resStr += " | " + cpuStr
			}
			if status.MemCapacity > 0 {
				memPct := float64(status.MemUsage) / float64(status.MemCapacity) * 100
				memStr := fmt.Sprintf("Mem: %.0f%%", memPct)
				if memPct > 90 { memStr = errorStyle.Render(memStr) } else if memPct > 75 { memStr = warnStyle.Render(memStr) } else { memStr = okStyle.Render(memStr) }
				resStr += " | " + memStr
			}
			healthLines = append(healthLines, truncateStr(nodeStr+resStr, panelWidth-4))

			depStr := fmt.Sprintf("Deps: %d/%d", status.DeploymentsReady, status.DeploymentsTotal)
			if status.DeploymentsReady < status.DeploymentsTotal { depStr = warnStyle.Render(depStr) } else { depStr = okStyle.Render(depStr) }
			stsStr := fmt.Sprintf("STS: %d/%d", status.StatefulSetsReady, status.StatefulSetsTotal)
			if status.StatefulSetsReady < status.StatefulSetsTotal { stsStr = warnStyle.Render(stsStr) } else { stsStr = okStyle.Render(stsStr) }
			healthLines = append(healthLines, truncateStr(depStr+" | "+stsStr, panelWidth-4))

			podStr := fmt.Sprintf("Pods: %d (", status.PodsTotal)
			if status.PodsRunning > 0 { podStr += okStyle.Render(fmt.Sprintf("R:%d ", status.PodsRunning)) } else { podStr += fmt.Sprintf("R:%d ", status.PodsRunning) }
			if status.PodsPending > 0 { podStr += warnStyle.Render(fmt.Sprintf("P:%d ", status.PodsPending)) } else { podStr += fmt.Sprintf("P:%d ", status.PodsPending) }
			if status.PodsFailed > 0 { podStr += errorStyle.Render(fmt.Sprintf("F:%d", status.PodsFailed)) } else { podStr += fmt.Sprintf("F:%d", status.PodsFailed) }
			podStr += ")"
			healthLines = append(healthLines, truncateStr(podStr, panelWidth-4))

			oomStr := fmt.Sprintf("OOM: %d", status.PodsOOMKilled)
			if status.PodsOOMKilled > 0 { oomStr = errorStyle.Render(oomStr) }
			resCountStr := fmt.Sprintf("Restarts: %d", status.RestartsTotal)
			if status.RestartsTotal > 0 { resCountStr = warnStyle.Render(resCountStr) }
			healthLines = append(healthLines, truncateStr(oomStr+" | "+resCountStr, panelWidth-4))

			for len(healthLines) < healthHeight { healthLines = append(healthLines, "") }
			healthBlock := lipgloss.JoinVertical(lipgloss.Left, healthLines[:healthHeight]...)

			// --- Section 2: Active Warnings ---
			var warnLines []string
			warnLines = append(warnLines, sectionTitleStyle.Render("Active Warnings"))
			hasWarnings := false
			if status.DeploymentsReady < status.DeploymentsTotal {
				for _, d := range status.DeploymentsDegraded {
					msg := truncateStr("! Dep Degraded: "+d, panelWidth-4)
					warnLines = append(warnLines, errorStyle.Render(msg))
					hasWarnings = true
				}
			}
			if status.StatefulSetsReady < status.StatefulSetsTotal {
				for _, s := range status.StatefulSetsDegraded {
					msg := truncateStr("! STS Degraded: "+s, panelWidth-4)
					warnLines = append(warnLines, errorStyle.Render(msg))
					hasWarnings = true
				}
			}
			for _, w := range status.WarningEvents {
				msg := truncateStr("Event: "+w, panelWidth-4)
				warnLines = append(warnLines, warnStyle.Render(msg))
				hasWarnings = true
			}
			
			if !hasWarnings {
				warnLines = append(warnLines, okStyle.Render(truncateStr("✅ No active cluster warnings", panelWidth-4)))
			}

			for len(warnLines) < warningsHeight { warnLines = append(warnLines, "") }
			warnBlock := lipgloss.JoinVertical(lipgloss.Left, warnLines[:warningsHeight]...)

			// --- Section 3: Critical Logs ---
			var logLines []string
			logTitle := "Recent Logs"
			if a.logsOnlyErrors && a.logsOnlyWarns { logTitle += " (Err/Warn)" } else if a.logsOnlyErrors { logTitle += " (Err)" } else if a.logsOnlyWarns { logTitle += " (Warn)" }
			logLines = append(logLines, sectionTitleStyle.Render(logTitle))

			hasLogs := false
			if a.showLogs {
				// Filter logs first
				var filteredLogs []k8s.LogEntry
				for _, log := range status.RecentLogs {
					if a.logsOnlyErrors && !a.logsOnlyWarns && !log.IsError { continue }
					if a.logsOnlyWarns && !a.logsOnlyErrors && !log.IsWarn { continue }
					if a.logsOnlyErrors && a.logsOnlyWarns && !log.IsError && !log.IsWarn { continue }
					filteredLogs = append(filteredLogs, log)
					hasLogs = true
				}

				if !hasLogs {
					logLines = append(logLines, okStyle.Render(truncateStr("✅ No matching logs found", panelWidth-4)))
				} else {
					availableLogLines := logsHeight - 1
					var renderedLogs []string

					if a.wrapLogs {
						// Iterate newest-first so wrapped lines stay in correct order
						for i := len(filteredLogs) - 1; i >= 0 && len(renderedLogs) < availableLogLines; i-- {
							log := filteredLogs[i]
							logStr := fmt.Sprintf("[%s] %s", log.PodName, log.Message)
							var style lipgloss.Style
							if log.IsError {
								style = errorStyle
							} else if log.IsWarn {
								style = warnStyle
							} else {
								style = dimStyle
							}
							wrapped := wrapStr(logStr, panelWidth-4)
							remaining := availableLogLines - len(renderedLogs)
							if len(wrapped) > remaining {
								wrapped = wrapped[:remaining]
							}
							for _, line := range wrapped {
								renderedLogs = append(renderedLogs, style.Render(line))
							}
						}
					} else {
						for _, log := range filteredLogs {
							logStr := fmt.Sprintf("[%s] %s", log.PodName, log.Message)
							var style lipgloss.Style
							if log.IsError {
								style = errorStyle
							} else if log.IsWarn {
								style = warnStyle
							} else {
								style = dimStyle
							}
							renderedLogs = append(renderedLogs, style.Render(truncateStr(logStr, panelWidth-4)))
						}
						if len(renderedLogs) > availableLogLines {
							renderedLogs = renderedLogs[len(renderedLogs)-availableLogLines:]
						}
						for i, j := 0, len(renderedLogs)-1; i < j; i, j = i+1, j-1 {
							renderedLogs[i], renderedLogs[j] = renderedLogs[j], renderedLogs[i]
						}
					}

					if availableLogLines > 0 {
						logLines = append(logLines, renderedLogs...)
					}
				}
			} else {
				logLines = append(logLines, dimStyle.Render(truncateStr("Logs disabled. Press 'l' to select deployments.", panelWidth-4)))
			}

			for len(logLines) < logsHeight { logLines = append(logLines, "") }
			logBlock := lipgloss.JoinVertical(lipgloss.Left, logLines[:logsHeight]...)

			content = lipgloss.JoinVertical(lipgloss.Left,
				sectionBorderStyle.Render(healthBlock),
				sectionBorderStyle.Render(warnBlock),
				logBlock,
			)

			rowPanels = append(rowPanels, panelStyle.Render(content))
		}
		
		gridRows = append(gridRows, lipgloss.JoinHorizontal(lipgloss.Top, rowPanels...))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, gridRows...)
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
	if a.state == stateInfo {
		return a.viewInfo()
	}
	return a.viewDashboard()
}
