package transport

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

// TCPTransport implements Transport over TCP.
type TCPTransport struct {
	conn    net.Conn
	reader  *bufio.Reader
	writeMu sync.Mutex
}

// NewTCPTransport creates a transport by connecting to a TCP endpoint.
func NewTCPTransport(host string, port int, timeout time.Duration) (*TCPTransport, error) {
	addr := net.JoinHostPort(host, strconv.Itoa(port))

	var conn net.Conn
	var err error

	if timeout > 0 {
		conn, err = net.DialTimeout("tcp", addr, timeout)
	} else {
		conn, err = net.Dial("tcp", addr)
	}

	if err != nil {
		return nil, fmt.Errorf("failed to connect to LSP server at %s: %w", addr, err)
	}

	return &TCPTransport{
		conn:   conn,
		reader: bufio.NewReader(conn),
	}, nil
}

// Read reads a message from the transport following LSP framing.
func (t *TCPTransport) Read(ctx context.Context) ([]byte, error) {
	// Set read deadline if context has deadline
	if deadline, ok := ctx.Deadline(); ok {
		_ = t.conn.SetReadDeadline(deadline)
		defer func() { _ = t.conn.SetReadDeadline(time.Time{}) }()
	}

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

		if after, ok := strings.CutPrefix(line, "Content-Length:"); ok {
			lengthStr := strings.TrimSpace(after)
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
func (t *TCPTransport) Write(ctx context.Context, data []byte) error {
	// Set write deadline if context has deadline
	if deadline, ok := ctx.Deadline(); ok {
		_ = t.conn.SetWriteDeadline(deadline)
		defer func() { _ = t.conn.SetWriteDeadline(time.Time{}) }()
	}

	header := fmt.Sprintf("Content-Length: %d\r\n\r\n", len(data))

	t.writeMu.Lock()
	defer t.writeMu.Unlock()

	if _, err := t.conn.Write([]byte(header)); err != nil {
		return fmt.Errorf("failed to write header: %w", err)
	}

	if _, err := t.conn.Write(data); err != nil {
		return fmt.Errorf("failed to write content: %w", err)
	}

	return nil
}

// Close closes the TCP connection.
func (t *TCPTransport) Close() error {
	return t.conn.Close()
}

// Reader returns the connection as a reader.
func (t *TCPTransport) Reader() io.Reader {
	return t.conn
}

// Writer returns the connection as a writer.
func (t *TCPTransport) Writer() io.Writer {
	return t.conn
}

// LocalAddr returns the local address.
func (t *TCPTransport) LocalAddr() net.Addr {
	return t.conn.LocalAddr()
}

// RemoteAddr returns the remote address.
func (t *TCPTransport) RemoteAddr() net.Addr {
	return t.conn.RemoteAddr()
}

// Ensure TCPTransport implements interfaces
var (
	_ Transport    = (*TCPTransport)(nil)
	_ ReaderWriter = (*TCPTransport)(nil)
)
