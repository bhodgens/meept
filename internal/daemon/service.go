// Package daemon provides the main daemon lifecycle management.
package daemon

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// ServiceConfig holds configuration for the daemon service manager.
type ServiceConfig struct {
	// Name is the system service name (used as launchd label on macOS).
	Name string
	// DisplayName is the human-readable service name.
	DisplayName string
	// Description is the service description.
	Description string
	// StateDir is the meept state directory (e.g. ~/.meept).
	StateDir string
	// PIDFile is the path to the daemon PID file.
	PIDFile string
	// Arguments are extra arguments passed to the daemon binary when
	// installed as a system service.
	Arguments []string
}

// DefaultServiceConfig returns a ServiceConfig with sensible defaults.
func DefaultServiceConfig() (*ServiceConfig, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		if u, err := user.Current(); err == nil {
			homeDir = u.HomeDir
		} else {
			return nil, fmt.Errorf("failed to determine home directory: %w", err)
		}
	}

	stateDir := filepath.Join(homeDir, ".meept")
	return &ServiceConfig{
		Name:        LaunchAgentLabel,
		DisplayName: "Meept Daemon",
		Description: "Meept AI assistant daemon with multi-agent task orchestration",
		StateDir:    stateDir,
		PIDFile:     filepath.Join(stateDir, "meept.pid"),
		Arguments:   []string{},
	}, nil
}

// DaemonService provides the DaemonController contract expected by the HTTP
// and services layers.  It delegates to ServiceManager for actual platform
// service management (launchd on macOS), keeping the PID-file based
// IsRunning/PID/Uptime methods for status queries.
//
// Usage:
//
//	cfg, _ := DefaultServiceConfig()
//	mgr, _ := NewDaemonService(cfg)
//	mgr.Install()   // registers as system service
//	mgr.StartService() // starts via launchd
//	mgr.StopService()  // stops via launchd
//	mgr.Uninstall() // removes the system service
type DaemonService struct {
	cfg *ServiceConfig
	sm  *ServiceManager
}

// NewDaemonService creates a DaemonService backed by ServiceManager for
// platform service lifecycle.  It discovers the daemon executable path
// automatically.
func NewDaemonService(cfg *ServiceConfig) (*DaemonService, error) {
	if cfg == nil {
		return nil, fmt.Errorf("service config must not be nil")
	}

	// Ensure state directory exists.
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	// Create the underlying ServiceManager for platform operations.
	smCfg := &ServiceManagerConfig{
		LogDir: cfg.StateDir,
		Name:   cfg.Name,
	}
	sm, err := NewServiceManager(smCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create service manager: %w", err)
	}

	return &DaemonService{
		cfg: cfg,
		sm:  sm,
	}, nil
}

// ---------------------------------------------------------------------------
// Higher-level methods matching the DaemonController interface
//
// These implement the same contract as DaemonControl in launchd.go:
//
//	interface { IsRunning() bool; PID() int; Uptime() time.Duration; Restart(ctx) error }
//
// This allows DaemonService to be used as a drop-in replacement for
// DaemonControl in the HTTP server and service registry wiring.
// ---------------------------------------------------------------------------

// IsRunning returns true if the daemon process is alive (checks PID file).
func (ds *DaemonService) IsRunning() bool {
	pid := ds.readPID()
	if pid == 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// PID returns the daemon PID from the PID file, or 0 if not running.
func (ds *DaemonService) PID() int {
	pid := ds.readPID()
	if pid == 0 {
		return 0
	}
	// Verify the process is alive.
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0
	}
	if proc.Signal(syscall.Signal(0)) != nil {
		return 0
	}
	return pid
}

// Uptime returns how long the daemon has been running.
func (ds *DaemonService) Uptime() time.Duration {
	pid := ds.readPID()
	if pid == 0 {
		return 0
	}

	// Parse elapsed time from ps.
	out, err := psElapsedTime(pid)
	if err != nil {
		return 0
	}
	return parseElapsedTime(out)
}

// Restart restarts the daemon via the service manager.
func (ds *DaemonService) Restart(_ context.Context) error {
	if err := ds.StopService(); err != nil {
		return fmt.Errorf("service stop failed: %w", err)
	}
	// Brief pause to ensure clean shutdown.
	time.Sleep(500 * time.Millisecond)
	return ds.StartService()
}

// ---------------------------------------------------------------------------
// Service lifecycle: Install / Uninstall / Start / Stop / Status
// ---------------------------------------------------------------------------

// Install registers the daemon as a system service (launchd agent on macOS,
// systemd unit on Linux, etc.).  Delegates to ServiceManager.
func (ds *DaemonService) Install() error {
	return ds.sm.Install()
}

// Uninstall removes the system service registration.  Delegates to ServiceManager.
func (ds *DaemonService) Uninstall() error {
	return ds.sm.Uninstall()
}

// StartService starts the daemon via the platform service manager (launchd).
func (ds *DaemonService) StartService() error {
	return ds.sm.Start()
}

// StopService stops the daemon via the platform service manager (launchd).
func (ds *DaemonService) StopService() error {
	return ds.sm.Stop()
}

// Status queries the platform service manager for the current service status.
// Returns status constants: 0 = unknown, 1 = running, 2 = stopped.
func (ds *DaemonService) Status() (int, error) {
	if ds.sm.IsRunning() {
		return 1, nil
	}
	if ds.sm.IsLoaded() {
		return 2, nil
	}
	return 0, nil
}

// ---------------------------------------------------------------------------
// Internal helpers
// ---------------------------------------------------------------------------

// readPID reads the PID file and returns the integer PID (0 on any error).
func (ds *DaemonService) readPID() int {
	data, err := os.ReadFile(ds.cfg.PIDFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0
	}
	return pid
}

// psElapsedTime runs "ps -o etime= -p <pid>" and returns the trimmed output.
// It is a package-level variable so tests can override it.
var psElapsedTime = func(pid int) (string, error) {
	//nolint:gosec // pid is an int, command is from known config values
	cmd := exec.Command("ps", "-o", "etime=", "-p", strconv.Itoa(pid))
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return cleanElapsedTime(out.String()), nil
}

// cleanElapsedTime trims whitespace from ps etime output.
func cleanElapsedTime(s string) string {
	// ps output may have leading/trailing whitespace and newlines.
	b := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			continue
		}
		b = append(b, c)
	}
	return string(b)
}

// Ensure DaemonService satisfies the DaemonController interface at compile time.
var _ DaemonController = (*DaemonService)(nil)
