package transport

import (
	"bufio"
	"context"
	"encoding/json"
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

	// stdoutFile is the raw stdout pipe so we can Close() it
	// to unblock relayStdout on shutdown.
	stdoutFile io.ReadCloser

	// relayCh delivers stdout lines from the relay goroutine to Send().
	relayCh chan stdoutLine

	// relayDone is closed when relayStdout exits.
	relayDone chan struct{}

	// stderrDone is closed when drainStderr exits, allowing Close() to wait.
	stderrDone chan struct{}

	// closeOnce ensures stderrDone is closed only once.
	closeOnce sync.Once
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

	if t.running.Load() { //nolint:mutexio // atomic.Bool.Load is not I/O
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
		stdin.Close() //nolint:mutexio // one-time init cleanup path
		return fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	stderr, err := cmd.StderrPipe()
	if err != nil {
		stdin.Close()   //nolint:mutexio // one-time init cleanup path
		stdout.Close()  //nolint:mutexio // one-time init cleanup path
		return fmt.Errorf("failed to create stderr pipe: %w", err)
	}

	// Start the process
	if err := cmd.Start(); err != nil {
		stdin.Close() //nolint:mutexio // one-time init cleanup path
		return fmt.Errorf("failed to start process: %w", err)
	}

	t.cmd = cmd
	t.stdin = stdin
	t.stdoutFile = stdout
	t.stdout = bufio.NewReader(stdout)
	t.stderr = bufio.NewReader(stderr)
	t.relayCh = make(chan stdoutLine, 32)
	t.relayDone = make(chan struct{})
	t.stderrDone = make(chan struct{})
	t.running.Store(true)

	// Start stderr reader (log to stderr of parent process)
	go t.drainStderr()

	// Start stdout relay: reads lines from the subprocess and makes
	// them available via a channel. This ensures exactly one reader
	// goroutine owns the bufio.Reader so there are no concurrent reads.
	go t.relayStdout()

	return nil
}

// drainStderr reads stderr from the subprocess and discards it.
// This prevents the subprocess from blocking on stderr writes.
//
// The goroutine exits when either:
//   - the subprocess's stderr pipe returns an error (typically EOF when
//     the process terminates), or
//   - Close() sets running to false, which causes the next Read to be
//     interrupted (subprocess killed in Close) or the loop condition to fail.
//
// stderrDone is closed on exit so Close() can wait for a clean shutdown.
func (t *StdioTransport) drainStderr() {
	defer t.closeOnce.Do(func() { close(t.stderrDone) })
	buf := make([]byte, 4096)
	for t.running.Load() {
		_, err := t.stderr.Read(buf)
		if err != nil {
			return
		}
	}
}

// stdoutLine is delivered by relayStdout for each line read from the subprocess.
type stdoutLine struct {
	data []byte
	err  error
}

// rpcEnvelope is a minimal JSON-RPC message used to extract the id and
// distinguish responses from notifications.
type rpcEnvelope struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
}

// extractRPCID parses the JSON-RPC id from a message. Returns nil for
// notifications (which have no id field or have "method" but no "id").
func extractRPCID(data []byte) json.RawMessage {
	var env rpcEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return nil
	}
	return env.ID
}

// isNotification reports whether a JSON-RPC message is a notification
// (has "method" but no "id").
func isNotification(data []byte) bool {
	var env rpcEnvelope
	if err := json.Unmarshal(data, &env); err != nil {
		return false
	}
	return env.Method != "" && len(env.ID) == 0
}

// relayStdout reads lines from stdout in a single goroutine and sends them
// on relayCh. This avoids spawning a new goroutine per Send() call that can
// leak when Send() times out.
func (t *StdioTransport) relayStdout() {
	defer close(t.relayDone)

	for {
		line, err := t.stdout.ReadBytes('\n')
		select {
		case t.relayCh <- stdoutLine{data: line, err: err}:
			if err != nil {
				return
			}
		default:
			// No one listening (Send timed out or transport closed).
			// Discard the line and keep reading.
			if err != nil {
				return
			}
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

	// Extract the request id so we can match the response and skip
	// interleaved notifications or unrelated responses.
	wantID := extractRPCID(message)

	// Write message with newline
	if _, err := t.stdin.Write(append(message, '\n')); err != nil {
		return nil, fmt.Errorf("failed to write message: %w", err)
	}

	// Read response with timeout
	timeout := time.Duration(t.config.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	readCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	for {
		select {
		case <-readCtx.Done():
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, fmt.Errorf("request timed out after %v", timeout)
		case line := <-t.relayCh:
			if line.err != nil {
				return nil, fmt.Errorf("failed to read response: %w", line.err)
			}
			// If we can't extract an id from the request (e.g., the message
			// doesn't have one), return the first non-notification line.
			if len(wantID) == 0 {
				if isNotification(line.data) {
					continue // skip notifications, wait for a response
				}
				return line.data, nil
			}
			// Skip notifications (no id field, has method).
			if isNotification(line.data) {
				continue
			}
			// Check if this response matches our request id.
			respID := extractRPCID(line.data)
			if len(respID) == 0 || string(respID) == string(wantID) {
				return line.data, nil
			}
			// Different response id — could be from a concurrent request
			// that timed out. Discard and keep waiting for ours.
		}
	}
}

// SendNotification sends a JSON-RPC notification without waiting for a response.
// It writes the message to stdin but does not read from stdout.
func (t *StdioTransport) SendNotification(_ context.Context, message []byte) error {
	if !t.running.Load() {
		return fmt.Errorf("transport not running")
	}

	// Serialize with requests to maintain write ordering
	t.reqMu.Lock()
	defer t.reqMu.Unlock()

	if _, err := t.stdin.Write(append(message, '\n')); err != nil {
		return fmt.Errorf("failed to write notification: %w", err)
	}

	return nil
}

// Close terminates the subprocess.
func (t *StdioTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running.Load() { //nolint:mutexio // atomic.Bool.Load is not I/O
		return nil
	}

	t.running.Store(false)

	// Close stdin to signal EOF to the subprocess
	if t.stdin != nil {
		t.stdin.Close() //nolint:mutexio // one-time teardown guarded by running flag
	}

	if t.cmd == nil || t.cmd.Process == nil {
		// Close stdout file to unblock relayStdout
		if t.stdoutFile != nil {
			t.stdoutFile.Close() //nolint:mutexio // one-time teardown guarded by running flag
		}
		return nil
	}

	// Give the process a chance to exit gracefully
	done := make(chan error, 1)
	go func() {
		done <- t.cmd.Wait()
	}()

	select {
	case <-done:
		// Process exited — pipes are now closed, relayStdout will unblock
	case <-time.After(5 * time.Second):
		// Force kill — this closes the process and its pipes
		_ = t.cmd.Process.Kill()
		<-done
	}

	// Wait for the relay goroutine to finish before closing stdoutFile,
	// otherwise we race with relayStdout's read.
	if t.relayDone != nil {
		select {
		case <-t.relayDone:
		case <-time.After(2 * time.Second):
			// Relay didn't exit in time; don't block forever
		}
	}

	// Wait for stderr drain goroutine to finish as well.
	if t.stderrDone != nil {
		select {
		case <-t.stderrDone:
		case <-time.After(2 * time.Second):
			// stderr drain didn't exit in time; don't block forever
		}
	}

	// Close stdout file after relayStdout has exited.
	if t.stdoutFile != nil {
		t.stdoutFile.Close() //nolint:mutexio // one-time teardown guarded by running flag
	}

	return nil
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
