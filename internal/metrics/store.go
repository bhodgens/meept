// Package metrics provides metrics collection and storage for Meept.
package metrics

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	"github.com/jmoiron/sqlx"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

const (
	// DefaultDatabasePath is the default path for the metrics database.
	DefaultDatabasePath = "~/.meept/metrics.db"

	// DefaultRetentionDays is how long to keep raw metrics data.
	DefaultRetentionDays = 30

	// DefaultBatchSize is the number of metrics to batch before writing.
	DefaultBatchSize = 100

	// DefaultFlushInterval is how often to flush batched metrics.
	DefaultFlushInterval = 10 * time.Second
)

// Store manages metrics storage.
type Store struct {
	logger        *slog.Logger
	mu            sync.RWMutex
	db            *sqlx.DB
	batchSize     int
	flushInterval time.Duration
	batch         []metricValue
	lastFlush     time.Time
	stopChan      chan struct{}
	closeOnce     sync.Once

	// Subscriber management for real-time updates
	subMu       sync.RWMutex
	subscribers map[chan *LiveMetricsSnapshot]struct{}
}

// metricValue represents a single metric value to store.
type metricValue struct {
	name      string
	value     float64
	tags      map[string]string
	timestamp time.Time
}

// StoreConfig configures the metrics store.
type StoreConfig struct {
	DatabasePath  string
	BatchSize     int
	FlushInterval time.Duration
	RetentionDays int
}

// DefaultStoreConfig returns default store configuration.
func DefaultStoreConfig() *StoreConfig {
	return &StoreConfig{
		DatabasePath:  DefaultDatabasePath,
		BatchSize:     DefaultBatchSize,
		FlushInterval: DefaultFlushInterval,
		RetentionDays: DefaultRetentionDays,
	}
}

// NewStore creates a new metrics store.
func NewStore(cfg *StoreConfig) (*Store, error) {
	if cfg == nil {
		cfg = DefaultStoreConfig()
	}

	// Expand path
	dbPath := expandPath(cfg.DatabasePath)

	// Ensure directory exists
	dir := filepath.Dir(dbPath)
	//nolint:gosec // user config directory/file permissions
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("failed to create metrics directory: %w", err)
	}

	// Open database
	rawDB, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := sqlx.NewDb(rawDB, "sqlite")

	// Set connection pool settings
	db.SetMaxOpenConns(1) // SQLite writes must be serialized
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(time.Hour)

	store := &Store{
		db:            db,
		batchSize:     cfg.BatchSize,
		flushInterval: cfg.FlushInterval,
		batch:         make([]metricValue, 0, cfg.BatchSize),
		lastFlush:     time.Now(),
		stopChan:      make(chan struct{}),
		logger:        slog.Default().With("component", "metrics-store"),
		subscribers:   make(map[chan *LiveMetricsSnapshot]struct{}),
	}

	// Initialize database schema
	if err := store.initSchema(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to initialize schema: %w", err)
	}

	// Start background flush goroutine
	go store.flushLoop()

	// Start hourly aggregation goroutine
	go store.aggregationLoop()

	return store, nil
}

// initSchema creates the database schema if it doesn't exist.
func (s *Store) initSchema() error {
	schema := `
-- Time-series metrics (1-second resolution)
CREATE TABLE IF NOT EXISTS metrics_live (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    metric_name TEXT NOT NULL,
    value REAL NOT NULL,
    tags TEXT  -- JSON for labels like agent_id, model_id
);

-- Aggregated hourly stats for historical reports
CREATE TABLE IF NOT EXISTS metrics_hourly (
    hour DATETIME NOT NULL,
    metric_name TEXT NOT NULL,
    sum_value REAL,
    avg_value REAL,
    min_value REAL,
    max_value REAL,
    count INTEGER,
    PRIMARY KEY (hour, metric_name)
);

-- Event log for discrete events
CREATE TABLE IF NOT EXISTS events (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp DATETIME DEFAULT CURRENT_TIMESTAMP,
    event_type TEXT NOT NULL,
    severity TEXT,  -- info, warn, error
    message TEXT,
    context TEXT    -- JSON
);

-- Response quality metrics for LLM responses
CREATE TABLE IF NOT EXISTS response_quality (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    message_id TEXT,
    is_well_formed BOOLEAN,
    parse_errors TEXT,
    has_code_blocks BOOLEAN,
    has_explanations BOOLEAN,
    is_lazy BOOLEAN,
    lazy_reason TEXT,
    token_count INTEGER,
    code_token_pct REAL
);

-- Model performance aggregation
CREATE TABLE IF NOT EXISTS model_performance (
    id              INTEGER PRIMARY KEY AUTOINCREMENT,
    model_id        TEXT NOT NULL,
    provider        TEXT NOT NULL DEFAULT '',
    total_requests  INTEGER NOT NULL DEFAULT 0,
    total_errors    INTEGER NOT NULL DEFAULT 0,
    avg_latency_ms  REAL NOT NULL DEFAULT 0,
    avg_tokens_in   REAL NOT NULL DEFAULT 0,
    avg_tokens_out  REAL NOT NULL DEFAULT 0,
    period_start    TEXT NOT NULL,
    period_end      TEXT NOT NULL,
    updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    UNIQUE(model_id, provider, period_start)
);

-- Error records for retry tracking
CREATE TABLE IF NOT EXISTS error_records (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    provider       TEXT NOT NULL DEFAULT '',
    model_id       TEXT NOT NULL DEFAULT '',
    error_type     TEXT NOT NULL DEFAULT '',
    error_message  TEXT NOT NULL DEFAULT '',
    limit_type     TEXT NOT NULL DEFAULT '',
    retry_attempts INTEGER NOT NULL DEFAULT 0,
    final_outcome  TEXT NOT NULL DEFAULT ''
);

-- Linting and test metrics (for auto-lint/test reflection loop)
CREATE TABLE IF NOT EXISTS lint_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    language TEXT,
    files_checked INTEGER,
    linters_runned INTEGER,
    errors_found INTEGER,
    errors_fixed INTEGER,
    duration_ms INTEGER,
    reflection_iterations INTEGER,
    success BOOLEAN
);

CREATE TABLE IF NOT EXISTS test_runs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    timestamp TEXT DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now')),
    task_id TEXT,
    agent_id TEXT,
    language TEXT,
    tests_run INTEGER,
    tests_passed INTEGER,
    tests_failed INTEGER,
    tests_skipped INTEGER,
    duration_ms INTEGER,
    reflection_iterations INTEGER,
    success BOOLEAN
);

-- Indexes for query performance
CREATE INDEX IF NOT EXISTS idx_metrics_live_ts ON metrics_live(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_live_name ON metrics_live(metric_name);
CREATE INDEX IF NOT EXISTS idx_metrics_hourly_ts ON metrics_hourly(hour DESC);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
CREATE INDEX IF NOT EXISTS idx_model_performance_period ON model_performance(period_start DESC);
CREATE INDEX IF NOT EXISTS idx_model_performance_model ON model_performance(model_id);
CREATE INDEX IF NOT EXISTS idx_error_records_ts ON error_records(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_error_records_model ON error_records(model_id);
CREATE INDEX IF NOT EXISTS idx_lint_runs_task ON lint_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_lint_runs_ts ON lint_runs(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_test_runs_task ON test_runs(task_id);
CREATE INDEX IF NOT EXISTS idx_test_runs_ts ON test_runs(timestamp DESC);
`

	_, err := s.db.Exec(schema)
	return err
}

// flushLoop periodically flushes batched metrics.
func (s *Store) flushLoop() {
	ticker := time.NewTicker(s.flushInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.flush()
		case <-s.stopChan:
			s.flush() // Final flush
			return
		}
	}
}

// aggregationLoop runs hourly aggregations.
func (s *Store) aggregationLoop() {
	ticker := time.NewTicker(time.Hour)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.aggregateHourly()
		case <-s.stopChan:
			return
		}
	}
}

// flush writes batched metrics to the database.
func (s *Store) flush() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if len(s.batch) == 0 {
		return
	}

	tx, err := s.db.Begin()
	if err != nil {
		return
	}
	defer func() { _ = tx.Rollback() }()

	stmt, err := tx.Prepare("INSERT INTO metrics_live (timestamp, metric_name, value, tags) VALUES (?, ?, ?, ?)")
	if err != nil {
		return
	}
	defer stmt.Close()

	for _, m := range s.batch {
		var tagsJSON string
		if len(m.tags) > 0 {
			data, _ := json.Marshal(m.tags)
			tagsJSON = string(data)
		}
		_, _ = stmt.Exec(m.timestamp, m.name, m.value, tagsJSON)
	}

	if err := tx.Commit(); err == nil {
		s.batch = s.batch[:0]
		s.lastFlush = time.Now()

		// Notify subscribers after successful flush
		go s.notifySubscribers()
	}
}

// aggregateHourly computes hourly aggregations from raw metrics.
func (s *Store) aggregateHourly() {
	query := `
	INSERT OR REPLACE INTO metrics_hourly (hour, metric_name, sum_value, avg_value, min_value, max_value, count)
	SELECT
		strftime('%Y-%m-%d %H:00:00', timestamp) as hour,
		metric_name,
		SUM(value),
		AVG(value),
		MIN(value),
		MAX(value),
		COUNT(*)
	FROM metrics_live
	WHERE timestamp < datetime('now', '-1 hour')
	GROUP BY hour, metric_name
	`

	_, err := s.db.Exec(query)
	if err != nil {
		s.logger.Error("failed to aggregate hourly metrics", "error", err)
		return
	}

	// Clean up old raw data (keep last 24 hours)
	_, _ = s.db.Exec("DELETE FROM metrics_live WHERE timestamp < datetime('now', '-24 hours')")
}

// Record records a metric value.
// Note: The shouldFlush check happens while holding the lock to ensure atomicity.
// The flush call releases the lock internally via flush() -> s.mu.Lock().
func (s *Store) Record(name string, value float64, tags map[string]string) {
	s.mu.Lock()

	s.batch = append(s.batch, metricValue{
		name:      name,
		value:     value,
		tags:      tags,
		timestamp: time.Now(),
	})

	// Check if batch is full while holding lock for atomic read
	shouldFlush := len(s.batch) >= s.batchSize
	s.mu.Unlock()

	// Flush outside of lock to allow other Record calls to proceed.
	// flush() will acquire the lock, and concurrent Record calls will
	// append to batch and potentially trigger additional flushes.
	if shouldFlush {
		s.flush()
	}
}

// RecordEvent records a discrete event.
func (s *Store) RecordEvent(eventType, severity, message string, context map[string]any) {
	ctxJSON := ""
	if len(context) > 0 {
		data, _ := json.Marshal(context)
		ctxJSON = string(data)
	}

	_, err := s.db.Exec(
		"INSERT INTO events (timestamp, event_type, severity, message, context) VALUES (?, ?, ?, ?, ?)",
		time.Now(), eventType, severity, message, ctxJSON,
	)
	if err != nil {
		s.logger.Error("failed to record event", "error", err, "event_type", eventType)
		return
	}
}

// GetLiveMetrics returns current live metrics snapshot.
func (s *Store) GetLiveMetrics() (*LiveMetricsSnapshot, error) {
	metrics := &LiveMetricsSnapshot{
		Timestamp: time.Now(),
		Metadata:  make(map[string]any),
	}

	// Get active agents (count distinct agent_id tags in last minute)
	query := `
	SELECT COUNT(DISTINCT json_extract(tags, '$.agent_id'))
	FROM metrics_live
	WHERE metric_name = 'agent.active'
	AND timestamp > datetime('now', '-1 minute')
	`
	err := s.db.QueryRow(query).Scan(&metrics.ActiveAgents)
	if err != nil {
		metrics.ActiveAgents = 0
	}

	// Get requests per second (count requests in last second)
	query = `
	SELECT COUNT(*) FROM metrics_live
	WHERE metric_name = 'request.count'
	AND timestamp > datetime('now', '-1 second')
	`
	var reqCount int
	err = s.db.QueryRow(query).Scan(&reqCount)
	if err == nil {
		metrics.RequestsPerSec = float64(reqCount)
	}

	// Get token usage rate (sum tokens in last minute / 60)
	query = `
	SELECT COALESCE(SUM(value), 0) FROM metrics_live
	WHERE metric_name IN ('tokens.input', 'tokens.output')
	AND timestamp > datetime('now', '-1 minute')
	`
	var tokenSum float64
	err = s.db.QueryRow(query).Scan(&tokenSum)
	if err == nil {
		metrics.TokenUsageRate = tokenSum / 60.0
	}

	// Get queue depth (last value)
	query = `
	SELECT value FROM metrics_live
	WHERE metric_name = 'queue.depth'
	ORDER BY timestamp DESC LIMIT 1
	`
	err = s.db.QueryRow(query).Scan(&metrics.QueueDepth)
	if err != nil {
		metrics.QueueDepth = 0
	}

	// Get model failover events (count in last hour)
	query = `
	SELECT COUNT(*) FROM events
	WHERE event_type = 'model.failover'
	AND timestamp > datetime('now', '-1 hour')
	`
	err = s.db.QueryRow(query).Scan(&metrics.ModelFailovers)
	if err != nil {
		metrics.ModelFailovers = 0
	}

	return metrics, nil
}

// LiveMetricsSnapshot represents a point-in-time metrics snapshot.
type LiveMetricsSnapshot struct {
	Timestamp      time.Time      `json:"timestamp"`
	ActiveAgents   int            `json:"active_agents"`
	RequestsPerSec float64        `json:"requests_per_sec"`
	TokenUsageRate float64        `json:"token_usage_rate"`
	QueueDepth     int            `json:"queue_depth"`
	ModelFailovers int            `json:"model_failovers"`
	Metadata       map[string]any `json:"metadata,omitempty"`
}

// GetHistoricalMetrics returns historical metrics for a time range.
func (s *Store) GetHistoricalMetrics(from, to time.Time, resolution string) ([]MetricPoint, error) {
	var query string

	switch resolution {
	case "minute":
		query = `
		SELECT timestamp AS timestamp, metric_name AS name, value, tags
		FROM metrics_live
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC
		`
	case "hour":
		query = `
		SELECT hour AS timestamp, metric_name AS name, avg_value AS value, '' AS tags
		FROM metrics_hourly
		WHERE hour BETWEEN ? AND ?
		ORDER BY hour ASC
		`
	case "day", "week":
		query = `
		SELECT date(hour) AS timestamp, metric_name AS name, avg_value AS value, '' AS tags
		FROM metrics_hourly
		WHERE hour BETWEEN ? AND ?
		ORDER BY hour ASC
		`
	default:
		query = `
		SELECT hour AS timestamp, metric_name AS name, avg_value AS value, '' AS tags
		FROM metrics_hourly
		WHERE hour BETWEEN ? AND ?
		ORDER BY hour ASC
		`
	}

	rows, err := s.db.Queryx(query, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []MetricPoint
	for rows.Next() {
		// metricRow is a scan-only struct matching the aliased SQL columns.
		type metricRow struct {
			Timestamp time.Time `db:"timestamp"`
			Name      string    `db:"name"`
			Value     float64   `db:"value"`
			Tags      string    `db:"tags"`
		}
		var r metricRow
		if err := rows.StructScan(&r); err != nil {
			continue
		}

		point := MetricPoint{
			Timestamp: r.Timestamp,
			Name:      r.Name,
			Value:     r.Value,
		}
		if r.Tags != "" {
			_ = json.Unmarshal([]byte(r.Tags), &point.Tags)
		}

		points = append(points, point)
	}

	return points, rows.Err()
}

// MetricPoint represents a single metric data point.
type MetricPoint struct {
	Timestamp time.Time         `json:"timestamp" db:"timestamp"`
	Name      string            `json:"name" db:"metric_name"`
	Value     float64           `json:"value" db:"value"`
	Tags      map[string]string `json:"tags,omitempty" db:"-"`
}

// Close closes the metrics store. Safe to call multiple times.
func (s *Store) Close() error {
	var dbErr error
	s.closeOnce.Do(func() {
		close(s.stopChan)
		// Final flush
		s.flush()
		dbErr = s.db.Close()
	})
	return dbErr
}

// expandPath expands ~ to the home directory.
// Compile-time assertion that Store implements io.Closer.
var _ io.Closer = (*Store)(nil)

func expandPath(path string) string {
	if path == "" || path[0] != '~' {
		return path
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		if u, err := user.Current(); err == nil {
			homeDir = u.HomeDir
		} else {
			return path
		}
	}

	if path == "~" {
		return homeDir
	}
	return filepath.Join(homeDir, path[2:])
}

// GetEvents returns recent events.
func (s *Store) GetEvents(limit int, severity string) ([]Event, error) {
	query := `
	SELECT timestamp, event_type, severity, message, context
	FROM events
	`

	args := []any{}
	if severity != "" {
		query += " WHERE severity = ?"
		args = append(args, severity)
	}

	query += " ORDER BY timestamp DESC LIMIT ?"
	args = append(args, limit)

	rows, err := s.db.Queryx(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		// eventRow is a scan-only struct matching the raw SQL columns.
		type eventRow struct {
			Timestamp time.Time `db:"timestamp"`
			EventType string    `db:"event_type"`
			Severity  string    `db:"severity"`
			Message   string    `db:"message"`
			Context   string    `db:"context"`
		}
		var r eventRow
		if err := rows.StructScan(&r); err != nil {
			continue
		}

		event := Event{
			Timestamp: r.Timestamp,
			EventType: r.EventType,
			Severity:  r.Severity,
			Message:   r.Message,
		}
		if r.Context != "" {
			_ = json.Unmarshal([]byte(r.Context), &event.Context)
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

// Event represents a logged event.
type Event struct {
	Timestamp time.Time      `json:"timestamp" db:"timestamp"`
	EventType string         `json:"event_type" db:"event_type"`
	Severity  string         `json:"severity" db:"severity"`
	Message   string         `json:"message" db:"message"`
	Context   map[string]any `json:"context,omitempty" db:"-"`
}

// ModelPerformanceRecord represents aggregated model performance metrics.
type ModelPerformanceRecord struct {
	ID             int64   `db:"id" json:"id"`
	ModelID        string  `db:"model_id" json:"model_id"`
	Provider       string  `db:"provider" json:"provider"`
	TotalRequests  int     `db:"total_requests" json:"total_requests"`
	TotalErrors    int     `db:"total_errors" json:"total_errors"`
	AvgLatencyMs   float64 `db:"avg_latency_ms" json:"avg_latency_ms"`
	AvgTokensIn    float64 `db:"avg_tokens_in" json:"avg_tokens_in"`
	AvgTokensOut   float64 `db:"avg_tokens_out" json:"avg_tokens_out"`
	PeriodStart    string  `db:"period_start" json:"period_start"`
	PeriodEnd      string  `db:"period_end" json:"period_end"`
	UpdatedAt      string  `db:"updated_at" json:"updated_at"`
}

// ErrorRecord represents an error record for retry tracking.
type ErrorRecord struct {
	ID            int64  `db:"id" json:"id"`
	Timestamp     string `db:"timestamp" json:"timestamp"`
	Provider      string `db:"provider" json:"provider"`
	ModelID       string `db:"model_id" json:"model_id"`
	ErrorType     string `db:"error_type" json:"error_type"`
	ErrorMessage  string `db:"error_message" json:"error_message"`
	LimitType     string `db:"limit_type" json:"limit_type,omitempty"`
	RetryAttempts int    `db:"retry_attempts" json:"retry_attempts"`
	FinalOutcome  string `db:"final_outcome" json:"final_outcome"`
}

// SubscribeMetrics returns a channel for receiving metric updates.
// The returned stop function must be called to unsubscribe and close the channel.
func (s *Store) SubscribeMetrics() (ch <-chan *LiveMetricsSnapshot, stop func()) {
	rawCh := make(chan *LiveMetricsSnapshot, 10)
	ch = rawCh

	s.subMu.Lock()
	s.subscribers[rawCh] = struct{}{}
	s.subMu.Unlock()

	var stopOnce sync.Once
	stop = func() {
		s.subMu.Lock()
		delete(s.subscribers, rawCh)
		s.subMu.Unlock()
		stopOnce.Do(func() { close(rawCh) })
	}

	return ch, stop
}

// GetAverageStepDuration returns the average duration per step for similar tasks.
// Returns 0 if no historical data is available.
func (s *Store) GetAverageStepDuration(agentType string) time.Duration {
	query := `
	SELECT AVG(value) FROM metrics_live
	WHERE metric_name = 'step.duration'
	AND json_extract(tags, '$.agent_type') = ?
	AND timestamp > datetime('now', '-24 hours')
	`
	var avgSecs float64
	err := s.db.QueryRow(query, agentType).Scan(&avgSecs)
	if err != nil || avgSecs <= 0 {
		return 0
	}
	return time.Duration(avgSecs * float64(time.Second))
}

// notifySubscribers sends a snapshot to all subscribers.
func (s *Store) notifySubscribers() {
	s.subMu.RLock()
	if len(s.subscribers) == 0 {
		s.subMu.RUnlock()
		return
	}
	subs := make([]chan *LiveMetricsSnapshot, 0, len(s.subscribers))
	for ch := range s.subscribers {
		subs = append(subs, ch)
	}
	s.subMu.RUnlock()

	snapshot, err := s.GetLiveMetrics()
	if err != nil {
		return
	}

	for _, ch := range subs {
		select {
		case ch <- snapshot:
		default:
			// Skip if channel buffer is full
		}
	}
}
