package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// agents_children.go — `meept agents children <agent-or-session>` (DSH-9d,
// audit item 5 operator half): lists the delegated child subagents of a
// session or agent id via the read-only delegate.status RPC. Report-only:
// no steer/interrupt surface here — child control stays with the model's
// subagent_* tools; this is the operator's read mirror (the
// `meept intake status` posture).

// newAgentsChildrenCmd builds `meept agents children <agent-or-session>`.
func newAgentsChildrenCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "children <agent-or-session>",
		Short: "list delegated children of a session or agent (read-only)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			raw, err := client.Call("delegate.status", map[string]any{"query": query})
			if err != nil {
				return fmt.Errorf("delegate.status: %w", err)
			}
			var res struct {
				Query    string                 `json:"query"`
				Children []agentChildStatusRow  `json:"children"`
				Note     string                 `json:"note"`
			}
			if err := json.Unmarshal(raw, &res); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			fmt.Println(agentChildrenTable(res.Children))
			if res.Note != "" {
				fmt.Printf("note: %s\n", res.Note)
			}
			return nil
		},
	}
}

// agentChildStatusRow mirrors the delegate.status row shape (the frozen
// rpc.DelegateChildRow wire contract).
type agentChildStatusRow struct {
	ChildID         string `json:"child_id"`
	AgentID         string `json:"agent_id"`
	ParentSessionID string `json:"parent_session_id"`
	Mode            string `json:"mode"`
	State           string `json:"state"`
	Model           string `json:"model"`
	CreatedAt       string `json:"created_at_rfc3339,omitempty"`
	HasResult       bool   `json:"has_result"`
	Error           string `json:"error,omitempty"`
}

// agentChildrenTable renders the children table (unit-tested formatter —
// no daemon, no RPC in the test). UI text is lowercase per convention.
func agentChildrenTable(rows []agentChildStatusRow) string {
	if len(rows) == 0 {
		return "no delegated children."
	}
	var b strings.Builder
	w := tabwriter.NewWriter(&b, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "CHILD\tAGENT\tSESSION\tMODE\tSTATE\tMODEL\tRESULT")
	for _, r := range rows {
		mode := r.Mode
		if mode == "" {
			mode = "spawn"
		}
		result := "no"
		if r.HasResult {
			result = "yes"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
			r.ChildID, r.AgentID, r.ParentSessionID, mode, r.State, r.Model, result)
	}
	//nolint:errcheck // strings.Builder writes never fail
	w.Flush()
	return strings.TrimRight(b.String(), "\n")
}
