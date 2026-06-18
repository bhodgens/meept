package services

import (
	"database/sql"
	"testing"
)

// TestAllSetters_NilSafe verifies that every Set* method on services-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// SecurityService only needs a nil checker for this test (the setter
	// under test does not dereference the receiver's existing fields).
	secSvc := NewSecurityService(nil)

	// TerminalService is constructed with NewTerminalService so that its
	// shellTool field is non-nil, allowing SetKnownSafeCommands to forward
	// without panicking on a nil receiver.
	termSvc := NewTerminalService("", nil, nil)

	tests := []struct {
		name    string
		setFunc func()
	}{
		// SecurityService setters (internal/services/security_service.go)
		{"SecurityService.SetAuditDB", func() { secSvc.SetAuditDB((*sql.DB)(nil)) }},

		// TerminalService setters (internal/services/terminal_service.go)
		// SetKnownSafeCommands forwards to shellTool.SetKnownSafeCommands;
		// pass nil to verify the nil slice is handled safely.
		{"TerminalService.SetKnownSafeCommands", func() { termSvc.SetKnownSafeCommands(nil) }},
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
