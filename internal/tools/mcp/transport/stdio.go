package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

// StdioTransport implements MCP transport over subprocess stdin/stdout.
//
// The subprocess is started when Start() is called and communicates via
// newline-delimited JSON-RPC messages.
type StdioTransport struct {
	command string
	args    []string
	config  Config

	mu      sync.Mutex
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  *bufio.Reader
	stderr  *bufio.Reader
	running atomic.Bool

	// Request serialization
	reqMu sync.Mutex
}

// NewStdioTransport creates a new stdio transport.
func NewStdioTransport(command string, args []string, config Config) *StdioTransport {
	return &StdioTransport{
		command: command,
		args:    args,
		config:  config,
	}
}

// Start launches the subprocess and sets up communication pipes.
func (t *StdioTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running.Load() {
		return fmt.Errorf("transport already running")
	}

	// Build command
	//nolint:gosec // validated input
	cmd := exec.CommandContext(ctx, t.command, t.args...)

	// Set up environment
	cmd.Env = os.Environ()
	for k, v := range t.config.Environment {
		cmd.Env = append(cmd.Env, fmt.Sprintf("%s=%s", k, v))
	}

	// Set up pipes
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()
		stdout.Close()
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return fmt.Errorf("failed to start process: %w", err)
	}

	t.cmd = cmd
	t.stdin = stdin
	t.stdout = bufio.NewReader(stdout)
	t.stderr = bufio.NewReader(stderr)
	t.running.Store(true)

	// Start stderr reader (log to stderr of parent process)
	go t.drainStderr()

	return nil
}

// drainStderr reads stderr from the subprocess and discards it.
// This prevents the subprocess from blocking on stderr writes.
func (t *StdioTransport) drainStderr() {
	buf := make([]byte, 4096)
	for t.running.Load() {
		_, err := t.stderr.Read(buf)
		if err != nil {
			return
		}
	}
}

// Send sends a JSON-RPC message and reads the response.
func (t *StdioTransport) Send(ctx context.Context, message []byte) ([]byte, error) {
	if !t.running.Load() {
		return nil, fmt.Errorf("transport not running")
	}

	// Serialize requests to ensure proper ordering
	t.reqMu.Lock()
	defer t.reqMu.Unlock()

	// Write message with newline
	if _, err := t.stdin.Write(append(message, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write message: %w", err)
	}

	// Read response with timeout
	timeout := time.Duration(t.config.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	type readResult struct {
		data []byte
		err  error
	}
	resultCh := make(chan readResult, 1)

	// Create a context with timeout for the read operation
	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	go func() {
		line, err := t.stdout.ReadBytes('\n')
		if err != nil {
			resultCh <- readResult{nil, fmt.Errorf("failed to read response: %w", err)}
			return
		}
		resultCh <- readResult{line, nil}
	}()

	select {
	case <-readCtx.Done():
		// Context cancelled or timed out - we cannot interrupt the blocked read,
		// but marking the transport as not running will cause the goroutine to
		// eventually exit when the process is closed or produces output.
		// The goroutine will be cleaned up when Close() is called.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("request timed out after %v", timeout)
	case result := <-resultCh:
		return result.data, result.err
	}
}

// Close terminates the subprocess.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running.Load() {
		return nil
	}

	t.running.Store(false)

	// Close stdin to signal EOF
	if t.stdin != nil {
		t.stdin.Close()
	}

	if t.cmd == nil || t.cmd.Process == nil {
		return nil
	}

	// Give the process a chance to exit gracefully
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case <-done:
		return nil
	case <-time.After(5 * time.Second):
		// Force kill
		_ = t.cmd.Process.Kill()
		<-done
		return nil
	}
}

// IsRunning returns true if the subprocess is running.
func (t *StdioTransport) IsRunning() bool {
	return t.running.Load()
}

// ProcessID returns the PID of the subprocess, or 0 if not running.
func (t *StdioTransport) ProcessID() int {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.cmd == nil || t.cmd.Process == nil {
		return 0
	}
	return t.cmd.Process.Pid
}

// Ensure StdioTransport implements Transport
var _ Transport = (*StdioTransport)(nil)
