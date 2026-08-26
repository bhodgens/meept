// health.go implements daemon health checks backing daemon.health RPC
// and the meept doctor CLI surface (leaf: loop-economics/06-doctor-lifecycle).
package daemon

import (
	"database/sql"
	"fmt"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	_ "modernc.org/sqlite"
)

// HealthStatus is the tri-state result of a single check.
type HealthStatus string

const (
	StatusPass HealthStatus = "pass"
	StatusWarn HealthStatus = "warn"
	StatusFail HealthStatus = "fail"
)

// HealthCheck is one named diagnostic result.
type HealthCheck struct {
	Name   string       `json:"name"`
	Status HealthStatus `json:"ok"`
	Detail string       `json:"detail"`
}

// MarshalJSON emits ok as a bool for wire compatibility with the
// daemon.health contract ({name, ok, detail}).
func (c HealthCheck) MarshalJSON() ([]byte, error) {
	ok := c.Status == StatusPass || c.Status == StatusWarn
	detail := c.Detail
	if detail == "" {
		detail = string(c.Status)
	}
	return []byte(fmt.Sprintf(`{"name":%q,"ok":%t,"detail":%q}`, c.Name, ok, detail)), nil
}

// RuntimeProcsFunc lists processes that look like local LLM runtimes.
type RuntimeProcsFunc func() []string

// HealthOptions carries injected paths/procs so every check is unit-testable.
type HealthOptions struct {
	SocketPath   string
	PIDFile      string
	StateDir     string
	DBPaths      []string // sqlite databases to quick_check
	ConfigPath   string   // optional config file to parse-check
	StartTime    time.Time
	RuntimeProcs RuntimeProcsFunc
	Now          func() time.Time
	DiskMinBytes int64 // warn threshold; 0 => 200MB default
}

// diskFreeWarnBytes is the low-disk warning threshold (>200MB required).
const diskFreeWarnBytes = int64(200) * 1024 * 1024

// RunHealthChecks executes all checks and returns their results.
func RunHealthChecks(opts HealthOptions) []HealthCheck {
	if opts.Now == nil {
		opts.Now = time.Now
	}
	if opts.DiskMinBytes == 0 {
		opts.DiskMinBytes = diskFreeWarnBytes
	}
	checks := []HealthCheck{
		checkSocketListening(opts.SocketPath),
		checkPIDFileFresh(opts.PIDFile),
		checkDataDirWritable(opts.StateDir),
		checkConfigParse(opts.ConfigPath),
		checkDiskFreeThreshold(opts.StateDir, opts.DiskMinBytes),
		checkRuntimeProcesses(opts.RuntimeProcs),
	}
	if opts.StartTime.IsZero() {
		checks = append(checks, HealthCheck{Name: "uptime", Status: StatusWarn, Detail: "start time unknown"})
	} else {
		up := opts.Now().Sub(opts.StartTime).Truncate(time.Second)
		checks = append(checks, HealthCheck{Name: "uptime", Status: StatusPass, Detail: up.String()})
	}
	for _, dbPath := range opts.DBPaths {
		checks = append(checks, checkSQLiteIntegrity(dbPath))
	}
	return checks
}

func checkSocketListening(socketPath string) HealthCheck {
	name := "socket-listening"
	if socketPath == "" {
		return HealthCheck{name, StatusFail, "socket path not configured"}
	}
	conn, err := net.DialTimeout("unix", socketPath, 2*time.Second)
	if err != nil {
		return HealthCheck{name, StatusFail, fmt.Sprintf("no listener on %s: %v", socketPath, err)}
	}
	if cerr := conn.Close(); cerr != nil {
		slog.Debug("doctor: probe close failed", "error", cerr)
	}
	return HealthCheck{name, StatusPass, "listening on " + socketPath}
}

func checkPIDFileFresh(pidFile string) HealthCheck {
	const name = "pidfile-fresh"
	data, err := os.ReadFile(pidFile)
	if err != nil {
		if os.IsNotExist(err) {
			return HealthCheck{name, StatusFail, "pidfile missing"}
		}
		return HealthCheck{name, StatusFail, err.Error()}
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return HealthCheck{name, StatusFail, "pidfile contains non-numeric pid"}
	}
	proc, err := os.FindProcess(pid)
	if err == nil {
		if serr := proc.Signal(syscall.Signal(0)); serr == nil {
			return HealthCheck{name, StatusPass, fmt.Sprintf("pid %d alive", pid)}
		}
	}
	return HealthCheck{name, StatusFail, fmt.Sprintf("pid %d not running", pid)}
}

func checkSQLiteIntegrity(dbPath string) HealthCheck {
	const name = "sqlite-integrity"
	if _, err := os.Stat(dbPath); err != nil {
		return HealthCheck{name, StatusFail, fmt.Sprintf("database missing: %s", dbPath)}
	}
	db, err := sql.Open("sqlite", dbPath+"?mode=ro")
	if err != nil {
		return HealthCheck{name, StatusFail, fmt.Sprintf("open failed: %v", err)}
	}
	defer db.Close()
	var result string
	if err := db.QueryRow("PRAGMA quick_check").Scan(&result); err != nil {
		return HealthCheck{name, StatusFail, fmt.Sprintf("quick_check failed: %v", err)}
	}
	if result != "ok" {
		return HealthCheck{name, StatusFail, "quick_check: " + result}
	}
	return HealthCheck{name, StatusPass, filepath.Base(dbPath) + ": ok"}
}

func checkDataDirWritable(stateDir string) HealthCheck {
	const name = "data-dir-writable"
	if stateDir == "" {
		return HealthCheck{name, StatusFail, "state dir not configured"}
	}
	if _, err := os.Stat(stateDir); os.IsNotExist(err) {
		return HealthCheck{name, StatusFail, fmt.Sprintf("%s does not exist", stateDir)}
	}
	probe := filepath.Join(stateDir, ".doctor-write-probe")
	if err := os.WriteFile(probe, []byte("ok"), 0o600); err != nil {
		return HealthCheck{name, StatusFail, fmt.Sprintf("%s not writable: %v", stateDir, err)}
	}
	if rmErr := os.Remove(probe); rmErr != nil && !os.IsNotExist(rmErr) {
		slog.Debug("doctor: probe cleanup failed", "error", rmErr)
	}
	return HealthCheck{name, StatusPass, stateDir + " writable"}
}

func checkConfigParse(configPath string) HealthCheck {
	const name = "config-parse"
	if configPath == "" {
		return HealthCheck{name, StatusWarn, "no config file set"}
	}
	f, err := os.Open(configPath)
	if err != nil {
		return HealthCheck{name, StatusFail, err.Error()}
	}
	if cerr := f.Close(); cerr != nil {
		slog.Debug("doctor: temp close failed", "error", cerr)
	}
	return HealthCheck{name, StatusPass, "readable: " + configPath}
}

func checkDiskFreeThreshold(dir string, minBytes int64) HealthCheck {
	const name = "disk-free"
	free, err := diskFreeBytes(dir)
	if err != nil {
		return HealthCheck{name, StatusWarn, fmt.Sprintf("statfs failed: %v", err)}
	}
	human := func(b int64) string { return fmt.Sprintf("%.0fMB", float64(b)/(1024*1024)) }
	switch {
	case free <= minBytes/4:
		return HealthCheck{name, StatusFail, fmt.Sprintf("only %s free (<%s)", human(free), human(minBytes))}
	case free < minBytes:
		return HealthCheck{name, StatusWarn, fmt.Sprintf("%s free (below %s threshold)", human(free), human(minBytes))}
	default:
		return HealthCheck{name, StatusPass, fmt.Sprintf("%s free", human(free))}
	}
}

func checkRuntimeProcesses(procs RuntimeProcsFunc) HealthCheck {
	const name = "runtime-processes"
	if procs == nil {
		return HealthCheck{name, StatusWarn, "runtime process scanner unavailable"}
	}
	found := procs()
	if len(found) == 0 {
		return HealthCheck{name, StatusWarn, "no local llm runtime processes detected"}
	}
	return HealthCheck{name, StatusPass, strings.Join(found, ", ")}
}

// listRuntimeProcs scans ps output for known local LLM runtime binaries
// (llama.cpp / MLX servers). Returns display names for healthy-looking procs.
func listRuntimeProcs() []string {
	out, err := exec.Command("ps", "-axo", "command").Output()
	if err != nil {
		return nil
	}
	var found []string
	for _, line := range strings.Split(string(out), "\n") {
		l := strings.ToLower(line)
		switch {
		case strings.Contains(l, "llama-server"), strings.Contains(l, "llama.cpp"),
			strings.Contains(l, "mlx"), strings.Contains(l, "mlx_lm.server"):
			found = append(found, strings.TrimSpace(line))
		}
	}
	return found
}

// IsStalePIDFile reports whether the given pidfile refers to a dead process.
// Used by doctor --fix before removing anything.
func IsStalePIDFile(pidFile string) bool {
	res := checkPIDFileFresh(pidFile)
	return res.Status == StatusFail
}

// StaleSocketExists reports whether a unix-socket file has no listener.
func StaleSocketExists(socketPath string) bool {
	if _, err := os.Stat(socketPath); err != nil {
		return false
	}
	conn, err := net.DialTimeout("unix", socketPath, time.Second)
	if err != nil {
		return true
	}
	if cerr := conn.Close(); cerr != nil {
		slog.Debug("doctor: probe close failed", "error", cerr)
	}
	return false
}

// diskFreeBytes returns available bytes on the filesystem containing dir.
func diskFreeBytes(dir string) (int64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, fmt.Errorf("statfs %s: %w", dir, err)
	}
	return int64(st.Bavail) * int64(st.Bsize), nil
}
