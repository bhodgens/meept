package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newDispatchCmd creates the dispatch command tree.
// meept dispatch <node> <agent> <task>          — submit a task
// meept dispatch status <jobID>                 — query status
// meept dispatch results <jobID>                — fetch results
func newDispatchCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "dispatch",
		Short: "dispatch tasks to cluster nodes",
		Long:  `submit tasks to remote cluster daemons, query status, and fetch results.`,
	}
	cmd.AddCommand(newDispatchSubmitCmd())
	cmd.AddCommand(newDispatchStatusCmd())
	cmd.AddCommand(newDispatchResultsCmd())
	return cmd
}

// --- dispatch submit ---

func newDispatchSubmitCmd() *cobra.Command {
	var (
		jsonOutput   bool
		priority     int
		resources    []string
		workspaceRef string
	)

	cmd := &cobra.Command{
		Use:   "submit <node> <agent> <task description>",
		Short: "submit a task to a remote cluster node",
		Long: `submit a task for execution on a specific cluster node. the task is
sent via gRPC dispatch to the target daemon which materializes the
workspace and runs the agent locally.

examples:
  meept dispatch submit node-1 coder "refactor auth module"
  meept dispatch submit node-2 analyst "review pr #42" --priority 5`,
		Args: cobra.MinimumNArgs(3),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{
				"target_node":      args[0],
				"agent_id":         args[1],
				"task_description": strings.Join(args[2:], " "),
			}
			if priority != 0 {
				params["priority"] = priority
			}
			if len(resources) > 0 {
				params["required_resources"] = resources
			}
			if workspaceRef != "" {
				var ws map[string]any
				if err := json.Unmarshal([]byte(workspaceRef), &ws); err != nil {
					return fmt.Errorf("invalid --workspace JSON: %w", err)
				}
				params["workspace"] = ws
			}

			raw, err := client.Call("dispatch.submit", params)
			if err != nil {
				return fmt.Errorf("dispatch submit failed: %w", err)
			}

			if jsonOutput {
				output, err := json.MarshalIndent(raw, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			var ack struct {
				JobID    string `json:"job_id"`
				Accepted bool   `json:"accepted"`
				Message  string `json:"message"`
			}
			if err := json.Unmarshal(raw, &ack); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if ack.Accepted {
				fmt.Printf("job submitted\n")
				fmt.Printf("  job id: %s\n", ack.JobID)
			} else {
				fmt.Printf("job rejected\n")
				if ack.Message != "" {
					fmt.Printf("  reason: %s\n", ack.Message)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")
	cmd.Flags().IntVar(&priority, "priority", 0, "task priority (higher = more urgent)")
	cmd.Flags().StringArrayVar(&resources, "resource", nil, "required resource hash (repeatable)")
	cmd.Flags().StringVar(&workspaceRef, "workspace", "", "workspace reference JSON (repo_url, commit_sha, diff_blob_hash, dirty)")

	return cmd
}

// --- dispatch status ---

func newDispatchStatusCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "status <jobID>",
		Short: "query dispatch job status",
		Long:  `display the current state of a dispatched task on a remote node.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("dispatch.status", map[string]any{
				"job_id": args[0],
			})
			if err != nil {
				return fmt.Errorf("dispatch status failed: %w", err)
			}

			if jsonOutput {
				output, err := json.MarshalIndent(raw, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			var status struct {
				JobID     string `json:"job_id"`
				State     string `json:"state"`
				StartedAt int64  `json:"started_at"`
				UpdatedAt int64  `json:"updated_at"`
				Error     string `json:"error"`
			}
			if err := json.Unmarshal(raw, &status); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			fmt.Printf("status\n")
			fmt.Printf("  job id: %s\n", status.JobID)
			fmt.Printf("  state:  %s\n", status.State)
			if status.Error != "" {
				fmt.Printf("  error:  %s\n", status.Error)
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}

// --- dispatch results ---

func newDispatchResultsCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "results <jobID>",
		Short: "fetch dispatch job results",
		Long:  `fetch completed results for a dispatched task from a remote node.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("dispatch.results", map[string]any{
				"job_id": args[0],
			})
			if err != nil {
				return fmt.Errorf("dispatch results failed: %w", err)
			}

			if jsonOutput {
				output, err := json.MarshalIndent(raw, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			var results []struct {
				JobID       string                 `json:"job_id"`
				OutputRef   string                 `json:"output_ref"`
				Workspace   map[string]interface{} `json:"workspace"`
				Error       string                 `json:"error"`
				CompletedAt int64                  `json:"completed_at"`
			}
			if err := json.Unmarshal(raw, &results); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("(no results yet)")
				return nil
			}

			fmt.Printf("results (%d)\n", len(results))
			for i, r := range results {
				fmt.Printf("  [%d] job id: %s\n", i, r.JobID)
				if r.OutputRef != "" {
					fmt.Printf("      output:  %s\n", r.OutputRef)
				}
				if r.Error != "" {
					fmt.Printf("      error:   %s\n", r.Error)
				}
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "output as JSON")

	return cmd
}
