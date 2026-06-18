package lint

import (
	"testing"
)

// TestAllSetters_NilSafe verifies that every Set* method on lint-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// NewRegistry initializes the mutex and language map.
	reg := NewRegistry()

	tests := []struct {
		name    string
		setFunc func()
	}{
		// Registry setters (internal/lint/registry.go)
		{"Registry.SetGlobalLinter", func() { reg.SetGlobalLinter(nil) }},
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
