package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
	socket := getSocketPath()

	// Check if we have a message argument
	if len(args) == 0 {
		// No arguments - launch TUI
		return runTUI(socket)
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

	// Single message mode
	return sendSingleMessage(socket, message)
}

func runTUI(socketPath string) error {
	app := tui.NewApp(socketPath)
	p := tea.NewProgram(app,
		tea.WithAltScreen(),
		// No mouse capture - allows native terminal text selection.
		// Viewport scrolling via keyboard: j/k, arrows, PgUp/PgDn.
	)
	_, err := p.Run()
	return err
}

func sendSingleMessage(socketPath, message string) error {
	client := tui.NewRPCClient(socketPath)

	if err := client.Connect(); err != nil {
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
