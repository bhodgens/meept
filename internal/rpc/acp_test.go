package rpc

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/config"
)

func TestACPHandler_List_NilManager(t *testing.T) {
	h := NewACPHandler(nil)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.list nil manager: %v", err)
	}
	got := mustACPList(t, res)
	if got.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if got.Agents == nil {
		t.Fatal("agents = nil, want empty slice")
	}
	if len(got.Agents) != 0 {
		t.Fatalf("agents len = %d, want 0", len(got.Agents))
	}
}

func TestACPHandler_List_DisabledManager(t *testing.T) {
	cfg := config.ACPConfig{
		Enabled:        false,
		DialTimeout:    10,
		CallTimeout:    120,
		MaxAgents:      3,
		PermissionMode: "permissive",
	}
	catalog := &config.ACPAgentsConfig{
		Agents: []config.ACPAgentEntry{
			{ID: "codex", Enabled: true},
		},
	}
	mgr := acp.NewManager(cfg, catalog)
	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.list disabled manager: %v", err)
	}
	got := mustACPList(t, res)
	if got.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if len(got.Agents) != 0 {
		t.Fatalf("agents len = %d, want 0", len(got.Agents))
	}
}

func TestACPHandler_List_EnabledCatalog(t *testing.T) {
	cfg := config.ACPConfig{
		Enabled:        true,
		DialTimeout:    10,
		CallTimeout:    120,
		MaxAgents:      3,
		PermissionMode: "permissive",
	}
	catalog := &config.ACPAgentsConfig{
		Agents: []config.ACPAgentEntry{
			{ID: "codex", Enabled: true},
			{ID: "opencode", Enabled: false},
		},
	}
	mgr := acp.NewManager(cfg, catalog)
	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.list enabled: %v", err)
	}
	got := mustACPList(t, res)
	if !got.Enabled {
		t.Fatal("enabled = false, want true")
	}
	if len(got.Agents) != 2 {
		t.Fatalf("agents len = %d, want 2", len(got.Agents))
	}
	byID := map[string]ACPAgentStatus{}
	for _, a := range got.Agents {
		byID[a.ID] = a
	}
	codex, ok := byID["codex"]
	if !ok {
		t.Fatal("missing agent id=codex")
	}
	if !codex.Enabled {
		t.Error("codex.enabled = false, want true")
	}
	if codex.Running {
		t.Error("codex.running = true, want false (no live session)")
	}
	if codex.State != "" {
		t.Errorf("codex.state = %q, want empty", codex.State)
	}
	if codex.UptimeS != 0 {
		t.Errorf("codex.uptime_s = %d, want 0", codex.UptimeS)
	}
	open, ok := byID["opencode"]
	if !ok {
		t.Fatal("missing agent id=opencode")
	}
	if open.Enabled {
		t.Error("opencode.enabled = true, want false")
	}
}

func TestACPListResult_JSONAndYAMLTags(t *testing.T) {
	assertJSONYAMLTags(t, reflect.TypeFor[ACPListResult](), map[string]string{
		"Enabled": "enabled",
		"Agents":  "agents",
	})
	assertJSONYAMLTags(t, reflect.TypeFor[ACPAgentStatus](), map[string]string{
		"ID":      "id",
		"Enabled": "enabled",
		"Running": "running",
		"State":   "state",
		"UptimeS": "uptime_s",
	})

	raw, err := json.Marshal(ACPListResult{
		Enabled: false,
		Agents:  []ACPAgentStatus{},
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if _, ok := decoded["enabled"]; !ok {
		t.Fatal("json missing enabled")
	}
	agents, ok := decoded["agents"].([]any)
	if !ok {
		t.Fatalf("agents type = %T, want array", decoded["agents"])
	}
	if agents == nil {
		t.Fatal("agents marshaled as null, want []")
	}
}

// ---------------------------------------------------------------------------
// acp.permissions.list / acp.permissions.resolve (acp-human-permission-relay
// leaf 03, Contract C). Human relay surface: pending snapshot + resolve.
// ---------------------------------------------------------------------------

// acpTestNotifier is the minimal PermissionNotifier for wiring the relay
// seam in RPC tests (behavior lives in internal/acp; here we only need
// HasPermissionRelay() to report the seam as armed).
type acpTestNotifier struct{}

func (acpTestNotifier) NotifyPermissionRequested(_ acp.PendingPermission) bool        { return true }
func (acpTestNotifier) NotifyPermissionResolved(_ acp.PendingPermission, _, _ string) {}

func acpEnabledRelayManager(t *testing.T) (*acp.Manager, *acp.Relay) {
	t.Helper()
	cfg := config.ACPConfig{
		Enabled:        true,
		DialTimeout:    10,
		CallTimeout:    120,
		MaxAgents:      3,
		PermissionMode: "human",
		RelayTimeout:   120,
	}
	catalog := &config.ACPAgentsConfig{}
	mgr := acp.NewManager(cfg, catalog)
	rl := acp.NewRelay()
	mgr.SetPermissionRelay(rl, acpTestNotifier{})
	t.Cleanup(mgr.StopAll)
	return mgr, rl
}

func TestACPHandler_PermissionsList_NilManager(t *testing.T) {
	h := NewACPHandler(nil)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.permissions.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.permissions.list nil manager: %v", err)
	}
	got := mustACPPermissions(t, res)
	if got.Enabled {
		t.Fatal("enabled = true, want false")
	}
	if got.Pending == nil {
		t.Fatal("pending = nil, want empty slice")
	}
	if len(got.Pending) != 0 {
		t.Fatalf("pending len = %d, want 0", len(got.Pending))
	}
}

func TestACPHandler_PermissionsList_DisabledManager(t *testing.T) {
	mgr := acp.NewManager(config.ACPConfig{PermissionMode: "permissive"}, &config.ACPAgentsConfig{})
	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.permissions.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.permissions.list disabled manager: %v", err)
	}
	got := mustACPPermissions(t, res)
	if got.Enabled {
		t.Fatal("enabled = true, want false (disabled manager)")
	}
	if got.Pending == nil || len(got.Pending) != 0 {
		t.Fatalf("pending = %+v, want empty slice", got.Pending)
	}
}

func TestACPHandler_PermissionsList_EnabledRelay(t *testing.T) {
	mgr, rl := acpEnabledRelayManager(t)
	if _, err := rl.Register(acp.PendingPermission{
		ID:        "perm-0001",
		AgentID:   "codex",
		SessionID: "sess-abc",
		Summary:   "tool exec: rm -rf",
	}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.permissions.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.permissions.list: %v", err)
	}
	got := mustACPPermissions(t, res)
	if !got.Enabled {
		t.Fatal("enabled = false, want true (armed human relay)")
	}
	if len(got.Pending) != 1 {
		t.Fatalf("pending len = %d, want 1", len(got.Pending))
	}
	p := got.Pending[0]
	if p.ID != "perm-0001" || p.AgentID != "codex" || p.SessionID != "sess-abc" || p.Summary != "tool exec: rm -rf" {
		t.Fatalf("pending row = %+v", p)
	}
	if p.CreatedAt.IsZero() {
		t.Error("created_at zero — Relay stamps CreatedAt on register")
	}

	// Frozen wire keys (leaf 03 task 1): id, agent_id, session_id, summary,
	// created_at.
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"enabled", "pending"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("envelope missing %q: %s", key, raw)
		}
	}
	rows, ok := decoded["pending"].([]any)
	if !ok || len(rows) != 1 {
		t.Fatalf("pending = %v, want 1-row array", decoded["pending"])
	}
	row, ok := rows[0].(map[string]any)
	if !ok {
		t.Fatalf("pending[0] = %T, want object", rows[0])
	}
	for _, key := range []string{"id", "agent_id", "session_id", "summary", "created_at"} {
		if _, ok := row[key]; !ok {
			t.Errorf("pending row missing frozen key %q: %v", key, row)
		}
	}
}

func TestACPHandler_PermissionsList_TagParity(t *testing.T) {
	assertJSONYAMLTags(t, reflect.TypeOf(ACPPendingPermissionStatus{}), map[string]string{
		"ID":        "id",
		"AgentID":   "agent_id",
		"SessionID": "session_id",
		"Summary":   "summary",
		"CreatedAt": "created_at",
	})
	assertJSONYAMLTags(t, reflect.TypeOf(ACPPermissionsResult{}), map[string]string{
		"Enabled": "enabled",
		"Pending": "pending",
	})
}

func TestACPHandler_PermissionsResolve_NilManager(t *testing.T) {
	h := NewACPHandler(nil)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.permissions.resolve",
		json.RawMessage(`{"id":"perm-x","decision":"allow"}`))
	if err == nil {
		t.Fatalf("nil manager resolve returned result %v, want not-found error", res)
	}
	if res != nil {
		t.Errorf("nil manager resolve result = %v, want nil", res)
	}
}

func TestACPHandler_PermissionsResolve_InvalidDecision(t *testing.T) {
	// Explicit RPC-boundary validation: a typo must NEVER silently deny
	// (relay-level normalize must not be relied on here).
	for _, decision := range []string{"", "ALWAYS", "yes", "nope"} {
		t.Run("decision_"+decision, func(t *testing.T) {
			mgr, _ := acpEnabledRelayManager(t)
			h := NewACPHandler(mgr)
			srv := New(&Config{SocketPath: ""}, nil, nil)
			h.RegisterACPMethods(srv)

			res, err := srv.CallMethod(context.Background(), "acp.permissions.resolve",
				json.RawMessage(`{"id":"perm-1","decision":"`+decision+`"}`))
			if err == nil {
				t.Fatalf("decision %q accepted (result %v), want invalid-params error", decision, res)
			}
			if !strings.Contains(strings.ToLower(err.Error()), "invalid params") {
				t.Errorf("error = %v, want invalid-params envelope", err)
			}
		})
	}
}

func TestACPHandler_PermissionsResolve_MissingID(t *testing.T) {
	mgr, _ := acpEnabledRelayManager(t)
	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.permissions.resolve",
		json.RawMessage(`{"decision":"allow"}`))
	if err == nil {
		t.Fatalf("empty id accepted (result %v), want invalid-params error", res)
	}
	if !strings.Contains(strings.ToLower(err.Error()), "invalid params") {
		t.Errorf("error = %v, want invalid-params envelope", err)
	}
}

func TestACPHandler_PermissionsResolve_UnknownID(t *testing.T) {
	mgr, _ := acpEnabledRelayManager(t)
	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	_, err := srv.CallMethod(context.Background(), "acp.permissions.resolve",
		json.RawMessage(`{"id":"perm-nope","decision":"allow"}`))
	if err == nil {
		t.Fatal("unknown id resolved, want not-found error")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "not found") {
		t.Errorf("error = %v, want not-found", err)
	}
}

func TestACPHandler_PermissionsResolve_HappyPathEchoes(t *testing.T) {
	mgr, rl := acpEnabledRelayManager(t)
	ch, err := rl.Register(acp.PendingPermission{ID: "perm-42", AgentID: "codex", SessionID: "s1"})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}
	// Drain the decision the RPC delivers so the pending-wait shape matches
	// production (the registry removes entries on the session's consume; the
	// RPC test just proves the send landed).
	_ = ch

	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)

	res, err := srv.CallMethod(context.Background(), "acp.permissions.resolve",
		json.RawMessage(`{"id":"perm-42","decision":"allow"}`))
	if err != nil {
		t.Fatalf("resolve allow: %v", err)
	}
	raw, mErr := json.Marshal(res)
	if mErr != nil {
		t.Fatalf("marshal resolve result: %v", mErr)
	}
	var echoed struct {
		ID       string `json:"id"`
		Decision string `json:"decision"`
		Reason   string `json:"reason"`
	}
	if uErr := json.Unmarshal(raw, &echoed); uErr != nil {
		t.Fatalf("unmarshal resolve result %s: %v", raw, uErr)
	}
	if echoed.ID != "perm-42" || echoed.Decision != "allow" {
		t.Errorf("echo = %+v, want id perm-42 decision allow", echoed)
	}
	if echoed.Reason == "" {
		t.Error("resolve reply must carry reason")
	}

	// Already-answered second call errors (exactly-once).
	if _, err := srv.CallMethod(context.Background(), "acp.permissions.resolve",
		json.RawMessage(`{"id":"perm-42","decision":"deny"}`)); err == nil {
		t.Fatal("double resolve accepted, want already-answered error")
	}
}

func mustACPPermissions(t *testing.T, res any) ACPPermissionsResult {
	t.Helper()
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal %T: %v", res, err)
	}
	var got ACPPermissionsResult
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("decode %T into ACPPermissionsResult: %v", res, err)
	}
	return got
}

func mustACPList(t *testing.T, res any) ACPListResult {
	t.Helper()
	switch v := res.(type) {
	case ACPListResult:
		return v
	case *ACPListResult:
		if v == nil {
			t.Fatal("nil *ACPListResult")
		}
		return *v
	default:
		raw, err := json.Marshal(res)
		if err != nil {
			t.Fatalf("marshal %T: %v", res, err)
		}
		var got ACPListResult
		if err := json.Unmarshal(raw, &got); err != nil {
			t.Fatalf("decode %T into ACPListResult: %v", res, err)
		}
		return got
	}
}

func assertJSONYAMLTags(t *testing.T, typ reflect.Type, want map[string]string) {
	t.Helper()
	for field, key := range want {
		f, ok := typ.FieldByName(field)
		if !ok {
			t.Errorf("%s: missing field %s", typ.Name(), field)
			continue
		}
		if got := f.Tag.Get("json"); got != key {
			t.Errorf("%s.%s json tag = %q, want %q", typ.Name(), field, got, key)
		}
		if got := f.Tag.Get("yaml"); got != key {
			t.Errorf("%s.%s yaml tag = %q, want %q", typ.Name(), field, got, key)
		}
	}
}
