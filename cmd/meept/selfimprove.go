package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui"
)

func newSelfImproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "selfimprove",
		Short: "self-improvement commands",
		Long:  "Run self-improvement cycles to detect and fix issues automatically.",
	}

	cmd.AddCommand(
		newSelfImproveDetectCmd(),
		newSelfImproveAnalyzeCmd(),
		newSelfImproveGenerateCmd(),
		newSelfImproveValidateCmd(),
		newSelfImproveApplyCmd(),
		newSelfImproveRejectCmd(),
		newSelfImproveFullCycleCmd(),
		newSelfImproveStatusCmd(),
	)

	return cmd
}

// CLI-8 FIX: helper to print raw JSON bytes as pretty-printed JSON.
func printJSON(data []byte) error {
	var parsed any
	if err := json.Unmarshal(data, &parsed); err != nil {
		return fmt.Errorf("failed to parse response as JSON: %w", err)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(&parsed)
}

func dialRPC() (*tui.RPCClient, error) {
	client := tui.NewRPCClient(getSocketPath())
	if err := client.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	return client, nil
}

func newSelfImproveDetectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "detect issues in the codebase",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("selfimprove.detect", nil)
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}

			var resp struct {
				Issues []struct {
					ID          string `json:"id"`
					Type        string `json:"type"`
					Severity    string `json:"severity"`
					Description string `json:"description"`
				} `json:"issues"`
				Count int `json:"count"`
			}
			if err := json.Unmarshal(result, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if resp.Count == 0 {
				fmt.Println("no issues detected.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTYPE\tSEVERITY\tDESCRIPTION")
			for _, issue := range resp.Issues {
				desc := issue.Description
				if len(desc) > 60 {
					desc = desc[:60] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					issue.ID, issue.Type, issue.Severity, desc)
			}
			w.Flush()

			fmt.Printf("\ntotal: %d issues\n", resp.Count)
			return nil
		},
	}
}

func newSelfImproveAnalyzeCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "analyze",
		Short: "analyze detected issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("selfimprove.analyze", nil)
			if err != nil {
				return fmt.Errorf("analysis failed: %w", err)
			}

			if jsonOutput {
				// CLI-8 FIX: use pretty-printed JSON instead of raw bytes
				return printJSON(result)
			}

			var resp struct {
				Analyses []struct {
					IssueID    string `json:"issue_id"`
					RootCause  string `json:"root_cause"`
					Confidence float64 `json:"confidence"`
				} `json:"analyses"`
				Count int `json:"count"`
			}
			if err := json.Unmarshal(result, &resp); err != nil {
				// CLI-8 FIX: fallback to pretty-printed JSON instead of raw bytes
				return printJSON(result)
				return nil
			}

			if resp.Count == 0 {
				fmt.Println("no analyses completed.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ISSUE ID\tROOT CAUSE\tCONFIDENCE")
			for _, a := range resp.Analyses {
				cause := a.RootCause
				if len(cause) > 50 {
					cause = cause[:50] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%.2f\n", a.IssueID, cause, a.Confidence)
			}
			w.Flush()

			fmt.Printf("\ntotal: %d analyses\n", resp.Count)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func newSelfImproveGenerateCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "generate-fixes",
		Short: "generate fixes for analyzed issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("selfimprove.generate", nil)
			if err != nil {
				return fmt.Errorf("fix generation failed: %w", err)
			}

			if jsonOutput {
				// CLI-8 FIX: use pretty-printed JSON instead of raw bytes
				return printJSON(result)
			}

			var resp struct {
				Fixes []struct {
					ID          string `json:"id"`
					IssueID     string `json:"issue_id"`
					Description string `json:"description"`
					FilesCount  int    `json:"files_count"`
				} `json:"fixes"`
				Count int `json:"count"`
			}
			if err := json.Unmarshal(result, &resp); err != nil {
				// CLI-8 FIX: fallback to pretty-printed JSON instead of raw bytes
				return printJSON(result)
				return nil
			}

			if resp.Count == 0 {
				fmt.Println("no fixes generated.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "FIX ID\tISSUE ID\tFILES\tDESCRIPTION")
			for _, f := range resp.Fixes {
				desc := f.Description
				if len(desc) > 40 {
					desc = desc[:40] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%d\t%s\n", f.ID, f.IssueID, f.FilesCount, desc)
			}
			w.Flush()

			fmt.Printf("\ntotal: %d fixes\n", resp.Count)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func newSelfImproveValidateCmd() *cobra.Command {
	var jsonOutput bool

	cmd := &cobra.Command{
		Use:   "validate",
		Short: "validate generated fixes",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("selfimprove.validate", nil)
			if err != nil {
				return fmt.Errorf("validation failed: %w", err)
			}

			if jsonOutput {
				fmt.Println(string(result))
				return nil
			}

			var resp struct {
				Validations []struct {
					FixID   string `json:"fix_id"`
					Success bool   `json:"success"`
					Message string `json:"message"`
				} `json:"validations"`
				Count int `json:"count"`
			}
			if err := json.Unmarshal(result, &resp); err != nil {
				// Fallback to raw output
				fmt.Println(string(result))
				return nil
			}

			if resp.Count == 0 {
				fmt.Println("no validations completed.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "FIX ID\tSTATUS\tMESSAGE")
			passed := 0
			for _, v := range resp.Validations {
				status := "FAIL"
				if v.Success {
					status = "PASS"
					passed++
				}
				msg := v.Message
				if len(msg) > 50 {
					msg = msg[:50] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n", v.FixID, status, msg)
			}
			w.Flush()

			fmt.Printf("\ntotal: %d validations (%d passed)\n", resp.Count, passed)
			return nil
		},
	}

	cmd.Flags().BoolVar(&jsonOutput, "json", false, "Output as JSON")
	return cmd
}

func newSelfImproveApplyCmd() *cobra.Command {
	var fixID string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "approve and apply a pending fix",
		RunE: func(cmd *cobra.Command, args []string) error {
			if fixID == "" {
				return fmt.Errorf("fix ID is required (use --fix-id)")
			}

			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("selfimprove.apply", map[string]string{"fix_id": fixID})
			if err != nil {
				return fmt.Errorf("apply failed: %w", err)
			}

			fmt.Printf("fix %s applied successfully\n", fixID)
			fmt.Println(string(result))
			return nil
		},
	}

	cmd.Flags().StringVar(&fixID, "fix-id", "", "ID of the fix to apply")

	return cmd
}

func newSelfImproveRejectCmd() *cobra.Command {
	var fixID string
	var reason string

	cmd := &cobra.Command{
		Use:   "reject",
		Short: "reject a pending fix",
		RunE: func(cmd *cobra.Command, args []string) error {
			if fixID == "" {
				return fmt.Errorf("fix ID is required (use --fix-id)")
			}

			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			params := map[string]string{
				"fix_id": fixID,
				"reason": reason,
			}
			_, err = client.Call("selfimprove.reject", params)
			if err != nil {
				return fmt.Errorf("reject failed: %w", err)
			}

			fmt.Printf("fix %s rejected\n", fixID)
			return nil
		},
	}

	cmd.Flags().StringVar(&fixID, "fix-id", "", "ID of the fix to reject")
	cmd.Flags().StringVar(&reason, "reason", "rejected via cli", "rejection reason")

	return cmd
}

func newSelfImproveFullCycleCmd() *cobra.Command {
	var interactive bool

	cmd := &cobra.Command{
		Use:   "full-cycle",
		Short: "run a complete improvement cycle",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			client.SetTimeout(10 * time.Minute) // full cycle can take a while

			fmt.Println("running full improvement cycle...")

			result, err := client.Call("selfimprove.cycle", map[string]any{
				"interactive": interactive,
			})
			if err != nil {
				return fmt.Errorf("cycle failed: %w", err)
			}

			var cycle struct {
				ID             string `json:"id"`
				Status         string `json:"status"`
				IssuesDetected int    `json:"issues_detected"`
				IssuesAnalyzed int    `json:"issues_analyzed"`
				FixesGenerated int    `json:"fixes_generated"`
				FixesValidated int    `json:"fixes_validated"`
				FixesApplied   int    `json:"fixes_applied"`
			}
			if err := json.Unmarshal(result, &cycle); err != nil {
				// Print raw result if parsing fails
				fmt.Println(string(result))
				return nil
			}

			fmt.Printf("\ncycle %s completed:\n", cycle.ID)
			fmt.Printf("  status:          %s\n", cycle.Status)
			fmt.Printf("  issues detected: %d\n", cycle.IssuesDetected)
			fmt.Printf("  issues analyzed: %d\n", cycle.IssuesAnalyzed)
			fmt.Printf("  fixes generated: %d\n", cycle.FixesGenerated)
			fmt.Printf("  fixes validated: %d\n", cycle.FixesValidated)
			fmt.Printf("  fixes applied:   %d\n", cycle.FixesApplied)

			return nil
		},
	}

	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "run in interactive mode (approve fixes manually)")

	return cmd
}

func newSelfImproveStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "show self-improvement status",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := dialRPC()
			if err != nil {
				return err
			}
			defer client.Close()

			result, err := client.Call("selfimprove.status", nil)
			if err != nil {
				return fmt.Errorf("status query failed: %w", err)
			}

			var status struct {
				IssuesCount         int              `json:"issues_count"`
				AnalysesCount       int              `json:"analyses_count"`
				FixesCount          int              `json:"fixes_count"`
				ValidationsCount    int              `json:"validations_count"`
				AppliedCount        int              `json:"applied_count"`
				ConsecutiveFailures int              `json:"consecutive_failures"`
				CircuitBreakerTripped bool            `json:"circuit_breaker_tripped"`
				PendingApprovals    []string         `json:"pending_approvals"`
				CyclesCompleted     int              `json:"cycles_completed"`
			}
			if err := json.Unmarshal(result, &status); err != nil {
				return fmt.Errorf("failed to parse status: %w", err)
			}

			fmt.Println("self-improvement status")
			fmt.Println("=======================")
			fmt.Printf("issues:         %d\n", status.IssuesCount)
			fmt.Printf("analyses:       %d\n", status.AnalysesCount)
			fmt.Printf("fixes:          %d\n", status.FixesCount)
			fmt.Printf("validations:    %d\n", status.ValidationsCount)
			fmt.Printf("applied:        %d\n", status.AppliedCount)
			fmt.Printf("cycles:         %d\n", status.CyclesCompleted)

			if len(status.PendingApprovals) > 0 {
				fmt.Printf("\npending approvals: %d\n", len(status.PendingApprovals))
				for _, id := range status.PendingApprovals {
					fmt.Printf("  - %s\n", id)
				}
			}

			if status.CircuitBreakerTripped {
				fmt.Println("\ncircuit breaker tripped - too many consecutive failures")
			}

			return nil
		},
	}
}
