package http

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIntakeHandlers_NilRPCCall pins the 503 posture when the RPC seam is
// absent (same shape as TestActivityHandlers_NilRPCCall).
func TestIntakeHandlers_NilRPCCall(t *testing.T) {
	s := &Server{} // s.rpcCall is nil by default

	req := httptest.NewRequest(http.MethodGet, "/api/v1/intake/status", nil)
	w := httptest.NewRecorder()
	s.handleIntakeStatus(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("nil rpcCall: status = %d, want 503", w.Code)
	}
	if !strings.Contains(w.Body.String(), "intake service not available") {
		t.Errorf("nil rpcCall: body = %q, want 'intake service not available'", w.Body.String())
	}
}

// TestIntakeHandlers_ProxiesMethod pins the bridge contract: the handler
// calls exactly intake.status with an empty-params envelope and echoes the
// RPC result verbatim (the frozen cross-surface field names ride through
// untouched — GUI parity depends on it).
func TestIntakeHandlers_ProxiesMethod(t *testing.T) {
	fake := &fakeRPCCall{Result: struct {
		Enabled bool   `json:"enabled"`
		Note    string `json:"note"`
	}{Enabled: false, Note: "intake disabled (default); no poll job is scheduled"}}
	s := &Server{rpcCall: fake.Call}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/intake/status", nil)
	w := httptest.NewRecorder()
	s.handleIntakeStatus(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("proxy: status = %d, want 200 (body: %s)", w.Code, w.Body.String())
	}
	if fake.Method != "intake.status" {
		t.Errorf("proxy: rpcCall method = %q, want %q", fake.Method, "intake.status")
	}
	if string(fake.Params) != "{}" {
		t.Errorf("proxy: params = %q, want %q", string(fake.Params), "{}")
	}
	for _, want := range []string{`"enabled"`, `"note"`, "intake disabled"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("proxy: body %q missing %q", w.Body.String(), want)
		}
	}
}

// TestIntakeHandlers_RPCCallError pins the 503->lowered-error path: an RPC
// failure surfaces as a 500 with the lowercased message (same posture as
// the activity handlers).
func TestIntakeHandlers_RPCCallError(t *testing.T) {
	fake := &fakeRPCCall{Err: errors.New("Intake Status Boom")}
	s := &Server{rpcCall: fake.Call}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/intake/status", nil)
	w := httptest.NewRecorder()
	s.handleIntakeStatus(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("rpc error: status = %d, want 500", w.Code)
	}
	body := w.Body.String()
	if body != strings.ToLower(body) || !strings.Contains(body, "error") {
		t.Errorf("rpc error: body = %q, want lowercase error message", body)
	}
}
