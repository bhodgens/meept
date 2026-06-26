package main

import (
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/agent"
	"github.com/spf13/cobra"
)

func newImprovementsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "improvements",
		Short:   "manage improvement proposals",
		Aliases: []string{"improve", "improvement"},
	}
	cmd.AddCommand(newImprovementsListCmd())
	cmd.AddCommand(newImprovementsApplyCmd())
	cmd.AddCommand(newImprovementsSkipCmd())
	return cmd
}

func newImprovementsListCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "list",
		Short: "list pending improvement proposals",
		RunE: func(cmd *cobra.Command, args []string) error {
			q := agent.NewExternalProposalQueue(".meept/improvements.md")
			pending, err := q.ListPending()
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}
			if len(pending) == 0 {
				fmt.Println("no pending proposals")
				return nil
			}
			for _, p := range pending {
				fmt.Printf("[%s] %s -> %s\n", p.ID, p.Type, p.Target)
				fmt.Printf("  confidence: %.2f  source: %s\n", p.Confidence, p.Source)
				fmt.Printf("  %s\n\n", p.Justification)
			}
			return nil
		},
	}
}

func newImprovementsApplyCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "apply <id>",
		Short: "apply a pending proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := agent.NewExternalProposalQueue(".meept/improvements.md")
			pending, err := q.ListPending()
			if err != nil {
				return fmt.Errorf("list: %w", err)
			}
			var target *agent.ReflectionProposal
			for i := range pending {
				if pending[i].ID == args[0] {
					target = &pending[i]
					break
				}
			}
			if target == nil {
				return fmt.Errorf("proposal %s not found", args[0])
			}
			// Authorization check
			if agent.IsAlwaysProposeOnly(target.Target) {
				fmt.Printf("warning: %s is always propose-only; write manually.\n", target.Target)
				fmt.Printf("proposed change:\n%s\n", target.Change)
				return nil
			}
			// Apply — write the change to the target file
			if err := os.WriteFile(target.Target, []byte(target.Change), 0o644); err != nil {
				return fmt.Errorf("apply: %w", err)
			}
			if err := q.MarkApplied(target.ID); err != nil {
				return fmt.Errorf("mark applied: %w", err)
			}
			fmt.Printf("applied: %s\n", target.Target)
			return nil
		},
	}
}

func newImprovementsSkipCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "skip <id>",
		Short: "skip a pending proposal",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			q := agent.NewExternalProposalQueue(".meept/improvements.md")
			if err := q.MarkSkipped(args[0]); err != nil {
				return fmt.Errorf("skip: %w", err)
			}
			fmt.Printf("skipped: %s\n", args[0])
			return nil
		},
	}
}
