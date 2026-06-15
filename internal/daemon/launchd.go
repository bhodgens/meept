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
	"strings"
	"syscall"
	"time"

	"github.com/caimlas/meept/internal/errcls"
	"github.com/kardianos/service"
)

const (
	// LaunchAgentLabel is the label for the launchd agent.
	LaunchAgentLabel = "com.caimlas.meept-daemon"
)

// --- launchService: implements service.Interface ---

// launchService wires kardianos/service into the meept-daemon lifecycle.
type launchService struct {
	daemonPath string
	logDir     string
}

func (l *launchService) Init(*service.Service) error { return nil }

func (l *launchService) Start(s service.Service) error {
	_ = s
	// Exec the daemon binary. daemon.Run() blocks in the foreground by default.
	cmd := exec.Command(l.daemonPath) //nolint:gosec
	cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
	return cmd.Run()
}

func (l *launchService) Stop(s service.Service) error {
	// Forward to legacy controller which handles PID-based stop.
	legacy, err := newLegacyController(l.logDir)
	if err != nil {
		return err
	}
	return legacy.Stop()
}

// --- Legacy controller: kept as a fallback for PID/status queries. ---

// legacyController wraps the PID-file + signal-based approach for
// querying status, which kardianos/service doesn't provide.
type legacyController struct {
	meeptDir string
}

func getHomeDirOrFallback() string {
	hd, err := os.UserHomeDir()
	if err != nil {
		if u, err := user.Current(); err == nil {
			return u.HomeDir
		}
		return "/tmp"
	}
	return hd
}

func newLegacyController(logDir string) (*legacyController, error) {
	home := getHomeDirOrFallback()
	meeptDir := logDir
	if meeptDir == "" {
		meeptDir = filepath.Join(home, ".meept")
	}
	if err := os.MkdirAll(meeptDir, 0o755); err != nil { //nolint:gosec
		return nil, err
	}
	return &legacyController{meeptDir: meeptDir}, nil
}

// IsRunning checks if the daemon process is actually running via PID file.
func (c *legacyController) IsRunning() bool {
	pid := c.GetPID()
	return pid > 0
}

// GetPID returns the daemon PID if running, 0 otherwise.
func (c *legacyController) GetPID() int {
	pidFile := filepath.Join(c.meeptDir, "meept.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	if proc, err := os.FindProcess(pid); err == nil && proc.Signal(syscall.Signal(0)) == nil {
		return pid
	}
	return 0
}

// GetUptime returns the daemon uptime.
func (c *legacyController) GetUptime() time.Duration {
	pidFile := filepath.Join(c.meeptDir, "meept.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0
	}
	cmd := exec.Command("ps", "-o", "etime=", "-p", strconv.Itoa(pid)) //nolint:gosec
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return 0
	}
	return parseElapsedTime(strings.TrimSpace(out.String()))
}

// Stop sends SIGTERM to the running daemon process.
func (c *legacyController) Stop() error {
	pid := c.GetPID()
	if pid == 0 {
		return nil
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}
	if proc.Signal(syscall.SIGTERM) == nil {
		// Best-effort wait for graceful shutdown.
		for i := 0; i < 40; i++ {
			time.Sleep(250 * time.Millisecond)
			if proc.Signal(syscall.Signal(0)) != nil {
				return nil
			}
		}
		// Force kill if still alive.
		_ = proc.Signal(syscall.SIGKILL)
		return nil
	}
	return nil
}

// parseElapsedTime parses the elapsed time string from ps.
func parseElapsedTime(s string) time.Duration {
	// Format can be: MM:SS, HH:MM:SS, or DD-HH:MM:SS
	parts := strings.Split(s, ":")
	if len(parts) == 0 {
		return 0
	}
	var days, hours, mins, secs int
	switch len(parts) {
	case 3:
		dayPart := parts[0]
		if idx := strings.Index(dayPart, "-"); idx > 0 {
			days, _ = strconv.Atoi(dayPart[:idx])
			hours, _ = strconv.Atoi(dayPart[idx+1:])
		} else {
			hours, _ = strconv.Atoi(parts[0])
		}
		mins, _ = strconv.Atoi(parts[1])
		secs, _ = strconv.Atoi(parts[2])
	case 2:
		mins, _ = strconv.Atoi(parts[0])
		secs, _ = strconv.Atoi(parts[1])
	default:
		return 0
	}
	return time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(mins)*time.Minute +
		time.Duration(secs)*time.Second
}

// --- findDaemonBinary ---

// findDaemonBinary tries to find the meept-daemon binary.
func findDaemonBinary() (string, error) {
	locations := []string{
		"./bin/meept-daemon",
		filepath.Join(os.Getenv("HOME"), "bin", "meept-daemon"),
		"/usr/local/bin/meept-daemon",
		"/opt/homebrew/bin/meept-daemon",
		"/usr/bin/meept-daemon",
	}
	for _, path := range locations {
		//nolint:gosec // path validated by config directory check
		if _, err := os.Stat(path); err == nil {
			if !filepath.IsAbs(path) {
				if abs, err := filepath.Abs(path); err == nil {
					return abs, nil
				}
			}
			return path, nil
		}
	}
	return "", fmt.Errorf("meept-daemon binary not found")
}

// --- ServiceManager: Kardianos/service-backed lifecycle ---

// ServiceManager manages the daemon as a platform service (launchd on macOS).
type ServiceManager struct {
	daemonPath string
	logDir     string
	svc        service.Service // set after Install/Uninstall for status queries
	name       string
}

// ServiceManagerConfig holds options for NewServiceManager.
type ServiceManagerConfig struct {
	// LogDir is the directory for daemon logs (default: ~/.meept).
	LogDir string
	// Name overrides the service label (default: LaunchAgentLabel).
	Name string
}

// NewServiceManager creates a service manager that wraps kardianos/service.
func NewServiceManager(cfg *ServiceManagerConfig) (*ServiceManager, error) {
	if cfg == nil {
		cfg = &ServiceManagerConfig{}
	}
	daemonPath, err := findDaemonBinary()
	if err != nil {
		daemonPath = "/usr/local/bin/meept-daemon"
	}
	home := getHomeDirOrFallback()
	logDir := cfg.LogDir
	if logDir == "" {
		logDir = filepath.Join(home, ".meept")
	}
	return &ServiceManager{
		daemonPath: daemonPath,
		logDir:     logDir,
		name:       cfg.Name,
	}, nil
}

// Install registers the launchd agent and starts it.
func (m *ServiceManager) Install() error {
	label := m.name
	if label == "" {
		label = LaunchAgentLabel
	}

	prg := &launchService{
		daemonPath: m.daemonPath,
		logDir:     m.logDir,
	}
	cfg := &service.Config{
		Name:        label,
		DisplayName: "Meept Daemon",
		Description: "Meept AI daemon -- agent loop orchestration",
		Executable:  m.daemonPath,
		Arguments:   []string{},
	}

	svc, err := service.New(prg, cfg)
	if err != nil {
		return fmt.Errorf("create service: %w", err)
	}

	// Use launchctl load as the primary mechanism; kardianos/service
	// internally writes a plist and runs launchctl, which is exactly
	// what we want on macOS.
	err = svc.Install()
	if err != nil && !errcls.IsAlreadyInstalled(err) {
		// Fallback: write plist manually + launchctl load.
		m.writePlistAndLoad(label)
		return nil
	}

	return svc.Start()
}

// writePlistAndLoad creates a plist file and loads it via launchctl.
func (m *ServiceManager) writePlistAndLoad(label string) {
	home := getHomeDirOrFallback()
	laDir := filepath.Join(home, "Library", "LaunchAgents")
	if err := os.MkdirAll(laDir, 0o700); err != nil {
		return
	}
	plistPath := filepath.Join(laDir, label+".plist")

	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
    </array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
    <key>WorkingDirectory</key><string>%s</string>
    <key>EnvironmentVariables</key>
    <dict><key>PATH</key><string>/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin</string></dict>
</dict>
</plist>
`, label, m.daemonPath, getHomeDirOrFallback())

	if err := os.WriteFile(plistPath, []byte(plist), 0o644); err != nil {
		return
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	cmd := exec.Command("launchctl", "load", "-w", plistPath)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	_ = cmd.Run()
}

// Uninstall stops the service and removes the plist.
func (m *ServiceManager) Uninstall() error {
	label := m.name
	if label == "" {
		label = LaunchAgentLabel
	}

	prg := &launchService{
		daemonPath: m.daemonPath,
		logDir:     m.logDir,
	}
	cfg := &service.Config{
		Name:        label,
		DisplayName: "Meept Daemon",
		Description: "Meept AI daemon",
		Executable:  m.daemonPath,
		Arguments:   []string{},
	}
	svc, err := service.New(prg, cfg)
	if err == nil {
		_ = svc.Stop()
		_ = svc.Uninstall()
	}

	// Also clean up the plist file directly as a fallback.
	home := getHomeDirOrFallback()
	laDir := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(laDir, label+".plist")
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	return os.Remove(plistPath)
}

// Start starts the service via launchctl kickstart.
func (m *ServiceManager) Start() error {
	// Ensure service is installed first.
	label := m.name
	if label == "" {
		label = LaunchAgentLabel
	}
	// Try kardianos/service first.
	prg := &launchService{daemonPath: m.daemonPath, logDir: m.logDir}
	cfg := &service.Config{
		Name:        label,
		DisplayName: "Meept Daemon",
		Description: "Meept AI daemon",
		Executable:  m.daemonPath,
		Arguments:   []string{},
	}
	svc, err := service.New(prg, cfg)
	if err == nil {
		if e := svc.Start(); e == nil {
			return nil
		}
	}
	// Fallback: launchctl kickstart.
	cmd := exec.Command("launchctl", "kickstart", "-k", label) //nolint:gosec
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	if err := cmd.Run(); err != nil {
		if strings.Contains(out.String(), "already running") {
			return nil
		}
		return fmt.Errorf("launchctl kickstart failed: %w", err)
	}
	return nil
}

// Stop stops the service via launchctl stop.
func (m *ServiceManager) Stop() error {
	label := m.name
	if label == "" {
		label = LaunchAgentLabel
	}
	cmd := exec.Command("launchctl", "stop", label) //nolint:gosec
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	return cmd.Run()
}

// Restart stops then starts the service.
func (m *ServiceManager) Restart() error {
	if err := m.Stop(); err != nil {
		return err
	}
	time.Sleep(500 * time.Millisecond)
	return m.Start()
}

// Load loads (but does not start) the launchd config.
func (m *ServiceManager) Load() error {
	return m.Install()
}

// Unload removes the launchd config without removing the plist file.
func (m *ServiceManager) Unload() error {
	label := m.name
	if label == "" {
		label = LaunchAgentLabel
	}
	home := getHomeDirOrFallback()
	laDir := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(laDir, label+".plist")
	cmd := exec.Command("launchctl", "unload", "-w", plistPath) //nolint:gosec
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	return cmd.Run()
}

// IsLoaded checks if the service is registered with launchd.
func (m *ServiceManager) IsLoaded() bool {
	label := m.name
	if label == "" {
		label = LaunchAgentLabel
	}
	home := getHomeDirOrFallback()
	laDir := filepath.Join(home, "Library", "LaunchAgents")
	plistPath := filepath.Join(laDir, label+".plist")
	_, err := os.Stat(plistPath)
	return err == nil
}

// IsRunning checks if the daemon process is actually running.
func (m *ServiceManager) IsRunning() bool {
	legacy, err := newLegacyController(m.logDir)
	if err != nil {
		return false
	}
	return legacy.IsRunning()
}

// GetPID returns the daemon PID if running, 0 otherwise.
func (m *ServiceManager) GetPID() int {
	legacy, err := newLegacyController(m.logDir)
	if err != nil {
		return 0
	}
	return legacy.GetPID()
}

// GetUptime returns the daemon uptime.
func (m *ServiceManager) GetUptime() time.Duration {
	legacy, err := newLegacyController(m.logDir)
	if err != nil {
		return 0
	}
	return legacy.GetUptime()
}

// DaemonPIDFile returns the path to the daemon PID file.
func (m *ServiceManager) DaemonPIDFile() string {
	return filepath.Join(m.logDir, "meept.pid")
}


// --- DaemonControl: HTTP/server control interface ---

// DaemonController is the interface used by HTTP handlers and services.
type DaemonController interface {
	IsRunning() bool
	PID() int
	Uptime() time.Duration
	Restart(ctx context.Context) error
}

// DaemonControl provides daemon control functionality for HTTP servers.
//
//nolint:revive // stutter with package name is intentional for API clarity
type DaemonControl struct {
	manager *ServiceManager
}

// NewDaemonControl creates a new DaemonControl.
func NewDaemonControl() (*DaemonControl, error) {
	mgr, err := NewServiceManager(nil)
	if err != nil {
		return nil, err
	}
	return &DaemonControl{manager: mgr}, nil
}

// IsRunning returns true if the daemon is running.
func (d *DaemonControl) IsRunning() bool {
	return d.manager.IsRunning()
}

// PID returns the daemon PID.
func (d *DaemonControl) PID() int {
	return d.manager.GetPID()
}

// Uptime returns the daemon uptime.
func (d *DaemonControl) Uptime() time.Duration {
	return d.manager.GetUptime()
}

// Restart restarts the daemon.
func (d *DaemonControl) Restart(ctx context.Context) error {
	return d.manager.Restart()
}
