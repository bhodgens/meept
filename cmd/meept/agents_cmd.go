package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/caimlas/meept/internal/config"
	"github.com/spf13/cobra"
)

// agentsLong is the long help for the agents command. It explains the
// employee/constitution model and links to the docs.
const agentsLong = `Manage AI employees: persistent agents bounded by a constitution.

An employee is a long-running agent with an explicit constitution that
defines its role, goals, constraints, and escalation policy. Employees
operate in one of three tiers (1=autonomous, 2=plan-signoff, 3=approval
required) and are continuously audited for constitution drift.

The constitution is the source of truth: every lifecycle operation
(create, update, amend, delete) validates against it. Amendments route
through the Plan signoff workflow before taking effect.

Examples:
  meept agents list                              # all employees, status, tier, drift
  meept agents show <id>                         # full definition
  meept agents create definition.json            # validates constitution
  meept agents amend <id> --field=constraints.risk_ceiling high
  meept agents goals --employee=researcher       # list goals with health
  meept agents audit <id> --since=24h            # recent audit findings

See: docs/workflows/employees.md
`

// ---------------------------------------------------------------------------
// root: meept agents
// ---------------------------------------------------------------------------

func newAgentsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "agents",
		Short: "manage ai employees",
		Long:  agentsLong,
	}

	cmd.AddCommand(newAgentsListCmd())
	cmd.AddCommand(newAgentsShowCmd())
	cmd.AddCommand(newAgentsCreateCmd())
	cmd.AddCommand(newAgentsUpdateCmd())
	cmd.AddCommand(newAgentsDeleteCmd())
	cmd.AddCommand(newAgentsPauseCmd())
	cmd.AddCommand(newAgentsResumeCmd())
	cmd.AddCommand(newAgentsAmendCmd())
	cmd.AddCommand(newAgentsMigrateCmd())
	cmd.AddCommand(newAgentsGoalsCmd())
	cmd.AddCommand(newAgentsGoalCmd())
	cmd.AddCommand(newAgentsAuditCmd())

	return cmd
}

// ---------------------------------------------------------------------------
// helpers
// ---------------------------------------------------------------------------

// rpcError extracts an "error" string from an RPC result map. Returns empty
// string if absent.
func rpcError(m map[string]any) string {
	if errMsg, ok := m["error"].(string); ok {
		return errMsg
	}
	return ""
}

// confirmDelete prompts the user to confirm a destructive action unless force
// is set. Returns true if the action should proceed.
func confirmDelete(reader *bufio.Reader, label, id string, force bool) bool {
	if force {
		return true
	}
	fmt.Printf("delete %s %s? this cannot be undone. [y/N] ", label, id)
	line, _ := reader.ReadString('\n')
	line = strings.TrimSpace(strings.ToLower(line))
	return line == "y" || line == "yes"
}

// severityColor renders a severity string with lipgloss coloring.
// critical=red, warning=yellow, info=white, unknown=white.
func severityColor(sev string) string {
	switch strings.ToLower(sev) {
	case "critical", "high", "error":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(sev)
	case "warning", "medium":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(sev)
	case "info", "low":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("7")).Render(sev)
	default:
		return sev
	}
}

// healthColor renders a health string with lipgloss coloring.
// green, yellow, red.
func healthColor(h string) string {
	switch strings.ToLower(h) {
	case "green":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render(h)
	case "yellow":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("3")).Render(h)
	case "red":
		return lipgloss.NewStyle().Foreground(lipgloss.Color("1")).Render(h)
	default:
		return h
	}
}

// truncateID truncates long IDs for table display.
func truncateID(id string) string {
	if len(id) > 40 {
		return id[:37] + "..."
	}
	return id
}

// readDefinitionFile reads a JSON5 employee definition file and unmarshals it
// into v. This supports the same JSON5 syntax (comments, trailing commas,
// unquoted keys) used everywhere else in the project. Falls back to raw JSON
// if hujson standardization fails so strict-JSON definitions still work.
func readDefinitionFile(path string, v any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("failed to read definition file: %w", err)
	}
	if err := config.UnmarshalJSON5(data, v); err != nil {
		return fmt.Errorf("invalid JSON5 in definition file: %w", err)
	}
	return nil
}

// ---------------------------------------------------------------------------
// agents list
// ---------------------------------------------------------------------------

func newAgentsListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "list all employees",
		Long: `List all registered AI employees with their ID, role, status,
tier, drift score, daily cost, and last invocation time.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.list", map[string]any{})
			if err != nil {
				return fmt.Errorf("failed to list agents: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
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

			agentsList, ok := resultMap["agents"].([]any)
			if !ok || len(agentsList) == 0 {
				fmt.Println("no agents found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tROLE\tSTATUS\tTIER\tDRIFT\tDAILY_COST\tLAST_INVOCATION")

			for _, a := range agentsList {
				agent, ok := a.(map[string]any)
				if !ok {
					continue
				}

				id := truncateID(getStringOr(agent, "id", ""))
				role := getStringOr(agent, "role", "")
				status := getStringOr(agent, "status", "")
				tier := getStringOr(agent, "tier", "")
				drift := getStringOr(agent, "drift_score", "")
				dailyCost := getStringOr(agent, "daily_cost", "")
				lastInv := getStringOr(agent, "last_invocation", "")

				if drift == "" {
					drift = getStringOr(agent, "drift", "0")
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\t%s\n",
					id, role, status, tier, drift, dailyCost, lastInv)
			}

			w.Flush()
			fmt.Printf("\ntotal: %d agents\n", len(agentsList))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

// ---------------------------------------------------------------------------
// agents show
// ---------------------------------------------------------------------------

func newAgentsShowCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "show <id>",
		Short: "show employee details",
		Long:  "Show full employee definition: constitution, state, goals, and recent findings.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.get", map[string]any{"id": agentID})
			if err != nil {
				return fmt.Errorf("failed to get agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
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

			// Render detail view.
			fmt.Printf("ID:          %s\n", getStringOr(resultMap, "id", ""))
			fmt.Printf("Role:        %s\n", getStringOr(resultMap, "role", ""))
			fmt.Printf("Status:      %s\n", getStringOr(resultMap, "status", ""))
			fmt.Printf("Tier:        %s\n", getStringOr(resultMap, "tier", ""))
			if drift := getStringOr(resultMap, "drift_score", ""); drift != "" {
				fmt.Printf("Drift:       %s\n", drift)
			}
			if cost := getStringOr(resultMap, "daily_cost", ""); cost != "" {
				fmt.Printf("Daily cost:  %s\n", cost)
			}
			if lastInv := getStringOr(resultMap, "last_invocation", ""); lastInv != "" {
				fmt.Printf("Last run:    %s\n", lastInv)
			}
			fmt.Printf("Created:     %s\n", getStringOr(resultMap, "created_at", ""))
			fmt.Printf("Updated:     %s\n", getStringOr(resultMap, "updated_at", ""))

			// Constitution summary.
			if constitution, ok := resultMap["constitution"].(map[string]any); ok {
				fmt.Println("\nConstitution:")
				if role := getStringOr(constitution, "role", ""); role != "" {
					fmt.Printf("  Role:          %s\n", role)
				}
				if desc := getStringOr(constitution, "description", ""); desc != "" {
					fmt.Printf("  Description:   %s\n", desc)
				}
				if constraints, ok := constitution["constraints"].([]any); ok {
					fmt.Printf("  Constraints:   %d rules\n", len(constraints))
				}
				if goals, ok := constitution["goals"].([]any); ok {
					fmt.Printf("  Goals:         %d declared\n", len(goals))
				}
				if esc := getStringOr(constitution, "escalates_to", ""); esc != "" {
					fmt.Printf("  Escalates to:  %s\n", esc)
				}
			}

			// Goals summary.
			if goals, ok := resultMap["goals"].([]any); ok && len(goals) > 0 {
				fmt.Printf("\nGoals (%d):\n", len(goals))
				for _, g := range goals {
					goal, ok := g.(map[string]any)
					if !ok {
						continue
					}
					gid := truncateID(getStringOr(goal, "id", ""))
					title := getStringOr(goal, "title", "")
					health := getStringOr(goal, "health", "")
					fmt.Printf("  %s  %s  [%s]\n", gid, title, healthColor(health))
				}
			}

			// Recent findings summary.
			if findings, ok := resultMap["findings"].([]any); ok && len(findings) > 0 {
				fmt.Printf("\nRecent findings (%d):\n", len(findings))
				for _, f := range findings {
					finding, ok := f.(map[string]any)
					if !ok {
						continue
					}
					fid := truncateID(getStringOr(finding, "id", ""))
					sev := getStringOr(finding, "severity", "")
					rule := getStringOr(finding, "rule", "")
					fmt.Printf("  %s  %s  %s\n", fid, severityColor(sev), rule)
				}
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

// ---------------------------------------------------------------------------
// agents create
// ---------------------------------------------------------------------------

func newAgentsCreateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "create <definition.json>",
		Short: "create an employee from a definition file",
		Long: `Create a new AI employee from a JSON definition file.

The definition must include a constitution block. The daemon validates
the constitution before creating the employee; creation is refused if
the constitution is missing or invalid.

Example definition:
  {
    "role": "researcher",
    "constitution": {
      "role": "researcher",
      "description": "gathers and synthesizes information",
      "constraints": [{"rule": "never modify files"}],
      "goals": [{"id": "daily-brief", "title": "daily research brief"}],
      "escalates_to": "user",
      "tier": 2
    }
  }`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			defPath := args[0]

			var def map[string]any
			if err := readDefinitionFile(defPath, &def); err != nil {
				return err
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.create", def)
			if err != nil {
				return fmt.Errorf("failed to create agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			agentID := getStringOr(resultMap, "id", "unknown")
			role := getStringOr(resultMap, "role", "")
			fmt.Printf("created agent: %s (%s)\n", role, agentID)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// agents update
// ---------------------------------------------------------------------------

func newAgentsUpdateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "update <id> <definition.json>",
		Short: "update non-constitution employee fields",
		Long: `Update an existing AI employee's NON-CONSTITUTION fields from a JSON
definition file.

The update command modifies fields like triggers, model, prompt, tools,
and enabled status. Constitution changes (purpose, charter, constraints,
frozen fields, escalates_to, etc.) are REJECTED here — use
` + "`meept agents amend`" + ` to propose constitution amendments through
the plan signoff workflow.

The definition replaces the employee's non-constitution configuration.
If the definition includes a constitution block, the update fails with a
message directing you to the amend command.`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]
			defPath := args[1]

			var def map[string]any
			if err := readDefinitionFile(defPath, &def); err != nil {
				return err
			}

			def["id"] = agentID

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.update", def)
			if err != nil {
				return fmt.Errorf("failed to update agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("updated agent: %s\n", agentID)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// agents delete
// ---------------------------------------------------------------------------

func newAgentsDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <id>",
		Short: "delete an employee",
		Long:  "Permanently delete an AI employee. stops the employee and removes its definition.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]

			reader := bufio.NewReader(os.Stdin)
			if !confirmDelete(reader, "agent", agentID, force) {
				fmt.Println("cancelled.")
				return nil
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.delete", map[string]any{"id": agentID})
			if err != nil {
				return fmt.Errorf("failed to delete agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("deleted agent: %s\n", agentID)
			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "skip confirmation")
	return cmd
}

// ---------------------------------------------------------------------------
// agents pause
// ---------------------------------------------------------------------------

func newAgentsPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <id>",
		Short: "pause an employee",
		Long:  "Pause an AI employee. the employee stops executing until resumed.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.pause", map[string]any{"id": agentID})
			if err != nil {
				return fmt.Errorf("failed to pause agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("paused agent: %s\n", agentID)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// agents resume
// ---------------------------------------------------------------------------

func newAgentsResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <id>",
		Short: "resume a paused employee",
		Long:  "Resume a paused AI employee. this is the only un-pause path.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.resume", map[string]any{"id": agentID})
			if err != nil {
				return fmt.Errorf("failed to resume agent: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("resumed agent: %s\n", agentID)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// agents amend
// ---------------------------------------------------------------------------

func newAgentsAmendCmd() *cobra.Command {
	var field string

	cmd := &cobra.Command{
		Use:   "amend <id> --field=<key> <value>",
		Short: "propose a constitution amendment",
		Long: `Propose an amendment to an employee's CONSTITUTION.

The amendment is routed through the Plan signoff workflow. The field
path supports dotted notation for nested keys (e.g.
--field=constraints.risk_ceiling high). This is the ONLY way to modify
constitution fields (purpose, charter, constraints, frozen fields, etc.).

For non-constitution changes (triggers, model, prompt, tools), use
` + "`meept agents update`" + ` instead.

Examples:
  meept agents amend researcher --field=constraints.risk_ceiling high
  meept agents amend coder --field=escalates_to user`,
		Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]
			value := args[1]

			if field == "" {
				return fmt.Errorf("--field is required")
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("agents.amend", map[string]any{
				"id":    agentID,
				"field": field,
				"value": value,
			})
			if err != nil {
				return fmt.Errorf("failed to propose amendment: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			planID := getStringOr(resultMap, "plan_id", "")
			fmt.Printf("amendment proposed for %s: %s = %s\n", agentID, field, value)
			if planID != "" {
				fmt.Printf("plan id: %s (awaiting signoff)\n", planID)
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&field, "field", "", "constitution field path (dotted, e.g. constraints.risk_ceiling)")
	_ = cmd.MarkFlagRequired("field")
	return cmd
}

// ---------------------------------------------------------------------------
// agents migrate
// ---------------------------------------------------------------------------

func newAgentsMigrateCmd() *cobra.Command {
	var applyID string
	var showID string
	var listOnly bool

	cmd := &cobra.Command{
		Use:   "migrate",
		Short: "scan legacy bot definitions and propose constitutions",
		Long: `Scan ~/.meept/bots/*.json for legacy bot definitions and propose
AI employee constitutions for each.

Without flags, prints a table of all proposed constitutions.
  --list             Show all proposals (same as no flags, explicit).
  --show <bot-id>    Show a single proposal in detail (full constitution).
  --apply <bot-id>   Write the proposed constitution for the given bot ID.
  --apply (no arg)   Apply all proposals. Use --apply=* to apply all.

Examples:
  meept agents migrate                       # list all proposals
  meept agents migrate --list                 # same as above (explicit)
  meept agents migrate --show researcher      # view single proposal detail
  meept agents migrate --apply researcher     # apply one
  meept agents migrate --apply="*"            # apply all`,
		Args: cobra.MaximumNArgs(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			// --show <id>: fetch all proposals, find the one, print in detail.
			if showID != "" {
				rawResult, err := client.Call("agents.migrate", map[string]any{})
				if err != nil {
					return fmt.Errorf("failed to run migration: %w", err)
				}
				var resultMap map[string]any
				if err := json.Unmarshal(rawResult, &resultMap); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
				if errMsg := rpcError(resultMap); errMsg != "" {
					return fmt.Errorf("%s", errMsg)
				}
				proposals, ok := resultMap["proposals"].([]any)
				if !ok {
					return fmt.Errorf("no proposals returned")
				}
				for _, p := range proposals {
					prop, ok := p.(map[string]any)
					if !ok {
						continue
					}
					if getStringOr(prop, "bot_id", "") != showID {
						continue
					}
					// Print full detail.
					fmt.Printf("bot id:         %s\n", getStringOr(prop, "bot_id", ""))
					fmt.Printf("role:           %s\n", getStringOr(prop, "role", ""))
					fmt.Printf("proposed tier:  %s\n", getStringOr(prop, "proposed_tier", ""))
					fmt.Printf("source:         %s\n", getStringOr(prop, "source", ""))
					if goals, ok := prop["goals"].([]any); ok {
						fmt.Printf("goals (%d):\n", len(goals))
						for _, g := range goals {
							fmt.Printf("  - %v\n", g)
						}
					}
					if constraints, ok := prop["constraints"].([]any); ok {
						fmt.Printf("constraints (%d):\n", len(constraints))
						for _, c := range constraints {
							fmt.Printf("  - %v\n", c)
						}
					}
					if warnings, ok := prop["warnings"].([]any); ok && len(warnings) > 0 {
						fmt.Printf("warnings (%d):\n", len(warnings))
						for _, w := range warnings {
							fmt.Printf("  - %v\n", w)
						}
					}
					if constJSON, err := json.MarshalIndent(prop["constitution"], "", "  "); err == nil && len(constJSON) > 2 {
						fmt.Printf("\nproposed constitution:\n%s\n", constJSON)
					}
					fmt.Println("\nto apply: meept agents migrate --apply " + showID)
					return nil
				}
				return fmt.Errorf("no proposal found for bot id %q", showID)
			}

			// --apply <id>: apply for specific bot.
			if applyID != "" && applyID != "*" {
				rawResult, err := client.Call("agents.migrate", map[string]any{"apply": applyID})
				if err != nil {
					return fmt.Errorf("failed to run migration: %w", err)
				}
				var resultMap map[string]any
				if err := json.Unmarshal(rawResult, &resultMap); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
				if errMsg := rpcError(resultMap); errMsg != "" {
					return fmt.Errorf("%s", errMsg)
				}
				path := getStringOr(resultMap, "path", "")
				fmt.Printf("constitution written for %s\n", applyID)
				if path != "" {
					fmt.Printf("file: %s\n", path)
				}
				return nil
			}

			// --apply=*: apply ALL proposals.
			if applyID == "*" {
				// First fetch all proposals (dry-run).
				rawResult, err := client.Call("agents.migrate", map[string]any{})
				if err != nil {
					return fmt.Errorf("failed to fetch proposals: %w", err)
				}
				var resultMap map[string]any
				if err := json.Unmarshal(rawResult, &resultMap); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}
				if errMsg := rpcError(resultMap); errMsg != "" {
					return fmt.Errorf("%s", errMsg)
				}
				proposals, ok := resultMap["proposals"].([]any)
				if !ok || len(proposals) == 0 {
					fmt.Println("no legacy bot definitions found to migrate.")
					return nil
				}
				applied := 0
				for _, p := range proposals {
					prop, ok := p.(map[string]any)
					if !ok {
						continue
					}
					botID := getStringOr(prop, "bot_id", "")
					if botID == "" {
						continue
					}
					applyRaw, err := client.Call("agents.migrate", map[string]any{"apply": botID})
					if err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to apply %s: %v\n", botID, err)
						continue
					}
					var applyResult map[string]any
					if err := json.Unmarshal(applyRaw, &applyResult); err != nil {
						fmt.Fprintf(os.Stderr, "warning: failed to parse result for %s: %v\n", botID, err)
						continue
					}
					if errMsg := rpcError(applyResult); errMsg != "" {
						fmt.Fprintf(os.Stderr, "warning: failed to apply %s: %s\n", botID, errMsg)
						continue
					}
					path := getStringOr(applyResult, "path", "")
					fmt.Printf("constitution written for %s\n", botID)
					if path != "" {
						fmt.Printf("  file: %s\n", path)
					}
					applied++
				}
				fmt.Printf("\napplied: %d of %d proposals\n", applied, len(proposals))
				return nil
			}

			// No flags or --list: dry-run scan for all.
			// --list is explicitly acknowledged; same code path as no-flags.
			_ = listOnly
			params := map[string]any{}
			rawResult, err := client.Call("agents.migrate", params)
			if err != nil {
				return fmt.Errorf("failed to run migration: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			proposals, ok := resultMap["proposals"].([]any)
			if !ok || len(proposals) == 0 {
				fmt.Println("no legacy bot definitions found to migrate.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "BOT_ID\tROLE\tPROPOSED_TIER\tGOALS\tCONSTRAINTS\tSOURCE")

			for _, p := range proposals {
				prop, ok := p.(map[string]any)
				if !ok {
					continue
				}

				botID := truncateID(getStringOr(prop, "bot_id", ""))
				role := getStringOr(prop, "role", "")
				tier := getStringOr(prop, "proposed_tier", "")
				source := getStringOr(prop, "source", "")

				goalsCount := 0
				if goals, ok := prop["goals"].([]any); ok {
					goalsCount = len(goals)
				}
				constraintsCount := 0
				if constraints, ok := prop["constraints"].([]any); ok {
					constraintsCount = len(constraints)
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%d\t%s\n",
					botID, role, tier, goalsCount, constraintsCount, source)
			}

			w.Flush()
			fmt.Printf("\ntotal: %d proposals\n", len(proposals))
			fmt.Println("\nto apply: meept agents migrate --apply <bot-id>")
			fmt.Println("to view:  meept agents migrate --show <bot-id>")
			return nil
		},
	}

	cmd.Flags().StringVar(&applyID, "apply", "", "write proposed constitution for the given bot ID (use * for all)")
	cmd.Flags().StringVar(&showID, "show", "", "show a single proposal in detail")
	cmd.Flags().BoolVar(&listOnly, "list", false, "list all proposals (explicit form of no-flags)")
	return cmd
}

// ---------------------------------------------------------------------------
// agents goals
// ---------------------------------------------------------------------------

func newAgentsGoalsCmd() *cobra.Command {
	var employeeID string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "goals",
		Short: "list goals with health status",
		Long:  "List AI employee goals with health (green/yellow/red), active plan, and last assessment.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{}
			if employeeID != "" {
				params["employee"] = employeeID
			}

			rawResult, err := client.Call("agents.goals.list", params)
			if err != nil {
				return fmt.Errorf("failed to list goals: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
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

			goalsList, ok := resultMap["goals"].([]any)
			if !ok || len(goalsList) == 0 {
				fmt.Println("no goals found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tEMPLOYEE\tTITLE\tHEALTH\tACTIVE_PLAN\tLAST_ASSESSED")

			for _, g := range goalsList {
				goal, ok := g.(map[string]any)
				if !ok {
					continue
				}

				gid := truncateID(getStringOr(goal, "id", ""))
				emp := getStringOr(goal, "employee", "")
				title := getStringOr(goal, "title", "")
				health := getStringOr(goal, "health", "")
				activePlan := getStringOr(goal, "active_plan", "")
				lastAssessed := getStringOr(goal, "last_assessed", "")

				if len(title) > 40 {
					title = title[:37] + "..."
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					gid, emp, title, healthColor(health), activePlan, lastAssessed)
			}

			w.Flush()
			fmt.Printf("\ntotal: %d goals\n", len(goalsList))
			return nil
		},
	}

	cmd.Flags().StringVar(&employeeID, "employee", "", "filter by employee ID")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

// ---------------------------------------------------------------------------
// agents goal
// ---------------------------------------------------------------------------

func newAgentsGoalCmd() *cobra.Command {
	var approvePlanID string
	var rejectPlanID string
	var reason string

	cmd := &cobra.Command{
		Use:   "goal <goal-id>",
		Short: "show goal detail or approve/reject a plan",
		Long: `Show goal detail with active plan and history, or approve/reject
a plan associated with the goal.

Examples:
  meept agents goal daily-brief              # show goal detail
  meept agents goal daily-brief --approve plan-123
  meept agents goal daily-brief --reject plan-123 --reason="needs revision"`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			goalID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			// Approve a plan.
			if approvePlanID != "" {
				rawResult, err := client.Call("agents.goals.approve", map[string]any{
					"goal_id": goalID,
					"plan_id": approvePlanID,
					"by":      "cli",
				})
				if err != nil {
					return fmt.Errorf("failed to approve plan: %w", err)
				}

				var resultMap map[string]any
				if err := json.Unmarshal(rawResult, &resultMap); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}

				if errMsg := rpcError(resultMap); errMsg != "" {
					return fmt.Errorf("%s", errMsg)
				}

				fmt.Printf("approved plan %s for goal %s\n", approvePlanID, goalID)
				return nil
			}

			// Reject a plan.
			if rejectPlanID != "" {
				params := map[string]any{
					"goal_id": goalID,
					"plan_id": rejectPlanID,
					"by":      "cli",
				}
				if reason != "" {
					params["reason"] = reason
				}

				rawResult, err := client.Call("agents.goals.reject", params)
				if err != nil {
					return fmt.Errorf("failed to reject plan: %w", err)
				}

				var resultMap map[string]any
				if err := json.Unmarshal(rawResult, &resultMap); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}

				if errMsg := rpcError(resultMap); errMsg != "" {
					return fmt.Errorf("%s", errMsg)
				}

				fmt.Printf("rejected plan %s for goal %s\n", rejectPlanID, goalID)
				return nil
			}

			// Show goal detail.
			rawResult, err := client.Call("agents.goals.get", map[string]any{"id": goalID})
			if err != nil {
				return fmt.Errorf("failed to get goal: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("ID:            %s\n", getStringOr(resultMap, "id", ""))
			fmt.Printf("Title:         %s\n", getStringOr(resultMap, "title", ""))
			fmt.Printf("Employee:      %s\n", getStringOr(resultMap, "employee", ""))
			fmt.Printf("Health:        %s\n", healthColor(getStringOr(resultMap, "health", "")))
			if desc := getStringOr(resultMap, "description", ""); desc != "" {
				fmt.Printf("Description:   %s\n", desc)
			}
			if activePlan := getStringOr(resultMap, "active_plan", ""); activePlan != "" {
				fmt.Printf("Active plan:   %s\n", activePlan)
			}
			fmt.Printf("Last assessed: %s\n", getStringOr(resultMap, "last_assessed", ""))
			fmt.Printf("Created:       %s\n", getStringOr(resultMap, "created_at", ""))

			// History.
			if history, ok := resultMap["history"].([]any); ok && len(history) > 0 {
				fmt.Printf("\nHistory (%d):\n", len(history))
				for _, h := range history {
					entry, ok := h.(map[string]any)
					if !ok {
						continue
					}
					timestamp := getStringOr(entry, "at", "")
					event := getStringOr(entry, "event", "")
					detail := getStringOr(entry, "detail", "")
					fmt.Printf("  %s  %s  %s\n", timestamp, event, detail)
				}
			}

			return nil
		},
	}

	cmd.Flags().StringVar(&approvePlanID, "approve", "", "approve a plan for this goal")
	cmd.Flags().StringVar(&rejectPlanID, "reject", "", "reject a plan for this goal")
	cmd.Flags().StringVar(&reason, "reason", "", "rejection reason (use with --reject)")
	return cmd
}

// ---------------------------------------------------------------------------
// agents audit
// ---------------------------------------------------------------------------

func newAgentsAuditCmd() *cobra.Command {
	var since string
	var resolveID string
	var resolveAs string

	cmd := &cobra.Command{
		Use:   "audit <id>",
		Short: "list audit findings or resolve one",
		Long: `List recent audit findings for an employee, or resolve a specific
finding.

Severity is color-coded: red=critical, yellow=warning, white=info.

Examples:
  meept agents audit researcher                       # recent findings
  meept agents audit researcher --since=24h           # last 24 hours
  meept agents audit researcher --resolve f-123 --as=false_positive`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			agentID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			// Resolve a finding.
			if resolveID != "" {
				if resolveAs == "" {
					return fmt.Errorf("--as is required with --resolve")
				}

				rawResult, err := client.Call("agents.audit.resolve", map[string]any{
					"agent_id":   agentID,
					"finding_id": resolveID,
					"resolution": resolveAs,
				})
				if err != nil {
					return fmt.Errorf("failed to resolve finding: %w", err)
				}

				var resultMap map[string]any
				if err := json.Unmarshal(rawResult, &resultMap); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}

				if errMsg := rpcError(resultMap); errMsg != "" {
					return fmt.Errorf("%s", errMsg)
				}

				fmt.Printf("resolved finding %s as: %s\n", resolveID, resolveAs)
				return nil
			}

			// List findings.
			params := map[string]any{"id": agentID}
			if since != "" {
				// Validate duration format.
				if _, err := time.ParseDuration(since); err != nil {
					return fmt.Errorf("invalid --since duration %q: %w (e.g. '24h', '7d')", since, err)
				}
				params["since"] = since
			}

			rawResult, err := client.Call("agents.audit.list", params)
			if err != nil {
				return fmt.Errorf("failed to list audit findings: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg := rpcError(resultMap); errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			findings, ok := resultMap["findings"].([]any)
			if !ok || len(findings) == 0 {
				fmt.Println("no audit findings found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tSEVERITY\tCHECKPOINT\tRULE\tDETECTED_AT\tRESOLUTION")

			for _, f := range findings {
				finding, ok := f.(map[string]any)
				if !ok {
					continue
				}

				fid := truncateID(getStringOr(finding, "id", ""))
				sev := getStringOr(finding, "severity", "")
				checkpoint := getStringOr(finding, "checkpoint", "")
				rule := getStringOr(finding, "rule", "")
				detected := getStringOr(finding, "detected_at", "")
				resolution := getStringOr(finding, "resolution", "")

				if len(rule) > 40 {
					rule = rule[:37] + "..."
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n",
					fid, severityColor(sev), checkpoint, rule, detected, resolution)
			}

			w.Flush()
			fmt.Printf("\ntotal: %d findings\n", len(findings))
			return nil
		},
	}

	cmd.Flags().StringVar(&since, "since", "", "only show findings newer than this duration (e.g. '24h', '7d')")
	cmd.Flags().StringVar(&resolveID, "resolve", "", "resolve a finding by ID")
	cmd.Flags().StringVar(&resolveAs, "as", "", "resolution type (e.g. 'false_positive', 'acknowledged', 'fixed')")
	return cmd
}
