package id

import (
	"crypto/rand"
	"encoding/hex"
)

// TraceID mints an OpenTelemetry-shaped trace identifier: 16 random bytes
// rendered as 32 lowercase hex chars (the canonical representation every
// tracing backend accepts -- Jaeger UI search, Tempo GET /api/traces/<id>,
// TraceQL). Uses crypto/rand for uniqueness and unpredictability.
//
// Entropy contract: on ANY crypto/rand failure (catastrophic system
// failure -- entropy exhaustion, broken /dev/urandom), TraceID returns the
// zero fallback "000...0" of the FULL 32-char length rather than panicking.
// Like Generate's zero-suffix fallback, this value is predictable and not
// unique; triggering it means the host is already in an unrecoverable
// state where panic would be worse than a degraded ID. Callers that need
// hard uniqueness guarantees should treat an all-zero trace ID as a fatal
// signal. Never time.Now()/math/rand -- see the predictable-ID prevention
// rule (predid analyzer).
func TraceID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// Fallback should never happen on supported platforms. The
		// all-zero value still works as a correlation key, just not
		// guaranteed unique.
		return "00000000000000000000000000000000"
	}
	return hex.EncodeToString(b)
}

// SpanID mints an OpenTelemetry-shaped span identifier: 8 random bytes
// rendered as 16 lowercase hex chars. Entropy and fallback contract mirror
// TraceID: crypto/rand only, and the documented all-zero "000...0" fallback
// of the full 16-char length on entropy failure -- never a predictable
// substitute.
func SpanID() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(b)
}
