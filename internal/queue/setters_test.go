package queue

import (
	"testing"
)

// TestAllSetters_NilSafe verifies that every Set* method on queue-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// PersistentQueue.SetTaskCancelledCallback uses the queue's mutex; a
	// zero-value instance has a ready-to-use zero mutex, so we can construct
	// directly without invoking NewPersistentQueue (which opens a SQLite DB).
	pq := &PersistentQueue{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// PersistentQueue setters (internal/queue/queue.go)
		{"PersistentQueue.SetTaskCancelledCallback", func() { pq.SetTaskCancelledCallback(nil) }},
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
