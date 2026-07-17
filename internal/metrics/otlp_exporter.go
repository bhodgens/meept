// Package metrics provides metrics collection, storage, and export for Meept.
package metrics

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// OTLPExporterConfig configures the OTLP metrics exporter.
type OTLPExporterConfig struct {
	// Endpoint for the OTLP gRPC receiver (default "http://localhost:4317").
	Endpoint string

	// ServiceName identifies the service in exported metrics (default "meept-daemon").
	ServiceName string

	// ExportInterval is the periodic batch export interval (default 30s).
	ExportInterval time.Duration

	// Enabled toggles exporter operation; call Start() when true.
	Enabled bool
}

// OTLPMetricSink is the bridge to an actual OTLP collector.
// The OTLPExporter talks through this interface so tests can inject
// a no-op or capture implementation without any OTLP SDK dependency.
type OTLPMetricSink interface {
	// ExportMetrics sends a batch of metric data to the OTLP collector.
	ExportMetrics(ctx context.Context, data []MetricDatum) error

	// Close flushes remaining data and releases resources.
	Close() error
}

// MetricDatum represents a single metric point ready for OTLP export.
type MetricDatum struct {
	Name      string
	Value     float64
	Type      MetricType
	Timestamp time.Time
	Tags      map[string]string
}

// MetricType identifies the aggregation model of a datum.
type MetricType string

const (
	MetricTypeCounter   MetricType = "counter"
	MetricTypeGauge     MetricType = "gauge"
	MetricTypeHistogram MetricType = "histogram"
)

// OTLPExporter periodically batches agent and self-improvement metrics
// and exports them via an OTLP gRPC sink.
type OTLPExporter struct {
	cfg      OTLPExporterConfig
	sink     OTLPMetricSink
	started  atomic.Bool
	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// Metric buffers protected by mu
	mu          sync.RWMutex
	turnsByPair map[pairID]turnCount
	toolCalls   map[string]uint64
	tokens      atomic.Int64
	failures    atomic.Int64
	patches     atomic.Int64
	analysisDur []float64

	logger *exporterLogger
}

type pairID struct {
	employee string
	session  string
}

type turnCount struct {
	employeeID string
	sessionID  string
	count      float64
}

// NewOTLPExporter creates an OTLP exporter from config and a metric sink.
func NewOTLPExporter(cfg OTLPExporterConfig, sink OTLPMetricSink) *OTLPExporter {
	if cfg.Endpoint == "" {
		cfg.Endpoint = "http://localhost:4317"
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "meept-daemon"
	}
	if cfg.ExportInterval == 0 {
		cfg.ExportInterval = 30 * time.Second
	}

	return &OTLPExporter{
		cfg:         cfg,
		sink:        sink,
		stopCh:      make(chan struct{}),
		turnsByPair: make(map[pairID]turnCount),
		toolCalls:   make(map[string]uint64),
		logger:      &exporterLogger{prefix: "otlp-exporter"},
	}
}

// Start begins the background export loop. Safe to call multiple times;
// only the first call takes effect.
func (o *OTLPExporter) Start() {
	if !o.started.CompareAndSwap(false, true) {
		return
	}

	o.wg.Add(1)
	go o.exportLoop()
}

// Stop gracefully shuts down the exporter and flushes remaining metrics.
func (o *OTLPExporter) Stop() {
	o.stopOnce.Do(func() {
		o.started.Store(false)
		close(o.stopCh)
		o.wg.Wait()

		if o.sink != nil {
			if err := o.sink.Close(); err != nil {
				o.logger.Warn("sink close error", "error", err)
			}
		}
	})
}

// IsStarted reports whether the exporter is actively running.
func (o *OTLPExporter) IsStarted() bool {
	return o.started.Load()
}

// RecordTurn records a completed agent turn for the given employee and session.
func (o *OTLPExporter) RecordTurn(employeeID, sessionID string) {
	o.mu.Lock()
	k := pairID{employee: employeeID, session: sessionID}
	o.turnsByPair[k] = turnCount{employeeID: employeeID, sessionID: sessionID, count: o.turnsByPair[k].count + 1}
	o.mu.Unlock()

	o.recordImmediate(MetricDatum{
		Name:      "agent.turns.count",
		Value:     1,
		Type:      MetricTypeCounter,
		Timestamp: time.Now(),
		Tags: map[string]string{
			"employee_id": employeeID,
			"session_id":  sessionID,
		},
	})
}

// RecordToolCall records a tool execution for the given tool name.
func (o *OTLPExporter) RecordToolCall(toolName string) {
	o.mu.Lock()
	o.toolCalls[toolName]++
	o.mu.Unlock()

	o.recordImmediate(MetricDatum{
		Name:      "agent.tool.calls",
		Value:     1,
		Type:      MetricTypeCounter,
		Timestamp: time.Now(),
		Tags: map[string]string{
			"tool_name": toolName,
		},
	})
}

// RecordTokenUsage records the cumulative token usage count.
func (o *OTLPExporter) RecordTokenUsage(tokens int) {
	o.tokens.Store(int64(tokens))
}

// RecordFailureDetected increments the self-improvement failure detection counter.
func (o *OTLPExporter) RecordFailureDetected() {
	o.failures.Add(1)

	o.recordImmediate(MetricDatum{
		Name:      "selfimprove.failures_detected",
		Value:     1,
		Type:      MetricTypeCounter,
		Timestamp: time.Now(),
	})
}

// RecordPatchApplied increments the patch application counter.
func (o *OTLPExporter) RecordPatchApplied() {
	o.patches.Add(1)

	o.recordImmediate(MetricDatum{
		Name:      "selfimprove.patches_applied",
		Value:     1,
		Type:      MetricTypeCounter,
		Timestamp: time.Now(),
	})
}

// RecordAnalysisDuration records an analysis step duration in milliseconds.
// It emits immediately and also buffers for batch export.
func (o *OTLPExporter) RecordAnalysisDuration(ms float64) {
	o.mu.Lock()
	o.analysisDur = append(o.analysisDur, ms)
	o.mu.Unlock()

	o.recordImmediate(MetricDatum{
		Name:      "trace.analysis.duration_ms",
		Value:     ms,
		Type:      MetricTypeHistogram,
		Timestamp: time.Now(),
	})
}

func (o *OTLPExporter) recordImmediate(d MetricDatum) {
	o.mu.RLock()
	sink := o.sink
	o.mu.RUnlock()

	if sink == nil || !o.started.Load() {
		return
	}

	if err := sink.ExportMetrics(context.Background(), []MetricDatum{d}); err != nil {
		o.logger.Warn("immediate export failed for "+d.Name, "error", err)
	}
}

func (o *OTLPExporter) exportLoop() {
	defer o.wg.Done()

	tk := time.NewTicker(o.cfg.ExportInterval)
	defer tk.Stop()

	for {
		select {
		case <-tk.C:
			o.flushBatch()
		case <-o.stopCh:
			o.flushBatch()
			return
		}
	}
}

func (o *OTLPExporter) flushBatch() {
	o.mu.Lock()
	defer o.mu.Unlock()

	sink := o.sink
	if sink == nil {
		return
	}

	ts := time.Now()
	data := make([]MetricDatum, 0, 32)

	for _, tc := range o.turnsByPair {
		if tc.count > 0 {
			data = append(data, MetricDatum{
				Name:      "agent.turns.count",
				Value:     tc.count,
				Type:      MetricTypeCounter,
				Timestamp: ts,
				Tags: map[string]string{
					"employee_id": tc.employeeID,
					"session_id":  tc.sessionID,
				},
			})
		}
	}

	for name, count := range o.toolCalls {
		if count > 0 {
			data = append(data, MetricDatum{
				Name:      "agent.tool.calls",
				Value:     float64(count),
				Type:      MetricTypeCounter,
				Timestamp: ts,
				Tags: map[string]string{
					"tool_name": name,
				},
			})
		}
	}

	val := o.tokens.Swap(0)
	if val != 0 {
		data = append(data, MetricDatum{
			Name:      "agent.token.usage",
			Value:     float64(val),
			Type:      MetricTypeGauge,
			Timestamp: ts,
		})
	}

	failures := o.failures.Swap(0)
	if failures > 0 {
		data = append(data, MetricDatum{
			Name:      "selfimprove.failures_detected",
			Value:     float64(failures),
			Type:      MetricTypeCounter,
			Timestamp: ts,
		})
	}

	patches := o.patches.Swap(0)
	if patches > 0 {
		data = append(data, MetricDatum{
			Name:      "selfimprove.patches_applied",
			Value:     float64(patches),
			Type:      MetricTypeCounter,
			Timestamp: ts,
		})
	}

	tmpDur := o.analysisDur
	o.analysisDur = o.analysisDur[:0]
	for _, ms := range tmpDur {
		data = append(data, MetricDatum{
			Name:      "trace.analysis.duration_ms",
			Value:     ms,
			Type:      MetricTypeHistogram,
			Timestamp: ts,
		})
	}

	if len(data) > 0 {
		if err := sink.ExportMetrics(context.Background(), data); err != nil {
			o.logger.Error("batch export failed", "count", len(data), "error", err)
		}
	}
}

// ExportForTestFlushBatch forces a batch export outside the normal timer loop.
// This is intentionally exported for testing only.
func (o *OTLPExporter) ExportForTestFlushBatch() {
	o.flushBatch()
}

type exporterLogger struct {
	prefix string
}

func (l *exporterLogger) Warn(msg string, fields ...any) {
	fmt.Printf("[%s WARN] %s %v\n", l.prefix, msg, fields)
}

func (l *exporterLogger) Error(msg string, fields ...any) {
	fmt.Printf("[%s ERROR] %s %v\n", l.prefix, msg, fields)
}
