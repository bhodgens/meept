package rpc

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/auditlog"
	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/groups"
)

// groupsTestConfig builds a handler config with one policy and the C5
// admin tables the gate tests vary.
func groupsTestConfig(t *testing.T) (*config.Config, string) {
	t.Helper()
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "meept.json5")
	cfg := config.DefaultConfig()
	cfg.Security.Groups.AdminAgents = []string{"agent-update"}
	cfg.Security.Groups.AdminUsers = []string{"boss-user"}
	cfg.Security.Groups.Policies = []config.GroupsPolicyConfig{
		{Name: "coders", ToolsAllowed: []string{"file_read"}},
	}
	cfgPathData := []byte("{}")
	if err := os.WriteFile(cfgPath, cfgPathData, 0o600); err != nil {
		t.Fatalf("seed config: %v", err)
	}
	return cfg, cfgPath
}

func newGateHandler(t *testing.T) (*GroupsHandler, *auditlog.Store, string) {
	t.Helper()
	cfg, cfgPath := groupsTestConfig(t)
	audit, err := auditlog.OpenStore(filepath.Join(t.TempDir(), "audit.db"), nil)
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	t.Cleanup(func() { _ = audit.Close() })
	h := NewGroupsHandler(cfg, cfgPath, nil, nil, audit)
	h.SetToolValidation(map[string]struct{}{"file_read": {}, "shell": {}}, nil)
	return h, audit, cfgPath
}

func mustParams(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatalf("marshal params: %v", err)
	}
	return b
}

// peerCtx fakes a kernel-verified socket peer.
func peerCtx(daemonDescendant bool) context.Context {
	return context.WithValue(context.Background(), peerIdentityKey{}, PeerIdentity{
		PID:             4242,
		HasPID:          true,
		SpawnedByDaemon: daemonDescendant,
	})
}

// T2 PASS state: privileged => applied + audited; non-privileged agent +
// plain user => denied with reason; unknown subject => error.
func TestGroupsMutationGate(t *testing.T) {
	h, audit, _ := newGateHandler(t)

	tests := []struct {
		name    string
		ctx     context.Context
		params  map[string]any
		wantErr string // empty = expect success
	}{
		{
			name: "admin agent via daemon-descendant peer is allowed",
			ctx:  peerCtx(true),
			params: map[string]any{
				"agent_id": "agent-update",
				"subject":  "agent:coder",
				"policy":   map[string]any{"name": "coders"},
			},
		},
		{
			name: "admin human via store identity is allowed",
			ctx: ContextWithCallerIdentity(context.Background(), &auth.Identity{
				UserID: "boss-user", KeyID: "k-1", Role: "human",
			}),
			params: map[string]any{
				"subject":     "agent:coder",
				"policy_name": "coders",
			},
		},
		{
			name: "non-admin agent (daemon descendant, self-declared, not in admin_agents) is denied",
			ctx:  peerCtx(true),
			params: map[string]any{
				"agent_id":    "aastub",
				"policy_name": "coders",
			},
			wantErr: "denied",
		},
		{
			name: "self-declared agent id from a NON-daemon peer is denied (issue #76 class)",
			ctx:  peerCtx(false),
			params: map[string]any{
				"agent_id":    "agent-update",
				"policy_name": "coders",
			},
			wantErr: "denied",
		},
		{
			name: "plain user (store identity not in admin_users) is denied",
			ctx: ContextWithCallerIdentity(context.Background(), &auth.Identity{
				UserID: "plain-user", KeyID: "k-2",
			}),
			params: map[string]any{
				"subject":     "agent:coder",
				"policy_name": "coders",
			},
			wantErr: "denied",
		},
		{
			name: "no identity at all is denied (fail closed)",
			ctx:  context.Background(),
			params: map[string]any{
				"subject":     "agent:coder",
				"policy_name": "coders",
			},
			wantErr: "denied",
		},
		{
			name: "unknown subject kind errors",
			ctx:  peerCtx(true),
			params: map[string]any{
				"subject":     "robot:x",
				"policy_name": "coders",
			},
			wantErr: "must be agent:<id>",
		},
		{
			name: "unknown policy on assign errors",
			ctx:  peerCtx(true),
			params: map[string]any{
				"agent_id":    "agent-update",
				"subject":     "agent:coder",
				"policy_name": "no-such-policy",
			},
			wantErr: "does not exist",
		},
		{
			name: "admin identity without a daemon peer cannot claim agent privilege",
			ctx:  ContextWithCallerIdentity(context.Background(), &auth.Identity{UserID: "plain-user"}),
			params: map[string]any{
				"agent_id":    "agent-update",
				"policy_name": "coders",
			},
			wantErr: "denied",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := h.handleAssign(tt.ctx, mustParams(t, tt.params))
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("assign: unexpected error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("assign: expected error containing %q, got success", tt.wantErr)
			}
			if !strings.Contains(strings.ToLower(err.Error()), tt.wantErr) {
				t.Fatalf("assign: error %q does not contain %q", err, tt.wantErr)
			}
		})
	}

	// Successful mutations audited: the first case landed one record.
	recs, err := audit.List(context.Background(), 10)
	if err != nil {
		t.Fatalf("audit list: %v", err)
	}
	found := false
	for _, r := range recs {
		if r.Type == "groups_membership_change" {
			found = true
		}
	}
	if !found {
		t.Fatalf("successful assign landed no audit record; records: %d", len(recs))
	}

	// remove is gated identically: unprivileged caller denied.
	if _, err := h.handleRemove(context.Background(), mustParams(t, map[string]any{
		"subject": "agent:coder", "policy_name": "coders",
	})); err == nil || !strings.Contains(err.Error(), "denied") {
		t.Fatalf("remove by unprivileged caller: want deny, got %v", err)
	}
	// ...and privileged remove succeeds.
	if _, err := h.handleRemove(peerCtx(true), mustParams(t, map[string]any{
		"agent_id": "agent-update",
		"subject":  "agent:coder", "policy_name": "coders",
	})); err != nil {
		t.Fatalf("privileged remove: %v", err)
	}
}

// T4 PASS state: a policy with an unregistered tool is rejected BEFORE the
// gate persists anything — and the rejection names the offending tool.
func TestGroupsAssignValidatesPolicyToolsBeforePersist(t *testing.T) {
	h, _, cfgPath := newGateHandler(t)
	ctx := peerCtx(true) // privileged caller so the gate is not the rejector

	_, err := h.handleAssign(ctx, mustParams(t, map[string]any{
		"subject": "agent:coder",
		"policy":  map[string]any{"name": "probe", "tools_allowed": []string{"file_read", "definitely_not_a_tool"}},
	}))
	if err == nil || !strings.Contains(err.Error(), "definitely_not_a_tool") {
		t.Fatalf("assign: want validation error naming the unregistered tool, got %v", err)
	}

	// Nothing persisted: the config file still carries no probe policy.
	cfg, err := config.Load(cfgPath)
	if err != nil {
		t.Fatalf("reload config: %v", err)
	}
	for _, p := range cfg.Security.Groups.Policies {
		if p.Name == "probe" {
			t.Fatalf("rejected policy was persisted anyway")
		}
	}
}

// T1 PASS state: list returns policies+bindings; explain returns the
// resolution with per-binding sources.
func TestGroupsListAndExplain(t *testing.T) {
	h, _, _ := newGateHandler(t)

	// Bind agent:coder -> coders, then list + explain it.
	if _, err := h.handleAssign(peerCtx(true), mustParams(t, map[string]any{
		"agent_id": "agent-update",
		"subject":  "agent:coder", "policy_name": "coders",
	})); err != nil {
		t.Fatalf("assign for explain: %v", err)
	}
	lr, err := h.handleList(context.Background(), mustParams(t, map[string]any{}))
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	list := lr.(map[string]any)
	if len(listPol(list)) == 0 {
		t.Fatalf("list: policies missing: %v", list["policies"])
	}
	if len(listBind(list)) == 0 {
		t.Fatalf("list: bindings empty")
	}
	xr, err := h.handleExplain(context.Background(), mustParams(t, map[string]any{"agent_id": "coder"}))
	if err != nil {
		t.Fatalf("explain: %v", err)
	}
	ex := xr.(map[string]any)
	if srcs, ok := ex["bindings"].([]map[string]any); !ok || len(srcs) == 0 {
		t.Fatalf("explain: no binding sources: %v", ex["bindings"])
	}
	if _, ok := ex["resolved"]; !ok {
		t.Fatalf("explain: resolved record missing")
	}
}

// tiny shape helpers keeping the assertions readable without importing
// encoding/json plumbing into every branch.
func listPol(m map[string]any) []groups.Policy {
	raw, _ := m["policies"].([]groups.Policy)
	return raw
}

func listBind(m map[string]any) []groups.Binding {
	raw, _ := m["bindings"].([]groups.Binding)
	return raw
}

var _ = agent.CanonicalToolName // referenced via SetToolValidation wiring in daemon; keep import honest in unit builds
