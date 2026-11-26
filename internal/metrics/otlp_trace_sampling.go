// Package metrics — otlp_trace_sampling.go: deterministic per-trace
// sampling for the OTLP trace exporter (leaf 07 T1 "sample_rate honored").
//
// The decision is a pure function of the trace ID, so all spans of one
// trace sample identically (a tail torn by sampling is worse than no
// tail) and repeated flushes of the same trace are stable. Uses the
// trace ID's own entropy (crypto/rand bytes) — no hashing rounds needed:
// the leading 8 bytes are a uniform uint64 already.
package metrics

import (
	"crypto/sha256"
	"encoding/binary"
	"math"
)

// sampleTrace reports whether a span's trace passes the configured rate.
// rate <= 0 drops everything; rate >= 1 keeps everything; otherwise the
// decision is deterministic in the trace ID: the leading 64 bits are
// compared against the threshold scaled into [0, 2^64).
//
// Bughunt A1-7 fix: the previous scale math (rate / (1/2^63) / 2)
// produced threshold = rate*2^62 — a quarter of the correct cut, so a
// configured 0.25 kept ~6.25%. The threshold is now the exact uniform
// cut: v < rate * 2^64, expressed via float64(math.MaxUint64) (~2^64 for
// any rate strictly below 1, and rate >= 1 short-circuits above, so the
// uint64 conversion can never see 2^64).
func sampleTrace(sp Span, rate float64) bool {
	if rate >= 1 {
		return true
	}
	if rate <= 0 {
		return false
	}
	threshold := uint64(rate * float64(math.MaxUint64)) // uniform cut in [0, 2^64)
	v := binary.BigEndian.Uint64(sp.TraceID[:8])
	return v < threshold
}

// deriveTraceID maps a stable grouping key (e.g. a task id) to a
// deterministic trace ID via SHA-256, so every span that belongs to the
// same logical trace samples identically (bughunt A1-7: step spans
// carried no TraceID, the sampler saw the zero value and kept 100%).
// An empty key returns the zero ID, which the exporter fills from
// crypto/rand (no synthetic entropy here).
func deriveTraceID(key string) [16]byte {
	var out [16]byte
	if key == "" {
		return out
	}
	sum := sha256.Sum256([]byte(key))
	copy(out[:], sum[:16])
	return out
}
