package voice

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/pkg/id"
)

// VoiceAttemptOutcomeFunc observes one call's terminal outcome (leaf 02
// T2). The wiring adapts it onto the manager's release-hook stream: the
// SAME terminal event that drives the release fan-out carries the
// outcome here, so the chain reads honest call state (the ended-state
// fix, GH #123, is the prereq that made outcomes observable).
type VoiceAttemptOutcomeFunc func(ctx context.Context, ccid string) (voiceOutcome, bool)

// voiceOutcome aliases the manager's Outcome so this file's signature
// reads without dragging the concrete manager type into call sites that
// already have it (voice.Outcome == voiceOutcome by assignment).
type voiceOutcome = Outcome

// NewTelnyxVoiceAttempt is the telnyx_voice contact.ChannelAttempt
// (leaf 02 T2): dial through the EXISTING outbound path, then wait for
// the terminal outcome. A CONNECTED call ("answered"; hangup after a
// live conversation is chain-terminal too) STOPS the chain - the call
// continues on its own session. no_answer, busy, and failed advance.
//
// dial is the daemon's existing outbound dial closure (dialForTool);
// outcome observes the terminal outcome. Both injected: this adapter
// owns NO dialing logic of its own.
func NewTelnyxVoiceAttempt(
	dial func(ctx context.Context, employeeID, target, requestID string) (ccid string, err error),
	outcome VoiceAttemptOutcomeFunc,
) contact.ChannelAttempt {
	return &telnyxVoiceAttempt{dial: dial, outcome: outcome}
}

type telnyxVoiceAttempt struct {
	dial    func(ctx context.Context, employeeID, target, requestID string) (string, error)
	outcome VoiceAttemptOutcomeFunc
}

// Channel pins the seam key (C1 vocabulary).
func (a *telnyxVoiceAttempt) Channel() contact.Channel { return contact.ChannelTelnyxVoice }

// OutcomeTimeout is the DISTINCT attempt outcome for an outcome wait that
// hit its deadline (A5-1/A6-12 leg): the wiring's chain-run cap (the 10-min
// context.WithTimeout in the daemon's contact chain runner) expires while the
// call is still live — frequently one the callee ALREADY ANSWERED (the outcome
// registry only records at call teardown). Recording that as no_answer made
// the chain report a connected long call as unanswered and advance to the next
// channel; "timeout" names what actually happened (the wait, not the callee).
// Like no_answer/busy it is non-terminal for the chain (advances), but honest
// in the report line.
const OutcomeTimeout Outcome = "timeout"

// Attempt places the call and classifies the terminal outcome.
// request_id comes from pkg/id (the chain supplies per-attempt
// identity; the effects ledger's retry protection rides the dial path's
// own idempotency as today).
func (a *telnyxVoiceAttempt) Attempt(ctx context.Context, rec contact.Record, message string) (string, error) {
	if a == nil || a.dial == nil {
		return string(OutcomeFailed), errVoiceAttemptUnwired
	}
	requestID := "contact-" + id.Generate("cv")
	ccid, err := a.dial(ctx, "", rec.Phone, requestID)
	if err != nil {
		return string(OutcomeFailed), err
	}
	if a.outcome == nil {
		// No outcome observer wired: the dial landed (someone was rung).
		// Fail-soft terminal rather than blocking the chain forever.
		return string(OutcomeAnswered), nil
	}
	out, ok := a.outcome(ctx, ccid)
	if !ok {
		// Lookup answers false ONLY on ctx expiry/cancellation (it polls
		// otherwise), so this is the call-cap leg, not an unanswered
		// call: the call may be live (or answered) with no terminal
		// record yet. Report the distinct 'timeout' outcome instead of
		// misreporting no_answer (A5-1).
		return string(OutcomeTimeout), errOutcomeWaitTimeout
	}
	switch out {
	case OutcomeAnswered, OutcomeHangup:
		// Terminal for the chain; a live call continues on its session.
		return string(OutcomeAnswered), nil
	default:
		return string(out), errOutcomeAdvanced
	}
}

// Named errors (repo rule): the non-nil err on every non-terminal
// outcome is the C2 contract (err == nil means CONNECTED/DELIVERED).
var (
	errVoiceAttemptUnwired = errors.New("voiceattempt: dial seam not wired")
	errOutcomeAdvanced     = errors.New("voiceattempt: outcome advanced the chain")
	// errOutcomeWaitTimeout names the call-cap leg: the outcome wait hit
	// its deadline while the call had no terminal record yet. The call
	// itself may still be live — the report must not claim it went
	// unanswered (A5-1).
	errOutcomeWaitTimeout = errors.New("voiceattempt: outcome wait hit its deadline (call cap); terminal outcome unknown, call may still be live")
)

// OutcomeRegistry is the wiring-side terminal-outcome collector: the
// daemon adapts Manager.SetReleaseHook onto it so a voice Attempt can
// wait on a bounded map instead of holding a goroutine per attempt.
// The mutex guards the map only; the WAIT is the caller's ctx with a
// poll tick - no I/O under lock.
type OutcomeRegistry struct {
	mu       sync.Mutex
	outcomes map[string]Outcome
	order    []string
	cap      int
}

// NewOutcomeRegistry constructs the collector.
func NewOutcomeRegistry() *OutcomeRegistry {
	return &OutcomeRegistry{
		outcomes: make(map[string]Outcome),
		cap:      1024,
	}
}

// Record is the release-hook adapter target (call once per terminal
// outcome, from the wiring's hook closure).
func (r *OutcomeRegistry) Record(ccid string, out Outcome) {
	if r == nil || ccid == "" {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	if len(r.order) >= r.cap {
		oldest := r.order[0]
		r.order = r.order[1:]
		delete(r.outcomes, oldest)
	}

	if _, exists := r.outcomes[ccid]; !exists {
		r.order = append(r.order, ccid)
	}
	r.outcomes[ccid] = out
}

// Lookup waits (bounded by ctx) for one terminal outcome. False on ctx
// expiry — the attempt classifies that as the distinct 'timeout' outcome
// (A5-1) and advances.
func (r *OutcomeRegistry) Lookup(ctx context.Context, ccid string) (Outcome, bool) {
	if r == nil || ccid == "" {
		return "", false
	}
	for {
		r.mu.Lock()
		out, ok := r.outcomes[ccid]
		r.mu.Unlock()
		if ok {
			return out, true
		}
		select {
		case <-ctx.Done():
			return "", false
		case <-time.After(100 * time.Millisecond):
		}
	}
}
