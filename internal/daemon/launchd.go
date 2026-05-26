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
)

const (
	// LaunchAgentLabel is the label for the launchd agent.
	LaunchAgentLabel = "com.caimlas.meept-daemon"

	// launchdPlistTemplate is the template for the launchd plist file.
	launchdPlistTemplate = `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>%s</string>
    <key>ProgramArguments</key>
    <array>
        <string>%s</string>
        <string>-f</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
    <key>StandardOutPathString</key>
    <string>%s</string>
    <key>StandardErrorPathString</key>
    <string>%s</string>
    <key>WorkingDirectory</key>
    <string>%s</string>
    <key>EnvironmentVariables</key>
    <dict>
        <key>PATH</key>
        <string>/usr/bin:/bin:/usr/sbin:/sbin:/usr/local/bin:/opt/homebrew/bin</string>
    </dict>
</dict>
</plist>
`
)

// LaunchAgentController manages the launchd agent for the daemon.
type LaunchAgentController struct {
	meeptDir   string
	plistPath  string
	daemonPath string
	logPath    string
	errLogPath string
	workingDir string
}

// NewLaunchAgentController creates a new LaunchAgentController.
func NewLaunchAgentController() (*LaunchAgentController, error) {
	// Get home directory
	homeDir, err := os.UserHomeDir()
	if err != nil {
		// Try user.Current as fallback
		if u, err := user.Current(); err == nil {
			homeDir = u.HomeDir
		} else {
			return nil, fmt.Errorf("failed to get home directory: %w", err)
		}
	}

	meeptDir := filepath.Join(homeDir, ".meept")
	launchAgentsDir := filepath.Join(homeDir, "Library", "LaunchAgents")

	// Ensure directories exist
	if err := os.MkdirAll(meeptDir, 0o755); err != nil { //nolint:gosec // task workspace dirs are user-readable
		return nil, fmt.Errorf("failed to create meept directory: %w", err)
	}
	if err := os.MkdirAll(launchAgentsDir, 0o700); err != nil {
		return nil, fmt.Errorf("failed to create LaunchAgents directory: %w", err)
	}

	// Find daemon binary
	daemonPath, err := findDaemonBinary()
	if err != nil {
		// Use a default path if not found
		daemonPath = "/usr/local/bin/meept-daemon"
	}

	return &LaunchAgentController{
		meeptDir:   meeptDir,
		plistPath:  filepath.Join(launchAgentsDir, "com.caimlas.meept-daemon.plist"),
		daemonPath: daemonPath,
		logPath:    filepath.Join(meeptDir, "daemon.log"),
		errLogPath: filepath.Join(meeptDir, "daemon.err"),
		workingDir: homeDir,
	}, nil
}

// findDaemonBinary tries to find the meept-daemon binary.
func findDaemonBinary() (string, error) {
	// Check common locations
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
			// Convert to absolute path
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

// generatePlist generates the launchd plist content.
func (c *LaunchAgentController) generatePlist() string {
	return fmt.Sprintf(
		launchdPlistTemplate,
		LaunchAgentLabel,
		c.daemonPath,
		c.logPath,
		c.errLogPath,
		c.workingDir,
	)
}

// ensurePlistFile writes the plist file if it doesn't exist.
func (c *LaunchAgentController) ensurePlistFile() error {
	content := c.generatePlist()
	return os.WriteFile(c.plistPath, []byte(content), 0o644) //nolint:gosec // workspace plan/data files are user-readable
}

// IsLoaded checks if the launchd agent is currently loaded.
func (c *LaunchAgentController) IsLoaded() bool {
	cmd := exec.Command("launchctl", "list", LaunchAgentLabel) //nolint:gosec // path is constructed from known config values
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	err := cmd.Run()
	// If the command succeeds and output contains the label, it's loaded
	return err == nil && strings.Contains(out.String(), LaunchAgentLabel)
}

// IsRunning checks if the daemon process is actually running.
func (c *LaunchAgentController) IsRunning() bool {
	// Check PID file
	pidFile := filepath.Join(c.meeptDir, "meept.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return false
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return false
	}

	// Check if process is running
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Send signal 0 to check if process exists
	return proc.Signal(syscall.Signal(0)) == nil
}

// GetPID returns the daemon PID if running, 0 otherwise.
func (c *LaunchAgentController) GetPID() int {
	pidFile := filepath.Join(c.meeptDir, "meept.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0
	}

	// Verify process is running
	proc, err := os.FindProcess(pid)
	if err != nil {
		return 0
	}

	if proc.Signal(syscall.Signal(0)) != nil {
		return 0
	}

	return pid
}

// GetUptime returns the uptime of the daemon.
func (c *LaunchAgentController) GetUptime() time.Duration {
	pidFile := filepath.Join(c.meeptDir, "meept.pid")
	data, err := os.ReadFile(pidFile)
	if err != nil {
		return 0
	}

	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return 0
	}

	// Get process info
	cmd := exec.Command("ps", "-o", "etime=", "-p", strconv.Itoa(pid)) //nolint:gosec // path is constructed from known config values
	var out bytes.Buffer
	cmd.Stdout = &out

	if err := cmd.Run(); err != nil {
		return 0
	}

	// Parse elapsed time (format: [[DD-]hh:]mm:ss)
	elapsed := strings.TrimSpace(out.String())
	return parseElapsedTime(elapsed)
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
		// Could be DD-HH:MM:SS or HH:MM:SS
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

	// CORE-8 FIX: Use explicit time.Duration arithmetic instead of
	// int(time.Hour) casting. The old pattern `int(time.Hour)` was
	// fragile because it casts a Duration type to int before multiplying,
	// which can be confusing. Now we use `time.Duration(days)*24*time.Hour`
	// which keeps everything in Duration types and reads more clearly.
	return time.Duration(days)*24*time.Hour +
		time.Duration(hours)*time.Hour +
		time.Duration(mins)*time.Minute +
		time.Duration(secs)*time.Second
}

// Load loads the launchd agent (creates plist and loads it).
func (c *LaunchAgentController) Load() error {
	// Generate and write plist file
	if err := c.ensurePlistFile(); err != nil {
		return fmt.Errorf("failed to create plist file: %w", err)
	}

	// Unload first if already loaded (ignore errors)
	_ = exec.Command("launchctl", "unload", c.plistPath).Run() //nolint:gosec // path is constructed from known config values

	// Load the agent
	cmd := exec.Command("launchctl", "load", "-w", c.plistPath) //nolint:gosec // path is constructed from known config values
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to load launchd agent: %w, output: %s", err, out.String())
	}

	return nil
}

// Unload unloads the launchd agent.
func (c *LaunchAgentController) Unload() error {
	if !c.IsLoaded() {
		return nil
	}

	cmd := exec.Command("launchctl", "unload", "-w", c.plistPath) //nolint:gosec // path is constructed from known config values
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to unload launchd agent: %w, output: %s", err, out.String())
	}

	return nil
}

// Start starts the daemon via launchd.
func (c *LaunchAgentController) Start() error {
	// Ensure plist is loaded
	if err := c.Load(); err != nil {
		return err
	}

	// Start the service
	cmd := exec.Command("launchctl", "kickstart", "-k", LaunchAgentLabel) //nolint:gosec // path is constructed from known config values
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		// kickstart might fail if already running, that's ok
		if !strings.Contains(out.String(), "already running") {
			return fmt.Errorf("failed to start launchd agent: %w, output: %s", err, out.String())
		}
	}

	return nil
}

// Stop stops the daemon via launchd.
func (c *LaunchAgentController) Stop() error {
	if !c.IsLoaded() {
		return nil
	}

	cmd := exec.Command("launchctl", "stop", LaunchAgentLabel) //nolint:gosec // path is constructed from known config values
	var out bytes.Buffer
	cmd.Stdout = &out
	cmd.Stderr = &out

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("failed to stop launchd agent: %w, output: %s", err, out.String())
	}

	return nil
}

// Restart restarts the daemon via launchd.
func (c *LaunchAgentController) Restart() error {
	if err := c.Stop(); err != nil {
		return err
	}

	// Brief pause to ensure clean stop
	time.Sleep(500 * time.Millisecond)

	return c.Start()
}

// Install installs the launchd agent for auto-start on login.
func (c *LaunchAgentController) Install() error {
	return c.Load()
}

// Uninstall removes the launchd agent.
func (c *LaunchAgentController) Uninstall() error {
	if err := c.Unload(); err != nil {
		return err
	}

	// Remove plist file
	return os.Remove(c.plistPath)
}

// DaemonController interface implementation for HTTP server

// DaemonControl provides daemon control functionality.
//
//nolint:revive // stutter with package name is intentional for API clarity
type DaemonControl struct {
	controller *LaunchAgentController
}

// NewDaemonControl creates a new DaemonControl.
func NewDaemonControl() (*DaemonControl, error) {
	controller, err := NewLaunchAgentController()
	if err != nil {
		return nil, err
	}

	return &DaemonControl{
		controller: controller,
	}, nil
}

// IsRunning returns true if the daemon is running.
func (d *DaemonControl) IsRunning() bool {
	return d.controller.IsRunning()
}

// PID returns the daemon PID.
func (d *DaemonControl) PID() int {
	return d.controller.GetPID()
}

// Uptime returns the daemon uptime.
func (d *DaemonControl) Uptime() time.Duration {
	return d.controller.GetUptime()
}

// Restart restarts the daemon.
func (d *DaemonControl) Restart(ctx context.Context) error {
	return d.controller.Restart()
}
