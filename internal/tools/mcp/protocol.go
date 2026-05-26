// Package mcp provides MCP (Model Context Protocol) client implementation.
//
// MCP is a protocol for connecting AI models to external tools and services.
// This package implements the JSON-RPC 2.0 based MCP client protocol.
package mcp

import (
	"encoding/json"
	"fmt"
)

// JSON-RPC 2.0 error codes
const (
	ErrCodeParse          = -32700 // Invalid JSON was received
	ErrCodeInvalidRequest = -32600 // The JSON sent is not a valid Request object
	ErrCodeMethodNotFound = -32601 // The method does not exist / is not available
	ErrCodeInvalidParams  = -32602 // Invalid method parameter(s)
	ErrCodeInternal       = -32603 // Internal JSON-RPC error
)

// MCP specific error codes
const (
	ErrCodeNotImplemented   = -32001 // Method not implemented
	ErrCodeInvalidArguments = -32002 // Invalid tool arguments
)

// Request is a JSON-RPC 2.0 request.
type Request struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"` // nil for notifications
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

// NewRequest creates a new JSON-RPC request with an ID.
func NewRequest(id any, method string, params any) *Request {
	return &Request{
		JSONRPC: "2.0",
		ID:      id,
		Method:  method,
		Params:  params,
	}
}

// NewNotification creates a JSON-RPC notification (no ID, no response expected).
func NewNotification(method string, params any) *Request {
	return &Request{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
	}
}

// Response is a JSON-RPC 2.0 response.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC 2.0 error object.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// Error implements the error interface.
func (e *RPCError) Error() string {
	if e.Data != nil {
		return fmt.Sprintf("RPC error %d: %s (data: %v)", e.Code, e.Message, e.Data)
	}
	return fmt.Sprintf("RPC error %d: %s", e.Code, e.Message)
}

// MCP Protocol Version
const ProtocolVersion = "2024-11-05"

// InitializeParams are the parameters for the initialize request.
type InitializeParams struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ClientCapabilities `json:"capabilities"`
	ClientInfo      ImplementationInfo `json:"clientInfo"`
}

// ClientCapabilities describes the capabilities supported by the client.
type ClientCapabilities struct {
	Roots    *RootsCapability `json:"roots,omitempty"`
	Sampling map[string]any   `json:"sampling,omitempty"`
}

// RootsCapability describes roots capability.
type RootsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ImplementationInfo describes the client or server implementation.
type ImplementationInfo struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// InitializeResult is the result of the initialize request.
type InitializeResult struct {
	ProtocolVersion string             `json:"protocolVersion"`
	Capabilities    ServerCapabilities `json:"capabilities"`
	ServerInfo      ImplementationInfo `json:"serverInfo"`
	Instructions    string             `json:"instructions,omitempty"`
}

// ServerCapabilities describes what the server supports.
type ServerCapabilities struct {
	Tools     *ToolsCapability     `json:"tools,omitempty"`
	Resources *ResourcesCapability `json:"resources,omitempty"`
	Prompts   *PromptsCapability   `json:"prompts,omitempty"`
	Logging   map[string]any       `json:"logging,omitempty"`
}

// ToolsCapability describes tool support.
type ToolsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ResourcesCapability describes resource support.
type ResourcesCapability struct {
	Subscribe   bool `json:"subscribe,omitempty"`
	ListChanged bool `json:"listChanged,omitempty"`
}

// PromptsCapability describes prompt support.
type PromptsCapability struct {
	ListChanged bool `json:"listChanged,omitempty"`
}

// ToolInfo describes a tool provided by the MCP server.
type ToolInfo struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"inputSchema,omitempty"`
}

// ListToolsResult is the result of tools/list.
type ListToolsResult struct {
	Tools      []ToolInfo `json:"tools"`
	NextCursor string     `json:"nextCursor,omitempty"`
}

// CallToolParams are the parameters for tools/call.
type CallToolParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments,omitempty"`
}

// CallToolResult is the result of tools/call.
type CallToolResult struct {
	Content []ContentBlock `json:"content"`
	IsError bool           `json:"isError,omitempty"`
}

// ContentBlock is a single block of content in a tool result.
type ContentBlock struct {
	Type     string         `json:"type"` // "text", "image", "resource"
	Text     string         `json:"text,omitempty"`
	Data     string         `json:"data,omitempty"` // base64 encoded
	MimeType string         `json:"mimeType,omitempty"`
	Resource *ResourceBlock `json:"resource,omitempty"`
}

// ResourceBlock is a resource reference.
type ResourceBlock struct {
	URI      string `json:"uri"`
	MimeType string `json:"mimeType,omitempty"`
	Text     string `json:"text,omitempty"`
}

// Serialize serializes a value to JSON bytes.
func Serialize(v any) ([]byte, error) {
	return json.Marshal(v)
}

// Deserialize deserializes JSON bytes into a value.
func Deserialize(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// ParseResponse parses a JSON-RPC response from bytes.
func ParseResponse(data []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}
	return &resp, nil
}

// ExtractResult extracts and unmarshals the result from a response.
func ExtractResult[T any](resp *Response) (T, error) {
	var result T
	if resp.Error != nil {
		return result, resp.Error
	}
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return result, fmt.Errorf("failed to unmarshal result: %w", err)
	}
	return result, nil
}
