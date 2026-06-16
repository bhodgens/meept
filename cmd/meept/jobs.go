package main

import (
	"fmt"

	"github.com/spf13/cobra"
)

func newJobsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "jobs",
		Aliases: []string{"tasks"},
		Short:   "List scheduled jobs",
		Long: `List all scheduled jobs/tasks.

This shows jobs registered with the scheduler, including their
schedule, next run time, and current status.`,
		RunE: runJobs,
	}

	return cmd
}

func runJobs(cmd *cobra.Command, args []string) error {
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	resp, err := client.ListJobs()
	if err != nil {
		return fmt.Errorf("failed to list jobs: %w", err)
	}

	if len(resp.Jobs) == 0 {
		fmt.Println("no scheduled jobs")
		return nil
	}

	// Print header
	fmt.Printf("%-20s %-20s %-25s %-10s\n", "NAME", "SCHEDULE", "NEXT RUN", "STATUS")
	fmt.Printf("%-20s %-20s %-25s %-10s\n", "----", "--------", "--------", "------")

	for _, job := range resp.Jobs {
		name := job.Name
		if name == "" {
			name = job.ID
		}
		if len([]rune(name)) > 20 {
			name = string([]rune(name)[:17]) + "..."
		}

		schedule := job.Schedule
		if schedule == "" {
			schedule = job.Trigger
		}
		if schedule == "" {
			schedule = "n/a"
		}
		if len([]rune(schedule)) > 20 {
			schedule = string([]rune(schedule)[:17]) + "..."
		}

		nextRun := job.NextRunTime
		if nextRun == "" {
			nextRun = "n/a"
		}
		if len(nextRun) > 25 {
			nextRun = nextRun[:22] + "..."
		}

		status := "active"
		if job.Paused {
			status = "paused"
		}

		fmt.Printf("%-20s %-20s %-25s %-10s\n", name, schedule, nextRun, status)
	}

	return nil
}
