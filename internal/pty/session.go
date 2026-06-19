// Package pty provides pseudo-terminal session management for interactive tools.
//
// It wraps github.com/creack/pty to create and manage PTY sessions that support
// real-time I/O streaming. When a PTY device is not available (e.g. Windows),
// NewSession falls back to a non-PTY subprocess mode where Read/Write operate
// directly on cmd.Stdout/cmd.Stderr/Stdin pipes.
package pty

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
)

// Session represents an interactive PTY session.
type Session interface {
	// Write sends input to the session.
	Write(data []byte) (int, error)

	// Read reads output from the session (context-aware blocking read).
	Read(ctx context.Context, buf []byte) (int, error)

	// Output returns a channel for streaming output from the session.
	Output() <-chan []byte

	// Errors returns a channel for error notifications.
	Errors() <-chan error

	// Size returns the terminal dimensions.
	Size() (rows, cols int)

	// Resize changes the terminal dimensions.
	Resize(rows, cols int) error

	// Close terminates the session.
	Close() error

	// IsRunning returns true if the session is active.
	IsRunning() bool

	// ExitCode returns the exit code (only valid after IsRunning returns false).
	ExitCode() int
}

// SessionConfig holds PTY session configuration.
type SessionConfig struct {
	// Cmd is the command to execute (e.g., "ipython", "gdb").
	Cmd string
	// Args are command-line arguments.
	Args []string
	// Dir is the working directory.
	Dir string
	// Env is the environment variables.
	Env []string
	// Rows is the initial terminal rows (default: 24).
	Rows int
	// Cols is the initial terminal columns (default: 80).
	Cols int
	// Timeout is the command timeout (0 = no timeout).
	Timeout time.Duration
}

// ptySession implements Session using github.com/creack/pty.
type ptySession struct {
	mu         sync.RWMutex
	wg         sync.WaitGroup // waits for readLoop before waitLoop closes channels
	ptyCmd     *exec.Cmd      // PTY mode: the actual command started by pty.StartWithSize
	plainCmd   *exec.Cmd      // Non-PTY mode: process to Wait on.
	ptmx       *os.File       // PTY master device (PTY mode)
	stdoutPipe io.Reader      // Non-PTY mode: cmd.Stdout
	stdinPipe  io.Writer      // Non-PTY mode: cmd.Stdin
	outputChan chan []byte
	errorChan  chan error
	// pending holds the unread remainder of a large output chunk so
	// that the next Read() call can serve it without data loss.
	pending   []byte
	done      chan struct{}
	closed    bool
	exitCode  int
	rows      int
	cols      int
	fallback  bool
}

var (
	// ErrSessionClosed is returned when operating on a closed session.
	ErrSessionClosed = errors.New("session closed")
	// ErrSessionNotFound is returned when accessing a session by ID that doesn't exist.
	ErrSessionNotFound = errors.New("session not found")
	// ErrSessionLimit is returned when max sessions is reached.
	ErrSessionLimit = errors.New("session limit reached")
	// ErrSessionExists is returned when a session ID already exists.
	ErrSessionExists = errors.New("session ID already exists")
)

// NewSession creates a new PTY session.
func NewSession(cfg SessionConfig) (Session, error) {
	if cfg.Cmd == "" {
		return nil, errors.New("empty command")
	}

	rows := cfg.Rows
	if rows <= 0 {
		rows = 24
	}
	cols := cfg.Cols
	if cols <= 0 {
		cols = 80
	}

	// Try PTY mode first
	ptmx, plainCmd, err := startPTY(cfg, rows, cols)
	if err == nil {
		sess := &ptySession{
			ptyCmd:     plainCmd,
			ptmx:       ptmx,
			outputChan: make(chan []byte, 100),
			errorChan:  make(chan error, 10),
			done:       make(chan struct{}),
			rows:       rows,
			cols:       cols,
			fallback:   false,
		}

		sess.wg.Add(1)
		go sess.readLoop()
		go sess.waitLoop()

		return sess, nil
	}

	// Fall back to non-PTY mode
	sess := &ptySession{
		plainCmd:   exec.Command(cfg.Cmd, cfg.Args...),
		outputChan: make(chan []byte, 100),
		errorChan:  make(chan error, 10),
		done:       make(chan struct{}),
		rows:       rows,
		cols:       cols,
		fallback:   true,
	}
	sess.plainCmd.Dir = cfg.Dir
	sess.plainCmd.Env = cfg.Env

	if err := sess.plainCmd.Start(); err != nil {
		return nil, fmt.Errorf("failed to start command: %w", err)
	}

	stdout, err := sess.plainCmd.StdoutPipe()
	if err != nil {
		_ = sess.plainCmd.Process.Kill()
		_ = sess.plainCmd.Wait()
		return nil, fmt.Errorf("failed to get stdout: %w", err)
	}
	stdin, err := sess.plainCmd.StdinPipe()
	if err != nil {
		_ = sess.plainCmd.Process.Kill()
		_ = sess.plainCmd.Wait()
		return nil, fmt.Errorf("failed to get stdin: %w", err)
	}

	sess.stdoutPipe = stdout
	sess.stdinPipe = stdin

	sess.wg.Add(1)
	go sess.readLoop()
	go sess.waitLoop()

	return sess, nil
}

func startPTY(cfg SessionConfig, rows, cols int) (*os.File, *exec.Cmd, error) {
	cleanCmd := exec.Command(cfg.Cmd, cfg.Args...)
	cleanCmd.Dir = cfg.Dir
	cleanCmd.Env = cfg.Env
	cleanCmd.Stdin = nil
	cleanCmd.Stdout = nil
	cleanCmd.Stderr = nil

	ptmx, err := pty.StartWithSize(cleanCmd, &pty.Winsize{
		Rows: uint16(rows),
		Cols: uint16(cols),
	})
	if err != nil {
		return nil, nil, err
	}

	return ptmx, cleanCmd, nil
}

// Write sends input to the PTY.
func (s *ptySession) Write(data []byte) (int, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.closed {
		return 0, ErrSessionClosed
	}

	if !s.fallback {
		if s.ptmx == nil {
			return 0, fmt.Errorf("PTY master is nil")
		}
		return s.ptmx.Write(data)
	}

	if s.stdinPipe == nil {
		return 0, fmt.Errorf("stdin pipe is nil")
	}
	return s.stdinPipe.Write(data)
}

// Read reads output from the PTY (context-aware).
func (s *ptySession) Read(ctx context.Context, buf []byte) (int, error) {
	// Serve any remaining unread data from a previous large chunk.
	if len(s.pending) > 0 {
		n := copy(buf, s.pending)
		s.pending = s.pending[n:]
		return n, nil
	}

	select {
	case <-ctx.Done():
		return 0, ctx.Err()
	case <-s.done:
		return 0, io.EOF
	case err := <-s.errorChan:
		return 0, err
	case chunk := <-s.outputChan:
		n := copy(buf, chunk)
		if n < len(chunk) {
			// Chunk is larger than the caller's buffer; save the
			// remainder so the next Read() call can serve it.
			s.pending = append(s.pending[:0], chunk[n:]...)
		}
		return n, nil
	}
}

// Output returns the output streaming channel.
func (s *ptySession) Output() <-chan []byte {
	return s.outputChan
}

// Errors returns the error channel.
func (s *ptySession) Errors() <-chan error {
	return s.errorChan
}

// Size returns terminal dimensions.
func (s *ptySession) Size() (int, int) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.rows, s.cols
}

// Resize changes terminal dimensions.
func (s *ptySession) Resize(rows, cols int) error {
	if rows <= 0 || cols <= 0 {
		return errors.New("rows and cols must be positive")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return ErrSessionClosed
	}

	s.rows = rows
	s.cols = cols

	if !s.fallback && s.ptmx != nil {
		return pty.Setsize(s.ptmx, &pty.Winsize{
			Rows: uint16(rows),
			Cols: uint16(cols),
		})
	}

	return nil
}

// Close terminates the session.
func (s *ptySession) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return nil
	}

	s.closed = true

	// Close PTY master
	if s.ptmx != nil {
		s.ptmx.Close() //nolint:mutexio // one-time teardown guarded by closed flag
	}

	// Close stdin pipe in fallback mode
	if s.stdinPipe != nil {
		if closer, ok := s.stdinPipe.(io.Closer); ok {
			closer.Close() //nolint:mutexio // one-time teardown guarded by closed flag
		}
	}

	// Kill command if still running. waitLoop is already calling
	// cmd.Wait() in the background, so Kill on its own is sufficient to
	// trigger reap-on-wait. We do NOT call cmd.Wait() here because doing
	// so concurrently with waitLoop would race (only one caller can
	// successfully Wait on a process). waitLoop's goroutine is the
	// single owner of the reaping call.
	if s.ptyCmd != nil && s.ptyCmd.Process != nil {
		if err := s.ptyCmd.Process.Kill(); err != nil {
			log.Printf("pty: kill ptyCmd: %v", err)
		}
	}
	if s.plainCmd != nil && s.plainCmd.Process != nil {
		if err := s.plainCmd.Process.Kill(); err != nil {
			log.Printf("pty: kill plainCmd: %v", err)
		}
	}

	close(s.done)
	return nil
}

// IsRunning returns true if session is active.
func (s *ptySession) IsRunning() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return !s.closed
}

// ExitCode returns the command exit code.
func (s *ptySession) ExitCode() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.exitCode
}

// readLoop continuously reads from the PTY (or stdout pipe in fallback mode)
// and pushes to the output channel.
func (s *ptySession) readLoop() {
	defer s.wg.Done()
	var src io.Reader
	if !s.fallback && s.ptmx != nil {
		src = s.ptmx
	} else if s.stdoutPipe != nil {
		src = s.stdoutPipe
	} else {
		s.errorChan <- fmt.Errorf("read source is nil")
		close(s.outputChan)
		return
	}

	buf := make([]byte, 4096)
	for {
		select {
		case <-s.done:
			return
		default:
		}

		n, err := src.Read(buf)
		if n > 0 {
			output := make([]byte, n)
			copy(output, buf[:n])

			select {
			case s.outputChan <- output:
			case <-s.done:
				return
			}
		}

		if err != nil {
			if err != io.EOF {
				select {
				case s.errorChan <- err:
				case <-s.done:
				}
			}
			// Drain remaining output before closing
			for {
				n, e := src.Read(buf)
				if n > 0 {
					output := make([]byte, n)
					copy(output, buf[:n])
					select {
					case s.outputChan <- output:
					case <-s.done:
						return
					}
				}
				if e != nil || n == 0 {
					break
				}
			}
			return
		}
	}
}

// waitLoop waits for command exit and captures exit code.
func (s *ptySession) waitLoop() {
	var err error
	if s.ptyCmd != nil {
		err = s.ptyCmd.Wait()
	} else if s.plainCmd != nil {
		err = s.plainCmd.Wait()
	}

	s.mu.Lock()
	if exitErr, ok := err.(*exec.ExitError); ok {
		s.exitCode = exitErr.ExitCode()
	} else if err != nil && err != context.DeadlineExceeded && err != io.EOF {
		select {
		case s.errorChan <- err:
		default:
		}
	}
	s.mu.Unlock()

	// Wait for readLoop to finish before closing channels to avoid races
	// between readLoop sending and waitLoop/closing.
	s.wg.Wait()

	// Drain any remaining output to unblock readers
	for {
		select {
		case <-s.outputChan:
		default:
			close(s.outputChan)
			goto drained
		}
	}
drained:

	close(s.errorChan)
}

// IsTerminalAvailable checks if PTY is available on this platform.
func IsTerminalAvailable() bool {
	f, err := os.OpenFile("/dev/ptmx", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// SessionInfo holds session metadata for API responses.
type SessionInfo struct {
	ID        string    `json:"id"`
	Cmd       string    `json:"cmd"`
	CreatedAt time.Time `json:"created_at"`
	Rows      int       `json:"rows,omitempty"`
	Cols      int       `json:"cols,omitempty"`
	IsRunning bool      `json:"is_running"`
}

// KillProcessTree sends SIGKILL to the process group of a command.
func KillProcessTree(cmd *exec.Cmd) error {
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	pid := cmd.Process.Pid
	if pid > 0 {
		syscall.Kill(-pid, syscall.SIGKILL)
	}
	return cmd.Process.Kill()
}
