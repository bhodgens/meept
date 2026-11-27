package rpc

import (
	"encoding/json"
	"fmt"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestGroupsAssignRemoveRace drives the assign/remove handler path
// concurrently (multiple goroutines) under go test -race. The mutation
// path is a single unexported helper (applyPolicyBinding) called from
// both RPC paths and the test, so the RPC behavior is unchanged.
func TestGroupsAssignRemoveRace(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			Groups: config.SecurityGroupsConfig{
				AdminAgents: []string{"agent-update"},
				Policies: []config.GroupsPolicyConfig{
					{
						Name:         "policy1",
						ToolsAllowed: []string{"tool1"},
					},
				},
				Bindings: []config.GroupsBindingConfig{
					{Subject: "agent:1", Policy: "policy1"},
				},
			},
		},
	}
	// cfgPath empty keeps the mutation in-memory only (no file I/O in
	// the race test — the race is about the in-memory slice, not disk).
	h := NewGroupsHandler(cfg, "", nil, nil, nil)
	ctx := peerCtx(true) // daemon-descendant peer so the C5 gate lets the mutation through

	// Race 1: concurrent assign/remove (different subjects so both
	// lanes actually mutate the bindings slice).
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < 100; j++ {
				// Assign a unique subject each time so the append
				// lane always runs (not the duplicate no-op).
				subj := fmt.Sprintf("agent:%d-%d", i, j)
				_, _ = h.handleAssign(ctx, json.RawMessage(`{"subject": "`+subj+`", "policy_name": "policy1", "agent_id": "agent-update"}`))
				_, _ = h.handleRemove(ctx, json.RawMessage(`{"subject": "`+subj+`", "policy_name": "policy1", "agent_id": "agent-update"}`))
			}
		}(i)
	}
	wg.Wait()

	// After the race, the bindings slice must be consistent: every
	// entry has a subject and a policy, no duplicates, and the original
	// binding (agent:1/policy1) is still present (no binding was
	// removed for it during the race).
	h.mu.Lock()
	g := h.cfg.Security.Groups
	h.mu.Unlock()

	seen := make(map[string]struct{})
	for _, b := range g.Bindings {
		if b.Subject == "" || b.Policy == "" {
			t.Fatalf("binding with empty subject or policy: %+v", b)
		}
		key := b.Subject + "/" + b.Policy
		if _, dup := seen[key]; dup {
			t.Fatalf("duplicate binding in live config: %s", key)
		}
		seen[key] = struct{}{}
	}
	if _, ok := seen["agent:1/policy1"]; !ok {
		t.Fatalf("original binding agent:1/policy1 missing after race; bindings=%+v", g.Bindings)
	}
}

// TestGroupsAssignRemovePayload checks that the assign/remove success
// payloads carry effective_at_restart=true and the note explaining that
// existing sessions keep boot-time enforcement until restart.
func TestGroupsAssignRemovePayload(t *testing.T) {
	cfg := &config.Config{
		Security: config.SecurityConfig{
			Groups: config.SecurityGroupsConfig{
				AdminAgents: []string{"agent-update"},
				Policies: []config.GroupsPolicyConfig{
					{Name: "policy1", ToolsAllowed: []string{"tool1"}},
				},
				Bindings: []config.GroupsBindingConfig{
					{Subject: "agent:1", Policy: "policy1"},
				},
			},
		},
	}
	h := NewGroupsHandler(cfg, "", nil, nil, nil)
	ctx := peerCtx(true) // daemon-descendant peer so the C5 gate lets the mutation through

	// Assign: new subject, existing policy.
	res, err := h.handleAssign(ctx, json.RawMessage(`{"subject": "agent:2", "policy_name": "policy1", "agent_id": "agent-update"}`))
	if err != nil {
		t.Fatalf("handleAssign unexpected error: %v", err)
	}
	m, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("handleAssign result is not a map: %T", res)
	}
	if m["assigned"] != true {
		t.Errorf("expected assigned=true, got %v", m["assigned"])
	}
	if m["effective_at_restart"] != true {
		t.Errorf("expected effective_at_restart=true, got %v", m["effective_at_restart"])
	}
	if m["note"] != "existing sessions keep boot-time enforcement until restart" {
		t.Errorf("unexpected note: %v", m["note"])
	}

	// Remove: same subject.
	res, err = h.handleRemove(ctx, json.RawMessage(`{"subject": "agent:2", "policy_name": "policy1", "agent_id": "agent-update"}`))
	if err != nil {
		t.Fatalf("handleRemove unexpected error: %v", err)
	}
	m, ok = res.(map[string]any)
	if !ok {
		t.Fatalf("handleRemove result is not a map: %T", res)
	}
	if m["removed"] != true {
		t.Errorf("expected removed=true, got %v", m["removed"])
	}
	if m["effective_at_restart"] != true {
		t.Errorf("expected effective_at_restart=true, got %v", m["effective_at_restart"])
	}
	if m["note"] != "existing sessions keep boot-time enforcement until restart" {
		t.Errorf("unexpected note: %v", m["note"])
	}
}
