package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// HTTPTransport implements MCP transport over HTTP.
//
// Requests are sent via HTTP POST to the server URL.
// Responses may be either JSON or Server-Sent Events (SSE).
type HTTPTransport struct {
	url     string
	headers map[string]string
	config  Config

	client    *http.Client
	sessionID string
	running   atomic.Bool
	mu        sync.RWMutex
}

// NewHTTPTransport creates a new HTTP transport.
func NewHTTPTransport(url string, headers map[string]string, config Config) *HTTPTransport {
	timeout := time.Duration(config.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &HTTPTransport{
		url:     url,
		headers: headers,
		config:  config,
		client: &http.Client{
			Timeout: timeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= 5 {
					return fmt.Errorf("too many redirects")
				}
				return nil
			},
		},
	}
}

// Start initializes the HTTP transport.
// For HTTP, this just marks the transport as running.
func (t *HTTPTransport) Start(ctx context.Context) error {
	t.running.Store(true)
	return nil
}

// Send sends a JSON-RPC request via HTTP POST.
func (t *HTTPTransport) Send(ctx context.Context, message []byte) ([]byte, error) {
	if !t.running.Load() {
		return nil, fmt.Errorf("transport not running")
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, "POST", t.url, bytes.NewReader(message))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	// Set headers
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json, text/event-stream")

	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	// Add session ID if we have one
	t.mu.RLock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.RUnlock()

	// Send request
	resp, err := t.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	// Track session ID from response
	if sessionID := resp.Header.Get("Mcp-Session-Id"); sessionID != "" {
		t.mu.Lock()
		t.sessionID = sessionID
		t.mu.Unlock()
	}

	// Handle error status codes
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")

	// Handle SSE response
	if strings.Contains(contentType, "text/event-stream") {
		return t.parseSSEResponse(resp.Body)
	}

	// Handle JSON response
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return body, nil
}

// parseSSEResponse parses a Server-Sent Events response.
func (t *HTTPTransport) parseSSEResponse(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(r)

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Handle data line
		if strings.HasPrefix(line, "data: ") {
			data := strings.TrimPrefix(line, "data: ")

			// Try to parse as JSON to verify it's a response
			var parsed map[string]any
			if err := json.Unmarshal([]byte(data), &parsed); err != nil {
				continue
			}

			// Check if it's a response (has result or error)
			if _, hasResult := parsed["result"]; hasResult {
				return []byte(data), nil
			}
			if _, hasError := parsed["error"]; hasError {
				return []byte(data), nil
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading SSE: %w", err)
	}

	return nil, fmt.Errorf("no response received in SSE stream")
}

// Close terminates the HTTP transport.
func (t *HTTPTransport) Close() error {
	t.running.Store(false)
	return nil
}

// IsRunning returns true if the transport is active.
func (t *HTTPTransport) IsRunning() bool {
	return t.running.Load()
}

// GetSessionID returns the current MCP session ID.
func (t *HTTPTransport) GetSessionID() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessionID
}

// SetSessionID sets the MCP session ID.
func (t *HTTPTransport) SetSessionID(id string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.sessionID = id
}

// Ensure HTTPTransport implements Transport
var _ Transport = (*HTTPTransport)(nil)
