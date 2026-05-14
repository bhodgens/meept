// Package metrics provides metrics collection and storage for Meept.
package metrics

import (
	"io"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"sync"
	"time"

	_ "github.com/mattn/go-sqlite3" //nolint:revive // blank import for side effects
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
	mu           sync.RWMutex
	db           *sql.DB
	batchSize    int
	flushInterval time.Duration
	batch        []metricValue
	lastFlush    time.Time
	stopChan     chan struct{}

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
	DatabasePath   string
	BatchSize      int
	FlushInterval  time.Duration
	RetentionDays  int
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
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create metrics directory: %w", err)
	}

	// Open database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

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

-- Indexes for query performance
CREATE INDEX IF NOT EXISTS idx_metrics_live_ts ON metrics_live(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_metrics_live_name ON metrics_live(metric_name);
CREATE INDEX IF NOT EXISTS idx_metrics_hourly_ts ON metrics_hourly(hour DESC);
CREATE INDEX IF NOT EXISTS idx_events_ts ON events(timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_events_type ON events(event_type);
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
		return
	}

	// Clean up old raw data (keep last 24 hours)
	_, _ = s.db.Exec("DELETE FROM metrics_live WHERE timestamp < datetime('now', '-24 hours')")
}

// Record records a metric value.
func (s *Store) Record(name string, value float64, tags map[string]string) {
	s.mu.Lock()

	s.batch = append(s.batch, metricValue{
		name:      name,
		value:     value,
		tags:      tags,
		timestamp: time.Now(),
	})

	// Check if batch is full
	shouldFlush := len(s.batch) >= s.batchSize
	s.mu.Unlock()

	// Flush outside of lock to avoid deadlock
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
		SELECT timestamp, metric_name, value, tags
		FROM metrics_live
		WHERE timestamp BETWEEN ? AND ?
		ORDER BY timestamp ASC
		`
	case "hour":
		query = `
		SELECT hour, metric_name, avg_value, ''
		FROM metrics_hourly
		WHERE hour BETWEEN ? AND ?
		ORDER BY hour ASC
		`
	case "day", "week":
		query = `
		SELECT date(hour), metric_name, avg_value, ''
		FROM metrics_hourly
		WHERE hour BETWEEN ? AND ?
		ORDER BY hour ASC
		`
	default:
		query = `
		SELECT hour, metric_name, avg_value, ''
		FROM metrics_hourly
		WHERE hour BETWEEN ? AND ?
		ORDER BY hour ASC
		`
	}

	rows, err := s.db.Query(query, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []MetricPoint
	for rows.Next() {
		var point MetricPoint
		var tagsStr string

		err := rows.Scan(&point.Timestamp, &point.Name, &point.Value, &tagsStr)
		if err != nil {
			continue
		}

		if tagsStr != "" {
			_ = json.Unmarshal([]byte(tagsStr), &point.Tags)
		}

		points = append(points, point)
	}

	return points, rows.Err()
}

// MetricPoint represents a single metric data point.
type MetricPoint struct {
	Timestamp time.Time         `json:"timestamp"`
	Name      string            `json:"name"`
	Value     float64           `json:"value"`
	Tags      map[string]string `json:"tags,omitempty"`
}

// Close closes the metrics store.
func (s *Store) Close() error {
	close(s.stopChan)

	// Final flush
	s.flush()

	return s.db.Close()
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

	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []Event
	for rows.Next() {
		var event Event
		var ctxJSON string

		err := rows.Scan(&event.Timestamp, &event.EventType, &event.Severity, &event.Message, &ctxJSON)
		if err != nil {
			continue
		}

		if ctxJSON != "" {
			_ = json.Unmarshal([]byte(ctxJSON), &event.Context)
		}

		events = append(events, event)
	}

	return events, rows.Err()
}

// Event represents a logged event.
type Event struct {
	Timestamp time.Time         `json:"timestamp"`
	EventType string            `json:"event_type"`
	Severity  string            `json:"severity"`
	Message   string            `json:"message"`
	Context   map[string]any    `json:"context,omitempty"`
}

// SubscribeMetrics returns a channel for receiving metric updates.
// The returned stop function must be called to unsubscribe and close the channel.
func (s *Store) SubscribeMetrics() (<-chan *LiveMetricsSnapshot, func()) {
	ch := make(chan *LiveMetricsSnapshot, 10)

	s.subMu.Lock()
	s.subscribers[ch] = struct{}{}
	s.subMu.Unlock()

	stop := func() {
		s.subMu.Lock()
		delete(s.subscribers, ch)
		s.subMu.Unlock()
		close(ch)
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
