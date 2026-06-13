package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/spf13/cobra"
)

func newSessionCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:     "session",
		Short:   "Manage chat sessions",
		Long:    "Manage chat sessions including creation, listing, attachment, and deletion.",
		Aliases: []string{"sessions"},
	}

	cmd.AddCommand(newSessionListCmd())
	cmd.AddCommand(newSessionCreateCmd())
	cmd.AddCommand(newSessionGetCmd())
	cmd.AddCommand(newSessionDeleteCmd())
	cmd.AddCommand(newSessionAttachCmd())
	cmd.AddCommand(newSessionDetachCmd())
	cmd.AddCommand(newSessionMessagesCmd())

	return cmd
}

func newSessionListCmd() *cobra.Command {
	var limit int
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "List sessions",
		Long:  "List all chat sessions.",
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			rawResult, err := client.Call("session.list", map[string]any{"limit": limit})
			if err != nil {
				return fmt.Errorf("failed to list sessions: %w", err)
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

			sessionsList, ok := resultMap["sessions"].([]any)
			if !ok || len(sessionsList) == 0 {
				fmt.Println("No sessions found.")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tNAME\tDESCRIPTION\tCREATED\tTASKS")

			for _, s := range sessionsList {
				session, ok := s.(map[string]any)
				if !ok {
					continue
				}

				id := getStringOr(session, "id", "")
				name := getStringOr(session, "name", "")
				desc := getStringOr(session, "description", "")
				created := getStringOr(session, "created_at", "")

				// Count linked tasks
				tasksCount := 0
				if tasks, ok := session["tasks"].([]any); ok {
					tasksCount = len(tasks)
				}

				// Truncate description (rune-aware)
				if len([]rune(desc)) > 40 {
					desc = string([]rune(desc)[:37]) + "..."
				}

				// Truncate created date
				if len(created) > 10 {
					created = created[:10]
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%d\n", id, name, desc, created, tasksCount)
			}

			if err := w.Flush(); err != nil {
				return err
			}
			fmt.Printf("\nTotal: %d sessions\n", len(sessionsList))
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of sessions to return")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newSessionCreateCmd() *cobra.Command {
	var description string

	cmd := &cobra.Command{
		Use:   "create <name>",
		Short: "Create a new session",
		Long:  "Create a new chat session with the given name.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]string{
				"name":        name,
				"description": description,
			}

			rawResult, err := client.Call("session.create", params)
			if err != nil {
				return fmt.Errorf("failed to create session: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			id := getStringOr(resultMap, "id", "")
			fmt.Printf("Created session: %s\n", id)
			if name := getStringOr(resultMap, "name", ""); name != "" {
				fmt.Printf("Name: %s\n", name)
			}
			if description := getStringOr(resultMap, "description", ""); description != "" {
				fmt.Printf("Description: %s\n", description)
			}

			return nil
		},
	}

	cmd.Flags().StringVarP(&description, "description", "d", "", "Session description")

	return cmd
}

func newSessionGetCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "get <session-id>",
		Short: "Get session details",
		Long:  "Get detailed information about a specific session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]string{"session_id": sessionID}
			rawResult, err := client.Call("session.get", params)
			if err != nil {
				return fmt.Errorf("failed to get session: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			session := resultMap

			fmt.Printf("ID:          %s\n", getStringOr(session, "id", ""))
			fmt.Printf("Name:        %s\n", getStringOr(session, "name", ""))
			if desc := getStringOr(session, "description", ""); desc != "" {
				fmt.Printf("Description: %s\n", desc)
			}
			fmt.Printf("Created:     %s\n", getStringOr(session, "created_at", ""))
			fmt.Printf("Updated:     %s\n", getStringOr(session, "updated_at", ""))

			if tasks, ok := session["tasks"].([]any); ok && len(tasks) > 0 {
				taskIDs := make([]string, 0, len(tasks))
				for _, t := range tasks {
					if tmap, ok := t.(map[string]any); ok {
						if id := getStringOr(tmap, "id", ""); id != "" {
							taskIDs = append(taskIDs, id)
						}
					}
				}
				fmt.Printf("Tasks:       %s\n", strings.Join(taskIDs, ", "))
			}

			return nil
		},
	}
}

func newSessionDeleteCmd() *cobra.Command {
	var force bool

	cmd := &cobra.Command{
		Use:   "delete <session-id>",
		Short: "Delete a session",
		Long:  "Delete a chat session and all associated messages.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			if !force {
				fmt.Printf("This will delete session '%s' and all associated messages. Are you sure? [y/N] ", sessionID)
				var confirm string
				fmt.Scanln(&confirm)
				if strings.ToLower(confirm) != "y" {
					fmt.Println("Cancelled.")
					return nil
				}
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]string{"session_id": sessionID}
			rawResult, err := client.Call("session.delete", params)
			if err != nil {
				return fmt.Errorf("failed to delete session: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Deleted session: %s\n", sessionID)
			return nil
		},
	}

	cmd.Flags().BoolVarP(&force, "force", "f", false, "Skip confirmation prompt")

	return cmd
}

func newSessionAttachCmd() *cobra.Command {
	var clientID string

	cmd := &cobra.Command{
		Use:   "attach <session-id>",
		Short: "Attach to a session",
		Long:  "Attach the current client to a session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			if clientID == "" {
				clientID = "cli"
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]string{
				"session_id": sessionID,
				"client_id":  clientID,
			}

			rawResult, err := client.Call("session.attach", params)
			if err != nil {
				return fmt.Errorf("failed to attach session: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Attached to session: %s\n", sessionID)
			return nil
		},
	}

	cmd.Flags().StringVarP(&clientID, "client-id", "c", "", "Client identifier")

	return cmd
}

func newSessionDetachCmd() *cobra.Command {
	var clientID string

	cmd := &cobra.Command{
		Use:   "detach <session-id>",
		Short: "Detach from a session",
		Long:  "Detach the current client from a session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			if clientID == "" {
				clientID = "cli"
			}

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]string{
				"session_id": sessionID,
				"client_id":  clientID,
			}

			rawResult, err := client.Call("session.detach", params)
			if err != nil {
				return fmt.Errorf("failed to detach session: %w", err)
			}

			var resultMap map[string]any
			if err := json.Unmarshal(rawResult, &resultMap); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("Detached from session: %s\n", sessionID)
			return nil
		},
	}

	cmd.Flags().StringVarP(&clientID, "client-id", "c", "", "Client identifier")

	return cmd
}

func newSessionMessagesCmd() *cobra.Command {
	var limit int
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "messages <session-id>",
		Short: "Get session messages",
		Long:  "Retrieve messages from a session.",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			sessionID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w", err)
			}
			defer client.Close()

			params := map[string]any{
				"session_id": sessionID,
				"offset":     0,
				"limit":      limit,
			}

			rawResult, err := client.Call("session.messages.get", params)
			if err != nil {
				return fmt.Errorf("failed to get session messages: %w", err)
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

			messages, ok := resultMap["messages"].([]any)
			if !ok || len(messages) == 0 {
				fmt.Println("No messages found.")
				return nil
			}

			for _, m := range messages {
				msg, ok := m.(map[string]any)
				if !ok {
					continue
				}

				role := getStringOr(msg, "role", "")
				content := getStringOr(msg, "content", "")
				timestamp := getStringOr(msg, "timestamp", "")

				roleBadge := "[user]"
				if role == "assistant" {
					roleBadge = "[assistant]"
				}

				fmt.Printf("\n%s %s\n", roleBadge, timestamp)
				fmt.Printf("%s\n", content)
			}

			fmt.Printf("\n%d messages\n", len(messages))
			return nil
		},
	}

	cmd.Flags().IntVarP(&limit, "limit", "n", 50, "Maximum number of messages to return")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}
