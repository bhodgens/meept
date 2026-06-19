package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"
)

// StdioTransport implements Transport over stdio to a subprocess.
type StdioTransport struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	reader  *bufio.Reader
	writeMu sync.Mutex // serializes header+content writes to avoid interleaved frames
}

// NewStdioTransport creates a transport by starting a subprocess.
func NewStdioTransport(command string, args ...string) (*StdioTransport, error) {
	cmd := exec.Command(command, args...)
	cmd.Stderr = os.Stderr // Forward server errors

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("failed to create stdin pipe: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		stdin.Close()
		stdout.Close()
		return nil, fmt.Errorf("failed to start LSP server: %w", err)
	}

	return &StdioTransport{
		cmd:    cmd,
		stdin:  stdin,
		stdout: stdout,
		reader: bufio.NewReader(stdout),
	}, nil
}

// Read reads a message from the transport following LSP framing.
func (t *StdioTransport) Read(ctx context.Context) ([]byte, error) {
	// Wrap reader in a channel to enable context cancellation
	type readResult struct {
		data []byte
		err  error
	}

	// Use a channel to unblock when context is cancelled
	resultCh := make(chan readResult, 1)

	go func() {
		var contentLength int
		var readErr error
		for {
			var line string
			line, readErr = t.reader.ReadString('\n')
			if readErr != nil {
				resultCh <- readResult{nil, fmt.Errorf("failed to read header: %w", readErr)}
				return
			}

			line = strings.TrimSpace(line)
			if line == "" {
				break
			}

			if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
				lengthStr := strings.TrimSpace(after)
				contentLength, readErr = strconv.Atoi(lengthStr)
				if readErr != nil {
					resultCh <- readResult{nil, fmt.Errorf("invalid content length: %w", readErr)}
					return
				}
			}
		}

		if contentLength == 0 {
			resultCh <- readResult{nil, fmt.Errorf("missing Content-Length header")}
			return
		}

		content := make([]byte, contentLength)
		_, readErr = io.ReadFull(t.reader, content)
		if readErr != nil {
			resultCh <- readResult{nil, fmt.Errorf("failed to read content: %w", readErr)}
			return
		}

		resultCh <- readResult{content, nil}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case result := <-resultCh:
		return result.data, result.err
	}
}

// Write writes a message to the transport with LSP framing.
func (t *StdioTransport) Write(ctx context.Context, data []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))

	// Hold the write mutex across both writes so concurrent Write callers can't
	// interleave header and content on the pipe and corrupt the JSON-RPC framing.
	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if _, err := t.stdin.Write([]byte(header)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	return nil
}

// Close closes the transport and terminates the subprocess.
// After killing the process, Wait() is called to reap it, avoiding zombie
// processes. The wait is bounded by a 5-second timeout so a wedged process
// doesn't block Close indefinitely.
func (t *StdioTransport) Close() error {
	if t.stdin != nil {
		t.stdin.Close()
	}
	if t.stdout != nil {
		t.stdout.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		if err := t.cmd.Process.Kill(); err != nil {
			// Process may already be dead; fall through to Wait.
		}
		// Reap the process to avoid zombies. Use a goroutine + timer so
		// Close doesn't block forever on an unresponsive child.
		done := make(chan error, 1)
		go func() { done <- t.cmd.Wait() }()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			// Best-effort; process may still be reaped by init later.
		}
	}
	return nil
}

// Reader returns the stdout reader.
func (t *StdioTransport) Reader() io.Reader {
	return t.stdout
}

// Writer returns the stdin writer.
func (t *StdioTransport) Writer() io.Writer {
	return t.stdin
}

// Process returns the underlying process.
func (t *StdioTransport) Process() *os.Process {
	return t.cmd.Process
}

// Wait waits for the subprocess to exit.
func (t *StdioTransport) Wait() error {
	return t.cmd.Wait()
}

// Ensure StdioTransport implements interfaces
var (
	_ Transport    = (*StdioTransport)(nil)
	_ ReaderWriter = (*StdioTransport)(nil)
)
