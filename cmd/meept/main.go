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
		Use:   "meept",
		Short: "Meept AI assistant",
		Long: `Meept is an AI assistant with task orchestration, memory, and skill capabilities.

Start chatting:
  meept chat "What's the weather like?"
  meept chat  # Interactive TUI mode

Manage daemon:
  meept daemon start
  meept daemon stop
  meept status`,
		SilenceUsage: true,
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
	rootCmd.AddCommand(newClawSkillsCmd())
	rootCmd.AddCommand(newSelfImproveCmd())
	rootCmd.AddCommand(newDevCmd())
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
			fmt.Printf("meept version %s\n", version)
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
