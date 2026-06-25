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
	cmd.AddCommand(newSkillsStatsCmd())
	cmd.AddCommand(newSkillsArchiveCmd())
	cmd.AddCommand(newSkillsRestoreCmd())
	cmd.AddCommand(newSkillsHistoryCmd())
	cmd.AddCommand(newSkillsEvolveCmd())

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
				if len([]rune(desc)) > 50 {
					desc = string([]rune(desc)[:47]) + "..."
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

func newSkillsStatsCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "stats [skill-name]",
		Short: "show skill usage statistics",
		Long: `Show usage statistics for a specific skill or all skills.

Without an argument, shows statistics for all tracked skills.
With a skill name, shows detailed statistics for that skill.

Examples:
  meept skills stats
  meept skills stats debug-systematically
`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			skillName := ""
			if len(args) > 0 {
				skillName = args[0]
			}

			params := map[string]string{"name": skillName}
			rawResult, err := c.Call("skills.stats", params)
			if err != nil {
				return fmt.Errorf("failed to get skill stats: %w", err)
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

			// Pretty print single skill
			if skillName != "" {
				fmt.Printf("skill: %s\n", getStringOr(resultMap, "skill_name", skillName))
				fmt.Printf("inject count:    %v\n", resultMap["inject_count"])
				fmt.Printf("positive count:  %v\n", resultMap["positive_count"])
				fmt.Printf("negative count:  %v\n", resultMap["negative_count"])
				fmt.Printf("neutral count:   %v\n", resultMap["neutral_count"])
				fmt.Printf("effectiveness:   %v\n", resultMap["effectiveness"])
				return nil
			}

			// Pretty print all skills
			statsMap, ok := resultMap["stats"].(map[string]any)
			if !ok || len(statsMap) == 0 {
				fmt.Println("no skill usage data available.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SKILL\tINJECTS\tPOSITIVE\tNEGATIVE\tEFFECTIVENESS")

			for _, v := range statsMap {
				s, ok := v.(map[string]any)
				if !ok {
					continue
				}
				fmt.Fprintf(w, "%s\t%v\t%v\t%v\t%v\n",
					getStringOr(s, "skill_name", "?"),
					s["inject_count"],
					s["positive_count"],
					s["negative_count"],
					s["effectiveness"],
				)
			}
			w.Flush()
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

func newSkillsArchiveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "archive <skill-name>",
		Short: "archive a skill (move to archived directory)",
		Long: `Archive a skill by moving it from the skills directory to the
archived directory. The skill is unregistered from the live registry.

Examples:
  meept skills archive debug-systematically
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := args[0]

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]string{"name": skillName}
			_, err = c.Call("skills.archive", params)
			if err != nil {
				return fmt.Errorf("failed to archive skill: %w", err)
			}

			fmt.Printf("archived skill: %s\n", skillName)
			return nil
		},
	}
	return cmd
}

func newSkillsRestoreCmd() *cobra.Command {
	var version int

	cmd := &cobra.Command{
		Use:   "restore <skill-name>",
		Short: "restore an archived skill or a prior version",
		Long: `Restore a skill from the archived directory back to the live
skills directory. The skill is re-registered in the live registry.

With --version=N, instead restores the skill content from version bundle
v<N> (recorded by the Versioner), overwriting the live SKILL.md.

Examples:
  meept skills restore debug-systematically
  meept skills restore debug-systematically --version=3
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := args[0]

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]any{"name": skillName}
			if version > 0 {
				params["version"] = version
			}

			rawResult, err := c.Call("skills.restore", params)
			if err != nil {
				return fmt.Errorf("failed to restore skill: %w", err)
			}

			// Parse result to differentiate archive-restore vs version-restore.
			var resultMap map[string]any
			_ = json.Unmarshal(rawResult, &resultMap)

			if version > 0 {
				fmt.Printf("restored skill %s to version %d\n", skillName, version)
			} else {
				fmt.Printf("restored skill: %s\n", skillName)
			}
			return nil
		},
	}

	cmd.Flags().IntVar(&version, "version", 0, "restore a specific version (from version bundles) instead of un-archiving")
	return cmd
}

func newSkillsHistoryCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "history <skill-name>",
		Short: "show version history for a skill",
		Long: `Show the versioned snapshot history for a skill.

Each entry shows the version number, content SHA-256, timestamp, and action.
Versioned snapshots are captured automatically before each overwrite when a
Versioner is wired into the Writer.

Examples:
  meept skills history debug-systematically
  meept skills history debug-systematically --json
`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			skillName := args[0]

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]string{"name": skillName}
			rawResult, err := c.Call("skills.history", params)
			if err != nil {
				return fmt.Errorf("failed to get skill history: %w", err)
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

			entries, ok := resultMap["entries"].([]any)
			if !ok || len(entries) == 0 {
				fmt.Printf("no version history for skill: %s\n", skillName)
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "VERSION\tCONTENT_SHA\tTIMESTAMP\tACTION")

			for _, e := range entries {
				entry, ok := e.(map[string]any)
				if !ok {
					continue
				}
				version := entry["version"]
				sha := getStringOr(entry, "content_sha", "")
				if len(sha) > 12 {
					sha = sha[:12]
				}
				ts := getStringOr(entry, "timestamp", "")
				action := getStringOr(entry, "action", "")
				fmt.Fprintf(w, "v%v\t%s\t%s\t%s\n", version, sha, ts, action)
			}
			w.Flush()
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}

func newSkillsEvolveCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "evolve",
		Short: "run a skill evolution cycle",
		Long: `Run one full skill-evolution cycle (refine, promote, prune) synchronously.

The cycle respects the configured skills.evolver.auto_apply flag:
when false, proposals are emitted as plans for operator approval
rather than applied directly.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			fmt.Println("Running skill evolution cycle...")

			rawResult, err := c.Call("skills.evolve", nil)
			if err != nil {
				return fmt.Errorf("skills.evolve failed: %w", err)
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

			fmt.Println("\n--- Evolution Report ---")
			fmt.Printf("Refined:  %v\n", resultMap["refined"])
			fmt.Printf("Promoted: %v\n", resultMap["promoted"])
			fmt.Printf("Pruned:   %v\n", resultMap["pruned"])
			fmt.Printf("Skipped:  %v\n", resultMap["skipped"])
			fmt.Printf("Rejected: %v\n", resultMap["rejected"])
			fmt.Printf("Planned:  %v\n", resultMap["planned"])

			if details, ok := resultMap["details"].([]any); ok && len(details) > 0 {
				fmt.Println("\nProposals:")
				w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
				fmt.Fprintln(w, "ACTION\tSKILL\tRATIONALE")
				for _, d := range details {
					detail, ok := d.(map[string]any)
					if !ok {
						continue
					}
					action := getStringOr(detail, "action", "?")
					skill := getStringOr(detail, "skill_name", "?")
					rationale := getStringOr(detail, "rationale", "")
					if len([]rune(rationale)) > 60 {
						rationale = string([]rune(rationale)[:57]) + "..."
					}
					fmt.Fprintf(w, "%s\t%s\t%s\n", action, skill, rationale)
				}
				w.Flush()
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "output as JSON")
	return cmd
}
