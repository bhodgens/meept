package rpc

// groups_fsroots_test.go — owner ruling D1 feature pins: the groups.assign
// / groups.remove RPC lane must be able to AMEND a policy's additive
// fs_roots (planner/orchestrator = groups admin amends fence roots for
// other agents' policies), persisting through the same atomic config save
// as bindings and passing the SAME C5 write gate. Non-vacuity: the amend
// lane did not exist pre-feature — an fs_roots field on the request was
// silently ignored (no struct field existed), so every assertion below
// (roots in memory, roots on disk, gate denial, remove lane) is new
// behavior that cannot pass vacuously.

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/config"
)

func newFSRootsHandler(t *testing.T) (*GroupsHandler, string) {
	t.Helper()
	cfg, cfgPath := groupsTestConfig(t)
	cfg.Security.Groups.Policies = append(cfg.Security.Groups.Policies,
		config.GroupsPolicyConfig{Name: "sandboxed", FSRoots: []string{"/srv/data"}})
	h := NewGroupsHandler(cfg, cfgPath, nil, nil, nil)
	h.SetToolValidation(map[string]struct{}{"file_read": {}, "shell": {}}, nil)
	return h, cfgPath
}

func readPersistedPolicyRoots(t *testing.T, cfgPath, policyName string) []string {
	t.Helper()
	data, err := os.ReadFile(cfgPath)
	if err != nil {
		t.Fatalf("read persisted config: %v", err)
	}
	var doc struct {
		Security struct {
			Groups struct {
				Policies []struct {
					Name    string   `json:"name"`
					FSRoots []string `json:"fs_roots"`
				} `json:"policies"`
			} `json:"groups"`
		} `json:"security"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("decode persisted config: %v (raw=%s)", err, data)
	}
	for _, p := range doc.Security.Groups.Policies {
		if p.Name == policyName {
			return p.FSRoots
		}
	}
	return nil
}

func policyRoots(h *GroupsHandler, name string) []string {
	for _, p := range h.cfg.Security.Groups.Policies {
		if p.Name == name {
			return p.FSRoots
		}
	}
	return nil
}

func TestGroupsAssignAmendsFSRoots(t *testing.T) {
	h, cfgPath := newFSRootsHandler(t)

	// Admin agent assigns: add a root to the named policy AND bind it.
	res, err := h.handleAssign(peerCtx(true), mustParams(t, map[string]any{
		"agent_id":    "agent-update",
		"subject":     "agent:coder",
		"policy_name": "sandboxed",
		"fs_roots":    []string{"/srv/cache", "/srv/data"}, // dup: pre-existing
	}))
	if err != nil {
		t.Fatalf("assign with fs_roots: %v", err)
	}
	if res == nil {
		t.Fatal("nil result")
	}
	// In-memory policy: unioned + deduped, original order preserved.
	if got := strings.Join(policyRoots(h, "sandboxed"), ","); got != "/srv/data,/srv/cache" {
		t.Fatalf("policy fs_roots = %v, want [/srv/data /srv/cache] (deduped union)", policyRoots(h, "sandboxed"))
	}
	// Binding lane still ran (assign both amends and binds).
	var bound bool
	for _, b := range h.cfg.Security.Groups.Bindings {
		if b.Subject == "agent:coder" && b.Policy == "sandboxed" {
			bound = true
		}
	}
	if !bound {
		t.Fatal("fs_roots assign must also create the binding")
	}
	// Persisted to disk (the RPC able to persist fs_roots, per ruling).
	persisted := readPersistedPolicyRoots(t, cfgPath, "sandboxed")
	if strings.Join(persisted, ",") != "/srv/data,/srv/cache" {
		t.Fatalf("persisted fs_roots = %v, want [/srv/data /srv/cache]", persisted)
	}
}

func TestGroupsAssignPolicyObjectCarriesFSRoots(t *testing.T) {
	h, _ := newFSRootsHandler(t)
	// A full policy OBJECT plus request fs_roots: roots merge into the
	// upserted object (one save), no separate amendment needed.
	if _, err := h.handleAssign(peerCtx(true), mustParams(t, map[string]any{
		"agent_id": "agent-update",
		"subject":  "agent:planner",
		"policy": map[string]any{
			"name":          "sandboxed",
			"tools_allowed": []string{"file_read"},
			"fs_roots":      []string{"/srv/data"},
		},
		"fs_roots": []string{"/srv/spill"},
	})); err != nil {
		t.Fatalf("assign policy object + fs_roots: %v", err)
	}
	if got := strings.Join(policyRoots(h, "sandboxed"), ","); got != "/srv/data,/srv/spill" {
		t.Fatalf("upserted fs_roots = %v, want [/srv/data /srv/spill]", policyRoots(h, "sandboxed"))
	}
}

func TestGroupsRemoveDeletesOnlyFSRoots(t *testing.T) {
	h, cfgPath := newFSRootsHandler(t)

	if _, err := h.handleAssign(peerCtx(true), mustParams(t, map[string]any{
		"agent_id":    "agent-update",
		"subject":     "agent:coder",
		"policy_name": "sandboxed",
		"fs_roots":    []string{"/srv/cache"},
	})); err != nil {
		t.Fatalf("seed assign: %v", err)
	}
	// Remove ONE root: binding untouched, other roots intact.
	if _, err := h.handleRemove(peerCtx(true), mustParams(t, map[string]any{
		"agent_id":    "agent-update",
		"subject":     "agent:coder",
		"policy_name": "sandboxed",
		"fs_roots":    []string{"/srv/cache"},
	})); err != nil {
		t.Fatalf("remove fs_roots: %v", err)
	}
	if got := strings.Join(policyRoots(h, "sandboxed"), ","); got != "/srv/data" {
		t.Fatalf("after remove, fs_roots = %v, want [/srv/data]", policyRoots(h, "sandboxed"))
	}
	var bound bool
	for _, b := range h.cfg.Security.Groups.Bindings {
		if b.Subject == "agent:coder" && b.Policy == "sandboxed" {
			bound = true
		}
	}
	if !bound {
		t.Fatal("fs_roots removal must NOT drop the binding")
	}
	persisted := readPersistedPolicyRoots(t, cfgPath, "sandboxed")
	if strings.Join(persisted, ",") != "/srv/data" {
		t.Fatalf("persisted roots = %v, want [/srv/data]", persisted)
	}
}

func TestGroupsFSRootsAmendGateAndValidation(t *testing.T) {
	h, _ := newFSRootsHandler(t)

	// C5 gate: a non-admin agent caller cannot amend roots (same denial
	// model as bindings — the new lane sits behind resolveCaller).
	if _, err := h.handleAssign(peerCtx(true), mustParams(t, map[string]any{
		"agent_id":    "intruder",
		"subject":     "agent:coder",
		"policy_name": "sandboxed",
		"fs_roots":    []string{"/etc"},
	})); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("non-admin fs_roots amend: err = %v, want C5 denial", err)
	}
	// Denied mutate must not have touched the policy.
	for _, r := range policyRoots(h, "sandboxed") {
		if r == "/etc" {
			t.Fatal("denied amend still mutated the policy")
		}
	}
	// Human admin via store identity works (gate shape (b)).
	if _, err := h.handleAssign(ContextWithCallerIdentity(context.Background(), &auth.Identity{
		UserID: "boss-user", KeyID: "k-1", Role: "human",
	}), mustParams(t, map[string]any{
		"subject":     "agent:coder",
		"policy_name": "sandboxed",
		"fs_roots":    []string{"/srv/human"},
	})); err != nil {
		t.Fatalf("admin-user amend: %v", err)
	}
	// Unknown policy: error, nothing mutated (define-first rule).
	if _, err := h.handleAssign(peerCtx(true), mustParams(t, map[string]any{
		"agent_id":    "agent-update",
		"subject":     "agent:coder",
		"policy_name": "ghost",
		"fs_roots":    []string{"/srv/x"},
	})); err == nil || !strings.Contains(err.Error(), "does not exist") {
		t.Fatalf("unknown-policy amend error = %v, want does-not-exist", err)
	}
	// Bare fs_roots with no policy_name errors (roots live on the policy).
	if _, err := h.handleAssign(peerCtx(true), mustParams(t, map[string]any{
		"agent_id": "agent-update",
		"subject":  "agent:coder",
		"fs_roots": []string{"/srv/x"},
	})); err == nil || !strings.Contains(err.Error(), "policy_name") {
		t.Fatalf("fs_roots without policy_name: err = %v, want policy_name-required", err)
	}
}
