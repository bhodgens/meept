// Package employee — metrics.go provides the telemetry emission helper
// used by the Manager, GoalLoop, and enforcement auditors. It centralises
// the nil-check + lock-snapshot pattern so call sites stay one-liners.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md lines
// 664-674 for the metric names and tagging scheme.
package employee

import "github.com/caimlas/meept/internal/metrics"

// SetMetricsStore wires the metrics store used for telemetry emission.
// Nil is ignored (typed-nil guard per CLAUDE.md setter convention). The
// store is snapshot under the manager's read lock at emission time, so
// concurrent SetMetricsStore calls are safe.
func (m *Manager) SetMetricsStore(store *metrics.Store) {
	if store == nil {
		return
	}
	m.mu.Lock()
	m.metricsStore = store
	m.mu.Unlock()
}

// emitMetric is the thread-safe emission helper used by all call sites.
// It snapshots the metrics store under the manager's read lock, nil-checks,
// then calls Record outside the lock (Record itself is lock-safe). When
// the store is nil the call is a no-op — callers do not need their own
// nil-checks.
func (m *Manager) emitMetric(name string, value float64, tags map[string]string) {
	m.mu.RLock()
	store := m.metricsStore
	m.mu.RUnlock()
	if store == nil {
		return
	}
	store.Record(name, value, tags)
}
