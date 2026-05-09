package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

func newTaskCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "task",
		Short: "Manage background tasks",
		Long: `Manage background tasks for multi-agent orchestration.

Tasks are units of work that can spawn multiple jobs. They have their own
isolated workspace, git tracking, and memvid zone.

Examples:
  meept task list                    # List all tasks
  meept task create "API refactor"   # Create a new task
  meept task get <task-id>           # Get task details
  meept task delete <task-id>        # Delete a task`,
	}

	cmd.AddCommand(newTaskListCmd())
	cmd.AddCommand(newTaskCreateCmd())
	cmd.AddCommand(newTaskGetCmd())
	cmd.AddCommand(newTaskDeleteCmd())
	cmd.AddCommand(newTaskLinkCmd())
	cmd.AddCommand(newTaskUnlinkCmd())

	return cmd
}

func newTaskListCmd() *cobra.Command {
	var state string
	var limit int

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List tasks",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			resp, err := client.ListTasks(state, limit)
			if err != nil {
				return fmt.Errorf("failed to list tasks: %w", err)
			}

			if len(resp.Tasks) == 0 {
				fmt.Println("No tasks found")
				return nil
			}

			// Print header
			fmt.Printf("%-40s %-15s %-10s %-10s %-20s\n", "ID", "NAME", "STATE", "PROGRESS", "UPDATED")
			fmt.Printf("%-40s %-15s %-10s %-10s %-20s\n", strings.Repeat("-", 40), strings.Repeat("-", 15), strings.Repeat("-", 10), strings.Repeat("-", 10), strings.Repeat("-", 20))

			for _, task := range resp.Tasks {
				name := task.Name
				if len(name) > 15 {
					name = name[:12] + "..."
				}

				progress := fmt.Sprintf("%.0f%%", task.Progress())
				if task.TotalJobs > 0 {
					progress = fmt.Sprintf("%d/%d", task.CompletedJobs, task.TotalJobs)
				}

				updated := task.UpdatedAt
				if len(updated) > 20 {
					updated = updated[:20]
				}

				fmt.Printf("%-40s %-15s %-10s %-10s %-20s\n", task.ID, name, task.State, progress, updated)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&state, "state", "s", "", "Filter by state (pending, executing, completed, etc.)")
	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of tasks to return")

	return cmd
}

func newTaskCreateCmd() *cobra.Command {
	var description string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			task, err := client.CreateTask(name, description)
			if err != nil {
				return fmt.Errorf("failed to create task: %w", err)
			}

			fmt.Printf("Created task: %s\n", task.ID)
			fmt.Printf("Name: %s\n", task.Name)
			if task.Description != "" {
				fmt.Printf("Description: %s\n", task.Description)
			}
			fmt.Printf("State: %s\n", task.State)

			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Task description")

	return cmd
}

func newTaskGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <task-id>",
		Short: "Get task details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			task, err := client.GetTask(taskID)
			if err != nil {
				return fmt.Errorf("failed to get task: %w", err)
			}

			fmt.Printf("ID:          %s\n", task.ID)
			fmt.Printf("Name:        %s\n", task.Name)
			fmt.Printf("State:       %s\n", task.State)
			if task.Description != "" {
				fmt.Printf("Description: %s\n", task.Description)
			}
			if task.ProjectDir != "" {
				fmt.Printf("Project:     %s\n", task.ProjectDir)
			}
			if task.WorkspaceDir != "" {
				fmt.Printf("Workspace:   %s\n", task.WorkspaceDir)
			}
			if task.GitRepo != "" {
				fmt.Printf("Git:         %s\n", task.GitRepo)
			}
			if task.MemvidZone != "" {
				fmt.Printf("Memvid:      %s\n", task.MemvidZone)
			}
			fmt.Printf("Jobs:        %d/%d completed, %d failed\n", task.CompletedJobs, task.TotalJobs, task.FailedJobs)
			fmt.Printf("Progress:    %.0f%%\n", task.Progress())
			if len(task.LinkedSessions) > 0 {
				fmt.Printf("Sessions:    %s\n", strings.Join(task.LinkedSessions, ", "))
			}
			fmt.Printf("Created:     %s\n", task.CreatedAt)
			fmt.Printf("Updated:     %s\n", task.UpdatedAt)

			return nil
		},
	}
}

func newTaskDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <task-id>",
		Short: "Delete a task",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			if err := client.DeleteTask(taskID); err != nil {
				return fmt.Errorf("failed to delete task: %w", err)
			}

			fmt.Printf("Deleted task: %s\n", taskID)
			return nil
		},
	}
}

func newTaskLinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "link <task-id> <session-id>",
		Short: "Link a session to a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			sessionID := args[1]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			if err := client.LinkTaskSession(taskID, sessionID); err != nil {
				return fmt.Errorf("failed to link session: %w", err)
			}

			fmt.Printf("Linked session %s to task %s\n", sessionID, taskID)
			return nil
		},
	}
}

func newTaskUnlinkCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "unlink <task-id> <session-id>",
		Short: "Remove a session link from a task",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			sessionID := args[1]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			if err := client.UnlinkTaskSession(taskID, sessionID); err != nil {
				return fmt.Errorf("failed to unlink session: %w", err)
			}

			fmt.Printf("Unlinked session %s from task %s\n", sessionID, taskID)
			return nil
		},
	}
}
