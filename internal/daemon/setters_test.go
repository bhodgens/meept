package daemon

import (
	"testing"

	"github.com/caimlas/meept/internal/services"
)

// TestAllSetters_NilSafe verifies that every Set* method on daemon-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// DaemonRPCHandler.SetRuntimeService only assigns the field, so a
	// zero-value instance is sufficient to exercise nil-safety.
	h := &DaemonRPCHandler{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// DaemonRPCHandler setters (internal/daemon/daemon_rpc.go)
		{"DaemonRPCHandler.SetRuntimeService", func() {
			h.SetRuntimeService((*services.RuntimeService)(nil))
		}},
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
