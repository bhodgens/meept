package voice

// Signal-leg CallLeg adapter (tree 20261110-voice-callleg, leaf 01).
//
// The import-cycle rule binds here: internal/signalleg imports
// internal/voice, so this file may NEVER name *signalleg.Leg. The
// adapter consumes the voice-defined SignalLegSeam interface (the gate
// pins the shape); *signalleg.Leg satisfies it STRUCTURALLY. Terminals
// ride the optional SignalTerminalObserver extension (one additive
// method on the leg) — the leg's own FailOutcome fan-out stays the
// single terminal funnel.

import (
	"context"
	"fmt"
	"sync"
)

// signalAdapter implements CallLeg over the signal-leg seam.
type signalAdapter struct {
	seam SignalLegSeam
	b    *callEventBus

	// mu guards placed: ccids placed through THIS adapter (the leg's
	// terminal observer also fires for legs the adapter never saw,
	// e.g. inbound Accepts).
	mu     sync.Mutex
	placed map[string]bool
}

func newSignalAdapter(seam SignalLegSeam) *signalAdapter {
	a := &signalAdapter{seam: seam, b: newCallEventBus()}
	if obs, ok := seam.(SignalTerminalObserver); ok {
		obs.AddTerminalObserver(a.onTerminal)
	}
	return a
}

// onTerminal receives the leg's once-latched terminal (carrier ENDED /
// ringing watchdog / outbound abandon) with the leg's outcome + reason.
func (a *signalAdapter) onTerminal(ccid string, outcome Outcome, reason string) {
	if !a.forget(ccid) {
		return
	}
	a.b.emit(ccid, mapOutcomeEvent(reason, outcome))
}

func (a *signalAdapter) forget(ccid string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placed == nil || !a.placed[ccid] {
		return false
	}
	delete(a.placed, ccid)
	return true
}

func (a *signalAdapter) remember(ccid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placed == nil {
		a.placed = make(map[string]bool)
	}
	if len(a.placed) > 512 {
		// Bound the placed set: prune ids the leg no longer holds
		// live; terminals prune eagerly, so this only back-stops calls
		// abandoned before any observer fired.
		live := make(map[string]bool)
		for _, c := range a.seam.Live() {
			live[c] = true
		}
		for k := range a.placed {
			if !live[k] {
				delete(a.placed, k)
			}
		}
	}
	a.placed[ccid] = true
}

// Place rings the signal leg through the seam, carrying RequestID as
// the seam's idempotency identity (C-V2: the leg keys its effect
// reservation over (employee, canonical target, requestID) — the
// adapter passes one Place call per request and never re-keys).
func (a *signalAdapter) Place(ctx context.Context, req PlaceRequest) (CallHandle, error) {
	uuid := signalUUIDFromTarget(req.Target)
	ccid, err := a.seam.Place(ctx, req.EmployeeID, uuid, req.FirstLine, req.RequestID)
	if err != nil {
		return CallHandle{}, fmt.Errorf("voice: signal adapter: place: %w", err)
	}
	a.remember(ccid)
	return CallHandle{
		CCID:       ccid,
		Leg:        LegSignal,
		EmployeeID: req.EmployeeID,
		Target:     "signal:" + uuid,
	}, nil
}

// Accept on the signal leg: the leg's own Accept needs a full
// signal.CallEvent + the Deps employee fallback, which the string-id
// CallLeg.Accept cannot supply in leaf 01. Named refusal (leaf 02
// extends the seam); NOT ErrNoInbound — this leg DOES have inbound.
func (a *signalAdapter) Accept(_ context.Context, _ string) (CallHandle, error) {
	return CallHandle{}, errInboundAdoptionUnwired
}

// Hangup rides seam.End — already idempotent: re-End of a reaped
// signal-shaped ccid is nil, a foreign id is the named refusal (kept as
// an error, not swallowed). Adapter-initiated teardown emits Ended
// itself; the leg's carrier terminals are deduped by the bus latch.
func (a *signalAdapter) Hangup(_ context.Context, h CallHandle, reason string) error {
	if err := a.seam.End(h.CCID); err != nil {
		return fmt.Errorf("voice: signal adapter: hangup: %w", err)
	}
	a.forget(h.CCID)
	a.b.emit(h.CCID, CallEvent{Kind: CallEventEnded, Reason: reason})
	return nil
}

// Events returns the adapter's terminal-event stream.
func (a *signalAdapter) Events() <-chan CallEvent { return a.b.events() }
