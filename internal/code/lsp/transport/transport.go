// Package transport provides LSP transport layer implementations.
package transport

import (
	"context"
	"io"
)

// Transport is the interface for LSP communication.
type Transport interface {
	// Read reads a message from the transport.
	Read(ctx context.Context) ([]byte, error)
	// Write writes a message to the transport.
	Write(ctx context.Context, data []byte) error
	// Close closes the transport.
	Close() error
}

// ReaderWriter provides access to underlying streams.
type ReaderWriter interface {
	Reader() io.Reader
	Writer() io.Writer
}
