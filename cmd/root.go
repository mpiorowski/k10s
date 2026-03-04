package cmd

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"
	"github.com/mpiorowski/k10s/pkg/config"
	"github.com/mpiorowski/k10s/ui"
)

var contextsFlag string

var rootCmd = &cobra.Command{
	Use:   "k10s",
	Short: "A multi-cluster Kubernetes TUI dashboard",
	RunE: func(cmd *cobra.Command, args []string) error {
		var contexts []string
		if contextsFlag != "" {
			contexts = strings.Split(contextsFlag, ",")
			for i, c := range contexts {
				contexts[i] = strings.TrimSpace(c)
			}
		} else {
			cfg, err := config.LoadConfig()
			if err == nil && len(cfg.SelectedContexts) > 0 {
				contexts = cfg.SelectedContexts
			}
		}

		// Initialize our Bubble Tea application with the provided contexts
		p := tea.NewProgram(ui.NewApp(contexts), tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			return fmt.Errorf("failed to run TUI: %w", err)
		}

		return nil
	},
}

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.Flags().StringVarP(&contextsFlag, "contexts", "c", "", "Comma-separated list of kubeconfig contexts (e.g., dev-cluster,prod-cluster)")
}
