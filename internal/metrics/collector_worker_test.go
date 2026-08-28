package metrics

import (
	"encoding/json"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// newTestCollectorAndBus creates a Collector backed by a temp-file Store and
// a real MessageBus, ready for bus-driven tests. The caller must call
// collector.Shutdown() and store.Close().
func newTestCollectorAndBus(t *testing.T) (*Collector, *Store, *bus.MessageBus) {
	t.Helper()

	dbPath := filepath.Join(t.TempDir(), "metrics.db")
	store, err := NewStore(&StoreConfig{
		DatabasePath:  dbPath,
		BatchSize:     1, // flush on every Record call
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	messageBus := bus.New(nil, slog.Default())

	c := &Collector{
		store:    store,
		bus:      messageBus,
		stopChan: make(chan struct{}),
		logger:   slog.Default().With("component", "test-metrics-collector"),
	}
	c.subscribeToBus()

	return c, store, messageBus
}

// TestCollectorWorkerSubscription verifies that the collector receives
// worker.* events via the message bus and records counters tagged by
// event_type and source.
func TestCollectorWorkerSubscription(t *testing.T) {
	t.Parallel()

	c, store, messageBus := newTestCollectorAndBus(t)
	defer c.Shutdown()
	defer func() { _ = store.Close() }()

	events := []struct {
		topic  string
		source string
		data   map[string]any
	}{
		{"worker.started", "worker-pool", map[string]any{"worker_id": "w-1"}},
		{"worker.started", "worker-pool", map[string]any{"worker_id": "w-2"}},
		{"worker.completed", "chat-handler", map[string]any{"id": "w-3"}},
		{"worker.stopped", "worker-pool", map[string]any{"worker_id": "w-1"}},
		{"worker.state_changed", "chat-handler", map[string]any{"id": "w-3"}},
	}

	for _, e := range events {
		msg, err := models.NewBusMessage(models.MessageTypeEvent, e.source, e.data)
		if err != nil {
			t.Fatalf("NewBusMessage: %v", err)
		}
		messageBus.Publish(e.topic, msg)
	}

	// Give the subscriber goroutine a moment to drain the channel.
	time.Sleep(100 * time.Millisecond)

	type row struct {
		Name  string  `db:"metric_name"`
		Value float64 `db:"value"`
		Tags  string  `db:"tags"`
	}

	rows, err := store.DB().Queryx(
		`SELECT metric_name, value, tags FROM metrics_live WHERE metric_name = ?`,
		"worker.events_total",
	)
	if err != nil {
		t.Fatalf("query metrics_live: %v", err)
	}
	defer rows.Close()

	counts := map[string]float64{}
	for rows.Next() {
		var r row
		if err := rows.StructScan(&r); err != nil {
			t.Fatalf("scan: %v", err)
		}
		// Build a composite key from tags JSON for matching.
		counts[r.Tags] += r.Value
	}

	wantCases := []struct {
		eventType string
		source    string
		want      float64
	}{
		{"started", "worker-pool", 2},
		{"completed", "chat-handler", 1},
		{"stopped", "worker-pool", 1},
		{"state_changed", "chat-handler", 1},
	}
	for _, tc := range wantCases {
		var found float64
		for tagKey, val := range counts {
			if strings.Contains(tagKey, tc.eventType) && strings.Contains(tagKey, tc.source) {
				found = val
				break
			}
		}
		if found != tc.want {
			t.Errorf("event_type=%s source=%s: got %v, want %v",
				tc.eventType, tc.source, found, tc.want)
		}
	}
}

// TestRecordWorkerMetricsMalformedPayloadDoesNotPanic verifies defensive
// handling of invalid, empty, or non-standard payloads.
func TestRecordWorkerMetricsMalformedPayloadDoesNotPanic(t *testing.T) {
	t.Parallel()

	c, store, messageBus := newTestCollectorAndBus(t)
	defer c.Shutdown()
	defer func() { _ = store.Close() }()

	// Invalid JSON payload.
	msg1, _ := models.NewBusMessage(models.MessageTypeEvent, "worker-pool", nil)
	msg1.Payload = []byte("{not valid json")
	messageBus.Publish("worker.started", msg1)

	// Nil payload.
	msg2, _ := models.NewBusMessage(models.MessageTypeEvent, "worker-pool", nil)
	msg2.Payload = nil
	messageBus.Publish("worker.stopped", msg2)

	// Valid JSON but no worker_id key.
	msg3, _ := models.NewBusMessage(models.MessageTypeEvent, "worker-pool",
		map[string]any{"total": 4, "idle": 2})
	messageBus.Publish("worker.status", msg3)

	time.Sleep(50 * time.Millisecond)

	// If we get here, none of the messages panicked.
}

// TestExtractWorkerID covers the helper that reads worker IDs from
// heterogeneous JSON payloads (pool uses "worker_id", chat handler uses "id").
func TestExtractWorkerID(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name    string
		payload []byte
		want    string
	}{
		{"worker_id key", []byte(`{"worker_id": "w-abc"}`), "w-abc"},
		{"id key fallback", []byte(`{"id": "chat-worker-1"}`), "chat-worker-1"},
		{"worker_id preferred over id", []byte(`{"id": "x", "worker_id": "y"}`), "y"},
		{"missing both keys", []byte(`{"foo": "bar"}`), ""},
		{"empty payload", nil, ""},
		{"invalid json", []byte(`{broken`), ""},
		{"non-string value", []byte(`{"worker_id": 42}`), ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractWorkerID(tc.payload)
			if got != tc.want {
				t.Errorf("extractWorkerID(%s): got %q, want %q",
					string(tc.payload), got, tc.want)
			}
		})
	}
}

// TestRecordWorkerMetricsEventLog verifies that a RecordEvent row is written
// to the events table alongside the counter metric.
func TestRecordWorkerMetricsEventLog(t *testing.T) {
	t.Parallel()

	c, store, messageBus := newTestCollectorAndBus(t)
	defer c.Shutdown()
	defer func() { _ = store.Close() }()

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "worker-pool",
		map[string]any{"worker_id": "w-99"})
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	messageBus.Publish("worker.started", msg)

	time.Sleep(100 * time.Millisecond)

	var count int
	err = store.DB().Get(&count,
		`SELECT COUNT(*) FROM events WHERE event_type = ?`, "worker.event")
	if err != nil {
		t.Fatalf("query events: %v", err)
	}
	if count != 1 {
		t.Errorf("expected 1 worker.event row, got %d", count)
	}

	// Verify the context JSON contains the worker_id.
	var ctxJSON string
	err = store.DB().Get(&ctxJSON,
		`SELECT context FROM events WHERE event_type = ? LIMIT 1`, "worker.event")
	if err != nil {
		t.Fatalf("query event context: %v", err)
	}

	var ctx map[string]any
	if err := json.Unmarshal([]byte(ctxJSON), &ctx); err != nil {
		t.Fatalf("unmarshal context: %v", err)
	}
	if ctx["worker_id"] != "w-99" {
		t.Errorf("expected worker_id w-99 in event context, got %v", ctx["worker_id"])
	}
}

// TestCollectorTokenUsageSubscription verifies llm.tokens.used events
// from AgentLoop.publishTokenUsage are recorded as llm.tokens_used.
func TestCollectorTokenUsageSubscription(t *testing.T) {
	t.Parallel()

	c, store, messageBus := newTestCollectorAndBus(t)
	defer c.Shutdown()
	defer func() { _ = store.Close() }()

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "agent", map[string]any{
		"conversation_id": "conv-1",
		"total_tokens":    42,
	})
	if err != nil {
		t.Fatalf("NewBusMessage: %v", err)
	}
	if n := messageBus.Publish("llm.tokens.used", msg); n == 0 {
		t.Fatal("llm.tokens.used had no subscribers")
	}

	time.Sleep(100 * time.Millisecond)

	var sum float64
	err = store.DB().Get(&sum,
		`SELECT COALESCE(SUM(value), 0) FROM metrics_live WHERE metric_name = ?`,
		"llm.tokens_used")
	if err != nil {
		t.Fatalf("query metrics_live: %v", err)
	}
	if sum != 42 {
		t.Errorf("llm.tokens_used sum = %v, want 42", sum)
	}
}
