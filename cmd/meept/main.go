// Command meept is the CLI entry point for the Meept assistant.
package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"
)

var (
	version = "0.2.0-go"

	// Global flags
	socketPath string
	stateDir   string
	debug      bool
)

func main() {
	homeDir, _ := os.UserHomeDir()
	defaultStateDir := filepath.Join(homeDir, ".meept")
	defaultSocket := filepath.Join(defaultStateDir, "meept.sock")

	rootCmd := &cobra.Command{
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
  meept clawskills                       # Manage third-party skills`,
		SilenceUsage: true,
		Args:         cobra.MaximumNArgs(1),
		RunE:         runChat, // Default to chat when no subcommand
	}

	// Global flags
	rootCmd.PersistentFlags().StringVarP(&socketPath, "socket", "s", defaultSocket, "Unix socket path")
	rootCmd.PersistentFlags().StringVarP(&stateDir, "state-dir", "d", defaultStateDir, "State directory")
	rootCmd.PersistentFlags().BoolVar(&debug, "debug", false, "Enable debug output")

	// Add subcommands
	rootCmd.AddCommand(newChatCmd())
	rootCmd.AddCommand(newStatusCmd())
	rootCmd.AddCommand(newDaemonCmd())
	rootCmd.AddCommand(newJobsCmd())
	rootCmd.AddCommand(newMemoryCmd())
	rootCmd.AddCommand(newTaskCmd())
	rootCmd.AddCommand(newQueueCmd())
	rootCmd.AddCommand(newWorkersCmd())
	rootCmd.AddCommand(newSkillsCmd())
	rootCmd.AddCommand(newClawSkillsCmd())
	rootCmd.AddCommand(newSelfImproveCmd())
	rootCmd.AddCommand(newShadowCmd())
	rootCmd.AddCommand(newDevCmd())
	rootCmd.AddCommand(newVersionCmd())
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
			fmt.Printf("meept version %s\n", version)
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
