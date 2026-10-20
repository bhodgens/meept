package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// newACPCmd builds `meept acp` — the ACP (Agent Client Protocol) surface.
// The permissions subcommand tree (acp-human-permission-relay leaf 03,
// Contract C) drives the human permission relay: list what a paused
// external-agent is waiting on, answer allow/deny/always.
func newACPCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "acp",
		Short: "inspect external ACP agents and answer human permission requests",
		Long: `Inspect the ACP agent catalog and the human permission relay.

With [acp] permission_mode "human" (or decision_escalate_to_human), each
session/requestPermission from an external agent pauses until a human
answers it here. Unanswered requests fail closed after [acp] relay_timeout.

Examples:
  meept acp agents                              # catalog + live session states
  meept acp permissions list                    # pending permission requests
  meept acp permissions list --json             # machine-readable output
  meept acp permissions resolve <id> allow      # approve one request
  meept acp permissions resolve <id> always     # approve and let the agent
                                                # persist the grant
  meept acp permissions resolve <id> deny       # cancel one request`,
	}

	cmd.AddCommand(newACPAgentsCmd())
	cmd.AddCommand(newACPPermissionsCmd())

	return cmd
}

// acpAgentRow mirrors one acp.list agents entry.
type acpAgentRow struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	Running bool   `json:"running"`
	State   string `json:"state"`
}

// acpPendingRow mirrors one acp.permissions.list pending entry (frozen
// keys: id, agent_id, session_id, summary, created_at).
type acpPendingRow struct {
	ID        string `json:"id"`
	AgentID   string `json:"agent_id"`
	SessionID string `json:"session_id"`
	Summary   string `json:"summary"`
	CreatedAt string `json:"created_at"`
}

func newACPAgentsCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "agents",
		Short: "list ACP catalog agents and their live session states",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer closeDaemon(client)

			rawResult, err := client.Call("acp.list", map[string]any{})
			if err != nil {
				return fmt.Errorf("acp: %w", err)
			}
			if asJSON {
				return printRawJSON(os.Stdout, rawResult)
			}
			var res struct {
				Enabled bool          `json:"enabled"`
				Agents  []acpAgentRow `json:"agents"`
			}
			if err := json.Unmarshal(rawResult, &res); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			if !res.Enabled {
				fmt.Println("acp is disabled (set [acp] enabled=true)")
				return nil
			}
			return renderACPAgentsTable(os.Stdout, res.Agents)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newACPPermissionsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "permissions",
		Short: "list and resolve pending human permission requests",
	}
	cmd.AddCommand(newACPPermissionsListCmd())
	cmd.AddCommand(newACPPermissionsResolveCmd())
	return cmd
}

func newACPPermissionsListCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list pending permission requests from external ACP agents",
		Long:  "List the still-answerable human relay requests, oldest first. Empty when no agent is waiting or the relay is not armed.",
		RunE: func(cmd *cobra.Command, _ []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer closeDaemon(client)

			rawResult, err := client.Call("acp.permissions.list", map[string]any{})
			if err != nil {
				if strings.Contains(err.Error(), "method not found") {
					return fmt.Errorf("acp permission relay unavailable (daemon too old or [acp] relay not configured): %w", err)
				}
				return fmt.Errorf("acp: %w", err)
			}
			if asJSON {
				return printRawJSON(os.Stdout, rawResult)
			}

			var res struct {
				Enabled bool            `json:"enabled"`
				Pending []acpPendingRow `json:"pending"`
			}
			if err := json.Unmarshal(rawResult, &res); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			if !res.Enabled {
				fmt.Println("human permission relay is not armed (permission_mode is not \"human\")")
				return nil
			}
			return renderACPPermissionsTable(os.Stdout, res.Pending)
		},
	}

	cmd.Flags().BoolVar(&asJSON, "json", false, "Output as JSON")
	return cmd
}

func newACPPermissionsResolveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "resolve <id> <allow|deny|always>",
		Short: "answer one pending permission request",
		Long: `Answer one pending human relay request by id.

allow approves the request (ACP selected/allow-once); deny cancels it;
always approves with the agent's own allow_always option when it offers
one (the AGENT persists the grant — meept transports the choice and
remembers nothing itself). An agent without an allow_always option
resolves "always" exactly like "allow".
The verb is validated offline before any daemon call — a typo can never
silently deny.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := strings.TrimSpace(args[0])
			decision := strings.ToLower(strings.TrimSpace(args[1]))
			if id == "" {
				return fmt.Errorf("acp: permission id is required")
			}
			if decision != "allow" && decision != "deny" && decision != "always" {
				return fmt.Errorf("acp: decision must be \"allow\", \"deny\", or \"always\" (got %q)", args[1])
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer closeDaemon(client)

			rawResult, err := client.Call("acp.permissions.resolve", map[string]any{
				"id":       id,
				"decision": decision,
			})
			if err != nil {
				return fmt.Errorf("acp: %w", err)
			}
			var res struct {
				ID       string `json:"id"`
				Decision string `json:"decision"`
				Reason   string `json:"reason"`
			}
			if err := json.Unmarshal(rawResult, &res); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}
			fmt.Printf("resolved %s: %s (%s)\n", res.ID, res.Decision, res.Reason)
			return nil
		},
	}
	return cmd
}

// closeDaemon mirrors the effects.go defer shape: a close failure is
// reported on stderr, never swallowed and never fatal.
func closeDaemon(client interface{ Close() error }) {
	if cerr := client.Close(); cerr != nil {
		fmt.Fprintf(os.Stderr, "acp: close daemon connection: %v\n", cerr)
	}
}

func printRawJSON(w io.Writer, raw json.RawMessage) error {
	var buf bytes.Buffer
	if err := json.Indent(&buf, raw, "", "  "); err != nil {
		return fmt.Errorf("failed to format JSON: %w", err)
	}
	fmt.Fprintln(w, buf.String())
	return nil
}

func renderACPAgentsTable(w io.Writer, agents []acpAgentRow) error {
	if len(agents) == 0 {
		fmt.Fprintln(w, "no acp agents configured")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "id\tenabled\trunning\tstate")
	for _, a := range agents {
		state := a.State
		if state == "" {
			state = "stopped"
		}
		fmt.Fprintf(tw, "%s\t%t\t%t\t%s\n", a.ID, a.Enabled, a.Running, state)
	}
	return tw.Flush()
}

func renderACPPermissionsTable(w io.Writer, pending []acpPendingRow) error {
	if len(pending) == 0 {
		fmt.Fprintln(w, "no pending permission requests")
		return nil
	}
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "id\tagent\tsession\tcreated\tsummary")
	for _, p := range pending {
		summary := p.Summary
		if r := []rune(summary); len(r) > 80 {
			summary = string(r[:77]) + "..."
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", p.ID, p.AgentID, p.SessionID, p.CreatedAt, summary)
	}
	return tw.Flush()
}
