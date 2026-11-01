package http

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRPCCall is a fake rpcCall seam for testing.
type fakeRPCCall struct {
	Method string
	Params []byte
	Result any
	Err    error
}

func (f *fakeRPCCall) Call(ctx context.Context, method string, params json.RawMessage) (any, error) {
	f.Method = method
	f.Params = []byte(params)
	return f.Result, f.Err
}

func TestActivityHandlers_NilRPCCall(t *testing.T) {
	s := &Server{}
	// s.rpcCall is nil by default

	tests := []struct {
		name    string
		handler func(http.ResponseWriter, *http.Request)
		status  int
		bodySub string
	}{
		{"activity query nil", s.handleActivityQuery, http.StatusServiceUnavailable, "activity service not available"},
		{"activity recent nil", s.handleActivityRecent, http.StatusServiceUnavailable, "activity service not available"},
		{"activity overview nil", s.handleActivityOverview, http.StatusServiceUnavailable, "activity service not available"},
		{"activity findings nil", s.handleActivityFindings, http.StatusServiceUnavailable, "activity service not available"},
		{"agents review nil", s.handleAgentsReview, http.StatusServiceUnavailable, "activity service not available"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/activity", nil)
			w := httptest.NewRecorder()
			tt.handler(w, req)

			if w.Code != tt.status {
				t.Errorf("%s: status = %d, want %d", tt.name, w.Code, tt.status)
			}
			if !strings.Contains(w.Body.String(), tt.bodySub) {
				t.Errorf("%s: body = %q, want substring %q", tt.name, w.Body.String(), tt.bodySub)
			}
		})
	}
}

func TestActivityHandlers_BadSort(t *testing.T) {
	fake := &fakeRPCCall{}
	s := &Server{rpcCall: fake.Call}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/activity?sort=invalid", nil)
	w := httptest.NewRecorder()
	s.handleActivityQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("bad sort: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	if !strings.Contains(w.Body.String(), "invalid sort value") {
		t.Errorf("bad sort: body = %q, want 'invalid sort value'", w.Body.String())
	}
	// rpcCall should NOT be invoked for bad sort
	if fake.Method != "" {
		t.Errorf("bad sort: rpcCall was invoked with method=%s, want not invoked", fake.Method)
	}
}

func TestActivityHandlers_InvalidParams(t *testing.T) {
	fake := &fakeRPCCall{}
	s := &Server{rpcCall: fake.Call}

	// Invalid limit parameter
	req := httptest.NewRequest(http.MethodGet, "/api/v1/activity?limit=abc", nil)
	w := httptest.NewRecorder()
	s.handleActivityQuery(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("invalid limit: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestActivityHandlers_RPCDelegation(t *testing.T) {
	// Test activity.query
	t.Run("activity.query", func(t *testing.T) {
		fake := &fakeRPCCall{
			Result: map[string]any{"records": []any{}, "total": 0},
		}
		s := &Server{rpcCall: fake.Call}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/activity?sort=chrono&limit=10", nil)
		w := httptest.NewRecorder()
		s.handleActivityQuery(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if fake.Method != "activity.query" {
			t.Errorf("method = %s, want 'activity.query'", fake.Method)
		}
		var params activityHandlerParams
		if err := json.Unmarshal(fake.Params, &params); err != nil {
			t.Fatalf("params JSON unmarshal error: %v", err)
		}
		if params.Sort != "chrono" {
			t.Errorf("sort = %s, want 'chrono'", params.Sort)
		}
		if params.Limit != 10 {
			t.Errorf("limit = %d, want 10", params.Limit)
		}
	})

	// Test activity.recent
	t.Run("activity.recent", func(t *testing.T) {
		fake := &fakeRPCCall{
			Result: map[string]any{"records": []any{}, "dropped": 0},
		}
		s := &Server{rpcCall: fake.Call}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/activity/recent?limit=20", nil)
		w := httptest.NewRecorder()
		s.handleActivityRecent(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if fake.Method != "activity.recent" {
			t.Errorf("method = %s, want 'activity.recent'", fake.Method)
		}
		var params struct {
			Limit int `json:"limit"`
		}
		if err := json.Unmarshal(fake.Params, &params); err != nil {
			t.Fatalf("params JSON unmarshal error: %v", err)
		}
		if params.Limit != 20 {
			t.Errorf("limit = %d, want 20", params.Limit)
		}
	})

	// Test activity.overview
	t.Run("activity.overview", func(t *testing.T) {
		fake := &fakeRPCCall{
			Result: map[string]any{"by_agent": []any{}, "generated_at": "2026-10-31T00:00:00Z"},
		}
		s := &Server{rpcCall: fake.Call}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/activity/overview", nil)
		w := httptest.NewRecorder()
		s.handleActivityOverview(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if fake.Method != "activity.overview" {
			t.Errorf("method = %s, want 'activity.overview'", fake.Method)
		}
	})

	// Test activity.findings
	t.Run("activity.findings", func(t *testing.T) {
		fake := &fakeRPCCall{
			Result: map[string]any{"findings": []any{}, "total": 0},
		}
		s := &Server{rpcCall: fake.Call}

		req := httptest.NewRequest(http.MethodGet, "/api/v1/activity/findings?employee_id=emp1&severity=high", nil)
		w := httptest.NewRecorder()
		s.handleActivityFindings(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if fake.Method != "activity.findings" {
			t.Errorf("method = %s, want 'activity.findings'", fake.Method)
		}
		var params struct {
			EmployeeID string `json:"employee_id"`
			Severity   string `json:"severity"`
		}
		if err := json.Unmarshal(fake.Params, &params); err != nil {
			t.Fatalf("params JSON unmarshal error: %v", err)
		}
		if params.EmployeeID != "emp1" {
			t.Errorf("employee_id = %s, want 'emp1'", params.EmployeeID)
		}
		if params.Severity != "high" {
			t.Errorf("severity = %s, want 'high'", params.Severity)
		}
	})

	// Test agents.review - note: PathValue requires mux routing.
	// We test the handler logic by verifying it calls the RPC with the right method.
	// For PathValue to work, we need to route through a mux.
	t.Run("agents.review", func(t *testing.T) {
		fake := &fakeRPCCall{
			Result: map[string]any{"drift_score": 0.5, "employee_id": "emp1"},
		}
		s := &Server{rpcCall: fake.Call}

		// Use a mux to extract path value
		mux := http.NewServeMux()
		mux.HandleFunc("GET /api/v1/agents/{id}/review", s.handleAgentsReview)

		req := httptest.NewRequest(http.MethodGet, "/api/v1/agents/emp1/review", nil)
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
		}
		if fake.Method != "agents.review" {
			t.Errorf("method = %s, want 'agents.review'", fake.Method)
		}
		var params struct {
			EmployeeID string `json:"employee_id"`
		}
		if err := json.Unmarshal(fake.Params, &params); err != nil {
			t.Fatalf("params JSON unmarshal error: %v", err)
		}
		if params.EmployeeID != "emp1" {
			t.Errorf("employee_id = %s, want 'emp1'", params.EmployeeID)
		}
	})
}
