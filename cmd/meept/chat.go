package main

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tui"
	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/pkg/id"
)

var (
	// chat command flags
	chatProject   string
	chatNoFence   bool
	chatSessionID string // target specific session by ID
	chatCwd       string // working directory for session
)

func newChatCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "chat [message]",
		Short: "Chat with Meept",
		Long: `Start a chat session with Meept.

Without arguments, launches the interactive TUI.
With a message argument, sends a single message and prints the response.

Examples:
  meept chat                           # Interactive TUI
  meept chat "What time is it?"        # Single message (uses oneshot_responses)
  meept chat --session session-abc "msg"  # Send to specific session
  meept chat --session session-abc     # Open TUI to specific session
  echo "Hello" | meept chat -          # Read from stdin
  meept chat --project myapp           # Bind session to project
  meept chat --nofence                 # Disable path fencing`,
		Args: cobra.MaximumNArgs(1),
		RunE: runChat,
	}

	cmd.Flags().StringVar(&chatProject, "project", "", "bind session to named project")
	cmd.Flags().BoolVar(&chatNoFence, "nofence", false, "disable path fencing for this session")
	cmd.Flags().StringVar(&chatSessionID, "session", "", "target specific session by ID")
	cmd.Flags().StringVar(&chatCwd, "cwd", "", "set working directory for session")

	return cmd
}

func runChat(cmd *cobra.Command, args []string) error {
	// Extract message from args (may be empty for TUI launch)
	var message string
	if len(args) > 0 {
		message = args[0]
	}

	// Handle stdin input
	if message == "-" {
		var sb strings.Builder
		scanner := bufio.NewScanner(os.Stdin)
		for scanner.Scan() {
			if sb.Len() > 0 {
				sb.WriteString("\n")
			}
			sb.WriteString(scanner.Text())
		}
		if err := scanner.Err(); err != nil {
			return fmt.Errorf("failed to read stdin: %w", err)
		}
		message = sb.String()
	}

	// Connect to daemon
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	// CASE 1: --session with message - append to session, print response, exit
	if chatSessionID != "" && message != "" {
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("empty message")
		}
		return chatWithSession(client, chatSessionID, message)
	}

	// CASE 2: --session without message - open TUI to that session
	if chatSessionID != "" && message == "" {
		return openTUIToSession(chatSessionID)
	}

	// CASE 3: No --session with message - use oneshot_responses
	if chatSessionID == "" && message != "" {
		if strings.TrimSpace(message) == "" {
			return fmt.Errorf("empty message")
		}
		if chatProject != "" || chatNoFence {
			fmt.Fprintf(os.Stderr, "Note: --project and --nofence are not yet supported for oneshot CLI chat; use the TUI or session-based chat.\n")
		}
		sessionID, err := getOrCreateOneshotSession(client)
		if err != nil {
			// Fallback to ephemeral session
			sessionID = id.Generate("cli-")
		}
		reply, err := client.Chat(context.Background(), message, sessionID)
		if err != nil {
			return fmt.Errorf("%s", llm.UserMessage(err))
		}
		fmt.Println(reply)
		return nil
	}

	// CASE 4: No args, no --session - open TUI to most recent
	return runTUI(chatCwd)
}

// getOrCreateOneshotSession finds the oneshot_responses session or creates it.
func getOrCreateOneshotSession(client transport.Client) (string, error) {
	// Try to find existing oneshot_responses session
	rawResult, err := client.Call("session.list", map[string]int{"limit": 100})
	if err != nil {
		return "", fmt.Errorf("failed to list sessions: %w", err)
	}

	var resultMap map[string]any
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return "", fmt.Errorf("failed to parse sessions: %w", err)
	}

	sessions, ok := resultMap["sessions"].([]any)
	if ok {
		for _, s := range sessions {
			if sess, ok := s.(map[string]any); ok {
				if name, ok := sess["name"].(string); ok && name == "oneshot_responses" {
					if sessID, ok := sess["id"].(string); ok {
						return sessID, nil
					}
				}
			}
		}
	}

	// Create oneshot_responses session if not found
	createResult, err := client.Call("session.create", map[string]string{
		"name": "oneshot_responses",
	})
	if err != nil {
		return "", fmt.Errorf("failed to create oneshot session: %w", err)
	}

	var createMap map[string]any
	if err := json.Unmarshal(createResult, &createMap); err != nil {
		return "", fmt.Errorf("failed to parse create response: %w", err)
	}

	if sessID, ok := createMap["id"].(string); ok && sessID != "" {
		return sessID, nil
	}

	return "", fmt.Errorf("failed to get session ID from create response")
}

// chatWithSession sends a message to an existing session after validating it exists.
func chatWithSession(client transport.Client, sessionID, message string) error {
	// Verify session exists
	getParams := map[string]string{"id": sessionID}
	rawResult, err := client.Call("session.get", getParams)
	if err != nil {
		return fmt.Errorf("failed to get session %s: %w", sessionID, err)
	}

	var resultMap map[string]any
	if err := json.Unmarshal(rawResult, &resultMap); err != nil {
		return fmt.Errorf("failed to parse session response: %w", err)
	}

	if errMsg, ok := resultMap["error"].(string); ok && errMsg != "" {
		return fmt.Errorf("session %q not found", sessionID)
	}

	// Session exists - send message
	reply, err := client.Chat(context.Background(), message, sessionID)
	if err != nil {
		return fmt.Errorf("%s", llm.UserMessage(err))
	}

	fmt.Println(reply)
	return nil
}

// openTUIToSession opens the TUI targeted to a specific session.
func openTUIToSession(sessionID string) error {
	return runTUIWithSession(chatCwd, sessionID)
}

func runTUI(cwd string) error {
	return runTUIWithSession(cwd, "")
}

func runTUIWithSession(cwd, sessionID string) error {
	// The TUI requires RPC for event streaming and real-time updates.
	// If --transport=http is set, warn and fall back to RPC.
	if transportFlag == "http" {
		return fmt.Errorf("TUI does not yet support --transport=http; use the default RPC transport or the Flutter web UI")
	}
	app := tui.NewApp(getSocketPath(), cwd)
	if sessionID != "" {
		app.SetTargetSession(sessionID)
	}
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}
