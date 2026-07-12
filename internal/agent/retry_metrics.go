package agent

import (
	"fmt"
	"sync"
	"time"
)

// RetryMetrics tracks retry statistics broken down by operation type and error class.
// All methods are safe for concurrent use.
type RetryMetrics struct {
	mu              sync.Mutex
	totalRetries    int64
	retriesByType   map[string]int64 // "llm", "tool", "http"
	retriesByError  map[string]int64 // error type -> count
	backoffDuration time.Duration    // total time spent in backoff
}

// RetryMetricsSnapshot is an immutable point-in-time copy of RetryMetrics.
// The maps are fresh copies so callers cannot mutate internal state.
type RetryMetricsSnapshot struct {
	TotalRetries    int64
	RetriesByType   map[string]int64
	RetriesByError  map[string]int64
	BackoffDuration time.Duration
}

// NewRetryMetrics creates an empty RetryMetrics.
func NewRetryMetrics() *RetryMetrics {
	return &RetryMetrics{
		retriesByType:  make(map[string]int64),
		retriesByError: make(map[string]int64),
	}
}

// RecordRetry records a single retry event.
// opType is "llm" / "tool" / "http". err is the error that triggered the retry.
// delay is the backoff delay that will be slept.
func (m *RetryMetrics) RecordRetry(opType string, err error, delay time.Duration) {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.totalRetries++
	m.retriesByType[opType]++
	m.retriesByError[fmt.Sprintf("%T", err)]++
	m.backoffDuration += delay
}

// Snapshot returns a copy of the current metrics suitable for serialization.
// The returned maps are fresh copies; mutating them does not affect internal state.
func (m *RetryMetrics) Snapshot() RetryMetricsSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()

	byType := make(map[string]int64, len(m.retriesByType))
	for k, v := range m.retriesByType {
		byType[k] = v
	}
	byError := make(map[string]int64, len(m.retriesByError))
	for k, v := range m.retriesByError {
		byError[k] = v
	}

	return RetryMetricsSnapshot{
		TotalRetries:    m.totalRetries,
		RetriesByType:   byType,
		RetriesByError:  byError,
		BackoffDuration: m.backoffDuration,
	}
}
