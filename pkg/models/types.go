// Package models provides shared data types for the Meept daemon.
package models

import (
	"encoding/json"
	"fmt"
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
	ID        string          `json:"id"`
	Type      MessageType     `json:"type"`
	Topic     string          `json:"topic,omitempty"`
	Source    string          `json:"source"`
	Timestamp time.Time       `json:"timestamp"`
	Payload   json.RawMessage `json:"payload"`
	ReplyTo   string          `json:"reply_to,omitempty"`
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

// generateID creates a simple unique ID.
func generateID() string {
	return fmt.Sprintf("%d-%s", time.Now().UnixNano(), randomHex(8))
}

func randomHex(n int) string {
	const hex = "0123456789abcdef"
	b := make([]byte, n)
	now := time.Now().UnixNano()
	for i := range b {
		b[i] = hex[(now+int64(i))%16]
	}
	return string(b)
}
