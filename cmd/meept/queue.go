package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui"
)

func newQueueCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "queue",
		Short: "Manage the job queue",
		Long: `View and manage jobs in the persistent job queue.

The queue holds jobs waiting to be processed by the worker pool.
Jobs can be one-off tasks or part of a larger project task.

Examples:
  meept queue status              # Show queue statistics
  meept queue list                # List pending jobs
  meept queue list --state=failed # List failed jobs
  meept queue retry <job-id>      # Retry a failed job`,
	}

	cmd.AddCommand(newQueueStatusCmd())
	cmd.AddCommand(newQueueListCmd())
	cmd.AddCommand(newQueueRetryCmd())

	return cmd
}

func newQueueStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show queue statistics",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := tui.NewRPCClient(getSocketPath())
			if err := client.Connect(); err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			stats, err := client.GetQueueStats()
			if err != nil {
				return fmt.Errorf("failed to get queue stats: %w", err)
			}

			fmt.Println("Queue Statistics")
			fmt.Println("================")
			fmt.Println()

			fmt.Println("By State:")
			if len(stats.ByState) == 0 {
				fmt.Println("  (no jobs)")
			} else {
				for state, count := range stats.ByState {
					fmt.Printf("  %-12s %d\n", state+":", count)
				}
			}

			fmt.Println()
			fmt.Println("By Priority (pending):")
			if len(stats.ByPriority) == 0 {
				fmt.Println("  (none)")
			} else {
				priorityNames := map[string]string{
					"1": "low",
					"2": "normal",
					"3": "high",
					"4": "urgent",
				}
				for priority, count := range stats.ByPriority {
					name := priorityNames[priority]
					if name == "" {
						name = priority
					}
					fmt.Printf("  %-12s %d\n", name+":", count)
				}
			}

			fmt.Println()
			fmt.Printf("Dead Letter:   %d\n", stats.DeadCount)

			return nil
		},
	}
}

func newQueueListCmd() *cobra.Command {
	var state string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List jobs in the queue",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := tui.NewRPCClient(getSocketPath())
			if err := client.Connect(); err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			resp, err := client.ListQueueJobs(state, limit)
			if err != nil {
				return fmt.Errorf("failed to list jobs: %w", err)
			}

			if len(resp.Jobs) == 0 {
				if state != "" {
					fmt.Printf("No jobs with state '%s'\n", state)
				} else {
					fmt.Println("No jobs in queue")
				}
				return nil
			}

			// Print header
			fmt.Printf("%-45s %-10s %-10s %-10s %-20s\n", "ID", "TYPE", "PRIORITY", "STATE", "TASK")
			fmt.Printf("%-45s %-10s %-10s %-10s %-20s\n", strings.Repeat("-", 45), strings.Repeat("-", 10), strings.Repeat("-", 10), strings.Repeat("-", 10), strings.Repeat("-", 20))

			priorityNames := map[int]string{
				1: "low",
				2: "normal",
				3: "high",
				4: "urgent",
			}

			for _, job := range resp.Jobs {
				priorityName := priorityNames[job.Priority]
				if priorityName == "" {
					priorityName = fmt.Sprintf("%d", job.Priority)
				}

				taskID := job.TaskID
				if taskID == "" {
					taskID = "-"
				}
				if len(taskID) > 20 {
					taskID = taskID[:17] + "..."
				}

				fmt.Printf("%-45s %-10s %-10s %-10s %-20s\n", job.ID, job.Type, priorityName, job.State, taskID)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "pending", "Filter by state (pending, claimed, processing, completed, failed, dead)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of jobs to return")

	return cmd
}

func newQueueRetryCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "retry <job-id>",
		Short: "Retry a failed job",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			jobID := args[0]

			client := tui.NewRPCClient(getSocketPath())
			if err := client.Connect(); err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			if err := client.RetryQueueJob(jobID); err != nil {
				return fmt.Errorf("failed to retry job: %w", err)
			}

			fmt.Printf("Job %s queued for retry\n", jobID)
			return nil
		},
	}
}
