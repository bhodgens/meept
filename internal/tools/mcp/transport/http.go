package transport

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	// MaxResponseSize is the maximum allowed response body size (10MB).
	// This prevents memory exhaustion from malicious or misconfigured servers.
	MaxResponseSize = 10 * 1024 * 1024
)

func isBlockedAddress(addr string, allowPrivate bool) bool {
	if allowPrivate {
		return false
	}
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		host = addr
	}
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified()
}

func checkRedirectURL(ctx context.Context, rawURL string, allowPrivate bool) error {
	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid redirect URL: %w", err)
	}
	if !strings.EqualFold(u.Scheme, "http") && !strings.EqualFold(u.Scheme, "https") {
		return fmt.Errorf("scheme %q not allowed", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("redirect URL missing host")
	}
	if isBlockedAddress(host, allowPrivate) {
		slog.Warn("ssrf: blocked direct IP attempt", "host", host)
		return fmt.Errorf("redirect host %q is blocked", host)
	}
	// Use the request's context so that DNS resolution is cancelled when
	// the upstream caller cancels or the deadline expires. Falls back to
	// the background context if req ctx is nil (defensive).
	if ctx == nil {
		ctx = context.Background()
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return fmt.Errorf("resolve redirect %s: %w", host, err)
	}
	for _, ip := range ips {
		if isBlockedAddress(ip.IP.String(), allowPrivate) {
			slog.Warn("ssrf: blocked DNS rebind attempt", "host", host, "resolved", ip.IP)
			return fmt.Errorf("redirect host %s resolves to blocked address %s", host, ip.IP)
		}
	}
	return nil
}

// redirectChecker returns a CheckRedirect policy that threads the request
// context through to checkRedirectURL's DNS lookup. Installed on t.client in
// NewHTTPTransport and SetAllowPrivateRanges.
func redirectChecker() func(*http.Request, []*http.Request) error {
	return func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return checkRedirectURL(req.Context(), req.URL.String(), false)
	}
}

// HTTPTransport implements MCP transport over HTTP.
//
// Requests are sent via HTTP POST to the server URL.
// Responses may be either JSON or Server-Sent Events (SSE).
type HTTPTransport struct {
	url     string
	headers map[string]string
	config  Config

	client       *http.Client
	sessionID    string
	allowPrivate bool
	running      atomic.Bool
	mu           sync.RWMutex
}

// NewHTTPTransport creates a new HTTP transport.
func NewHTTPTransport(url string, headers map[string]string, config Config) *HTTPTransport {
	timeout := time.Duration(config.TimeoutMS) * time.Millisecond
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &HTTPTransport{
		url:          url,
		headers:      headers,
		config:       config,
		allowPrivate: false,
		client: &http.Client{
			Timeout: timeout,
			Transport: &http.Transport{
				DialContext: ssrfDialContext(false),
			},
			CheckRedirect: redirectChecker(),
		},
	}
}

// SetAllowPrivateRanges disables SSRF IP filtering for private/loopback
// addresses. This is intended only for tests that use httptest.NewServer
// (which binds 127.0.0.1). Production callers must not use it.
func (t *HTTPTransport) SetAllowPrivateRanges(allow bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.allowPrivate = allow
	timeout := t.client.Timeout
	t.client = &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			DialContext: ssrfDialContext(allow),
		},
		CheckRedirect: redirectChecker(),
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

	// Pre-check the initial URL for SSRF before making the request.
	// This is critical because the dial-time check in ssrfDialContext happens
	// after DNS resolution, and fast-flux DNS can return different IPs between
	// a pre-check and dial time. We need BOTH checks:
	// 1. Pre-check: validate the URL and its DNS resolution now
	// 2. Dial-time: re-validate at socket connection time (ssrfDialContext)
	if err := checkRedirectURL(ctx, t.url, t.allowPrivate); err != nil {
		return nil, err
	}

	// Check request body size to prevent memory exhaustion
	if len(message) > MaxResponseSize {
		return nil, fmt.Errorf("request body exceeds maximum size (%d bytes)", MaxResponseSize)
	}

	// Create request
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(message))
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
		// Limit error body size to prevent memory exhaustion
		limitedReader := io.LimitReader(resp.Body, MaxResponseSize)
		body, _ := io.ReadAll(limitedReader)
		return nil, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(body))
	}

	contentType := resp.Header.Get("Content-Type")

	// Wrap body in a limited reader to prevent memory exhaustion
	limitedBody := io.LimitReader(resp.Body, MaxResponseSize)

	// Handle SSE response
	if strings.Contains(contentType, "text/event-stream") {
		return t.parseSSEResponse(limitedBody)
	}

	// Handle JSON response
	body, err := io.ReadAll(limitedBody)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	return body, nil
}

// parseSSEResponse parses a Server-Sent Events response.
//
// B-13 FIX: Previously this returned on the first event containing a result
// or error, silently discarding subsequent events. Now it collects ALL
// result/error payloads. For the common single-response case (standard MCP
// request/response), it returns the payload as-is for backwards
// compatibility. If multiple result/error events are present, it returns a
// JSON array of all payloads.
func (t *HTTPTransport) parseSSEResponse(r io.Reader) ([]byte, error) {
	scanner := bufio.NewScanner(r)
	// Use a larger buffer (up to 10MB) to handle large SSE events.
	scanner.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)

	var payloads []json.RawMessage

	for scanner.Scan() {
		line := scanner.Text()

		// Skip empty lines and comments
		if line == "" || strings.HasPrefix(line, ":") {
			continue
		}

		// Handle data line — SSE spec allows "data:" with or without a space.
		// Try "data: " (with space) first, then fall back to "data:" (without space).
		var data string
		if after, ok := strings.CutPrefix(line, "data: "); ok {
			data = after
		} else if after, ok := strings.CutPrefix(line, "data:"); ok {
			data = strings.TrimSpace(after)
		} else {
			continue
		}

		// Try to parse as JSON to verify it's a response
		var parsed map[string]any
		if err := json.Unmarshal([]byte(data), &parsed); err != nil {
			continue
		}

		// Collect result/error events instead of returning on the first one.
		if _, hasResult := parsed["result"]; hasResult {
			payloads = append(payloads, json.RawMessage(data))
			continue
		}
		if _, hasError := parsed["error"]; hasError {
			payloads = append(payloads, json.RawMessage(data))
			continue
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("error reading SSE: %w", err)
	}

	switch len(payloads) {
	case 0:
		return nil, fmt.Errorf("no response received in SSE stream")
	case 1:
		// Common case: single response — return as-is for backwards compatibility.
		return payloads[0], nil
	default:
		// Multiple result/error events — return as a JSON array.
		combined, err := json.Marshal(payloads)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal multiple SSE payloads: %w", err)
		}
		return combined, nil
	}
}

// SendNotification sends a JSON-RPC notification via HTTP POST without waiting
// for a meaningful response. It fires the request and discards the body.
func (t *HTTPTransport) SendNotification(ctx context.Context, message []byte) error {
	if !t.running.Load() {
		return fmt.Errorf("transport not running")
	}

	// Pre-check the initial URL for SSRF before making the request.
	// Same dual-check pattern as Send(): pre-check + dial-time validation.
	if err := checkRedirectURL(ctx, t.url, t.allowPrivate); err != nil {
		return err
	}

	// Enforce the same size cap as Send to prevent memory-exhaustion via
	// oversized notifications.
	if len(message) > MaxResponseSize {
		return fmt.Errorf("notification body exceeds maximum size (%d bytes)", MaxResponseSize)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url, bytes.NewReader(message))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	for k, v := range t.headers {
		req.Header.Set(k, v)
	}

	t.mu.RLock()
	if t.sessionID != "" {
		req.Header.Set("Mcp-Session-Id", t.sessionID)
	}
	t.mu.RUnlock()

	resp, err := t.client.Do(req)
	if err != nil {
		return fmt.Errorf("request failed: %w", err)
	}
	resp.Body.Close()

	return nil
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
