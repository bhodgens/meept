package employee

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/auth"
	commhttp "github.com/caimlas/meept/internal/comm/http"
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
		return nil, nil //nolint:nilnil // test double: the callback is asserted to never be invoked
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

// issue76NamedKey builds a users store with one named account and one raw
// key. The returned user id is the store identity, never the synthetic id
// "user". The raw key is authentication material: callers must not log it.
func issue76NamedKey(t *testing.T, name string) (*auth.Store, string, string) {
	t.Helper()
	store, err := auth.NewStore(filepath.Join(t.TempDir(), "users.json5"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	user, err := store.AddUser(name)
	if err != nil {
		t.Fatalf("AddUser: %v", err)
	}
	if user.ID == "" || user.ID == "user" {
		t.Fatalf("store user id = %q, want a named identity", user.ID)
	}
	raw, err := store.AddKey(user.ID, "laptop", nil)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	return store, user.ID, raw
}

func issue76Dispatch(t *testing.T, h *AgentAPIHandler, rawKey string, middleware bool, store *auth.Store) *httptest.ResponseRecorder {
	t.Helper()
	mux := http.NewServeMux()
	h.RegisterRoutes(mux)
	var handler http.Handler = mux
	if middleware {
		handler = commhttp.NewStoreAuth(store).Middleware(mux)
	}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/agents", nil)
	if rawKey != "" {
		req.Header.Set("Authorization", "Bearer "+rawKey)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestAgentHTTPCallerStoreIdentityNotUser pins issue 76: a named multi-user
// key is attributed to the auth-store user id, not the synthetic id "user".
func TestAgentHTTPCallerStoreIdentityNotUser(t *testing.T) {
	t.Parallel()
	store, userID, raw := issue76NamedKey(t, "ada")

	var gotID string
	var hasCaller bool
	h := NewAgentAPIHandler(func(ctx context.Context, method string, _ json.RawMessage) (any, error) {
		if method != "agents.list" {
			t.Errorf("method = %q, want agents.list", method)
		}
		gotID, hasCaller = callerUserID(ctx)
		return map[string]any{"agents": []any{}}, nil
	})
	h.SetAuthStore(store)

	rec := issue76Dispatch(t, h, raw, false, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), raw) {
		t.Fatal("response contains the raw api key")
	}
	if !hasCaller || gotID != userID {
		t.Fatalf("caller UserID = %q (present=%v), want store id %q", gotID, hasCaller, userID)
	}
	if gotID == "user" {
		t.Fatal("named key attributed to synthetic user")
	}
}

// TestAgentHTTPCallerContextIdentityNotUser pins the request-context path:
// StoreAuth already validated the key; the handler must use that identity
// even when it has no store of its own.
func TestAgentHTTPCallerContextIdentityNotUser(t *testing.T) {
	t.Parallel()
	store, userID, raw := issue76NamedKey(t, "ada")

	var gotID string
	var hasCaller bool
	h := NewAgentAPIHandler(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		gotID, hasCaller = callerUserID(ctx)
		return map[string]any{"agents": []any{}}, nil
	})

	rec := issue76Dispatch(t, h, raw, true, store)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	if strings.Contains(rec.Body.String(), raw) {
		t.Fatal("response contains the raw api key")
	}
	if !hasCaller || gotID != userID || gotID == "user" {
		t.Fatalf("caller UserID = %q (present=%v), want store id %q", gotID, hasCaller, userID)
	}
}

// TestAgentHTTPCallerUnknownKeyFailsClosed: a key the store does not match
// is denied before RPC dispatch. The synthetic id "user" is not invented,
// and the response does not echo the key.
func TestAgentHTTPCallerUnknownKeyFailsClosed(t *testing.T) {
	t.Parallel()
	store, _, _ := issue76NamedKey(t, "ada")
	const unknown = "issue76-unknown-key-sentinel"

	called := false
	h := NewAgentAPIHandler(func(context.Context, string, json.RawMessage) (any, error) {
		called = true
		return map[string]any{}, nil
	})
	h.SetAuthStore(store)

	rec := issue76Dispatch(t, h, unknown, false, nil)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
	if called {
		t.Fatal("rpc dispatched for an unresolved key")
	}
	body := rec.Body.String()
	if strings.Contains(body, unknown) {
		t.Fatal("error response contains the raw api key")
	}
	if strings.Contains(body, `"user"`) {
		t.Fatal("unresolved key attributed to synthetic user")
	}
}

// TestAgentHTTPCallerNoStoreDoesNotInventUser: legacy flat-key mode has no
// users store. A presented key must not become the synthetic id "user".
func TestAgentHTTPCallerNoStoreDoesNotInventUser(t *testing.T) {
	t.Parallel()
	var gotID string
	var hasCaller bool
	h := NewAgentAPIHandler(func(ctx context.Context, _ string, _ json.RawMessage) (any, error) {
		gotID, hasCaller = callerUserID(ctx)
		return map[string]any{"agents": []any{}}, nil
	})

	rec := issue76Dispatch(t, h, "legacy-flat-key", false, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (legacy dispatch stays open)", rec.Code)
	}
	if hasCaller || gotID == "user" {
		t.Fatalf("caller = %q present=%v, want no synthetic user", gotID, hasCaller)
	}
}

func callerUserID(ctx context.Context) (string, bool) {
	info := CallerFromContext(ctx)
	if info == nil {
		return "", false
	}
	return info.UserID, true
}
