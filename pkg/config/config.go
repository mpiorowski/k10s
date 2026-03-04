package config

import (
	"encoding/json"
	"os"
	"path/filepath"

	"k8s.io/client-go/util/homedir"
)

type Config struct {
	SelectedContexts   []string            `json:"selected_contexts"`
	SelectedLogFilters []string            `json:"selected_log_filters"`
	SelectedLogKeys    map[string][]string `json:"selected_log_keys"`
	ShowLogs           bool                `json:"show_logs"`
	LogsOnlyErrors     bool                `json:"logs_only_errors"`
	LogsOnlyWarns      bool                `json:"logs_only_warns"`
}

func GetConfigPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil {
		// Fallback to home dir if UserConfigDir fails
		return filepath.Join(homedir.HomeDir(), ".k10s.json")
	}
	return filepath.Join(configDir, "k10s", "config.json")
}

func LoadConfig() (Config, error) {
	var cfg Config
	path := GetConfigPath()
	b, err := os.ReadFile(path)
	if err != nil {
		return cfg, err
	}
	err = json.Unmarshal(b, &cfg)
	return cfg, err
}

func SaveConfig(cfg Config) error {
	path := GetConfigPath()
	
	// Ensure the config directory exists
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	b, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0644)
}
