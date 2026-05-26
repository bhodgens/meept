package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"
)

func newDaemonCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "daemon",
		Short: "Manage the Meept daemon",
		Long: `Control the Meept daemon process.

Commands:
  start     Start the daemon in the background
  stop      Stop the running daemon
  restart   Restart the daemon
  status    Check if daemon is running (alias for 'meept status')`,
	}

	cmd.AddCommand(newDaemonStartCmd())
	cmd.AddCommand(newDaemonStopCmd())
	cmd.AddCommand(newDaemonRestartCmd())
	cmd.AddCommand(newDaemonStatusCmd())

	return cmd
}

func newDaemonStartCmd() *cobra.Command {
	var foreground bool

	cmd := &cobra.Command{
		Use:   "start",
		Short: "Start the daemon",
		Long: `Start the Meept daemon.

By default, the daemon runs in the background. Use -f to run in foreground.`,
		RunE: func(cmd *cobra.Command, args []string) error {
			return startDaemon(foreground)
		},
	}

	cmd.Flags().BoolVarP(&foreground, "foreground", "f", false, "Run in foreground")

	return cmd
}

func newDaemonStopCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "stop",
		Short: "Stop the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			return stopDaemon()
		},
	}
}

func newDaemonRestartCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "restart",
		Short: "Restart the daemon",
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := stopDaemon(); err != nil {
				// Ignore stop errors - daemon might not be running
				if debugEnabled() {
					fmt.Printf("Stop warning: %v\n", err)
				}
			}
			// Wait a bit for cleanup
			time.Sleep(500 * time.Millisecond)
			return startDaemon(false)
		},
	}
}

func newDaemonStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   cmdStatus,
		Short: "Check daemon status",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runStatus(false)
		},
	}
}

func startDaemon(foreground bool) error {
	// Check if already running
	pidFile := filepath.Join(stateDir, "meept.pid")
	if pid, running := isDaemonRunning(pidFile); running {
		return fmt.Errorf("daemon already running (PID %d)", pid)
	}

	// Find the daemon binary
	daemonBin := findDaemonBinary()
	if daemonBin == "" {
		return fmt.Errorf("meept-daemon binary not found\n\nBuild it with: go build -o bin/meept-daemon ./cmd/meept-daemon")
	}

	// Build command arguments
	daemonArgs := []string{}
	if socketPath != "" {
		daemonArgs = append(daemonArgs, "-s", socketPath)
	}
	if stateDir != "" {
		daemonArgs = append(daemonArgs, "-d", stateDir)
	}
	if debugEnabled() {
		daemonArgs = append(daemonArgs, "--debug")
	}

	if foreground {
		// Run in foreground - just exec the daemon
		daemonArgs = append(daemonArgs, "-f")
		daemonCmd := exec.Command(daemonBin, daemonArgs...)
		daemonCmd.Stdout = os.Stdout
		daemonCmd.Stderr = os.Stderr
		daemonCmd.Stdin = os.Stdin
		return daemonCmd.Run()
	}

	// Background mode
	daemonArgs = append(daemonArgs, "-f") // Daemon always runs in "foreground" mode, we background it

	// Create the command
	daemonCmd := exec.Command(daemonBin, daemonArgs...)

	// Detach from terminal
	daemonCmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true,
	}

	// Redirect output to log file
	logFile := filepath.Join(stateDir, "meept.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	daemonCmd.Stdout = f
	daemonCmd.Stderr = f

	// Start the daemon
	if err := daemonCmd.Start(); err != nil {
		f.Close()
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Close log file in parent process - the child inherits the fd
	f.Close()

	// Wait briefly to check if it started successfully
	time.Sleep(200 * time.Millisecond)

	// Check if process is still running
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			// Timeout waiting for startup
			fmt.Printf("Daemon starting (PID %d)...\n", daemonCmd.Process.Pid)
			fmt.Printf("Log file: %s\n", logFile)
			return nil
		default:
			// Check if PID file was created
			if _, err := os.Stat(pidFile); err == nil {
				fmt.Printf("Daemon started (PID %d)\n", daemonCmd.Process.Pid)
				return nil
			}
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func stopDaemon() error {
	pidFile := filepath.Join(stateDir, "meept.pid")

	pid, running := isDaemonRunning(pidFile)
	if !running {
		fmt.Println("Daemon is not running")
		return nil
	}

	// Send SIGTERM
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	fmt.Printf("Stopping daemon (PID %d)...\n", pid)

	// Wait for process to exit
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	for {
		select {
		case <-ctx.Done():
			// Force kill
			fmt.Println("Daemon not responding, sending SIGKILL...")
			_ = proc.Signal(syscall.SIGKILL)
			os.Remove(pidFile)
			return nil
		default:
			// Check if still running
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				// Process exited
				fmt.Println("Daemon stopped")
				return nil
			}
			time.Sleep(100 * time.Millisecond)
		}
	}
}

func isDaemonRunning(pidFile string) (int, bool) {
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0, false
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, false
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0, false
	}

	// Check if process is actually running
	if err := proc.Signal(syscall.Signal(0)); err != nil {
		return 0, false
	}

	return pid, true
}

func findDaemonBinary() string {
	// Check common locations
	candidates := []string{
		"meept-daemon",       // In PATH
		"./meept-daemon",     // Current directory
		"./bin/meept-daemon", // Local bin
		filepath.Join(stateDir, "bin", "meept-daemon"), // State directory
	}

	// Also check relative to current executable
	if exe, err := os.Executable(); err == nil {
		dir := filepath.Dir(exe)
		candidates = append(candidates,
			filepath.Join(dir, "meept-daemon"),
			filepath.Join(dir, "..", "meept-daemon"),
		)
	}

	for _, candidate := range candidates {
		if path, err := exec.LookPath(candidate); err == nil {
			return path
		}
	}

	return ""
}
