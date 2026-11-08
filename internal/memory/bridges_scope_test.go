package memory

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Pins for bughunt wave 2026-11-06 memory F-1: bridge recall bypassed the
// C2 scope filter (bridge rows were appended AFTER filterScopeResults and
// carried no scope key, so they were implicitly global). The fix stamps a
// conservative scope onto every bridge at generation (folded from the
// evidence sources) and re-applies the predicate at recall (bridgeVisible
// inside mergeBridges).

func scopedBridgeMemory(id string, scope Scope, meta map[string]any) Memory {
	m := Memory{
		ID:      id,
		Content: id + " fact about sharedentity",
		Type:    MemoryTypeEpisodic,
		Scope:   scope,
		Metadata: map[string]any{
			BridgeMetaEntities: "sharedentity",
		},
	}
	for k, v := range meta {
		m.Metadata[k] = v
	}
	return m
}

// TestBridgeSourceScopeMatrix pins the conservative fold.
func TestBridgeSourceScopeMatrix(t *testing.T) {
	p1 := scopedBridgeMemory("m-p1", ScopeProject, map[string]any{"project_id": "alpha"})
	p1b := scopedBridgeMemory("m-p2", ScopeProject, map[string]any{"project_id": "alpha"})
	p2 := scopedBridgeMemory("m-q", ScopeProject, map[string]any{"project_id": "beta"})
	pNoBind := scopedBridgeMemory("m-nb", ScopeProject, nil)
	u1 := scopedBridgeMemory("m-u1", ScopeUser, map[string]any{"user_id": "alice"})
	u2 := scopedBridgeMemory("m-u2", ScopeUser, map[string]any{"user_id": "bob"})
	g := scopedBridgeMemory("m-g", ScopeGlobal, nil)
	legacy := Memory{ID: "m-legacy", Content: "x sharedentity", Metadata: map[string]any{BridgeMetaEntities: "sharedentity"}}

	tests := []struct {
		name        string
		sources     []Memory
		wantScope   Scope
		wantProject string
		wantUser    string
	}{
		{"all-global", []Memory{g, scopedBridgeMemory("m-g2", ScopeGlobal, nil)}, ScopeGlobal, "", ""},
		{"legacy-only-is-global", []Memory{legacy, g}, ScopeGlobal, "", ""},
		{"single-project", []Memory{p1, p1b, g}, ScopeProject, "alpha", ""},
		{"two-projects-fails-to-user", []Memory{p1, p2, g}, ScopeUser, "", ""},
		{"project-without-binding-fails-to-user", []Memory{pNoBind, g}, ScopeUser, "", ""},
		{"project-plus-user-capped-at-user", []Memory{p1, u1}, ScopeUser, "", "alice"},
		{"two-users-blank-id", []Memory{u1, u2}, ScopeUser, "", ""},
		{"single-user", []Memory{u1, g}, ScopeUser, "", "alice"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			scope, pid, uid := bridgeSourceScope(tc.sources)
			if scope != tc.wantScope || pid != tc.wantProject || uid != tc.wantUser {
				t.Errorf("bridgeSourceScope = (%q,%q,%q), want (%q,%q,%q)",
					scope, pid, uid, tc.wantScope, tc.wantProject, tc.wantUser)
			}
		})
	}
}

// TestGenerateBridgesStampsScope pins the write side: a bridge folded from
// one project's sources persists scope=project + project_id, and the row
// round-trips through SearchBridgesByQuery with the stamp intact.
func TestGenerateBridgesStampsScope(t *testing.T) {
	ctx := context.Background()
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()

	gen := &fakeBridgeFactGen{responses: []string{
		`[{"text":"bridged fact","evidence_ids":["m-p1","m-p2"]}]`,
	}}
	candidates := []Memory{
		scopedBridgeMemory("m-p1", ScopeProject, map[string]any{"project_id": "alpha"}),
		scopedBridgeMemory("m-p2", ScopeProject, map[string]any{"project_id": "alpha"}),
	}
	generated, _, err := GenerateBridges(ctx, bs, gen, BridgesConfig{Enabled: true, BridgeDFMax: 10}, candidates)
	if err != nil {
		t.Fatalf("GenerateBridges: %v", err)
	}
	if generated != 1 {
		t.Fatalf("generated = %d, want 1", generated)
	}

	hits, err := bs.SearchBridgesByQuery(ctx, "bridged fact", 4)
	if err != nil || len(hits) != 1 {
		t.Fatalf("SearchBridgesByQuery = %d hits, err %v, want 1", len(hits), err)
	}
	md := hits[0].Metadata
	if md["scope"] != string(ScopeProject) {
		t.Errorf("bridge metadata scope = %v, want %q", md["scope"], ScopeProject)
	}
	if md["project_id"] != "alpha" {
		t.Errorf("bridge metadata project_id = %v, want alpha", md["project_id"])
	}
}

// TestMergeBridgesAppliesScopeFilter pins the recall gate: with the filter
// resolving a DIFFERENT project, the stamped bridge is hidden; with the
// matching project it is appended; with a nil filter (legacy unfiltered
// callers) it is appended byte-identically.
func TestMergeBridgesAppliesScopeFilter(t *testing.T) {
	ctx := context.Background()
	m := mustNewManager(t)
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()
	if err := bs.PutBridge(ctx, BridgeRecord{
		ID:          "bridge-gated",
		Content:     "cross-project bridged secret",
		Embedding:   hashBridgeEmbedding("bridged secret probe"),
		EvidenceIDs: []string{"m-src"},
		Metadata: map[string]any{
			"scope":      string(ScopeProject),
			"project_id": "alpha",
		},
	}); err != nil {
		t.Fatalf("PutBridge: %v", err)
	}
	m.SetBridgeStore(bs)

	probe := "bridged secret probe"
	alphaFilter := resolveScopeOptions([]SearchOption{WithScopeFilter(ScopeProject, "alpha", "")})
	betaFilter := resolveScopeOptions([]SearchOption{WithScopeFilter(ScopeProject, "beta", "")})

	mergeHas := func(f *scopeRecallFilter) bool {
		out, err := m.mergeBridges(ctx, probe, nil, f)
		if err != nil {
			t.Fatalf("mergeBridges: %v", err)
		}
		for _, r := range out {
			if strings.Contains(r.Memory.Content, "cross-project bridged secret") {
				return true
			}
		}
		return false
	}

	if !mergeHas(nil) {
		t.Error("nil filter hid a bridge (legacy path must stay byte-identical)")
	}
	if !mergeHas(alphaFilter) {
		t.Error("matching-project filter hid its own bridge")
	}
	if mergeHas(betaFilter) {
		t.Error("foreign-project filter LEAKED an alpha-scoped bridge (F-1 regression)")
	}
}

// TestManagerSearchScopeFilterHidesForeignBridge pins the end-to-end
// contract: Manager.Search with WithScopeFilter must never surface a
// bridge stamped to another project, regardless of which backend answered.
func TestManagerSearchScopeFilterHidesForeignBridge(t *testing.T) {
	ctx := context.Background()
	m := mustNewManager(t)
	if _, err := m.Store(ctx, Memory{
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
		Content:  "global anchor row bridgeword",
		Scope:    ScopeGlobal,
	}); err != nil {
		t.Fatalf("Store: %v", err)
	}
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()
	if err := bs.PutBridge(ctx, BridgeRecord{
		ID:          "bridge-e2e",
		Content:     "bridgeword foreign-project secret bridge",
		Embedding:   hashBridgeEmbedding("bridgeword foreign-project secret bridge"),
		EvidenceIDs: []string{"m-src"},
		Metadata: map[string]any{
			"scope":      string(ScopeProject),
			"project_id": "other",
		},
	}); err != nil {
		t.Fatalf("PutBridge: %v", err)
	}
	m.SetBridgeStore(bs)

	results, err := m.Search(ctx, MemoryQuery{Query: "bridgeword", Limit: 10},
		WithScopeFilter(ScopeProject, "mine", ""))
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	for _, r := range results {
		if r.Source == "bridge" && strings.Contains(r.Memory.Content, "foreign-project secret") {
			t.Fatalf("Search leaked foreign-project bridge: %q", r.Memory.Content)
		}
	}
}

// TestListBridgesScopedGatesListing (bughunt 2026-11-07 agent-fixes F-H):
// the bridge LISTING surface must apply the same C2 predicate as recall.
// Before the fix, Services.Bridges -> store.ListBridges returned every
// persisted bridge — including one distilled from another project's
// memories — while mergeBridges recall was already gated by 78a1da70.
// No options = unfiltered legacy posture (byte-identical); a project
// caller must NOT see another project's bridge rows.
func TestListBridgesScopedGatesListing(t *testing.T) {
	ctx := context.Background()
	m := mustNewManager(t)
	bs, err := NewBridgeStore(filepath.Join(t.TempDir(), "bridge-list.vec.db"))
	if err != nil {
		t.Fatalf("NewBridgeStore: %v", err)
	}
	defer bs.Close()

	seedBridges := []BridgeRecord{
		{ID: "bridge-alpha", Content: "alpha project fact", Embedding: hashBridgeEmbedding("a"),
			EvidenceIDs: []string{"m-a"}, Metadata: map[string]any{"scope": string(ScopeProject), "project_id": "alpha"}},
		{ID: "bridge-beta", Content: "beta project fact", Embedding: hashBridgeEmbedding("b"),
			EvidenceIDs: []string{"m-b"}, Metadata: map[string]any{"scope": string(ScopeProject), "project_id": "beta"}},
		{ID: "bridge-global", Content: "global fact", Embedding: hashBridgeEmbedding("g"),
			EvidenceIDs: []string{"m-g"}, Metadata: map[string]any{"scope": string(ScopeGlobal)}},
	}
	for _, b := range seedBridges {
		if err := bs.PutBridge(ctx, b); err != nil {
			t.Fatalf("PutBridge %s: %v", b.ID, err)
		}
	}
	m.SetBridgeStore(bs)

	// Unfiltered: all three (legacy, single-user owner-trusted posture).
	all, err := m.ListBridgesScoped(ctx)
	if err != nil {
		t.Fatalf("ListBridgesScoped (unfiltered): %v", err)
	}
	if len(all) != 3 {
		t.Fatalf("unfiltered listing = %d bridges, want 3", len(all))
	}

	// Project-beta caller: sees beta + global, NEVER alpha's bridge
	// (the F-H leak: distilled from alpha's memories, visible via the
	// dashboard lane).
	gated, err := m.ListBridgesScoped(ctx, WithScopeFilter(ScopeProject, "beta", ""))
	if err != nil {
		t.Fatalf("ListBridgesScoped (beta): %v", err)
	}
	if len(gated) != 2 {
		t.Fatalf("beta listing = %d bridges, want 2 (%+v)", len(gated), gated)
	}
	for _, b := range gated {
		if b.ID == "bridge-alpha" {
			t.Fatal("F-H leak: cross-project bridge row survived the scope gate on the LISTING lane")
		}
	}

	// Nil-store manager: empty, no error (same as recall's nil-store path).
	m2 := mustNewManager(t)
	empty, err := m2.ListBridgesScoped(ctx, WithScopeFilter(ScopeUser, "", ""))
	if err != nil || len(empty) != 0 {
		t.Fatalf("nil store: %v / %d, want nil error + empty", err, len(empty))
	}
}
