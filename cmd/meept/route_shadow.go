package main

// route_shadow.go — `meept routing shadow` (sovereign-router-parity leaf
// 04, T11): the CLI surface of the read-only llm.route.shadow RPC. Prints
// the decision as JSON (--json, the repo's one-tree --json convention) or a
// lowercase text table. Report-only: the daemon answers with NO upstream
// call, no llm_calls row, no budget consumption, no alias-health or
// sticky-pin mutation (Contract C6).

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newRoutingShadowCmd() *cobra.Command {
	var (
		outputJSON bool
		alias      string
		prompt     string
		modelRef   string
	)

	cmd := &cobra.Command{
		Use:   "shadow",
		Short: "preview which model routing would pick (no tokens spent)",
		Long: `Ask the daemon which model it WOULD pick for a prompt, and what it would cost.

The shadow route runs the full selection pipeline (health filters, sticky
simulation, quality floor, cost estimate) and stops: no upstream call, no
llm_calls row, no budget consumption, no rotation or sticky-pin changes.

Examples:
  meept routing shadow --alias general --prompt "summarize this log"
  meept routing shadow --model-ref openai/gpt-4o-mini --prompt "hi" --json
`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if alias == "" && modelRef == "" {
				return fmt.Errorf("one of --alias or --model-ref is required")
			}

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]any{"prompt": prompt}
			if alias != "" {
				params["alias"] = alias
			}
			if modelRef != "" {
				params["model_ref"] = modelRef
			}

			rawResult, err := c.Call("llm.route.shadow", params)
			if err != nil {
				return fmt.Errorf("llm.route.shadow failed: %w", err)
			}

			var view struct {
				ModelID    string  `json:"model_id"`
				ProviderID string  `json:"provider_id"`
				Alias      string  `json:"alias"`
				Reason     string  `json:"reason"`
				EstInput   int     `json:"est_input_tokens"`
				EstOutput  int     `json:"est_output_tokens"`
				EstCostUSD float64 `json:"est_cost_usd"`
				Quality    int     `json:"quality"`
				Candidates []struct {
					ModelID    string  `json:"model_id"`
					ProviderID string  `json:"provider_id"`
					Quality    int     `json:"quality"`
					EstCostUSD float64 `json:"est_cost_usd"`
					Selected   bool    `json:"selected"`
					SkipReason string  `json:"skip_reason"`
				} `json:"candidates"`
			}
			if err := json.Unmarshal(rawResult, &view); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if outputJSON {
				out, err := json.MarshalIndent(view, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(out))
				return nil
			}

			fmt.Println("routing shadow (decision-only; nothing was spent)")
			if view.ModelID == "" {
				fmt.Printf("  pick:        none (%s)\n", view.Reason)
			} else {
				fmt.Printf("  pick:        %s/%s\n", view.ProviderID, view.ModelID)
				fmt.Printf("  reason:      %s\n", view.Reason)
				fmt.Printf("  est input:   %d tokens\n", view.EstInput)
				fmt.Printf("  est output:  %d tokens\n", view.EstOutput)
				fmt.Printf("  est cost:    $%.6f\n", view.EstCostUSD)
				fmt.Printf("  quality:     %d\n", view.Quality)
			}
			if len(view.Candidates) == 0 {
				fmt.Println("  candidates:  none")
				return nil
			}
			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "\tPROVIDER\tMODEL\tQUALITY\tEST COST\tPICK\tSKIP REASON")
			for _, c := range view.Candidates {
				pick := ""
				if c.Selected {
					pick = "yes"
				}
				fmt.Fprintf(w, "\t%s\t%s\t%d\t$%.6f\t%s\t%s\n",
					c.ProviderID, c.ModelID, c.Quality, c.EstCostUSD, pick, c.SkipReason)
			}
			w.Flush()
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as json")
	cmd.Flags().StringVar(&alias, "alias", "", "alias name to preview (or leave empty with --model-ref)")
	cmd.Flags().StringVar(&prompt, "prompt", "", "prompt text used for the token/cost estimate")
	cmd.Flags().StringVar(&modelRef, "model-ref", "", "provider/model ref to preview (takes precedence over --alias)")

	return cmd
}
