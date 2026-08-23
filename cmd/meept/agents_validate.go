package main

import (
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/employee"
	"github.com/spf13/cobra"
)

// newAgentsValidateCmd implements `meept agents validate <definition.json5>`:
// offline pre-flight validation of an employee definition file. It decodes
// the constitution exactly as agents.create will (decodeConstitution via
// marshal-then-unmarshal) and runs Constitution.Validate — without needing
// a running daemon. This catches the common failure modes (numeric
// autonomy_tier, missing escalates_to, out-of-range tiers, bad trigger
// kinds) before a hire attempt.
func newAgentsValidateCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "validate <definition.json5>",
		Short: "validate an employee definition file without creating it",
		Long: `Offline pre-flight validation of an employee definition file.

Decodes the constitution block and runs the same structural validation the
daemon performs on 'agents create' — no daemon connection required. Reports:

  - JSON5 syntax and field-type errors (e.g. numeric autonomy_tier; the
    tier must be "tier_1_reactive" | "tier_2_propose" | "tier_3_autonomous")
  - Missing required fields (purpose, autonomy_tier, escalates_to)
  - Invalid escalation triggers, frozen fields, amendment policy

Exit code 0 = valid, 1 = invalid. Tool-name reference checks still require
the daemon's registries, so they run only at create time.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			defPath := args[0]

			var def struct {
				ID           string         `json:"id"`
				Constitution map[string]any `json:"constitution"`
			}
			if err := readDefinitionFile(defPath, &def); err != nil {
				return err
			}

			if def.ID == "" {
				return fmt.Errorf("definition is missing required field \"id\"")
			}
			if len(def.Constitution) == 0 {
				return fmt.Errorf("definition is missing the \"constitution\" block")
			}

			// Decode exactly as Manager.Hire does.
			data, err := json.Marshal(def.Constitution)
			if err != nil {
				return fmt.Errorf("constitution encode: %w", err)
			}
			var c employee.Constitution
			if err := json.Unmarshal(data, &c); err != nil {
				return fmt.Errorf("constitution decode: %w\n\nHint: autonomy_tier must be one of %q, %q, %q — numeric values are rejected",
					err,
					employee.Tier1Reactive, employee.Tier2Propose, employee.Tier3Autonomous)
			}

			if err := c.Validate(def.ID); err != nil {
				return fmt.Errorf("constitution invalid:\n  %w", err)
			}

			fmt.Printf("valid: %s (%s, tier=%s)\n", def.ID, c.Role, c.AutonomyTier)
			return nil
		},
	}
}
