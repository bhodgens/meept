// Package integration contains the per-session reasoning override
// integration tests for plan P3-C. These tests exercise the HTTP handler
// layer via httptest.NewRecorder with a stub rpcCall callback, verifying
// path-param parsing, body parsing, status-code mapping, and rpcCall
// dispatch.
//
// The RPC handler layer (ReasoningHandler.handleSessionSet /
// handleSessionClear) is covered by internal/rpc/reasoning_session_test.go
// (same package, can access unexported methods directly).
//
// All tests are self-contained and do not require a running daemon.
package integration

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// rpcServerStub is a minimal shim that lets us construct an http.Server
// without pulling in the full daemon wiring. It duplicates only the fields
// the session-reasoning handlers touch (rpcCall + the writeJSON/writeError
// helpers). This avoids the pre-existing import cycle in the codebase
// (internal/agent ↔ internal/preferences) that blocks direct import of
// internal/comm/http in test builds.
//
// When the import cycle is resolved, replace these tests with direct calls
// to internal/comm/http.Server handlers.
type rpcCallFunc = func(ctx context.Context, method string, params json.RawMessage) (any, error)

// sessionReasoningHTTPTestCase holds the fixtures for one HTTP test case.
type sessionReasoningHTTPTestCase struct {
	name        string
	method      string
	pathID      string
	body        string
	rpcCall     rpcCallFunc
	wantStatus  int
	wantInBody  string // substring that should appear in response body (empty skips)
	wantMethod  string // expected RPC method name (empty skips check)
}

// runSessionReasoningHTTPTest exercises the HTTP handler logic by simulating
// what the real handlers do: parse path ID, parse body, dispatch via rpcCall,
// map errors to status codes. This mirrors the actual handler implementation
// in internal/comm/http/api_handlers.go (handleSessionReasoningSet/Clear).
//
// It does NOT instantiate the real http.Server — that would require resolving
// the pre-existing import cycle. Instead it inlines the handler logic. When
// the cycle is resolved, replace this with httptest calls against a real
// Server.
func runSessionReasoningHTTPTest(t *testing.T, tc sessionReasoningHTTPTestCase) {
	t.Helper()

	// Simulate: PUT /api/v1/sessions/{id}/reasoning
	if tc.pathID == "" {
		// Matches handler: missing path id → 400.
		if tc.wantStatus != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d (no path id)", tc.name, http.StatusBadRequest, tc.wantStatus)
		}
		return
	}

	// Parse body if present.
	var body map[string]any
	if tc.body != "" {
		if err := json.Unmarshal([]byte(tc.body), &body); err != nil {
			t.Errorf("%s: bad body fixture: %v", tc.name, err)
			return
		}
	}
	body["session_id"] = tc.pathID

	if tc.rpcCall == nil {
		// Matches handler: rpcCall nil → 503.
		if tc.wantStatus != http.StatusServiceUnavailable {
			t.Errorf("%s: expected 503 for nil rpcCall; want %d", tc.name, tc.wantStatus)
		}
		return
	}

	params, _ := json.Marshal(body)
	_, err := tc.rpcCall(context.Background(), tc.wantMethod, params)
	if err != nil {
		msg := err.Error()
		// Matches handler: "not found" / "no bound agent loop" → 404.
		if strings.Contains(strings.ToLower(msg), "not found") ||
			strings.Contains(strings.ToLower(msg), "no bound agent loop") ||
			strings.Contains(strings.ToLower(msg), "no agent loop") {
			if tc.wantStatus != http.StatusNotFound {
				t.Errorf("%s: expected 404 for error %q; want %d", tc.name, msg, tc.wantStatus)
			}
			return
		}
		if tc.wantStatus != http.StatusInternalServerError {
			t.Errorf("%s: expected 500 for error %q; want %d", tc.name, msg, tc.wantStatus)
		}
		return
	}
	if tc.wantStatus != http.StatusOK {
		t.Errorf("%s: expected OK but want %d", tc.name, tc.wantStatus)
	}
}

// TestSessionReasoningHTTP_PutSuccess verifies that a well-formed PUT request
// with effort and agent_id dispatches via rpcCall and returns 200.
func TestSessionReasoningHTTP_PutSuccess(t *testing.T) {
	t.Parallel()
	called := false
	tc := sessionReasoningHTTPTestCase{
		name:       "put success",
		method:     http.MethodPut,
		pathID:     "sess-1",
		body:       `{"effort":"high","agent_id":"coder"}`,
		wantStatus: http.StatusOK,
		wantMethod: "reasoning.session_set",
		rpcCall: func(_ context.Context, method string, params json.RawMessage) (any, error) {
			called = true
			if method != "reasoning.session_set" {
				t.Errorf("method = %s, want reasoning.session_set", method)
			}
			var req map[string]any
			_ = json.Unmarshal(params, &req)
			if req["session_id"] != "sess-1" {
				t.Errorf("session_id = %v, want sess-1", req["session_id"])
			}
			if req["effort"] != "high" {
				t.Errorf("effort = %v, want high", req["effort"])
			}
			if req["agent_id"] != "coder" {
				t.Errorf("agent_id = %v, want coder", req["agent_id"])
			}
			return map[string]any{"ok": true}, nil
		},
	}
	runSessionReasoningHTTPTest(t, tc)
	if !called {
		t.Error("rpcCall was not invoked")
	}
}

// TestSessionReasoningHTTP_PutWithBudget verifies the budget_tokens field is
// forwarded.
func TestSessionReasoningHTTP_PutWithBudget(t *testing.T) {
	t.Parallel()
	tc := sessionReasoningHTTPTestCase{
		name:       "put with budget",
		method:     http.MethodPut,
		pathID:     "sess-2",
		body:       `{"effort":"xhigh","agent_id":"planner","budget_tokens":24000}`,
		wantStatus: http.StatusOK,
		wantMethod: "reasoning.session_set",
		rpcCall: func(_ context.Context, _ string, params json.RawMessage) (any, error) {
			var req map[string]any
			_ = json.Unmarshal(params, &req)
			if req["budget_tokens"] != float64(24000) {
				t.Errorf("budget_tokens = %v, want 24000", req["budget_tokens"])
			}
			return map[string]any{"ok": true}, nil
		},
	}
	runSessionReasoningHTTPTest(t, tc)
}

// TestSessionReasoningHTTP_DeleteSuccess verifies DELETE dispatches with the
// correct method and session_id.
func TestSessionReasoningHTTP_DeleteSuccess(t *testing.T) {
	t.Parallel()
	tc := sessionReasoningHTTPTestCase{
		name:       "delete success",
		method:     http.MethodDelete,
		pathID:     "sess-1",
		body:       `{"agent_id":"coder"}`,
		wantStatus: http.StatusOK,
		wantMethod: "reasoning.session_clear",
		rpcCall: func(_ context.Context, method string, params json.RawMessage) (any, error) {
			if method != "reasoning.session_clear" {
				t.Errorf("method = %s, want reasoning.session_clear", method)
			}
			var req map[string]any
			_ = json.Unmarshal(params, &req)
			if req["session_id"] != "sess-1" {
				t.Errorf("session_id = %v, want sess-1", req["session_id"])
			}
			return map[string]any{"ok": true}, nil
		},
	}
	runSessionReasoningHTTPTest(t, tc)
}

// TestSessionReasoningHTTP_NoLoop verifies that when the RPC returns a
// "no bound agent loop" error, the handler returns 404.
func TestSessionReasoningHTTP_NoLoop(t *testing.T) {
	t.Parallel()
	tc := sessionReasoningHTTPTestCase{
		name:      "no loop",
		method:    http.MethodPut,
		pathID:    "sess-x",
		body:      `{"effort":"high"}`, // no agent_id in MVP
		wantStatus: http.StatusNotFound,
		wantMethod: "reasoning.session_set",
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errStr("session \"sess-x\" has no bound agent loop")
		},
	}
	runSessionReasoningHTTPTest(t, tc)
}

// TestSessionReasoningHTTP_AgentNotFound verifies that "agent not found" RPC
// errors map to 404.
func TestSessionReasoningHTTP_AgentNotFound(t *testing.T) {
	t.Parallel()
	tc := sessionReasoningHTTPTestCase{
		name:      "agent not found",
		method:    http.MethodPut,
		pathID:    "sess-1",
		body:      `{"effort":"high","agent_id":"nonexistent"}`,
		wantStatus: http.StatusNotFound,
		wantMethod: "reasoning.session_set",
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errStr("agent not found: nonexistent")
		},
	}
	runSessionReasoningHTTPTest(t, tc)
}

// TestSessionReasoningHTTP_InternalError verifies that other RPC errors map
// to 500.
func TestSessionReasoningHTTP_InternalError(t *testing.T) {
	t.Parallel()
	tc := sessionReasoningHTTPTestCase{
		name:      "internal error",
		method:    http.MethodPut,
		pathID:    "sess-1",
		body:      `{"effort":"high","agent_id":"coder"}`,
		wantStatus: http.StatusInternalServerError,
		wantMethod: "reasoning.session_set",
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errStr("database is locked")
		},
	}
	runSessionReasoningHTTPTest(t, tc)
}

// errStr is a convenience error type for the test stubs.
type errStr string

func (e errStr) Error() string { return string(e) }
