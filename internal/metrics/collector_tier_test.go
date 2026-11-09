package metrics

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// newTierTestStore builds a Store over a temp metrics.db for the tier-field
// pins.
func newTierTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(&StoreConfig{
		DatabasePath:  filepath.Join(t.TempDir(), "metrics.db"),
		BatchSize:     1,
		FlushInterval: time.Hour, // disable background flush; tests flush manually
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestCollector_AfterProviderResponseData_TierFieldsMirror (TIER-1 residual)
// pins the collector's mirror of agent.AfterProviderResponseData: the tier
// fields exist with the PINNED key names (llm_tier / llm_tier_degraded /
// llm_tier_depth, matching llm.MetaKey*), the string omitted when empty, and
// the shape round-trips so event.Data carries the tier into the listener.
func TestCollector_AfterProviderResponseData_TierFieldsMirror(t *testing.T) {
	data := AfterProviderResponseData{
		ModelID:         "m",
		LLMTier:         "prov/model-x",
		LLMTierDegraded: true,
		LLMTierDepth:    2,
	}
	raw, err := json.Marshal(data)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if decoded["llm_tier"] != "prov/model-x" {
		t.Fatalf("llm_tier = %v, want prov/model-x", decoded["llm_tier"])
	}
	if degraded, ok := decoded["llm_tier_degraded"].(bool); !ok || !degraded {
		t.Fatalf("llm_tier_degraded = %v, want true", decoded["llm_tier_degraded"])
	}
	if depth, ok := decoded["llm_tier_depth"].(float64); !ok || depth != 2 {
		t.Fatalf("llm_tier_depth = %v, want 2", decoded["llm_tier_depth"])
	}

	// Feature-off posture: empty tier string OMITTED from the payload
	// (omitempty), matching the agent-side pin; the bool/int stay
	// zero-valued ("not degraded"/"depth 0"), also matching it.
	off, err := json.Marshal(AfterProviderResponseData{ModelID: "m"})
	if err != nil {
		t.Fatalf("marshal off: %v", err)
	}
	var offDecoded map[string]any
	if err := json.Unmarshal(off, &offDecoded); err != nil {
		t.Fatalf("unmarshal off: %v", err)
	}
	if _, present := offDecoded["llm_tier"]; present {
		t.Fatalf("feature-off payload carries llm_tier: %s", off)
	}
	if degraded, _ := offDecoded["llm_tier_degraded"].(bool); degraded {
		t.Fatalf("feature-off llm_tier_degraded = true, want false")
	}
	if depth, _ := offDecoded["llm_tier_depth"].(float64); depth != 0 {
		t.Fatalf("feature-off llm_tier_depth = %v, want 0", offDecoded["llm_tier_depth"])
	}
}

// TestCollector_RecordLLMCall_TierTag pins the persistence surface: a
// non-empty tier rides the metrics_live tags as "llm_tier"; an empty tier
// keeps the tags free of it (feature-off rows unchanged).
func TestCollector_RecordLLMCall_TierTag(t *testing.T) {
	store := newTierTestStore(t)
	c := NewCollector(store, nil, &CollectorConfig{Enabled: false})

	c.RecordLLMCall("m1", 10, 20, 50*time.Millisecond, "prov/model-x")
	// Force the flush so the batch lands in metrics_live.
	store.flush()

	var tags string
	err := store.db.QueryRow(
		`SELECT tags FROM metrics_live WHERE metric_name = 'llm.calls' LIMIT 1`,
	).Scan(&tags)
	if err != nil {
		t.Fatalf("query llm.calls row: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(tags), &decoded); err != nil {
		t.Fatalf("unmarshal tags %q: %v", tags, err)
	}
	if decoded["llm_tier"] != "prov/model-x" {
		t.Fatalf("llm_tier tag = %v, want prov/model-x", decoded["llm_tier"])
	}

	// Feature-off: no llm_tier tag.
	c.RecordLLMCall("m2", 1, 2, 5*time.Millisecond, "")
	store.flush()
	rows := store.db.QueryRow(
		`SELECT tags FROM metrics_live WHERE metric_name = 'llm.calls' AND json_extract(tags, '$.model') = 'm2' LIMIT 1`,
	)
	var offTags string
	if err := rows.Scan(&offTags); err != nil {
		t.Fatalf("query feature-off row: %v", err)
	}
	if strings.Contains(offTags, "llm_tier") {
		t.Fatalf("feature-off tags carry llm_tier: %s", offTags)
	}
}

// TestCollector_TypedListener_BridgesTierIntoRecordedCalls pins the
// AfterProviderResponse listener path: a typed event carrying LLMTier lands
// the tier tag on the llm.calls metric row.
func TestCollector_TypedListener_BridgesTierIntoRecordedCalls(t *testing.T) {
	store := newTierTestStore(t)
	c := NewCollector(store, nil, &CollectorConfig{Enabled: false})

	emitter := newTestEmitter()
	c.RegisterEventListeners(emitter)

	emitter.emit(context.Background(), AgentEvent{
		Type:      AgentEventAfterProviderResponse,
		AgentID:   "a",
		Timestamp: time.Now(),
		Data: AfterProviderResponseData{
			ModelID:        "m1",
			ResponseTokens: 42,
			Latency:        10 * time.Millisecond,
			LLMTier:        "prov/model-degraded",
		},
	})

	// Listeners are async: poll briefly for the row.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		store.flush()
		var tags string
		err := store.db.QueryRow(
			`SELECT tags FROM metrics_live WHERE metric_name = 'llm.calls' LIMIT 1`,
		).Scan(&tags)
		if err == nil && strings.Contains(tags, "prov/model-degraded") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("tier tag never landed on the llm.calls metric row via the typed listener")
}

// testEmitter is a minimal TypedEventEmitter double recording listeners and
// firing them synchronously.
type testEmitter struct {
	listeners map[AgentEventType][]func(ctx context.Context, event AgentEvent)
}

func newTestEmitter() *testEmitter {
	return &testEmitter{listeners: make(map[AgentEventType][]func(ctx context.Context, event AgentEvent))}
}

func (e *testEmitter) On(eventType AgentEventType, _ string, listener func(ctx context.Context, event AgentEvent)) {
	e.listeners[eventType] = append(e.listeners[eventType], listener)
}

func (e *testEmitter) OnAsync(eventType AgentEventType, _ string, listener func(ctx context.Context, event AgentEvent)) {
	e.listeners[eventType] = append(e.listeners[eventType], listener)
}

func (e *testEmitter) emit(ctx context.Context, event AgentEvent) {
	for _, fn := range e.listeners[event.Type] {
		fn(ctx, event)
	}
}
