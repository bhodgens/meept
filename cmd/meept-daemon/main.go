// Command meept-daemon runs the Meept daemon server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/caimlas/meept/internal/version"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/daemon"
)

var (
	

	// Flags
	configPath string
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

	rootCmd.Flags().StringVarP(&configPath, "config", "c", "", "Config file path (default: ~/.meept/meept.toml)")
	rootCmd.Flags().StringVarP(&socketPath, "socket", "s", "", "Unix socket path (default: ~/.meept/meept.sock)")
	rootCmd.Flags().StringVarP(&stateDir, "state-dir", "d", defaultStateDir, "State directory")
	rootCmd.Flags().BoolVarP(&foreground, "foreground", "f", true, "Run in foreground")
	rootCmd.Flags().BoolVar(&debug, "debug", false, "Enable debug logging")

	// Version command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Println(version.String())
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
	// Load configuration from TOML file
	var appCfg *config.Config
	var err error

	if configPath != "" {
		appCfg, err = config.Load(configPath)
	} else {
		appCfg, err = config.LoadDefault()
	}
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	// Build daemon config from app config
	daemonCfg := &daemon.Config{
		SocketPath:                  appCfg.Daemon.SocketPath,
		PIDFile:                     appCfg.Daemon.PIDFile,
		StateDir:                    appCfg.Daemon.DataDir,
		ShutdownTimeout:             appCfg.ShutdownTimeout(),
		LogLevel:                    config.ParseLogLevel(appCfg.Daemon.LogLevel),
		AllowedPaths:                appCfg.Security.AllowedPaths,
		BlockedPaths:                appCfg.Security.BlockedPaths,
		BlockFinancial:              appCfg.Security.BlockFinancial,
		RequireConfirmationHigh:     appCfg.Security.RequireConfirmationHigh,
		RequireConfirmationCritical: appCfg.Security.RequireConfirmationCritical,
	}

	// Override with command-line flags
	if stateDir != "" {
		daemonCfg.StateDir = stateDir
		daemonCfg.SocketPath = filepath.Join(stateDir, "meept.sock")
		daemonCfg.PIDFile = filepath.Join(stateDir, "meept.pid")
	}

	if socketPath != "" {
		daemonCfg.SocketPath = socketPath
	}

	if debug {
		daemonCfg.LogLevel = slog.LevelDebug
	}

	// Ensure data directory exists
	if err := config.EnsureDataDir(appCfg); err != nil {
		return err
	}

	d, err := daemon.New(daemonCfg)
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
