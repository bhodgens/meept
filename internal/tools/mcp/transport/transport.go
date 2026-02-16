// Package transport provides MCP transport implementations.
//
// MCP supports multiple transport mechanisms:
// - stdio: Communication via stdin/stdout with a subprocess
// - HTTP: Communication via HTTP POST with optional SSE
// - WebSocket: Bidirectional communication (requires SDK)
package transport

import (
	"context"
)

// Transport defines the interface for MCP communication transports.
type Transport interface {
	// Start initializes the transport connection.
	Start(ctx context.Context) error

	// Send sends a request and returns the response.
	// The message should be a complete JSON-RPC request.
	Send(ctx context.Context, message []byte) ([]byte, error)

	// Close terminates the transport connection.
	Close() error

	// IsRunning returns true if the transport is active.
	IsRunning() bool
}

// Config holds common transport configuration.
type Config struct {
	// Timeout for individual requests in milliseconds.
	TimeoutMS int

	// Environment variables to set (for stdio transport).
	Environment map[string]string
}

// DefaultConfig returns a Config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		TimeoutMS:   30000,
		Environment: make(map[string]string),
	}
}
