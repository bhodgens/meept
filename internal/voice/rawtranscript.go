package voice

import "context"

// Leaf 03 (T3) raw-transcript context transport. The child normalizes the
// STT final BEFORE the prompt frame's Text reaches the talk loop's turn
// dispatch; the RAW (un-normalized) transcript rides the frame's From
// field and the wiring swaps it into the turn context here, so the
// binding can persist the caller's actual words (AppendTranscript keeps
// RAW) while the LLM session runs on the normalized prompt.

// rawTranscriptContextKey scopes the raw-transcript stamp on turn
// contexts (dedicated struct key, repo SA1029 pattern).
type rawTranscriptContextKey struct{}

// WithRawTranscript stamps the raw (un-normalized) caller transcript onto
// a turn context. An empty raw stamp is a no-op (nothing to carry).
func WithRawTranscript(ctx context.Context, raw string) context.Context {
	if raw == "" {
		return ctx
	}
	return context.WithValue(ctx, rawTranscriptContextKey{}, raw)
}

// RawTranscriptFrom reads the raw transcript off a turn context; "" when
// absent (older callers, tests).
func RawTranscriptFrom(ctx context.Context) string {
	if v, ok := ctx.Value(rawTranscriptContextKey{}).(string); ok {
		return v
	}
	return ""
}
