// bughunt 2026-11-25 gaps A3-8 / A3-9 pin tests.
package security

import (
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// A3-8: lookupBaseRule must fail CLOSED on an action absent from the
// seeded rules and every prefix classifier. The old fallback was
// MEDIUM/no-confirm, which Stage 5 let through as Permitted — the two
// permission engines (this one and pkg/security) disagreed on the
// repo's fail-closed rule.
func TestLookupBaseRule_UnknownActionFailsClosed(t *testing.T) {
	tmpDir := t.TempDir()
	engine, err := NewEngine(filepath.Join(tmpDir, "security.db"), &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	risk, confirm := engine.lookupBaseRule("totally_unknown_action_xyz", "totally_unknown_action_xyz")
	if risk != RiskCritical {
		t.Errorf("unknown action risk = %v, want CRITICAL (fail-closed fallback)", risk)
	}
	if !confirm {
		t.Error("unknown action rule-level confirm should be true (deny under default config)")
	}

	// End-to-end through the real Check seam: the unseeded action must NOT
	// come back Permitted.
	d := engine.Check("totally_unknown_action_xyz", "totally_unknown_action_xyz", nil, "")
	if d.Allowed {
		t.Fatalf("A3-8 regression: unknown action returned Allowed=true (risk=%s)", d.RiskLevel)
	}

	// Seeded actions are unaffected (the DB rule hit short-circuits before
	// the fallback).
	sd := engine.Check("file_read", "file_read", nil, "")
	if !sd.Allowed {
		t.Errorf("seeded file_read must stay allowed, got: %+v", sd)
	}
}

// A3-9: checkOverrides must NOT swallow a DB query error as "no override
// applies" — a stored DENY override would be silently skipped while the
// override table is unreadable. The fix returns a deny-conservative
// decision with the override_query_error rule source.
func TestCheckOverrides_QueryErrorDenies(t *testing.T) {
	tmpDir := t.TempDir()
	engine, err := NewEngine(filepath.Join(tmpDir, "security.db"), &config.SecurityConfig{
		RequireConfirmationHigh: true,
	}, nil)
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Close()

	// Positive control FIRST: with the table intact, an action with no
	// override returns nil ("nothing applies") and a seeded LOW action
	// checks allowed.
	if dec := engine.checkOverrides("file_read", map[string]string{"path": "/tmp/x"}); dec != nil {
		t.Fatalf("clean-table control: expected nil, got %+v", dec)
	}

	// Simulate the unreadable override table (busy/IO error shape): drop
	// the table so the SELECT fails while every other stage still works.
	if _, err := engine.db.Exec(`DROP TABLE permission_overrides`); err != nil {
		t.Fatalf("drop table: %v", err)
	}

	dec := engine.checkOverrides("file_read", map[string]string{"path": "/tmp/x"})
	if dec == nil {
		t.Fatal("A3-9 regression: checkOverrides swallowed the query error and returned nil — a stored DENY override would be skipped")
	}
	if dec.Allowed {
		t.Fatal("query-error decision must deny (Allowed=false), never allow")
	}
	if dec.RuleSource != "override_query_error" {
		t.Errorf("RuleSource = %q, want override_query_error", dec.RuleSource)
	}

	// End-to-end: the full Check pipeline on an otherwise-permitted action
	// must deny while the override state is unknowable.
	d := engine.Check("file_read", "file_read", map[string]string{"path": "/tmp/x"}, "")
	if d.Allowed {
		t.Fatalf("A3-9 regression: Check allowed an action with an unreadable override table: %+v", d)
	}
}
