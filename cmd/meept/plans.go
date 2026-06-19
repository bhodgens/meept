package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newPlansCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "plans",
		Short:   "manage plans",
		Long:    "List, show, approve, reject, and confirm plans.",
		Aliases: []string{"plan"},
	}

	cmd.AddCommand(newPlansListCmd())
	cmd.AddCommand(newPlansShowCmd())
	cmd.AddCommand(newPlansApproveCmd())
	cmd.AddCommand(newPlansRejectCmd())
	cmd.AddCommand(newPlansConfirmCmd())

	return cmd
}

func newPlansListCmd() *cobra.Command {
	var projectID string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list plans",
		Long:  "List all plans, optionally filtered by project.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{
				"limit": 50,
			}
			if projectID != "" {
				params["project_id"] = projectID
			}

			rawResult, err := client.Call("plan.list", params)
			if err != nil {
				return fmt.Errorf("failed to list plans: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			if outputJSON {
				output, err := json.MarshalIndent(resultMap, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			plansList, ok := resultMap["plans"].([]any)
			if !ok || len(plansList) == 0 {
				fmt.Println("No plans found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTITLE\tSTATE\tPHASES\tSTEPS\tUPDATED")

			for _, p := range plansList {
				plan, ok := p.(map[string]any)
				if !ok {
					continue
				}

				id := getStringOr(plan, "id", "")
				title := getStringOr(plan, "title", "")
				state := getStringOr(plan, "state", "")
				updated := getStringOr(plan, "updated_at", "")

				// Count phases
				phasesCount := 0
				if phases, ok := plan["phases"].([]any); ok {
					phasesCount = len(phases)
				}

				// Count total steps across all phases
				stepsCount := 0
				if phases, ok := plan["phases"].([]any); ok {
					for _, ph := range phases {
						if phMap, ok := ph.(map[string]any); ok {
							if steps, ok := phMap["steps"].([]any); ok {
								stepsCount += len(steps)
							}
						}
					}
				}

				// Truncate title
				if len([]rune(title)) > 40 {
					title = string([]rune(title)[:37]) + "..."
				}

				// Truncate updated date
				if len(updated) > 10 {
					updated = updated[:10]
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n", id, title, state, phasesCount, stepsCount, updated)
			}

			w.Flush()
			fmt.Printf("\nTotal: %d plans\n", len(plansList))
			return nil
		},
	}

	cmd.Flags().StringVar(&projectID, "project", "", "Filter by project ID")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newPlansShowCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show <id>",
		Short: "show plan details",
		Long:  "Show detailed information about a specific plan.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("plan.get", map[string]any{"id": planID})
			if err != nil {
				return fmt.Errorf("failed to get plan: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			plan := resultMap

			fmt.Printf("ID:          %s\n", getStringOr(plan, "id", ""))
			fmt.Printf("Title:       %s\n", getStringOr(plan, "title", ""))
			if desc := getStringOr(plan, "description", ""); desc != "" {
				fmt.Printf("Description: %s\n", desc)
			}
			fmt.Printf("State:       %s\n", getStringOr(plan, "state", ""))
			if fp := getStringOr(plan, "file_path", ""); fp != "" {
				fmt.Printf("File:        %s\n", fp)
			}
			if proj := getStringOr(plan, "project_id", ""); proj != "" {
				fmt.Printf("Project:     %s\n", proj)
			}
			fmt.Printf("Created:     %s\n", getStringOr(plan, "created_at", ""))
			fmt.Printf("Updated:     %s\n", getStringOr(plan, "updated_at", ""))

			// Show phases with progress
			if phases, ok := plan["phases"].([]any); ok && len(phases) > 0 {
				fmt.Printf("\nPhases (%d):\n", len(phases))
				for i, ph := range phases {
					phase, ok := ph.(map[string]any)
					if !ok {
						continue
					}

					phaseName := getStringOr(phase, "name", fmt.Sprintf("Phase %d", i+1))
					phaseState := getStringOr(phase, "state", "")
					totalSteps := 0
					completedSteps := 0

					if steps, ok := phase["steps"].([]any); ok {
						totalSteps = len(steps)
						for _, s := range steps {
							if stepMap, ok := s.(map[string]any); ok {
								if getStringOr(stepMap, "state", "") == "completed" {
									completedSteps++
								}
							}
						}
					}

					// Build progress bar
					barWidth := 20
					filled := 0
					if totalSteps > 0 {
						filled = (completedSteps * barWidth) / totalSteps
					}
					bar := ""
					for j := range barWidth {
						if j < filled {
							bar += "#"
						} else {
							bar += "-"
						}
					}

					fmt.Printf("  %d. [%s] %s  [%s] %d/%d steps\n",
						i+1, phaseState, phaseName, bar, completedSteps, totalSteps)
				}
			}

			// Show signoffs if any
			if signoffs, ok := plan["signoffs"].([]any); ok && len(signoffs) > 0 {
				fmt.Printf("\nSignoffs (%d):\n", len(signoffs))
				for _, so := range signoffs {
					signoff, ok := so.(map[string]any)
					if !ok {
						continue
					}
					by := getStringOr(signoff, "by", "unknown")
					action := getStringOr(signoff, "action", "")
					at := getStringOr(signoff, "at", "")
					reason := getStringOr(signoff, "reason", "")
					line := fmt.Sprintf("  - %s: %s", by, action)
					if at != "" {
						line += fmt.Sprintf(" (%s)", at)
					}
					if reason != "" {
						line += fmt.Sprintf(" [%s]", reason)
					}
					fmt.Println(line)
				}
			}

			return nil
		},
	}
}

func newPlansApproveCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "approve <id>",
		Short: "approve a plan",
		Long:  "Approve a plan, signalling readiness to proceed.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("plan.approve", map[string]any{
				"plan_id": planID,
				"by":      "cli",
			})
			if err != nil {
				return fmt.Errorf("failed to approve plan: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			title := getStringOr(resultMap, "title", planID)
			fmt.Printf("Plan approved: %s\n", title)
			return nil
		},
	}
}

func newPlansRejectCmd() *cobra.Command {
	var reason string

	cmd := &cobra.Command{
		Use:   "reject <id>",
		Short: "reject a plan",
		Long:  "Reject a plan with an optional reason.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{
				"plan_id": planID,
				"by":      "cli",
			}
			if reason != "" {
				params["reason"] = reason
			}

			rawResult, err := client.Call("plan.reject", params)
			if err != nil {
				return fmt.Errorf("failed to reject plan: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			title := getStringOr(resultMap, "title", planID)
			fmt.Printf("Plan rejected: %s\n", title)
			return nil
		},
	}

	cmd.Flags().StringVar(&reason, "reason", "", "Rejection reason")

	return cmd
}

func newPlansConfirmCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "confirm <id>",
		Short: "confirm a plan",
		Long:  "Confirm a plan, acknowledging completion review.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			planID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("plan.confirm", map[string]any{
				"plan_id": planID,
				"by":      "cli",
			})
			if err != nil {
				return fmt.Errorf("failed to confirm plan: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			title := getStringOr(resultMap, "title", planID)
			fmt.Printf("Plan confirmed: %s\n", title)
			return nil
		},
	}
}
