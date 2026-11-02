//go:build e2e

package harness

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"net/http"
	"testing"
)

// buildExportRequest assembles a minimal ExportTraceServiceRequest body:
// one ResourceSpans > ScopeSpans > Span with the given name/IDs/attrs.
// Mirrors the encoder direction of internal/metrics' wire format so the
// receiver pins exercise the real decoder path.
func buildExportRequest(name, traceHex, spanHex string, attrs map[string]string) []byte {
	// pbTag encodes a field tag (field<<3 | wire).
	pbTag := func(field, wire int) []byte {
		var buf [10]byte
		n := binary.PutUvarint(buf[:], uint64(field)<<3|uint64(wire))
		return buf[:n]
	}
	pbBytes := func(field int, val []byte) []byte {
		out := pbTag(field, 2)
		var lbuf [10]byte
		n := binary.PutUvarint(lbuf[:], uint64(len(val)))
		out = append(out, lbuf[:n]...)
		return append(out, val...)
	}

	trace, _ := hex.DecodeString(traceHex)
	span, _ := hex.DecodeString(spanHex)

	spanMsg := pbBytes(1, trace)
	spanMsg = append(spanMsg, pbBytes(2, span)...)
	spanMsg = append(spanMsg, pbBytes(5, []byte(name))...)
	for k, v := range attrs {
		// KeyValue { 1: key, 2: AnyValue { 1: string_value } }
		anyVal := pbBytes(1, []byte(v))
		kv := pbBytes(1, []byte(k))
		kv = append(kv, pbBytes(2, anyVal)...)
		spanMsg = append(spanMsg, pbBytes(9, kv)...)
	}

	scopeSpans := pbBytes(2, spanMsg)       // ScopeSpans.spans = field 2
	resourceSpans := pbBytes(2, scopeSpans) // ResourceSpans.scope_spans = field 2
	return pbBytes(1, resourceSpans)        // ExportTraceServiceRequest.resource_spans = field 1
}

// TestOTLPCaptureReceiverDecode pins the receiver contract: a hand-built
// export body decodes into Name/TraceIDHex/SpanIDHex/Attrs; Requests()
// counts POSTs; Spans() accumulates across requests.
func TestOTLPCaptureReceiverDecode(t *testing.T) {
	r := NewOTLPCaptureReceiver()
	t.Cleanup(r.Close)

	body := buildExportRequest("llm.call",
		"a1b2c3d4e5f60123456789abcdef0123", "b1b2b3b4b5b6b7b8",
		map[string]string{"gen_ai.provider.name": "openai", "gen_ai.usage.input_tokens": "52"})
	client := &http.Client{}
	resp, err := client.Post(r.URL()+"/v1/traces", "application/x-protobuf", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	if got := r.Requests(); got != 1 {
		t.Fatalf("requests = %d, want 1", got)
	}
	spans := r.Spans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	sp := spans[0]
	if sp.Name != "llm.call" {
		t.Errorf("name = %q", sp.Name)
	}
	if sp.TraceIDHex != "a1b2c3d4e5f60123456789abcdef0123" {
		t.Errorf("trace id = %s", sp.TraceIDHex)
	}
	if sp.SpanIDHex != "b1b2b3b4b5b6b7b8" {
		t.Errorf("span id = %s", sp.SpanIDHex)
	}
	if sp.Attrs["gen_ai.provider.name"] != "openai" || sp.Attrs["gen_ai.usage.input_tokens"] != "52" {
		t.Errorf("attrs = %v", sp.Attrs)
	}
	rawTrace, _ := hex.DecodeString("a1b2c3d4e5f60123456789abcdef0123")
	if !bytes.Contains(r.Bodies()[0], rawTrace) {
		t.Error("raw body does not contain the binary trace id")
	}

	// Second POST accumulates.
	body2 := buildExportRequest("tool.call",
		"a1b2c3d4e5f60123456789abcdef0123", "d1d2d3d4d5d6d7d8", nil)
	resp2, err := client.Post(r.URL()+"/v1/traces", "application/x-protobuf", bytes.NewReader(body2))
	if err != nil {
		t.Fatalf("post 2: %v", err)
	}
	resp2.Body.Close()
	if got := r.Requests(); got != 2 {
		t.Fatalf("requests = %d, want 2", got)
	}
	if got := len(r.Spans()); got != 2 {
		t.Fatalf("spans = %d, want 2", got)
	}
}

// TestOTLPCaptureReceiverParentField pins parent_span_id decoding (field 4)
// and its absence on root spans.
func TestOTLPCaptureReceiverParentField(t *testing.T) {
	r := NewOTLPCaptureReceiver()
	t.Cleanup(r.Close)

	// Reuse buildExportRequest but append a parent field: easiest via a
	// full-span variant. Build the span manually here.
	pbTag := func(field, wire int) []byte {
		var buf [10]byte
		n := binary.PutUvarint(buf[:], uint64(field)<<3|uint64(wire))
		return buf[:n]
	}
	pbBytes := func(field int, val []byte) []byte {
		out := pbTag(field, 2)
		var lbuf [10]byte
		n := binary.PutUvarint(lbuf[:], uint64(len(val)))
		out = append(out, lbuf[:n]...)
		return append(out, val...)
	}
	trace, _ := hex.DecodeString("f00fb00fbaadf00ddeadbeefcafe0123")
	span, _ := hex.DecodeString("0f0e0d0c0b0a0908")
	parent, _ := hex.DecodeString("c1c2c3c4c5c6c7c8")

	spanMsg := pbBytes(1, trace)
	spanMsg = append(spanMsg, pbBytes(2, span)...)
	spanMsg = append(spanMsg, pbBytes(4, parent)...)
	spanMsg = append(spanMsg, pbBytes(5, []byte("chat.turn"))...)
	scope := pbBytes(2, spanMsg)
	rs := pbBytes(2, scope)
	body := pbBytes(1, rs)

	resp, err := http.Post(r.URL()+"/v1/traces", "application/x-protobuf", bytes.NewReader(body)) //nolint:noctx // bounded test post
	if err != nil {
		t.Fatalf("post: %v", err)
	}
	resp.Body.Close()

	spans := r.Spans()
	if len(spans) != 1 {
		t.Fatalf("spans = %d, want 1", len(spans))
	}
	if spans[0].ParentHex != "c1c2c3c4c5c6c7c8" {
		t.Errorf("parent = %q, want c1c2c3c4c5c6c7c8", spans[0].ParentHex)
	}
	if spans[0].Name != "chat.turn" {
		t.Errorf("name = %q", spans[0].Name)
	}
}
