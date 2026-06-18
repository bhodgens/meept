package metrics

import (
	"log/slog"
	"testing"
)

// TestAllSetters_NilSafe verifies that every Set* method on llm/metrics-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// NewCalculator requires a non-nil store normally, but SetLogger does not
	// touch the store, so a zero-value Calculator is sufficient.
	calc := &Calculator{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// Calculator setters (internal/llm/metrics/adaptive.go)
		{"Calculator.SetLogger", func() { calc.SetLogger((*slog.Logger)(nil)) }},
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
