package rpc

import (
	"context"
	"encoding/json"
	"reflect"
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
	assertJSONYAMLTags(t, reflect.TypeOf(ACPListResult{}), map[string]string{
		"Enabled": "enabled",
		"Agents":  "agents",
	})
	assertJSONYAMLTags(t, reflect.TypeOf(ACPAgentStatus{}), map[string]string{
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
