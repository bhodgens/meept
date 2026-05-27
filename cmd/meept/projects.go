package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newProjectsCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "projects",
		Short:   "Manage projects",
		Long:    `Manage registered projects including listing, adding, removing, syncing, and status.`,
		Aliases: []string{"project"},
	}

	cmd.AddCommand(newProjectsListCmd())
	cmd.AddCommand(newProjectsAddCmd())
	cmd.AddCommand(newProjectsRemoveCmd())
	cmd.AddCommand(newProjectsSyncCmd())
	cmd.AddCommand(newProjectsStatusCmd())

	return cmd
}

func newProjectsListCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List all registered projects",
		Long:  `List all registered projects with mode, branch, and status.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("project.list", map[string]any{})
			if err != nil {
				return fmt.Errorf("failed to list projects: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if outputJSON {
				output, err := json.MarshalIndent(resultMap, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			projectsList, ok := resultMap["projects"].([]any)
			if !ok || len(projectsList) == 0 {
				fmt.Println("No projects registered.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "NAME\tMODE\tBRANCH\tSTATUS\tPATH")

			for _, p := range projectsList {
				proj, ok := p.(map[string]any)
				if !ok {
					continue
				}

				name := getStringOr(proj, "name", "")
				mode := getStringOr(proj, "mode", "")
				branch := getStringOr(proj, "branch", "")
				status := getStringOr(proj, "status", "")
				localPath := getStringOr(proj, "local_path", "")

				// Truncate path
				if len(localPath) > 50 {
					localPath = "..." + localPath[len(localPath)-47:]
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", name, mode, branch, status, localPath)
			}

			w.Flush()
			fmt.Printf("\nTotal: %d projects\n", len(projectsList))
			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newProjectsAddCmd() *cobra.Command {
	var projectName string

	cmd := &cobra.Command{
		Use:   "add <path-or-url>",
		Short: "Register a project",
		Long: `Register a new project with the daemon.

Accepts either a local filesystem path or a git URL.

Examples:
  meept projects add /home/user/myapp --name myapp
  meept projects add https://github.com/org/repo.git --name repo`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			source := args[0]

			if projectName == "" {
				// Derive name from path/URL
				parts := strings.Split(strings.TrimRight(source, "/"), "/")
				projectName = parts[len(parts)-1]
				// Strip .git suffix
				projectName = strings.TrimSuffix(projectName, ".git")
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{
				"name": projectName,
			}

			// Determine if source is a git URL or local path
			if strings.HasPrefix(source, "http://") || strings.HasPrefix(source, "https://") || strings.HasPrefix(source, "git@") || strings.HasPrefix(source, "ssh://") {
				params["git_url"] = source
			} else {
				params["local_path"] = source
			}

			rawResult, err := client.Call("project.register", params)
			if err != nil {
				return fmt.Errorf("failed to register project: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			id := getStringOr(resultMap, "id", "")
			mode := getStringOr(resultMap, "mode", "")
			fmt.Printf("Registered project: %s (id: %s, mode: %s)\n", projectName, id, mode)
			return nil
		},
	}

	cmd.Flags().StringVar(&projectName, "name", "", "Project name (default: derived from path/URL)")

	return cmd
}

func newProjectsRemoveCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "remove <name-or-id>",
		Short: "Unregister a project",
		Long:  `Remove a project registration from the daemon.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("project.unregister", map[string]any{"id": id})
			if err != nil {
				return fmt.Errorf("failed to unregister project: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Unregistered project: %s\n", id)
			return nil
		},
	}

	return cmd
}

func newProjectsSyncCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "sync <name-or-id>",
		Short: "Pull latest for a project",
		Long:  `Synchronize a git-based project by pulling the latest changes.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("project.sync", map[string]any{"id": id})
			if err != nil {
				return fmt.Errorf("failed to sync project: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Synced project: %s\n", id)
			return nil
		},
	}

	return cmd
}

func newProjectsStatusCmd() *cobra.Command {
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "status <name-or-id>",
		Short: "Show git status for a project",
		Long:  `Show the git status of a registered project including branch, dirty state, and sync info.`,
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			id := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("project.status", map[string]any{"id": id})
			if err != nil {
				return fmt.Errorf("failed to get project status: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if outputJSON {
				output, err := json.MarshalIndent(resultMap, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			branch := getStringOr(resultMap, "branch", "unknown")
			dirty := resultMap["dirty"]
			ahead := resultMap["ahead"]
			behind := resultMap["behind"]
			modifiedFiles := resultMap["modified_files"]

			fmt.Printf("Project: %s\n", id)
			fmt.Printf("Branch:  %s\n", branch)
			if dirty != nil {
				fmt.Printf("Dirty:   %v\n", dirty)
			}
			if ahead != nil {
				fmt.Printf("Ahead:   %v\n", ahead)
			}
			if behind != nil {
				fmt.Printf("Behind:  %v\n", behind)
			}
			if modifiedFiles != nil {
				fmt.Printf("Modified files: %v\n", modifiedFiles)
			}

			return nil
		},
	}

	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}
