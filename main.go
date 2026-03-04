package main

import (
	"fmt"
	"io"
	"os"

	"k10s/cmd"
	"k8s.io/klog/v2"
)

func main() {
	// Save the original stderr so we can still print fatal startup errors
	originalStderr := os.Stderr

	// Redirect os.Stderr to /dev/null to prevent client-go exec plugins
	// (like gke-gcloud-auth-plugin) from writing directly to the terminal
	// and breaking the Bubble Tea UI.
	if devNull, err := os.Open(os.DevNull); err == nil {
		os.Stderr = devNull
	}

	// Silence klog output to prevent client-go background errors from destroying the TUI
	klog.SetOutput(io.Discard)
	klog.LogToStderr(false)

	if err := cmd.Execute(); err != nil {
		fmt.Fprintln(originalStderr, err)
		os.Exit(1)
	}
}
