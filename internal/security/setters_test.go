package security

import (
	"testing"

	"github.com/caimlas/meept/internal/security/taint"
)

// TestAllSetters_NilSafe verifies that every Set* method on security-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// The setters under test only assign their argument to a field (with a
	// nil guard), so a zero-value struct is sufficient.
	engine := &Engine{}
	orch := &Orchestrator{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// Engine setters (internal/security/engine.go)
		{"Engine.SetFenceChecker", func() { engine.SetFenceChecker((*FenceChecker)(nil)) }},

		// Orchestrator setters (internal/security/orchestrator.go).
		// TaintTracker is an alias for taint.ExtendedTracker (struct).
		{"Orchestrator.SetTaintTracker", func() { orch.SetTaintTracker((*TaintTracker)(nil)) }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("Set method panicked on nil: %v", r)
				}
			}()
			tt.setFunc()
		})
	}
}

// silence unused import if TaintTracker's underlying type resolution needs the
// taint package transitively.
var _ = (*taint.ExtendedTracker)(nil)
