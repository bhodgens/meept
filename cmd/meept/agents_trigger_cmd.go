package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// newAgentsTriggerCmd implements `meept agents trigger <id> [payload...]`
// (goal-completion finding #3): a user-facing trigger invocation that
// reports skipped rounds instead of implying completion. The payload
// arguments, when present, are joined and parsed as one JSON object.
func newAgentsTriggerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "trigger <id> [payload-json]",
		Short: "invoke an employee's goal loop now",
		Long: `Invoke an AI employee's goal loop immediately (the agents.trigger RPC).

The optional payload argument is a JSON object passed through as the
trigger payload (e.g. '{"source":"manual","event":"push"}').

The outcome is reported explicitly: a round the employee's active-plan cap
blocked prints "employee busy: active plan cap reached (skipped)" instead
of implying completion.

Examples:
  meept agents trigger ci-monitor
  meept agents trigger researcher '{"source":"manual","round":"probe"}'`,
		Args: cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]

			var payload map[string]any
			if len(args) == 2 && strings.TrimSpace(args[1]) != "" {
				if err := json.Unmarshal([]byte(args[1]), &payload); err != nil {
					return fmt.Errorf("payload must be a JSON object: %w", err)
				}
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.trigger", map[string]any{
				"id":      agentID,
				"payload": payload,
			})
			if err != nil {
				return fmt.Errorf("failed to trigger agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			// Envelope status is "skipped" when the round was
			// cap-blocked (goal-completion finding #3); anything else
			// is a dispatched round ("triggered" envelope / "completed"
			// or "skipped" inside invocation).
			if status, _ := resultMap["status"].(string); status == "skipped" {
				reason, _ := resultMap["skip_reason"].(string)
				switch reason {
				case "max_active_plans":
					fmt.Println("employee busy: active plan cap reached (skipped)")
				default:
					fmt.Printf("skipped (%s)\n", reason)
				}
				return nil
			}
			fmt.Printf("triggered agent: %s\n", agentID)
			return nil
		},
	}
}
