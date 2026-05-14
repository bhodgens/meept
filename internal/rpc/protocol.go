// Package rpc provides the JSON-RPC 2.0 server implementation.
package rpc

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/caimlas/meept/pkg/models"
)

// FrameReader reads length-prefixed JSON-RPC frames.
type FrameReader struct {
	reader *bufio.Reader
}

// NewFrameReader creates a new frame reader.
func NewFrameReader(r io.Reader) *FrameReader {
	return &FrameReader{reader: bufio.NewReader(r)}
}

// ReadFrame reads a single length-prefixed frame.
// Format: <length>\n<payload>
func (f *FrameReader) ReadFrame() ([]byte, error) {
	// Read length line
	lengthLine, err := f.reader.ReadString('\n')
	if err != nil {
		return nil, fmt.Errorf("failed to read length: %w", err)
	}

	length, err := strconv.Atoi(strings.TrimSpace(lengthLine))
	if err != nil {
		return nil, fmt.Errorf("invalid length: %w", err)
	}

	if length <= 0 || length > 10*1024*1024 { // 10MB max
		return nil, fmt.Errorf("invalid frame length: %d", length)
	}

	// Read payload
	payload := make([]byte, length)
	_, err = io.ReadFull(f.reader, payload)
	if err != nil {
		return nil, fmt.Errorf("failed to read payload: %w", err)
	}

	return payload, nil
}

// ReadRequest reads and parses a JSON-RPC request.
func (f *FrameReader) ReadRequest() (*models.JSONRPCRequest, error) {
	payload, err := f.ReadFrame()
	if err != nil {
		return nil, err
	}

	var req models.JSONRPCRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return nil, fmt.Errorf("invalid JSON-RPC request: %w", err)
	}

	return &req, nil
}

// FrameWriter writes length-prefixed JSON-RPC frames.
type FrameWriter struct {
	writer io.Writer
}

// NewFrameWriter creates a new frame writer.
func NewFrameWriter(w io.Writer) *FrameWriter {
	return &FrameWriter{writer: w}
}

// WriteFrame writes a length-prefixed frame.
func (f *FrameWriter) WriteFrame(payload []byte) error {
	// Write length\n
	_, err := fmt.Fprintf(f.writer, "%d\n", len(payload))
	if err != nil {
		return fmt.Errorf("failed to write length: %w", err)
	}

	// Write payload
	_, err = f.writer.Write(payload)
	if err != nil {
		return fmt.Errorf("failed to write payload: %w", err)
	}

	return nil
}

// WriteResponse marshals and writes a JSON-RPC response.
func (f *FrameWriter) WriteResponse(resp *models.JSONRPCResponse) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return fmt.Errorf("failed to marshal response: %w", err)
	}
	return f.WriteFrame(data)
}

// MakeResponse creates a success response.
func MakeResponse(id, result any) *models.JSONRPCResponse {
	data, _ := json.Marshal(result)
	return &models.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Result:  data,
	}
}

// MakeErrorResponse creates an error response.
func MakeErrorResponse(id any, code int, message string, data any) *models.JSONRPCResponse {
	return &models.JSONRPCResponse{
		JSONRPC: "2.0",
		ID:      id,
		Error: &models.JSONRPCError{
			Code:    code,
			Message: message,
			Data:    data,
		},
	}
}
