package voice

// CallLeg contract (tree 20261110-voice-callleg, leaf 01; master C-B1).
//
// One carrier-neutral interface over the three voice legs (phone/Telnyx,
// local, Signal). The adapters are OBSERVABILITY + facade only: every
// method is a call-through onto the leg's existing public API, and the
// releaseOnce fan-out in Manager remains the single terminal funnel
// (unchanged, frozen 20261109 — the adapters ADD an observer, never a
// second teardown path). No caller migrates in leaf 01 (leaf 03 moves
// the wiring).
//
// The signal adapter is keyed by an interface, not a type: internal/
// signalleg imports internal/voice, so voice can NEVER import signalleg
// (import cycle). SignalLegSeam below is the structural shape
// *signalleg.Leg satisfies; nothing in voice names signalleg.
//
// Events scope (leaf 01): adapters emit ONLY terminal kinds (Ended /
// Errored). Ringing/Connected/Reconnecting are defined by the contract
// but not yet emitted — the legs' answer/connect signals have no
// observer seam at this layer (leaf 02 maps them; the gate's parity
// reads expect the terminal event first).

import (
	"context"
	"errors"
	"sync"

	"github.com/caimlas/meept/internal/signal"
)

// ErrNoInbound is the named refusal for Accept on a leg with no inbound
// concept (the local leg). It is part of the C-B1 contract.
var ErrNoInbound = errors.New("voice: call leg has no inbound concept")

// errInboundAdoptionUnwired is the leaf-01 Accept refusal for legs whose
// inbound adoption rides a seam the string-id CallLeg.Accept cannot
// reach (phone: the webhook's spawn seam; signal: the leg's Accept needs
// a signal.CallEvent + the Deps employee fallback). Leaf 02 extends the
// seam shape; this keeps the refusal NAMED, never a silent success.
var errInboundAdoptionUnwired = errors.New("voice: adapter: inbound adoption is not wired behind CallLeg.Accept until leaf 02")

// CallEventKind is the shared, carrier-neutral event enum. A leg's raw
// carrier state is translated INTO one of these kinds (never leaked as a
// carrier-specific value); free text rides Reason.
type CallEventKind string

// The five pinned kinds (gate: pairwise distinct).
const (
	CallEventRinging      CallEventKind = "ringing"
	CallEventConnected    CallEventKind = "connected"
	CallEventReconnecting CallEventKind = "reconnecting"
	CallEventEnded        CallEventKind = "ended"
	CallEventErrored      CallEventKind = "errored"
)

// CallEvent is one entry on a CallLeg's Events stream.
type CallEvent struct {
	Kind   CallEventKind
	Reason string
}

// PlaceRequest carries the C-B1 place fields. FreshSession and DayBrief
// are accepted at this boundary and remain UNUSED until leaf 02 threads
// them through (the legs resolve both from their own deps today).
type PlaceRequest struct {
	EmployeeID   string
	Target       string
	Leg          string
	RequestID    string
	FreshSession bool
	DayBrief     DayBrief
	// FirstLine is the greeting line the contact chain carried (the
	// signal leg's Place parameter; the phone/local legs ignore it —
	// their greeting rides the existing talk-loop seams).
	FirstLine string
}

// CallHandle is the canonical identity of one placed call, populated
// from the leg's own view (normalized target on the phone leg,
// "local:<label>"/"signal:<uuid>" on the others).
type CallHandle struct {
	CCID       string
	Leg        string
	EmployeeID string
	Target     string
}

// CallLeg is the one contract every voice leg exposes.
type CallLeg interface {
	// Place reserves + rings a new outbound call. RequestID idempotency
	// rides the leg's own ledger untouched (adapters must never bypass
	// or re-key it).
	Place(ctx context.Context, req PlaceRequest) (CallHandle, error)
	// Accept adopts an inbound call by the leg's inbound identity. Legs
	// with no inbound concept fail with ErrNoInbound (named, never a
	// silent success).
	Accept(ctx context.Context, id string) (CallHandle, error)
	// Hangup tears the call down idempotently: a second Hangup of an
	// already-reaped handle returns nil and fires no second release.
	Hangup(ctx context.Context, h CallHandle, reason string) error
	// Events returns the adapter's CallEvent stream (shared across
	// Places on one adapter). Ownership/buffering is the adapter's:
	// deliveries are best-effort and NEVER block the call lifecycle.
	Events() <-chan CallEvent
}

// ---- shared adapter event plumbing ----

// callEventBusCap bounds one adapter's undelivered events. The send is
// non-blocking by design: a full bus DROPS the event, it NEVER blocks
// the lifecycle path that observed the terminal (the releaseOnce funnel
// stays independent of observers — AGENTS.md release fan-out).
const callEventBusCap = 16

// callEventBus is the per-adapter CallEvent fan-out with exactly-once
// terminal semantics per ccid. The terminal latch mirrors the legs' own
// claimTerminal posture at the OBSERVER layer: whichever terminal signal
// is observed first (carrier webhook, manager terminal observer, adapter
// Hangup) wins; later duplicates are ignored.
type callEventBus struct {
	mu      sync.Mutex
	ch      chan CallEvent
	latched map[string]bool
}

func newCallEventBus() *callEventBus {
	return &callEventBus{
		ch:      make(chan CallEvent, callEventBusCap),
		latched: make(map[string]bool),
	}
}

// events exposes the bus's read side.
func (b *callEventBus) events() <-chan CallEvent { return b.ch }

// emit delivers ev for ccid, best-effort. Terminal kinds
// (Ended/Errored) latch exactly-once per ccid; a terminal event always
// carries a non-empty Reason (empty reason falls back to the kind) so
// observers never see a blank close.
func (b *callEventBus) emit(ccid string, ev CallEvent) {
	if ev.Kind != CallEventEnded && ev.Kind != CallEventErrored {
		select {
		case b.ch <- ev:
		default:
		}
		return
	}
	if ev.Reason == "" {
		ev.Reason = string(ev.Kind)
	}
	b.mu.Lock()
	if b.latched[ccid] {
		b.mu.Unlock()
		return
	}
	if len(b.latched) > 512 {
		// Bounded convenience, never a leak: the upstream legs latch
		// their own terminals, so this map only holds one entry per
		// terminal seen by this adapter; a cap-clear races at most a
		// duplicate-terminal window long after teardown.
		b.latched = make(map[string]bool)
	}
	b.latched[ccid] = true
	b.mu.Unlock()
	select {
	case b.ch <- ev:
	default:
	}
}

// mapOutcomeEvent translates a Manager terminal Outcome + reason into a
// CallEvent: OutcomeFailed is Errored, every other terminal outcome
// (hangup / no_answer / busy) is Ended — the shared enum parity the gate
// pins across all three legs. An empty reason falls back to the outcome
// name so terminals always carry a non-empty Reason.
func mapOutcomeEvent(reason string, outcome Outcome) CallEvent {
	if reason == "" {
		reason = string(outcome)
	}
	kind := CallEventEnded
	if outcome == OutcomeFailed {
		kind = CallEventErrored
	}
	return CallEvent{Kind: kind, Reason: reason}
}

// ---- signal seam (import-cycle guard) ----

// SignalLegSeam is the voice-side structural seam the signal adapter
// consumes: the public *signalleg.Leg surface the adapter needs (the
// gate pins this shape — Place / Accept / End / Live). *signalleg.Leg
// satisfies it with no changes to the leg's core surface; the optional
// SignalTerminalObserver extension below is the only addition.
type SignalLegSeam interface {
	Place(ctx context.Context, employeeID, targetUUID, firstLine string, requestIDs ...string) (string, error)
	Accept(ctx context.Context, callEvent signal.CallEvent, employeeID string) (string, error)
	End(ccid string) error
	Live() []string
}

// SignalTerminalObserver is an OPTIONAL extension the signal adapter
// probes for (satisfied by *signalleg.Leg): reports the leg's own
// terminal transitions (carrier ENDED / ringing watchdog / outbound
// abandon) with outcome and carrier reason, which the string-id seam
// cannot surface. Adapters degrade to adapter-initiated observation
// when a seam lacks it.
type SignalTerminalObserver interface {
	AddTerminalObserver(fn func(ccid string, outcome Outcome, reason string))
}

// NewSignalAdapter wraps a signalleg-shaped seam in the CallLeg
// contract. The argument is typed as the seam interface, NOT
// *signalleg.Leg, because voice must never import signalleg (the leg
// imports voice — cycle).
func NewSignalAdapter(seam SignalLegSeam) CallLeg {
	return newSignalAdapter(seam)
}

// NewPhoneAdapter wraps the dial path (Dialer) + teardown path (Manager)
// in the CallLeg contract for the phone leg.
func NewPhoneAdapter(d *Dialer, m *Manager) CallLeg {
	return newPhoneAdapter(d, m)
}

// NewLocalAdapter wraps LocalLeg in the CallLeg contract.
func NewLocalAdapter(l *LocalLeg) CallLeg {
	return newLocalAdapter(l)
}

// localLabelFromTarget strips a canonical "local:" prefix so the label
// can be passed to LocalLeg.Open, which re-adds it. Idempotent on bare
// labels.
func localLabelFromTarget(target string) string {
	const prefix = "local:"
	if len(target) > len(prefix) && target[:len(prefix)] == prefix {
		return target[len(prefix):]
	}
	return target
}

// signalUUIDFromTarget strips a canonical "signal:" prefix for
// SignalLegSeam.Place, which re-adds it. Idempotent on bare uuids.
func signalUUIDFromTarget(target string) string {
	const prefix = "signal:"
	if len(target) > len(prefix) && target[:len(prefix)] == prefix {
		return target[len(prefix):]
	}
	return target
}

// compile-time proof the three adapters satisfy the contract (the gate
// asserts the same through the unexported types).
var (
	_ CallLeg = (*phoneAdapter)(nil)
	_ CallLeg = (*localAdapter)(nil)
	_ CallLeg = (*signalAdapter)(nil)
)
