package services

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
)

// DaemonController provides daemon lifecycle control.
type DaemonController interface {
	IsRunning() bool
	PID() int
	Uptime() time.Duration
	Restart(ctx context.Context) error
}

// DaemonStatus contains daemon status information.
type DaemonStatus struct {
	Status          string  `json:"status"`
	PID             int     `json:"pid,omitempty"`
	UptimeSeconds   float64 `json:"uptime_seconds,omitempty"`
	Model           string  `json:"model,omitempty"`
	TokensUsed      int     `json:"tokens_used"`
	TokensRemaining int     `json:"tokens_remaining"`
	BudgetUsed      float64 `json:"budget_used"`
	BudgetRemaining float64 `json:"budget_remaining"`
	Methods         int     `json:"registered_methods"`
	BusSubscribers  int     `json:"bus_subscribers"`
}

// DaemonService handles daemon lifecycle operations.
type DaemonService struct {
	pidFile    string
	stateDir   string
	binPath    string
	controller DaemonController
}

// NewDaemonService creates a daemon service.
func NewDaemonService(pidFile, stateDir, binPath string, controller DaemonController) *DaemonService {
	return &DaemonService{
		pidFile:    pidFile,
		stateDir:   stateDir,
		binPath:    binPath,
		controller: controller,
	}
}

// Status returns the current daemon status.
func (s *DaemonService) Status(ctx context.Context) (*DaemonStatus, error) {
	status := &DaemonStatus{
		Status: "stopped",
		PID:    0,
	}

	// Check if running via controller
	if s.controller != nil && s.controller.IsRunning() {
		status.Status = "running"
		status.PID = s.controller.PID()
		status.UptimeSeconds = s.controller.Uptime().Seconds()
	} else {
		// Fallback: check PID file
		if s.pidFile != "" {
			pidData, err := os.ReadFile(s.pidFile)
			if err == nil {
				pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
				if err == nil {
					// Check if process is running
					proc, err := os.FindProcess(pid)
					if err == nil && proc.Signal(syscall.Signal(0)) == nil {
						status.Status = "running"
						status.PID = pid
						// Can't get uptime without controller, but we know it's running
					}
				}
			}
		}
	}

	// TODO: Get detailed status from daemon via RPC if available
	// For now, return basic status
	return status, nil
}

// Start starts the daemon in the background.
func (s *DaemonService) Start(ctx context.Context) error {
	if s.controller != nil && s.controller.IsRunning() {
		return fmt.Errorf("daemon is already running")
	}

	// Check PID file for existing daemon
	if s.pidFile != "" {
		if pidData, err := os.ReadFile(s.pidFile); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(pidData))); err == nil {
				if proc, err := os.FindProcess(pid); err == nil && proc.Signal(syscall.Signal(0)) == nil {
					return fmt.Errorf("daemon already running (PID %d)", pid)
				}
			}
		}
	}

	// Find daemon binary
	daemonBin := s.binPath
	if daemonBin == "" {
		var err error
		daemonBin, err = exec.LookPath("meept-daemon")
		if err != nil {
			// Check common locations
			candidates := []string{
				"./bin/meept-daemon",
				filepath.Join(s.stateDir, "bin", "meept-daemon"),
			}
			for _, candidate := range candidates {
				if _, err := os.Stat(candidate); err == nil {
					daemonBin = candidate
					break
				}
			}
		}
		if daemonBin == "" {
			return fmt.Errorf("meept-daemon binary not found")
		}
	}

	// Start daemon with -f (foreground) flag, detached
	daemonArgs := []string{"-f"}
	if s.stateDir != "" {
		daemonArgs = append([]string{"-d", s.stateDir}, daemonArgs...)
	}

	cmd := exec.Command(daemonBin, daemonArgs...)
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setsid: true, // Detach from terminal
	}

	// Redirect output to log file
	logFile := filepath.Join(s.stateDir, "meept.log")
	f, err := os.OpenFile(logFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("failed to open log file: %w", err)
	}
	cmd.Stdout = f
	cmd.Stderr = f
	defer f.Close()

	if err := cmd.Start(); err != nil {
		return fmt.Errorf("failed to start daemon: %w", err)
	}

	// Wait briefly to check if it started successfully
	time.Sleep(200 * time.Millisecond)

	// Check if process is still running
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		return fmt.Errorf("daemon failed to start: %w", err)
	}

	return nil
}

// Stop stops the running daemon.
func (s *DaemonService) Stop(ctx context.Context) error {
	var pid int

	// Try controller first
	if s.controller != nil && s.controller.IsRunning() {
		pid = s.controller.PID()
	} else if s.pidFile != "" {
		// Read PID file
		pidData, err := os.ReadFile(s.pidFile)
		if err != nil {
			return fmt.Errorf("daemon is not running (no PID file)")
		}
		pid, err = strconv.Atoi(strings.TrimSpace(string(pidData)))
		if err != nil {
			return fmt.Errorf("invalid PID file")
		}
	} else {
		return fmt.Errorf("daemon is not running")
	}

	// Find and stop process
	proc, err := os.FindProcess(pid)
	if err != nil {
		return fmt.Errorf("failed to find process: %w", err)
	}

	// Send SIGTERM
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		return fmt.Errorf("failed to send SIGTERM: %w", err)
	}

	// Wait for process to exit
	timeout := 10 * time.Second
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Force kill
			_ = proc.Signal(syscall.SIGKILL)
			return nil
		case <-ticker.C:
			if err := proc.Signal(syscall.Signal(0)); err != nil {
				// Process exited
				return nil
			}
		}
	}
}

// Restart restarts the daemon.
func (s *DaemonService) Restart(ctx context.Context) error {
	if s.controller != nil {
		return s.controller.Restart(ctx)
	}

	// Manual restart
	if err := s.Stop(ctx); err != nil && !strings.Contains(err.Error(), "not running") {
		return err
	}

	// Wait for cleanup
	select {
	case <-time.After(500 * time.Millisecond):
	case <-ctx.Done():
		return ctx.Err()
	}

	return s.Start(ctx)
}
