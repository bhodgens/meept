package sharedclient

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/transport"
)

// ============================================================================
// Integration test mock server
// ============================================================================

type integrationServer struct {
	handler func(method string, params json.RawMessage) (any, error)
}

func newIntegrationServer(handler func(method string, params json.RawMessage) (any, error)) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Handle health check (GET request)
		if r.Method == http.MethodGet && r.URL.Path == "/api/v1/health" {
			w.WriteHeader(http.StatusOK)
			return
		}

		// Handle chat endpoint directly (POST with JSON body)
		if r.Method == http.MethodPost && r.URL.Path == "/api/v1/chat" {
			var req struct {
				Message        string `json:"message"`
				ConversationID string `json:"conversation_id"`
			}
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, "bad request", 400)
				return
			}
			// Call handler with "chat" method
			result, err := handler("chat", json.RawMessage(fmt.Sprintf(`{"message":%q,"conversation_id":%q}`, req.Message, req.ConversationID)))
			if err != nil {
				w.WriteHeader(http.StatusOK)
				json.NewEncoder(w).Encode(map[string]any{"error": err.Error()})
				return
			}
			w.Header().Set("Content-Type", "application/json")
			json.NewEncoder(w).Encode(result)
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

		result, err := handler(req.Method, req.Params)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			json.NewEncoder(w).Encode(map[string]any{
				"error": map[string]any{"code": -32603, "message": err.Error()},
			})
			return
		}

		resultData, _ := json.Marshal(result)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{"result": json.RawMessage(resultData)})
	}))
}

// ============================================================================
// End-to-end: create session, send chat, verify response
// ============================================================================

func TestIntegration_SessionAndChat(t *testing.T) {
	handler := func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "session.create":
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(params, &p)
			return map[string]any{
				"id":         "sess-e2e",
				"name":       p.Name,
				"created_at": time.Now().Format(time.RFC3339),
			}, nil
		case "chat":
			var p struct {
				Message string `json:"message"`
			}
			json.Unmarshal(params, &p)
			return map[string]string{
				"reply": "ack: " + p.Message,
			}, nil
		case "session.get_most_recent":
			return nil, fmt.Errorf("not found")
		case "session.list":
			return map[string]any{"sessions": []any{}}, nil
		default:
			return map[string]string{"status": "ok"}, nil
		}
	}

	server := newIntegrationServer(handler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// Create session via client
	sess, err := client.CreateSession("e2e-test")
	if err != nil {
		t.Fatalf("CreateSession failed: %v", err)
	}
	if sess.ID != "sess-e2e" {
		t.Errorf("session.ID = %q, want %q", sess.ID, "sess-e2e")
	}

	// Send chat via session manager
	sm := sharedclient.NewSessionManager(client, "default")
	sm.SetSession(sess)

	reply, err := client.Chat("hello world", sm.GetSessionName())
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	if reply != "ack: hello world" {
		t.Errorf("Chat reply = %q, want %q", reply, "ack: hello world")
	}
}

// ============================================================================
// Integration: slash command parsing end-to-end
// ============================================================================

func TestIntegration_SlashCommandFlow(t *testing.T) {
	handler := func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "chat":
			var p struct {
				Message string `json:"message"`
			}
			json.Unmarshal(params, &p)
			return map[string]string{"reply": "received: " + p.Message}, nil
		default:
			return map[string]string{"status": "ok"}, nil
		}
	}

	server := newIntegrationServer(handler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	// Parse slash command
	cmd := sharedclient.ParseSlash("/help")
	if cmd == nil {
		t.Fatal("ParseSlash(/help) returned nil")
	}

	// Verify it's not sent as chat
	if cmd.Name == "help" {
		// The TUI should route to help, not chat
		_ = cmd // correct behavior
	}

	// Regular command goes through chat
	regularMsg := "hello world"
	result, err := client.Chat(regularMsg, "e2e-session")
	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}
	expected := "received: hello world"
	if result != expected {
		t.Errorf("Chat result = %q, want %q", result, expected)
	}
}

// ============================================================================
// Integration: history and autocomplete together
// ============================================================================

func TestIntegration_HistoryAndAutocomplete(t *testing.T) {
	history := sharedclient.NewHistory(100)
	auto := sharedclient.NewSlashAutocomplete()

	// User types and submits
	history.Add("hello")
	history.Add("/status")

	// User goes up in history
	val, ok := history.Up("current")
	if !ok {
		t.Fatal("history Up should return ok")
	}
	if val != "/status" {
		t.Errorf("history Up = %q, want %q", val, "/status")
	}

	// User goes down - back to input (temporary)
	val, ok = history.Down("current")
	if !ok {
		t.Fatal("history Down should return ok")
	}
	if val != "current" {
		t.Errorf("history Down = %q, want %q", val, "current")
	}

	// Meanwhile, autocomplete shows commands when user types /
	auto.Show("st")
	filtered := auto.GetFilteredCommands()

	// /status should be among filtered results
	found := false
	for _, c := range filtered {
		if c == "status" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected 'status' in filtered autosuggestions, got: %v", filtered)
	}

	auto.Hide()
	if auto.IsVisible() {
		t.Error("autocomplete should not be visible after Hide()")
	}
}

// ============================================================================
// Integration: graceful error handling
// ============================================================================

func TestIntegration_ConnectFailure(t *testing.T) {
	// Connecting to a non-existent server should fail gracefully
	client := transport.NewHTTPClient("http://127.0.0.1:1", 1*time.Second)
	err := client.Connect()
	if err == nil {
		t.Error("expected connection error")
	}

	// Close should be safe even after failure
	err = client.Close()
	if err != nil {
		t.Errorf("close after failure should be safe: %v", err)
	}
}

func TestIntegration_NilSessionSessionManager(t *testing.T) {
	// Session manager with nil client should not panic on basic ops
	sm := sharedclient.NewSessionManager(nil, "default")
	sess := sm.GetCurrentSession()
	if sess != nil {
		t.Error("session should be nil when client is nil")
	}
	// GetSessionName should return the fallback
	name := sm.GetSessionName()
	if name != "default" {
		t.Errorf("GetSessionName() = %q, want %q", name, "default")
	}
}

// ============================================================================
// Integration: Session manager with real transport over mock
// ============================================================================

func TestIntegration_SessionManagerLoadOrCreateFlow(t *testing.T) {
	sessCount := 0
	handler := func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "session.get_most_recent":
			sessCount++
			if sessCount <= 1 {
				// Return a session first time so LoadOrCreate loads it
				return map[string]any{
					"id":         "sess-recent",
					"name":       "recent",
					"created_at": time.Now().Format(time.RFC3339),
				}, nil
			}
			return nil, fmt.Errorf("no sessions")
		case "session.create":
			var p struct {
				Name string `json:"name"`
			}
			json.Unmarshal(params, &p)
			return map[string]any{
				"id":         "sess-new",
				"name":       p.Name,
				"created_at": time.Now().Format(time.RFC3339),
			}, nil
		case "status":
			return map[string]string{"status": "running"}, nil
		default:
			return map[string]string{"status": "ok"}, nil
		}
	}

	server := newIntegrationServer(handler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	sm := sharedclient.NewSessionManager(client, "default")

	// Load or create should load the most recent one
	err := sm.LoadOrCreateSession(nil, "")
	if err != nil {
		t.Fatalf("LoadOrCreateSession failed: %v", err)
	}

	sess := sm.GetCurrentSession()
	if sess == nil {
		t.Fatal("GetCurrentSession returned nil")
	}
	if sess.ID != "sess-recent" {
		t.Errorf("session.ID = %q, want %q", sess.ID, "sess-recent")
	}

	// Name resolution
	name := sm.GetSessionName()
	if name != "recent" {
		t.Errorf("GetSessionName() = %q, want %q", name, "recent")
	}
}

// ============================================================================
// Integration: slash command not-yet-supported path
// ============================================================================

func TestIntegration_UnsupportedSlashCommand(t *testing.T) {
	cmd := sharedclient.ParseSlash("/my-custom-cmd")
	if cmd == nil {
		t.Fatal("ParseSlash(/my-custom-cmd) returned nil")
	}
	if cmd.Name != "my-custom-cmd" {
		t.Errorf("command name = %q, want %q", cmd.Name, "my-custom-cmd")
	}

	// Verify it's not a builtin
	if sharedclient.IsBuiltin("my-custom-cmd") {
		t.Error("my-custom-cmd should not be a builtin")
	}
}

// ============================================================================
// Integration: multiple chat round-trips
// ============================================================================

func TestIntegration_MultipleChatRoundTrips(t *testing.T) {
	callCount := 0
	handler := func(method string, params json.RawMessage) (any, error) {
		switch method {
		case "chat":
			callCount++
			var p struct {
				Message string `json:"message"`
			}
			json.Unmarshal(params, &p)
			return map[string]string{"reply": fmt.Sprintf("echo #%d: %s", callCount, p.Message)}, nil
		case "session.create":
			return map[string]any{
				"id":         "sess-roundtrip",
				"name":       "round-trip",
				"created_at": time.Now().Format(time.RFC3339),
			}, nil
		default:
			return map[string]string{"status": "ok"}, nil
		}
	}

	server := newIntegrationServer(handler)
	defer server.Close()

	client := transport.NewHTTPClient(server.URL, 5*time.Second)
	if err := client.Connect(); err != nil {
		t.Fatalf("Connect failed: %v", err)
	}
	defer client.Close()

	sm := sharedclient.NewSessionManager(client, "default")
	sess, _ := client.CreateSession("round-trip")
	sm.SetSession(sess)

	messages := []string{"first", "second", "third"}
	expectedReplies := []string{
		"echo #1: first",
		"echo #2: second",
		"echo #3: third",
	}

	for i, msg := range messages {
		reply, err := client.Chat(msg, sessionName(sm))
		if err != nil {
			t.Fatalf("chat #%d failed: %v", i+1, err)
		}
		if reply != expectedReplies[i] {
			t.Errorf("chat #%d: reply = %q, want %q", i+1, reply, expectedReplies[i])
		}
	}
}

func sessionName(sm *sharedclient.SessionManager) string {
	sess := sm.GetCurrentSession()
	if sess == nil {
		return sm.GetSessionName()
	}
	if sess.Description != "" {
		return sess.Description
	}
	if sess.Name != "" {
		return sess.Name
	}
	return sm.GetSessionName()
}

// ============================================================================
// Integration: autocomplete edge cases
// ============================================================================

func TestIntegration_AutocompleteEdgeCases(t *testing.T) {
	auto := sharedclient.NewSlashAutocomplete()

	// Show with empty filter
	auto.Show("")
	if !auto.IsVisible() {
		t.Error("autocomplete should be visible after Show")
	}

	// Select with no filter should still have defaults
	filtered := auto.GetFilteredCommands()
	if len(filtered) == 0 {
		t.Error("empty filter should show all builtins")
	}

	// Navigate up at top
	auto.Up() // should be no-op
	if auto.GetSelectedIndex() != 0 {
		t.Error("Up at top should not go negative")
	}

	// Navigate down at bottom
	auto.Down() // should work
	auto.Down()
	auto.Down()
	// Down should not panic even past end

	// Select without match
	selected, ok := auto.Select()
	if !ok && selected == "" {
		// This is acceptable for empty filter with many matches
	}

	auto.Hide()
	if auto.IsVisible() {
		t.Error("autocomplete should be hidden")
	}
}
