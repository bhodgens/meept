package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newClawSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "clawskills",
		Short:   "Manage third-party clawskills",
		Long:    "Discover, install, and manage community-contributed clawskills.",
		Aliases: []string{"clawskill"},
	}

	cmd.AddCommand(newClawSkillsSearchCmd())
	cmd.AddCommand(newClawSkillsInstallCmd())
	cmd.AddCommand(newClawSkillsListCmd())
	cmd.AddCommand(newClawSkillsUninstallCmd())
	cmd.AddCommand(newClawSkillsUpdateCmd())

	return cmd
}

func newClawSkillsSearchCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for clawskills",
		Long:  "Search the clawskills registry for skills matching a query.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := args[0]

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]string{"query": query}
			rawResult, err := c.Call("clawskills.search", params)
			if err != nil {
				return fmt.Errorf("failed to search clawskills: %w", err)
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

			skillsList, ok := resultMap["skills"].([]any)
			if !ok || len(skillsList) == 0 {
				fmt.Println("No clawskills found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tNAME\tDESCRIPTION\tVERSION\tRISK")

			for _, s := range skillsList {
				skill, ok := s.(map[string]any)
				if !ok {
					continue
				}

				slug := getStringOr(skill, "slug", "")
				name := getStringOr(skill, "name", "")
				desc := getStringOr(skill, "description", "")
				version := getStringOr(skill, "version", "")
				risk := getStringOr(skill, "risk_level", "medium")

				// Strip claw: prefix for display
				slug = strings.TrimPrefix(slug, "claw:")

				// Truncate description
				if len(desc) > 40 {
					desc = desc[:37] + "..."
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", slug, name, desc, version, risk)
			}

			w.Flush()
			fmt.Printf("\nTotal: %d clawskills found\n", len(skillsList))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newClawSkillsInstallCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "install <slug>",
		Short: "Install a clawskill",
		Long:  "Install a clawskill from the registry.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			// Add claw: prefix if not present
			if !strings.HasPrefix(slug, "claw:") {
				slug = "claw:" + slug
			}

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			fmt.Printf("Installing clawskill '%s'...\n", strings.TrimPrefix(slug, "claw:"))

			params := map[string]string{"slug": slug}
			rawResult, err := c.Call("clawskills.install", params)
			if err != nil {
				return fmt.Errorf("failed to install clawskill: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			name := getStringOr(resultMap, "name", "")
			version := getStringOr(resultMap, "version", "")
			path := getStringOr(resultMap, "install_path", "")

			fmt.Printf("Successfully installed %s v%s\n", name, version)
			if force {
				fmt.Printf("Location: %s\n", path)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&force, "force", false, "Show full installation path")

	return cmd
}

func newClawSkillsListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed clawskills",
		Long:  "List all installed clawskills.",
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			rawResult, err := c.Call("clawskills.list", nil)
			if err != nil {
				return fmt.Errorf("failed to list clawskills: %w", err)
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

			skillsList, ok := resultMap["skills"].([]any)
			if !ok || len(skillsList) == 0 {
				fmt.Println("No clawskills installed.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tNAME\tVERSION\tINSTALLED")

			for _, s := range skillsList {
				skill, ok := s.(map[string]any)
				if !ok {
					continue
				}

				slug := getStringOr(skill, "slug", "")
				name := getStringOr(skill, "name", "")
				version := getStringOr(skill, "version", "")
				installed := getStringOr(skill, "installed_at", "")

				// Strip claw: prefix for display
				slug = strings.TrimPrefix(slug, "claw:")

				// Truncate installed date
				if len(installed) > 10 {
					installed = installed[:10]
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", slug, name, version, installed)
			}

			w.Flush()
			fmt.Printf("\nTotal: %d clawskills installed\n", len(skillsList))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newClawSkillsUninstallCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "uninstall <slug>",
		Short: "Uninstall a clawskill",
		Long:  "Remove an installed clawskill.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			// Add claw: prefix if not present
			if !strings.HasPrefix(slug, "claw:") {
				slug = "claw:" + slug
			}

			if !force {
				fmt.Printf("This will remove clawskill '%s'. Are you sure? [y/N] ", strings.TrimPrefix(slug, "claw:"))
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(confirm) != "y" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			params := map[string]string{"slug": slug}
			rawResult, err := c.Call("clawskills.uninstall", params)
			if err != nil {
				return fmt.Errorf("failed to uninstall clawskill: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Successfully uninstalled %s\n", strings.TrimPrefix(slug, "claw:"))
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")

	return cmd
}

func newClawSkillsUpdateCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "update [slug]",
		Short: "Update a clawskill",
		Long:  "Update a clawskill to the latest version. Use --all to update all installed skills.",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			c, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer c.Close()

			if all && len(args) > 0 {
				return fmt.Errorf("cannot specify both --all and a slug")
			}

			if all {
				// List all installed and update each
				rawResult, err := c.Call("clawskills.list", nil)
				if err != nil {
					return fmt.Errorf("failed to list clawskills: %w", err)
				}

				var resultMap map[string]any
				if err := json.Unmarshal(rawResult, &resultMap); err != nil {
					return fmt.Errorf("failed to parse response: %w", err)
				}

				skillsList, _ := resultMap["skills"].([]any)
				if len(skillsList) == 0 {
					fmt.Println("No clawskills installed.")
					return nil
				}

				fmt.Printf("Updating %d clawskills...\n", len(skillsList))
				for _, s := range skillsList {
					skill, _ := s.(map[string]any)
					if skill == nil {
						continue
					}
					slug, _ := skill["slug"].(string)
					if slug == "" {
						continue
					}

					params := map[string]string{"slug": slug}
					rawResult, err := c.Call("clawskills.update", params)
					if err != nil {
						fmt.Printf("  ✗ %s: %v\n", strings.TrimPrefix(slug, "claw:"), err)
						continue
					}
					// Check for server-side errors in response body
					var updateResult map[string]any
					if json.Unmarshal(rawResult, &updateResult) == nil {
						if errMsg, ok := updateResult["error"].(string); ok && errMsg != "" {
							fmt.Printf("  ✗ %s: %s\n", strings.TrimPrefix(slug, "claw:"), errMsg)
							continue
						}
					}
					fmt.Printf("  ✓ %s updated\n", strings.TrimPrefix(slug, "claw:"))
				}
				return nil
			}

			if len(args) == 0 {
				return fmt.Errorf("specify a slug or use --all")
			}

			slug := args[0]
			if !strings.HasPrefix(slug, "claw:") {
				slug = "claw:" + slug
			}

			fmt.Printf("Updating clawskill '%s'...\n", strings.TrimPrefix(slug, "claw:"))

			params := map[string]string{"slug": slug}
			rawResult, err := c.Call("clawskills.update", params)
			if err != nil {
				return fmt.Errorf("failed to update clawskill: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			name := getStringOr(resultMap, "name", "")
			version := getStringOr(resultMap, "version", "")

			fmt.Printf("Successfully updated %s to v%s\n", name, version)
			return nil
		},
	}

	cmd.Flags().BoolVar(&all, "all", false, "Update all installed clawskills")

	return cmd
}

// getInstallDir returns the clawskills install directory.
func getInstallDir() (string, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(homeDir, ".meept", "clawskills"), nil
}
