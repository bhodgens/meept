// Command meept-lite is a lightweight TUI client for the Meept assistant.
// It provides a shell-like interactive prompt with built-in scrollback.
package main

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/lite"
	"github.com/caimlas/meept/internal/tui"
)

var (
	version = "0.1.0"

	// Global flags
	socketPath string
	stateDir   string
	debugFile  string // Empty = no debug, "-" = stderr, "filename" = file
)

func main() {
	homeDir, _ := os.UserHomeDir()
	defaultStateDir := filepath.Join(homeDir, ".meept")
	defaultSocket := filepath.Join(defaultStateDir, "meept.sock")

	var debugWriter io.WriteCloser

	rootCmd := &cobra.Command{
		PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
			// Configure logging based on debug flag
			if debugFile == "" {
				// No debug: discard all logs
				slog.SetDefault(slog.New(slog.NewTextHandler(io.Discard, nil)))
			} else {
				var output io.Writer
				if debugFile == "-" {
					output = os.Stderr
				} else {
					f, err := os.OpenFile(debugFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
					if err != nil {
						return fmt.Errorf("failed to open debug file: %w", err)
					}
					debugWriter = f
					output = f
				}
				slog.SetDefault(slog.New(slog.NewTextHandler(output, &slog.HandlerOptions{
					Level: slog.LevelDebug,
				})))
			}
			return nil
		},
		PersistentPostRunE: func(cmd *cobra.Command, args []string) error {
			if debugWriter != nil {
				return debugWriter.Close()
			}
			return nil
		},
		Use:   "meept-lite [message]",
		Short: "Lightweight TUI client for Meept",
		Long: `meept-lite is a lightweight terminal interface for Meept with shell-like editing.

Running 'meept-lite' without arguments launches the interactive TUI.
Running 'meept-lite "message"' sends a single message and prints the response.

Features:
  - Shell-like prompt with bash keybindings (Ctrl+Left/Right for word navigation)
  - Built-in scrollback navigation by response blocks
  - 2-line prompt with model/token info and input area
  - Keyboard-driven menu system (Ctrl+X or / at start of line)
  - Session and task management
  - Configuration editor integration

Examples:
  meept-lite                            # Interactive TUI
  meept-lite "What's the weather?"      # Single message
  echo "Hello" | meept-lite -           # Read from stdin`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE:         runLite,
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", defaultSocket, "Unix socket path")
	rootCmd.PersistentFlags().StringVarP(&stateDir, "state-dir", "d", defaultStateDir, "State directory")
	rootCmd.PersistentFlags().StringVar(&debugFile, "debug", "", "Enable debug output (--debug or --debug=file, use '-' for stderr)")
	rootCmd.PersistentFlags().Lookup("debug").NoOptDefVal = "debug.log"

	// Add subcommands
	rootCmd.AddCommand(newVersionCmd())

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("meept-lite version %s\n", version)
		},
	}
}

// runLite is the main entry point for the lite TUI.
func runLite(cmd *cobra.Command, args []string) error {
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

// runTUI launches the interactive lite TUI.
func runTUI(socketPath string) error {
	app := lite.NewApp(socketPath)
	p := tea.NewProgram(app,
		tea.WithAltScreen(),
		// Mouse capture disabled to allow terminal text selection
	)
	_, err := p.Run()
	return err
}

// sendSingleMessage sends a single message and prints the response.
func sendSingleMessage(socketPath, message string) error {
	// Use the shared RPC client from internal/tui
	client := tui.NewRPCClient(socketPath)

	if err := client.Connect(); err != nil {
		return fmt.Errorf("failed to connect to daemon: %w\n\nMake sure the daemon is running:\n  meept daemon start", err)
	}
	defer client.Close()

	// Generate a conversation ID for this single message
	conversationID := fmt.Sprintf("lite-cli-%d", os.Getpid())

	reply, err := client.Chat(message, conversationID)
	if err != nil {
		return fmt.Errorf("chat error: %w", err)
	}

	fmt.Println(reply)
	return nil
}

// getSocketPath returns the socket path, applying defaults if needed.
func getSocketPath() string {
	if socketPath != "" {
		return socketPath
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meept", "meept.sock")
}
