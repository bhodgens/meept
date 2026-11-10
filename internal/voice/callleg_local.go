package voice

// Local-leg CallLeg adapter (tree 20261110-voice-callleg, leaf 01).
//
// Pure facade over LocalLeg: Place rides Open (the local leg has no
// effects ledger — the deliberate CL5 posture, not an adapter gap),
// Hangup rides End (already idempotent nil on a reaped local ccid),
// Accept is the named ErrNoInbound (the leg has no inbound concept).
// Events rides the leg's Manager terminal-observer lane — the SAME
// additive seam the phone adapter uses (releaseOnce stays the single
// terminal funnel; the release hook is untouched). Leaf 02 adds the
// Connected kind from the leg's client-join edge (LocalLeg
// AddClientJoinObserver, fired by the splice's first mic byte).

import (
	"context"
	"fmt"
	"sync"
)

// localAdapter implements CallLeg for the local leg.
type localAdapter struct {
	l *LocalLeg
	b *callEventBus

	// mu guards placed: ccids opened through THIS adapter, so the
	// shared Manager's terminal observer only forwards this adapter's
	// calls (the manager fans out every leg's teardown).
	mu     sync.Mutex
	placed map[string]bool
}

func newLocalAdapter(l *LocalLeg) *localAdapter {
	a := &localAdapter{l: l, b: newCallEventBus()}
	if l != nil && l.deps.Manager != nil {
		l.deps.Manager.AddTerminalObserver(a.onTerminal)
	}
	if l != nil {
		// Leaf 02: Connected from the client-join edge (the splice's
		// first mic byte) — the leg-local last-mile seam. The child's
		// setup answer rides daemon-side routeFrame (off-limits here).
		// Ringing stays unmapped PER LEG: Open returning IS the dial
		// edge and the frozen parity gate reads the terminal from this
		// same bus — a Place-time Ringing would pre-empt it.
		l.AddClientJoinObserver(a.onClientJoin)
	}
	return a
}

// onClientJoin maps the join edge to Connected for ccids THIS adapter
// placed. The bus drops it if the terminal already latched.
func (a *localAdapter) onClientJoin(ccid string) {
	a.mu.Lock()
	placed := a.placed[ccid]
	a.mu.Unlock()
	if !placed {
		return
	}
	a.b.emit(ccid, CallEvent{Kind: CallEventConnected, Reason: "client joined"})
}

// onTerminal translates the shared release fan-out into a CallEvent,
// filtered to this adapter's placed calls.
func (a *localAdapter) onTerminal(ccid string, outcome Outcome, reason string) {
	if !a.forget(ccid) {
		return
	}
	a.b.emit(ccid, mapOutcomeEvent(reason, outcome))
}

// forget reports whether ccid belongs to this adapter and drops it from
// the placed set (a terminal call is no longer observable); bounded by
// the same 512-entry rebuild the phone adapter applies.
func (a *localAdapter) forget(ccid string) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placed == nil {
		return false
	}
	if !a.placed[ccid] {
		return false
	}
	delete(a.placed, ccid)
	return true
}

// remember adds ccid to the placed set with a live-set rebuild when the
// map outgrows its bound (dead ccids pruned; the leg's Live is the
// authority).
func (a *localAdapter) remember(ccid string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.placed == nil {
		a.placed = make(map[string]bool)
	}
	if len(a.placed) > 512 {
		live := make(map[string]bool)
		for _, c := range a.l.Live() {
			live[c.CallID] = true
		}
		for k := range a.placed {
			if !live[k] {
				delete(a.placed, k)
			}
		}
	}
	a.placed[ccid] = true
}

// Place opens a local call. req.Target may be a bare label or the
// canonical "local:<label>" (Open re-canonicalizes either way). Fresh-
// session/brief/day-brief knobs ride the leg's own deps today (leaf 02
// threads PlaceRequest through).
func (a *localAdapter) Place(ctx context.Context, req PlaceRequest) (CallHandle, error) {
	call, err := a.l.Open(ctx, req.EmployeeID, localLabelFromTarget(req.Target))
	if err != nil {
		return CallHandle{}, fmt.Errorf("voice: local adapter: place: %w", err)
	}
	a.remember(call.CallID)
	return CallHandle{
		CCID:       call.CallID,
		Leg:        LegLocal,
		EmployeeID: req.EmployeeID,
		Target:     a.canonicalTarget(call.CallID, req.Target),
	}, nil
}

// canonicalTarget reads the registry's stored target ("local:<label>")
// for a live ccid; a released call falls back to re-canonicalizing the
// request target.
func (a *localAdapter) canonicalTarget(ccid, raw string) string {
	if reg := a.l.deps.Registry; reg != nil {
		for _, inst := range reg.Live() {
			if inst.CallControlID == ccid {
				return inst.Target
			}
		}
	}
	return "local:" + localLabelFromTarget(raw)
}

// Accept: the local leg has NO inbound concept. Named refusal per
// contract, never a silent success.
func (a *localAdapter) Accept(_ context.Context, _ string) (CallHandle, error) {
	return CallHandle{}, ErrNoInbound
}

// Hangup rides LocalLeg.End — already idempotent: re-End of a reaped
// local-shaped ccid is nil, and the manager's FailOutcome fan-out (via
// End) fires the release exactly once.
func (a *localAdapter) Hangup(_ context.Context, h CallHandle, reason string) error {
	if err := a.l.End(h.CCID, reason); err != nil {
		return fmt.Errorf("voice: local adapter: hangup: %w", err)
	}
	return nil
}

// Events returns the adapter's terminal-event stream.
func (a *localAdapter) Events() <-chan CallEvent { return a.b.events() }
