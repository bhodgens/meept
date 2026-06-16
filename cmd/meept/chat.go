package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tui"
	"github.com/caimlas/meept/pkg/id"
)

var (
	// chat command flags
	chatProject string
	chatNoFence bool
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
  meept chat "What time is it?"        # Single message
  echo "Hello" | meept chat -          # Read from stdin
  meept chat --project myapp           # Bind session to project
  meept chat --nofence                 # Disable path fencing`,
		Args: cobra.MaximumNArgs(1),
		RunE: runChat,
	}

	cmd.Flags().StringVar(&chatProject, "project", "", "bind session to named project")
	cmd.Flags().BoolVar(&chatNoFence, "nofence", false, "disable path fencing for this session")

	return cmd
}

func runChat(cmd *cobra.Command, args []string) error {
	// Check if we have a message argument
	if len(args) == 0 {
		// No arguments - launch TUI
		// TUI always uses RPC directly (it needs streaming/event RPC)
		return runTUI()
	}

	message := args[0]

	// Check for stdin input
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

	if strings.TrimSpace(message) == "" {
		return fmt.Errorf("empty message")
	}

	// Single message mode - uses transport.Client for --transport flag support
	client, err := connectDaemon()
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	// Generate a conversation ID for this single message.
	// Include a nanosecond timestamp so multiple `meept "msg"` invocations
	// from the same shell do not collide in the session store.
	conversationID := id.Generate("cli-")

	// If --project or --nofence are set, create a managed session and bind project
	if chatProject != "" || chatNoFence {
		sessParams := map[string]any{
			"name":     "cli-single",
			"no_fence": chatNoFence,
		}
		if chatProject != "" {
			sessParams["project_id"] = chatProject
		} else {
			// No project specified; let daemon auto-detect from cwd
			cwd, _ := os.Getwd()
			sessParams["detect_path"] = cwd
		}

		rawResult, err := client.Call("session.create", sessParams)
		if err != nil {
			return fmt.Errorf("failed to create session: %w", err)
		}

		var sessResult map[string]any
		if err := json.Unmarshal(rawResult, &sessResult); err != nil {
			return fmt.Errorf("failed to parse session response: %w", err)
		}

		if errMsg, ok := sessResult["error"].(string); ok && errMsg != "" {
			return fmt.Errorf("session create error: %s", errMsg)
		}

		// Use the created session's conversation ID
		if sid, ok := sessResult["id"].(string); ok && sid != "" {
			conversationID = sid
		}
	}

	reply, err := client.Chat(message, conversationID)
	if err != nil {
		return fmt.Errorf("%s", llm.UserMessage(err))
	}

	fmt.Println(reply)
	return nil
}

func runTUI() error {
	// The TUI requires RPC for event streaming and real-time updates.
	// If --transport=http is set, warn and fall back to RPC.
	if transportFlag == "http" {
		return fmt.Errorf("TUI does not yet support --transport=http; use the default RPC transport or the Flutter web UI")
	}
	app := tui.NewApp(getSocketPath())
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}
