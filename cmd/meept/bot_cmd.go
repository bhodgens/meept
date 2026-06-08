package main

import (
	"encoding/json"
	"fmt"
	"os"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

// ---------------------------------------------------------------------------
// Root command
// ---------------------------------------------------------------------------

func newBotsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bots",
		Short: "manage persistent bots",
		Long: `Create, list, pause, resume, and delete persistent autonomous bots.

Bots are long-running agents that execute recurring tasks on a schedule,
monitor resources, or respond to events autonomously.

Examples:
  meept bots list                          # List all bots
  meept bots show <bot-id>                 # Show bot details
  meept bots create definition.json        # Create from file
  meept bots delete <bot-id>               # Delete a bot
  meept bots pause <bot-id>                # Pause a running bot
  meept bots resume <bot-id>               # Resume a paused bot`,
	}

	cmd.AddCommand(newBotsListCmd())
	cmd.AddCommand(newBotsShowCmd())
	cmd.AddCommand(newBotsCreateCmd())
	cmd.AddCommand(newBotsDeleteCmd())
	cmd.AddCommand(newBotsPauseCmd())
	cmd.AddCommand(newBotsResumeCmd())

	return cmd
}

// ---------------------------------------------------------------------------
// bots list
// ---------------------------------------------------------------------------

func newBotsListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "list all bots",
		Long:  "List all registered bots with their ID, name, status, and enabled state.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("bot.list", map[string]any{})
			if err != nil {
				return fmt.Errorf("failed to list bots: %w", err)
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

			botsList, ok := resultMap["bots"].([]any)
			if !ok || len(botsList) == 0 {
				fmt.Println("No bots found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tSTATUS\tENABLED")

			for _, b := range botsList {
				bot, ok := b.(map[string]any)
				if !ok {
					continue
				}

				id := getStringOr(bot, "id", "")
				name := getStringOr(bot, "name", "")
				status := getStringOr(bot, "status", "")
				enabled := getStringOr(bot, "enabled", "true")

				// Truncate long IDs
				if len(id) > 40 {
					id = id[:37] + "..."
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, name, status, enabled)
			}

			w.Flush()
			fmt.Printf("\nTotal: %d bots\n", len(botsList))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

// ---------------------------------------------------------------------------
// bots show
// ---------------------------------------------------------------------------

func newBotsShowCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "show <bot-id>",
		Short: "show bot details",
		Long:  "Show detailed information about a specific bot.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			botID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("bot.get", map[string]any{"id": botID})
			if err != nil {
				return fmt.Errorf("failed to get bot: %w", err)
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

			fmt.Printf("ID:          %s\n", getStringOr(resultMap, "id", ""))
			fmt.Printf("Name:        %s\n", getStringOr(resultMap, "name", ""))
			if desc := getStringOr(resultMap, "description", ""); desc != "" {
				fmt.Printf("Description: %s\n", desc)
			}
			fmt.Printf("Status:      %s\n", getStringOr(resultMap, "status", ""))
			fmt.Printf("Enabled:     %s\n", getStringOr(resultMap, "enabled", "true"))
			if schedule := getStringOr(resultMap, "schedule", ""); schedule != "" {
				fmt.Printf("Schedule:    %s\n", schedule)
			}
			if agentID := getStringOr(resultMap, "agent_id", ""); agentID != "" {
				fmt.Printf("Agent:       %s\n", agentID)
			}
			if prompt := getStringOr(resultMap, "prompt", ""); prompt != "" {
				fmt.Printf("Prompt:      %s\n", prompt)
			}
			fmt.Printf("Created:     %s\n", getStringOr(resultMap, "created_at", ""))
			fmt.Printf("Updated:     %s\n", getStringOr(resultMap, "updated_at", ""))

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

// ---------------------------------------------------------------------------
// bots create
// ---------------------------------------------------------------------------

func newBotsCreateCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "create <definition.json>",
		Short: "create a bot from a definition file",
		Long: `Create a new bot from a JSON definition file.

The definition file should contain the bot configuration as a JSON object.
Example:
  {
    "name": "daily-standup",
    "description": "Generate daily standup summaries",
    "schedule": "0 9 * * 1-5",
    "agent_id": "analyst",
    "prompt": "Summarize yesterday's progress from memory",
    "enabled": true
  }`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			defPath := args[0]

			data, err := os.ReadFile(defPath)
			if err != nil {
				return fmt.Errorf("failed to read definition file: %w", err)
			}

			// Validate JSON
			var def map[string]any
			if err := json.Unmarshal(data, &def); err != nil {
				return fmt.Errorf("invalid JSON in definition file: %w", err)
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("bot.create", def)
			if err != nil {
				return fmt.Errorf("failed to create bot: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			botID := getStringOr(resultMap, "id", "unknown")
			botName := getStringOr(resultMap, "name", "")
			fmt.Printf("Created bot: %s (%s)\n", botName, botID)

			return nil
		},
	}

	return cmd
}

// ---------------------------------------------------------------------------
// bots delete
// ---------------------------------------------------------------------------

func newBotsDeleteCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "delete <bot-id>",
		Short: "delete a bot",
		Long:  "Permanently delete a bot by its ID.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			botID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("bot.delete", map[string]any{"id": botID})
			if err != nil {
				return fmt.Errorf("failed to delete bot: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Deleted bot: %s\n", botID)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// bots pause
// ---------------------------------------------------------------------------

func newBotsPauseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "pause <bot-id>",
		Short: "pause a running bot",
		Long:  "Pause a bot, preventing it from executing its schedule or responding to events.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			botID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("bot.pause", map[string]any{"id": botID})
			if err != nil {
				return fmt.Errorf("failed to pause bot: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Paused bot: %s\n", botID)
			return nil
		},
	}
}

// ---------------------------------------------------------------------------
// bots resume
// ---------------------------------------------------------------------------

func newBotsResumeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "resume <bot-id>",
		Short: "resume a paused bot",
		Long:  "Resume a paused bot, allowing it to execute its schedule and respond to events again.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			botID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("bot.resume", map[string]any{"id": botID})
			if err != nil {
				return fmt.Errorf("failed to resume bot: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Resumed bot: %s\n", botID)
			return nil
		},
	}
}
