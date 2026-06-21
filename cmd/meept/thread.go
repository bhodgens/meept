package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/caimlas/meept/internal/agent"
	"github.com/spf13/cobra"
)

func newThreadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "thread",
		Short: "Manage conversation threads within a session",
		Long: `Manage conversation threads to partition context by topic.

Threads let you separate unrelated conversation topics within the same
session, preventing context bloat when switching between different
subjects.

Examples:
  meept thread new "code review"              # Create a thread with a custom topic
  meept thread new                            # Create a thread with auto-detected topic
  meept thread list                           # List all threads in the current session
  meept thread switch thread-id               # Switch to a different thread
  meept thread current                        # Show the current active thread
  meept thread delete thread-id               # Delete a thread`,
	}

	cmd.AddCommand(newThreadNewCmd())
	cmd.AddCommand(newThreadListCmd())
	cmd.AddCommand(newThreadSwitchCmd())
	cmd.AddCommand(newThreadCurrentCmd())
	cmd.AddCommand(newThreadDeleteCmd())

	return cmd
}

func newThreadNewCmd() *cobra.Command {
	var sessionID string
	var topicLabel string

	cmd := &cobra.Command{
		Use:   "new [topic-label]",
		Short: "create a new thread for the current session",
		Long: `Create a new conversation thread and activate it.

If a topic-label is provided, it is used as the thread's topic.
Otherwise, the topic is auto-detected from your input text (if you
also provide --input), or "general" is used as the default.`,
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
			}
			defer client.Close()

			if sessionID == "" {
				sessionID, err = resolveMostRecentSessionID(client)
				if err != nil {
					return err
				}
			}

			// Determine topic label
			if topicLabel == "" && len(args) > 0 {
				topicLabel = args[0]
			} else if topicLabel == "" && len(args) == 0 {
				// Use default topic for auto detection
				topicLabel = "general"
			} else if len(args) > 0 && topicLabel == "" {
				topicLabel = args[0]
			}

			// Auto-detect topic if not explicitly set and input provided
			if topicLabel == "" {
				input := strings.Join(args, " ")
				if input != "" {
				detector := agent.NewTopicDetector()
					topicLabel = detector.Detect(input)
				}
			}

			params := map[string]any{
				"session_id":     sessionID,
				"topic_label":    topicLabel,
				"conversation_id": sessionID,
			}

			result, err := client.Call("session.thread.new", params)
			if err != nil {
				return fmt.Errorf("failed to create thread: %w", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(result, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Println("created new thread")
			fmt.Printf("  id:        %s\n", getStringOr(resp, "id", ""))
			fmt.Printf("  topic:     %s\n", getStringOr(resp, "topic_label", ""))
			fmt.Printf("  session:   %s\n", getStringOr(resp, "session_id", ""))

			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")
	cmd.Flags().StringVar(&topicLabel, "label", "", "Topic label for the thread (auto-detected if not provided)")

	return cmd
}

func newThreadListCmd() *cobra.Command {
	var sessionID string
	var outputJSON bool

	cmd := &cobra.Command{
		Use:   "list",
		Short: "list all threads in the current session",
		Long:  `Display all threads in the current session, including the active thread and their topics.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
			}
			defer client.Close()

			if sessionID == "" {
				sessionID, err = resolveMostRecentSessionID(client)
				if err != nil {
					return err
				}
			}

			params := map[string]string{"session_id": sessionID}
			result, err := client.Call("session.thread.list", params)
			if err != nil {
				return fmt.Errorf("failed to list threads: %w", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(result, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			if outputJSON {
				output, err := json.MarshalIndent(resp, "", "  ")
				if err != nil {
					return fmt.Errorf("failed to marshal JSON: %w", err)
				}
				fmt.Println(string(output))
				return nil
			}

			threadsList, ok := resp["threads"].([]any)
			if !ok || len(threadsList) == 0 {
				fmt.Println("no threads found")
				return nil
			}

			w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
			fmt.Fprintln(w, "ID\tTOPIC\tACTIVE\tLAST ACTIVITY\tMESSAGES")

			for _, t := range threadsList {
				thread, ok := t.(map[string]any)
				if !ok {
					continue
				}

				id := getStringOr(thread, "id", "")
				topic := getStringOr(thread, "topic_label", "")
				createdAt := getStringOr(thread, "last_activity_at", "")
				isActiveVal := thread["is_active"]
				active := ""
				if b, ok := isActiveVal.(bool); ok && b {
					active = "*"
				}

				if len(createdAt) > 10 {
					createdAt = createdAt[:10]
				}
				if len(createdAt) > 19 {
					createdAt = createdAt[:19]
				}

				fmt.Fprintf(w, "%s\t%s\t%s\t%s\n", id, topic, active, createdAt)
			}

			if err := w.Flush(); err != nil {
				return err
			}
			fmt.Printf("\n%d thread(s)\n", len(threadsList))
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")
	cmd.Flags().BoolVar(&outputJSON, "json", false, "Output as JSON")

	return cmd
}

func newThreadSwitchCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "switch <thread-id>",
		Short: "switch to a different thread",
		Long: `Switch the active thread to the specified thread ID.
This deactivates the current thread and activates the target thread.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			threadID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
			}
			defer client.Close()

			if sessionID == "" {
				sessionID, err = resolveMostRecentSessionID(client)
				if err != nil {
					return err
				}
			}

			params := map[string]string{
				"session_id": sessionID,
				"thread_id":  threadID,
			}

			result, err := client.Call("session.thread.switch", params)
			if err != nil {
				return fmt.Errorf("failed to switch thread: %w", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(result, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			topic := getStringOr(resp, "topic_label", "")
			fmt.Printf("switched to thread: %s (%s)\n", threadID, topic)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")

	return cmd
}

func newThreadCurrentCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "current",
		Short: "show the current active thread",
		Long:  `Display details of the currently active thread for the session.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
			}
			defer client.Close()

			if sessionID == "" {
				sessionID, err = resolveMostRecentSessionID(client)
				if err != nil {
					return err
				}
			}

			params := map[string]string{"session_id": sessionID}
			result, err := client.Call("session.thread.current", params)
			if err != nil {
				return fmt.Errorf("failed to get current thread: %w", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(result, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			threadVal, ok := resp["thread"]
			if !ok || threadVal == nil {
				fmt.Println("no active thread")
				fmt.Println("create one with: meept thread new")
				return nil
			}

			thread, ok := threadVal.(map[string]any)
			if !ok {
				return fmt.Errorf("unexpected thread format")
			}

			fmt.Println("active thread")
			fmt.Println("=============")
			fmt.Printf("  id:            %s\n", getStringOr(thread, "id", ""))
			fmt.Printf("  topic:         %s\n", getStringOr(thread, "topic_label", ""))
			fmt.Printf("  conversation:  %s\n", getStringOr(thread, "conversation_id", ""))
			fmt.Printf("  created:       %s\n", getStringOr(thread, "created_at", ""))
			fmt.Printf("  last activity: %s\n", getStringOr(thread, "last_activity_at", ""))

			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")

	return cmd
}

func newThreadDeleteCmd() *cobra.Command {
	var sessionID string

	cmd := &cobra.Command{
		Use:   "delete <thread-id>",
		Short: "delete a thread",
		Long: `Delete a thread from the session.
If the active thread is deleted, you will need to switch to another thread.`,
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			threadID := args[0]

			client, err := connectDaemon()
			if err != nil {
				return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
			}
			defer client.Close()

			if sessionID == "" {
				sessionID, err = resolveMostRecentSessionID(client)
				if err != nil {
					return err
				}
			}

			params := map[string]string{
				"session_id": sessionID,
				"thread_id":  threadID,
			}

			result, err := client.Call("session.thread.delete", params)
			if err != nil {
				return fmt.Errorf("failed to delete thread: %w", err)
			}

			var resp map[string]any
			if err := json.Unmarshal(result, &resp); err != nil {
				return fmt.Errorf("failed to parse response: %w", err)
			}

			if errMsg, ok := resp["error"].(string); ok && errMsg != "" {
				return fmt.Errorf("%s", errMsg)
			}

			fmt.Printf("deleted thread: %s\n", threadID)
			return nil
		},
	}

	cmd.Flags().StringVar(&sessionID, "session", "", "Session ID (defaults to most recent)")

	return cmd
}
