package shadow

import (
	"sync"
	"sync/atomic"
	"time"
)

// Metrics tracks shadow training system metrics.
type Metrics struct {
	// Record collection metrics
	RecordsCollected   atomic.Int64
	RecordsHighQuality atomic.Int64
	RecordsLowQuality  atomic.Int64

	// Teacher call metrics
	TeacherCalls       atomic.Int64
	TeacherCallsFailed atomic.Int64
	TeacherTokensIn    atomic.Int64
	TeacherTokensOut   atomic.Int64

	// Scoring distribution
	ScoreSum     atomic.Int64 // Sum * 1000 for precision
	ScoreCount   atomic.Int64
	ScoreBuckets [10]atomic.Int64 // 0.0-0.1, 0.1-0.2, ..., 0.9-1.0

	// Example selection metrics
	ExampleSelections  atomic.Int64
	ExampleCacheHits   atomic.Int64
	ExampleCacheMisses atomic.Int64

	// Export metrics
	ExportCount        atomic.Int64
	ExportRecordsTotal atomic.Int64

	// Timing metrics
	mu                  sync.RWMutex
	lastRecordTime      time.Time
	lastTeacherCallTime time.Time
	lastExportTime      time.Time

	// Start time for uptime calculation
	startTime time.Time
}

// NewMetrics creates a new metrics collector.
func NewMetrics() *Metrics {
	return &Metrics{
		startTime: time.Now(),
	}
}

// RecordCollected increments the record collection counter and updates scoring stats.
func (m *Metrics) RecordCollected(score float64, isHighQuality bool) {
	m.RecordsCollected.Add(1)
	if isHighQuality {
		m.RecordsHighQuality.Add(1)
	} else {
		m.RecordsLowQuality.Add(1)
	}

	// Update score distribution
	m.ScoreSum.Add(int64(score * 1000))
	m.ScoreCount.Add(1)

	// Update bucket
	bucket := int(score * 10)
	if bucket >= 10 {
		bucket = 9
	}
	if bucket < 0 {
		bucket = 0
	}
	m.ScoreBuckets[bucket].Add(1)

	m.mu.Lock()
	m.lastRecordTime = time.Now()
	m.mu.Unlock()
}

// TeacherCallCompleted records a successful teacher call.
func (m *Metrics) TeacherCallCompleted(tokensIn, tokensOut int) {
	m.TeacherCalls.Add(1)
	m.TeacherTokensIn.Add(int64(tokensIn))
	m.TeacherTokensOut.Add(int64(tokensOut))

	m.mu.Lock()
	m.lastTeacherCallTime = time.Now()
	m.mu.Unlock()
}

// TeacherCallFailed records a failed teacher call.
func (m *Metrics) TeacherCallFailed() {
	m.TeacherCalls.Add(1)
	m.TeacherCallsFailed.Add(1)
}

// ExampleSelected records an example selection operation.
func (m *Metrics) ExampleSelected(cacheHit bool) {
	m.ExampleSelections.Add(1)
	if cacheHit {
		m.ExampleCacheHits.Add(1)
	} else {
		m.ExampleCacheMisses.Add(1)
	}
}

// ExportCompleted records an export operation.
func (m *Metrics) ExportCompleted(recordsExported int) {
	m.ExportCount.Add(1)
	m.ExportRecordsTotal.Add(int64(recordsExported))

	m.mu.Lock()
	m.lastExportTime = time.Now()
	m.mu.Unlock()
}

// MetricsSnapshot represents a point-in-time snapshot of all metrics.
type MetricsSnapshot struct {
	// Record metrics
	RecordsCollected   int64   `json:"records_collected"`
	RecordsHighQuality int64   `json:"records_high_quality"`
	RecordsLowQuality  int64   `json:"records_low_quality"`
	AvgQualityScore    float64 `json:"avg_quality_score"`

	// Score distribution (percentage in each bucket)
	ScoreDistribution [10]float64 `json:"score_distribution"`

	// Teacher metrics
	TeacherCalls       int64   `json:"teacher_calls"`
	TeacherCallsFailed int64   `json:"teacher_calls_failed"`
	TeacherSuccessRate float64 `json:"teacher_success_rate"`
	TeacherTokensIn    int64   `json:"teacher_tokens_in"`
	TeacherTokensOut   int64   `json:"teacher_tokens_out"`

	// Example selection metrics
	ExampleSelections   int64   `json:"example_selections"`
	ExampleCacheHits    int64   `json:"example_cache_hits"`
	ExampleCacheMisses  int64   `json:"example_cache_misses"`
	ExampleCacheHitRate float64 `json:"example_cache_hit_rate"`

	// Export metrics
	ExportCount        int64 `json:"export_count"`
	ExportRecordsTotal int64 `json:"export_records_total"`

	// Timing
	LastRecordTime      *time.Time `json:"last_record_time,omitempty"`
	LastTeacherCallTime *time.Time `json:"last_teacher_call_time,omitempty"`
	LastExportTime      *time.Time `json:"last_export_time,omitempty"`
	UptimeSeconds       float64    `json:"uptime_seconds"`
}

// Snapshot returns a point-in-time snapshot of all metrics.
func (m *Metrics) Snapshot() *MetricsSnapshot {
	s := &MetricsSnapshot{
		RecordsCollected:   m.RecordsCollected.Load(),
		RecordsHighQuality: m.RecordsHighQuality.Load(),
		RecordsLowQuality:  m.RecordsLowQuality.Load(),

		TeacherCalls:       m.TeacherCalls.Load(),
		TeacherCallsFailed: m.TeacherCallsFailed.Load(),
		TeacherTokensIn:    m.TeacherTokensIn.Load(),
		TeacherTokensOut:   m.TeacherTokensOut.Load(),

		ExampleSelections:  m.ExampleSelections.Load(),
		ExampleCacheHits:   m.ExampleCacheHits.Load(),
		ExampleCacheMisses: m.ExampleCacheMisses.Load(),

		ExportCount:        m.ExportCount.Load(),
		ExportRecordsTotal: m.ExportRecordsTotal.Load(),

		UptimeSeconds: time.Since(m.startTime).Seconds(),
	}

	// Compute averages and rates
	scoreCount := m.ScoreCount.Load()
	if scoreCount > 0 {
		s.AvgQualityScore = float64(m.ScoreSum.Load()) / float64(scoreCount) / 1000.0
	}

	// Score distribution
	for i := range 10 {
		if scoreCount > 0 {
			s.ScoreDistribution[i] = float64(m.ScoreBuckets[i].Load()) / float64(scoreCount) * 100.0
		}
	}

	// Success rates
	if s.TeacherCalls > 0 {
		s.TeacherSuccessRate = float64(s.TeacherCalls-s.TeacherCallsFailed) / float64(s.TeacherCalls) * 100.0
	}

	if s.ExampleSelections > 0 {
		s.ExampleCacheHitRate = float64(s.ExampleCacheHits) / float64(s.ExampleSelections) * 100.0
	}

	// Timing (copy under lock)
	m.mu.RLock()
	if !m.lastRecordTime.IsZero() {
		t := m.lastRecordTime
		s.LastRecordTime = &t
	}
	if !m.lastTeacherCallTime.IsZero() {
		t := m.lastTeacherCallTime
		s.LastTeacherCallTime = &t
	}
	if !m.lastExportTime.IsZero() {
		t := m.lastExportTime
		s.LastExportTime = &t
	}
	m.mu.RUnlock()

	return s
}

// Reset resets all metrics to zero.
func (m *Metrics) Reset() {
	m.RecordsCollected.Store(0)
	m.RecordsHighQuality.Store(0)
	m.RecordsLowQuality.Store(0)

	m.TeacherCalls.Store(0)
	m.TeacherCallsFailed.Store(0)
	m.TeacherTokensIn.Store(0)
	m.TeacherTokensOut.Store(0)

	m.ScoreSum.Store(0)
	m.ScoreCount.Store(0)
	for i := range 10 {
		m.ScoreBuckets[i].Store(0)
	}

	m.ExampleSelections.Store(0)
	m.ExampleCacheHits.Store(0)
	m.ExampleCacheMisses.Store(0)

	m.ExportCount.Store(0)
	m.ExportRecordsTotal.Store(0)

	m.mu.Lock()
	m.lastRecordTime = time.Time{}
	m.lastTeacherCallTime = time.Time{}
	m.lastExportTime = time.Time{}
	m.startTime = time.Now()
	m.mu.Unlock()
}
