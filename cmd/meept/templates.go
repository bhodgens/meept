package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newTemplatesCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "templates",
		Short:   "Manage prompt templates",
		Long:    "List, inspect, and invoke prompt templates available to meept.",
		Aliases: []string{"template"},
	}

	cmd.AddCommand(newTemplatesListCmd())
	cmd.AddCommand(newTemplatesShowCmd())
	cmd.AddCommand(newTemplatesInvokeCmd())
	cmd.AddCommand(newTemplatesClearCmd())

	return cmd
}

func newTemplatesListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "List available templates",
		Long:  "List all templates discovered from the template directories.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			rawResult, err := c.Call("templates.list", nil)
			if err != nil {
				return fmt.Errorf("failed to list templates: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			// Check for error in response
			if errMsg, ok := resultMap["error"].(string); ok {
				return fmt.Errorf("%s", errMsg)
			}

			templatesList, ok := resultMap["templates"].([]any)
			if !ok {
				return fmt.Errorf("unexpected templates format")
			}

			if outputJSON {
				output, _ := json.MarshalIndent(resultMap, "", "  ")
				fmt.Println(string(output))
				return nil
			}

			if len(templatesList) == 0 {
				fmt.Println("No templates found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tDESCRIPTION\tSCOPE")

			for _, t := range templatesList {
				tmpl, ok := t.(map[string]any)
				if !ok {
					continue
				}

				name := getStringOr(tmpl, "name", "")
				desc := getStringOr(tmpl, "description", "")
				scope := getStringOr(tmpl, "scope", "turn")

				// Truncate description
				if len(desc) > 60 {
					desc = desc[:57] + "..."
				}

				fmt.Fprintf(w, "%s\t%s\t%s\n", name, desc, scope)
			}

			w.Flush()
			fmt.Printf("\nTotal: %d templates\n", len(templatesList))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newTemplatesShowCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "show <template-name>",
		Short: "Show template details",
		Long:  "Display detailed information about a specific template.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			templateName := args[0]

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]string{paramName: templateName}
			rawResult, err := c.Call("templates.get", params)
			if err != nil {
				return fmt.Errorf("failed to get template: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			// Check for error in response
			if errMsg, ok := resultMap["error"].(string); ok {
				return fmt.Errorf("%s", errMsg)
			}

			if outputJSON {
				output, _ := json.MarshalIndent(resultMap, "", "  ")
				fmt.Println(string(output))
				return nil
			}

			// Pretty print
			fmt.Printf("Name: %s\n", getStringOr(resultMap, "name", ""))
			fmt.Printf("Description: %s\n", getStringOr(resultMap, "description", "(none)"))
			fmt.Printf("Scope: %s\n", getStringOr(resultMap, "scope", "turn"))
			fmt.Printf("Path: %s\n", getStringOr(resultMap, "path", ""))
			fmt.Printf("Priority: %v\n", resultMap["priority"])

			if body, ok := resultMap["body"].(string); ok && body != "" {
				fmt.Printf("\n--- Template Body ---\n%s\n", body)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newTemplatesInvokeCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "invoke <template-name> [args...]",
		Short: "Invoke a template",
		Long: `Invoke a template with the given arguments.

The arguments are substituted into the template body, and the resulting
prompt is sent to the LLM for processing.

Examples:
  meept templates invoke summarize "Long text to summarize..."
  meept templates invoke translate "Spanish" "Hello world"
  meept templates invoke explain "some code here"
`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			templateName := args[0]
			templateArgs := args[1:]

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			fmt.Printf("Invoking template '%s'...\n", templateName)

			params := map[string]any{
				"name": templateName,
				"args": templateArgs,
			}
			rawResult, err := c.Call("templates.invoke", params)
			if err != nil {
				return fmt.Errorf("template invocation failed: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			// Check for error in response
			if errMsg, ok := resultMap["error"].(string); ok {
				return fmt.Errorf("%s", errMsg)
			}

			if outputJSON {
				output, _ := json.MarshalIndent(resultMap, "", "  ")
				fmt.Println(string(output))
				return nil
			}

			// Print result
			if content, ok := resultMap["content"].(string); ok && content != "" {
				fmt.Println("\n--- Result ---")
				fmt.Println(content)
			} else if prompt, ok := resultMap["prompt"].(string); ok {
				fmt.Println("\n--- Substituted Prompt ---")
				fmt.Println(prompt)
			}

			// Print token usage
			if model, ok := resultMap["model"].(string); ok {
				fmt.Printf("\nModel: %s\n", model)
			}
			if totalTokens, ok := resultMap["total_tokens"].(float64); ok && totalTokens > 0 {
				fmt.Printf("Tokens used: %.0f\n", totalTokens)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newTemplatesClearCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clear [conversation-id] [template-name]",
		Short: "Clear session-scoped templates",
		Long: `Clear session-scoped templates for a conversation.

If a template name is provided, only that template is deactivated.
Otherwise, all session-scoped templates for the conversation are cleared.

Examples:
  meept templates clear abc123                     # Clear all for conversation
  meept templates clear abc123 role-senior-dev     # Clear specific template
`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			conversationID := args[0]
			var templateName string
			if len(args) > 1 {
				templateName = strings.Join(args[1:], " ")
			}

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]any{
				"conversation_id": conversationID,
			}
			if templateName != "" {
				params["name"] = templateName
			}

			rawResult, err := c.Call("templates.clear", params)
			if err != nil {
				return fmt.Errorf("failed to clear templates: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			// Check for error in response
			if errMsg, ok := resultMap["error"].(string); ok {
				return fmt.Errorf("%s", errMsg)
			}

			if cleared, ok := resultMap["cleared"].([]any); ok && len(cleared) > 0 {
				names := make([]string, len(cleared))
				for i, n := range cleared {
					if s, ok := n.(string); ok {
						names[i] = s
					}
				}
				fmt.Printf("Cleared %d template(s): %s\n", len(names), strings.Join(names, ", "))
			} else {
				fmt.Println("No active templates to clear.")
			}

			return nil
		},
	}

	return cmd
}
