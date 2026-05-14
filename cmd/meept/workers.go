package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newWorkersCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "workers",
		Short: "Manage the worker pool",
		Long: `View and manage the worker pool.

Workers are goroutines that process jobs from the queue. Each worker
can have specific capabilities that determine which jobs it can handle.

Examples:
  meept workers              # Show worker pool status
  meept workers list         # List all workers with details
  meept workers scale 8      # Scale to 8 workers`,
		RunE: runWorkersStatus,
	}

	cmd.AddCommand(newWorkersListCmd())
	cmd.AddCommand(newWorkersStatusCmd())
	cmd.AddCommand(newWorkersScaleCmd())

	return cmd
}

func newWorkersStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdStatus,
		Short: "Show worker pool status",
		RunE:  runWorkersStatus,
	}
}

func runWorkersStatus(cmd *cobra.Command, args []string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	stats, err := client.GetWorkerPoolStats()
	if err != nil {
		return fmt.Errorf("failed to get worker stats: %w", err)
	}

	fmt.Println("Worker Pool Status")
	fmt.Println("==================")
	fmt.Println()
	fmt.Printf("Total Workers:  %d\n", stats.TotalWorkers)
	fmt.Printf("Idle:           %d\n", stats.IdleWorkers)
	fmt.Printf("Busy:           %d\n", stats.BusyWorkers)
	fmt.Printf("Error:          %d\n", stats.ErrorWorkers)

	return nil
}

func newWorkersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdList,
		Short: "List all workers with details",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			resp, err := client.ListPoolWorkers()
			if err != nil {
				return fmt.Errorf("failed to list workers: %w", err)
			}

			if len(resp.Workers) == 0 {
				fmt.Println("No workers running")
				return nil
			}

			// Print header
			fmt.Printf("%-30s %-12s %-25s %-6s %-6s %-20s\n", "ID", "STATE", "CAPABILITIES", "DONE", "FAIL", "CURRENT JOB")
			fmt.Printf("%-30s %-12s %-25s %-6s %-6s %-20s\n", strings.Repeat("-", 30), strings.Repeat("-", 12), strings.Repeat("-", 25), strings.Repeat("-", 6), strings.Repeat("-", 6), strings.Repeat("-", 20))

			for _, worker := range resp.Workers {
				caps := strings.Join(worker.Capabilities, ", ")
				if len(caps) > 25 {
					caps = caps[:22] + "..."
				}
				if caps == "" {
					caps = "-"
				}

				jobID := worker.CurrentJobID
				if jobID == "" {
					jobID = "-"
				}
				if len(jobID) > 20 {
					jobID = jobID[:17] + "..."
				}

				fmt.Printf("%-30s %-12s %-25s %-6d %-6d %-20s\n",
					worker.ID,
					worker.State,
					caps,
					worker.JobsComplete,
					worker.JobsFailed,
					jobID,
				)
			}

			fmt.Println()
			fmt.Printf("Total: %d workers\n", len(resp.Workers))

			return nil
		},
	}
}

func newWorkersScaleCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "scale <count>",
		Short: "Scale the worker pool to the specified size",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var targetCount int
			if _, err := fmt.Sscanf(args[0], "%d", &targetCount); err != nil {
				return fmt.Errorf("invalid count: %s", args[0])
			}

			if targetCount < 0 {
				return fmt.Errorf("count must be non-negative")
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			if err := client.ScaleWorkerPool(targetCount); err != nil {
				return fmt.Errorf("failed to scale worker pool: %w", err)
			}

			fmt.Printf("Worker pool scaling to %d workers\n", targetCount)
			return nil
		},
	}
}
