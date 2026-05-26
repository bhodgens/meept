package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui/types"
)

func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   cmdStatus,
		Short: "Show daemon status",
		Long: `Display the current status of the Meept daemon.

Shows:
  - Running state and PID
  - Uptime
  - Active model
  - Token/budget usage
  - Registered RPC methods`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(jsonOutput)
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output in JSON format")

	return cmd
}

func runStatus(jsonOutput bool) error {
	// First check if daemon is running via PID file
	pidFile := stateDir + "/meept.pid"
	pidData, err := os.ReadFile(pidFile)
	if os.IsNotExist(err) {
		fmt.Println("Daemon is not running")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		// Invalid PID file
		fmt.Println("Daemon is not running (invalid PID file)")
		return nil
	}

	// Try to connect and get detailed status
	client, err := connectDaemon()
	if err != nil {
		// Daemon PID exists but can't connect
		fmt.Printf("Daemon process exists (PID %d) but not responding\n", pid)
		fmt.Println("Try restarting: meept daemon restart")
		return nil
	}
	defer client.Close()

	status, err := client.Status()
	if err != nil {
		fmt.Printf("Daemon is running (PID %d) but status unavailable: %v\n", pid, err)
		return nil
	}
	if status == nil {
		fmt.Printf("Daemon is running (PID %d) but returned empty status\n", pid)
		return nil
	}

	if jsonOutput {
		printStatusJSON(status, pid)
		return nil
	}

	printStatusText(status, pid)
	return nil
}

func printStatusText(status *types.DaemonStatusResponse, pid int) {
	// Status header
	statusColor := "\033[32m" // Green
	if status.Status != "running" {
		statusColor = "\033[31m" // Red
	}
	resetColor := "\033[0m"

	fmt.Printf("Meept Daemon Status\n")
	fmt.Printf("===================\n\n")

	fmt.Printf("  Status:     %s%s%s\n", statusColor, status.Status, resetColor)
	fmt.Printf("  PID:        %d\n", pid)
	fmt.Printf("  Uptime:     %s\n", types.FormatUptime(status.UptimeSeconds))

	// Model
	model := status.Model
	if model == "" {
		model = status.DefaultModel
	}
	if model == "" {
		model = "n/a"
	}
	fmt.Printf("  LLM Model:  %s\n", model)

	fmt.Println()

	// Token usage
	fmt.Printf("Token Budget\n")
	fmt.Printf("------------\n")
	tokensUsed := status.TokensUsed
	tokensRemaining := status.TokensRemaining
	totalTokens := tokensUsed + tokensRemaining
	if totalTokens == 0 {
		totalTokens = 100000
	}

	tokenPercent := float64(tokensUsed) / float64(totalTokens) * 100
	fmt.Printf("  Used:       %d / %d (%.1f%%)\n", tokensUsed, totalTokens, tokenPercent)

	// Budget usage
	budgetUsed := status.BudgetUsed
	budgetRemaining := status.BudgetRemaining
	totalBudget := budgetUsed + budgetRemaining
	if totalBudget > 0 {
		budgetPercent := budgetUsed / totalBudget * 100
		fmt.Printf("  Cost:       $%.4f / $%.4f (%.1f%%)\n", budgetUsed, totalBudget, budgetPercent)
	}

	fmt.Println()

	// RPC info
	fmt.Printf("RPC Server\n")
	fmt.Printf("----------\n")
	fmt.Printf("  Methods:    %d registered\n", len(status.RegisteredMethods))
	fmt.Printf("  Bus Subs:   %d\n", status.BusSubscribers)
}

func printStatusJSON(status *types.DaemonStatusResponse, pid int) {
	out := map[string]any{
		"status":             status.Status,
		"pid":                pid,
		"uptime_seconds":     status.UptimeSeconds,
		"model":              status.Model,
		"tokens_used":        status.TokensUsed,
		"tokens_remaining":   status.TokensRemaining,
		"budget_used":        status.BudgetUsed,
		"budget_remaining":   status.BudgetRemaining,
		"registered_methods": len(status.RegisteredMethods),
		"bus_subscribers":    status.BusSubscribers,
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.Encode(out)
}
