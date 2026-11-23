package acp

import (
	"context"
	"errors"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestManager_EffectivePermissionModePrecedence pins the single canonical
// resolver: call option > catalog entry > global. The tier-3 D3 guard
// consults this same precedence so it cannot diverge from the session.
func TestManager_EffectivePermissionModePrecedence(t *testing.T) {
	t.Parallel()
	cfg := testACPConfig(true, 3)
	cfg.PermissionMode = "human"
	m := NewManager(cfg, testAgents(
		config.ACPAgentEntry{ID: "permissive-entry", Command: []string{"true"}, Enabled: true, PermissionMode: "permissive"},
		config.ACPAgentEntry{ID: "inherit", Command: []string{"true"}, Enabled: true},
	))

	cases := []struct {
		name, agentID, callMode, want string
	}{
		{"catalog overrides global", "permissive-entry", "", "permissive"},
		{"call option overrides catalog", "permissive-entry", "human", "human"},
		{"call option overrides catalog to permissive", "inherit", "permissive", "permissive"},
		{"global fallback", "inherit", "", "human"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := m.EffectivePermissionMode(tc.agentID, tc.callMode); got != tc.want {
				t.Errorf("EffectivePermissionMode(%q, %q) = %q, want %q", tc.agentID, tc.callMode, got, tc.want)
			}
		})
	}

	// The internal resolver used by GetOrCreatePersona must agree with the
	// exported one on the same entry.
	entry, _ := m.lookup("permissive-entry")
	if got := m.effectivePermissionMode(entry, ""); got != "permissive" {
		t.Errorf("internal effectivePermissionMode = %q, want permissive", got)
	}
}

// TestManager_NoPermissiveRefusesCatalogPermissive is the core H5 pin: a
// no-permissive caller (a tier-3 employee) whose catalog entry says
// permissive while the global says human must NEVER get a permissive
// session. The session is never created, so session.go's auto-approve
// branch is unreachable. RED before the fix: the manager applied the
// catalog mode with no fail-closed check and returned a permissive session.
func TestManager_NoPermissiveRefusesCatalogPermissive(t *testing.T) {
	t.Parallel()
	cfg := testACPConfig(true, 3)
	cfg.PermissionMode = "human"
	m := NewManager(cfg, testAgents(
		config.ACPAgentEntry{ID: "bob", Command: []string{"true"}, Enabled: true, PermissionMode: "permissive"},
	))
	rec := &startRecorder{}
	attachStart(m, rec)

	ctx := WithNoPermissive(context.Background())
	sess, err := m.GetOrCreatePersona(ctx, "bob", "", "/work", PersonaOpts{})
	if !errors.Is(err, ErrPermissiveRefused) {
		t.Fatalf("GetOrCreatePersona error = %v, want ErrPermissiveRefused", err)
	}
	if sess != nil {
		t.Error("no-permissive caller must not receive a session")
	}
	if rec.calls() != 0 {
		t.Fatalf("start called %d times, want 0 (permissive session must never be created)", rec.calls())
	}

	// An UNMARKED caller still gets the permissive session (byte-identical
	// to the pre-guard behavior for chat / lower tiers). This proves the
	// guard is scoped to no-permissive callers only.
	sess2, err2 := m.GetOrCreatePersona(context.Background(), "bob", "", "/work", PersonaOpts{})
	if err2 != nil || sess2 == nil {
		t.Fatalf("unmarked caller: got (sess=%v, err=%v), want a permissive session", sess2, err2)
	}
	if rec.calls() != 1 {
		t.Errorf("start called %d times, want 1 for the unmarked caller", rec.calls())
	}
}

// TestManager_NoPermissiveRefusesAllThreeSources pins fail-closed for each
// of the three permission-mode sources: the marker must refuse permissive
// whether it came from a call option, the catalog entry, or the global.
func TestManager_NoPermissiveRefusesAllThreeSources(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name       string
		global     string
		catalog    string
		callMode   string
		wantRefuse bool
	}{
		{"global permissive", "permissive", "", "", true},
		{"catalog permissive", "human", "permissive", "", true},
		{"call option permissive", "human", "human", "permissive", true},
		{"call option human overrides catalog permissive", "human", "permissive", "human", false},
		{"human everywhere", "human", "", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testACPConfig(true, 3)
			cfg.PermissionMode = tc.global
			m := NewManager(cfg, testAgents(
				config.ACPAgentEntry{ID: "bob", Command: []string{"true"}, Enabled: true, PermissionMode: tc.catalog},
			))
			rec := &startRecorder{}
			attachStart(m, rec)
			_, err := m.GetOrCreatePersona(WithNoPermissive(context.Background()), "bob", "", "/work", PersonaOpts{PermissionMode: tc.callMode})
			refused := errors.Is(err, ErrPermissiveRefused)
			if refused != tc.wantRefuse {
				t.Errorf("refused=%v (err=%v), want %v", refused, err, tc.wantRefuse)
			}
		})
	}
}

// TestTurnCostSink_ForwardsCost pins the M2 wire-forward seam: ReportTurnCost
// calls the ctx sink with a positive cost and ignores non-positive / absent
// sinks, so a harness-reported cost can reach the goal-loop budget lane.
func TestTurnCostSink_ForwardsCost(t *testing.T) {
	t.Parallel()
	var got float64
	ctx := WithTurnCostSink(context.Background(), func(c float64) { got += c })
	ReportTurnCost(ctx, 0.01)
	ReportTurnCost(ctx, 0.02)
	if got != 0.03 {
		t.Errorf("sink got %v, want 0.03", got)
	}
	// Non-positive costs are ignored.
	ReportTurnCost(ctx, 0)
	ReportTurnCost(ctx, -1)
	if got != 0.03 {
		t.Errorf("sink got %v after non-positive costs, want 0.03", got)
	}
	// A dark ctx is a no-op (must not panic).
	ReportTurnCost(context.Background(), 1.0)
}
