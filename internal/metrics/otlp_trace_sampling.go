// Package metrics — otlp_trace_sampling.go: deterministic per-trace
// sampling for the OTLP trace exporter (leaf 07 T1 "sample_rate honored").
//
// The decision is a pure function of the trace ID, so all spans of one
// trace sample identically (a tail torn by sampling is worse than no
// tail) and repeated flushes of the same trace are stable. Uses the
// trace ID's own entropy (crypto/rand bytes) — no hashing rounds needed:
// the leading 8 bytes are a uniform uint64 already.
package metrics

import "encoding/binary"

// sampleTrace reports whether a span's trace passes the configured rate.
// rate <= 0 drops everything; rate >= 1 keeps everything; otherwise the
// decision is deterministic in the trace ID: the leading 64 bits are
// compared against the threshold scaled into [0, 2^64).
func sampleTrace(sp Span, rate float64) bool {
	if rate >= 1 {
		return true
	}
	if rate <= 0 {
		return false
	}
	const scale = float64(1) / float64(1<<63) // 2^64 range via float division guard
	threshold := uint64(rate / scale / 2)     // scale rate into [0, 2^64)
	v := binary.BigEndian.Uint64(sp.TraceID[:8])
	return v < threshold
}
