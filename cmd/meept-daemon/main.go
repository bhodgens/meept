// Command meept-daemon runs the Meept daemon server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/daemon"
)

var (
	version = "0.2.0-go"

	// Flags
	socketPath string
	stateDir   string
	foreground bool
	debug      bool
)

func main() {
	rootCmd := &cobra.Command{
		Use:   "meept-daemon",
		Short: "Meept daemon server",
		Long:  "The Meept daemon provides the core message bus, RPC server, and component registry.",
		RunE:  runDaemon,
	}

	homeDir, _ := os.UserHomeDir()
	defaultStateDir := filepath.Join(homeDir, ".meept")

	rootCmd.Flags().StringVarP(&socketPath, "socket", "s", "", "Unix socket path (default: ~/.meept/meept.sock)")
	rootCmd.Flags().StringVarP(&stateDir, "state-dir", "d", defaultStateDir, "State directory")
	rootCmd.Flags().BoolVarP(&foreground, "foreground", "f", true, "Run in foreground")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging")

	// Version command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version)
		},
	})

	// Status command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		RunE:  checkStatus,
	})

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runDaemon(cmd *cobra.Command, args []string) error {
	cfg := daemon.DefaultConfig()

	if stateDir != "" {
		cfg.StateDir = stateDir
		cfg.SocketPath = filepath.Join(stateDir, "meept.sock")
		cfg.PIDFile = filepath.Join(stateDir, "meept.pid")
	}

	if socketPath != "" {
		cfg.SocketPath = socketPath
	}

	if debug {
		cfg.LogLevel = slog.LevelDebug
	}

	d, err := daemon.New(cfg)
	if err != nil {
		return fmt.Errorf("failed to create daemon: %w", err)
	}

	return d.Run(context.Background())
}

func checkStatus(cmd *cobra.Command, args []string) error {
	cfg := daemon.DefaultConfig()
	if stateDir != "" {
		cfg.PIDFile = filepath.Join(stateDir, "meept.pid")
	}

	data, err := os.ReadFile(cfg.PIDFile)
	if os.IsNotExist(err) {
		fmt.Println("Daemon is not running")
		return nil
	}
	if err != nil {
		return fmt.Errorf("failed to read PID file: %w", err)
	}

	fmt.Printf("Daemon is running (PID %s)\n", string(data))
	return nil
}
