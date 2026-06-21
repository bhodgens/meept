package rpc

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
)

// TestReasoningSessionSet_NoRegistry verifies the RPC handler returns a
// "registry not available" error when constructed with a nil registry.
func TestReasoningSessionSet_NoRegistry(t *testing.T) {
	t.Parallel()
	h := NewReasoningHandler(nil, nil, "", nil, "")

	params := mustMarshal(t, map[string]any{
		"session_id": "sess-1",
		"agent_id":   "coder",
		"effort":     "high",
	})
	_, err := h.handleSessionSet(context.Background(), params)
	if err == nil {
		t.Fatal("expected error from nil registry")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error = %v, want 'not available'", err)
	}
}

// TestReasoningSessionClear_NoRegistry mirrors the above for the clear path.
func TestReasoningSessionClear_NoRegistry(t *testing.T) {
	t.Parallel()
	h := NewReasoningHandler(nil, nil, "", nil, "")

	params := mustMarshal(t, map[string]any{
		"session_id": "sess-1",
		"agent_id":   "coder",
	})
	_, err := h.handleSessionClear(context.Background(), params)
	if err == nil {
		t.Fatal("expected error from nil registry")
	}
	if !strings.Contains(err.Error(), "not available") {
		t.Errorf("error = %v, want 'not available'", err)
	}
}

// TestReasoningSessionSet_MissingSessionID verifies session_id is required.
func TestReasoningSessionSet_MissingSessionID(t *testing.T) {
	t.Parallel()
	// Construct a registry so we get past the nil check and hit the
	// session_id validation.
	registry := agent.NewAgentRegistry(agent.RegistryConfig{})
	h := NewReasoningHandler(registry, nil, "", nil, "")

	params := mustMarshal(t, map[string]any{
		"agent_id": "coder",
		"effort":   "high",
	})
	_, err := h.handleSessionSet(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when session_id is missing")
	}
	if !strings.Contains(err.Error(), "session_id") {
		t.Errorf("error = %v, want mention of session_id", err)
	}
}

// TestReasoningSessionSet_MissingAgentID verifies the MVP requirement that
// agent_id is required (since no session→loop map exists yet).
func TestReasoningSessionSet_MissingAgentID(t *testing.T) {
	t.Parallel()
	registry := agent.NewAgentRegistry(agent.RegistryConfig{})
	h := NewReasoningHandler(registry, nil, "", nil, "")

	params := mustMarshal(t, map[string]any{
		"session_id": "sess-1",
		"effort":     "high",
	})
	_, err := h.handleSessionSet(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when agent_id is missing (MVP limitation)")
	}
	if !strings.Contains(err.Error(), "no bound agent loop") {
		t.Errorf("error = %v, want 'no bound agent loop'", err)
	}
}

// TestReasoningSessionSet_InvalidEffort verifies effort tier validation.
func TestReasoningSessionSet_InvalidEffort(t *testing.T) {
	t.Parallel()
	registry := agent.NewAgentRegistry(agent.RegistryConfig{})
	h := NewReasoningHandler(registry, nil, "", nil, "")

	params := mustMarshal(t, map[string]any{
		"session_id": "sess-1",
		"agent_id":   "coder",
		"effort":     "turbo", // not a valid tier
	})
	_, err := h.handleSessionSet(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for invalid effort tier")
	}
	if !strings.Contains(err.Error(), "invalid effort tier") {
		t.Errorf("error = %v, want 'invalid effort tier'", err)
	}
}

// TestReasoningSessionSet_BadParams verifies malformed JSON is rejected.
func TestReasoningSessionSet_BadParams(t *testing.T) {
	t.Parallel()
	registry := agent.NewAgentRegistry(agent.RegistryConfig{})
	h := NewReasoningHandler(registry, nil, "", nil, "")

	_, err := h.handleSessionSet(context.Background(), json.RawMessage(`{bad json`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "invalid params") {
		t.Errorf("error = %v, want 'invalid params'", err)
	}
}

// TestReasoningSessionSet_AgentNotFound verifies that a non-existent agent_id
// returns an "agent not found" error (registry.Get fails).
func TestReasoningSessionSet_AgentNotFound(t *testing.T) {
	t.Parallel()
	registry := agent.NewAgentRegistry(agent.RegistryConfig{})
	h := NewReasoningHandler(registry, nil, "", nil, "")

	params := mustMarshal(t, map[string]any{
		"session_id": "sess-1",
		"agent_id":   "nonexistent-agent",
		"effort":     "high",
	})
	_, err := h.handleSessionSet(context.Background(), params)
	if err == nil {
		t.Fatal("expected error for unknown agent_id")
	}
	if !strings.Contains(err.Error(), "agent not found") {
		t.Errorf("error = %v, want 'agent not found'", err)
	}
}

// TestReasoningSessionClear_MissingAgentID verifies the clear path also
// requires agent_id in the MVP.
func TestReasoningSessionClear_MissingAgentID(t *testing.T) {
	t.Parallel()
	registry := agent.NewAgentRegistry(agent.RegistryConfig{})
	h := NewReasoningHandler(registry, nil, "", nil, "")

	params := mustMarshal(t, map[string]any{
		"session_id": "sess-1",
	})
	_, err := h.handleSessionClear(context.Background(), params)
	if err == nil {
		t.Fatal("expected error when agent_id is missing for clear")
	}
	if !strings.Contains(err.Error(), "no bound agent loop") {
		t.Errorf("error = %v, want 'no bound agent loop'", err)
	}
}

// TestReasoningSessionClear_BadParams verifies malformed JSON is rejected.
func TestReasoningSessionClear_BadParams(t *testing.T) {
	t.Parallel()
	registry := agent.NewAgentRegistry(agent.RegistryConfig{})
	h := NewReasoningHandler(registry, nil, "", nil, "")

	_, err := h.handleSessionClear(context.Background(), json.RawMessage(`{broken`))
	if err == nil {
		t.Fatal("expected error for malformed JSON")
	}
	if !strings.Contains(err.Error(), "invalid params") {
		t.Errorf("error = %v, want 'invalid params'", err)
	}
}

// TestReasoningSessionRegistration verifies that RegisterReasoningMethods
// actually registers both new methods on the RPC server.
func TestReasoningSessionRegistration(t *testing.T) {
	t.Parallel()
	srv := New(&Config{}, nil, nil)
	h := NewReasoningHandler(nil, nil, "", nil, "")
	h.RegisterReasoningMethods(srv)

	// Both methods should be registered and dispatch to the handler (which
	// will return "registry not available" since we passed nil).
	for _, method := range []string{"reasoning.session_set", "reasoning.session_clear"} {
		_, err := srv.CallMethod(context.Background(), method, json.RawMessage(`{}`))
		if err == nil {
			t.Fatalf("%s: expected error from nil registry", method)
		}
		if !strings.Contains(err.Error(), "not available") {
			t.Errorf("%s: error = %v, want 'not available'", method, err)
		}
	}
}

// mustMarshal is a test helper that marshals v or fails the test.
func mustMarshal(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}
