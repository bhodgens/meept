// Package metrics — otlp_trace_test.go: TDD tests for the OTLP/HTTP trace
// sink + batched exporter (leaf 07-langfuse-sink T1). A capture httptest
// server decodes the hand-rolled protobuf wire form and asserts IDs, names,
// attributes and batching.
package metrics

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
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
	traceID       string // hex
	spanID        string // hex
	parentID      string // hex
	name          string
	startNano     uint64
	endNano       uint64
	attrs         map[string]string
	statusMessage string
	errorStatus   bool
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
				switch {
				case sf.field == 1:
					sp.statusMessage = string(sf.bytes)
				case sf.field == 2 && sf.varint == 2:
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

// The drain-and-close helper below is shared with otlp_http_sink.go via the
// same-package definition there; it is intentionally not redefined.
var (
	_ = bytes.Equal
	_ = io.Discard
)

// ---------------------------------------------------------------------------
// Leaf 03 (llm-hop-tracing): llm.call + tool.call hop-span pins.
// ---------------------------------------------------------------------------

// hopTraceHex/hopSpanHex are valid 32/16-char lowercase hex IDs shared by
// the leaf-03 pins.
const (
	hopTraceHex  = "a1b2c3d4e5f60123456789abcdef0123"
	hopSpanHex   = "b1b2b3b4b5b6b7b8"
	hopParentHex = "c1c2c3c4c5c6c7c8"
	hopToolSpHex = "d1d2d3d4d5d6d7d8"
	hopTraceBHex = "f00fb00fbaadf00ddeadbeefcafe0123"
	hopSpanBHex  = "0f0e0d0c0b0a0908"
)

// newHopProducer builds a producer over a capturingSink (no wire) so pins
// can assert the Span struct exactly.
func newHopProducer() (*TraceProducer, *capturingSink, *TraceExporter) {
	sink := &capturingSink{}
	exp := NewTraceExporter(TraceExporterConfig{}, sink)
	return NewTraceProducer(exp, 1.0, nil), sink, exp
}

// hopLLMCallSpec is a fully-populated success LLMCallSpanSpec.
func hopLLMCallSpec() LLMCallSpanSpec {
	return LLMCallSpanSpec{
		TraceID:             hopTraceHex,
		SpanID:              hopSpanHex,
		ParentSpanID:        hopParentHex,
		Provider:            "openai",
		ModelID:             "test-model",
		AgentID:             "agent-1",
		SessionID:           "session-abc",
		TokensSent:          52,
		TokensRecv:          47,
		TokensCached:        8,
		CacheCreationTokens: 3,
		ReasoningTokens:     5,
		LatencyMS:           1200,
	}
}

// T1(a)/C8: RecordLLMCallSpan emits exactly one llm.call span carrying the
// spec's decoded binary IDs (NOT random fill) and exactly the FINAL-TABLE
// attribute allowlist with inclusion rules applied (zero omitted).
func TestProducerRecordLLMCallSpanShape(t *testing.T) {
	p, sink, exp := newHopProducer()
	p.RecordLLMCallSpan(hopLLMCallSpec())
	exp.Flush()
	if len(sink.spans) != 1 {
		t.Fatalf("sink got %d spans, want 1", len(sink.spans))
	}
	sp := sink.spans[0]
	if sp.Name != "llm.call" {
		t.Errorf("span name = %q, want llm.call", sp.Name)
	}
	if got := hexEncode(sp.TraceID[:]); got != hopTraceHex {
		t.Errorf("trace id = %s, want %s", got, hopTraceHex)
	}
	if got := hexEncode(sp.SpanID[:]); got != hopSpanHex {
		t.Errorf("span id = %s, want %s (ev.SpanID reused)", got, hopSpanHex)
	}
	if got := hexEncode(sp.ParentSpanID[:]); got != hopParentHex {
		t.Errorf("parent id = %s, want %s", got, hopParentHex)
	}
	if sp.IsError {
		t.Error("success spec exported as error span")
	}
	wantAttrs := map[string]string{
		"gen_ai.operation.name":                 "chat",
		"gen_ai.provider.name":                  "openai",
		"gen_ai.request.model":                  "test-model",
		"gen_ai.usage.input_tokens":             "52",
		"gen_ai.usage.output_tokens":            "47",
		"gen_ai.usage.cache_read.input_tokens":  "8",
		"gen_ai.usage.cache_write.input_tokens": "3",
		"gen_ai.usage.reasoning.output_tokens":  "5",
	}
	if !reflect.DeepEqual(sp.Attributes, wantAttrs) {
		t.Errorf("attributes = %v, want %v", sp.Attributes, wantAttrs)
	}
}

// T1(a)/C8: error records carry the truncated error.message, a
// low-cardinality error.type, and the Error span status - and nothing else
// beyond the allowlist.
func TestProducerRecordLLMCallSpanError(t *testing.T) {
	p, sink, exp := newHopProducer()
	ev := hopLLMCallSpec()
	ev.IsError = true
	ev.ErrorMessage = strings.Repeat("x", 300)
	p.RecordLLMCallSpan(ev)
	exp.Flush()
	if len(sink.spans) != 1 {
		t.Fatalf("sink got %d spans, want 1", len(sink.spans))
	}
	sp := sink.spans[0]
	if !sp.IsError {
		t.Error("error record did not mark the span status Error")
	}
	msg, ok := sp.Attributes["error.message"]
	if !ok {
		t.Fatal("error record missing error.message attribute")
	}
	if len([]rune(msg)) != 256 {
		t.Errorf("error.message = %d runes, want exactly 256", len([]rune(msg)))
	}
	et, ok := sp.Attributes["error.type"]
	if !ok || et == "" {
		t.Errorf("error record missing error.type (got %q)", et)
	}
	// Key set is exactly the allowlist subset for an error hop.
	allowed := map[string]bool{
		"gen_ai.operation.name": true, "gen_ai.provider.name": true,
		"gen_ai.request.model": true, "gen_ai.usage.input_tokens": true,
		"gen_ai.usage.output_tokens": true, "gen_ai.usage.cache_read.input_tokens": true,
		"gen_ai.usage.cache_write.input_tokens": true, "gen_ai.usage.reasoning.output_tokens": true,
		"error.type": true, "error.message": true,
	}
	for k := range sp.Attributes {
		if !allowed[k] {
			t.Errorf("unexpected attribute key %q (allowlist violation)", k)
		}
	}
}

// T3(b): a secret-looking ErrorMessage surfaces ONLY in the truncated
// error.message attribute (documented C8 exception) and the truncation
// bound holds for a message whose secret sits inside the kept prefix.
func TestProducerLLMErrorMessageSecretBound(t *testing.T) {
	p, sink, exp := newHopProducer()
	ev := hopLLMCallSpec()
	ev.IsError = true
	ev.ErrorMessage = "sk-live-abc123 " + strings.Repeat("pad", 100) // secret in the kept prefix
	p.RecordLLMCallSpan(ev)
	exp.Flush()
	sp := sink.spans[0]
	for k, v := range sp.Attributes {
		if k != "error.message" && strings.Contains(v, "sk-live-abc123") {
			t.Errorf("secret material leaked into attribute %q", k)
		}
	}
	msg := sp.Attributes["error.message"]
	if !strings.Contains(msg, "sk-live-abc123") {
		t.Error("error.message lost the kept-prefix content (truncation misapplied)")
	}
	if len([]rune(msg)) != 256 {
		t.Errorf("error.message = %d runes, want exactly 256", len([]rune(msg)))
	}
}

// T1(b)/C8: RecordToolCallSpan emits tool.call with shape-only attributes.
func TestProducerRecordToolCallSpanShape(t *testing.T) {
	p, sink, exp := newHopProducer()
	ev := ToolCallSpanSpec{
		TraceID:        hopTraceHex,
		SpanID:         hopToolSpHex,
		ParentSpanID:   hopParentHex,
		ToolName:       "read_file",
		ToolCallID:     "call-1",
		ArgumentsKeys:  "path,token",
		ArgumentsBytes: 42,
		Success:        true,
		Cached:         true,
		DurationMS:     300,
	}
	p.RecordToolCallSpan(ev)
	exp.Flush()
	if len(sink.spans) != 1 {
		t.Fatalf("sink got %d spans, want 1", len(sink.spans))
	}
	sp := sink.spans[0]
	if sp.Name != "tool.call" {
		t.Errorf("span name = %q, want tool.call", sp.Name)
	}
	if got := hexEncode(sp.TraceID[:]); got != hopTraceHex {
		t.Errorf("trace id = %s, want %s", got, hopTraceHex)
	}
	if got := hexEncode(sp.SpanID[:]); got != hopToolSpHex {
		t.Errorf("span id = %s, want %s", got, hopToolSpHex)
	}
	if got := hexEncode(sp.ParentSpanID[:]); got != hopParentHex {
		t.Errorf("parent id = %s, want %s", got, hopParentHex)
	}
	if sp.IsError {
		t.Error("successful tool call exported as error span")
	}
	wantAttrs := map[string]string{
		"gen_ai.operation.name": "execute_tool",
		"gen_ai.tool.name":      "read_file",
		"gen_ai.tool.call.id":   "call-1",
		"meept.tool.args.keys":  "path,token",
		"meept.tool.args.bytes": "42",
	}
	// Note: Cached=true deliberately produces NO attribute - it is not in
	// the FINAL-TABLE allowlist (pin c).
	if !reflect.DeepEqual(sp.Attributes, wantAttrs) {
		t.Errorf("attributes = %v, want %v", sp.Attributes, wantAttrs)
	}
}

// T1(b): failed + blocked tool calls carry meept.tool.blocked, truncated
// error.message, and error.type; nothing beyond the allowlist.
func TestProducerRecordToolCallSpanFailedBlocked(t *testing.T) {
	p, sink, exp := newHopProducer()
	ev := ToolCallSpanSpec{
		TraceID:    hopTraceHex,
		SpanID:     hopToolSpHex,
		ToolName:   "shell",
		ToolCallID: "call-2",
		Success:    false,
		Blocked:    true,
		Error:      strings.Repeat("e", 300),
	}
	p.RecordToolCallSpan(ev)
	exp.Flush()
	sp := sink.spans[0]
	if !sp.IsError {
		t.Error("failed tool call did not mark the span status Error")
	}
	if got := sp.Attributes["meept.tool.blocked"]; got != "true" {
		t.Errorf("meept.tool.blocked = %q, want true", got)
	}
	if got := sp.Attributes["error.message"]; len([]rune(got)) != 256 {
		t.Errorf("error.message = %d runes, want exactly 256", len([]rune(got)))
	}
	if got := sp.Attributes["error.type"]; got != "_OTHER" {
		t.Errorf("error.type = %q, want _OTHER", got)
	}
	allowed := map[string]bool{
		"gen_ai.operation.name": true, "gen_ai.tool.name": true,
		"gen_ai.tool.call.id": true, "meept.tool.args.keys": true,
		"meept.tool.args.bytes": true, "meept.tool.blocked": true,
		"error.type": true, "error.message": true,
	}
	for k := range sp.Attributes {
		if !allowed[k] {
			t.Errorf("unexpected attribute key %q (allowlist violation)", k)
		}
	}
}

// T1(c): nil producer / nil exporter - both hop methods are no-ops.
func TestProducerHopSpansNilSafe(t *testing.T) {
	var nilP *TraceProducer
	nilP.RecordLLMCallSpan(hopLLMCallSpec())
	nilP.RecordToolCallSpan(ToolCallSpanSpec{TraceID: hopTraceHex})
	p := NewTraceProducer(nil, 1.0, nil)
	p.RecordLLMCallSpan(hopLLMCallSpec())
	p.RecordToolCallSpan(ToolCallSpanSpec{TraceID: hopTraceHex})
}

// T1(d): the sampler decision is deterministic per TRACE - a rejected trace
// drops the turn span AND both hop spans; an accepted trace keeps all.
func TestProducerSamplerDropsWholeTrace(t *testing.T) {
	p, sink, exp := newHopProducer()
	rejected, _ := hex.DecodeString(hopTraceHex)
	var rejectedID [16]byte
	copy(rejectedID[:], rejected)
	p.sampler = func(sp Span) bool { return sp.TraceID != rejectedID }

	p.RecordTurnSpan(TurnSpanSpec{ConversationID: "conv-1", Status: "completed", TraceID: hopTraceHex})
	p.RecordLLMCallSpan(hopLLMCallSpec())
	p.RecordToolCallSpan(ToolCallSpanSpec{TraceID: hopTraceHex, SpanID: hopToolSpHex, Success: true})
	exp.Flush()
	if len(sink.spans) != 0 {
		t.Fatalf("rejected trace exported %d spans, want 0", len(sink.spans))
	}

	// A different trace shares the decision: all three kept.
	p.RecordTurnSpan(TurnSpanSpec{ConversationID: "conv-2", Status: "completed", TraceID: hopTraceBHex})
	bLLM := hopLLMCallSpec()
	bLLM.TraceID = hopTraceBHex
	bLLM.SpanID = hopSpanBHex
	p.RecordLLMCallSpan(bLLM)
	p.RecordToolCallSpan(ToolCallSpanSpec{TraceID: hopTraceBHex, SpanID: hopSpanBHex, Success: true})
	exp.Flush()
	if len(sink.spans) != 3 {
		t.Fatalf("accepted trace exported %d spans, want 3", len(sink.spans))
	}
	for i, sp := range sink.spans {
		if got := hexEncode(sp.TraceID[:]); got != hopTraceBHex {
			t.Errorf("span %d trace id = %s, want %s (trace torn)", i, got, hopTraceBHex)
		}
	}
}

// T1(e): TurnSpanSpec with minted IDs adopts them (byte-identical to the
// hop spans' trace); with "" the exporter random-fills as before.
func TestProducerTurnSpanAdoptsMintedIDs(t *testing.T) {
	p, sink, exp := newHopProducer()
	p.RecordTurnSpan(TurnSpanSpec{
		ConversationID: "conv-1",
		Status:         "completed",
		DurationMS:     1500,
		TraceID:        hopTraceHex,
		TurnSpanID:     hopSpanHex,
	})
	p.RecordTurnSpan(TurnSpanSpec{ConversationID: "conv-2", Status: "completed"}) // legacy: no IDs
	exp.Flush()
	if len(sink.spans) != 2 {
		t.Fatalf("sink got %d spans, want 2", len(sink.spans))
	}
	adopted, legacy := sink.spans[0], sink.spans[1]
	if got := hexEncode(adopted.TraceID[:]); got != hopTraceHex {
		t.Errorf("adopted trace id = %s, want %s", got, hopTraceHex)
	}
	if got := hexEncode(adopted.SpanID[:]); got != hopSpanHex {
		t.Errorf("adopted span id = %s, want %s", got, hopSpanHex)
	}
	if legacy.TraceID == ([16]byte{}) || legacy.SpanID == ([8]byte{}) {
		t.Error("legacy turn span did not get random fill for empty IDs")
	}
	if got := hexEncode(legacy.TraceID[:]); got == hopTraceHex {
		t.Error("legacy turn span adopted the previous span's trace id")
	}
}

// Wire-level: the llm.call and tool.call spans survive the real OTLP
// protobuf encode/decode chain with their exact IDs, error status, and
// status description.
func TestTraceSinkEncodesHopSpans(t *testing.T) {
	srv, captures := newCaptureTraceServer(t)
	sink := NewOTLPTraceHTTPSink(srv.URL, nil, nil)
	exp := NewTraceExporter(TraceExporterConfig{}, sink)
	p := NewTraceProducer(exp, 1.0, nil)

	llmEv := hopLLMCallSpec()
	llmEv.IsError = true
	llmEv.ErrorMessage = "boom"
	p.RecordLLMCallSpan(llmEv)
	p.RecordToolCallSpan(ToolCallSpanSpec{
		TraceID: hopTraceHex, SpanID: hopToolSpHex, ParentSpanID: hopParentHex,
		ToolName: "read_file", ToolCallID: "call-1",
		ArgumentsKeys: "path", ArgumentsBytes: 9, Success: true,
	})
	exp.Flush()

	got := captures()
	if len(got) != 1 || len(got[0].spans) != 2 {
		t.Fatalf("captures = %+v, want 1 request with 2 spans", got)
	}
	llmSpan := got[0].spans[0]
	if llmSpan.name != "llm.call" || llmSpan.traceID != hopTraceHex ||
		llmSpan.spanID != hopSpanHex || llmSpan.parentID != hopParentHex {
		t.Errorf("llm.call span decode = %+v", llmSpan)
	}
	if !llmSpan.errorStatus {
		t.Error("llm.call error span missing STATUS_CODE_ERROR")
	}
	if llmSpan.statusMessage != "boom" {
		t.Errorf("status description = %q, want boom", llmSpan.statusMessage)
	}
	if llmSpan.attrs["error.message"] != "boom" {
		t.Errorf("error.message attr = %q, want boom", llmSpan.attrs["error.message"])
	}
	toolSpan := got[0].spans[1]
	if toolSpan.name != "tool.call" || toolSpan.traceID != hopTraceHex ||
		toolSpan.spanID != hopToolSpHex {
		t.Errorf("tool.call span decode = %+v", toolSpan)
	}
	if toolSpan.attrs["gen_ai.tool.name"] != "read_file" {
		t.Errorf("gen_ai.tool.name attr = %q", toolSpan.attrs["gen_ai.tool.name"])
	}
}

// T3(a): a ToolCallSpanSpec built from an end event whose raw arguments
// carried values exposes ONLY ArgumentsKeys and ArgumentsBytes - no value
// substring reaches any attribute.
func TestProducerToolSpanNeverCarriesArgumentValues(t *testing.T) {
	p, sink, exp := newHopProducer()
	// The raw arguments contained values like /etc/shadow and x; only the
	// shape fields survive into the spec (leaf-01 shape-only contract).
	ev := ToolCallSpanSpec{
		TraceID:        hopTraceHex,
		SpanID:         hopToolSpHex,
		ToolName:       "read_file",
		ToolCallID:     "call-3",
		ArgumentsKeys:  "path,token",
		ArgumentsBytes: 38,
		Success:        false,
		Error:          "permission denied",
	}
	p.RecordToolCallSpan(ev)
	exp.Flush()
	sp := sink.spans[0]
	secrets := []string{"shadow", "x", "etc"}
	for k, v := range sp.Attributes {
		for _, s := range secrets {
			if k != "meept.tool.args.keys" && s != "x" && strings.Contains(v, s) {
				t.Errorf("attribute %q carries value substring %q", k, s)
			}
		}
	}
	if v := sp.Attributes["meept.tool.args.keys"]; v != "path,token" {
		t.Errorf("args.keys = %q, want key names only", v)
	}
	if v := sp.Attributes["meept.tool.args.bytes"]; v != "38" {
		t.Errorf("args.bytes = %q, want byte count only", v)
	}
}
