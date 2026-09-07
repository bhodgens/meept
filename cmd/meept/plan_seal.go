package main

// Brainstorm draft lifecycle subcommands on the `meept plans` command tree
// (plan-compiler leaf 04, Contract C). Transport is the low-level
// extensibility Call seam, same as every other plans subcommand:
//
//   meept plans draft <task-id>          draft status (version, sealed?)
//   meept plans show-draft <task-id>     print draft markdown
//   meept plans edit-draft <task-id> [file|-]   replace draft content
//   meept plans seal <task-id>           seal + compile + execute
//
// `show`/`edit` names are taken by the plan-lifecycle show subcommand on the
// same tree, so the draft commands carry the -draft suffix (deviation noted
// in the leaf report; `meept plans show <task-id>` remains plan.show).

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
)

// draftSealClient dials the daemon and invokes an RPC method.
func draftSealCall(method string, params map[string]any) (map[string]any, error) {
	client, err := connectDaemon()
	if err != nil {
		return nil, fmt.Errorf("failed to connect to daemon: %w", err)
	}
	defer client.Close()

	rawResult, err := client.Call(method, params)
	if err != nil {
		return nil, err
	}
	var resultMap map[string]any
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
		return nil, fmt.Errorf("%s", errMsg)
	}
	return resultMap, nil
}

// newPlansDraftCmd reports the draft status for a task.
func newPlansDraftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "draft <task-id>",
		Short: "show brainstorm draft status for a task",
		Long:  "Show the brainstorm plan draft status (version, sealed hash) for a task.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := draftSealCall("plan.draft", map[string]any{"task_id": args[0]})
			if err != nil {
				return err
			}
			version := 0
			if v, ok := res["version"].(float64); ok {
				version = int(v)
			}
			sealed := getStringOr(res, "sealed", "")
			fmt.Printf("task:    %s\n", getStringOr(res, "task_id", args[0]))
			fmt.Printf("status:  %s\n", getStringOr(res, "status", ""))
			fmt.Printf("version: %d\n", version)
			if sealed != "" {
				fmt.Printf("sealed:  %s\n", sealed)
			}
			return nil
		},
	}
}

// newPlansShowDraftCmd prints the draft markdown.
func newPlansShowDraftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "show-draft <task-id>",
		Short: "print a task's brainstorm draft markdown",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := draftSealCall("plan.draft", map[string]any{"task_id": args[0]})
			if err != nil {
				return err
			}
			fmt.Print(getStringOr(res, "markdown", ""))
			return nil
		},
	}
}

// newPlansEditDraftCmd replaces the draft content from a file or stdin.
func newPlansEditDraftCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "edit-draft <task-id> [file|-]",
		Short: "replace a task's brainstorm draft (file or stdin)",
		Long:  "Replace a task's brainstorm draft markdown from a file, or from stdin when the path is \"-\" or omitted.",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			taskID := args[0]
			var (
				content []byte
				err     error
			)
			if len(args) == 2 && args[1] != "-" {
				content, err = os.ReadFile(args[1])
				if err != nil {
					return fmt.Errorf("read draft file: %w", err)
				}
			} else {
				content, err = io.ReadAll(os.Stdin)
				if err != nil {
					return fmt.Errorf("read draft from stdin: %w", err)
				}
			}
			res, err := draftSealCall("plan.draft", map[string]any{
				"task_id":  taskID,
				"markdown": string(content),
			})
			if err != nil {
				return err
			}
			version := 0
			if v, ok := res["version"].(float64); ok {
				version = int(v)
			}
			fmt.Printf("Draft saved (version %d).\n", version)
			return nil
		},
	}
}

// newPlansSealCmd seals + compiles + executes the task's draft.
func newPlansSealCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "seal <task-id>",
		Short: "seal and compile a task's brainstorm draft",
		Long: `Seal a task's brainstorm draft (records its hash), compile it into
executable phases (zero LLM), and start execution. On compile problems the
draft stays a draft and the problem list is printed.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			res, err := draftSealCall("plan.seal", map[string]any{"task_id": args[0]})
			if err != nil {
				return err
			}
			switch getStringOr(res, "status", "") {
			case "sealed":
				fmt.Printf("Plan sealed (mode: %s)\n", getStringOr(res, "mode", "flat"))
				fmt.Printf("hash:    %s\n", getStringOr(res, "hash", ""))
				if warns, ok := res["warnings"].([]any); ok && len(warns) > 0 {
					fmt.Println("warnings:")
					for _, w := range warns {
						if s, ok := w.(string); ok {
							fmt.Printf("  - %s\n", s)
						}
					}
				}
				return nil
			case "problems":
				fmt.Println("plan compile failed: problems found (draft stays a draft)")
				if probs, ok := res["problems"].([]any); ok {
					for _, p := range probs {
						if m, ok := p.(map[string]any); ok {
							line := 0
							if v, ok := m["line"].(float64); ok {
								line = int(v)
							}
							fmt.Printf("  line %d: %s\n", line, getStringOr(m, "message", ""))
						}
					}
				}
				os.Exit(2)
				return nil
			default:
				return fmt.Errorf("unexpected seal status: %v", res["status"])
			}
		},
	}
}
