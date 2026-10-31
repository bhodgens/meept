// Package metrics — otlp_trace_exporter.go: bounded-queue async batcher for
// spans. Mirrors the otlp_exporter.go batching posture (periodic flush +
// flush-on-stop), but optimized for the turn path: RecordSpan NEVER blocks
// the caller and NEVER drops on a healthy queue — non-blocking enqueue onto
// a bounded channel, drop + counter only on overflow. Export failures are
// the sink's concern (circuit + bounded logging); they never surface to a
// turn.
package metrics

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"sync"
	"sync/atomic"
	"time"
)

// TraceExporter tuning. Small bounded constants: async batch, bounded queue,
// drop+counter on overflow.
const (
	// DefaultTraceExportInterval is the periodic flush cadence.
	DefaultTraceExportInterval = 5 * time.Second
	// traceQueueCapacity bounds the in-flight span queue. Overflow drops
	// (with a counter) rather than blocking a turn.
	traceQueueCapacity = 512
	// traceMaxBatchSize is the max spans per export request; a larger
	// buffered batch is split into sequential requests.
	traceMaxBatchSize = 256
)

// TraceExporterConfig configures the span exporter.
type TraceExporterConfig struct {
	// ExportInterval is the periodic flush cadence (default 5s; zero or
	// negative takes the default).
	ExportInterval time.Duration
}

// TraceExporter batches spans and exports them via a TraceSink. Start
// spawns exactly one flush goroutine; Stop flushes the remainder and tears
// down. With no Start call, RecordSpan buffers and an explicit Flush
// exports (tests use this to avoid timing flakiness).
type TraceExporter struct {
	cfg      TraceExporterConfig
	sink     TraceSink
	queue    chan Span
	started  atomic.Bool
	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup

	// DroppedSpans counts spans shed on queue overflow (bounded-queue
	// drop+counter contract).
	DroppedSpans atomic.Int64
}

// NewTraceExporter builds an exporter over the given sink. The sink is
// retained as the interface value it arrives as: a nil INTERFACE is
// distinguishable (typed-nil guard), concrete sinks must be checked by the
// caller before construction.
func NewTraceExporter(cfg TraceExporterConfig, sink TraceSink) *TraceExporter {
	if cfg.ExportInterval <= 0 {
		cfg.ExportInterval = DefaultTraceExportInterval
	}
	return &TraceExporter{
		cfg:    cfg,
		sink:   sink,
		queue:  make(chan Span, traceQueueCapacity),
		stopCh: make(chan struct{}),
	}
}

// Start begins the background flush loop. Safe to call multiple times; only
// the first call takes effect.
func (e *TraceExporter) Start() {
	if !e.started.CompareAndSwap(false, true) {
		return
	}
	e.wg.Add(1)
	go e.flushLoop()
}

// Stop flushes remaining spans and releases the sink. Idempotent.
func (e *TraceExporter) Stop() {
	e.stopOnce.Do(func() {
		close(e.stopCh)
		e.wg.Wait()
		e.Flush()
		if e.sink != nil {
			if err := e.sink.Close(); err != nil {
				slog := defaultExporterLogger()
				slog.Warn("trace sink close error", "error", err)
			}
		}
	})
}

// RecordSpan enqueues a span without blocking: the exporter fills zero
// trace/span IDs from crypto/rand, then non-blocking enqueue — a full queue
// drops the span and bumps the counter (a degraded export path must never
// stall a turn).
func (e *TraceExporter) RecordSpan(sp Span) {
	if sp.TraceID == ([16]byte{}) {
		var tid [16]byte
		if _, err := rand.Read(tid[:]); err != nil {
			return // entropy failure: no span, never a pseudo-random ID
		}
		sp.TraceID = tid
	}
	if sp.SpanID == ([8]byte{}) {
		var sid [8]byte
		if _, err := rand.Read(sid[:]); err != nil {
			return
		}
		sp.SpanID = sid
	}
	select {
	case e.queue <- sp:
	default:
		e.DroppedSpans.Add(1)
	}
}

// Flush drains the queue and exports everything buffered. Safe for
// concurrent use; safe when never started.
func (e *TraceExporter) Flush() {
	e.flushLocked()
}

// flushLocked drains up to the whole queue, splitting into bounded export
// requests. Draining the channel non-blockingly here (rather than under a
// lock around the slice) means RecordSpan stays lock-free.
func (e *TraceExporter) flushLocked() {
	if e.sink == nil {
		return
	}
	batch := make([]Span, 0, traceMaxBatchSize)
	drain := func() {
		for {
			select {
			case sp := <-e.queue:
				batch = append(batch, sp)
				if len(batch) >= traceMaxBatchSize {
					return
				}
			default:
				return
			}
		}
	}
	drain()
	for len(batch) > 0 {
		// Export failures never propagate: the sink owns failure logging
		// (circuit + bounded lines); the exporter owns the drop decision.
		_ = e.sink.ExportSpans(context.Background(), batch)
		batch = make([]Span, 0, traceMaxBatchSize)
		drain()
		if len(batch) == 0 {
			return
		}
	}
}

func (e *TraceExporter) flushLoop() {
	defer e.wg.Done()
	tk := time.NewTicker(e.cfg.ExportInterval)
	defer tk.Stop()
	for {
		select {
		case <-tk.C:
			e.flushLocked()
		case <-e.stopCh:
			return
		}
	}
}

// defaultExporterLogger adapts the fmt-based exporter logger for the rare
// sink-close warning without importing fmt at each call site.
func defaultExporterLogger() *exporterLogger {
	return &exporterLogger{prefix: "otlp-trace-exporter"}
}

// TraceIDHex renders a trace ID as the canonical 32-char hex string.
func TraceIDHex(id [16]byte) string { return hex.EncodeToString(id[:]) }

// SpanIDHex renders a span ID as the canonical 16-char hex string.
func SpanIDHex(id [8]byte) string { return hex.EncodeToString(id[:]) }

// SpanBrief is a one-line human summary used in daemon debug logs only.
func SpanBrief(sp Span) string {
	return fmt.Sprintf("%s trace=%s span=%s", sp.Name, TraceIDHex(sp.TraceID), SpanIDHex(sp.SpanID))
}
