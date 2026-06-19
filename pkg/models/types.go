// Package models provides shared data types for the Meept daemon.
package models

import (
	crypto_rand "crypto/rand"
	"encoding/hex"
	"encoding/json"
	"time"
)

// MessageType represents the type of bus message.
type MessageType string

const (
	MessageTypeRequest      MessageType = "request"
	MessageTypeResponse     MessageType = "response"
	MessageTypeEvent        MessageType = "event"
	MessageTypeStatusUpdate MessageType = "status_update"
	MessageTypeError        MessageType = "error"
)

// BusMessage represents a message on the internal message bus.
type BusMessage struct {
	ID           string          `json:"id"`
	Type         MessageType     `json:"type"`
	Topic        string          `json:"topic,omitempty"`
	Source       string          `json:"source"`
	SourceClient string          `json:"source_client,omitempty"`
	Timestamp    time.Time       `json:"timestamp"`
	Payload      json.RawMessage `json:"payload"`
	ReplyTo      string          `json:"reply_to,omitempty"`
}

// NewBusMessage creates a new BusMessage with a generated ID.
func NewBusMessage(msgType MessageType, source string, payload any) (*BusMessage, error) {
	data, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	return &BusMessage{
		ID:        generateID(),
		Type:      msgType,
		Source:    source,
		Timestamp: time.Now().UTC(),
		Payload:   data,
	}, nil
}

// JSONRPCRequest represents a JSON-RPC 2.0 request.
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse represents a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC 2.0 error object.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Standard JSON-RPC error codes.
const (
	ErrCodeParse          = -32700
	ErrCodeInvalidRequest = -32600
	ErrCodeMethodNotFound = -32601
	ErrCodeInvalidParams  = -32602
	ErrCodeInternal       = -32603
)

// DaemonStatus represents the current state of the daemon.
type DaemonStatus string

const (
	StatusStarting DaemonStatus = "starting"
	StatusRunning  DaemonStatus = "running"
	StatusStopping DaemonStatus = "stopping"
	StatusStopped  DaemonStatus = "stopped"
)

// DaemonInfo contains information about the running daemon.
type DaemonInfo struct {
	PID       int          `json:"pid"`
	Status    DaemonStatus `json:"status"`
	StartTime time.Time    `json:"start_time"`
	Version   string       `json:"version"`
	Socket    string       `json:"socket"`
}

// ComponentInfo describes a registered component.
type ComponentInfo struct {
	Name    string `json:"name"`
	Type    string `json:"type"`
	Running bool   `json:"running"`
}

// generateID creates a unique ID for a bus message.
// Uses 16 bytes (128 bits) of crypto randomness rendered as 32 hex chars.
// The previous implementation mixed time.Now().UnixNano() with 8 hex chars
// of randomness, which produced collisions on fast hosts where two messages
// could land in the same nanosecond (the predictable-IDs anti-pattern called
// out in CLAUDE.md). The new form drops the timestamp and doubles the
// entropy. crypto/rand failures are treated the same way as pkg/id.Generate:
// the all-zero ID is returned and the caller should treat it as a fatal
// signal of catastrophic entropy starvation.
func generateID() string {
	b := make([]byte, 16)
	if _, err := crypto_rand.Read(b); err != nil {
		return hex.EncodeToString(b) // all-zero ID
	}
	return hex.EncodeToString(b)
}
