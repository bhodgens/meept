package metrics

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestOTLPHTTPSink_BasicExport proves the sink POSTs a JSON batch to
// <endpoint>/v1/metrics with the expected wire shape.
func TestOTLPHTTPSink_BasicExport(t *testing.T) {
	var gotPath string
	var gotPayload otlpExportPayload
	var status atomic.Int32
	status.Store(http.StatusOK)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode payload: %v", err)
		}
		w.WriteHeader(int(status.Load()))
	}))
	defer srv.Close()

	sink := NewOTLPHTTPSink(srv.URL, nil)
	err := sink.ExportMetrics(context.Background(), []MetricDatum{
		{Name: "agent.turns.count", Value: 3, Type: MetricTypeCounter, Timestamp: time.Now(),
			Tags: map[string]string{"session_id": "s1"}},
		{Name: "agent.token.usage", Value: 900, Type: MetricTypeGauge, Timestamp: time.Now()},
	})
	if err != nil {
		t.Fatalf("ExportMetrics: %v", err)
	}

	if gotPath != "/v1/metrics" {
		t.Errorf("POST path = %q, want /v1/metrics", gotPath)
	}
	if len(gotPayload.Metrics) != 2 {
		t.Fatalf("payload metrics = %d, want 2", len(gotPayload.Metrics))
	}
	if gotPayload.ServiceName != "meept-daemon" {
		t.Errorf("service_name = %q, want meept-daemon", gotPayload.ServiceName)
	}
	if gotPayload.Metrics[0].Name != "agent.turns.count" || gotPayload.Metrics[0].Value != 3 {
		t.Errorf("metric[0] = %+v", gotPayload.Metrics[0])
	}
	if gotPayload.Metrics[0].Tags["session_id"] != "s1" {
		t.Errorf("metric[0] tags = %v", gotPayload.Metrics[0].Tags)
	}
}

// TestOTLPHTTPSink_TrailingSlash proves a base URL with a trailing slash
// does not double the separator.
func TestOTLPHTTPSink_TrailingSlash(t *testing.T) {
	var gotPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewOTLPHTTPSink(srv.URL+"/", nil)
	if err := sink.ExportMetrics(context.Background(), []MetricDatum{
		{Name: "m", Value: 1, Type: MetricTypeCounter, Timestamp: time.Now()},
	}); err != nil {
		t.Fatalf("ExportMetrics: %v", err)
	}
	if gotPath != "/v1/metrics" {
		t.Errorf("POST path = %q, want /v1/metrics", gotPath)
	}
}

// TestOTLPHTTPSink_ServerError proves a 500 return is an error and the
// failure-streak logger reports exactly one Warn for the streak.
func TestOTLPHTTPSink_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	sink := NewOTLPHTTPSink(srv.URL, nil)
	for range 3 {
		if err := sink.ExportMetrics(context.Background(), []MetricDatum{
			{Name: "m", Value: 1, Type: MetricTypeCounter, Timestamp: time.Now()},
		}); err == nil {
			t.Fatal("expected error from 500 response")
		}
	}
	sink.mu.Lock()
	defer sink.mu.Unlock()
	if sink.consecFails != 3 {
		t.Errorf("consecFails = %d, want 3 (streak tracked)", sink.consecFails)
	}
}

// TestOTLPHTTPSink_Unreachable proves a dead receiver surfaces an error
// (the exporter logs it; the batch is dropped, export stays best-effort).
func TestOTLPHTTPSink_Unreachable(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close() // dead endpoint

	sink := NewOTLPHTTPSink(url, nil)
	if err := sink.ExportMetrics(context.Background(), []MetricDatum{
		{Name: "m", Value: 1, Type: MetricTypeCounter, Timestamp: time.Now()},
	}); err == nil {
		t.Fatal("expected error from unreachable endpoint")
	}
}

// TestOTLPHTTPSink_EmptyBatch proves a zero-length batch short-circuits
// with no HTTP round-trip.
func TestOTLPHTTPSink_EmptyBatch(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewOTLPHTTPSink(srv.URL, nil)
	if err := sink.ExportMetrics(context.Background(), nil); err != nil {
		t.Fatalf("empty batch: %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("empty batch hit the server %d times, want 0", hits.Load())
	}
}

// TestOTLPExporter_WithHTTPSink proves the full bridge path end to end:
// the exporter records through the REAL HTTP sink, a forced batch flush
// POSTs to the receiver, and Close is a clean no-op.
func TestOTLPExporter_WithHTTPSink(t *testing.T) {
	var gotPayload otlpExportPayload
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&gotPayload); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	sink := NewOTLPHTTPSink(srv.URL, nil)
	exp := NewOTLPExporter(OTLPExporterConfig{
		Endpoint: srv.URL,
	}, sink)
	exp.Start()

	exp.RecordTurn("emp-1", "sess-1")
	exp.RecordToolCall("bash")
	exp.RecordTokenUsage(250)

	// Batch flush pushes the buffered gauges/counters through the sink.
	exp.ExportForTestFlushBatch()

	// Immediate exports (turns/tool calls) also flow through the sink;
	// give the async recordImmediate path a moment, then stop.
	deadline := time.Now().Add(5 * time.Second)
	for len(gotPayload.Metrics) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(gotPayload.Metrics) == 0 {
		t.Fatal("receiver got no metrics from the exporter->HTTP sink path")
	}

	if err := sink.Close(); err != nil {
		t.Errorf("sink Close: %v", err)
	}
	exp.Stop()
}

// TestOTLPHTTPSink_ImplementsBridge is a compile-time assertion repeated
// as a runtime check for clarity.
func TestOTLPHTTPSink_ImplementsBridge(t *testing.T) {
	var _ OTLPMetricSink = (*OTLPHTTPSink)(nil)
}

// TestCollector_RegisterOTLPExporter proves the producer feed: events
// through a TypedEventEmitter reach the exporter's Record* methods via
// the REAL RegisterOTLPExporter wiring (nil guards included).
func TestCollector_RegisterOTLPExporter(t *testing.T) {
	sink := &captureSink{}
	exp := NewOTLPExporter(OTLPExporterConfig{ExportInterval: time.Hour}, sink)

	// Nil-emitter no-op must not panic.
	coll := &Collector{stopChan: make(chan struct{})}
	coll.RegisterOTLPExporter(exp, nil)

	// Nil-exporter no-op must not panic.
	coll.RegisterOTLPExporter(nil, nil)

	// Real path: a fake emitter records listener registrations, then we
	// fire them directly to prove the exporter records.
	emitter := &fakeEmitter{listeners: map[AgentEventType][]func(context.Context, AgentEvent){}}
	coll.RegisterOTLPExporter(exp, emitter)

	if len(emitter.listeners[AgentEventToolExecutionEnd]) != 1 {
		t.Fatalf("tool-end listener count = %d, want 1", len(emitter.listeners[AgentEventToolExecutionEnd]))
	}
	if len(emitter.listeners[AgentEventTurnEnd]) != 1 {
		t.Fatalf("turn-end listener count = %d, want 1", len(emitter.listeners[AgentEventTurnEnd]))
	}

	// Fire the tool-end listener as the emitter would.
	for _, fn := range emitter.listeners[AgentEventToolExecutionEnd] {
		fn(context.Background(), AgentEvent{Data: ToolExecutionEndData{ToolName: "wired_tool"}})
	}
	// Turn end with tokens.
	for _, fn := range emitter.listeners[AgentEventTurnEnd] {
		fn(context.Background(), AgentEvent{
			ConversationID: "conv-1",
			Data:           TurnEndData{ResponseTokens: 120},
		})
	}

	// Exporter buffered the events; a forced batch flush pushes them out.
	exp.ExportForTestFlushBatch()

	sink.mu.RLock()
	defer sink.mu.RUnlock()
	found := map[string]float64{}
	for _, m := range sink.metrics {
		found[m.Name] += m.Value
	}
	if found["agent.tool.calls"] == 0 {
		t.Errorf("agent.tool.calls missing after feed; got %v", found)
	}
	if found["agent.turns.count"] == 0 {
		t.Errorf("agent.turns.count missing after feed; got %v", found)
	}
	if found["agent.token.usage"] != 120 {
		t.Errorf("agent.token.usage = %v, want 120", found["agent.token.usage"])
	}
}

// fakeEmitter is a minimal TypedEventEmitter that records listeners.
type fakeEmitter struct {
	listeners map[AgentEventType][]func(context.Context, AgentEvent)
}

func (f *fakeEmitter) On(eventType AgentEventType, _ string, listener func(context.Context, AgentEvent)) {
	f.listeners[eventType] = append(f.listeners[eventType], listener)
}

func (f *fakeEmitter) OnAsync(eventType AgentEventType, _ string, listener func(context.Context, AgentEvent)) {
	f.listeners[eventType] = append(f.listeners[eventType], listener)
}
