package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/caimlas/meept/internal/clawskills"
	"github.com/spf13/cobra"
)

func newClawSkillsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "clawskills",
		Short: "Manage third-party skills from ClawHub",
		Long:  "Search, install, and manage third-party skills from the ClawHub registry.",
	}

	cmd.AddCommand(
		newClawSkillsSearchCmd(),
		newClawSkillsInstallCmd(),
		newClawSkillsUninstallCmd(),
		newClawSkillsListCmd(),
		newClawSkillsUpdateCmd(),
		newClawSkillsInfoCmd(),
		newClawSkillsInspectCmd(),
	)

	return cmd
}

func newClawSkillsSearchCmd() *cobra.Command {
	var limit int

	cmd := &cobra.Command{
		Use:   "search <query>",
		Short: "Search for skills on ClawHub",
		Args:  cobra.MinimumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			query := strings.Join(args, " ")

			client := clawskills.NewClient()
			defer client.Close()

			results, err := client.Search(context.Background(), query, limit)
			if err != nil {
				return fmt.Errorf("search failed: %w", err)
			}

			if len(results) == 0 {
				fmt.Println("No skills found matching your query.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tNAME\tVERSION\tDOWNLOADS\tVERIFIED")
			for _, r := range results {
				verified := ""
				if r.Verified {
					verified = "✓"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%d\t%s\n",
					r.Slug, r.Name, r.Version, r.Downloads, verified)
			}
			w.Flush()

			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 20, "Maximum number of results")

	return cmd
}

func newClawSkillsInstallCmd() *cobra.Command {
	var version string

	cmd := &cobra.Command{
		Use:   "install <slug>",
		Short: "Install a skill from ClawHub",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			client := clawskills.NewClient()
			defer client.Close()

			installer, err := clawskills.NewInstaller(client, clawskills.DefaultInstallerConfig(), nil)
			if err != nil {
				return fmt.Errorf("failed to create installer: %w", err)
			}
			defer installer.Close()

			installed, err := installer.Install(context.Background(), slug, version)
			if err != nil {
				return fmt.Errorf("installation failed: %w", err)
			}

			fmt.Printf("✓ Installed %s v%s\n", installed.Slug, installed.Version)
			fmt.Printf("  Path: %s\n", installed.Path)

			return nil
		},
	}

	cmd.Flags().StringVarP(&version, "version", "v", "", "Specific version to install (default: latest)")

	return cmd
}

func newClawSkillsUninstallCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "uninstall <slug>",
		Short: "Uninstall a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			client := clawskills.NewClient()
			defer client.Close()

			installer, err := clawskills.NewInstaller(client, clawskills.DefaultInstallerConfig(), nil)
			if err != nil {
				return fmt.Errorf("failed to create installer: %w", err)
			}
			defer installer.Close()

			if err := installer.Uninstall(slug); err != nil {
				return fmt.Errorf("uninstall failed: %w", err)
			}

			fmt.Printf("✓ Uninstalled %s\n", slug)
			return nil
		},
	}

	return cmd
}

func newClawSkillsListCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "list",
		Short: "List installed skills",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := clawskills.NewClient()
			defer client.Close()

			installer, err := clawskills.NewInstaller(client, clawskills.DefaultInstallerConfig(), nil)
			if err != nil {
				return fmt.Errorf("failed to create installer: %w", err)
			}
			defer installer.Close()

			skills := installer.List()

			if len(skills) == 0 {
				fmt.Println("No skills installed.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "SLUG\tVERSION\tINSTALLED\tAUTO-UPDATE\tVERIFIED")
			for _, s := range skills {
				autoUpdate := ""
				if s.AutoUpdate {
					autoUpdate = "✓"
				}
				verified := ""
				if s.Verified {
					verified = "✓"
				}
				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n",
					s.Slug, s.Version, s.InstalledAt.Format("2006-01-02"), autoUpdate, verified)
			}
			w.Flush()

			return nil
		},
	}

	return cmd
}

func newClawSkillsUpdateCmd() *cobra.Command {
	var all bool

	cmd := &cobra.Command{
		Use:   "update [slug]",
		Short: "Update a skill to the latest version",
		RunE: func(cmd *cobra.Command, args []string) error {
			client := clawskills.NewClient()
			defer client.Close()

			installer, err := clawskills.NewInstaller(client, clawskills.DefaultInstallerConfig(), nil)
			if err != nil {
				return fmt.Errorf("failed to create installer: %w", err)
			}
			defer installer.Close()

			ctx := context.Background()

			if all {
				updated, errors := installer.UpdateAll(ctx)
				if len(updated) > 0 {
					fmt.Println("Updated skills:")
					for _, slug := range updated {
						fmt.Printf("  ✓ %s\n", slug)
					}
				}
				if len(errors) > 0 {
					fmt.Println("Errors:")
					for _, err := range errors {
						fmt.Printf("  ✗ %v\n", err)
					}
				}
				if len(updated) == 0 && len(errors) == 0 {
					fmt.Println("No skills to update.")
				}
				return nil
			}

			if len(args) == 0 {
				return fmt.Errorf("specify a skill slug or use --all")
			}

			installed, err := installer.Update(ctx, args[0])
			if err != nil {
				return fmt.Errorf("update failed: %w", err)
			}

			fmt.Printf("✓ Updated %s to v%s\n", installed.Slug, installed.Version)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&all, "all", "a", false, "Update all skills with auto-update enabled")

	return cmd
}

func newClawSkillsInfoCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "info <slug>",
		Short: "Show information about a skill",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			client := clawskills.NewClient()
			defer client.Close()

			skill, err := client.SkillDetail(context.Background(), slug)
			if err != nil {
				return fmt.Errorf("failed to get skill info: %w", err)
			}

			fmt.Printf("Name:        %s\n", skill.Name)
			fmt.Printf("Slug:        %s\n", skill.Slug)
			fmt.Printf("Author:      %s\n", skill.Author)
			fmt.Printf("Version:     %s\n", skill.Version)
			fmt.Printf("Description: %s\n", skill.Description)
			fmt.Printf("Downloads:   %d\n", skill.Downloads)
			fmt.Printf("Stars:       %d\n", skill.Stars)
			fmt.Printf("Verified:    %v\n", skill.Verified)

			if len(skill.Tags) > 0 {
				fmt.Printf("Tags:        %s\n", strings.Join(skill.Tags, ", "))
			}

			if len(skill.Requirements) > 0 {
				fmt.Printf("Requires:    %s\n", strings.Join(skill.Requirements, ", "))
			}

			if len(skill.Capabilities) > 0 {
				fmt.Printf("Capabilities: %s\n", strings.Join(skill.Capabilities, ", "))
			}

			return nil
		},
	}

	return cmd
}

func newClawSkillsInspectCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "inspect <slug>",
		Short: "Show installed skill details",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			slug := args[0]

			client := clawskills.NewClient()
			defer client.Close()

			installer, err := clawskills.NewInstaller(client, clawskills.DefaultInstallerConfig(), nil)
			if err != nil {
				return fmt.Errorf("failed to create installer: %w", err)
			}
			defer installer.Close()

			skill := installer.Get(slug)
			if skill == nil {
				return fmt.Errorf("skill not installed: %s", slug)
			}

			fmt.Printf("Name:        %s\n", skill.Name)
			fmt.Printf("Slug:        %s\n", skill.Slug)
			fmt.Printf("Version:     %s\n", skill.Version)
			fmt.Printf("Path:        %s\n", skill.Path)
			fmt.Printf("Installed:   %s\n", skill.InstalledAt.Format("2006-01-02"))
			fmt.Printf("Auto-Update: %v\n", skill.AutoUpdate)
			fmt.Printf("Verified:    %v\n", skill.Verified)

			return nil
		},
	}
}
