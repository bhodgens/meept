package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "skills",
		Short:   "Manage skills",
		Long:    "List, inspect, and execute skills available to meept.",
		Aliases: []string{"skill"},
	}

	cmd.AddCommand(newSkillsListCmd())
	cmd.AddCommand(newSkillsShowCmd())
	cmd.AddCommand(newSkillsRunCmd())

	return cmd
}

func newSkillsListCmd() *cobra.Command {
	var (
		outputJSON bool
		filterTag  string
	)

	cmd := &cobra.Command{
		Use:   cmdList,
		Short: "List available skills",
		Long:  "List all skills discovered from the skill directories.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			rawResult, err := c.Call("skills.list", nil)
			if err != nil {
				return fmt.Errorf("failed to list skills: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			// Check for error in response
			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			skillsList, ok := resultMap["skills"].([]any)
			if !ok {
				return fmt.Errorf("unexpected skills format")
			}

			// Filter by tag if specified
			if filterTag != "" {
				filtered := make([]any, 0)
				for _, s := range skillsList {
					skill, ok := s.(map[string]any)
					if !ok {
						continue
					}
					if tags, ok := skill["tags"].([]any); ok {
						for _, t := range tags {
							if tagStr, ok := t.(string); ok && strings.EqualFold(tagStr, filterTag) {
								filtered = append(filtered, s)
								break
							}
						}
					}
				}
				skillsList = filtered
				resultMap["skills"] = skillsList
			}

			if outputJSON {
				output, err := json.MarshalIndent(resultMap, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			if len(skillsList) == 0 {
				fmt.Println("No skills found.")
				if filterTag != "" {
					fmt.Printf("(filtered by tag: %s)\n", filterTag)
				}
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tDESCRIPTION\tTAGS\tRISK")

			for _, s := range skillsList {
				skill, ok := s.(map[string]any)
				if !ok {
					continue
				}

				name := getStringOr(skill, "name", "")
				desc := getStringOr(skill, "description", "")
				risk := getStringOr(skill, "risk_level", "medium")

				// Truncate description
				if len(desc) > 50 {
					desc = desc[:47] + "..."
				}

				// Format tags
				var tagsStr string
				if tags, ok := skill["tags"].([]any); ok {
					tagStrs := make([]string, 0, len(tags))
					for _, t := range tags {
						if tagStr, ok := t.(string); ok {
							tagStrs = append(tagStrs, tagStr)
						}
					}
					tagsStr = strings.Join(tagStrs, ", ")
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", name, desc, tagsStr, risk)
			}

			w.Flush()
			fmt.Printf("\nTotal: %d skills\n", len(skillsList))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")
	cmd.Flags().StringVar(&filterTag, "tag", "", "Filter by tag")

	return cmd
}

func newSkillsShowCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "show <skill-name>",
		Short: "Show skill details",
		Long:  "Display detailed information about a specific skill.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := args[0]

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]string{paramName: skillName}
			rawResult, err := c.Call("skills.get", params)
			if err != nil {
				return fmt.Errorf("failed to get skill: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			// Check for error in response
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

			// Pretty print
			fmt.Printf("Name: %s\n", getStringOr(resultMap, "name", ""))
			fmt.Printf("Description: %s\n", getStringOr(resultMap, "description", "(none)"))
			fmt.Printf("Path: %s\n", getStringOr(resultMap, "path", ""))
			fmt.Printf("Risk Level: %s\n", getStringOr(resultMap, "risk_level", "medium"))
			fmt.Printf("Max Iterations: %v\n", resultMap["max_iterations"])

			if requires, ok := resultMap["requires"].([]any); ok && len(requires) > 0 {
				reqStrs := make([]string, 0, len(requires))
				for _, r := range requires {
					if rs, ok := r.(string); ok {
						reqStrs = append(reqStrs, rs)
					}
				}
				fmt.Printf("Requires: %s\n", strings.Join(reqStrs, ", "))
			}

			if tags, ok := resultMap["tags"].([]any); ok && len(tags) > 0 {
				tagStrs := make([]string, 0, len(tags))
				for _, t := range tags {
					if ts, ok := t.(string); ok {
						tagStrs = append(tagStrs, ts)
					}
				}
				fmt.Printf("Tags: %s\n", strings.Join(tagStrs, ", "))
			}

			if allowedTools, ok := resultMap["allowed_tools"].([]any); ok && len(allowedTools) > 0 {
				toolStrs := make([]string, 0, len(allowedTools))
				for _, t := range allowedTools {
					if ts, ok := t.(string); ok {
						toolStrs = append(toolStrs, ts)
					}
				}
				fmt.Printf("Allowed Tools: %s\n", strings.Join(toolStrs, ", "))
			}

			if examples, ok := resultMap["examples"].([]any); ok && len(examples) > 0 {
				fmt.Println("\nExamples:")
				for _, e := range examples {
					if es, ok := e.(string); ok {
						fmt.Printf("  - %s\n", es)
					}
				}
			}

			if body, ok := resultMap["body"].(string); ok && body != "" {
				fmt.Printf("\n--- Skill Instructions ---\n%s\n", body)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newSkillsRunCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "run <skill-name> [input]",
		Short: "Execute a skill",
		Long: `Execute a skill with the given input.

Examples:
  meept skills run code-review "Review my Python script"
  meept skills run summarize "Long text to summarize..."
`,
		Args: cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := args[0]
			input := ""
			if len(args) > 1 {
				input = strings.Join(args[1:], " ")
			}

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			fmt.Printf("Executing skill '%s'...\n", skillName)

			params := map[string]string{
				"name":  skillName,
				"input": input,
			}
			rawResult, err := c.Call("skills.execute", params)
			if err != nil {
				return fmt.Errorf("skill execution failed: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			// Check for error in response
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

			// Print result
			if content, ok := resultMap["content"].(string); ok {
				fmt.Println("\n--- Result ---")
				fmt.Println(content)
			}

			// Print token usage
			if model, ok := resultMap["model"].(string); ok {
				fmt.Printf("\nModel: %s\n", model)
			}
			if totalTokens, ok := resultMap["total_tokens"].(float64); ok {
				fmt.Printf("Tokens used: %.0f\n", totalTokens)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

// getStringOr returns a string value from a map or a default.
func getStringOr(m map[string]any, key, defaultVal string) string {
	if val, ok := m[key].(string); ok {
		return val
	}
	return defaultVal
}
