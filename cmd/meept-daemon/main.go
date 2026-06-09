// Command meept-daemon runs the Meept daemon server.
package main

import (
	"context"
	"fmt"
	"log/slog"
	_ "modernc.org/sqlite" // Ensure sqlite driver is registered for side effects
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
			slog.Info("meept-daemon version", "version", version.String())
		},
	})

	// Status command
	rootCmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "Check daemon status",
		RunE:  checkStatus,
	})

	// Service management subcommand (uses kardianos/service)
	serviceCmd := &cobra.Command{
		Use:   "service",
		Short: "Manage system service (install/uninstall/start/stop/status)",
	}
	serviceCmd.AddCommand(
		&cobra.Command{
			Use:   "install",
			Short: "Install daemon as a system service",
			RunE:  runServiceInstall,
		},
		&cobra.Command{
			Use:   "uninstall",
			Short: "Uninstall the system service",
			RunE:  runServiceUninstall,
		},
		&cobra.Command{
			Use:   "start",
			Short: "Start the system service",
			RunE:  runServiceStart,
		},
		&cobra.Command{
			Use:   "stop",
			Short: "Stop the system service",
			RunE:  runServiceStop,
		},
		&cobra.Command{
			Use:   "status",
			Short: "Query system service status",
			RunE:  runServiceStatus,
		},
	)
	rootCmd.AddCommand(serviceCmd)

	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func runDaemon(cmd *cobra.Command, args []string) error {
	slog.Info("daemon starting", "version", version.String())

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

func newServiceMgr() (*daemon.DaemonService, error) {
	svcCfg, err := daemon.DefaultServiceConfig()
	if err != nil {
		return nil, fmt.Errorf("failed to get service config: %w", err)
	}
	if stateDir != "" {
		svcCfg.StateDir = stateDir
		svcCfg.PIDFile = filepath.Join(stateDir, "meept.pid")
	}
	return daemon.NewServiceManager(svcCfg)
}

func runServiceInstall(cmd *cobra.Command, args []string) error {
	mgr, err := newServiceMgr()
	if err != nil {
		return err
	}
	if err := mgr.Install(); err != nil {
		return fmt.Errorf("failed to install service: %w", err)
	}
	slog.Info("service installed successfully")
	return nil
}

func runServiceUninstall(cmd *cobra.Command, args []string) error {
	mgr, err := newServiceMgr()
	if err != nil {
		return err
	}
	if err := mgr.Uninstall(); err != nil {
		return fmt.Errorf("failed to uninstall service: %w", err)
	}
	slog.Info("service uninstalled successfully")
	return nil
}

func runServiceStart(cmd *cobra.Command, args []string) error {
	mgr, err := newServiceMgr()
	if err != nil {
		return err
	}
	if err := mgr.StartService(); err != nil {
		return fmt.Errorf("failed to start service: %w", err)
	}
	slog.Info("service started successfully")
	return nil
}

func runServiceStop(cmd *cobra.Command, args []string) error {
	mgr, err := newServiceMgr()
	if err != nil {
		return err
	}
	if err := mgr.StopService(); err != nil {
		return fmt.Errorf("failed to stop service: %w", err)
	}
	slog.Info("service stopped successfully")
	return nil
}

func runServiceStatus(cmd *cobra.Command, args []string) error {
	mgr, err := newServiceMgr()
	if err != nil {
		return err
	}
	status, err := mgr.Status()
	if err != nil {
		return fmt.Errorf("failed to get service status: %w", err)
	}
	switch status {
	case 0:
		fmt.Println("Service status: unknown")
	case 1:
		fmt.Println("Service status: running")
	case 2:
		fmt.Println("Service status: stopped")
	}
	return nil
}
