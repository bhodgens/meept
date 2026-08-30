package eval

import (
	"sync/atomic"
)

// MetricsObserver records pass^k outcomes and oracle runs into the daemon
// metrics store (harness-eval leaf 15). It is wired IN-PROCESS so daemon-only
// runs (no TUI subscribed) still record — the "bus event with only optional
// UI consumers is a dead letter" lesson. Nil-safe: a nil observer makes every
// call a no-op.
type MetricsObserver struct {
	// Record is the sink (e.g. *metrics.Store.Record). Injected; nil-safe.
	Record func(name string, value float64, tags map[string]string)

	// passKTotal and oracleTotal are cheap in-process counters so tests can
	// assert recording happened without a real store.
	passKTotal   atomic.Int64
	oracleTotal  atomic.Int64
	passKPassed  atomic.Int64
	oraclePassed atomic.Int64
}

// NewMetricsObserver builds an observer over the given record sink.
func NewMetricsObserver(record func(name string, value float64, tags map[string]string)) *MetricsObserver {
	return &MetricsObserver{Record: record}
}

// RecordPassK records one completed pass^k run. passed is 1/0.
func (m *MetricsObserver) RecordPassK(taskID string, passed bool) {
	if m == nil {
		return
	}
	m.passKTotal.Add(1)
	if passed {
		m.passKPassed.Add(1)
	}
	if m.Record == nil {
		return
	}
	m.Record("eval_pass_k_total", 1, map[string]string{
		"task":   taskID,
		"passed": boolToFlag(passed),
	})
}

// RecordOracle records one individual oracle execution.
func (m *MetricsObserver) RecordOracle(oracleName string, passed bool) {
	if m == nil {
		return
	}
	m.oracleTotal.Add(1)
	if passed {
		m.oraclePassed.Add(1)
	}
	if m.Record == nil {
		return
	}
	m.Record("eval_oracle_runs_total", 1, map[string]string{
		"oracle": oracleName,
		"passed": boolToFlag(passed),
	})
}

// Counters exposes the in-process counters (test seam). Nil-safe: all zeros.
func (m *MetricsObserver) Counters() (passK, passKPassed, oracle, oraclePassed int64) {
	if m == nil {
		return 0, 0, 0, 0
	}
	return m.passKTotal.Load(), m.passKPassed.Load(), m.oracleTotal.Load(), m.oraclePassed.Load()
}

func boolToFlag(b bool) string {
	if b {
		return "true"
	}
	return "false"
}
