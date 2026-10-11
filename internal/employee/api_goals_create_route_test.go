package employee

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestGoalsCreateRouteRegistered mirrors api_gate_route_test.go: the POST
// route must exist and 503 when the RPC callback is nil (leaf 01 Task 5).
func TestGoalsCreateRouteRegistered(t *testing.T) {
	t.Parallel()
	h := NewAgentAPIHandler(nil)
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/emp1/goals", strings.NewReader(`{"title":"t","mandate":"m"}`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code == http.StatusNotFound {
		t.Fatal("POST /api/v1/agents/{id}/goals is not registered")
	}
	// rpc is nil → 503 service unavailable, which still proves the route exists.
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want 503 (nil rpc)", rec.Code)
	}
}

// TestGoalsCreateRouteDispatch exercises the full HTTP → RPC dispatch path:
// the path employee ID must override any body employee_id (C4).
func TestGoalsCreateRouteDispatch(t *testing.T) {
	t.Parallel()

	var gotMethod string
	var gotParams GoalSpec
	h := NewAgentAPIHandler(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		gotMethod = method
		if err := json.Unmarshal(params, &gotParams); err != nil {
			return nil, err
		}
		return map[string]any{"goal": map[string]any{"id": "goal_x", "health": "unknown"}}, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	// Body claims a different employee; the path must win.
	body := `{"employee_id":"body-emp","title":"http goal","mandate":"m"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/path-emp/goals", strings.NewReader(body))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body.String())
	}
	if gotMethod != "agents.goals.create" {
		t.Errorf("dispatched method = %q, want agents.goals.create", gotMethod)
	}
	if gotParams.EmployeeID != "path-emp" {
		t.Errorf("employee_id = %q, want path-emp (path overrides body)", gotParams.EmployeeID)
	}
	if gotParams.Title != "http goal" {
		t.Errorf("title = %q, want http goal", gotParams.Title)
	}
}

// TestGoalsCreateRouteBadJSON checks the 400 path for malformed bodies.
func TestGoalsCreateRouteBadJSON(t *testing.T) {
	t.Parallel()
	h := NewAgentAPIHandler(func(ctx context.Context, method string, params json.RawMessage) (any, error) {
		t.Error("rpc callback must not be called for malformed JSON")
		return nil, nil
	})
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/agents/e/goals", strings.NewReader(`{not json`))
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
