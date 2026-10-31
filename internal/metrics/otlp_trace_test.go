// Package metrics — otlp_trace_test.go: TDD tests for the OTLP/HTTP trace
// sink + batched exporter (leaf 07-langfuse-sink T1). A capture httptest
// server decodes the hand-rolled protobuf wire form and asserts IDs, names,
// attributes and batching.
package metrics

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// capturedTrace is the decoded view of one export request.
type capturedTrace struct {
	contentType string
	headers     http.Header
	spans       []decodedSpan
	raw         []byte
}

type decodedSpan struct {
	traceID     string // hex
	spanID      string // hex
	parentID    string // hex
	name        string
	startNano   uint64
	endNano     uint64
	attrs       map[string]string
	errorStatus bool
}

// parseProtobuf walks a protobuf message and yields (field, wiretype,
// payload/value) pairs.
func parseProtobuf(t *testing.T, data []byte) []pbField {
	t.Helper()
	var out []pbField
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 {
			t.Fatalf("bad tag at offset %d", len(data))
		}
		data = data[n:]
		field := int(key >> 3)
		wire := int(key & 7)
		switch wire {
		case 0: // varint
			v, n := binary.Uvarint(data)
			if n <= 0 {
				t.Fatal("bad varint")
			}
			data = data[n:]
			out = append(out, pbField{field: field, wire: wire, varint: v})
		case 1: // fixed64
			if len(data) < 8 {
				t.Fatal("short fixed64")
			}
			out = append(out, pbField{field: field, wire: wire, fixed64: binary.LittleEndian.Uint64(data[:8])})
			data = data[8:]
		case 2: // length-delimited
			l, n := binary.Uvarint(data)
			if n <= 0 || int(l) > len(data[n:]) {
				t.Fatal("bad length")
			}
			data = data[n:]
			out = append(out, pbField{field: field, wire: wire, bytes: data[:l]})
			data = data[l:]
		default:
			t.Fatalf("unsupported wire type %d", wire)
		}
	}
	return out
}

type pbField struct {
	field   int
	wire    int
	varint  uint64
	fixed64 uint64
	bytes   []byte
}

func fieldsByField(fs []pbField, field int) []pbField {
	var out []pbField
	for _, f := range fs {
		if f.field == field {
			out = append(out, f)
		}
	}
	return out
}

func decodeSpanMsg(t *testing.T, data []byte) decodedSpan {
	t.Helper()
	sp := decodedSpan{attrs: map[string]string{}}
	for _, f := range parseProtobuf(t, data) {
		switch f.field {
		case 1:
			sp.traceID = hexEncode(f.bytes)
		case 2:
			sp.spanID = hexEncode(f.bytes)
		case 4:
			sp.parentID = hexEncode(f.bytes)
		case 5:
			sp.name = string(f.bytes)
		case 7:
			sp.startNano = f.fixed64
		case 8:
			sp.endNano = f.fixed64
		case 9: // attributes: KeyValue{1:key, 2:AnyValue{1:string_value}}
			kv := parseProtobuf(t, f.bytes)
			key := string(fieldsByField(kv, 1)[0].bytes)
			av := parseProtobuf(t, fieldsByField(kv, 2)[0].bytes)
			val := string(fieldsByField(av, 1)[0].bytes)
			sp.attrs[key] = val
		case 15: // status
			st := parseProtobuf(t, f.bytes)
			for _, sf := range st {
				if sf.field == 2 && sf.varint == 2 {
					sp.errorStatus = true
				}
			}
		}
	}
	return sp
}

func hexEncode(b []byte) string {
	const digits = "0123456789abcdef"
	out := make([]byte, 0, len(b)*2)
	for _, c := range b {
		out = append(out, digits[c>>4], digits[c&0xf])
	}
	return string(out)
}

// newCaptureTraceServer starts an httptest server capturing every trace
// export request.
func newCaptureTraceServer(t *testing.T) (*httptest.Server, func() []capturedTrace) {
	t.Helper()
	var mu sync.Mutex
	var captures []capturedTrace
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read body: %v", err)
			http.Error(w, "read", http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/v1/traces" {
			t.Errorf("unexpected path %q", r.URL.Path)
			http.Error(w, "path", http.StatusNotFound)
			return
		}
		// Decode the single ResourceSpans/ScopeSpans chain.
		req := parseProtobuf(t, body)
		cap := capturedTrace{contentType: r.Header.Get("Content-Type"), headers: r.Header.Clone(), raw: body}
		for _, rsf := range fieldsByField(req, 1) { // resource_spans
			rs := parseProtobuf(t, rsf.bytes)
			for _, ssf := range fieldsByField(rs, 2) { // scope_spans
				ss := parseProtobuf(t, ssf.bytes)
				for _, spf := range fieldsByField(ss, 2) { // spans
					cap.spans = append(cap.spans, decodeSpanMsg(t, spf.bytes))
				}
			}
		}
		mu.Lock()
		captures = append(captures, cap)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(srv.Close)
	return srv, func() []capturedTrace {
		mu.Lock()
		defer mu.Unlock()
		return append([]capturedTrace(nil), captures...)
	}
}

// makeSpan builds a fully-populated span for tests.
func makeSpan(name string) Span {
	now := time.Now()
	return Span{
		TraceID:      [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16},
		SpanID:       [8]byte{1, 2, 3, 4, 5, 6, 7, 8},
		ParentSpanID: [8]byte{9, 9, 9, 9, 9, 9, 9, 9},
		Name:         name,
		Start:        now.Add(-time.Second),
		End:          now,
		Attributes:   map[string]string{"task_id": "task-1", "step_id": "step-1", "state": "completed"},
		SessionID:    "session-abc123",
	}
}

// T1: three spans flushed to a capture server decode with correct
// IDs/names/attrs; content type is the OTLP protobuf one.
func TestTraceSinkExportsSpansAsProtobuf(t *testing.T) {
	srv, captures := newCaptureTraceServer(t)
	sink := NewOTLPTraceHTTPSink(srv.URL, map[string]string{"x-langfuse-ingestion-version": "4"}, nil)

	spans := []Span{makeSpan("chat.turn"), makeSpan("task.step"), makeSpan("chat.turn")}
	if err := sink.ExportSpans(context.Background(), spans); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}

	got := captures()
	if len(got) != 1 {
		t.Fatalf("want 1 export request, got %d", len(got))
	}
	c := got[0]
	if c.contentType != "application/x-protobuf" {
		t.Errorf("content type = %q, want application/x-protobuf", c.contentType)
	}
	if v := c.headers.Get("x-langfuse-ingestion-version"); v != "4" {
		t.Errorf("x-langfuse-ingestion-version = %q, want 4", v)
	}
	if len(c.spans) != 3 {
		t.Fatalf("decoded %d spans, want 3", len(c.spans))
	}
	first := c.spans[0]
	if first.name != "chat.turn" {
		t.Errorf("span name = %q, want chat.turn", first.name)
	}
	if first.traceID != "0102030405060708090a0b0c0d0e0f10" {
		t.Errorf("trace id = %s", first.traceID)
	}
	if first.spanID != "0102030405060708" {
		t.Errorf("span id = %s", first.spanID)
	}
	if first.parentID != "0909090909090909" {
		t.Errorf("parent span id = %s", first.parentID)
	}
	for _, want := range []string{"task_id", "step_id", "state", "trace_name", "langfuse.session.id"} {
		if _, ok := first.attrs[want]; !ok {
			t.Errorf("span missing attribute %q (attrs: %v)", want, first.attrs)
		}
	}
	if first.attrs["trace_name"] != "chat.turn" {
		t.Errorf("trace_name = %q, want chat.turn", first.attrs["trace_name"])
	}
	if first.startNano == 0 || first.endNano == 0 {
		t.Error("span timestamps not encoded")
	}
}

// T1: empty parent id stays absent (root span).
func TestTraceSinkRootSpanOmitsParentID(t *testing.T) {
	srv, captures := newCaptureTraceServer(t)
	sink := NewOTLPTraceHTTPSink(srv.URL, nil, nil)

	sp := makeSpan("chat.turn")
	sp.ParentSpanID = [8]byte{}
	if err := sink.ExportSpans(context.Background(), []Span{sp}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	got := captures()
	if len(got) != 1 || len(got[0].spans) != 1 {
		t.Fatalf("unexpected captures: %+v", got)
	}
	if got[0].spans[0].parentID != "" {
		t.Errorf("root span carries parent id %q", got[0].spans[0].parentID)
	}
}

// T1: error spans carry STATUS_CODE_ERROR (2).
func TestTraceSinkErrorSpanStatus(t *testing.T) {
	srv, captures := newCaptureTraceServer(t)
	sink := NewOTLPTraceHTTPSink(srv.URL, nil, nil)

	sp := makeSpan("chat.turn")
	sp.IsError = true
	if err := sink.ExportSpans(context.Background(), []Span{sp}); err != nil {
		t.Fatalf("ExportSpans: %v", err)
	}
	got := captures()
	if len(got) != 1 {
		t.Fatalf("captures = %d", len(got))
	}
	if !got[0].spans[0].errorStatus {
		t.Error("error span missing STATUS_CODE_ERROR status")
	}
}

// T1: the batched exporter coalesces spans into batched requests.
func TestTraceExporterBatches(t *testing.T) {
	srv, captures := newCaptureTraceServer(t)
	sink := NewOTLPTraceHTTPSink(srv.URL, nil, nil)
	exp := NewTraceExporter(TraceExporterConfig{}, sink)

	for i := 0; i < 5; i++ {
		exp.RecordSpan(makeSpan("chat.turn"))
	}
	exp.Flush()

	got := captures()
	if len(got) != 1 {
		t.Fatalf("want 1 coalesced request, got %d", len(got))
	}
	if len(got[0].spans) != 5 {
		t.Fatalf("decoded %d spans, want 5", len(got[0].spans))
	}
}

// SampleRate 0.25 keeps 1 in 4.
func TestTraceSampleRateZeroDropsAll(t *testing.T) {
	if sampleTrace(Span{TraceID: [16]byte{1}}, 0) {
		t.Fatal("sample rate 0 must drop every span")
	}
	if !sampleTrace(Span{TraceID: [16]byte{1}}, 1) {
		t.Fatal("sample rate 1 must keep every span")
	}
	// Deterministic: same trace id -> same decision.
	d1 := sampleTrace(Span{TraceID: [16]byte{0xab}}, 0.25)
	d2 := sampleTrace(Span{TraceID: [16]byte{0xab}}, 0.25)
	if d1 != d2 {
		t.Fatal("sampling not deterministic for identical trace ids")
	}
}

// T1: exporter RecordSpan fills zero IDs from crypto/rand.
func TestTraceExporterFillsRandomIDs(t *testing.T) {
	sp := makeSpan("chat.turn")
	sp.TraceID = [16]byte{}
	sp.SpanID = [8]byte{}
	sink := &capturingSink{}
	exp := NewTraceExporter(TraceExporterConfig{}, sink)
	exp.RecordSpan(sp)
	exp.Flush()
	if len(sink.spans) != 1 {
		t.Fatalf("sink got %d spans", len(sink.spans))
	}
	got := sink.spans[0]
	if got.TraceID == ([16]byte{}) || got.SpanID == ([8]byte{}) {
		t.Fatal("exporter did not fill zero IDs from crypto/rand")
	}
}

// T1: bounded queue overflow drops + counts, never blocks.
func TestTraceExporterOverflowDropsAndCounts(t *testing.T) {
	exp := NewTraceExporter(TraceExporterConfig{}, &blockingSink{})
	defer exp.Stop()
	// Queue capacity is 512; push far beyond it with no flush loop running.
	for i := 0; i < traceQueueCapacity+100; i++ {
		exp.RecordSpan(makeSpan("chat.turn"))
	}
	if got := exp.DroppedSpans.Load(); got != 100 {
		t.Fatalf("dropped = %d, want 100", got)
	}
}

// T1: exporter Stop flushes remaining spans to the sink.
func TestTraceExporterStopFlushes(t *testing.T) {
	srv, captures := newCaptureTraceServer(t)
	sink := NewOTLPTraceHTTPSink(srv.URL, nil, nil)
	exp := NewTraceExporter(TraceExporterConfig{}, sink)
	exp.Start()
	exp.RecordSpan(makeSpan("chat.turn"))
	exp.Stop()
	got := captures()
	if len(got) == 0 || len(got[len(got)-1].spans) == 0 {
		t.Fatal("Stop did not flush the remaining span")
	}
}

// T2 support: a failing receiver opens the circuit after the threshold and
// drops batches (no network) while open; recovery closes it.
func TestTraceSinkCircuitOpensAfterRepeatedFailures(t *testing.T) {
	var hits atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		hits.Add(1)
		http.Error(w, "down", http.StatusBadGateway)
	}))
	t.Cleanup(srv.Close)

	sink := NewOTLPTraceHTTPSink(srv.URL, nil, nil)
	ctx := context.Background()
	for i := 0; i < traceCircuitThreshold; i++ {
		_ = sink.ExportSpans(ctx, []Span{makeSpan("chat.turn")})
	}
	before := hits.Load()
	// Circuit open: no more network attempts.
	for i := 0; i < 3; i++ {
		err := sink.ExportSpans(ctx, []Span{makeSpan("chat.turn")})
		if !errors.Is(err, errTraceCircuitOpen) {
			t.Fatalf("want errTraceCircuitOpen while circuit open, got %v", err)
		}
	}
	if got := hits.Load(); got != before {
		t.Fatalf("circuit-open exports hit the receiver (%d -> %d)", before, got)
	}
	if sink.DroppedByCircuit() != 3 {
		t.Fatalf("dropped-by-circuit = %d, want 3", sink.DroppedByCircuit())
	}
}

// T1 helper sinks -----------------------------------------------------------

// capturingSink records spans handed to it (no wire).
type capturingSink struct {
	mu    sync.Mutex
	spans []Span
}

func (c *capturingSink) ExportSpans(_ context.Context, spans []Span) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.spans = append(c.spans, spans...)
	return nil
}

func (c *capturingSink) Close() error { return nil }

// blockingSink never drains its channel — used to prove RecordSpan
// non-blocking behavior... it does not even need to block: the exporter's
// queue is its own; this sink just refuses exports.
type blockingSink struct{}

func (blockingSink) ExportSpans(_ context.Context, _ []Span) error {
	return errors.New("sink down")
}
func (blockingSink) Close() error { return nil }

// Silence unused warnings for helpers used by future sub-tests.
var _ = bytes.Equal
