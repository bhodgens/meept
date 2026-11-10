package voice

// Phone-leg CallLeg adapter (tree 20261110-voice-callleg, leaf 01).
//
// Pure facade: Place rides Dialer.Dial (C9 RequestID idempotency stays
// inside the dialer's ledger — the adapter never bypasses or re-keys
// it), Hangup mirrors the wiring's endCall posture (carrier hangup,
// then Manager.FailOutcome, whose releaseOnce keeps the fan-out
// exactly-once so double Hangup is the idempotent nil). Events rides
// the Manager's additive terminal-observer lane — the single-occupancy
// release hook is NOT stolen (existing wiring owns it; the observer is
// notified after the hook, outside the mutex). Leaf 02 adds the
// Connected kind on the additive answered-observer lane (call.answered
// on a placed ccid); the single-occupancy answerHook stays the dialer's
// timer-cancel seam. Ringing stays unmapped PER LEG: Telnyx gives the
// webhook no outbound-ring event (only call.initiated/answered/hangup
// reach HandleCallEvent) and the leaf-02 rule forbids inventing timers.

import (
	"context"
	"fmt"
	"sync"
)

// phoneAdapter implements CallLeg for the Telnyx phone leg.
type phoneAdapter struct {
	d *Dialer
	m *Manager
	b *callEventBus

	// mu guards placed: the ccids dialed through THIS adapter, so the
	// shared Manager's terminal observer (which sees every leg's and
	// every inbound call's teardown) only forwards this adapter's
	// calls onto its Events stream.
	mu     sync.Mutex
	placed map[string]bool
}

func newPhoneAdapter(d *Dialer, m *Manager) *phoneAdapter {
	a := &phoneAdapter{d: d, m: m, b: newCallEventBus()}
	if m != nil {
		m.AddTerminalObserver(a.onTerminal)
		// Leaf 02: the ADDITIVE answered lane (call.answered) maps to
		// Connected. The single-occupancy answerHook (H1 timer cancel)
		// is untouched.
		m.AddAnsweredObserver(a.onAnswered)
	}
	return a
}

// onAnswered is the Manager answered-observer lane: a call.answered on
// a ccid THIS adapter placed maps to CallEventConnected. The bus
// drops it if the terminal already latched (answer vs hangup race —
// the terminal wins).
func (a *phoneAdapter) onAnswered(ccid string) {
	if !a.has(ccid) {
		return
	}
	a.b.emit(ccid, CallEvent{Kind: CallEventConnected, Reason: "answered"})
}

// has reports whether ccid belongs to this adapter WITHOUT dropping it
// (non-terminal edges peek; only terminals forget).
func (a *phoneAdapter) has(ccid string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.placed[ccid]
}

// onTerminal is the Manager terminal-observer lane: translate the
// release fan-out's (ccid, outcome, reason) into a CallEvent. Only
// ccids this adapter placed are tracked (the manager is shared across
// legs and inbound calls); the bus latches exactly-once per ccid.
func (a *phoneAdapter) onTerminal(ccid string, outcome Outcome, reason string) {
	if !a.forget(ccid) {
		return
	}
	a.b.emit(ccid, mapOutcomeEvent(reason, outcome))
}

// forget reports whether ccid belongs to this adapter and drops it from
// the placed set — a terminal call is no longer observable, keeping the
// map bounded to live calls.
func (a *phoneAdapter) forget(ccid string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placed == nil || !a.placed[ccid] {
		return false
	}
	delete(a.placed, ccid)
	return true
}

// remember adds ccid to the placed set (bounded: forget prunes on every
// observed terminal; the upstream leg latches make duplicate terminals
// impossible, so the map tracks live calls of this adapter).
func (a *phoneAdapter) remember(ccid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placed == nil {
		a.placed = make(map[string]bool)
	}
	a.placed[ccid] = true
}

// Place rings the phone leg through the dialer and returns the leg's
// canonical identity (the normalized target the registry holds).
func (a *phoneAdapter) Place(ctx context.Context, req PlaceRequest) (CallHandle, error) {
	res, err := a.d.Dial(ctx, DialRequest{
		EmployeeID: req.EmployeeID,
		Target:     req.Target,
		RequestID:  req.RequestID,
	})
	if err != nil {
		return CallHandle{}, fmt.Errorf("voice: phone adapter: place: %w", err)
	}
	a.remember(res.CallControlID)
	return CallHandle{
		CCID:       res.CallControlID,
		Leg:        LegPhone,
		EmployeeID: req.EmployeeID,
		Target:     a.canonicalTarget(res.CallControlID, req.Target),
	}, nil
}

// canonicalTarget reads the registry's normalized target for a live
// ccid (the dialer Acquires with NormalizeE164(target)); a call already
// released falls back to the same normalization the dialer applied.
func (a *phoneAdapter) canonicalTarget(ccid, raw string) string {
	if a.d != nil {
		for _, inst := range a.d.reg.Live() {
			if inst.CallControlID == ccid {
				return inst.Target
			}
		}
	}
	return NormalizeE164(raw)
}

// Accept on the phone leg: inbound adoption rides the webhook's spawn
// seam (Manager.spawn), which the string-id CallLeg.Accept cannot
// reach in leaf 01. Named refusal, never a silent success (leaf 02
// extends the seam).
func (a *phoneAdapter) Accept(_ context.Context, _ string) (CallHandle, error) {
	return CallHandle{}, errInboundAdoptionUnwired
}

// Hangup mirrors the wiring's endCall: best-effort carrier hangup, then
// the once-per-outcome release fan-out through the manager. Unknown or
// already-reaped ccids are the idempotent nil (releaseOnce decides), so
// a double Hangup releases exactly once.
func (a *phoneAdapter) Hangup(ctx context.Context, h CallHandle, reason string) error {
	if a.d != nil && a.d.tel != nil {
		if err := a.d.tel.HangupCall(ctx, h.CCID); err != nil {
			// The carrier may already be gone (late webhook beat us);
			// the manager teardown below is still the authority.
			a.d.logger.Debug("voice: phone adapter: carrier hangup", "ccid", h.CCID, "err", err)
		}
	}
	if a.m != nil {
		a.m.FailOutcome(h.CCID, OutcomeHangup, reason)
	}
	return nil
}

// Events returns the adapter's terminal-event stream.
func (a *phoneAdapter) Events() <-chan CallEvent { return a.b.events() }
