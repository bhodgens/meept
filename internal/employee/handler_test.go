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

// TestRPCHandler_Update_RejectsConstitution verifies S5: update rejects
// constitution fields with a message directing to `meept agents amend`.
func TestRPCHandler_Update_RejectsConstitution(t *testing.T) {
	t.Parallel()
	m := NewManager(nil)
	h := NewRPCHandler(m)

	cases := []struct {
		name    string
		payload string
	}{
		{
			name:    "with_constitution_block",
			payload: `{"id":"x","name":"n","constitution":{"purpose":"new"}}`,
		},
		{
			name:    "with_constraints_block",
			payload: `{"id":"x","constitution":{"constraints":{"risk_ceiling":"high"}}}`,
		},
		{
			name:    "with_escalates_to",
			payload: `{"id":"x","constitution":{"escalates_to":"user"}}`,
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, err := h.handleUpdate(context.Background(), json.RawMessage(tc.payload))
			if err == nil {
				t.Fatal("expected error for constitution field in update, got nil")
			}
			// Verify the error message references amend.
			if !containsSubstring(err.Error(), "amend") {
				t.Errorf("error should mention 'amend', got: %v", err)
			}
		})
	}
}

// containsSubstring is a simple string contains check for test assertions.
func containsSubstring(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// TestConstitutionFields_CoversCoreFields verifies the canonical constitution
// field set includes all spec-required fields.
func TestConstitutionFields_CoversCoreFields(t *testing.T) {
	t.Parallel()
	required := []string{
		"purpose", "role", "charter", "autonomy_tier", "escalates_to",
		"constraints", "amendment_policy", "version", "authored_by",
		"tools_allowed", "tools_forbidden", "risk_ceiling",
		"max_tokens_per_turn", "max_conversation_tokens",
		"daily_budget_cents", "max_invocations_per_day",
		"escalation_triggers", "never", "assessment_interval",
		"frozen_fields", "self_propose_allowed", "requires_approval",
	}
	for _, f := range required {
		if _, ok := constitutionFields[f]; !ok {
			t.Errorf("constitutionFields missing %q", f)
		}
	}
}
