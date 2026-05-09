// Command meept is the CLI entry point for the Meept assistant.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/version"
)

var (
	// Global flags
	socketPath    string
	stateDir      string
	debugFile     string // Empty = no debug, "-" = stderr, "filename" = file
	transportFlag string // "rpc" or "http"
	httpURLFlag   string // HTTP base URL (e.g. "http://localhost:8081")
)

// debugEnabled returns whether debug mode is active.
func debugEnabled() bool {
	return debugFile != ""
}

func main() {
	homeDir, _ := os.UserHomeDir()
	defaultStateDir := filepath.Join(homeDir, ".meept")
	defaultSocket := filepath.Join(defaultStateDir, "meept.sock")

	// We need to parse flags early to configure logging before command execution.
	// Cobra's PersistentPreRunE runs after flag parsing but before the command.
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
		Use:   "meept [message]",
		Short: "Meept AI assistant",
		Long: `Meept is an AI assistant with multi-agent task orchestration, memory, and skill capabilities.

Running 'meept' without arguments launches the interactive TUI.
Running 'meept "message"' sends a single message and prints the response.

Examples:
  meept                                  # Interactive TUI
  meept "What's the weather like?"       # Single message
  meept chat "message"                   # Explicit chat subcommand

Core Commands:
  meept status                           # Check daemon status
  meept daemon start/stop                # Manage daemon
  meept chat                             # Interactive chat

Multi-Agent Orchestration:
  meept task list/create/get/delete      # Manage background tasks
  meept queue status/list/retry          # View job queue
  meept workers                          # Manage worker pool

Memory & Skills:
  meept memory                           # Search memory
  meept jobs                             # View scheduled jobs
  meept config                           # View/edit configuration`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE:         runChat, // Default to chat when no subcommand
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", defaultSocket, "Unix socket path (for RPC)")
	rootCmd.PersistentFlags().StringVarP(&stateDir, "state-dir", "d", defaultStateDir, "State directory")
	rootCmd.PersistentFlags().StringVar(&debugFile, "debug", "", "Enable debug output (--debug or --debug=file, use '-' for stderr)")
	rootCmd.PersistentFlags().StringVar(&transportFlag, "transport", "rpc", "Transport: rpc or http")
	rootCmd.PersistentFlags().StringVar(&httpURLFlag, "http-url", "", "HTTP base URL for daemon (default: http://localhost:8081)")
	rootCmd.PersistentFlags().Lookup("debug").NoOptDefVal = "debug.log"

	// Add subcommands
	rootCmd.AddCommand(newChatCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newJobsCmd())
	rootCmd.AddCommand(newMemoryCmd())
	rootCmd.AddCommand(newCacheCmd())
	rootCmd.AddCommand(newTaskCmd())
	rootCmd.AddCommand(newQueueCmd())
	rootCmd.AddCommand(newWorkersCmd())
	rootCmd.AddCommand(newSkillsCmd())
	rootCmd.AddCommand(newSelfImproveCmd())
	rootCmd.AddCommand(newShadowCmd())
	rootCmd.AddCommand(newDevCmd())
	rootCmd.AddCommand(newModelsCmd())
	rootCmd.AddCommand(newVersionCmd())
	rootCmd.AddCommand(newQCmd())
	rootCmd.AddCommand(newCalendarCmd())
	rootCmd.AddCommand(newHelpCmd(rootCmd))

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version information",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("meept version %s\n", version.String())
		},
	}
}

func newHelpCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:   "help [command]",
		Short: "Help about any command",
		Long:  "Display help information about meept and its subcommands.",
		Run: func(cmd *cobra.Command, args []string) {
			if len(args) == 0 {
				root.Help()
				return
			}
			// Find the subcommand
			target, _, err := root.Find(args)
			if err != nil || target == nil {
				fmt.Printf("Unknown command: %s\n", args[0])
				root.Help()
				return
			}
			target.Help()
		},
	}
}

// getSocketPath returns the socket path, applying defaults if needed.
func getSocketPath() string {
	if socketPath != "" {
		return socketPath
	}
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meept", "meept.sock")
}

// getTransportConfig builds a transport.Config from the CLI flags.
func getTransportConfig() *transport.Config {
	cfg := transport.DefaultConfig()
	cfg.Transport = transportFlag
	cfg.SocketPath = getSocketPath()
	if httpURLFlag != "" {
		cfg.HTTPBaseURL = httpURLFlag
	}
	return cfg
}

// connectDaemon creates and connects a transport.Client based on CLI flags.
// It returns the connected client; the caller is responsible for calling Close().
func connectDaemon() (transport.Client, error) {
	cfg := getTransportConfig()
	client, err := transport.New(cfg)
	if err != nil {
		return nil, err
	}
	if err := client.Connect(); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}
