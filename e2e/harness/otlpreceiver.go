package harness

import (
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
)

// OTLPCaptureReceiver is an in-process OTLP/HTTP capture receiver for the
// hermetic e2e tier: an httptest server accepting protobuf POSTs at
// /v1/traces, keeping every raw body and a lenient decode of each span.
// No protobuf dependency - the decode walks the wire format by field
// number (same approach as internal/metrics/otlp_trace_test.go, which e2e
// cannot import), only fields the suite asserts on.
//
// Deterministic: every request is drained and decoded synchronously in the
// handler; accessors are mutex-guarded snapshots; there are no background
// timers. Suites stop the daemon (harness Daemon.Stop) to force the
// exporter's final flush, then poll Spans() with a deadline.
type OTLPCaptureReceiver struct {
	server *httptest.Server

	mu       sync.Mutex
	requests int
	bodies   [][]byte // raw request bodies, arrival order
	spans    []CapturedSpan
}

// CapturedSpan is the lenient decode of one Span message inside an export
// request (OTLP trace.proto field numbers).
type CapturedSpan struct {
	Name       string
	TraceIDHex string // 16 bytes -> 32-char lowercase hex
	SpanIDHex  string // 8 bytes -> 16-char lowercase hex
	ParentHex  string // parent_span_id, "" when absent
	Attrs      map[string]string
}

// NewOTLPCaptureReceiver starts the capture server on an ephemeral
// loopback port. The caller registers t.Cleanup(receiver.Close).
func NewOTLPCaptureReceiver() *OTLPCaptureReceiver {
	r := &OTLPCaptureReceiver{}
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/traces", func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodPost {
			http.Error(w, "post only", http.StatusMethodNotAllowed)
			return
		}
		body := make([]byte, 0, 4096)
		buf := make([]byte, 32*1024)
		for {
			n, err := req.Body.Read(buf)
			body = append(body, buf[:n]...)
			if err != nil {
				break
			}
		}
		r.mu.Lock()
		r.requests++
		r.bodies = append(r.bodies, body)
		r.spans = append(r.spans, decodeExportRequest(body)...)
		r.mu.Unlock()
		w.WriteHeader(http.StatusOK)
	})
	r.server = httptest.NewServer(mux)
	return r
}

// URL is the receiver base URL for metrics.otlp_traces.endpoint (spans
// POST to URL + "/v1/traces").
func (r *OTLPCaptureReceiver) URL() string { return r.server.URL }

// Requests is the count of export POSTs received so far.
func (r *OTLPCaptureReceiver) Requests() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.requests
}

// Bodies returns copies of the raw request bodies in arrival order.
func (r *OTLPCaptureReceiver) Bodies() [][]byte {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([][]byte, len(r.bodies))
	for i, b := range r.bodies {
		out[i] = append([]byte(nil), b...)
	}
	return out
}

// Spans returns every span decoded so far (all requests concatenated).
func (r *OTLPCaptureReceiver) Spans() []CapturedSpan {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]CapturedSpan, len(r.spans))
	copy(out, r.spans)
	return out
}

// Close shuts the server down (idempotent via httptest).
func (r *OTLPCaptureReceiver) Close() { r.server.Close() }

// --- lenient protobuf walk --------------------------------------------------
//
// OTLP trace.proto shapes used here (field numbers only):
//
//	ExportTraceServiceRequest { 1: repeated ResourceSpans }
//	ResourceSpans           { 2: repeated ScopeSpans }
//	ScopeSpans              { 2: repeated Span }
//	Span { 1: trace_id bytes, 2: span_id bytes, 4: parent_span_id bytes,
//	       5: name string, 9: repeated KeyValue attributes }
//	KeyValue                { 1: key string, 2: AnyValue }
//	AnyValue                { 1: string_value }

// pbField is one decoded protobuf field (tag key, wire type, payload).
type pbField struct {
	field   int
	wire    int
	varint  uint64
	fixed64 uint64
	bytes   []byte
}

// walkProtobuf splits one message into its fields. Malformed trailing data
// is dropped (lenient by design - the receiver must never fail an export).
func walkProtobuf(data []byte) []pbField {
	var out []pbField
	for len(data) > 0 {
		key, n := binary.Uvarint(data)
		if n <= 0 {
			return out
		}
		data = data[n:]
		field := int(key >> 3)
		wire := int(key & 7)
		switch wire {
		case 0:
			v, n := binary.Uvarint(data)
			if n <= 0 {
				return out
			}
			data = data[n:]
			out = append(out, pbField{field: field, wire: wire, varint: v})
		case 1:
			if len(data) < 8 {
				return out
			}
			out = append(out, pbField{field: field, wire: wire, fixed64: binary.LittleEndian.Uint64(data[:8])})
			data = data[8:]
		case 2:
			l, n := binary.Uvarint(data)
			if n <= 0 || l > uint64(len(data[n:])) {
				return out
			}
			data = data[n:]
			out = append(out, pbField{field: field, wire: wire, bytes: data[:l]})
			data = data[l:]
		case 5:
			if len(data) < 4 {
				return out
			}
			data = data[4:]
		default:
			return out
		}
	}
	return out
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

// decodeExportRequest walks ResourceSpans -> ScopeSpans -> spans.
func decodeExportRequest(body []byte) []CapturedSpan {
	var out []CapturedSpan
	for _, rsf := range fieldsByField(walkProtobuf(body), 1) { // resource_spans
		for _, ssf := range fieldsByField(walkProtobuf(rsf.bytes), 2) { // scope_spans
			for _, spf := range fieldsByField(walkProtobuf(ssf.bytes), 2) { // spans
				out = append(out, decodeSpan(spf.bytes))
			}
		}
	}
	return out
}

func decodeSpan(data []byte) CapturedSpan {
	sp := CapturedSpan{Attrs: map[string]string{}}
	for _, f := range walkProtobuf(data) {
		switch f.field {
		case 1:
			sp.TraceIDHex = hex.EncodeToString(f.bytes)
		case 2:
			sp.SpanIDHex = hex.EncodeToString(f.bytes)
		case 4:
			sp.ParentHex = hex.EncodeToString(f.bytes)
		case 5:
			sp.Name = string(f.bytes)
		case 9: // attributes: KeyValue{1: key, 2: AnyValue{1: string_value}}
			kv := walkProtobuf(f.bytes)
			var key, val string
			if kf := fieldsByField(kv, 1); len(kf) > 0 {
				key = string(kf[0].bytes)
			}
			if af := fieldsByField(kv, 2); len(af) > 0 {
				if sv := fieldsByField(walkProtobuf(af[0].bytes), 1); len(sv) > 0 {
					val = string(sv[0].bytes)
				}
			}
			sp.Attrs[key] = val
		}
	}
	return sp
}

// BodiesContain reports whether any captured raw body contains substr -
// the byte-level fallback assertion (e.g. the trace-id hex must appear in
// the exported payload).
func (r *OTLPCaptureReceiver) BodiesContain(substr string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, b := range r.bodies {
		if strings.Contains(string(b), substr) {
			return true
		}
	}
	return false
}
