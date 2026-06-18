package selfimprove

import (
	"testing"

	intsecurity "github.com/caimlas/meept/internal/security"
)

// TestAllSetters_NilSafe verifies that every Set* method on
// selfimprove-package structs that accepts a pointer, interface, slice, map,
// or func argument is nil-safe. See CLAUDE.md "Setter methods" coding
// practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// Controller setters only assign their argument (with a nil guard), so a
	// zero-value instance is sufficient.
	controller := &Controller{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// Controller setters (internal/selfimprove/controller.go)
		{"Controller.SetSecurityOrchestrator", func() {
			controller.SetSecurityOrchestrator((*intsecurity.Orchestrator)(nil))
		}},
		{"Controller.SetProgressCallback", func() { controller.SetProgressCallback(nil) }},
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
