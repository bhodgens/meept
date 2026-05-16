package mcp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
)

// BufferedReader provides a buffered reader that wraps an io.Reader.
// This persists the bufio.Reader across multiple ReadMessage calls
// to avoid losing data that was pre-buffered by a fresh reader.
type BufferedReader struct {
	r *bufio.Reader
}

// NewBufferedReader creates a new BufferedReader around the given io.Reader.
func NewBufferedReader(r io.Reader) *BufferedReader {
	return &BufferedReader{r: bufio.NewReaderSize(r, 4096)}
}

// ReadLine reads one line from the buffered reader and returns its content
// (without the trailing newline). Returns an io.Reader error if read fails.
func (br *BufferedReader) ReadLine() ([]byte, error) {
	line, err := br.r.ReadBytes('\n')
	if err != nil {
		return nil, err
	}
	return line, nil
}

// JSONRPCRequest is a JSON-RPC 2.0 request (MCP uses JSON-RPC over stdio).
type JSONRPCRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// JSONRPCResponse is a JSON-RPC 2.0 response.
type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
}

// JSONRPCError represents a JSON-RPC error.
type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Data    any    `json:"data,omitempty"`
}

// ReadMessage reads a single JSON-RPC message from the reader (one line).
// Deprecated: use ReadMessageFromBufferedReader / Server with BufferedReader
// instead. ReadMessage creates a new bufio.Reader on every call, which can
// cause data loss when multiple messages are piped at once.
func ReadMessage(r io.Reader) (*JSONRPCRequest, error) {
	reader := bufio.NewReader(r)
	line, err := reader.ReadBytes('\n')

	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, fmt.Errorf("empty message")
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &req, nil
}

// ReadMessageBuffered reads a single JSON-RPC message from a BufferedReader.
// This is the preferred method for MCP server use since it preserves the
// bufio.Reader across multiple calls, preventing data loss.
func ReadMessageBuffered(br *BufferedReader) (*JSONRPCRequest, error) {
	line, err := br.ReadLine()
	if err != nil {
		return nil, fmt.Errorf("read message: %w", err)
	}
	line = bytes.TrimSpace(line)
	if len(line) == 0 {
		return nil, fmt.Errorf("empty message")
	}

	var req JSONRPCRequest
	if err := json.Unmarshal(line, &req); err != nil {
		return nil, fmt.Errorf("unmarshal message: %w", err)
	}
	return &req, nil
}

// WriteMessage writes a JSON-RPC response as a single line.
func WriteMessage(w io.Writer, resp *JSONRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("marshal response: %w", err)
	}
	if _, err := fmt.Fprintf(w, "%s\n", data); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	return nil
}
