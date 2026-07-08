package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newRoutingCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "routing",
		Short: "inspect model routing decisions",
		Long: `Query the routing decision log persisted by the resolver.

Subcommands:
  meept routing recent [N]             Last N decisions (default 20)
  meept routing by-model <model-id>    Decisions for a specific model
`,
	}

	cmd.AddCommand(newRoutingRecentCmd())
	cmd.AddCommand(newRoutingByModelCmd())

	return cmd
}

func newRoutingRecentCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "recent [N]",
		Short: "show recent routing decisions",
		Long: `Show the most recent model routing decisions, newest-first.

A numeric positional argument overrides the default limit of 20.

Examples:
  meept routing recent
  meept routing recent 50
  meept routing recent --json
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			limit := 20
			if len(args) > 0 {
				var n int
				if _, err := fmt.Sscanf(args[0], "%d", &n); err == nil && n > 0 {
					limit = n
				}
			}

			params := map[string]any{"limit": limit}
			rawResult, err := c.Call("routing.recent", params)
			if err != nil {
				return fmt.Errorf("routing.recent failed: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
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

			decisions, ok := resultMap["decisions"].([]any)
			if !ok || len(decisions) == 0 {
				fmt.Println("no routing decisions recorded")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TIMESTAMP\tMODEL\tPROVIDER\tALIAS\tREASON\tSKILL")
			for _, d := range decisions {
				dec, ok := d.(map[string]any)
				if !ok {
					continue
				}
				ts := getStringOr(dec, "timestamp", "")
				model := getStringOr(dec, "chosen_model_id", "?")
				provider := getStringOr(dec, "chosen_provider_id", "?")
				alias := getStringOr(dec, "alias", "")
				reason := getStringOr(dec, "reason", "")
				skill := getStringOr(dec, "skill", "")
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", ts, model, provider, alias, reason, skill)
			}
			w.Flush()
			fmt.Printf("\ntotal: %v decisions\n", resultMap["count"])
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

func newRoutingByModelCmd() *cobra.Command {
	var (
		outputJSON bool
		modelID    string
		limit      int
	)

	cmd := &cobra.Command{
		Use:   "by-model <model-id>",
		Short: "show routing decisions for a specific model",
		Long: `Show routing decisions filtered by chosen_model_id.

The model ID can be provided positionally or via --model-id.

Examples:
  meept routing by-model qwen2.5:7b
  meept routing by-model qwen2.5:7b --limit=50
  meept routing by-model --model-id=qwen2.5:7b --json
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			effectiveModel := modelID
			if len(args) > 0 {
				effectiveModel = args[0]
			}
			if effectiveModel == "" {
				return fmt.Errorf("model-id is required (positional arg or --model-id flag)")
			}

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]any{
				"model_id": effectiveModel,
			}
			if limit > 0 {
				params["limit"] = limit
			}

			rawResult, err := c.Call("routing.by_model", params)
			if err != nil {
				return fmt.Errorf("routing.by_model failed: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
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

			decisions, ok := resultMap["decisions"].([]any)
			if !ok || len(decisions) == 0 {
				fmt.Printf("no routing decisions found for model: %s\n", effectiveModel)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "TIMESTAMP\tMODEL\tPROVIDER\tALIAS\tREASON\tSKILL")
			for _, d := range decisions {
				dec, ok := d.(map[string]any)
				if !ok {
					continue
				}
				ts := getStringOr(dec, "timestamp", "")
				model := getStringOr(dec, "chosen_model_id", "?")
				provider := getStringOr(dec, "chosen_provider_id", "?")
				alias := getStringOr(dec, "alias", "")
				reason := getStringOr(dec, "reason", "")
				skill := getStringOr(dec, "skill", "")
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", ts, model, provider, alias, reason, skill)
			}
			w.Flush()
			fmt.Printf("\ntotal: %v decisions\n", resultMap["count"])
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	cmd.Flags().StringVar(&modelID, "model-id", "", "model ID to filter by (alternative to positional arg)")
	cmd.Flags().IntVar(&limit, "limit", 0, "max decisions to return (default 50)")

	return cmd
}
