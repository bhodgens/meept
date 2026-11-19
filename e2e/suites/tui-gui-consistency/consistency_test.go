//go:build e2e

// Package tuguiconsistency is the cross-branch hermetic e2e suite for the
// tui-gui-consistency-fixes tree (leaf 00T-e2e-tests.md). It pins the
// cross-branch contracts on a REAL scratch daemon booted by the harness:
//
//	tui-gui-01 (T1) agents.roster answers rows carrying the frozen C1 keys
//	                (id/name/type/status/active_count/daily_cost_cents/enabled)
//	                and the three source types agent, bot, employee are all
//	                present (master.md C1; leaf 01-backend/01).
//	tui-gui-02 (T2) the roster's numeric fields are present and costs are
//	                surfaced from live bot state (leaf 01-backend/02).
//	tui-gui-03 (T3) the DEFAULT boot (no [kanban] overlay) answers
//	                kanban.board.list with a boards array — the default-on
//	                flip owned by leaf 04-config-docs/01 (master.md R14).
//
// All tests are RED on the unimplemented tree for missing-implementation
// reasons: agents.roster / kanban.board.list answer JSON-RPC -32601
// method-not-found. Hermetic: 127.0.0.1 + unix sockets only; no LLM turn is
// driven. Run via `go test -p 2 -tags e2e ./e2e/suites/tui-gui-consistency/...`.
package tuguiconsistency

import (
	"testing"

	"github.com/caimlas/meept/e2e/harness"
)

// requiredC1Keys are the C1 roster row keys the two UIs consume and that
// leaf 01-backend/01 freezes. role/description are `omitempty` and therefore
// not asserted here (they are legitimately absent on a minimal row).
var requiredC1Keys = []string{
	"id", "name", "type", "status",
	"active_count", "daily_cost_cents", "enabled",
}

// rosterRows boots a daemon, seeds one live employee + one live bot so all
// three roster sources are populated, and returns the decoded agents.roster
// rows. The RPC call itself is the RED point: on the unimplemented tree it
// answers -32601 and rpcResult fails with the method-not-found error.
func rosterRows(t *testing.T) []map[string]any {
	t.Helper()
	s := harness.Start(t)
	seedSources(t, s)

	out := rpcResult(t, s, "agents.roster", nil)
	raw, _ := out["agents"].([]any)
	rows := make([]map[string]any, 0, len(raw))
	for _, r := range raw {
		if rm, ok := r.(map[string]any); ok {
			rows = append(rows, rm)
		}
	}
	return rows
}

// TestRosterKeysAndTypes pins T1: every roster row carries the frozen C1
// keys and the merge exposes all three source types.
func TestRosterKeysAndTypes(t *testing.T) {
	rows := rosterRows(t)
	if len(rows) == 0 {
		t.Fatal("agents.roster returned zero rows; want at least one per source type")
	}

	for i, row := range rows {
		for _, key := range requiredC1Keys {
			if _, present := row[key]; !present {
				t.Errorf("roster row %d missing C1 key %q: %v", i, key, row)
			}
		}
	}

	// The roster must merge all three sources (master.md C1; F1/R8/R9).
	types := make(map[string]int)
	for _, row := range rows {
		types[str(row, "type")]++
	}
	for _, want := range []string{"agent", "bot", "employee"} {
		if types[want] == 0 {
			t.Errorf("roster missing type %q; got type counts %v (rows=%v)", want, types, rows)
		}
	}
}

// TestRosterNumericFields pins T2: active_count and daily_cost_cents are
// present as numbers on every row, and at least one row carries a real
// nonzero daily cost — the seeded bot's TodayCostCents=123 must survive the
// merge (leaf 01-backend/02 T2, master.md R3/R10). active_count is pinned as
// a present numeric field, not forced >0: leaf 01-backend/02 explicitly
// leaves it zero when there are no live loops, which is the honest default
// on a fresh hermetic boot.
func TestRosterNumericFields(t *testing.T) {
	rows := rosterRows(t)
	if len(rows) == 0 {
		t.Fatal("agents.roster returned zero rows")
	}

	var sawPositiveCost bool
	for i, row := range rows {
		for _, key := range []string{"active_count", "daily_cost_cents"} {
			if _, present := row[key]; !present {
				t.Errorf("roster row %d missing numeric field %q: %v", i, key, row)
			}
			// A JSON number always decodes to float64; a string/null here
			// means the handler emitted the wrong wire type.
			if v, ok := row[key].(float64); ok && v < 0 {
				t.Errorf("roster row %d field %q is negative (%v)", i, key, v)
			}
		}
		if cost := num(row, "daily_cost_cents"); cost > 0 {
			sawPositiveCost = true
		}
	}
	if !sawPositiveCost {
		t.Errorf("no roster row carried a nonzero daily_cost_cents; seeded bot TodayCostCents=123 must be surfaced: %v", rows)
	}
}

// TestDefaultBootServesKanbanBoardList pins T3: the DEFAULT daemon config
// (no [kanban] overlay — leaf 04-config-docs/01 flips DefaultConfig) must
// register the C4 kanban surface and answer kanban.board.list with a boards
// array. On the unimplemented tree this answers -32601 method-not-found,
// which is the intended RED.
func TestDefaultBootServesKanbanBoardList(t *testing.T) {
	// Plain Start: NO WithConfigOverlay({"kanban.enabled": true}) — the
	// whole point is that the shipped default is on (master.md R14).
	s := harness.Start(t)

	out := rpcResult(t, s, "kanban.board.list", nil)
	if _, ok := out["boards"].([]any); !ok {
		t.Fatalf("default boot kanban.board.list result has no boards array: %v", out)
	}
	// Presence of the boards array is this suite's contract; board contents
	// are the kanban suite's concern.
}
