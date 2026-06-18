package rpc

import (
	"testing"

	"github.com/caimlas/meept/internal/queue"
)

// TestAllSetters_NilSafe verifies that every Set* method on rpc-package
// structs that accepts a pointer, interface, slice, map, or func argument is
// nil-safe. See CLAUDE.md "Setter methods" coding practice.
func TestAllSetters_NilSafe(t *testing.T) {
	// Handlers are simple structs; their setters only assign fields, so a
	// zero-value instance is sufficient to exercise nil-safety.
	clusterH := &ClusterHandler{}
	projectH := &ProjectHandler{}

	tests := []struct {
		name    string
		setFunc func()
	}{
		// ClusterHandler setters (internal/rpc/cluster_handler.go)
		{"ClusterHandler.SetClusterQueue", func() { clusterH.SetClusterQueue((*queue.ClusterQueue)(nil)) }},
		{"ClusterHandler.SetStore", func() { clusterH.SetStore((*queue.Store)(nil)) }},

		// ProjectHandler setters (internal/rpc/projects.go)
		{"ProjectHandler.SetArtifactInvalidator", func() { projectH.SetArtifactInvalidator(nil) }},
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
