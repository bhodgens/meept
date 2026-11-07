// Package metrics — otlp_trace_sink.go: concrete OTLP TraceSink that POSTs
// span batches as OTLP/HTTP protobuf (ExportTraceServiceRequest) to the
// receiver's /v1/traces endpoint. Hand-rolled minimal protobuf writer in the
// style of otlp_http_sink.go — the OpenTelemetry SDK stays out of the
// dependency tree (go.mod untouched; stdlib net/http + encoding/binary only).
//
// The wire shape is the standard OTLP/HTTP protobuf export request, so any
// spec-compliant receiver works — including self-hosted Langfuse v3+ at
// /api/public/otel (Langfuse additionally wants the
// "x-langfuse-ingestion-version: 4" header for realtime OTLP ingestion;
// pass it — and any auth material — via the Headers config map. Header
// values are never logged).
package metrics

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strings"
	"sync"
	"time"
)

// Trace/circuit tuning for the trace sink. Small bounded constants: the
// export path is best-effort and must never wedge the caller.
const (
	// traceSinkTimeout bounds each export request.
	traceSinkTimeout = 10 * time.Second
	// traceCircuitThreshold is the consecutive-failure count that opens the
	// circuit. While open, batches are dropped locally (counted) and no
	// network call is attempted.
	traceCircuitThreshold = 3
	// traceCircuitCooldown is how long the circuit stays open before the
	// next export attempt probes the receiver again.
	traceCircuitCooldown = 30 * time.Second
	// DefaultOTLPTracesScope is the instrumentation scope name on exported
	// spans.
	DefaultOTLPTracesScope = "github.com/caimlas/meept/internal/metrics"
	// DefaultTraceServiceName is the resource service.name when unset.
	DefaultTraceServiceName = "meept-daemon"
)

// errTraceCircuitOpen is returned while the circuit is open. Callers treat
// it as a drop, not a retry signal.
var errTraceCircuitOpen = errors.New("otlp trace export circuit open")

// TraceSink is the bridge that ships a batch of spans off-host. The
// TraceExporter talks through this interface so tests can inject a capture
// implementation without any OTLP dependency.
type TraceSink interface {
	// ExportSpans sends a batch of spans to the receiver. Best-effort:
	// implementations must not block unboundedly and failures are the
	// implementation's own logging concern.
	ExportSpans(ctx context.Context, spans []Span) error

	// Close flushes remaining data and releases resources.
	Close() error
}

// Span is a single trace span ready for export. Producers fill Name, Start,
// End and business Attributes; the exporter fills zero trace/span IDs from
// crypto/rand and the sink adds the shared base attributes (gen_ai.*,
// langfuse.*, trace_name) at serialization time.
type Span struct {
	TraceID      [16]byte
	SpanID       [8]byte
	ParentSpanID [8]byte
	Name         string
	Start        time.Time
	End          time.Time
	Attributes   map[string]string
	// Correlation fields for the shared base attributes. Zero values simply
	// omit the corresponding langfuse.*/gen_ai.* attribute.
	ConversationID string
	SessionID      string
	AgentID        string
	// IsError marks the span status as STATUS_CODE_ERROR.
	IsError bool
}

// OTLPTraceHTTPSink implements TraceSink over plain HTTP POST of an
// OTLP/HTTP protobuf ExportTraceServiceRequest. One batch = one request.
// Failures are logged once per circuit transition (see noteFailure): a dead
// receiver produces at most one Warn per threshold-crossing and one Info on
// recovery — never steady-state noise — and batches arriving while the
// circuit is open are dropped locally with a counter.
type OTLPTraceHTTPSink struct {
	endpoint string // full URL: <base>/v1/traces
	client   *http.Client
	headers  map[string]string
	logger   *slog.Logger
	timeout  time.Duration

	mu               sync.Mutex
	consecFails      int
	circuitOpenUntil time.Time
	droppedByCircuit int
}

// Compile-time bridge check.
var _ TraceSink = (*OTLPTraceHTTPSink)(nil)

// NewOTLPTraceHTTPSink builds a sink that POSTs to <endpoint>/v1/traces.
// The endpoint is the OTLP receiver BASE URL (e.g.
// http://langfuse:3000/api/public/otel) — the /v1/traces suffix is appended
// here, the standard OTLP/HTTP path. Headers (auth, x-langfuse-ingestion-
// version, ...) are attached to every request and NEVER logged.
func NewOTLPTraceHTTPSink(endpoint string, headers map[string]string, logger *slog.Logger) *OTLPTraceHTTPSink {
	if logger == nil {
		logger = slog.Default()
	}
	endpoint = strings.TrimRight(endpoint, "/")
	return &OTLPTraceHTTPSink{
		endpoint: endpoint + "/v1/traces",
		client:   &http.Client{Timeout: traceSinkTimeout},
		headers:  headers,
		logger:   logger.With("component", "otlp-trace-sink"),
		timeout:  traceSinkTimeout,
	}
}

// ExportSpans serializes the batch into an OTLP/HTTP protobuf export
// request and POSTs it. Context-aware: an already-canceled ctx (daemon
// shutdown) skips the send. While the circuit is open the batch is dropped
// locally (counted) and no network call is made.
func (s *OTLPTraceHTTPSink) ExportSpans(ctx context.Context, spans []Span) error {
	if len(spans) == 0 {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return fmt.Errorf("otlp trace export skipped (context done): %w", err)
	}
	if s.circuitBlocked() {
		return errTraceCircuitOpen
	}

	body := buildTraceExportRequest(s.serviceName(), spans)

	ctx, cancel := context.WithTimeout(ctx, s.timeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		s.noteFailure(err)
		return err
	}
	req.Header.Set("Content-Type", "application/x-protobuf")
	for k, v := range s.headers {
		req.Header.Set(k, v)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		s.noteFailure(err)
		return err
	}
	// Drain + close are best-effort cleanup on a body we never read; a
	// drain failure cannot change the export outcome (status is checked
	// below and errors already flow through noteFailure).
	bodyDrainAndClose(resp.Body)

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		err := fmt.Errorf("otlp trace receiver returned status %d", resp.StatusCode)
		s.noteFailure(err)
		return err
	}

	s.noteSuccess()
	return nil
}

// serviceName returns the resource service.name for the export request.
func (s *OTLPTraceHTTPSink) serviceName() string {
	if v, ok := s.headers["service.name"]; ok && v != "" {
		return v
	}
	return DefaultTraceServiceName
}

// circuitBlocked reports (and counts) a drop while the circuit is open.
func (s *OTLPTraceHTTPSink) circuitBlocked() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consecFails >= traceCircuitThreshold && time.Now().Before(s.circuitOpenUntil) {
		s.droppedByCircuit++ // one counter increment per dropped batch
		return true
	}
	return false
}

// DroppedByCircuit reports how many batches were dropped while the circuit
// was open.
func (s *OTLPTraceHTTPSink) DroppedByCircuit() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.droppedByCircuit
}

// Close is a no-op: the sink holds no persistent connections or buffers
// (each ExportSpans is a self-contained request). Present to satisfy
// TraceSink.
func (s *OTLPTraceHTTPSink) Close() error { return nil }

// noteFailure records a failed export. Logging is bounded: the threshold
// crossing that OPENS the circuit logs exactly one Warn line; intermediate
// failures stay at Debug. A dead receiver therefore produces one circuit
// line per cooldown window at most — never steady-state noise.
func (s *OTLPTraceHTTPSink) noteFailure(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.consecFails++
	if s.consecFails == traceCircuitThreshold {
		s.circuitOpenUntil = time.Now().Add(traceCircuitCooldown)
		s.logger.Warn("otlp trace export failing; circuit open, dropping batches until cooldown elapses",
			"endpoint", s.endpoint,
			"consecutive_failures", s.consecFails,
			"cooldown", traceCircuitCooldown.String(),
			"error", err)
		return
	}
	if s.consecFails < traceCircuitThreshold {
		s.logger.Debug("otlp trace export failed", "endpoint", s.endpoint, "error", err)
	}
}

// noteSuccess resets the failure streak and closes the circuit, logging one
// Info line when a failure streak ends.
func (s *OTLPTraceHTTPSink) noteSuccess() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.consecFails > 0 {
		s.logger.Info("otlp trace export recovered",
			"endpoint", s.endpoint, "consecutive_failures", s.consecFails)
	}
	s.consecFails = 0
	s.circuitOpenUntil = time.Time{}
}

// ---------------------------------------------------------------------------
// Minimal OTLP/HTTP protobuf writer
//
// Wire reference (opentelemetry/proto/trace/v1 + collector trace_service):
//
//	ExportTraceServiceRequest { repeated ResourceSpans resource_spans = 1 }
//	ResourceSpans { Resource resource = 1; repeated ScopeSpans scope_spans = 2 }
//	Resource { repeated KeyValue attributes = 1 }
//	ScopeSpans { InstrumentationScope scope = 1; repeated Span spans = 2 }
//	InstrumentationScope { string name = 1; string version = 2 }
//	Span { bytes trace_id=1; bytes span_id=2; bytes parent_span_id=4;
//	       string name=5; fixed64 start_time_unix_nano=7;
//	       fixed64 end_time_unix_nano=8; repeated KeyValue attributes=9;
//	       Status status=15 }
//	KeyValue { string key = 1; AnyValue value = 2 }
//	AnyValue { string string_value = 1 }
//	Status { string message = 1; StatusCode code = 2 }  // 2 = STATUS_CODE_ERROR
// ---------------------------------------------------------------------------

func appendVarint(dst []byte, v uint64) []byte {
	for v >= 0x80 {
		dst = append(dst, byte(v)|0x80)
		v >>= 7
	}
	return append(dst, byte(v))
}

// appendTag writes a field tag (field number + wire type).
func appendTag(dst []byte, field, wire int) []byte {
	return appendVarint(dst, uint64(field)<<3|uint64(wire))
}

// appendBytesField writes a length-delimited field (wire type 2).
func appendBytesField(dst []byte, field int, payload []byte) []byte {
	dst = appendTag(dst, field, 2)
	dst = appendVarint(dst, uint64(len(payload)))
	return append(dst, payload...)
}

func appendString(dst []byte, field int, s string) []byte {
	return appendBytesField(dst, field, []byte(s))
}

// appendFixed64Field writes a fixed64 field (wire type 1, little-endian).
func appendFixed64Field(dst []byte, field int, v uint64) []byte {
	dst = appendTag(dst, field, 1)
	var b [8]byte
	binary.LittleEndian.PutUint64(b[:], v)
	return append(dst, b[:]...)
}

func appendKeyValue(dst []byte, key, value string) []byte {
	var kv, av []byte
	kv = appendString(kv, 1, key)
	av = appendString(av, 1, value)
	kv = appendBytesField(kv, 2, av)
	return appendBytesField(dst, 9, kv) // Span.attributes = 9; Resource.attributes = 1 is handled by callers
}

// spanBaseAttributes returns the shared per-span attribute set every
// exported span carries (Langfuse filters operate per-observation, so the
// langfuse.*/trace_name correlation attributes ride on EVERY span, not just
// the trace root). Secret material never appears here — auth rides on the
// Headers config, not span attributes.
func spanBaseAttributes(sp Span) [][2]string {
	attrs := [][2]string{
		{"trace_name", sp.Name},
		{"gen_ai.system", DefaultTraceServiceName},
	}
	if sp.ConversationID != "" {
		attrs = append(attrs, [2]string{"gen_ai.conversation.id", sp.ConversationID})
	}
	if sp.AgentID != "" {
		attrs = append(attrs, [2]string{"langfuse.user.id", sp.AgentID})
	}
	if sp.SessionID != "" {
		attrs = append(attrs, [2]string{"langfuse.session.id", sp.SessionID})
	} else if sp.ConversationID != "" {
		// Langfuse session grouping falls back to the conversation id when
		// no session id is known (conversation/session are distinct ids in
		// meept; bus events carry conversation_id).
		attrs = append(attrs, [2]string{"langfuse.session.id", sp.ConversationID})
	}
	return attrs
}

// appendSpanAttributes writes Span.attributes (field 9): the business
// attributes sorted by key (deterministic wire output for tests and
// snapshot diffs) followed by the shared base attributes.
func appendSpanAttributes(dst []byte, sp Span) []byte {
	keys := make([]string, 0, len(sp.Attributes))
	for k := range sp.Attributes {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		dst = appendKeyValue(dst, k, sp.Attributes[k])
	}
	for _, kv := range spanBaseAttributes(sp) {
		dst = appendKeyValue(dst, kv[0], kv[1])
	}
	return dst
}

// encodeSpan encodes one Span message.
func encodeSpan(sp Span) []byte {
	var out []byte
	if tid := sp.TraceID; tid != ([16]byte{}) {
		out = appendBytesField(out, 1, tid[:]) // trace_id
	}
	if sid := sp.SpanID; sid != ([8]byte{}) {
		out = appendBytesField(out, 2, sid[:]) // span_id
	}
	if pid := sp.ParentSpanID; pid != ([8]byte{}) {
		out = appendBytesField(out, 4, pid[:]) // parent_span_id
	}
	out = appendString(out, 5, sp.Name) // name
	if !sp.Start.IsZero() {
		out = appendFixed64Field(out, 7, uint64(sp.Start.UnixNano())) // start_time_unix_nano
	}
	if !sp.End.IsZero() {
		out = appendFixed64Field(out, 8, uint64(sp.End.UnixNano())) // end_time_unix_nano
	}
	out = appendSpanAttributes(out, sp)
	if sp.IsError {
		// Leaf 03 (RESEARCH-FINDINGS note E1): on a failed operation the
		// span status DESCRIPTION carries the error message (the
		// convention-preferred channel); the truncated message rides the
		// error.message attribute in addition for SQL-join workflows. The
		// producer truncates before it ever gets here.
		var status []byte
		if msg := sp.Attributes["error.message"]; msg != "" {
			status = appendString(status, 1, msg) // Status.message field 1
		}
		status = appendVarint(status, 2<<3) // Status.code field 2, varint
		status = appendVarint(status, 2)    // STATUS_CODE_ERROR
		out = appendBytesField(out, 15, status)
	}
	return out
}

// buildTraceExportRequest serializes spans into an ExportTraceServiceRequest
// with a single ResourceSpans/ScopeSpans pair.
func buildTraceExportRequest(serviceName string, spans []Span) []byte {
	var res []byte
	res = appendString(res, 1, "service.name") // Resource.attributes = 1
	res = appendBytesField(res, 2, func() []byte {
		var av []byte
		return appendString(av, 1, serviceName)
	}())

	var scope []byte
	scope = appendString(scope, 1, DefaultOTLPTracesScope)

	var ss []byte
	ss = appendBytesField(ss, 1, scope)
	for i := range spans {
		ss = appendBytesField(ss, 2, encodeSpan(spans[i]))
	}

	var rs []byte
	rs = appendBytesField(rs, 1, res)
	rs = appendBytesField(rs, 2, ss)

	var req []byte
	return appendBytesField(req, 1, rs)
}

// The drain-and-close helper below is shared with otlp_http_sink.go via the
// same-package definition there; it is intentionally not redefined.
var _ = io.Discard
