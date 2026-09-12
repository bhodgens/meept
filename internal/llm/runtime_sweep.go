package llm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
)

// Startup orphan sweep.
//
// A local runtime is a direct child of the meept process that spawned it, in
// its own process group (Setpgid, see RuntimeProcess.Start). macOS has no
// parent-death signal, so when a daemon dies hard (SIGKILL, panic, session
// teardown) its children are re-parented to init and keep running: they hold a
// full model in memory and the endpoint port. The ownership guard in
// runtime_process.go then prevents every LATER boot from stopping them (a
// foreign instance token is adopted observed-not-owned), so nothing ever reaps
// them — the leak is permanent until an operator kills the pid by hand.
//
// This sweep closes that hole with the two facts the daemon does have: the
// configured spawn command of every endpoint, and the process table. A process
// whose parent is init (ppid==1, so the meept process that spawned it is gone)
// and whose command line is exactly an endpoint's spawn command is a leftover
// of a dead generation. It is stopped before the current boot spawns its own
// runtimes, because it holds the endpoint port and the duplicate-spawn
// pre-check would otherwise refuse the spawn.
//
// Only endpoints with auto_stop_on_exit=true are swept: a runtime the user
// configured to outlive the daemon is left alone.
//
// Windows has no ps: the scan fails, the sweep skips, nothing is killed
// (documented platform gap, same posture as the rest of the lifecycle code).

// OrphanRuntime is one runtime process left behind by a meept process that no
// longer exists.
type OrphanRuntime struct {
	EndpointKey string
	PID         int
	Command     string
}

// RuntimeProcInfo is one row of the process table used for orphan detection.
type RuntimeProcInfo struct {
	PID     int
	PPID    int
	Command string
}

// RuntimeProcLister lists the process table (test seam over ps).
type RuntimeProcLister func() ([]RuntimeProcInfo, error)

// ListRuntimeProcesses reads the process table with ps. The error is returned
// rather than swallowed so the sweep can skip instead of guessing.
func ListRuntimeProcesses() ([]RuntimeProcInfo, error) {
	out, err := exec.Command("ps", "-axo", "pid=,ppid=,command=").Output()
	if err != nil {
		return nil, fmt.Errorf("ps scan failed: %w", err)
	}
	var procs []RuntimeProcInfo
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 3 {
			continue
		}
		pid, pidErr := strconv.Atoi(fields[0])
		if pidErr != nil {
			continue
		}
		ppid, ppidErr := strconv.Atoi(fields[1])
		if ppidErr != nil {
			continue
		}
		procs = append(procs, RuntimeProcInfo{
			PID:     pid,
			PPID:    ppid,
			Command: strings.Join(fields[2:], " "),
		})
	}
	return procs, nil
}

// FindOrphanRuntimes returns runtime processes that a dead meept process left
// behind. Report-only: the caller decides whether to signal them.
func FindOrphanRuntimes(cfgs []*RuntimeConfig, list RuntimeProcLister) ([]OrphanRuntime, error) {
	procs, err := list()
	if err != nil {
		return nil, err
	}
	var orphans []OrphanRuntime
	for _, cfg := range cfgs {
		if !sweepableEndpoint(cfg) {
			continue
		}
		for _, p := range procs {
			if p.PPID != 1 || !matchesSpawnCommand(p.Command, cfg.SpawnCommand) {
				continue
			}
			orphans = append(orphans, OrphanRuntime{
				EndpointKey: cfg.EndpointKey,
				PID:         p.PID,
				Command:     p.Command,
			})
		}
	}
	return orphans, nil
}

// OrphanRuntimesFromConfigs reports leftover runtime processes for a set of
// runtime configs (used by `meept doctor`, which has no RuntimeManager).
func OrphanRuntimesFromConfigs(cfgs []*RuntimeConfig) ([]OrphanRuntime, error) {
	return FindOrphanRuntimes(cfgs, ListRuntimeProcesses)
}

// OrphanRuntimes reports leftover runtime processes for this manager's
// endpoints without signalling anything.
func (m *RuntimeManager) OrphanRuntimes() ([]OrphanRuntime, error) {
	return FindOrphanRuntimes(m.sweepCandidates(), m.listRuntimeProcesses())
}

// SweepOrphanRuntimes stops every runtime a previous meept generation left
// behind: SIGTERM to the process group, then SIGKILL after waitAfterTerm, then
// the endpoint PID file is removed when it still names the reaped pid. Returns
// the reaped pids (nil when nothing matched). It never returns an error: a
// failed sweep must not block daemon start.
func (m *RuntimeManager) SweepOrphanRuntimes(waitAfterTerm time.Duration) []int {
	orphans, err := FindOrphanRuntimes(m.sweepCandidates(), m.listRuntimeProcesses())
	if err != nil {
		m.logger.Warn("orphan sweep: process scan unavailable; skipping", "error", err)
		return nil
	}
	if len(orphans) == 0 {
		return nil
	}
	for _, o := range orphans {
		m.logger.Warn("orphan sweep: stopping runtime left behind by a previous meept process",
			"endpoint_key", o.EndpointKey, "pid", o.PID, "command", o.Command)
		if sigErr := m.signalRuntime(o.PID, syscall.SIGTERM); sigErr != nil {
			m.logger.Debug("orphan sweep: SIGTERM failed", "pid", o.PID, "error", sigErr)
		}
	}
	if waitAfterTerm > 0 {
		time.Sleep(waitAfterTerm)
	}
	reaped := make([]int, 0, len(orphans))
	for _, o := range orphans {
		// SIGKILL is unconditional: a SIGTERM-resistant runtime (blocked in a
		// Metal call, no signal handler) would otherwise keep the port.
		if sigErr := m.signalRuntime(o.PID, syscall.SIGKILL); sigErr != nil {
			m.logger.Debug("orphan sweep: SIGKILL failed", "pid", o.PID, "error", sigErr)
		}
		reaped = append(reaped, o.PID)
		m.removePIDFileForPid(o.EndpointKey, o.PID)
	}
	return reaped
}

// sweepCandidates snapshots the endpoint configs the sweep may reap. The
// manager lock is not held across the scan or the signals.
func (m *RuntimeManager) sweepCandidates() []*RuntimeConfig {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*RuntimeConfig, 0, len(m.endpoints))
	for _, ep := range m.endpoints {
		if sweepableEndpoint(ep.cfg) {
			out = append(out, ep.cfg)
		}
	}
	return out
}

// sweepableEndpoint reports whether a leftover process for cfg may be reaped:
// the config must ask for the runtime to stop with the daemon, and it must have
// a spawn command to match the process table against.
func sweepableEndpoint(cfg *RuntimeConfig) bool {
	return cfg != nil && cfg.AutoStop && len(cfg.SpawnCommand) > 0
}

// signalRuntime sends sig to a runtime pid (process group when resolvable).
func (m *RuntimeManager) signalRuntime(pid int, sig syscall.Signal) error {
	if m.sweepSignal == nil {
		return killProcessGroupPID(pid, sig)
	}
	return m.sweepSignal(pid, sig)
}

// listRuntimeProcesses returns the process-table scanner for the sweep.
func (m *RuntimeManager) listRuntimeProcesses() RuntimeProcLister {
	if m.sweepLister == nil {
		return ListRuntimeProcesses
	}
	return m.sweepLister
}

// removePIDFileForPid removes the endpoint PID file when it still names pid,
// so the next Start() does not adopt a process that is now gone.
func (m *RuntimeManager) removePIDFileForPid(endpointKey string, pid int) {
	m.mu.Lock()
	ep, ok := m.endpoints[endpointKey]
	m.mu.Unlock()
	if !ok || ep.cfg == nil || ep.cfg.PIDFile == "" {
		return
	}
	filePID, err := ParsePIDFile(ep.cfg.PIDFile)
	if err != nil || filePID != pid {
		return
	}
	if rmErr := os.Remove(ep.cfg.PIDFile); rmErr != nil {
		m.logger.Debug("orphan sweep: pid file removal failed",
			"pid_file", ep.cfg.PIDFile, "error", rmErr)
	}
}

// matchesSpawnCommand reports whether a process command line came from spawn.
// The trailing arguments must equal spawn[1:] exactly, and either argv[0] or
// argv[1] must name the same binary as spawn[0] — script-based runtimes get an
// interpreter prefix in ps (mlx_lm shows as "<python> /path/mlx_lm server ...").
func matchesSpawnCommand(command string, spawn []string) bool {
	if len(spawn) == 0 {
		return false
	}
	fields := strings.Fields(command)
	if len(fields) < len(spawn) {
		return false
	}
	want := filepath.Base(spawn[0])
	head := fields[:len(fields)-len(spawn)+1]
	headMatch := false
	for _, token := range head {
		if filepath.Base(token) == want {
			headMatch = true
			break
		}
	}
	if !headMatch {
		return false
	}
	tail := fields[len(fields)-len(spawn)+1:]
	for i, want := range spawn[1:] {
		if tail[i] != want {
			return false
		}
	}
	return true
}
