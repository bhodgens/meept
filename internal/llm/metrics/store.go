// Package metrics provides HTTP-level observability for LLM provider requests.
package metrics

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/sqlite"
)

// ErrorType categorizes HTTP/request errors.
type ErrorType string

const (
	ErrorTypeNone      ErrorType = "none"
	ErrorTypeTimeout   ErrorType = "timeout"
	ErrorTypeRateLimit ErrorType = "rate_limit"
	ErrorTypeNetwork   ErrorType = "network"
	ErrorTypeAuth      ErrorType = "auth"
	ErrorTypeServer    ErrorType = "server"
	ErrorTypeOther     ErrorType = "other"
)

// RequestRecord captures metrics for a single LLM provider request.
type RequestRecord struct {
	Timestamp        time.Time
	ProviderID       string
	ModelID          string
	PromptTokens     int
	CompletionTokens int
	LatencyMs        int64
	TTFBMs           int64 // Time to first byte; 0 for non-streaming
	HTTPStatus       int
	ErrorType        ErrorType
	ErrorMessage     string
	Success          bool
}

// ProviderStats holds aggregated statistics for a provider/model within a time window.
type ProviderStats struct {
	ProviderID   string
	ModelID      string
	WindowHours  int
	RequestCount int64
	ErrorCount   int64
	ErrorRate    float64
	AvgLatencyMs float64
	P50LatencyMs float64
	P95LatencyMs float64
	P99LatencyMs float64
	AvgTokensSec float64
	LastUpdated  time.Time
}

// StoreConfig configures the metrics store.
type StoreConfig struct {
	DBPath           string
	RetentionDays    int
	StatsWindowHours int
	RefreshInterval  time.Duration
	Logger           *slog.Logger
}

// Store manages provider request metrics in SQLite.
type Store struct {
	pool             *sqlite.Pool
	config           StoreConfig
	logger           *slog.Logger
	mu               sync.Mutex
	closed           bool
	refreshTicker    *time.Ticker
	refreshCtx       context.Context
	refreshCancel    context.CancelFunc
	recordChan       chan RequestRecord // queue for async recording
	recordWorkerDone chan struct{}
	workerStarted    bool
}

// NewStore creates a new metrics store.
func NewStore(cfg StoreConfig) (*Store, error) {
	if cfg.DBPath == "" {
		return nil, errors.New("db_path is required")
	}
	if cfg.RetentionDays <= 0 {
		cfg.RetentionDays = 7
	}
	if cfg.StatsWindowHours <= 0 {
		cfg.StatsWindowHours = 24
	}
	if cfg.RefreshInterval <= 0 {
		cfg.RefreshInterval = 5 * time.Minute
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	pool, err := sqlite.NewPool(sqlite.PoolConfig{
		Path:     cfg.DBPath,
		PoolSize: 3,
		WALMode:  true,
		Logger:   cfg.Logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create db pool: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())

	s := &Store{
		pool:             pool,
		config:           cfg,
		logger:           cfg.Logger,
		refreshCtx:       ctx,
		refreshCancel:    cancel,
		recordChan:       make(chan RequestRecord, 1000),
		recordWorkerDone: make(chan struct{}),
	}

	return s, nil
}

// Initialize creates database schema if needed.
func (s *Store) Initialize(ctx context.Context) error {
	return s.pool.WithConn(ctx, func(db *sql.DB) error {
		const schema = `
CREATE TABLE IF NOT EXISTS provider_requests (
    id                INTEGER PRIMARY KEY AUTOINCREMENT,
    ts                INTEGER NOT NULL,
    provider_id       TEXT NOT NULL,
    model_id          TEXT NOT NULL,
    prompt_tokens     INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    latency_ms        INTEGER NOT NULL DEFAULT 0,
    ttfb_ms           INTEGER NOT NULL DEFAULT 0,
    http_status       INTEGER NOT NULL DEFAULT 0,
    error_type        TEXT NOT NULL DEFAULT 'none',
    error_message     TEXT NOT NULL DEFAULT '',
    success           INTEGER NOT NULL DEFAULT 0
);

CREATE INDEX IF NOT EXISTS idx_pr_provider_ts ON provider_requests(provider_id, ts);
CREATE INDEX IF NOT EXISTS idx_pr_model_ts ON provider_requests(model_id, ts);

CREATE TABLE IF NOT EXISTS provider_stats (
    provider_id    TEXT NOT NULL,
    model_id       TEXT NOT NULL,
    window_hours   INTEGER NOT NULL,
    request_count  INTEGER NOT NULL DEFAULT 0,
    error_count    INTEGER NOT NULL DEFAULT 0,
    error_rate     REAL NOT NULL DEFAULT 0,
    avg_latency    REAL NOT NULL DEFAULT 0,
    p50_latency    REAL NOT NULL DEFAULT 0,
    p95_latency    REAL NOT NULL DEFAULT 0,
    p99_latency    REAL NOT NULL DEFAULT 0,
    avg_tokens_sec REAL NOT NULL DEFAULT 0,
    updated_at     INTEGER NOT NULL,
    PRIMARY KEY (provider_id, model_id, window_hours)
);
`
		_, err := db.ExecContext(ctx, schema)
		return err
	})
}

// Record logs a provider request asynchronously.
// It queues the record for async processing; the caller is not blocked.
func (s *Store) Record(ctx context.Context, r RequestRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.closed {
		return errors.New("store is closed")
	}

	select {
	case s.recordChan <- r:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	default:
		// Channel full; silently drop (acceptable for metrics)
		return nil
	}
}

// recordWorker processes the record queue.
func (s *Store) recordWorker() {
	defer close(s.recordWorkerDone)

	for {
		select {
		case r, ok := <-s.recordChan:
			if !ok {
				return
			}
			s.recordSync(context.Background(), r)
		case <-s.refreshCtx.Done():
			return
		}
	}
}

// recordSync synchronously stores a request record in the database.
func (s *Store) recordSync(ctx context.Context, r RequestRecord) {
	if err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		const q = `
INSERT INTO provider_requests (ts, provider_id, model_id, prompt_tokens, completion_tokens, latency_ms, ttfb_ms, http_status, error_type, error_message, success)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
		ts := r.Timestamp.UnixMilli()
		success := 0
		if r.Success {
			success = 1
		}
		_, err := db.ExecContext(ctx, q, ts, r.ProviderID, r.ModelID, r.PromptTokens, r.CompletionTokens, r.LatencyMs, r.TTFBMs, r.HTTPStatus, string(r.ErrorType), r.ErrorMessage, success)
		return err
	}); err != nil {
		s.logger.Debug("failed to record metric", "error", err)
	}
}

// GetStats retrieves aggregated statistics for a provider/model pair.
func (s *Store) GetStats(ctx context.Context, providerID, modelID string, windowHours int) (*ProviderStats, error) {
	var stats ProviderStats
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		const q = `
SELECT provider_id, model_id, window_hours, request_count, error_count, error_rate, avg_latency, p50_latency, p95_latency, p99_latency, avg_tokens_sec, updated_at
FROM provider_stats
WHERE provider_id = ? AND model_id = ? AND window_hours = ?
LIMIT 1
`
		row := db.QueryRowContext(ctx, q, providerID, modelID, windowHours)
		var updatedAtUnix int64
		err := row.Scan(&stats.ProviderID, &stats.ModelID, &stats.WindowHours, &stats.RequestCount, &stats.ErrorCount, &stats.ErrorRate, &stats.AvgLatencyMs, &stats.P50LatencyMs, &stats.P95LatencyMs, &stats.P99LatencyMs, &stats.AvgTokensSec, &updatedAtUnix)
		if err == sql.ErrNoRows {
			return nil
		}
		stats.LastUpdated = time.UnixMilli(updatedAtUnix)
		return err
	})
	return &stats, err
}

// GetAllStats retrieves statistics for all providers/models in a window.
func (s *Store) GetAllStats(ctx context.Context, windowHours int) ([]*ProviderStats, error) {
	var allStats []*ProviderStats
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		const q = `
SELECT provider_id, model_id, window_hours, request_count, error_count, error_rate, avg_latency, p50_latency, p95_latency, p99_latency, avg_tokens_sec, updated_at
FROM provider_stats
WHERE window_hours = ?
ORDER BY provider_id, model_id
`
		rows, err := db.QueryContext(ctx, q, windowHours)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var stats ProviderStats
			var updatedAtUnix int64
			if err := rows.Scan(&stats.ProviderID, &stats.ModelID, &stats.WindowHours, &stats.RequestCount, &stats.ErrorCount, &stats.ErrorRate, &stats.AvgLatencyMs, &stats.P50LatencyMs, &stats.P95LatencyMs, &stats.P99LatencyMs, &stats.AvgTokensSec, &updatedAtUnix); err != nil {
				return err
			}
			stats.LastUpdated = time.UnixMilli(updatedAtUnix)
			allStats = append(allStats, &stats)
		}
		return rows.Err()
	})
	return allStats, err
}

// GetLatencies returns all latency_ms values for a provider/model within the window, sorted.
// Used by the adaptive timeout calculator.
func (s *Store) GetLatencies(ctx context.Context, providerID, modelID string, windowHours int) ([]float64, error) {
	var latencies []float64
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		cutoffTime := time.Now().Add(-time.Duration(windowHours) * time.Hour).UnixMilli()
		const q = `
SELECT latency_ms FROM provider_requests
WHERE provider_id = ? AND model_id = ? AND ts > ? AND success = 1
ORDER BY ts DESC
`
		rows, err := db.QueryContext(ctx, q, providerID, modelID, cutoffTime)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var latencyMs int64
			if err := rows.Scan(&latencyMs); err != nil {
				return err
			}
			latencies = append(latencies, float64(latencyMs))
		}
		return rows.Err()
	})
	return latencies, err
}

// GetTokenRates returns token rate (latency_ms / completion_tokens) for successful requests.
// Used by the adaptive timeout calculator in token-rate mode.
func (s *Store) GetTokenRates(ctx context.Context, providerID, modelID string, windowHours int) ([]float64, error) {
	var rates []float64
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		cutoffTime := time.Now().Add(-time.Duration(windowHours) * time.Hour).UnixMilli()
		const q = `
SELECT CAST(latency_ms AS REAL) / completion_tokens FROM provider_requests
WHERE provider_id = ? AND model_id = ? AND ts > ? AND success = 1 AND completion_tokens > 0
ORDER BY ts DESC
`
		rows, err := db.QueryContext(ctx, q, providerID, modelID, cutoffTime)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var rate float64
			if err := rows.Scan(&rate); err != nil {
				return err
			}
			rates = append(rates, rate)
		}
		return rows.Err()
	})
	return rates, err
}

// RefreshStats recomputes percentiles for all provider/model pairs in the window.
// Called periodically by the background worker.
func (s *Store) RefreshStats(ctx context.Context) error {
	cutoffTime := time.Now().Add(-time.Duration(s.config.RetentionDays*24) * time.Hour).UnixMilli()

	return s.pool.WithConn(ctx, func(db *sql.DB) error {
		// Get all unique provider/model combinations in the window
		const q = `
SELECT DISTINCT provider_id, model_id FROM provider_requests
WHERE ts > ?
`
		rows, err := db.QueryContext(ctx, q, cutoffTime)
		if err != nil {
			return err
		}
		defer rows.Close()

		var pairs []struct {
			providerID string
			modelID    string
		}
		for rows.Next() {
			var providerID, modelID string
			if err := rows.Scan(&providerID, &modelID); err != nil {
				return err
			}
			pairs = append(pairs, struct {
				providerID string
				modelID    string
			}{providerID, modelID})
		}
		if err := rows.Err(); err != nil {
			return err
		}

		// Compute stats for each pair
		windowStart := time.Now().Add(-time.Duration(s.config.StatsWindowHours) * time.Hour).UnixMilli()
		for _, pair := range pairs {
			if err := s.refreshStatsForPair(ctx, db, pair.providerID, pair.modelID, windowStart); err != nil {
				s.logger.Debug("failed to refresh stats", "provider", pair.providerID, "model", pair.modelID, "error", err)
			}
		}

		return nil
	})
}

// refreshStatsForPair computes and stores stats for a single provider/model pair.
func (s *Store) refreshStatsForPair(ctx context.Context, db *sql.DB, providerID, modelID string, windowStart int64) error {
	const qCount = `
SELECT COUNT(*), SUM(CASE WHEN success = 0 THEN 1 ELSE 0 END)
FROM provider_requests
WHERE provider_id = ? AND model_id = ? AND ts > ?
`
	row := db.QueryRowContext(ctx, qCount, providerID, modelID, windowStart)
	var requestCount, errorCount int64
	if err := row.Scan(&requestCount, &errorCount); err != nil {
		return err
	}

	if requestCount == 0 {
		return nil // No data, skip
	}

	// Get latencies for percentile computation
	const qLatencies = `
SELECT latency_ms FROM provider_requests
WHERE provider_id = ? AND model_id = ? AND ts > ? AND success = 1
ORDER BY latency_ms
`
	rows, err := db.QueryContext(ctx, qLatencies, providerID, modelID, windowStart)
	if err != nil {
		return err
	}
	defer rows.Close()

	var latencies []float64
	var totalLatencyMs int64
	var successCount int64
	for rows.Next() {
		var latencyMs int64
		if err := rows.Scan(&latencyMs); err != nil {
			return err
		}
		latencies = append(latencies, float64(latencyMs))
		totalLatencyMs += latencyMs
		successCount++
	}
	if err := rows.Err(); err != nil {
		return err
	}

	// Compute percentiles from sorted latencies
	p50, p95, p99 := computePercentiles(latencies)
	avgLatency := 0.0
	if successCount > 0 {
		avgLatency = float64(totalLatencyMs) / float64(successCount)
	}

	// Get token rate (avg tokens per second)
	//nolint:gosec // field name, not a secret
	const qTokens = `
SELECT SUM(completion_tokens), SUM(latency_ms) FROM provider_requests
WHERE provider_id = ? AND model_id = ? AND ts > ? AND success = 1
`
	rowTokens := db.QueryRowContext(ctx, qTokens, providerID, modelID, windowStart)
	var totalCompTokens int64
	var totalLatencyMsForRate int64
	if err := rowTokens.Scan(&totalCompTokens, &totalLatencyMsForRate); err != nil && err != sql.ErrNoRows {
		return err
	}

	avgTokensSec := 0.0
	if totalLatencyMsForRate > 0 && totalCompTokens > 0 {
		avgTokensSec = float64(totalCompTokens) / (float64(totalLatencyMsForRate) / 1000.0)
	}

	// Upsert into provider_stats
	const upsert = `
INSERT INTO provider_stats (provider_id, model_id, window_hours, request_count, error_count, error_rate, avg_latency, p50_latency, p95_latency, p99_latency, avg_tokens_sec, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(provider_id, model_id, window_hours) DO UPDATE SET
    request_count = excluded.request_count,
    error_count = excluded.error_count,
    error_rate = excluded.error_rate,
    avg_latency = excluded.avg_latency,
    p50_latency = excluded.p50_latency,
    p95_latency = excluded.p95_latency,
    p99_latency = excluded.p99_latency,
    avg_tokens_sec = excluded.avg_tokens_sec,
    updated_at = excluded.updated_at
`
	errorRate := 0.0
	if requestCount > 0 {
		errorRate = float64(errorCount) / float64(requestCount)
	}

	now := time.Now().UnixMilli()
	_, err = db.ExecContext(ctx, upsert, providerID, modelID, s.config.StatsWindowHours, requestCount, errorCount, errorRate, avgLatency, p50, p95, p99, avgTokensSec, now)
	return err
}

// computePercentiles computes p50, p95, p99 from a sorted slice of floats.
func computePercentiles(sorted []float64) (p50, p95, p99 float64) {
	if len(sorted) == 0 {
		return 0, 0, 0
	}

	// Ensure sorted
	sort.Float64s(sorted)

	p50 = percentile(sorted, 50)
	p95 = percentile(sorted, 95)
	p99 = percentile(sorted, 99)
	return
}

// percentile computes the pth percentile of a sorted slice.
func percentile(sorted []float64, p int) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}

	// Linear interpolation between closest ranks
	rank := float64(p) / 100.0 * float64(len(sorted)-1)
	lower := int(rank)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	fraction := rank - float64(lower)
	return sorted[lower]*(1-fraction) + sorted[upper]*fraction
}

// PruneOld removes records older than the retention period.
func (s *Store) PruneOld(ctx context.Context) error {
	return s.pool.WithConn(ctx, func(db *sql.DB) error {
		cutoffTime := time.Now().Add(-time.Duration(s.config.RetentionDays*24) * time.Hour).UnixMilli()
		const q = `DELETE FROM provider_requests WHERE ts < ?`
		_, err := db.ExecContext(ctx, q, cutoffTime)
		return err
	})
}

// StartBackground starts the background refresh and prune goroutine.
// Should be called once after Initialize.
func (s *Store) StartBackground(ctx context.Context) {
	s.mu.Lock()
	if s.closed || s.workerStarted {
		s.mu.Unlock()
		return
	}
	s.workerStarted = true
	s.mu.Unlock()

	// Start the record worker
	//nolint:gosec // goroutine outlives request context
	go s.recordWorker()

	// Start the refresh/prune ticker
	s.refreshTicker = time.NewTicker(s.config.RefreshInterval)
	//nolint:gosec // goroutine outlives request context
	go func() {
		for {
			select {
			case <-s.refreshTicker.C:
				if err := s.RefreshStats(context.Background()); err != nil {
					s.logger.Debug("failed to refresh stats", "error", err)
				}
				if err := s.PruneOld(context.Background()); err != nil {
					s.logger.Debug("failed to prune old records", "error", err)
				}
			case <-s.refreshCtx.Done():
				return
			}
		}
	}()

	s.logger.Debug("metrics store background worker started", "refresh_interval", s.config.RefreshInterval)
}

// Close closes the store and its background workers.
func (s *Store) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	workerStarted := s.workerStarted
	s.mu.Unlock()

	// Stop refresh ticker
	if s.refreshTicker != nil {
		s.refreshTicker.Stop()
	}

	// Stop refresh context
	s.refreshCancel()

	// Close record channel and wait for worker only if it was started
	if workerStarted {
		close(s.recordChan)
		<-s.recordWorkerDone
	}

	s.logger.Debug("metrics store closed")
	return s.pool.Close()
}

// ClassifyError categorizes an error based on type and HTTP status.
// Used by both Client and AnthropicClient to populate ErrorType in RequestRecord.
func ClassifyError(err error, httpStatus int) ErrorType {
	// HTTP status takes precedence
	switch httpStatus {
	case 401, 403:
		return ErrorTypeAuth
	case 429:
		return ErrorTypeRateLimit
	case 500, 502, 503, 504:
		return ErrorTypeServer
	}

	// If no error and no bad status, assume success
	if err == nil {
		return ErrorTypeNone
	}

	errMsg := err.Error()

	// Timeout detection
	if errors.Is(err, context.DeadlineExceeded) || errMsg == "context deadline exceeded" {
		return ErrorTypeTimeout
	}

	// Network error detection (crude but serviceable)
	if errors.Is(err, context.Canceled) {
		return ErrorTypeNetwork
	}

	return ErrorTypeOther
}
