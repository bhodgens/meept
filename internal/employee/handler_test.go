package employee

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
)

// TestRPCHandler_NilManager exercises the nil-manager guard on every handler.
// Each must return errNotConfigured, never panic.
func TestRPCHandler_NilManager(t *testing.T) {
	t.Parallel()

	h := NewRPCHandler(nil)
	handlers := h.Handlers()

	cases := []struct {
		name    string
		method  string
		payload string
	}{
		{"list", "agents.list", `{}`},
		{"get", "agents.get", `{"id":"x"}`},
		{"create", "agents.create", `{"id":"x","name":"n","prompt":"p","constitution":{}}`},
		{"update", "agents.update", `{"id":"x"}`},
		{"delete", "agents.delete", `{"id":"x"}`},
		{"pause", "agents.pause", `{"id":"x"}`},
		{"resume", "agents.resume", `{"id":"x"}`},
		{"trigger", "agents.trigger", `{"id":"x"}`},
		{"amend", "agents.amend", `{"id":"x","fields":{"role":"new"}}`},
		{"goals.list", "agents.goals.list", `{}`},
		{"goals.get", "agents.goals.get", `{"id":"g1"}`},
		{"goals.approve", "agents.goals.approve", `{"goal_id":"g","plan_id":"p"}`},
		{"goals.reject", "agents.goals.reject", `{"goal_id":"g","plan_id":"p","reason":"no"}`},
		{"audit.list", "agents.audit.list", `{"employee_id":"x"}`},
		{"audit.resolve", "agents.audit.resolve", `{"finding_id":"f","resolution":"false_positive"}`},
		{"migrate", "agents.migrate", `{}`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fn, ok := handlers[tc.method]
			if !ok {
				t.Fatalf("method %q not registered", tc.method)
			}
			_, err := fn(context.Background(), json.RawMessage(tc.payload))
			if !errors.Is(err, errNotConfigured) {
				t.Fatalf("method %s with nil manager: want errNotConfigured, got %v", tc.method, err)
			}
		})
	}
}

// TestRPCHandler_SetLogger_NilGuard verifies the setter nil guard.
func TestRPCHandler_SetLogger_NilGuard(t *testing.T) {
	t.Parallel()
	h := NewRPCHandler(nil)
	h.SetLogger(nil) // must not panic
}

// TestRPCHandler_Handlers_Registration verifies every spec-required method is
// present in the Handlers map (spec lines 532-540).
func TestRPCHandler_Handlers_Registration(t *testing.T) {
	t.Parallel()
	h := NewRPCHandler(nil)
	got := h.Handlers()

	required := []string{
		// lifecycle (spec line 533)
		"agents.list", "agents.get", "agents.create", "agents.update", "agents.delete",
		// runtime (spec line 534)
		"agents.pause", "agents.resume",
		// trigger (spec line 535)
		"agents.trigger",
		// amend (spec line 536)
		"agents.amend",
		// goals (spec lines 537-538)
		"agents.goals.list", "agents.goals.get",
		"agents.goals.approve", "agents.goals.reject",
		// audit (spec line 539)
		"agents.audit.list", "agents.audit.resolve",
		// migration (spec line 540)
		"agents.migrate",
	}

	for _, m := range required {
		if _, ok := got[m]; !ok {
			t.Errorf("missing required handler %q", m)
		}
	}
	if len(got) != len(required) {
		t.Errorf("handler count mismatch: want %d, got %d (extra or missing handlers)", len(required), len(got))
	}
}

// TestRPCHandler_Get_MissingID checks request validation.
func TestRPCHandler_Get_MissingID(t *testing.T) {
	t.Parallel()
	// Use a non-nil manager so we reach the ID check (not the nil guard).
	m := NewManager(nil)
	h := NewRPCHandler(m)
	_, err := h.handleGet(context.Background(), json.RawMessage(`{}`))
	if err == nil {
		t.Fatal("expected error for missing id, got nil")
	}
}

// TestRPCHandler_AuditResolve_InvalidResolution checks enum validation.
func TestRPCHandler_AuditResolve_InvalidResolution(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	h := NewRPCHandler(m)
	_, err := h.handleAuditResolve(
		context.Background(),
		json.RawMessage(`{"finding_id":"f","resolution":"bogus"}`),
	)
	if err == nil {
		t.Fatal("expected error for invalid resolution")
	}
}
