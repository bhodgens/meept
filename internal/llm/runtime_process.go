package llm

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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
	// waitDone receives the result of cmd.Wait() exactly once.
	// Created in Start(); consumed by Stop() to avoid a double-Wait race.
	waitDone chan error
}

// NewRuntimeProcess creates a new process manager.
func NewRuntimeProcess(cfg *RuntimeConfig) *RuntimeProcess {
	return &RuntimeProcess{
		config:  cfg,
		pidFile: cfg.PIDFile,
	}
}

// AlreadyRunning reports whether the runtime process is already running
// according to the PID file. Returns true when the PID file exists, parses,
// and the identified process is alive (signal-0 succeeds). Callers use this
// to decide whether to truncate the process log before calling Start: an
// already-running process should not have its log truncated because no new
// subprocess will be spawned.
func (p *RuntimeProcess) AlreadyRunning() bool {
	pid, err := p.readPIDFile()
	if err != nil || pid <= 0 {
		return false
	}
	return p.isProcessRunning(pid)
}

// Start spawns the runtime process. stdout and stderr are used for the
// subprocess's output streams; nil falls back to os.Stdout/os.Stderr.
func (p *RuntimeProcess) Start(ctx context.Context, stdout, stderr io.Writer) error {
	if stdout == nil {
		stdout = os.Stdout
	}
	if stderr == nil {
		stderr = os.Stderr
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	// Check if already running via PID file
	if pid, err := p.readPIDFile(); err == nil && pid > 0 {
		if p.isProcessRunning(pid) {
			return nil // Already running
		}
		// Stale PID file
		os.Remove(p.pidFile)
	}

	// Validate spawn command
	if len(p.config.SpawnCommand) == 0 {
		return fmt.Errorf("no spawn command configured")
	}

	name := p.config.SpawnCommand[0]
	args := p.config.SpawnCommand[1:]

	p.cmd = exec.CommandContext(ctx, name, args...)
	p.cmd.Stdout = stdout
	p.cmd.Stderr = stderr
	p.cmd.Stdin = nil // Explicitly set stdin to nil to avoid blocking
	p.cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

	if err := p.cmd.Start(); err != nil {
		return fmt.Errorf("failed to spawn runtime: %w", err)
	}

	p.pid = p.cmd.Process.Pid

	// Write PID file
	if err := p.writePIDFile(p.pid); err != nil {
		p.cmd.Process.Kill()
		return fmt.Errorf("failed to write PID file: %w", err)
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

// Stop gracefully terminates the runtime process.
func (p *RuntimeProcess) Stop(ctx context.Context) error {
	p.mu.Lock()
	if p.cmd == nil || p.cmd.Process == nil {
		// Try to recover from PID file
		if pid, err := p.readPIDFile(); err == nil && pid > 0 {
			proc, err := os.FindProcess(pid)
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
		os.Remove(p.pidFile)
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
				killProcessGroup(cmd, syscall.SIGKILL)
				os.Remove(p.pidFile)
				return nil
			case <-ticker.C:
				if !p.isProcessRunning(cmd.Process.Pid) {
					os.Remove(p.pidFile)
					return nil
				}
			}
		}
	}

	select {
	case <-ctx.Done():
		// Force-kill the process group on context cancellation
		killProcessGroup(cmd, syscall.SIGKILL)
	case <-waitDone:
	}

	os.Remove(p.pidFile)
	return nil
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
	if pid, err := p.readPIDFile(); err == nil && pid > 0 {
		if !p.isProcessRunning(pid) {
			os.Remove(p.pidFile)
		}
	}
}

func (p *RuntimeProcess) isProcessRunning(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	err = proc.Signal(syscall.Signal(0))
	return err == nil
}

// killProcessGroup sends a signal to the process group of cmd.
// Falls back to killing just the leader if getpgid fails.
func killProcessGroup(cmd *exec.Cmd, sig syscall.Signal) error {
	if cmd == nil || cmd.Process == nil {
		return fmt.Errorf("no process")
	}
	pgid, err := syscall.Getpgid(cmd.Process.Pid)
	if err != nil {
		// Fallback: kill leader only
		return cmd.Process.Signal(sig)
	}
	return syscall.Kill(-pgid, sig)
}

func (p *RuntimeProcess) writePIDFile(pid int) error {
	dir := filepath.Dir(p.pidFile)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	return os.WriteFile(p.pidFile, []byte(strconv.Itoa(pid)), 0o600)
}

func (p *RuntimeProcess) readPIDFile() (int, error) {
	data, err := os.ReadFile(p.pidFile)
	if err != nil {
		return 0, err
	}
	return strconv.Atoi(string(data))
}
