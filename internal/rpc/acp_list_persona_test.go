package rpc

// acp_list_persona_test.go — regression for the 2026-10-23 e2e leaf
// finding: handleList keyed the running-state lookup by the BARE catalog
// agent id, so persona-lane sessions (LiveSessions label "agent/persona")
// never surfaced as running=true even when live. The employee executor
// chain always spawns via GetOrCreatePersona, so on the real wire this
// meant acp.list could never report a persona-backed agent as running.

import (
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/config"
)

// acpFakeAgentBin builds the fake ACP agent from internal/acp testdata —
// the same subprocess the e2e acp-wire suite drives — so the persona-lane
// session is a REAL live session in the manager registry.
func acpFakeAgentBin(t *testing.T) string {
	t.Helper()
	_, thisFile, _, _ := runtime.Caller(0) //nolint:dogsled // identity probe
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(thisFile), "..", ".."))
	dir := t.TempDir()
	bin := filepath.Join(dir, "fakeagent")
	cmd := exec.Command("go", "build", "-o", bin, "./testdata/fakeagent")
	cmd.Dir = filepath.Join(repoRoot, "internal", "acp")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build fakeagent: %v\n%s", err, out)
	}
	return bin
}

func acpPersonaListCatalog(t *testing.T, bin string) (*acp.Manager, *ACPListResult) {
	t.Helper()
	cfg := config.ACPConfig{
		Enabled:        true,
		DialTimeout:    30,
		CallTimeout:    60,
		MaxAgents:      8,
		PermissionMode: "permissive",
	}
	catalog := &config.ACPAgentsConfig{Agents: []config.ACPAgentEntry{
		{ID: "fake", Command: []string{bin, "-mode", "echo"}, Enabled: true},
		{ID: "ghost", Command: []string{bin, "-mode", "echo"}, Enabled: true},
	}}
	mgr := acp.NewManager(cfg, catalog)
	t.Cleanup(mgr.StopAll)

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := mgr.GetOrCreatePersona(ctx, "fake", "bob", t.TempDir(), acp.PersonaOpts{}); err != nil {
		t.Fatalf("GetOrCreatePersona bob: %v", err)
	}

	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)
	res, err := srv.CallMethod(context.Background(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.list: %v", err)
	}
	got := mustACPList(t, res)
	return mgr, &got
}

func byAgentID(rows []ACPAgentStatus) map[string]ACPAgentStatus {
	out := map[string]ACPAgentStatus{}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out
}

// TestACPHandler_List_PersonaLaneRunning is the core regression: ONLY a
// persona session is live ("fake/bob"), no bare-lane session exists, yet
// the catalog agent must report running=true (pre-fix it reported
// running=false forever because live["fake"] never matched).
func TestACPHandler_List_PersonaLaneRunning(t *testing.T) {
	bin := acpFakeAgentBin(t)
	_, got := acpPersonaListCatalog(t, bin)

	if len(got.Agents) != 2 {
		t.Fatalf("agents len = %d, want 2", len(got.Agents))
	}
	byID := byAgentID(got.Agents)
	fake, ok := byID["fake"]
	if !ok {
		t.Fatal("missing agent id=fake")
	}
	if !fake.Running {
		t.Errorf("fake.running = false, want true (persona lane acp list is a live session)")
	}
	// Additive per-persona detail (only live persona is bob).
	if len(fake.Personas) != 1 || fake.Personas[0] != "bob" {
		t.Errorf("fake.personas = %v, want [bob]", fake.Personas)
	}
	// The bare-lane state field keeps its no-persona semantics: the bare
	// "fake" session does not exist, so state stays empty (contract
	// today: running spans lanes, state reflects the no-persona lane).
	if fake.State != "" {
		t.Errorf("fake.state = %q, want empty (no bare-lane session)", fake.State)
	}
	ghost, ok := byID["ghost"]
	if !ok {
		t.Fatal("missing agent id=ghost")
	}
	if ghost.Running {
		t.Errorf("ghost.running = true, want false (no live session on any lane)")
	}
	if ghost.Personas != nil {
		t.Errorf("ghost.personas = %v, want nil (omitted on wire)", ghost.Personas)
	}
}

// TestACPHandler_List_PersonaLaneWireShape pins the additive-only wire
// contract: a persona-live row carries a sorted "personas" key; a
// no-persona row OMITS the key entirely so pre-persona consumers see
// byte-identical rows.
func TestACPHandler_List_PersonaLaneWireShape(t *testing.T) {
	bin := acpFakeAgentBin(t)
	mgr, got := acpPersonaListCatalog(t, bin)
	if !byAgentID(got.Agents)["fake"].Running {
		t.Fatalf("precondition: fake.running = false after the persona spawn")
	}

	// Second persona to prove sorting; the real subprocess path makes
	// StopPersona cheap and observable on the RPC surface.
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	if _, err := mgr.GetOrCreatePersona(ctx, "fake", "alice", t.TempDir(), acp.PersonaOpts{}); err != nil {
		t.Fatalf("GetOrCreatePersona alice: %v", err)
	}
	h := NewACPHandler(mgr)
	srv := New(&Config{SocketPath: ""}, nil, nil)
	h.RegisterACPMethods(srv)
	res, err := srv.CallMethod(context.Background(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.list: %v", err)
	}
	raw, mErr := json.Marshal(res)
	if mErr != nil {
		t.Fatalf("marshal: %v", mErr)
	}
	var decoded struct {
		Enabled bool             `json:"enabled"`
		Agents  []map[string]any `json:"agents"`
	}
	if uErr := json.Unmarshal(raw, &decoded); uErr != nil {
		t.Fatalf("unmarshal %s: %v", raw, uErr)
	}
	var fakeRow, ghostRow map[string]any
	for _, row := range decoded.Agents {
		switch row["id"] {
		case "fake":
			fakeRow = row
		case "ghost":
			ghostRow = row
		}
	}
	if fakeRow == nil || ghostRow == nil {
		t.Fatalf("rows missing in %s", raw)
	}
	if running, _ := fakeRow["running"].(bool); !running {
		t.Errorf("wire fake.running = %v, want true", fakeRow["running"])
	}
	personas, ok := fakeRow["personas"].([]any)
	if !ok || len(personas) != 2 {
		t.Fatalf("wire fake.personas = %v, want [alice bob]", fakeRow["personas"])
	}
	// Sorted deterministically despite map iteration in LiveSessions.
	if personas[0] != "alice" || personas[1] != "bob" {
		t.Errorf("wire fake.personas = %v, want sorted [alice bob]", personas)
	}
	if _, has := ghostRow["personas"]; has {
		t.Errorf("no-persona row must OMIT personas key, row = %v", ghostRow)
	}

	// Dropping every persona lane flips running back to false.
	if err := mgr.StopPersona("fake", "bob"); err != nil {
		t.Fatalf("StopPersona bob: %v", err)
	}
	res2, err := srv.CallMethod(context.Background(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.list after stop bob: %v", err)
	}
	got2 := mustACPList(t, res2)
	if f := byAgentID(got2.Agents)["fake"]; !f.Running || len(f.Personas) != 1 || f.Personas[0] != "alice" {
		t.Fatalf("fake after stopping bob = %+v, want running with [alice]", f)
	}
	if err := mgr.StopPersona("fake", "alice"); err != nil {
		t.Fatalf("StopPersona alice: %v", err)
	}
	res3, err := srv.CallMethod(context.Background(), "acp.list", json.RawMessage("{}"))
	if err != nil {
		t.Fatalf("acp.list after stop alice: %v", err)
	}
	got3 := mustACPList(t, res3)
	if f := byAgentID(got3.Agents)["fake"]; f.Running || f.Personas != nil {
		t.Fatalf("fake after stopping all personas = %+v, want running=false personas omitted", f)
	}
}
