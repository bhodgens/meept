package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui"
	"github.com/caimlas/meept/internal/tui/types"
)

func newStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status",
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
	socket := getSocketPath()

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
	client := tui.NewRPCClient(socket)
	if err := client.Connect(); err != nil {
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

	if jsonOutput {
		return printStatusJSON(status, pid)
	}

	return printStatusText(status, pid)
}

func printStatusText(status *types.DaemonStatusResponse, pid int) error {
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

	return nil
}

func printStatusJSON(status *types.DaemonStatusResponse, pid int) error {
	// Simple JSON output without encoding/json import cycle
	fmt.Printf(`{
  "status": "%s",
  "pid": %d,
  "uptime_seconds": %.2f,
  "model": "%s",
  "tokens_used": %d,
  "tokens_remaining": %d,
  "budget_used": %.4f,
  "budget_remaining": %.4f,
  "registered_methods": %d,
  "bus_subscribers": %d
}
`,
		status.Status,
		pid,
		status.UptimeSeconds,
		status.Model,
		status.TokensUsed,
		status.TokensRemaining,
		status.BudgetUsed,
		status.BudgetRemaining,
		len(status.RegisteredMethods),
		status.BusSubscribers,
	)
	return nil
}
