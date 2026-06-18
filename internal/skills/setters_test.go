package skills

import (
	"testing"
)

// TestAllSetters_NilSafe verifies that every Set* method on skills-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// Use the public constructor to ensure mutex/cache/logger are initialized.
	loader := NewLazySkillLoader(nil)
	executor := NewExecutor(nil)

	tests := []struct {
		name    string
		setFunc func()
	}{
		// LazySkillLoader setters (internal/skills/lazy_loader.go)
		{"LazySkillLoader.SetIndex", func() { loader.SetIndex((*SkillIndex)(nil)) }},

		// Executor setters (internal/skills/executor.go)
		{"Executor.SetLazyLoader", func() { executor.SetLazyLoader((*LazySkillLoader)(nil)) }},
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
