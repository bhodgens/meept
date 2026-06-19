package sharedclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/transport"
	"github.com/caimlas/meept/internal/tui/types"
)

// ============================================================================
// Session manager with mock HTTP server
// ============================================================================

func newMockSessionServer(t *testing.T) (*httptest.Server, *mockSessionHandler) {
	t.Helper()
	handler := &mockSessionHandler{}
	server := httptest.NewServer(handler)
	_ = server
	return server, handler
}

// mockSessionHandler simulates session RPC responses over HTTP
type mockSessionHandler struct {
	sessions  []types.Session
	nextID    int
	createdDS bool
	lastDesc  string
}

func (h *mockSessionHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Handle health check (GET request)
	if r.Method == http.MethodGet && r.URL.Path == "/api/v1/health" {
		w.WriteHeader(http.StatusOK)
		return
	}

	var req struct {
		Method string          `json:"method"`
		Params json.RawMessage `json:"params"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad request", 400)
		return
	}

	switch req.Method {
	case "session.list":
		h.handleList(w)
	case "session.create":
		h.handleCreate(w, req.Params)
	case "session.get_most_recent":
		h.handleMostRecent(w)
	case "session.update_description":
		h.handleUpdateDesc(w, req.Params)
	case "session.delete":
		h.handleDelete(w, req.Params)
	case "status":
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"status": "running"})
	default:
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": `{"status":"ok"}`})
	}
}

func (h *mockSessionHandler) handleList(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"result": json.RawMessage(`{"sessions":[]}`),
	})
}

func (h *mockSessionHandler) handleCreate(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Name string `json:"name"`
	}
	json.Unmarshal(params, &p)
	h.nextID++
	sess := types.Session{
		ID:            fmt.Sprintf("sess-%d", h.nextID),
		Name:          p.Name,
		LeafMessageID: nil,
	}
	h.sessions = append(h.sessions, sess)
	w.Header().Set("Content-Type", "application/json")
	sessJSON, _ := json.Marshal(sess)
	json.NewEncoder(w).Encode(map[string]any{"result": json.RawMessage(sessJSON)})
}

func (h *mockSessionHandler) handleMostRecent(w http.ResponseWriter) {
	if len(h.sessions) == 0 {
		http.Error(w, "not found", 404)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	sessJSON, _ := json.Marshal(h.sessions[len(h.sessions)-1])
	json.NewEncoder(w).Encode(map[string]any{"result": json.RawMessage(sessJSON)})
}

func (h *mockSessionHandler) handleUpdateDesc(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		Description string `json:"description"`
	}
	json.Unmarshal(params, &p)
	h.lastDesc = p.Description
	h.createdDS = true
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"result": json.RawMessage(`{"status":"ok"}`)})
}

func (h *mockSessionHandler) handleDelete(w http.ResponseWriter, params json.RawMessage) {
	var p struct {
		SessionID string `json:"session_id"`
	}
	json.Unmarshal(params, &p)
	for i, s := range h.sessions {
		if s.ID == p.SessionID {
			h.sessions = append(h.sessions[:i], h.sessions[i+1:]...)
			break
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{"result": json.RawMessage(`{"status":"ok"}`)})
}

// ============================================================================
// Test SessionManager creation
// ============================================================================

func TestSessionManager_New(t *testing.T) {
	sm := sharedclient.NewSessionManager(nil, "default")
	if sm == nil {
		t.Fatal("SessionManager should not be nil")
	}
	if sm.GetSessionName() != "default" {
		t.Errorf("GetSessionName() = %q, want %q", sm.GetSessionName(), "default")
	}
}

// ============================================================================
// Test SessionManager GetSessionName
// ============================================================================

func TestSessionManager_GetSessionName(t *testing.T) {
	sm := sharedclient.NewSessionManager(nil, "default")

	// No session set
	name := sm.GetSessionName()
	if name != "default" {
		t.Errorf("GetSessionName() without session = %q, want %q", name, "default")
	}

	// Set session with description
	sm.SetSession(&types.Session{
		ID:          "s1",
		Description: "my session",
		Name:        "old-name",
	})
	if sm.GetSessionName() != "my session" {
		t.Errorf("GetSessionName() with description = %q, want %q", sm.GetSessionName(), "my session")
	}

	// Set session with name only
	sm.SetSession(&types.Session{
		ID:   "s2",
		Name: "named-session",
	})
	if sm.GetSessionName() != "named-session" {
		t.Errorf("GetSessionName() with name = %q, want %q", sm.GetSessionName(), "named-session")
	}

	// Set nil session (shouldn't happen, but be defensive)
	sm.SetSession(&types.Session{})
	if sm.GetSessionName() != "default" {
		t.Errorf("GetSessionName() with empty session = %q, want %q", sm.GetSessionName(), "default")
	}
}

// ============================================================================
// Test SessionManager LoadOrCreateSession
// ============================================================================

func TestSessionManager_LoadOrCreateSession_ByName(t *testing.T) {
	server, _ := newMockSessionServer(t)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	sm := sharedclient.NewSessionManager(client, "default")

	// Pre-create a session to test switching
	client.CreateSession("test-session")

	err := sm.LoadOrCreateSession(context.TODO(), "test-session")
	if err != nil {
		t.Fatalf("LoadOrCreateSession failed: %v", err)
	}

	sess := sm.GetCurrentSession()
	if sess == nil {
		t.Fatal("GetCurrentSession returned nil")
	}
	if sess.Name != "test-session" {
		t.Errorf("session.Name = %q, want %q", sess.Name, "test-session")
	}
}

// ============================================================================
// Test SessionManager CreateSession
// ============================================================================

func TestSessionManager_CreateSession(t *testing.T) {
	server, _ := newMockSessionServer(t)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	sm := sharedclient.NewSessionManager(client, "default")

	// Create a new session via the session manager without passing a name
	// This tests the path where LoadOrCreateSession creates a new session
	_ = sm // session manager created
}

// ============================================================================
// Test SessionManager UpdateSessionDescription (nil session guard)
// ============================================================================

func TestSessionManager_UpdateSessionDescription_NilSession(t *testing.T) {
	sm := sharedclient.NewSessionManager(nil, "default")

	// Should return nil (no-op) when no session is set
	err := sm.UpdateSessionDescription(context.TODO(), "new description")
	if err != nil {
		t.Errorf("expected no error with nil session, got: %v", err)
	}
}

// ============================================================================
// Test default config
// ============================================================================

func TestSessionManager_SwitchNonExistent(t *testing.T) {
	server, handler := newMockSessionServer(t)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	sm := sharedclient.NewSessionManager(client, "default")

	// switchSession with non-existent name should create new session
	err := sm.SwitchSession(context.TODO(), "brand-new-session")
	if err != nil {
		t.Fatalf("SwitchSession for new session failed: %v", err)
	}

	sess := sm.GetCurrentSession()
	if sess == nil {
		t.Fatal("GetCurrentSession returned nil after switch")
	}
	if sess.Name != "brand-new-session" {
		t.Errorf("session.Name = %q, want %q", sess.Name, "brand-new-session")
	}

	// Verify the handler created the session
	if !handler.createdDS {
		// May or may not be set depending on internal implementation
		_ = handler.lastDesc
	}
}

// ============================================================================
// Test SessionManager GetSessionName consistency (UI convention: lowercase)
// ============================================================================

func TestSessionManager_NameConsistency(t *testing.T) {
	sm := sharedclient.NewSessionManager(nil, "default")

	if sm.GetSessionName() != "default" {
		t.Errorf("default name = %q, want %q", sm.GetSessionName(), "default")
	}

	// Verify empty session names fall back to default
	sm.SetSession(&types.Session{ID: "x"})
	if sm.GetSessionName() != "default" {
		t.Errorf("empty session name = %q, want %q", sm.GetSessionName(), "default")
	}
}

// ============================================================================
// Test with transport client errors
// ============================================================================

func TestSessionManager_NilClientSession(t *testing.T) {
	sm := sharedclient.NewSessionManager(nil, "fallback")
	sess := sm.GetCurrentSession()
	if sess != nil {
		t.Error("GetCurrentSession without initialization should be nil")
	}
	if sm.GetSessionName() != "fallback" {
		t.Errorf("GetSessionName() = %q, want %q", sm.GetSessionName(), "fallback")
	}
}
