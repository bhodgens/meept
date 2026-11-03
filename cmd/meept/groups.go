package main

// groups.go — the `meept groups` CLI family (agent-groups plan tree leaf 06,
// docs/plans/20261030-agent-groups/06-diagnosis.md, task T2).
//
// Report-only diagnosis over the daemon's groups.* RPC surface:
//
//	meept groups list                — policy + binding tables
//	meept groups explain <agent>     — per grant/deny: binding + policy source
//	meept groups check [--json]      — defect enumeration (R5); exit 0 clean,
//	                                   exit 1 defects (scriptable)
//
// All human renders are lowercase (UI convention). --json prints the RPC
// result's stable keys verbatim (pinned by service unit tests, T4).

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/caimlas/meept/internal/groups"
	"github.com/spf13/cobra"
)

// groupsLong is the long help for the groups command.
const groupsLong = `Inspect agent group policies, bindings, and effective permissions.

Report-only: every subcommand reads state and never mutates it.

  meept groups list                 # policy + binding tables
  meept groups explain <agent>      # per grant/deny: the binding + policy behind it
  meept groups check                # config defect enumeration (exit 1 on defects)
  meept groups check --json         # machine-readable defects (stable keys)

See: docs/plans/20261030-agent-groups/06-diagnosis.md`

// ---------------------------------------------------------------------------
// root: meept groups
// ---------------------------------------------------------------------------

func newGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "groups",
		Short: "inspect agent group policies and effective permissions",
		Long:  groupsLong,
	}

	cmd.AddCommand(newGroupsListCmd())
	cmd.AddCommand(newGroupsExplainCmd())
	cmd.AddCommand(newGroupsCheckCmd())
	// Time-boxed overrides subtree (leaf 07-overrides, C8): operator
	// add/list/prune over $MEEPT_HOME/group_overrides.json5.
	cmd.AddCommand(newGroupsOverrideCmd())

	return cmd
}

// ---------------------------------------------------------------------------
// groups list
// ---------------------------------------------------------------------------

func newGroupsListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list group policies and bindings",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("groups.list", map[string]any{})
			if err != nil {
				return fmt.Errorf("failed to list groups: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			if outputJSON {
				return printGroupsJSON(resultMap)
			}

			policies, _ := resultMap["policies"].([]any)
			bindings, _ := resultMap["bindings"].([]any)

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "POLICY\tALLOW\tDENY\tMEMORY_SCOPE\tNETWORK")
			for _, p := range policies {
				pm, ok := p.(map[string]any)
				if !ok {
					continue
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					getStringOr(pm, "name", ""),
					joinAny(pm["tools_allowed"]),
					joinAny(pm["tools_denied"]),
					getStringOr(pm, "memory_max_scope", "-"),
					getStringOr(pm, "network_class", "-"),
				)
			}
			w.Flush()

			fmt.Fprintln(w, "")
			fmt.Fprintln(w, "SUBJECT\tPOLICY\tSCOPE")
			for _, b := range bindings {
				bm, ok := b.(map[string]any)
				if !ok {
					continue
				}
				scope := getStringOr(bm, "scope", "")
				if scope == "" {
					scope = "daemon"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\n",
					getStringOr(bm, "subject", ""),
					getStringOr(bm, "policy", ""),
					scope,
				)
			}
			w.Flush()

			defaultManaged, _ := resultMap["default_managed"].(bool)
			fmt.Printf("\ndefault_managed: %t\n", defaultManaged)
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

// ---------------------------------------------------------------------------
// groups explain
// ---------------------------------------------------------------------------

func newGroupsExplainCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "explain <agent>",
		Short: "explain an agent's effective permissions, naming the binding and policy behind each grant and deny",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("groups.explain", map[string]any{"agent_id": args[0]})
			if err != nil {
				return fmt.Errorf("failed to explain agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			if outputJSON {
				return printGroupsJSON(resultMap)
			}

			renderExplainHuman(resultMap)
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON")
	return cmd
}

// renderExplainHuman prints the explain table: resolution summary, the
// bindings that selected the subject, and every attributed grant/deny.
func renderExplainHuman(resultMap map[string]any) {
	agentID := getStringOr(resultMap, "agent_id", "")
	managed, _ := resultMap["managed"].(bool)

	fmt.Printf("agent: %s\n", agentID)
	if gs, ok := resultMap["groups"].([]any); ok && len(gs) > 0 {
		fmt.Printf("groups: %s\n", joinAny(gs))
	} else {
		fmt.Println("groups: (none)")
	}
	if managed {
		fmt.Println("managed: true (group policy active)")
	} else {
		fmt.Println("managed: false (legacy spec-allowlist posture)")
	}

	fmt.Printf("\nallowed (%s):\n", joinAny(resultMap["allowed_tools"]))
	fmt.Printf("denied (%s):\n", joinAny(resultMap["denied_tools"]))

	fmt.Println("\nrule_source:")
	grants, _ := resultMap["grants"].([]any)
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "TOOL\tEFFECT\tPOLICY\tBINDING\tSCOPE\tNOTE")
	for _, g := range grants {
		gm, ok := g.(map[string]any)
		if !ok {
			continue
		}
		note := getStringOr(gm, "reason", "")
		if note == "" {
			note = "-"
		}
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
			getStringOr(gm, "tool", ""),
			getStringOr(gm, "effect", ""),
			getStringOr(gm, "policy", "-"),
			getStringOr(gm, "binding_subject", "-"),
			getStringOr(gm, "binding_scope", "-"),
			note,
		)
	}
	w.Flush()

	bindings, _ := resultMap["bindings"].([]any)
	if len(bindings) > 0 {
		fmt.Println("\nbindings:")
		for _, b := range bindings {
			bm, ok := b.(map[string]any)
			if !ok {
				continue
			}
			line := fmt.Sprintf("  %s -> policy %s (scope %s)",
				getStringOr(bm, "subject", ""),
				getStringOr(bm, "policy", ""),
				getStringOr(bm, "scope", ""))
			if note := getStringOr(bm, "note", ""); note != "" {
				line += " — " + note
			}
			fmt.Println(line)
		}
	}
}

// ---------------------------------------------------------------------------
// groups check
// ---------------------------------------------------------------------------

func newGroupsCheckCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "check",
		Short: "enumerate group config defects (exits 1 when defects are found)",
		Long: `Enumerate group configuration defects: cyclic membership chains,
dangling policy references, unregistered tool names, unknown groups in
agent frontmatter, deny+allow conflicts, deny-everything policies,
default_managed surprises, and expired overrides.

Exit codes: 0 clean, 1 defects found (scriptable). Report-only.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("groups.check", map[string]any{})
			if err != nil {
				return fmt.Errorf("failed to check groups: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			if outputJSON {
				return printGroupsJSON(resultMap)
			}

			renderCheckHuman(resultMap)
			return exitCodeFromCheck(resultMap)
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "print the raw RPC result as JSON (still exits 1 on defects)")
	return cmd
}

// renderCheckHuman prints the defect enumeration.
func renderCheckHuman(resultMap map[string]any) {
	defects, _ := resultMap["defects"].([]any)
	count := 0
	if c, ok := resultMap["defect_count"].(float64); ok {
		count = int(c)
	}
	checked := 0
	if c, ok := resultMap["agents_checked"].(float64); ok {
		checked = int(c)
	}

	if count == 0 {
		fmt.Printf("groups config: ok (%d agents evaluated, no defects)\n", checked)
		return
	}

	fmt.Printf("groups config: %d defect(s) across %d agents evaluated\n", count, checked)
	for _, d := range defects {
		dm, ok := d.(map[string]any)
		if !ok {
			continue
		}
		fmt.Printf("  [%s] %s: %s\n",
			getStringOr(dm, "code", "?"),
			getStringOr(dm, "name", "?"),
			getStringOr(dm, "detail", ""))
	}
}

// exitCodeFromCheck maps the check result to the scriptable exit code:
// 0 clean, 1 defects (R5 exit-code contract).
func exitCodeFromCheck(resultMap map[string]any) error {
	count := 0
	if c, ok := resultMap["defect_count"].(float64); ok {
		count = int(c)
	}
	if count > 0 {
		return groups.ErrCheckDefects
	}
	return nil
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// printGroupsJSON marshals a result map with stable formatting.
func printGroupsJSON(m map[string]any) error {
	output, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal JSON: %w", err)
	}
	fmt.Println(string(output))
	return nil
}

// joinAny renders a JSON array of strings as a comma-joined line ("-"/""
// when absent/empty).
func joinAny(v any) string {
	items, ok := v.([]any)
	if !ok || len(items) == 0 {
		return "-"
	}
	parts := make([]string, 0, len(items))
	for _, it := range items {
		if s, ok := it.(string); ok {
			parts = append(parts, s)
		}
	}
	if len(parts) == 0 {
		return "-"
	}
	return strings.Join(parts, ",")
}
