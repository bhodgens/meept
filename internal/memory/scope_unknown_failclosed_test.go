package memory

import (
	"testing"
)

// A3-6 pins: scope_filter.visible must FAIL CLOSED on an unknown
// non-empty row scope (the file's own Flag 3 rule: an identity
// resolution gap must never widen recall), while the legacy
// empty-scope row keeps reading global via scopeFromMetadata's
// documented backfill.

func scopeMem(scope Scope, meta map[string]any) Memory {
	m := Memory{ID: "r1", Content: "unknown scope pin", Metadata: meta}
	m.Scope = scope
	return m
}

func TestUnknownScopeFailsClosed(t *testing.T) {
	t.Parallel()
	weird := Scope("corp") // outside {global, project, user}
	cases := []struct {
		name string
		f    *scopeRecallFilter
		mem  Memory
		want bool
	}{
		{"unknown scope hidden from project caller",
			&scopeRecallFilter{maxScope: ScopeProject, projectID: "p1"},
			scopeMem(weird, map[string]any{"scope": "corp"}), false},
		{"unknown scope hidden from user caller",
			&scopeRecallFilter{maxScope: ScopeUser, userID: "u1"},
			scopeMem(weird, map[string]any{"scope": "corp"}), false},
		{"unknown scope hidden from global caller",
			&scopeRecallFilter{maxScope: ScopeGlobal},
			scopeMem(weird, map[string]any{"scope": "corp"}), false},
		{"unknown scope hidden even with empty row scope field (metadata truth)",
			&scopeRecallFilter{maxScope: ScopeUser},
			scopeMem("", map[string]any{"scope": "corp"}), false},
		// Non-vacuity control (legacy direction unchanged):
		{"legacy empty scope with NO metadata stamp still global",
			&scopeRecallFilter{maxScope: ScopeProject, projectID: "p1"},
			scopeMem("", nil), true},
		{"legacy empty scope with empty-string stamp still global",
			&scopeRecallFilter{maxScope: ScopeProject, projectID: "p1"},
			scopeMem("", map[string]any{"scope": ""}), true},
		{"known scopes unchanged",
			&scopeRecallFilter{maxScope: ScopeProject, projectID: "p1"},
			scopeMem(ScopeProject, map[string]any{"scope": "project", "project_id": "p1"}), true},
	}
	for _, tc := range cases {
		if got := tc.f.visible(tc.mem); got != tc.want {
			t.Errorf("%s: visible = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestUnknownScopeViaFilterResultsAndBridge: the same predicate is the
// one FilterResults and bridgeVisible run - pin the exported surface
// too, not just the private method.
func TestUnknownScopeViaFilterResultsAndBridge(t *testing.T) {
	t.Parallel()
	unknown := Memory{ID: "u", Scope: Scope("workspace"),
		Metadata: map[string]any{"scope": "workspace"}}
	known := Memory{ID: "k", Scope: ScopeGlobal, Metadata: map[string]any{"scope": "global"}}
	res := FilterResults(
		[]MemoryResult{{Memory: unknown}, {Memory: known}},
		WithScopeFilter(ScopeUser, "", ""), WithMultiUserEnabled(true),
	)
	if len(res) != 1 || res[0].Memory.ID != "k" {
		t.Fatalf("FilterResults leaked unknown-scope row: %+v", res)
	}
	if bridgeVisible(&scopeRecallFilter{maxScope: ScopeUser}, BridgeRecord{
		ID: "b", Metadata: map[string]any{"scope": "workspace"},
	}) {
		t.Fatal("bridgeVisible leaked unknown-scope record")
	}
	// Legacy bridge (no scope key) stays visible (documented safe reading).
	if !bridgeVisible(&scopeRecallFilter{maxScope: ScopeUser}, BridgeRecord{ID: "b2"}) {
		t.Fatal("legacy unstamped bridge must stay visible")
	}
}

// TestVisibleScopeRowExportedPredicate: the exported one-row form
// (vector hybrid lane) shares the fail-closed default.
func TestVisibleScopeRowExportedPredicate(t *testing.T) {
	t.Parallel()
	if VisibleScopeRow(map[string]any{"scope": "corp"}, ScopeUser, "", "u1", false) {
		t.Fatal("VisibleScopeRow leaked unknown scope (A3-6 must be one predicate)")
	}
	if !VisibleScopeRow(nil, ScopeUser, "", "u1", false) {
		t.Fatal("legacy unstamped row must still read global")
	}
}
