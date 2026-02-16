package main

import (
	"context"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/caimlas/meept/internal/selfimprove"
	"github.com/spf13/cobra"
)

func newSelfImproveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "selfimprove",
		Short: "Self-improvement commands",
		Long:  "Run self-improvement cycles to detect and fix issues automatically.",
	}

	cmd.AddCommand(
		newSelfImproveDetectCmd(),
		newSelfImproveAnalyzeCmd(),
		newSelfImproveGenerateCmd(),
		newSelfImproveValidateCmd(),
		newSelfImproveApplyCmd(),
		newSelfImproveFullCycleCmd(),
		newSelfImproveStatusCmd(),
	)

	return cmd
}

func newSelfImproveDetectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "detect",
		Short: "Detect issues in the codebase",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := selfimprove.DefaultConfig()
			controller := selfimprove.NewController(cfg, nil, nil, "", nil)

			issues, err := controller.Detect(context.Background())
			if err != nil {
				return fmt.Errorf("detection failed: %w", err)
			}

			if len(issues) == 0 {
				fmt.Println("No issues detected.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTYPE\tSEVERITY\tDESCRIPTION")
			for _, issue := range issues {
				desc := issue.Description
				if len(desc) > 60 {
					desc = desc[:60] + "..."
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
					issue.ID, issue.Type, issue.Severity, desc)
			}
			w.Flush()

			fmt.Printf("\nTotal: %d issues\n", len(issues))
			return nil
		},
	}
}

func newSelfImproveAnalyzeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "analyze",
		Short: "Analyze detected issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Analyzing issues... (requires LLM client)")
			fmt.Println("Run 'meept selfimprove full-cycle' for a complete improvement cycle.")
			return nil
		},
	}
}

func newSelfImproveGenerateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "generate-fixes",
		Short: "Generate fixes for analyzed issues",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Generating fixes... (requires LLM client)")
			fmt.Println("Run 'meept selfimprove full-cycle' for a complete improvement cycle.")
			return nil
		},
	}
}

func newSelfImproveValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate",
		Short: "Validate generated fixes",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Println("Validating fixes...")
			fmt.Println("Run 'meept selfimprove full-cycle' for a complete improvement cycle.")
			return nil
		},
	}
}

func newSelfImproveApplyCmd() *cobra.Command {
	var fixID string

	cmd := &cobra.Command{
		Use:   "apply",
		Short: "Apply validated fixes",
		RunE: func(cmd *cobra.Command, args []string) error {
			if fixID == "" {
				return fmt.Errorf("fix ID is required (use --fix-id)")
			}

			fmt.Printf("Applying fix %s...\n", fixID)
			fmt.Println("Run 'meept selfimprove full-cycle --interactive' for guided application.")
			return nil
		},
	}

	cmd.Flags().StringVar(&fixID, "fix-id", "", "ID of the fix to apply")

	return cmd
}

func newSelfImproveFullCycleCmd() *cobra.Command {
	var interactive bool

	cmd := &cobra.Command{
		Use:   "full-cycle",
		Short: "Run a complete improvement cycle",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := selfimprove.DefaultConfig()
			controller := selfimprove.NewController(cfg, nil, nil, "", nil)

			fmt.Println("Running full improvement cycle...")

			cycle, err := controller.RunFullCycle(context.Background(), interactive)
			if err != nil {
				return fmt.Errorf("cycle failed: %w", err)
			}

			fmt.Printf("\nCycle %s completed:\n", cycle.ID)
			fmt.Printf("  Issues detected:  %d\n", cycle.IssuesDetected)
			fmt.Printf("  Issues analyzed:  %d\n", cycle.IssuesAnalyzed)
			fmt.Printf("  Fixes generated:  %d\n", cycle.FixesGenerated)
			fmt.Printf("  Fixes validated:  %d\n", cycle.FixesValidated)
			fmt.Printf("  Fixes applied:    %d\n", cycle.FixesApplied)

			return nil
		},
	}

	cmd.Flags().BoolVarP(&interactive, "interactive", "i", false, "Run in interactive mode (approve fixes manually)")

	return cmd
}

func newSelfImproveStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show self-improvement status",
		RunE: func(cmd *cobra.Command, args []string) error {
			cfg := selfimprove.DefaultConfig()
			controller := selfimprove.NewController(cfg, nil, nil, "", nil)

			status := controller.GetStatus()

			fmt.Println("Self-Improvement Status")
			fmt.Println("=======================")
			fmt.Printf("Issues:       %d\n", status.IssuesCount)
			fmt.Printf("Analyses:     %d\n", status.AnalysesCount)
			fmt.Printf("Fixes:        %d\n", status.FixesCount)
			fmt.Printf("Validations:  %d\n", status.ValidationsCount)
			fmt.Printf("Applied:      %d\n", status.AppliedCount)
			fmt.Printf("Cycles:       %d\n", status.CyclesCompleted)

			if len(status.PendingApprovals) > 0 {
				fmt.Printf("\nPending Approvals: %d\n", len(status.PendingApprovals))
				for _, id := range status.PendingApprovals {
					fmt.Printf("  - %s\n", id)
				}
			}

			if status.CircuitBreakerTripped {
				fmt.Println("\n⚠️  Circuit breaker tripped - too many consecutive failures")
			}

			return nil
		},
	}
}
