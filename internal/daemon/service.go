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
	"sync/atomic"
	"syscall"
	"time"

	"github.com/kardianos/service"
)

// serviceDependencies is a var so it can be overridden in tests.
var serviceDependencies = []string{}

// ServiceConfig holds configuration for the kardianos/service-based daemon manager.
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
		Arguments:   []string{"-f"},
	}, nil
}

// DaemonService satisfies kardianos/service.Interface and also provides the
// DaemonController contract expected by the HTTP and services layers.
//
// When the daemon is installed as a system service (launchd on macOS, systemd
// on Linux, etc.) kardianos/service manages the plist/unit generation and
// launchctl/systemctl interactions, replacing the hand-rolled code in
// launchd.go.
//
// Usage:
//
//	cfg, _ := DefaultServiceConfig()
//	mgr, _ := NewServiceManager(cfg)
//	mgr.Install()   // registers as system service
//	mgr.Start()     // starts via the service manager
//	mgr.Stop()      // stops via the service manager
//	mgr.Uninstall() // removes the system service
type DaemonService struct {
	cfg    *ServiceConfig
	svc    service.Service
	logger service.Logger

	// isRunning is set to true when Start is called and cleared on Stop.
	isRunning atomic.Bool
}

// NewServiceManager creates a DaemonService backed by kardianos/service.
// It discovers the daemon executable path automatically.  The returned
// DaemonService implements service.Interface but callers typically use the
// higher-level Install/Uninstall/Start/Stop/Status methods instead.
func NewServiceManager(cfg *ServiceConfig) (*DaemonService, error) {
	if cfg == nil {
		return nil, fmt.Errorf("service config must not be nil")
	}

	// Resolve the daemon executable path.
	daemonExe, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("failed to resolve executable path: %w", err)
	}

	// Ensure state directory exists.
	if err := os.MkdirAll(cfg.StateDir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create state directory: %w", err)
	}

	svcCfg := &service.Config{
		Name:             cfg.Name,
		DisplayName:      cfg.DisplayName,
		Description:      cfg.Description,
		Executable:       daemonExe,
		Arguments:         cfg.Arguments,
		Dependencies:      serviceDependencies,
		WorkingDirectory:  cfg.StateDir,
		Option:           service.KeyValue{},
	}

	// Set environment variables for the service.
	svcCfg.EnvVars = map[string]string{
		"PATH": "/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin",
	}

	// Request that kardianos/service redirects stdout/stderr to log files
	// (used on macOS/darwin launchd platform).
	svcCfg.Option["LogOutput"] = true

	// Create the kardianos/service.Service.
	svc, err := service.New(nil, svcCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create service: %w", err)
	}

	return &DaemonService{
		cfg:    cfg,
		svc:    svc,
		logger: &noopServiceLogger{},
	}, nil
}

// ---------------------------------------------------------------------------
// service.Interface methods (required by kardianos/service)
//
// These are called by kardianos/service when Start/Stop are invoked through
// the service manager.  The daemon main function would use:
//
//	svc, _ := service.New(daemonService, svcCfg)
//	svc.Run()
//
// where daemonService.Start launches the actual daemon logic and
// daemonService.Stop triggers graceful shutdown.
// ---------------------------------------------------------------------------

// Start implements service.Interface.  Called by the service manager when the
// service is started.  In the full integration path the daemon's Run()
// context would be passed here.
func (ds *DaemonService) Start(_ service.Service) error {
	ds.isRunning.Store(true)
	return nil
}

// Stop implements service.Interface.
func (ds *DaemonService) Stop(_ service.Service) error {
	ds.isRunning.Store(false)
	return nil
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
// systemd unit on Linux, etc.).
func (ds *DaemonService) Install() error {
	return ds.svc.Install()
}

// Uninstall removes the system service registration.
func (ds *DaemonService) Uninstall() error {
	// Stop first if running.
	_ = ds.StopService()
	return ds.svc.Uninstall()
}

// StartService starts the daemon via the service manager.
func (ds *DaemonService) StartService() error {
	return ds.svc.Start()
}

// StopService stops the daemon via the service manager.
func (ds *DaemonService) StopService() error {
	return ds.svc.Stop()
}

// Status queries the platform service manager for the current service status.
func (ds *DaemonService) Status() (service.Status, error) {
	return ds.svc.Status()
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

// noopServiceLogger discards service log output.
type noopServiceLogger struct{}

func (l *noopServiceLogger) Error(v ...interface{}) error  { return nil }
func (l *noopServiceLogger) Warning(v ...interface{}) error { return nil }
func (l *noopServiceLogger) Info(v ...interface{}) error   { return nil }

func (l *noopServiceLogger) Errorf(format string, a ...interface{}) error  { return nil }
func (l *noopServiceLogger) Warningf(format string, a ...interface{}) error { return nil }
func (l *noopServiceLogger) Infof(format string, a ...interface{}) error   { return nil }

// Ensure DaemonService satisfies the service.Interface at compile time.
var _ service.Interface = (*DaemonService)(nil)
