package signalleg

import (
	"context"

	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/pkg/id"
)

// VoiceAttempt is the signal_voice contact.ChannelAttempt (leaf 02):
// Place a Signal call through the Leg (the full C4 gate stack: allowlist,
// capacity, effects, audit) and classify the terminal outcome. CONNECTED
// stops the chain (the call continues on its own session); busy, timeout,
// no-answer, and failed advance.
//
// outcome observes the ENDED event's mapped outcome (OutcomeByReason).
// The wiring adapts it onto the same collector the phone attempt uses
// (voice.OutcomeRegistry fed from the Manager release hook), or the
// leg-local stream - both carry voice.Outcome.
func (l *Leg) VoiceAttempt(outcome func(ctx context.Context, ccid string) (voice.Outcome, bool)) contact.ChannelAttempt {
	return &signalVoiceAttempt{leg: l, outcome: outcome}
}

type signalVoiceAttempt struct {
	leg     *Leg
	outcome func(ctx context.Context, ccid string) (voice.Outcome, bool)
}

// Channel pins the seam key (C1 vocabulary).
func (a *signalVoiceAttempt) Channel() contact.Channel { return contact.ChannelSignalVoice }

// Attempt places the call and classifies the terminal outcome. The
// firstLine is the chain's message (the wiring serves it as the talk
// loop's greeting line - the only sanctioned pre-supervisor speech).
// request_id: a fresh per-attempt identity (the telnyx voice attempt's
// shape, voice/voiceattempt.go) — the chain's repeated outreach to one
// contact must never collide on a completed receipt, while a caller
// WITH retry context passes the SAME id through Place to reuse it
// (C-V2).
func (a *signalVoiceAttempt) Attempt(ctx context.Context, rec contact.Record, message string) (string, error) {
	if a == nil || a.leg == nil {
		return string(voice.OutcomeFailed), errSignalAttemptUnwired
	}
	requestID := "contact-" + id.Generate("sv")
	ccid, err := a.leg.Place(ctx, a.leg.deps.EmployeeID, rec.SignalUUID, message, requestID)
	if err != nil {
		// Place refusals (disabled, allowlist, capacity) are honest
		// per-attempt failures: the chain advances.
		return string(voice.OutcomeFailed), err
	}
	if a.outcome == nil {
		// No outcome observer: the call was placed and rings. Fail-soft
		// terminal rather than blocking the chain forever.
		return string(voice.OutcomeAnswered), nil
	}
	out, ok := a.outcome(ctx, ccid)
	if !ok {
		return string(voice.OutcomeNoAnswer), errSignalOutcomeUnknown
	}
	switch out {
	case voice.OutcomeAnswered, voice.OutcomeHangup:
		return string(voice.OutcomeAnswered), nil
	default:
		return string(out), errSignalOutcomeAdvanced
	}
}

// Named errors (repo rule): non-nil err on every non-terminal outcome
// is the C2 contract (err == nil means CONNECTED/DELIVERED).
var (
	errSignalAttemptUnwired  = errorStr("signalleg: voice attempt not wired")
	errSignalOutcomeUnknown  = errorStr("signalleg: call outcome unavailable")
	errSignalOutcomeAdvanced = errorStr("signalleg: outcome advanced the chain")
)

// errorStr is the package-local sentinel helper.
type errorStr string

func (e errorStr) Error() string { return string(e) }

// OutcomeRegistry re-exports the shared collector shape for the signal
// wiring (the daemon feeds BOTH attempts from ONE release-hook
// observer). It is an alias: voice.OutcomeRegistry carries voice
// outcomes, which is exactly what OutcomeByReason produces.
type OutcomeRegistry = voice.OutcomeRegistry
