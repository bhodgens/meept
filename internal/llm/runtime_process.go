package llm

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// RuntimeProcess manages a spawned LLM runtime process.
// All fields are protected by mu to prevent data races between Start, Stop,
// PID, and IsRunning callers (e.g. RuntimeManager.Status runs concurrently
// with StartProvider/StopProvider).
type RuntimeProcess struct {
	mu      sync.Mutex
	config  *RuntimeConfig
	cmd     *exec.Cmd
	pid     int
	pidFile string
	// instanceToken identifies the manager INSTANCE that constructed this
	// RuntimeProcess (a fresh 16-byte random token per construction — i.e.
	// per daemon boot, or per short-lived constructing process). It is
	// written into pidfiles this instance spawns and checked on adoption:
	// a pidfile whose token matches ours was written by this same instance
	// (same-boot re-Start path) and its process may be adopted as OWNED;
	// a foreign token (a second daemon, a test binary, a CLI process —
	// anything sharing the run dir) or a legacy tokenless pidfile is adopted
	// as OBSERVED, NOT OWNED. Such a process must never be killed through
	// Stop()/StopAll(); only its live state and endpoint health are this
	// instance's business.
	instanceToken string
	// spawnedByUs records whether THIS instance spawned or owned-adopted the
	// runtime process. Only an owner may Stop it: a secondary process (CLI
	// invocation, eval harness, tool subprocess) that constructed the LLM
	// stack and exits must never kill the daemon's healthy llama-server via
	// the shared PID file.
	spawnedByUs bool
	// waitDone receives the result of cmd.Wait() exactly once.
	// Created in Start(); consumed by Stop() to avoid a double-Wait race.
	waitDone chan error
}

// pidfileEntry is the on-disk pidfile record. The current format is a JSON
// object: {"pid":1234,"token":"<hex>"} written atomically (temp file +
// rename). Pidfiles written before instance tokens existed are a bare
// decimal integer; parsePIDFile still accepts that legacy format, and an
// entry with an empty Token is treated as OBSERVED-NOT-OWNED on adoption.
type pidfileEntry struct {
	PID   int    `json:"pid"`
	Token string `json:"token,omitempty"`
}

// ParsePIDFile reads and parses a runtime pidfile, returning its PID.
// Accepts both the current JSON format and the legacy bare-int format.
// Exported for CLI consumers (cmd/meept runtime start/stop) that inspect
// the same pidfiles without owning a RuntimeProcess.
func ParsePIDFile(path string) (int, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return 0, err
	}
	var entry pidfileEntry
	if err := json.Unmarshal(data, &entry); err == nil && entry.PID > 0 {
		return entry.PID, nil
	}
	// Legacy bare-int pidfile.
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		return 0, fmt.Errorf("invalid pidfile %s: %w", path, err)
	}
	return pid, nil
}

// NewRuntimeProcess creates a new process manager.
func NewRuntimeProcess(cfg *RuntimeConfig) *RuntimeProcess {
	return &RuntimeProcess{
		config:        cfg,
		pidFile:       cfg.PIDFile,
		instanceToken: newInstanceToken(),
	}
}

// newInstanceToken generates the identity token for one manager instance:
// 16 crypto/rand bytes, hex-encoded. There is no predictable fallback: a
// guessable token would defeat the ownership guarantee, so rand failure
// degrades to the empty token, which adoption treats as
// observed-not-owned (the safe direction).
func newInstanceToken() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return ""
	}
	return hex.EncodeToString(b)
}

// AlreadyRunning reports whether the runtime process is already running
// according to the PID file. Returns true when the PID file exists, parses,
// and the identified process is alive (signal-0 succeeds). Callers use this
// to decide whether to truncate the process log before calling Start: an
// already-running process should not have its log truncated because no new
// subprocess will be spawned.
func (p *RuntimeProcess) AlreadyRunning() bool {
	entry, err := p.readPIDFile()
	if err != nil || entry.PID <= 0 {
		return false
	}
	return p.isProcessRunning(entry.PID)
}

// endpointProbeTimeout bounds the pre-spawn duplicate check. A local listener
// answers or refuses in microseconds; the timeout only protects against a
// firewall that drops packets.
const endpointProbeTimeout = 300 * time.Millisecond

// endpointAddr returns the host:port this runtime is expected to serve, derived
// from the base URL the manager resolved. Empty means "unknown" (CLI paths):
// the duplicate-spawn pre-check is then skipped.
func (p *RuntimeProcess) endpointAddr() string {
	if p.config == nil || p.config.BaseURL == "" {
		return ""
	}
	host, port := hostPortFromBaseURL(p.config.BaseURL)
	if host == "" || port == "" {
		return ""
	}
	return net.JoinHostPort(host, port)
}

// endpointProbeTarget returns the address the duplicate-spawn pre-check must
// probe, and whether the check applies at all. It applies only when both are
// known: the endpoint address (from the resolved base URL) and that the spawn
// command itself declares the endpoint port. A command that never mentions the
// port is not the thing that binds it, so refusing there would be wrong.
func (p *RuntimeProcess) endpointProbeTarget() (string, bool) {
	addr := p.endpointAddr()
	if addr == "" {
		return "", false
	}
	_, port, err := net.SplitHostPort(addr)
	if err != nil || port == "" {
		return "", false
	}
	if p.config == nil || !spawnCommandBindsPort(p.config.SpawnCommand, port) {
		return "", false
	}
	return addr, true
}

// spawnCommandBindsPort reports whether the spawn command declares the given
// port as a standalone argument (e.g. "--port", "8082") or an assignment
// ("--port=8082", "ROUTER_PORT=8082"). Matching whole tokens keeps paths and
// hosts that merely contain the digits from counting.
func spawnCommandBindsPort(spawn []string, port string) bool {
	if port == "" {
		return false
	}
	for _, token := range spawn {
		if token == port || strings.HasSuffix(token, "="+port) {
			return true
		}
	}
	return false
}

// endpointInUse reports whether another process already accepts TCP
// connections on addr. Only a completed dial counts as "in use"; a refused or
// timed-out dial means the address looks free, so the caller may spawn.
func endpointInUse(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, endpointProbeTimeout)
	if err != nil {
		return false
	}
	if cerr := conn.Close(); cerr != nil {
		slog.Debug("endpoint probe: close", "addr", addr, "error", cerr)
	}
	return true
}

// Start spawns the runtime process. stdout and stderr are used for the
// subprocess's output streams; nil falls back to os.Stdout/os.Stderr.
//
// Adoption semantics (docs/bugs-and-gaps.md "Runtime adoption ownership
// race"): if the PID file names a live process,
//   - a pidfile carrying THIS instance's token (same-boot re-Start) is
//     adopted as OWNED — spawnedByUs stays true and Stop() remains
//     authorized;
//   - anything else (a foreign instance sharing the run dir, or a legacy
//     tokenless pidfile) is adopted as OBSERVED, NOT OWNED — this instance
//     records the PID and lets health checks verify the endpoint, but
//     Stop()/StopAll() refuse to kill a process it never spawned.
func (p *RuntimeProcess) Start(ctx context.Context, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	// Serialize the whole probe + adopt + spawn + PID-file-write sequence with
	// the endpoint start lock, so a second meept process cannot slip a spawn
	// into the window between the probe and the new PID file. The lock is
	// released on return and is not held across the health wait.
	unlockStart, lockErr := acquireStartLock(p.pidFile)
	if lockErr != nil {
		return lockErr
	}
	defer unlockStart()

	// Duplicate-spawn pre-check. The probe runs BEFORE p.mu is taken (network
	// I/O under the mutex breaks the mutex-scope rule) and only when the spawn
	// command declares the endpoint port (see endpointProbeTarget). An endpoint
	// owned by another meept instance still looks busy here; that case is
	// handled by the PID-file adoption branch below, which returns before the
	// refusal.
	probeAddr, probeApplies := p.endpointProbeTarget()
	endpointBusy := probeApplies && endpointInUse(probeAddr)
	p.mu.Lock()
	defer p.mu.Unlock()
	// Check if already running via PID file
	if entry, err := p.readPIDFile(); err == nil && entry.PID > 0 {
		if p.isProcessRunning(entry.PID) {
			if entry.Token == p.instanceToken {
				// Same-instance re-Start: the live process carries THIS
				// boot's token (e.g. a health-driven restart of a runtime
				// we spawned earlier in this boot). Adopt as OWNED so
				// Stop() remains authorized.
				p.pid = entry.PID
				p.spawnedByUs = true
				return nil // Already running (ours)
			}
			// Cross-instance adoption (second daemon boot, test binary,
			// CLI process — anything sharing this run dir) or a legacy
			// tokenless pidfile: adopt as OBSERVED, NOT OWNED. Record the
			// PID so Start is a no-op and health checks can verify the
			// endpoint, but do NOT grant kill rights: Stop() skips
			// non-owned processes, so a foreign instance's StopAll can no
			// longer take down a runtime it never spawned.
			p.pid = entry.PID
			p.spawnedByUs = false
			slog.Info("adopted external runtime (observed, not owned)",
				"pid", entry.PID,
				"pid_file", p.pidFile,
				"legacy_format", entry.Token == "")
			return nil // Already running (foreign)
		}
		// Stale PID file
		os.Remove(p.pidFile)
	}
	p.spawnedByUs = true

	// Validate spawn command
	if len(p.config.SpawnCommand) == 0 {
		return fmt.Errorf("no spawn command configured")
	}

	name := p.config.SpawnCommand[0]
	args := p.config.SpawnCommand[1:]

	// Refuse to spawn into an endpoint port this spawn command declares but
	// another process already serves. The child would fail to bind; for
	// script-based runtimes (mlx_lm) the process then survives as a loaded
	// model with no socket, and the periodic health check cannot see the
	// failure because the foreign listener answers it. Failing loudly here is
	// the only way an operator learns the port is taken.
	if endpointBusy {
		return fmt.Errorf("refusing to spawn %s: endpoint %s already has a listener "+
			"(a runtime from an earlier run, a hand-started server, or another "+
			"service on that port); stop that process or move this runtime to a "+
			"free port", name, probeAddr)
	}

	// Detach the runtime process from the caller's context. Callers pass
	// request-scoped contexts (e.g. the daemon's StartAll goroutine whose
	// ctx is cancelled the moment StartAll returns after WaitForHealthy);
	// binding exec.CommandContext to such a ctx SIGKILLs the freshly loaded
	// llama-server ~1s after it becomes healthy. The runtime's lifetime is
	// governed by explicit Stop()/StopAll and the health checker instead —
	// both of which SIGTERM/SIGKILL the process group on real shutdown.
	spawnCtx := context.WithoutCancel(ctx)
	p.cmd = exec.CommandContext(spawnCtx, name, args...)
	p.cmd.Stdout = stdout
	p.cmd.Stderr = stderr
	p.cmd.Stdin = nil // Explicitly set stdin to nil to avoid blocking
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to spawn runtime: %w", err)
	}

	p.pid = p.cmd.Process.Pid

	// Write PID file (atomic; carries this instance's identity token).
	if err := p.writePIDFile(pidfileEntry{PID: p.pid, Token: p.instanceToken}); err != nil {
		// Best-effort kill: the spawn is being abandoned; a Kill error
		// would only mask the writePIDFile cause (process is reaped by
		// the wait goroutine regardless).
		_ = p.cmd.Process.Kill()
		return fmt.Errorf("failed to write PID file: %w", err)
	}

	// Durable spawn record: written beside the PID file so the startup orphan
	// sweep can still match this runtime after the config drifts (unmounted
	// model volume, renamed provider) — exactly when a leak would otherwise be
	// unreapable. Best-effort: a record failure must not fail the spawn.
	if p.pidFile != "" {
		recErr := WriteSpawnRecord(SpawnRecord{
			EndpointKey: p.config.EndpointKey,
			PIDFile:     p.pidFile,
			Argv:        p.config.SpawnCommand,
			AutoStop:    p.config.AutoStop,
			PID:         p.pid,
		})
		if recErr != nil {
			slog.Warn("spawn record: write failed; the orphan sweep will rely on the config",
				"pid", p.pid, "pid_file", p.pidFile, "error", recErr)
		}
	}

	// Start a goroutine to wait for the process to exit and prevent zombies.
	// This is necessary because Setpgid=true creates a new process group,
	// and without waiting, exited processes become defunct (zombies).
	p.waitDone = make(chan error, 1)
	go func() {
		err := p.cmd.Wait() //nolint:mutexio // Wait runs BEFORE Lock; no I/O under mutex
		p.waitDone <- err
		p.mu.Lock()
		p.pid = 0
		p.cmd = nil
		p.mu.Unlock()
		if err != nil {
			slog.Warn("runtime process wait returned error", "error", err)
		}
	}()

	return nil
}

// ErrRuntimeNotOwned reports that Stop refused to stop a runtime this instance
// did not spawn: the PID file names a runtime another meept process owns, which
// this instance may have adopted as observed-not-owned. Operator surfaces that
// deliberately stop it anyway (the `meept runtime stop` CLI) must use
// StopAsOperator instead of ignoring this error — reporting "stopped" while the
// process keeps running and holding the endpoint port is a false success.
var ErrRuntimeNotOwned = errors.New("runtime not owned by this instance")

// Stop gracefully terminates the runtime process.
// Non-owners are refused with ErrRuntimeNotOwned: a RuntimeProcess that neither
// spawned nor owned-adopted the runtime (e.g. the LLM stack constructed inside a
// short-lived CLI or eval subprocess, or an instance that adopted a foreign
// instance's runtime as observed-not-owned) must not kill the daemon's healthy
// llama-server through the shared PID file.
func (p *RuntimeProcess) Stop(ctx context.Context) error {
	return p.stopProcess(ctx, false)
}

// StopAsOperator stops the runtime recorded in this process's PID file even when
// this instance did not spawn it. Use it only for an explicit operator request:
// the error message on the refusal path tells the operator the runtime is owned
// by another meept process, and the automatic LLM-stack paths (StopAll, health
// restart) must keep using Stop so a short-lived CLI or eval process can never
// take down the daemon's runtime.
//
// Stays a no-op when there is nothing to stop (no live process behind the PID
// file), matching Stop.
func (p *RuntimeProcess) StopAsOperator(ctx context.Context) error {
	return p.stopProcess(ctx, true)
}

func (p *RuntimeProcess) stopProcess(ctx context.Context, asOperator bool) error {
	p.mu.Lock()
	if !asOperator && !p.spawnedByUs {
		p.mu.Unlock()
		return ErrRuntimeNotOwned
	}
	if p.cmd == nil || p.cmd.Process == nil {
		// Try to recover from PID file
		if entry, err := p.readPIDFile(); err == nil && entry.PID > 0 {
			// An operator stop signals a process this instance never spawned, so
			// the PID file is the only handle — and it outlives the process that
			// wrote it. Verify the pid still runs this runtime before signalling
			// it; a reused pid must never receive the operator's stop.
			if asOperator {
				if mismatch := p.identityMismatch(entry.PID); mismatch != nil {
					p.mu.Unlock()
					return mismatch
				}
			}
			proc, err := os.FindProcess(entry.PID)
			if err != nil {
				p.mu.Unlock()
				return nil
			}
			p.cmd = &exec.Cmd{}
			p.cmd.Process = proc
		} else {
			p.mu.Unlock()
			return nil // Not running
		}
	}

	// Snapshot the fields we need after releasing the lock.
	cmd := p.cmd
	waitDone := p.waitDone
	fromPIDFile := p.cmd.Process != nil && p.pid == 0 // recovered from PID file, no Wait goroutine
	p.mu.Unlock()

	// Send SIGTERM to the entire process group for a clean shutdown.
	// Setpgid=true in Start() isolates the child; killing the group
	// ensures no grandchild survives the daemon's death.
	if err := killProcessGroup(cmd, syscall.SIGTERM); err != nil {
		// Already dead
		p.clearPIDFileAndRecord()
		return nil
	}

	// Wait for process to exit (outside the lock — Wait blocks).
	if fromPIDFile || waitDone == nil {
		// No Wait() goroutine (recovered from PID file). Poll until
		// the process exits or the context expires.
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				// Best-effort kill + cleanup on cancellation: a Kill error
				// after ctx.Done cannot be surfaced to a caller that has
				// already given up, and the returned nil IS the signal.
				_ = killProcessGroup(cmd, syscall.SIGKILL)
				p.clearPIDFileAndRecord() // ctx already cancelled; nothing to report to
				return nil
			case <-ticker.C:
				if !p.isProcessRunning(cmd.Process.Pid) {
					p.clearPIDFileAndRecord() // process already exited; stale file removal is best-effort
					return nil
				}
			}
		}
	}

	select {
	case <-ctx.Done():
		// Force-kill the process group on context cancellation
		_ = killProcessGroup(cmd, syscall.SIGKILL)
	case <-waitDone:
	}

	p.clearPIDFileAndRecord() // terminal cleanup; the runtime result is already decided
	return nil
}

// clearPIDFileAndRecord removes the runtime's PID file and its durable spawn
// record. Called where the process is known gone, or where the daemon has given
// up waiting: leaving the record behind would make the orphan sweep chase a pid
// that no longer exists.
func (p *RuntimeProcess) clearPIDFileAndRecord() {
	if p.pidFile == "" {
		return
	}
	if err := os.Remove(p.pidFile); err != nil && !os.IsNotExist(err) {
		slog.Debug("runtime pid file removal failed", "pid_file", p.pidFile, "error", err)
	}
	RemoveSpawnRecord(p.pidFile)
}

// identityMismatch reports an error when the process named by the runtime PID
// file is not running the runtime that file was written for. The PID file
// outlives the process that wrote it, so its pid can have been reused; an
// operator stop (StopAsOperator) must not signal an unrelated process because of
// it. Identity comes from the durable spawn record when one exists, otherwise
// from the config's spawn command. nil means "cannot tell, nothing to compare".
func (p *RuntimeProcess) identityMismatch(pid int) error {
	var want []string
	if p.config != nil {
		want = p.config.SpawnCommand
	}
	if rec, err := ReadSpawnRecord(p.pidFile); err == nil && len(rec.Argv) > 0 {
		want = rec.Argv
	}
	if len(want) == 0 {
		return nil
	}
	procs, err := ListRuntimeProcesses()
	if err != nil {
		return nil // cannot verify: do not block an explicit operator stop on a scan failure
	}
	for _, proc := range procs {
		if proc.PID != pid {
			continue
		}
		if !matchesSpawnCommand(proc.Command, want) {
			return fmt.Errorf("pid %d runs %q, not this runtime's %q: refusing to signal it (the pid file may name a reused pid)",
				pid, proc.Command, strings.Join(want, " "))
		}
		return nil
	}
	return nil // pid is not in the process table: nothing to signal anyway
}

// PID returns the process ID.
func (p *RuntimeProcess) PID() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.pid
}

// IsRunning checks if the process is still alive.
func (p *RuntimeProcess) IsRunning() bool {
	p.mu.Lock()
	pid := p.pid
	p.mu.Unlock()
	if pid == 0 {
		return false
	}
	return p.isProcessRunning(pid)
}

// StalePIDRemoval cleans up a stale PID file for a given runtime config.
// This is useful when the daemon restarts and discovers orphaned PID files.
func (p *RuntimeProcess) StalePIDRemoval() {
	if entry, err := p.readPIDFile(); err == nil && entry.PID > 0 {
		if !p.isProcessRunning(entry.PID) {
			os.Remove(p.pidFile)
		}
	}
}

func (p *RuntimeProcess) isProcessRunning(pid int) bool {
	return processAlive(pid)
}

// killProcessGroup sends a signal to the process group of cmd.
// Falls back to killing just the leader if getpgid fails.
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("no process")
	}
	return killProcessGroupPID(cmd.Process.Pid, sig)
}

// killProcessGroupPID sends a signal to the process group led by pid, but only
// when pid actually leads that group. A target that is not its own group leader
// (a hand-started server whose shell is gone, a process in a shared group) is
// signalled alone: killing the group would reach processes that are not ours.
// The daemon's own runtimes are spawned with Setpgid, so they are leaders and
// keep the group behaviour (no grandchild outlives the runtime).
func killProcessGroupPID(pid int, sig syscall.Signal) error {
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid != pid {
		// Fallback: kill leader only
		return syscall.Kill(pid, sig)
	}
	return syscall.Kill(-pgid, sig)
}

func (p *RuntimeProcess) writePIDFile(entry pidfileEntry) error {
	dir := filepath.Dir(p.pidFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	data, err := json.Marshal(entry)
	if err != nil {
		return err
	}
	// Atomic write: never let a concurrent reader (AlreadyRunning, another
	// instance adopting) observe a torn or half-written pidfile.
	tmp, err := os.CreateTemp(dir, filepath.Base(p.pidFile)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once the rename below has succeeded
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, p.pidFile)
}

func (p *RuntimeProcess) readPIDFile() (pidfileEntry, error) {
	data, err := os.ReadFile(p.pidFile)
	if err != nil {
		return pidfileEntry{}, err
	}
	return parsePIDFile(data)
}

// parsePIDFile accepts both the current JSON pidfile format and the legacy
// bare-decimal-integer format. Legacy entries carry no token: adoption of
// the process they name is downgraded to observed-not-owned.
func parsePIDFile(data []byte) (pidfileEntry, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return pidfileEntry{}, fmt.Errorf("empty pidfile")
	}
	var entry pidfileEntry
	if trimmed[0] == '{' {
		if err := json.Unmarshal(trimmed, &entry); err != nil {
			return pidfileEntry{}, fmt.Errorf("invalid pidfile JSON: %w", err)
		}
	} else {
		pid, err := strconv.Atoi(string(trimmed))
		if err != nil {
			return pidfileEntry{}, fmt.Errorf("invalid pidfile (neither JSON nor integer): %w", err)
		}
		entry.PID = pid
	}
	if entry.PID <= 0 {
		return pidfileEntry{}, fmt.Errorf("invalid pid %d in pidfile", entry.PID)
	}
	return entry, nil
}
