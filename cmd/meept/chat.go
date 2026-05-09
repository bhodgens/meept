package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/tui"
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
  echo "Hello" | meept chat -          # Read from stdin`,
		Args: cobra.MaximumNArgs(1),
		RunE: runChat,
	}

	return cmd
}

func runChat(cmd *cobra.Command, args []string) error {
	// Check if we have a message argument
	if len(args) == 0 {
		// No arguments - launch TUI
		// TUI always uses RPC directly (it needs streaming/event RPC)
		return runTUI(getSocketPath())
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

	// Generate a conversation ID for this single message
	conversationID := fmt.Sprintf("cli-%d", os.Getpid())

	reply, err := client.Chat(message, conversationID)
	if err != nil {
		return fmt.Errorf("chat error: %w", err)
	}

	fmt.Println(reply)
	return nil
}

func runTUI(socketPath string) error {
	app := tui.NewApp(socketPath)
	p := tea.NewProgram(app)
	_, err := p.Run()
	return err
}
