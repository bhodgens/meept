package agent

import "context"

// ReasoningSink is an OPTIONAL call-scoped collector for the model's
// separate-channel reasoning (llm.Response.Reasoning) produced during a
// turn. The default chat path never arms one (no sink in the context =
// zero behavior change: reasoning stays exactly as dropped as before);
// only callers that explicitly opt in — the daemon voice lane's
// loopTurnRunner — attach one and read it back after the turn.
//
// Why a context-carried sink rather than a return value on
// RunOnce/RunOnceWithParts: changing those signatures would touch every
// chat/lambda call site and the return-shape contract; a sink is
// per-CALL by construction, survives the spoken-guard rewrite retry
// (RunVoiceSpokenRewriteRetry re-enters the producer with the SAME
// context, so it overwrites the sink with the final reply's reasoning),
// and is invisible to any caller that does not set it.
type ReasoningSink struct {
	reasoning string
	set       bool
}

// Capture records the latest reasoning text. Last write wins — a multi-
// iteration turn's final response overwrites the earlier tool-call
// iterations' reasoning, and a bounded rewrite retry overwrites the
// pre-retry one, so the sink ends up holding the reasoning that produced
// the shipped reply text.
func (s *ReasoningSink) Capture(reasoning string) {
	if s == nil {
		return
	}
	if reasoning == "" {
		return
	}
	s.reasoning = reasoning
	s.set = true
}

// Get returns the captured reasoning ("" when nothing was captured or the
// sink is nil).
func (s *ReasoningSink) Get() string {
	if s == nil {
		return ""
	}
	return s.reasoning
}

// Has reports whether a non-empty reasoning was captured.
func (s *ReasoningSink) Has() bool {
	if s == nil {
		return false
	}
	return s.set && s.reasoning != ""
}

type reasoningSinkCtxKey struct{}

// WithReasoningSink attaches an opt-in reasoning sink to a turn context so
// the producer can capture llm.Response.Reasoning into it. A nil sink is a
// no-op (same as no sink). The context value is read by reasoningCycle on
// every successful /chat response.
func WithReasoningSink(ctx context.Context, sink *ReasoningSink) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, reasoningSinkCtxKey{}, sink)
}

// reasoningSinkFromContext returns the reasoning sink carried by ctx, or
// nil when none was set. Nil-safe.
func reasoningSinkFromContext(ctx context.Context) *ReasoningSink {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(reasoningSinkCtxKey{}).(*ReasoningSink)
	return s
}
