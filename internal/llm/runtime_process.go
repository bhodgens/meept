package llm

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"syscall"
)

// RuntimeProcess manages a spawned LLM runtime process.
type RuntimeProcess struct {
	config  *RuntimeConfig
	cmd     *exec.Cmd
	pid     int
	pidFile string
}

// NewRuntimeProcess creates a new process manager.
func NewRuntimeProcess(cfg *RuntimeConfig) *RuntimeProcess {
	return &RuntimeProcess{
		config:  cfg,
		pidFile: cfg.PIDFile,
	}
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

	return nil
}

// Stop gracefully terminates the runtime process.
func (p *RuntimeProcess) Stop(ctx context.Context) error {
	if p.cmd == nil || p.cmd.Process == nil {
		// Try to recover from PID file
		if pid, err := p.readPIDFile(); err == nil && pid > 0 {
			proc, err := os.FindProcess(pid)
			if err != nil {
				return nil
			}
			p.cmd = &exec.Cmd{}
			p.cmd.Process = proc
		} else {
			return nil // Not running
		}
	}

	// Send SIGTERM for graceful shutdown
	if err := p.cmd.Process.Signal(syscall.SIGTERM); err != nil {
		// Already dead
		os.Remove(p.pidFile)
		return nil
	}

	// Wait for process to exit
	done := make(chan error, 1)
	go func() {
		done <- p.cmd.Wait()
	}()

	select {
	case <-ctx.Done():
		// Force kill on context cancellation
		p.cmd.Process.Kill()
	case err := <-done:
		_ = err // Ignored
	}

	os.Remove(p.pidFile)
	return nil
}

// PID returns the process ID.
func (p *RuntimeProcess) PID() int {
	return p.pid
}

// IsRunning checks if the process is still alive.
func (p *RuntimeProcess) IsRunning() bool {
	if p.pid == 0 {
		return false
	}
	return p.isProcessRunning(p.pid)
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
