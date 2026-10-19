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
// Streaming lives in sse.go (phase 2): StreamRunEvents / StreamChat parse
// the SSE event streams, skipping the ':'-prefixed keepalive comment lines
// the gateway emits every 10s. The sync methods in this file use the
// non-stream endpoints and poll via GET status.
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
	// ErrHermesSessionExists is returned on HTTP 409 code=session_exists from
	// CreateSession: the chosen session id is already taken. Distinct from
	// ErrHermesSessionForeign (chat turn on a foreign-owned session):
	// callers may retry with a different id.
	ErrHermesSessionExists = errors.New("hermes session id already exists (pick another id)")
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
	// SessionKey, when non-empty, is sent as the X-Hermes-Session-Key
	// header (upstream capabilities flag "session_key_header"): a
	// client-declared long-term memory scope. An empty value sends NO
	// header, never an empty one.
	SessionKey string
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
	Object  string `json:"object"`
	Session string `json:"session_id"`
	Message struct {
		Role    string `json:"role"`
		Content string `json:"content"`
	} `json:"message"`
	Usage *Usage `json:"usage,omitempty"`
}

// Content returns the assistant text of the turn.
func (r *ChatResult) Content() string { return r.Message.Content }

// RunOptions carries the per-run model override for async runs plus an
// optional session binding (upstream POST /v1/runs accepts a body
// "session_id" to run a turn inside an existing session).
type RunOptions struct {
	Provider  string
	Model     string
	SessionID string
	// SessionKey mirrors ChatOptions.SessionKey: sent as the
	// X-Hermes-Session-Key header when non-empty, never as an empty value.
	SessionKey string
}

// RunStatus is the GET /v1/runs/{id} document: run_id + status + output
// when terminal. Upstream's status dict names the final text "output"
// (api_server_runs _finish("completed", ..., output=...)); "result" is
// accepted as a legacy alias for stubs written against the leaf-01 shape.
type RunStatus struct {
	RunID     string `json:"run_id"`
	Status    string `json:"status"`
	Output    string `json:"output,omitempty"`
	Result    string `json:"result,omitempty"`
	Error     string `json:"error,omitempty"`
	SessionID string `json:"session_id,omitempty"`
}

// FinalText returns the run's terminal output, preferring the upstream
// "output" field over the legacy "result" alias.
func (s *RunStatus) FinalText() string {
	if s.Output != "" {
		return s.Output
	}
	return s.Result
}

// Session is one entry of GET /api/sessions. Upstream names the start time
// "started_at" (epoch float) on the list rows; created_at is accepted as a
// fallback for gateways that name it directly.
type Session struct {
	ID        string          `json:"id"`
	CreatedAt json.RawMessage `json:"created_at,omitempty"`
	StartedAt json.RawMessage `json:"started_at,omitempty"`
	Title     string          `json:"title,omitempty"`
	Model     string          `json:"model,omitempty"`
	Source    string          `json:"source,omitempty"`
}

// Created returns the session's creation stamp — created_at when present,
// else started_at — as raw JSON text for the tool layer.
func (s *Session) Created() string {
	if len(s.CreatedAt) > 0 {
		return string(s.CreatedAt)
	}
	return string(s.StartedAt)
}

// SessionDetail is the session object inside POST /api/sessions and
// POST /api/sessions/{id}/model responses ("hermes.session" / runtime
// projection). started_at is an epoch float upstream, so raw JSON rides
// along for the tool layer to surface.
type SessionDetail struct {
	ID        string          `json:"id"`
	Source    string          `json:"source,omitempty"`
	Model     string          `json:"model,omitempty"`
	Title     string          `json:"title,omitempty"`
	CreatedAt json.RawMessage `json:"started_at,omitempty"`
	ParentID  string          `json:"parent_session_id,omitempty"`
}

// CreateSessionOptions carries the POST /api/sessions body fields (leaf-01
// follow-up; upstream _handle_create_session). Zero values omit the field;
// empty ID lets the gateway mint "api_<epoch>_<hex>". RequireModelLock
// makes the gateway refuse a model/provider it cannot route (409
// model_lock_unavailable) instead of silently falling back to the global
// model.
type CreateSessionOptions struct {
	ID               string
	Title            string
	SystemPrompt     string
	Provider         string
	Model            string
	RequireModelLock bool
}

// ModelLockResult is the projection of a POST /api/sessions/{id}/model
// ack: the runtime block (provider/model/route_source actually locked).
type ModelLockResult struct {
	SessionID string `json:"session_id"`
	Runtime   struct {
		Provider    string `json:"provider"`
		Model       string `json:"model"`
		RouteSource string `json:"route_source"`
		ModelLock   string `json:"model_lock"`
	} `json:"runtime"`
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
	return c.doHeaders(ctx, endpoint, method, path, in, out, nil, expectStatus...)
}

// doHeaders is do() plus extra request headers (the X-Hermes-Session-Key
// seam). headers may be nil; values are never logged.
func (c *Client) doHeaders(ctx context.Context, endpoint, method, path string, in, out any, headers map[string]string, expectStatus ...int) error {
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
	for k, v := range headers {
		req.Header.Set(k, v)
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

// clientSessionKeyHeaders builds the X-Hermes-Session-Key header map for a
// non-empty session key (empty -> nil map, i.e. NO header — never an empty
// value). Upstream rejects CR/LF/NUL in the header with a 400
// (api_server _parse_session_key_header), so such values never go on the
// wire: the client refuses them locally. `client`-prefixed per the p2-01/
// p2-02 name split (p2-02 owns sse-prefixed helpers in this package).
func clientSessionKeyHeaders(sessionKey string) (map[string]string, error) {
	if sessionKey == "" {
		return nil, nil
	}
	if strings.ContainsAny(sessionKey, "\r\n\x00") {
		return nil, fmt.Errorf("session key must not contain control characters")
	}
	return map[string]string{"X-Hermes-Session-Key": sessionKey}, nil
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
		// Two 409 families upstream: code=session_exists (the chosen id is
		// taken — retryable with another id) vs code=permission_denied
		// (foreign-owned session — never retried). Disambiguate by the
		// OpenAI-style error envelope's code field.
		if bodyHasErrorCode(body, "session_exists") {
			return fmt.Errorf("hermes %s: %w: %s", endpoint, ErrHermesSessionExists, snippet)
		}
		return fmt.Errorf("hermes %s: %w: %s", endpoint, ErrHermesSessionForeign, snippet)
	case http.StatusUnauthorized:
		return fmt.Errorf("hermes %s: %w: %s", endpoint, ErrHermesAuth, snippet)
	default:
		return &APIError{Endpoint: endpoint, StatusCode: resp.StatusCode, Body: snippet}
	}
}

// bodyHasErrorCode reports whether an upstream error body carries the given
// OpenAI-style code ({"error":{"code":...}}). A malformed/absent body simply
// does not match — classification falls through to the default family.
func bodyHasErrorCode(body []byte, code string) bool {
	var env struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return false
	}
	return env.Error.Code == code
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
	headers, err := clientSessionKeyHeaders(opts.SessionKey)
	if err != nil {
		return nil, fmt.Errorf("hermes chat: %w", err)
	}
	var res ChatResult
	if err := c.doHeaders(ctx, "POST chat", http.MethodPost, path, payload, &res, headers, http.StatusOK); err != nil {
		return nil, err
	}
	return &res, nil
}

// --- async runs --------------------------------------------------------------------

// CreateRun submits an async run (POST /v1/runs) and returns the run_id
// from the 202 response. opts.SessionID binds the run to an existing
// session (upstream body "session_id"); opts' model/provider ride the
// documented per-request override fields when non-empty.
func (c *Client) CreateRun(ctx context.Context, input string, opts RunOptions) (string, error) {
	if input == "" {
		return "", fmt.Errorf("hermes run: input required")
	}
	payload := map[string]any{"input": input}
	if opts.SessionID != "" {
		payload["session_id"] = opts.SessionID
	}
	if opts.Model != "" {
		payload["model"] = opts.Model
	}
	if opts.Provider != "" {
		payload["provider"] = opts.Provider
	}
	headers, err := clientSessionKeyHeaders(opts.SessionKey)
	if err != nil {
		return "", fmt.Errorf("hermes run: %w", err)
	}
	var res struct {
		RunID string `json:"run_id"`
	}
	if err := c.doHeaders(ctx, "POST /v1/runs", http.MethodPost, "/v1/runs", payload, &res, headers, http.StatusAccepted); err != nil {
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

// CreateSession creates an empty Hermes session row (POST /api/sessions,
// upstream _handle_create_session; hermes-interop follow-up to leaf 01).
// Empty opts.ID lets the gateway mint "api_<epoch>_<hex>"; an explicit ID
// already taken returns HTTP 409 code=session_exists, classified as
// ErrHermesSessionExists. A model/provider with RequireModelLock refuses
// an unroutable selection (409 model_lock_unavailable -> APIError) rather
// than silently falling back to the gateway's global model.
func (c *Client) CreateSession(ctx context.Context, opts CreateSessionOptions) (*SessionDetail, error) {
	payload := map[string]any{}
	if opts.ID != "" {
		payload["id"] = opts.ID
	}
	if opts.Title != "" {
		payload["title"] = opts.Title
	}
	if opts.SystemPrompt != "" {
		payload["system_prompt"] = opts.SystemPrompt
	}
	if opts.Provider != "" {
		payload["provider"] = opts.Provider
	}
	if opts.Model != "" {
		payload["model"] = opts.Model
	}
	if opts.RequireModelLock {
		payload["require_model_lock"] = true
	}
	var res struct {
		Object  string        `json:"object"`
		Session SessionDetail `json:"session"`
	}
	if err := c.do(ctx, "POST /api/sessions", http.MethodPost, "/api/sessions", payload, &res, http.StatusCreated); err != nil {
		return nil, err
	}
	if res.Session.ID == "" {
		return nil, fmt.Errorf("hermes create session: 201 response missing session.id")
	}
	return &res.Session, nil
}

// SetSessionModel locks a session's model for subsequent turns
// (POST /api/sessions/{id}/model, upstream _handle_session_model_lock —
// the gateway advertises it as feature flag "session_model_lock"). The
// ack echoes the effective runtime (provider/model actually locked).
func (c *Client) SetSessionModel(ctx context.Context, sessionID, provider, model string) (*ModelLockResult, error) {
	if sessionID == "" {
		return nil, fmt.Errorf("hermes model lock: session id required")
	}
	if provider == "" && model == "" {
		return nil, fmt.Errorf("hermes model lock: provider or model required")
	}
	payload := map[string]any{"require_model_lock": true}
	if provider != "" {
		payload["provider"] = provider
	}
	if model != "" {
		payload["model"] = model
	}
	path := "/api/sessions/" + url.PathEscape(sessionID) + "/model"
	var res ModelLockResult
	if err := c.do(ctx, "POST /api/sessions/{id}/model", http.MethodPost, path, payload, &res); err != nil {
		return nil, err
	}
	return &res, nil
}

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
	// Dual shape: upstream _handle_list_sessions returns
	// {"object":"list","data":[...]}; the leaf-01 tests speak
	// {"sessions":[...],"total":N}. Accept either.
	var res struct {
		Sessions []Session `json:"sessions"`
		Data     []Session `json:"data"`
		Total    int       `json:"total"`
	}
	if err := c.do(ctx, "GET /api/sessions", http.MethodGet, path, nil, &res, http.StatusOK); err != nil {
		return nil, err
	}
	list := res.Sessions
	if len(list) == 0 {
		list = res.Data
	}
	if list == nil {
		list = []Session{}
	}
	return list, nil
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
	// Dual shape, as in ListSessions: upstream returns
	// {"object":"list","data":[...]}; leaf-01 tests speak
	// {"messages":[...]}. Accept either.
	var res struct {
		Messages []Message `json:"messages"`
		Data     []Message `json:"data"`
	}
	if err := c.do(ctx, "GET /api/sessions/{id}/messages", http.MethodGet, path, nil, &res); err != nil {
		return nil, err
	}
	list := res.Messages
	if len(list) == 0 {
		list = res.Data
	}
	if list == nil {
		list = []Message{}
	}
	return list, nil
}

// --- run approval (hermes-interop p2-01, Contract P2-A) -------------------------

// Approval decisions accepted by RunApproval. Upstream's canonical
// vocabulary is once/session/always/deny (api_server_runs
// _handle_run_approval); "approve" is the server-side alias for "once"
// (_APPROVAL_CHOICE_ALIASES), normalized here so the wire body always
// carries the canonical choice.
const (
	ApprovalOnce    = "once"
	ApprovalSession = "session"
	ApprovalAlways  = "always"
	ApprovalDeny    = "deny"
)

// clientApprovalChoices maps the canonical choices plus the "approve"
// alias callers (and the p2-03 tool verb) naturally use.
var clientApprovalChoices = map[string]string{
	ApprovalOnce:    ApprovalOnce,
	ApprovalSession: ApprovalSession,
	ApprovalAlways:  ApprovalAlways,
	ApprovalDeny:    ApprovalDeny,
	"approve":       ApprovalOnce,
	"approved":      ApprovalOnce,
	"allow":         ApprovalOnce,
}

// RunApproval resolves a pending run approval
// (POST /v1/runs/{run_id}/approval). Body shape pinned against upstream
// api_server_runs._handle_run_approval (2026-10-19): {"choice":
// "once"|"session"|"always"|"deny"} — NOT the master-plan fallback
// {"decision": ...}. reason is not an upstream field; it is sent anyway
// (ignored server-side) so the audit trail rides the request, marked
// TODO(upstream-shape) until upstream grows a reason field. decision is
// normalized ("approve" -> "once") and validated locally: an unknown
// choice never reaches the wire. 409 (approval_not_pending /
// approval_not_active) classifies via the shared conflict path.
func (c *Client) RunApproval(ctx context.Context, runID, decision, reason string) error {
	if runID == "" {
		return fmt.Errorf("hermes approval: run id required")
	}
	choice, ok := clientApprovalChoices[strings.ToLower(strings.TrimSpace(decision))]
	if !ok {
		return fmt.Errorf("hermes approval: invalid decision %q (want approve/once/session/always/deny)", decision)
	}
	payload := map[string]any{"choice": choice}
	if reason != "" {
		// TODO(upstream-shape): upstream ignores unknown body fields;
		// kept for gateway-side audit logging if/when it lands.
		payload["reason"] = reason
	}
	path := "/v1/runs/" + url.PathEscape(runID) + "/approval"
	var res struct {
		RunID    string `json:"run_id"`
		Choice   string `json:"choice"`
		Resolved int    `json:"resolved"`
	}
	return c.do(ctx, "POST /v1/runs/{id}/approval", http.MethodPost, path, payload, &res, http.StatusOK)
}

// --- cron jobs (hermes-interop p2-01, Contract P2-A) ----------------------------

// Job is one upstream cron job record (cron/jobs.py create_job /
// _normalize_job_record). Fields pinned 2026-10-19: schedule is a dict
// {"kind","expr"|"minutes"|"run_at","display"} on the wire (a bare string
// on legacy records) and flattens to its display text here; Status maps
// the record's "state" ("scheduled"/"paused"/"completed"/"error").
// Extra stores fields (skills, deliver, next_run_at, ...) ride Raw for
// the tool layer without pinning every key.
type Job struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Prompt   string `json:"prompt"`
	Schedule string `json:"-"`
	Status   string `json:"state"`
	Enabled  bool   `json:"enabled"`
	Deliver  string `json:"deliver,omitempty"`
	// Repeat carries {"times": N|null, "completed": N}; times null =
	// forever (upstream create_job).
	Repeat *struct {
		Times     *int `json:"times"`
		Completed int  `json:"completed"`
	} `json:"repeat,omitempty"`
	NextRunAt  string          `json:"next_run_at,omitempty"`
	LastRunAt  string          `json:"last_run_at,omitempty"`
	LastStatus string          `json:"last_status,omitempty"`
	Raw        json.RawMessage `json:"-"`
}

// clientJobSchedule is the upstream schedule dict; Kind drives which of
// Expr/Minutes/RunAt is meaningful, Display is the human text.
type clientJobSchedule struct {
	Kind    string `json:"kind"`
	Expr    string `json:"expr,omitempty"`
	Minutes int    `json:"minutes,omitempty"`
	RunAt   string `json:"run_at,omitempty"`
	Display string `json:"display,omitempty"`
}

// text flattens the schedule to a single display string: display wins,
// then the kind-specific field, then kind itself.
func (s clientJobSchedule) text() string {
	if s.Display != "" {
		return s.Display
	}
	switch s.Kind {
	case "cron":
		if s.Expr != "" {
			return s.Expr
		}
	case "interval":
		if s.Minutes > 0 {
			return fmt.Sprintf("every %dm", s.Minutes)
		}
	case "once":
		if s.RunAt != "" {
			return s.RunAt
		}
	}
	return s.Kind
}

// UnmarshalJSON decodes the dual wire shape: schedule as a dict (current
// upstream) or a bare string (legacy/hand-edited records), keeping Raw
// for callers that need untouched fields.
func (j *Job) UnmarshalJSON(data []byte) error {
	type jobAlias Job
	var alias jobAlias
	if err := json.Unmarshal(data, &alias); err != nil {
		return err
	}
	*j = Job(alias)
	j.Raw = append(json.RawMessage(nil), data...)

	var probe struct {
		Schedule json.RawMessage `json:"schedule"`
		// schedule_display is stamped by _normalize_job_record on every
		// read path; prefer it when present.
		ScheduleDisplay string `json:"schedule_display"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return err
	}
	switch {
	case probe.ScheduleDisplay != "":
		j.Schedule = probe.ScheduleDisplay
	case len(probe.Schedule) > 0 && probe.Schedule[0] == '"':
		var s string
		if err := json.Unmarshal(probe.Schedule, &s); err != nil {
			return err
		}
		j.Schedule = s
	case len(probe.Schedule) > 0 && probe.Schedule[0] == '{':
		var s clientJobSchedule
		if err := json.Unmarshal(probe.Schedule, &s); err != nil {
			return err
		}
		j.Schedule = s.text()
	}
	return nil
}

// MarshalJSON encodes the create/update body shape: schedule goes on the
// wire as the bare string the upstream handler parses (parse_schedule
// accepts "30m", "every day at 9am", cron exprs, ISO timestamps). Zero
// fields are omitted so the server applies its own defaults.
func (j Job) MarshalJSON() ([]byte, error) {
	body := map[string]any{}
	if j.ID != "" {
		body["id"] = j.ID
	}
	if j.Name != "" {
		body["name"] = j.Name
	}
	if j.Prompt != "" {
		body["prompt"] = j.Prompt
	}
	if j.Schedule != "" {
		body["schedule"] = j.Schedule
	}
	if j.Deliver != "" {
		body["deliver"] = j.Deliver
	}
	if j.Repeat != nil && j.Repeat.Times != nil {
		body["repeat"] = *j.Repeat.Times
	}
	return json.Marshal(body)
}

// clientJobEnvelope is the {"job": ...} wrapper every /api/jobs mutation
// returns (upstream _job_response / _handle_create_job).
type clientJobEnvelope struct {
	Job Job `json:"job"`
}

// ListJobs reads GET /api/jobs. Upstream has NO pagination on this route
// (the whole store is one array; only include_disabled filters), so
// neither limit nor offset is offered. Disabled/paused jobs are included
// — filtering is the caller's decision.
func (c *Client) ListJobs(ctx context.Context) ([]Job, error) {
	var res struct {
		Jobs []Job `json:"jobs"`
	}
	if err := c.do(ctx, "GET /api/jobs", http.MethodGet, "/api/jobs?include_disabled=true", nil, &res, http.StatusOK); err != nil {
		return nil, err
	}
	if res.Jobs == nil {
		res.Jobs = []Job{}
	}
	return res.Jobs, nil
}

// CreateJob posts POST /api/jobs. Upstream requires name + schedule
// (400 otherwise); validate locally so a missing field never round-trips.
// Returns the stored record (with the gateway-minted 12-hex-char id).
func (c *Client) CreateJob(ctx context.Context, j Job) (Job, error) {
	if strings.TrimSpace(j.Name) == "" {
		return Job{}, fmt.Errorf("hermes create job: name required")
	}
	if strings.TrimSpace(j.Schedule) == "" {
		return Job{}, fmt.Errorf("hermes create job: schedule required")
	}
	var res clientJobEnvelope
	if err := c.do(ctx, "POST /api/jobs", http.MethodPost, "/api/jobs", j, &res, http.StatusOK); err != nil {
		return Job{}, err
	}
	return res.Job, nil
}

// GetJob reads GET /api/jobs/{id}. 404 (unknown id) surfaces as
// *APIError{StatusCode: 404}.
func (c *Client) GetJob(ctx context.Context, id string) (Job, error) {
	if id == "" {
		return Job{}, fmt.Errorf("hermes get job: id required")
	}
	var res clientJobEnvelope
	path := "/api/jobs/" + url.PathEscape(id)
	if err := c.do(ctx, "GET /api/jobs/{id}", http.MethodGet, path, nil, &res, http.StatusOK); err != nil {
		return Job{}, err
	}
	return res.Job, nil
}

// clientJobAction posts one of the /pause /resume /run mutations; all
// three return the updated {"job": ...} record and are discarded here
// (the contract is error-only).
func (c *Client) clientJobAction(ctx context.Context, endpoint, id, action string) error {
	if id == "" {
		return fmt.Errorf("hermes %s job: id required", action)
	}
	path := "/api/jobs/" + url.PathEscape(id) + "/" + action
	var res clientJobEnvelope
	// Upstream accepts an absent body on all three; run supports an
	// optional {"prompt": ...} override the contract does not expose.
	return c.do(ctx, endpoint, http.MethodPost, path, nil, &res, http.StatusOK)
}

// PauseJob posts POST /api/jobs/{id}/pause.
func (c *Client) PauseJob(ctx context.Context, id string) error {
	return c.clientJobAction(ctx, "POST /api/jobs/{id}/pause", id, "pause")
}

// ResumeJob posts POST /api/jobs/{id}/resume.
func (c *Client) ResumeJob(ctx context.Context, id string) error {
	return c.clientJobAction(ctx, "POST /api/jobs/{id}/resume", id, "resume")
}

// RunJob triggers an immediate execution: POST /api/jobs/{id}/run.
func (c *Client) RunJob(ctx context.Context, id string) error {
	return c.clientJobAction(ctx, "POST /api/jobs/{id}/run", id, "run")
}
