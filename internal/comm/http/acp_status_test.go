package http

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type acpListEnvelope struct {
	Enabled bool               `json:"enabled" yaml:"enabled"`
	Agents  []acpAgentEnvelope `json:"agents" yaml:"agents"`
}

type acpAgentEnvelope struct {
	ID      string `json:"id" yaml:"id"`
	Enabled bool   `json:"enabled" yaml:"enabled"`
	Running bool   `json:"running" yaml:"running"`
	State   string `json:"state" yaml:"state"`
	UptimeS int    `json:"uptime_s" yaml:"uptime_s"`
}

func TestACPAgentsList_NilRPCCallReturnsDisabledEnvelope(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/agents", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPAgentsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got acpListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if got.Agents == nil {
		t.Fatal("agents = nil, want empty slice")
	}
	if len(got.Agents) != 0 {
		t.Fatalf("agents len = %d, want 0", len(got.Agents))
	}
}

func TestACPAgentsList_FakeRPCCallReturnsEnvelope(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			if method != "acp.list" {
				t.Errorf("rpc method = %q, want acp.list", method)
			}
			return acpListEnvelope{
				Enabled: true,
				Agents: []acpAgentEnvelope{
					{
						ID:      "codex",
						Enabled: true,
						Running: true,
						State:   "ready",
						UptimeS: 0,
					},
				},
			}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/agents", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPAgentsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got acpListEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled {
		t.Fatal("enabled = false, want true")
	}
	if len(got.Agents) != 1 {
		t.Fatalf("agents len = %d, want 1", len(got.Agents))
	}
	a := got.Agents[0]
	if a.ID != "codex" || !a.Enabled || !a.Running || a.State != "ready" || a.UptimeS != 0 {
		t.Errorf("agent = %+v", a)
	}
}

func TestACPAgentsList_RPCErrorLowercase(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errors.New("ACP Not Available")
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/agents", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPAgentsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode: %v", err)
	}
	msg := body["error"]
	if msg == "" {
		t.Fatal("missing error field")
	}
	if msg != strings.ToLower(msg) {
		t.Errorf("error %q is not lowercase", msg)
	}
}

// ---------------------------------------------------------------------------
// GET /api/v1/acp/permissions + POST /api/v1/acp/permissions/resolve
// (acp-human-permission-relay leaf 03, Contract C — acp_status.go rpcCall
// pattern).
// ---------------------------------------------------------------------------

type acpPermissionsEnvelope struct {
	Enabled bool            `json:"enabled"`
	Pending []acpPendingRow `json:"pending"`
}

func TestACPACPPermissionsList_NilRPCCallReturnsDisabledEnvelope(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/permissions", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPPermissionsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got acpPermissionsEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if got.Pending == nil {
		t.Fatal("pending = nil, want empty slice")
	}
	if len(got.Pending) != 0 {
		t.Fatalf("pending len = %d, want 0", len(got.Pending))
	}
}

func TestACPACPPermissionsList_FakeRPCCallReturnsEnvelope(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, method string, _ json.RawMessage) (any, error) {
			if method != "acp.permissions.list" {
				t.Errorf("rpc method = %q, want acp.permissions.list", method)
			}
			return acpPermissionsEnvelope{
				Enabled: true,
				Pending: []acpPendingRow{{
					ID:        "perm-1",
					AgentID:   "codex",
					SessionID: "sess-a",
					Summary:   "run: rm -rf build",
					CreatedAt: "2026-10-19T00:00:00Z",
				}},
			}, nil
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/permissions", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPPermissionsList(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var got acpPermissionsEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if !got.Enabled || len(got.Pending) != 1 {
		t.Fatalf("envelope = %+v", got)
	}
	row := got.Pending[0]
	if row.ID != "perm-1" || row.AgentID != "codex" || row.SessionID != "sess-a" {
		t.Errorf("row = %+v", row)
	}
}

func TestACPACPPermissionsList_RPCError(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errors.New("boom")
		},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/acp/permissions", http.NoBody)
	w := httptest.NewRecorder()

	s.handleACPPermissionsList(w, req)

	if resp := w.Result(); resp.StatusCode != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", resp.StatusCode)
	}
}

func TestACPPermissionsResolve_NilRPCCall(t *testing.T) {
	s := &Server{}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/acp/permissions/resolve",
		strings.NewReader(`{"id":"perm-1","decision":"allow"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	s.handleACPPermissionsResolve(w, req)

	if resp := w.Result(); resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503", resp.StatusCode)
	}
}

func TestACPPermissionsResolve_BadBody(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			t.Error("rpcCall must not fire for a malformed body")
			return nil, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/acp/permissions/resolve",
		strings.NewReader(`{"decision":"allow"}`))
	w := httptest.NewRecorder()

	s.handleACPPermissionsResolve(w, req)

	if resp := w.Result(); resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400 (missing id)", resp.StatusCode)
	}
}

func TestACPPermissionsResolve_DecodesAndForwards(t *testing.T) {
	var gotMethod string
	var gotParams json.RawMessage
	s := &Server{
		rpcCall: func(_ context.Context, method string, params json.RawMessage) (any, error) {
			gotMethod = method
			gotParams = params
			return map[string]any{"id": "perm-1", "decision": "deny", "reason": "answered"}, nil
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/acp/permissions/resolve",
		strings.NewReader(`{"id":"perm-1","decision":"deny"}`))
	w := httptest.NewRecorder()

	s.handleACPPermissionsResolve(w, req)

	resp := w.Result()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if gotMethod != "acp.permissions.resolve" {
		t.Errorf("method = %q, want acp.permissions.resolve", gotMethod)
	}
	var forwarded map[string]string
	if err := json.Unmarshal(gotParams, &forwarded); err != nil {
		t.Fatalf("params decode: %v (%s)", err, gotParams)
	}
	if forwarded["id"] != "perm-1" || forwarded["decision"] != "deny" {
		t.Errorf("forwarded params = %v", forwarded)
	}
	var echoed map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&echoed); err != nil {
		t.Fatalf("response decode: %v", err)
	}
	if echoed["reason"] != "answered" {
		t.Errorf("echo = %v, want reason answered", echoed)
	}
}

func TestACPPermissionsResolve_RPCError(t *testing.T) {
	s := &Server{
		rpcCall: func(_ context.Context, _ string, _ json.RawMessage) (any, error) {
			return nil, errors.New("permission not found")
		},
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/acp/permissions/resolve",
		strings.NewReader(`{"id":"perm-x","decision":"allow"}`))
	w := httptest.NewRecorder()

	s.handleACPPermissionsResolve(w, req)

	if resp := w.Result(); resp.StatusCode == http.StatusOK {
		t.Fatal("RPC error surfaced as 200")
	}
}
