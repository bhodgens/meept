package rpc

// roster_test.go — RED tests for the unified agents.roster RPC (plan tree
// tui-gui-consistency-fixes, leaf 01-backend/00T-roster-tests.md).
//
// These tests are written FIRST and specify the C1 contract the roster
// implementation (leaf 01-backend/01-roster-rpc.md) must satisfy. They are
// deliberately RED: the symbols below (RosterEntry, RosterDeps,
// NewRosterHandler, RosterHandler.handleRoster) do not exist yet.
//
// Expected API contract for the implementer:
//
//	type RosterEntry struct {
//	    ID, Name, Type, Status, Role, Description string
//	    ActiveCount, DailyCostCents              int
//	    Enabled                                  bool
//	    // json tags: id,name,type,status,active_count,daily_cost_cents,
//	    //           role,description,enabled
//	}
//	type RosterDeps struct {
//	    ListAgents    func() []RosterEntry
//	    ListBots      func() []RosterEntry
//	    ListEmployees func() []RosterEntry
//	}
//	func NewRosterHandler(deps RosterDeps) *RosterHandler
//	func (h *RosterHandler) handleRoster(ctx context.Context, raw json.RawMessage) (any, error)
//
// The handler merges the three injected sources into one roster, stamping
// Type as "agent" | "bot" | "employee" and returning
// map[string]any{"agents": []RosterEntry}.
//
// NOTE ON INJECTION: internal/employee imports internal/rpc (handler_auth.go),
// so a package-rpc test cannot import concrete employee/agent/bot domain
// types. Neutral func seams are therefore both cycle-free and real fakes —
// no real daemon dependencies are constructed.

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"testing"
	"time"
)

// TestRosterEntry_JSONKeys pins the C1 row shape end-to-end (leaf 00T T1,
// updated for the additive M11 keys): every frozen JSON key survives
// marshal, no extra keys leak, and a marshal/unmarshal round-trip preserves
// every field. All fields are set to non-zero values so the zero-omission
// on the additive M11 keys cannot hide a key. A zero-valued additive row
// must stay byte-identical to the pre-M11 nine-key shape
// (TestRosterEntry_AdditiveKeysOmittedWhenZero below).
func TestRosterEntry_JSONKeys(t *testing.T) {
	lastRun := time.Date(2026, 11, 19, 12, 30, 0, 0, time.UTC)
	entry := RosterEntry{
		ID:             "eng-7",
		Name:           "engineer",
		Type:           "employee",
		Status:         "running",
		ActiveCount:    3,
		DailyCostCents: 1234,
		Role:           "executor",
		Description:    "ships the work",
		Enabled:        true,
		ExecutorAgent:  "hermes",
		DriftScore:     0.42,
		FindingsCount:  5,
		LastInvocation: lastRun,
	}

	raw, err := json.Marshal(entry)
	if err != nil {
		t.Fatalf("marshal RosterEntry: %v", err)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal RosterEntry into key map: %v", err)
	}

	wantKeys := []string{
		"id", "name", "type", "status", "active_count",
		"daily_cost_cents", "role", "description", "enabled",
		"executor_agent", "drift_score", "findings_count", "last_invocation",
	}
	gotKeys := make([]string, 0, len(got))
	for k := range got {
		gotKeys = append(gotKeys, k)
	}
	sort.Strings(gotKeys)
	sort.Strings(wantKeys)
	if !reflect.DeepEqual(gotKeys, wantKeys) {
		t.Errorf("RosterEntry JSON keys = %v, want exactly %v (raw=%s)", gotKeys, wantKeys, raw)
	}

	var back RosterEntry
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatalf("unmarshal RosterEntry round-trip: %v", err)
	}
	if back.ID != entry.ID || back.Name != entry.Name || back.Type != entry.Type ||
		back.Status != entry.Status || back.ActiveCount != entry.ActiveCount ||
		back.DailyCostCents != entry.DailyCostCents || back.Role != entry.Role ||
		back.Description != entry.Description || back.Enabled != entry.Enabled ||
		back.ExecutorAgent != entry.ExecutorAgent || back.DriftScore != entry.DriftScore ||
		back.FindingsCount != entry.FindingsCount || !back.LastInvocation.Equal(entry.LastInvocation) {
		t.Errorf("RosterEntry round-trip lost a field: got %+v, want %+v", back, entry)
	}
}

// TestRosterEntry_AdditiveKeysOmittedWhenZero pins the M11 additive
// contract: a row whose four additive fields are all zero marshals
// BYTE-IDENTICALLY to the pre-M11 nine-key shape (no executor_agent /
// drift_score / findings_count / last_invocation keys, and in particular
// no zero-time "0001-01-01T00:00:00Z" leak - last_invocation is omitzero,
// not omitempty).
func TestRosterEntry_AdditiveKeysOmittedWhenZero(t *testing.T) {
	zeroAdditive := RosterEntry{
		ID: "bot-1", Name: "bot", Type: "bot", Status: "stopped", Enabled: true,
	}
	raw, err := json.Marshal(zeroAdditive)
	if err != nil {
		t.Fatalf("marshal zero-additive RosterEntry: %v", err)
	}

	// The pre-M11 nine-key struct: marshaling it must produce identical bytes.
	type legacyRow struct {
		ID             string `json:"id"`
		Name           string `json:"name"`
		Type           string `json:"type"`
		Status         string `json:"status"`
		ActiveCount    int    `json:"active_count"`
		DailyCostCents int    `json:"daily_cost_cents"`
		Role           string `json:"role"`
		Description    string `json:"description"`
		Enabled        bool   `json:"enabled"`
	}
	legacyRaw, err := json.Marshal(legacyRow{
		ID: zeroAdditive.ID, Name: zeroAdditive.Name, Type: zeroAdditive.Type,
		Status: zeroAdditive.Status, Enabled: zeroAdditive.Enabled,
	})
	if err != nil {
		t.Fatalf("marshal legacy row: %v", err)
	}
	if string(raw) != string(legacyRaw) {
		t.Errorf("zero-additive row not byte-identical to pre-M11 shape:\n got %s\nwant %s", raw, legacyRaw)
	}

	var got map[string]json.RawMessage
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatalf("unmarshal zero-additive row: %v", err)
	}
	for _, k := range []string{"executor_agent", "drift_score", "findings_count", "last_invocation"} {
		if _, present := got[k]; present {
			t.Errorf("zero-valued additive key %q leaked onto the wire: %s", k, raw)
		}
	}
}

// TestRosterHandler_MergesSources pins leaf 00T T2: one registry agent, one
// bot and one employee injected through the seams must produce exactly three
// rows, one per source type, with the source fields preserved and Type
// stamped by the handler. The top-level response shape {"agents": [...]} is
// checked on the wire too.
func TestRosterHandler_MergesSources(t *testing.T) {
	h := NewRosterHandler(RosterDeps{
		ListAgents: func() []RosterEntry {
			return []RosterEntry{{
				ID: "coder", Name: "coder", Status: "running",
				Role: "executor", Description: "writes code", Enabled: true,
			}}
		},
		ListBots: func() []RosterEntry {
			return []RosterEntry{{
				ID: "bot-nightly", Name: "nightly", Status: "paused",
				DailyCostCents: 50, Enabled: true,
			}}
		},
		ListEmployees: func() []RosterEntry {
			return []RosterEntry{{
				ID: "eng-7", Name: "engineer", Status: "running",
				Role: "executor", ActiveCount: 2, Enabled: true,
			}}
		},
	})

	res, err := h.handleRoster(context.Background(), nil)
	if err != nil {
		t.Fatalf("handleRoster: %v", err)
	}
	body, ok := res.(map[string]any)
	if !ok {
		t.Fatalf("handleRoster response type = %T, want map[string]any", res)
	}
	rows, ok := body["agents"].([]RosterEntry)
	if !ok {
		t.Fatalf("response[agents] type = %T, want []RosterEntry", body["agents"])
	}
	if len(rows) != 3 {
		t.Fatalf("roster row count = %d, want 3 (agent+bot+employee): %+v", len(rows), rows)
	}

	byType := make(map[string]RosterEntry, len(rows))
	for _, r := range rows {
		if _, dup := byType[r.Type]; dup {
			t.Errorf("duplicate type %q in roster rows: %+v", r.Type, rows)
		}
		byType[r.Type] = r
	}
	for _, wantType := range []string{"agent", "bot", "employee"} {
		if _, present := byType[wantType]; !present {
			t.Errorf("roster missing type %q; got rows %+v", wantType, rows)
		}
	}

	// Source fields must survive the merge; only Type is stamped by the handler.
	if got := byType["agent"]; got.ID != "coder" || got.Name != "coder" || got.Status != "running" {
		t.Errorf("agent row not mapped from source: %+v", got)
	}
	if got := byType["bot"]; got.ID != "bot-nightly" || got.DailyCostCents != 50 || got.Status != "paused" {
		t.Errorf("bot row not mapped from source: %+v", got)
	}
	if got := byType["employee"]; got.ID != "eng-7" || got.ActiveCount != 2 || got.Role != "executor" {
		t.Errorf("employee row not mapped from source: %+v", got)
	}

	// Wire shape: top-level response must marshal to {"agents": [...]}.
	wire, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal roster response: %v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(wire, &top); err != nil {
		t.Fatalf("unmarshal roster response: %v", err)
	}
	if _, present := top["agents"]; !present {
		t.Errorf("roster response JSON missing top-level \"agents\" key: %s", wire)
	}
	if len(top) != 1 {
		t.Errorf("roster response JSON has unexpected keys %v: %s", keysOf(top), wire)
	}
}

func keysOf(m map[string]json.RawMessage) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
