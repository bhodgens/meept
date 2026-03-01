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
)

// StdioTransport implements Transport over stdio to a subprocess.
type StdioTransport struct {
	cmd    *exec.Cmd
	stdin  io.WriteCloser
	stdout io.ReadCloser
	reader *bufio.Reader
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
	// Read headers until blank line
	var contentLength int
	for {
		line, err := t.reader.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("failed to read header: %w", err)
		}

		line = strings.TrimSpace(line)
		if line == "" {
			break
		}

		if strings.HasPrefix(line, "Content-Length:") {
			lengthStr := strings.TrimSpace(strings.TrimPrefix(line, "Content-Length:"))
			contentLength, err = strconv.Atoi(lengthStr)
			if err != nil {
				return nil, fmt.Errorf("invalid content length: %w", err)
			}
		}
	}

	if contentLength == 0 {
		return nil, fmt.Errorf("missing Content-Length header")
	}

	// Read content
	content := make([]byte, contentLength)
	_, err := io.ReadFull(t.reader, content)
	if err != nil {
		return nil, fmt.Errorf("failed to read content: %w", err)
	}

	return content, nil
}

// Write writes a message to the transport with LSP framing.
func (t *StdioTransport) Write(ctx context.Context, data []byte) error {
	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))

	if _, err := t.stdin.Write([]byte(header)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	if _, err := t.stdin.Write(data); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	return nil
}

// Close closes the transport and terminates the subprocess.
func (t *StdioTransport) Close() error {
	if t.stdin != nil {
		t.stdin.Close()
	}
	if t.stdout != nil {
		t.stdout.Close()
	}
	if t.cmd != nil && t.cmd.Process != nil {
		return t.cmd.Process.Kill()
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
