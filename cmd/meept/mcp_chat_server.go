package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/mcp"
)

func newMCPChatServerCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "mcp-chat-server",
		Short: "run the mcp chat server (for external agent platforms)",
		Long: `Run the MCP chat server for external agent platforms (Claude, etc.).

Communicates via MCP protocol over stdin/stdout (JSON-RPC).
Connects to the meept daemon via Unix socket RPC.

Configuration is read from ~/.meept/meept.json5.

Register with Claude Code by adding to ~/.claude/settings.json:
{
  "mcpServers": {
    "meept": {
      "command": "meept",
      "args": ["mcp-chat-server"]
    }
  }
}`,
		RunE: runMCPChatServer,
	}
}

func runMCPChatServer(cmd *cobra.Command, args []string) error {
	socketPath := getSocketPath()

	srv := mcp.NewServer(os.Stdin, os.Stdout, nil)

	// Connect to daemon and subscribe to event topics
	subID, err := srv.ConnectAndSubscribe(socketPath)
	if err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}

	// Log subscription info to stderr (stdout is MCP protocol)
	fmt.Fprintf(os.Stderr, "meept mcp-chat-server: connected (subscription: %s)\n", subID)

	// Run the MCP message loop (blocks until stdin closes)
	if err := srv.Run(); err != nil {
		return fmt.Errorf("mcp server error: %w", err)
	}
	return nil
}
