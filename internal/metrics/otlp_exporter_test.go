package metrics

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// captureSink implements OTLPMetricSink for test use.
type captureSink struct {
	mu       sync.RWMutex
	metrics  []MetricDatum
	closed   bool
	closeErr error
}

func (c *captureSink) ExportMetrics(_ context.Context, data []MetricDatum) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = append(c.metrics, data...)
	return c.closeErr
}

func (c *captureSink) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.closed = true
	return c.closeErr
}

func (c *captureSink) Count() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.metrics)
}

func (c *captureSink) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.metrics = nil
}

func TestExportSink_Basic(t *testing.T) {
	sink := &captureSink{}
	err := sink.ExportMetrics(context.Background(), []MetricDatum{
		{Name: "test", Value: 1, Type: MetricTypeCounter},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if sink.Count() != 1 {
		t.Fatalf("expected count 1, got %d", sink.Count())
	}
}

func TestOTLPExporter_RecordMetrics(t *testing.T) {
	sink := &captureSink{}
	exp := NewOTLPExporter(OTLPExporterConfig{
		Endpoint:       "http://localhost:4317",
		ServiceName:    "test-daemon",
		ExportInterval: 60 * time.Second,
	}, sink)
	exp.Start()
	defer exp.Stop()

	// Record turns
	exp.RecordTurn("emp-1", "sess-1")
	exp.RecordTurn("emp-1", "sess-1")
	exp.RecordTurn("emp-2", "sess-1")

	// Record tool calls
	exp.RecordToolCall("read_file")
	exp.RecordToolCall("write_file")
	exp.RecordToolCall("read_file")
	exp.RecordToolCall("bash")

	// Record token usage
	exp.RecordTokenUsage(1500)
	exp.RecordTokenUsage(3200)

	// Record self-improvement events
	exp.RecordFailureDetected()
	exp.RecordFailureDetected()
	exp.RecordPatchApplied()

	// Record analysis durations
	exp.RecordAnalysisDuration(45.5)
	exp.RecordAnalysisDuration(120.3)
	exp.RecordAnalysisDuration(12.0)

	// Give sink time to collect all immediate exports
	time.Sleep(50 * time.Millisecond)

	sink.mu.RLock()
	all := sink.metrics
	sink.mu.RUnlock()

	if len(all) == 0 {
		t.Fatal("expected metrics from immediate recording, got none")
	}

	// Verify by-name presence
	found := make(map[string]bool)
	for _, m := range all {
		found[m.Name] = true
	}

	expected := []string{
		"agent.turns.count",
		"agent.tool.calls",
		"selfimprove.failures_detected",
		"selfimprove.patches_applied",
		"trace.analysis.duration_ms",
	}

	for _, name := range expected {
		if !found[name] {
			t.Errorf("expected metric %q not found (found: %v)", name, found)
		}
	}

	// Verify tag content of tool calls
	toolCounts := make(map[string]float64)
	for _, m := range all {
		if m.Name == "agent.tool.calls" {
			toolCounts[m.Tags["tool_name"]] += m.Value
		}
	}
	if got := toolCounts["read_file"]; got != 2 {
		t.Errorf("read_file calls: got %v, want 2", got)
	}
	if got := toolCounts["write_file"]; got != 1 {
		t.Errorf("write_file calls: got %v, want 1", got)
	}
	if got := toolCounts["bash"]; got != 1 {
		t.Errorf("bash calls: got %v, want 1", got)
	}

	// Verify turn counts per session
	turnSums := make(map[string]float64)
	for _, m := range all {
		if m.Name == "agent.turns.count" {
			key := m.Tags["employee_id"] + "|" + m.Tags["session_id"]
			turnSums[key] += m.Value
		}
	}
	if got := turnSums["emp-1|sess-1"]; got != 2 {
		t.Errorf("emp-1 sess-1 turns: got %v, want 2", got)
	}
	if got := turnSums["emp-2|sess-1"]; got != 1 {
		t.Errorf("emp-2 sess-1 turns: got %v, want 1", got)
	}
}

func TestOTLPExporter_GracefulShutdown(t *testing.T) {
	sink := &captureSink{}
	exp := NewOTLPExporter(OTLPExporterConfig{
		ExportInterval: 60 * time.Second,
	}, sink)

	exp.Start()

	// Record some metrics
	exp.RecordTurn("e1", "s1")
	exp.RecordToolCall("echo")
	exp.RecordFailureDetected()

	// Stop should return promptly
	done := make(chan struct{})
	go func() {
		exp.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5s")
	}

	if !sink.closed {
		t.Error("sink Close() was not called during exporter shutdown")
	}

	// Verify stop is idempotent
	exp.Stop()
	exp.Stop()
}

func TestOTLPExporter_ExportLoop(t *testing.T) {
	sink := &captureSink{}

	// Use a short export interval so the loop fires during test duration
	exp := NewOTLPExporter(OTLPExporterConfig{
		ExportInterval: 100 * time.Millisecond,
	}, sink)

	exp.Start()
	defer exp.Stop()

	// Buffer some metrics
	exp.RecordTurn("e1", "s1")
	exp.RecordToolCall("cat")
	exp.RecordTokenUsage(500)
	exp.RecordPatchApplied()
	exp.RecordAnalysisDuration(22.5)

	// Wait for at least one batch export cycle
	time.Sleep(350 * time.Millisecond)

	sink.mu.RLock()
	count := len(sink.metrics)
	sink.mu.RUnlock()

	if count == 0 {
		t.Error("expected batch metrics from export loop, got none")
	}

	// The batch should include token usage (which doesn't fire immediate export)
	foundToken := false
	for _, m := range sink.metrics {
		if m.Name == "agent.token.usage" {
			foundToken = true
			break
		}
	}
	if !foundToken {
		t.Error("expected agent.token.usage gauge from batch export")
	}
}

func TestOTLPExporter_DefaultConfig(t *testing.T) {
	sink := &captureSink{}
	exp := NewOTLPExporter(OTLPExporterConfig{}, sink)

	if exp.cfg.Endpoint != "http://localhost:4317" {
		t.Errorf("endpoint: got %q, want http://localhost:4317", exp.cfg.Endpoint)
	}
	if exp.cfg.ServiceName != "meept-daemon" {
		t.Errorf("service name: got %q, want meept-daemon", exp.cfg.ServiceName)
	}
	if exp.cfg.ExportInterval != 30*time.Second {
		t.Errorf("interval: got %v, want 30s", exp.cfg.ExportInterval)
	}
}

func TestOTLPExporter_StartIsIdempotent(t *testing.T) {
	sink := &captureSink{}
	exp := NewOTLPExporter(OTLPExporterConfig{}, sink)

	exp.Start()
	exp.Start()
	exp.Start()

	// Should not have multiple goroutines; verify with a Stop timeout
	done := make(chan struct{})
	go func() {
		exp.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("Stop() hung -- likely multiple export goroutines")
	}
}

func TestOTLPExporter_NilSink(t *testing.T) {
	exp := NewOTLPExporter(OTLPExporterConfig{}, nil)

	// Should not panic
	done := make(chan struct{})
	go func() {
		exp.Start()
		time.Sleep(50 * time.Millisecond)
		exp.Stop()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(3 * time.Second):
		t.Fatal("exporter with nil sink hung during shutdown")
	}
}

func TestOTLPExporter_ConcurrentRecording(t *testing.T) {
	sink := &captureSink{}
	exp := NewOTLPExporter(OTLPExporterConfig{}, sink)
	exp.Start()
	defer exp.Stop()

	const goroutines = 50
	const iterations = 20

	var wg sync.WaitGroup
	wg.Add(goroutines)

	for g := 0; g < goroutines; g++ {
		go func(id int) {
			defer wg.Done()
			for i := 0; i < iterations; i++ {
				empID := fmt.Sprintf("emp-%d", id%10)
				sessID := fmt.Sprintf("sess-%d", id%5)
				exp.RecordTurn(empID, sessID)
				exp.RecordToolCall(fmt.Sprintf("tool-%d", i%8))
				exp.RecordAnalysisDuration(float64(10 + i))
			}
		}(g)
	}

	wg.Wait()
	time.Sleep(200 * time.Millisecond)

	sink.mu.RLock()
	count := len(sink.metrics)
	sink.mu.RUnlock()

	if count == 0 {
		t.Error("no metrics recorded from concurrent goroutines")
	}
}

func TestOTLPExporter_RecordTokenUsageGauge(t *testing.T) {
	sink := &captureSink{}
	exp := NewOTLPExporter(OTLPExporterConfig{
		ExportInterval: 60 * time.Second, // prevent periodic batch
	}, sink)
	exp.Start()
	defer exp.Stop()

	exp.RecordTokenUsage(100)
	exp.RecordTokenUsage(500)

	// Token usage does NOT fire immediate export; only flushBatch does.
	time.Sleep(100 * time.Millisecond)

	// Verify no token gauge from immediate
	var foundImmediate bool
	sink.mu.RLock()
	for _, m := range sink.metrics {
		if m.Name == "agent.token.usage" {
			foundImmediate = true
		}
	}
	sink.mu.RUnlock()
	if foundImmediate {
		t.Error("agent.token.usage should not appear in immediate export")
	}

	// Force flush to validate batch behavior
	exp.ExportForTestFlushBatch()

	sink.mu.RLock()
	all := sink.metrics
	sink.mu.RUnlock()

	var foundToken bool
	var lastVal float64
	for _, m := range all {
		if m.Name == "agent.token.usage" {
			foundToken = true
			lastVal = m.Value
			if m.Type != MetricTypeGauge {
				t.Errorf("token usage type: got %q, want gauge", m.Type)
			}
		}
	}

	if !foundToken {
		t.Error("expected agent.token.usage metric from batch flush")
	}
	if lastVal != 500 {
		t.Errorf("token gauge value: got %v, want 500 (latest update after reset)", lastVal)
	}
}

func TestOTLPExporter_EnabledField(t *testing.T) {
	sink := &captureSink{}
	cfg := OTLPExporterConfig{
		Endpoint:       "https://otel.example.com:443",
		ServiceName:    "my-service",
		ExportInterval: 5 * time.Second,
		Enabled:        true,
	}
	exp := NewOTLPExporter(cfg, sink)

	if exp.cfg.Endpoint != "https://otel.example.com:443" {
		t.Errorf("endpoint: got %q, want https://otel.example.com:443", exp.cfg.Endpoint)
	}
	if exp.cfg.ServiceName != "my-service" {
		t.Errorf("service name: got %q, want my-service", exp.cfg.ServiceName)
	}
	if exp.cfg.ExportInterval != 5*time.Second {
		t.Errorf("interval: got %v, want 5s", exp.cfg.ExportInterval)
	}
}
