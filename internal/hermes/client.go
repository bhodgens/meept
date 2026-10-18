// Package hermes implements the HTTP client for the Hermes gateway API
// Server (hermes-interop tree, leaf 01 / master.md Contract B). Meept is the
// CLIENT; a running Hermes gateway is the remote agent.
//
// The upstream route table is frozen in
// docs/plans/hermes-interop/master.md "Verified upstream contract"
// (verified against hermes-agent gateway/platforms/api_server.py on
// 2026-10-15) — do not invent routes here.
//
// Error contract: typed sentinel errors (ErrHermesBusy for 429,
// ErrHermesSessionForeign for 409, ErrHermesAuth for 401) checked with
// errors.Is, mirroring internal/llm's typed-error style. Network and 5xx
// failures wrap *APIError with the endpoint name. The API key is sent as a
// Bearer header and is NEVER included in any error string or log output.
//
// Non-goals (this tree): no SSE/streaming — sync chat uses the non-stream
// endpoint and runs poll via GET status; keepalive comment lines therefore
// never reach this client.
package hermes

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

// Typed sentinel errors, per master.md Contract B.
var (
	// ErrHermesBusy is returned on HTTP 429 (concurrent-run cap exceeded).
	// Callers must back off, not error-loop; the tool surfaces it as a
	// readable busy message.
	ErrHermesBusy = errors.New("hermes busy: concurrent run cap reached (back off and retry)")
	// ErrHermesSessionForeign is returned on HTTP 409 permission_denied:
	// the named session belongs to another Hermes surface (CLI/ACP/
	// messaging). NEVER retried — surface it as a readable message.
	ErrHermesSessionForeign = errors.New("hermes session owned by another surface (permission_denied)")
	// ErrHermesAuth is returned on HTTP 401: missing/invalid API key.
	ErrHermesAuth = errors.New("hermes auth rejected (check the key in api_key_env)")
)

// capabilitiesTTL bounds the Capabilities probe cache. One probe per TTL,
// never per call (Contract B).
const capabilitiesTTL = 60 * time.Second

// Client is an HTTP client for the Hermes API Server. Safe for concurrent
// use; the capabilities cache is mutex-guarded.
type Client struct {
	baseURL    string
	apiKey     string
	httpClient *http.Client

	capsMu      sync.Mutex
	caps        *Capabilities
	capsFetched time.Time
}

// NewClient builds a client pointed at baseURL, authenticating with apiKey
// (from meept config's hermes.api_key_env indirection — the caller resolves
// the env var; the client never logs it). timeout bounds every request;
// sync chat turns are long, so callers pass the config timeout (default
// 300s), not an HTTP-client default.
func NewClient(baseURL, apiKey string, timeout time.Duration) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		apiKey:  apiKey,
		httpClient: &http.Client{
			Timeout: timeout,
		},
	}
}

// BaseURL returns the configured gateway root. Exposed for leaf 02's tool
// status verb (never the key).
func (c *Client) BaseURL() string { return c.baseURL }

// --- wire shapes (mirror the documented JSON, master.md) --------------------

// EndpointSpec is one entry of the capabilities endpoint table.
type EndpointSpec struct {
	Method string `json:"method"`
	Path   string `json:"path"`
}

// Capabilities is the GET /v1/capabilities document: feature flags plus the
// endpoint table. The client MUST probe this before first use and degrade
// per-flag.
type Capabilities struct {
	Features  map[string]bool         `json:"features"`
	Endpoints map[string]EndpointSpec `json:"endpoints"`
}

// HasFeature reports whether the gateway advertises a feature flag. Unknown
// flags are false (degrade per-flag).
func (c *Capabilities) HasFeature(name string) bool {
	if c == nil || c.Features == nil {
		return false
	}
	return c.Features[name]
}

// ChatOptions carries the per-request model override (master.md: `model` +
// `provider` fields). Zero values omit the fields entirely.
type ChatOptions struct {
	Provider string
	Model    string
}

// Usage is the token accounting block on a chat completion.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ChatResult is the final response of a synchronous chat turn
// (POST /api/sessions/{id}/chat). The documented shape is an object
// hermes.session.chat.completion carrying message.content.
type ChatResult struct {
	Type    string `json:"type"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Usage *Usage `json:"usage,omitempty"`
}

// Content returns the assistant text of the turn.
func (r *ChatResult) Content() string { return r.Message.Content }

// RunOptions carries the per-run model override for async runs.
type RunOptions struct {
	Provider string
	Model    string
}

// RunStatus is the GET /v1/runs/{id} document: run_id + status + result
// when terminal.
type RunStatus struct {
	RunID  string `json:"run_id"`
	Status string `json:"status"`
	Result string `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Session is one entry of GET /api/sessions.
type Session struct {
	ID        string `json:"id"`
	CreatedAt string `json:"created_at,omitempty"`
	Title     string `json:"title,omitempty"`
}

// Message is one entry of GET /api/sessions/{id}/messages.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// APIError wraps a non-2xx response that has no more specific typed
// classification. StatusCode is the upstream HTTP status; the body snippet
// is truncated and never contains the API key (the key only ever travels in
// the request header, and the snippet is upstream's own bytes).
type APIError struct {
	Endpoint   string
	StatusCode int
	Body       string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("hermes %s: status %d: %s", e.Endpoint, e.StatusCode, e.Body)
}

// --- core request plumbing ---------------------------------------------------

// do executes one authenticated JSON request. It returns the decoded JSON
// body on 2xx (into out, when out is non-nil) and a classified error
// otherwise. expectStatus accepts the documented success statuses (e.g. 200,
// or 202 for run creation).
func (c *Client) do(ctx context.Context, endpoint, method, path string, in, out any, expectStatus ...int) error {
	var bodyReader io.Reader
	if in != nil {
		buf, err := json.Marshal(in)
		if err != nil {
			return fmt.Errorf("hermes %s: encode request: %w", endpoint, err)
		}
		bodyReader = bytes.NewReader(buf)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, bodyReader)
	if err != nil {
		return fmt.Errorf("hermes %s: build request: %w", endpoint, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.apiKey)
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		// Network failure: wrap with the endpoint; never the key.
		return fmt.Errorf("hermes %s: %w", endpoint, err)
	}
	defer func() {
		// Best-effort close on a response we may not have fully read; a
		// close error cannot change the already-decided classification and
		// is consumed (debug-logged) inside the seam.
		respClosing(resp.Body)
	}()

	ok := len(expectStatus) == 0
	for _, s := range expectStatus {
		if resp.StatusCode == s {
			ok = true
			break
		}
	}
	if !ok {
		return c.classifyError(endpoint, resp)
	}

	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return fmt.Errorf("hermes %s: decode response: %w", endpoint, err)
		}
	}
	return nil
}

// respClosing is the deliberate best-effort close seam: the response has
// already been classified by status, and a close failure cannot change the
// outcome. The error is still consumed (logged at debug) rather than
// silently discarded — mirroring internal/llm/codex.go's close handling.
func respClosing(closer io.Closer) {
	if err := closer.Close(); err != nil {
		hermesDebugLog("hermes: close response body", err)
	}
}

// hermesDebugLog drops a best-effort diagnostic on stderr only when
// MEEPT_HERMES_DEBUG is set. Never logs configuration values — endpoint
// names and error text only (the API key never reaches this path).
func hermesDebugLog(msg string, err error) {
	if os.Getenv("MEEPT_HERMES_DEBUG") != "" {
		fmt.Fprintf(os.Stderr, "%s: %v\n", msg, err)
	}
}

// classifyError maps a non-2xx response onto the typed sentinels (Contract
// B): 429 busy, 409 foreign session, 401 auth, everything else a wrapped
// *APIError. Error strings carry only endpoint/status/body — never the key.
func (c *Client) classifyError(endpoint string, resp *http.Response) error {
	body, rerr := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	if rerr != nil {
		// A truncated error body still classifies by status; keep going
		// with whatever bytes arrived.
		body = append(body, []byte(fmt.Sprintf(" (body read error: %v)", rerr))...)
	}
	snippet := strings.TrimSpace(string(body))
	if snippet == "" {
		snippet = http.StatusText(resp.StatusCode)
	}
	// Belt-and-braces: if the upstream ever echoes our key in an error
	// body, strip it before the error escapes this package.
	if c.apiKey != "" {
		snippet = strings.ReplaceAll(snippet, c.apiKey, "***")
	}

	switch resp.StatusCode {
	case http.StatusTooManyRequests:
		return fmt.Errorf("hermes %s: %w: %s", endpoint, ErrHermesBusy, snippet)
	case http.StatusConflict:
		return fmt.Errorf("hermes %s: %w: %s", endpoint, ErrHermesSessionForeign, snippet)
	case http.StatusUnauthorized:
		return fmt.Errorf("hermes %s: %w: %s", endpoint, ErrHermesAuth, snippet)
	default:
		return &APIError{Endpoint: endpoint, StatusCode: resp.StatusCode, Body: snippet}
	}
}

// --- capabilities probe (60s TTL cache) ---------------------------------------

// Capabilities returns the gateway's feature flags + endpoint table. The
// result is cached for capabilitiesTTL (60s) — one probe, not per call.
func (c *Client) Capabilities(ctx context.Context) (*Capabilities, error) {
	c.capsMu.Lock()
	defer c.capsMu.Unlock()

	if c.caps != nil && time.Since(c.capsFetched) < capabilitiesTTL {
		return c.caps, nil
	}

	var caps Capabilities
	if err := c.do(ctx, "GET /v1/capabilities", http.MethodGet, "/v1/capabilities", nil, &caps); err != nil {
		return nil, err
	}
	c.caps = &caps
	c.capsFetched = time.Now()
	return c.caps, nil
}

// --- health --------------------------------------------------------------------

// Health probes GET /health. Non-nil error means the gateway is unreachable,
// unhealthy, or unauthorized.
func (c *Client) Health(ctx context.Context) error {
	return c.do(ctx, "GET /health", http.MethodGet, "/health", nil, nil, http.StatusOK)
}

// --- sync chat -------------------------------------------------------------------

// ChatSync performs one synchronous agent turn on an existing session
// (POST /api/sessions/{id}/chat, non-stream) and returns the final
// response + usage. opts' model/provider ride the documented per-request
// override fields when non-empty.
func (c *Client) ChatSync(ctx context.Context, sessionID, input string, opts ChatOptions) (*ChatResult, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("hermes chat: session id required")
	}
	if input == "" {
		return nil, fmt.Errorf("hermes chat: input required")
	}
	payload := map[string]any{"input": input}
	if opts.Model != "" {
		payload["model"] = opts.Model
	}
	if opts.Provider != "" {
		payload["provider"] = opts.Provider
	}
	path := "/api/sessions/" + url.PathEscape(sessionID) + "/chat"
	var res ChatResult
	if err := c.do(ctx, "POST chat", http.MethodPost, path, payload, &res, http.StatusOK); err != nil {
		return nil, err
	}
	return &res, nil
}

// --- async runs --------------------------------------------------------------------

// CreateRun submits an async run (POST /v1/runs) and returns the run_id
// from the 202 response.
func (c *Client) CreateRun(ctx context.Context, input string, opts RunOptions) (string, error) {
	if input == "" {
		return "", fmt.Errorf("hermes run: input required")
	}
	payload := map[string]any{"input": input}
	if opts.Model != "" {
		payload["model"] = opts.Model
	}
	if opts.Provider != "" {
		payload["provider"] = opts.Provider
	}
	var res struct {
		RunID string `json:"run_id"`
	}
	if err := c.do(ctx, "POST /v1/runs", http.MethodPost, "/v1/runs", payload, &res, http.StatusAccepted); err != nil {
		return "", err
	}
	if res.RunID == "" {
		return "", fmt.Errorf("hermes run: 202 response missing run_id")
	}
	return res.RunID, nil
}

// RunStatus fetches GET /v1/runs/{id} (poll-based; no SSE in this tree).
func (c *Client) RunStatus(ctx context.Context, runID string) (*RunStatus, error) {
	if runID == "" {
		return nil, fmt.Errorf("hermes run status: run id required")
	}
	var res RunStatus
	if err := c.do(ctx, "GET /v1/runs/{id}", http.MethodGet, "/v1/runs/"+url.PathEscape(runID), nil, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

// SteerRun delivers mid-run guidance at the next tool boundary
// (POST /v1/runs/{id}/steer).
func (c *Client) SteerRun(ctx context.Context, runID, message string) error {
	if runID == "" || message == "" {
		return fmt.Errorf("hermes steer: run id and message required")
	}
	payload := map[string]any{"message": message}
	return c.do(ctx, "POST /v1/runs/{id}/steer", http.MethodPost, "/v1/runs/"+url.PathEscape(runID)+"/steer", payload, nil)
}

// StopRun requests a stop (POST /v1/runs/{id}/stop).
func (c *Client) StopRun(ctx context.Context, runID string) error {
	if runID == "" {
		return fmt.Errorf("hermes stop: run id required")
	}
	return c.do(ctx, "POST /v1/runs/{id}/stop", http.MethodPost, "/v1/runs/"+url.PathEscape(runID)+"/stop", map[string]any{}, nil)
}

// --- session reads --------------------------------------------------------------------

// ListSessions reads GET /api/sessions (paginated). limit <= 0 means the
// gateway default (query omitted); offset >= 0 pages from the start.
func (c *Client) ListSessions(ctx context.Context, limit, offset int) ([]Session, error) {
	q := url.Values{}
	if limit > 0 {
		q.Set("limit", fmt.Sprintf("%d", limit))
	}
	if offset > 0 {
		q.Set("offset", fmt.Sprintf("%d", offset))
	}
	path := "/api/sessions"
	if len(q) > 0 {
		path += "?" + q.Encode()
	}
	var res struct {
		Sessions []Session `json:"sessions"`
		Total    int       `json:"total"`
	}
	if err := c.do(ctx, "GET /api/sessions", http.MethodGet, path, nil, &res, http.StatusOK); err != nil {
		return nil, err
	}
	if res.Sessions == nil {
		res.Sessions = []Session{}
	}
	return res.Sessions, nil
}

// SessionMessages reads GET /api/sessions/{id}/messages (paginated).
// limit <= 0 means the gateway default (query omitted).
func (c *Client) SessionMessages(ctx context.Context, sessionID string, limit int) ([]Message, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("hermes messages: session id required")
	}
	path := "/api/sessions/" + url.PathEscape(sessionID) + "/messages"
	if limit > 0 {
		path += "?limit=" + fmt.Sprintf("%d", limit)
	}
	var res struct {
		Messages []Message `json:"messages"`
	}
	if err := c.do(ctx, "GET /api/sessions/{id}/messages", http.MethodGet, path, nil, &res); err != nil {
		return nil, err
	}
	if res.Messages == nil {
		res.Messages = []Message{}
	}
	return res.Messages, nil
}
