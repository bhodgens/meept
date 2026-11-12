package voice

// Operator-visible call telemetry (voice-events): a small fixed-size
// event ring per call_id, appended by the talk loop through the optional
// TalkDeps.OnEvent hook and by the daemon frame router ("heard"), and read
// back by clients through the voice.local.events RPC (the daemon adapts
// this package's types onto the seam-based rpc.VoiceLocalSeams - no new
// bus topic: the ring is in-process control-plane state, polled over the
// existing RPC socket, never a publish/subscribe surface).
//
// NOTE: names here are "Telemetry*"/"Event*" prefixed on purpose - the
// CallLeg adapter already owns CallEvent/callEventBus in this package
// (tree 20261110-voice-callleg) and the two lanes must stay distinct.
//
// Shape contract:
//   - the ring is best-effort observability: a full ring DROPS the oldest
//     event (never blocks the emitter), an unknown call_id reads as not
//     found, and a nil *TelemetryRing is a safe no-op receiver everywhere.
//   - Seq is per-call, 1-based, monotonic (the RPC cursor domain).
//   - rings live for the last DefaultCallEventRings call ids (FIFO
//     eviction) so a long-lived daemon cannot grow the map across
//     thousands of calls.

import (
	"strings"
	"sync"
	"time"
)

// Event kinds on the ring. The strings are the wire values on the
// voice.local.events RPC result (the CLI renderer switches on them);
// renaming one is a CLI-visible change.
const (
	// EventKindHeard: a FINAL prompt frame reached the daemon frame router
	// (mic speech evidence - precedes the talk loop's dispatch of the same
	// text). Recorded by VoiceWiring.routeFrame, not by the loop.
	EventKindHeard = "heard"
	// EventKindDispatch: the talk loop dispatched a turn for a final
	// prompt (the "voice talk loop: turn dispatch" log site).
	EventKindDispatch = "prompt_dispatch"
	// EventKindFinal: the draft's turn-final telemetry (the "voice talk
	// loop: turn final" log site) - reply words + wall ms.
	EventKindFinal = "turn_final"
	// EventKindBargeIn: the call barged in on its own playback (the child
	// interrupt frame or the media-side cancel - both log sites).
	EventKindBargeIn = "barge_in"
	// EventKindCallEnded: the talk loop ended the call (Reason carries the
	// loop's named reason, or the manager outcome when no loop was armed).
	EventKindCallEnded = "call_ended"
	// EventKindHeld / EventKindReleased / EventKindDropped: the link
	// half-duplex park lifecycle (link calls only - the events the loop
	// already logs as prompt_held / prompt_released / prompt_dropped).
	EventKindHeld     = "prompt_held"
	EventKindReleased = "prompt_released"
	EventKindDropped  = "prompt_dropped"
	// EventKindSilenceDeferred: a silence-window fire DEFERRED because
	// the agent's own audio was still active inside the window (self-talk
	// fix, call local-53b347f028f0a43f). Reason carries the consecutive
	// deferral count. Local legs with the AudioActive seam only.
	EventKindSilenceDeferred = "silence_deferred"
)

// Ring sizing: DefaultCallEvents is the per-call ring capacity the daemon
// constructs with; DefaultCallEventRings bounds how many (most recent)
// call ids retain rings at once - an older call id's ring is evicted when
// a new one appends past the cap. 64 x ~120 B is a few KB per call.
const (
	DefaultCallEvents     = 64
	DefaultCallEventRings = 32
)

// TelemetryEvent is one ring entry. Text is always a PREVIEW (truncated
// at the emitter by PreviewEventText), never full transcript material.
type TelemetryEvent struct {
	Seq    uint64    `json:"seq"`
	Kind   string    `json:"kind"`
	At     time.Time `json:"at"`
	Text   string    `json:"text,omitempty"`
	Words  int       `json:"words,omitempty"`
	Ms     int64     `json:"ms,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

// EventInput is the caller-supplied payload for an Append (the ring owns
// Seq and At).
type EventInput struct {
	Kind   string
	Text   string
	Words  int
	Ms     int64
	Reason string
}

// telemetryRing is the fixed circular buffer for ONE call id.
type telemetryRing struct {
	events []TelemetryEvent // len == perCall capacity, allocated once
	head   int              // next write slot
	count  int              // retained events (<= cap)
	total  uint64           // events appended ever (== last assigned seq)
}

// TelemetryRing is the keyed set of per-call rings. All methods are
// nil-safe (a nil ring silently drops appends and reports no events).
type TelemetryRing struct {
	mu       sync.Mutex
	perCall  int
	maxRings int
	calls    map[string]*telemetryRing
	order    []string // FIFO of call ids by first append (eviction order)
	// everSeen records every ccid that EVER created a ring (survives
	// FIFO eviction). HasRing reads it: the release fan-out's "never
	// recorded anything" probe must not resurrect an EVICTED call's
	// ring, and a live-map miss alone cannot tell "evicted" from
	// "never existed" (C-20). Bounded like the rings themselves: a
	// ccid re-entering after eviction re-arms its entry (map write,
	// no growth) and long-lived daemons carry one small map entry per
	// distinct call id over the process lifetime - the same class of
	// bound the call store already accepts.
	everSeen map[string]struct{}
}

// NewTelemetryRing constructs the ring set. perCall <= 0 falls back to
// DefaultCallEvents; maxRings <= 0 to DefaultCallEventRings.
func NewTelemetryRing(perCall, maxRings int) *TelemetryRing {
	if perCall <= 0 {
		perCall = DefaultCallEvents
	}
	if maxRings <= 0 {
		maxRings = DefaultCallEventRings
	}
	return &TelemetryRing{
		perCall:  perCall,
		maxRings: maxRings,
		calls:    make(map[string]*telemetryRing),
		everSeen: make(map[string]struct{}),
	}
}

// Append records one event for ccid and returns its assigned seq (0 for a
// nil ring or empty ccid - callers never branch on the return). The ring
// is pure CPU bookkeeping: no I/O under the mutex (mutexio-safe).
func (r *TelemetryRing) Append(ccid string, in EventInput) uint64 {
	if r == nil || ccid == "" {
		return 0
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cr := r.calls[ccid]
	if cr == nil {
		cr = &telemetryRing{events: make([]TelemetryEvent, r.perCall)}
		r.calls[ccid] = cr
		if r.everSeen == nil {
			r.everSeen = make(map[string]struct{})
		}
		r.everSeen[ccid] = struct{}{}
		r.order = append(r.order, ccid)
		for len(r.order) > r.maxRings {
			oldest := r.order[0]
			r.order = r.order[1:]
			delete(r.calls, oldest)
		}
	}
	cr.total++
	cr.events[cr.head] = TelemetryEvent{
		Seq:    cr.total,
		Kind:   in.Kind,
		At:     time.Now(),
		Text:   in.Text,
		Words:  in.Words,
		Ms:     in.Ms,
		Reason: in.Reason,
	}
	cr.head = (cr.head + 1) % r.perCall
	if cr.count < r.perCall {
		cr.count++
	}
	return cr.total
}

// Since returns this call's events with Seq > cursor in seq order, capped
// at limit (limit <= 0 means the ring capacity). truncated reports ONLY
// genuine eviction: events after cursor were already overwritten in the
// ring (a slow poller lost rows). A limit-capped backlog read is NOT
// truncated - hasMore reports that matching events exist beyond the cap
// (the client just reads again with the returned cursor). found reports
// whether the call id has a LIVE ring; HasRing is the broader
// ever-recorded probe the release fan-out uses.
func (r *TelemetryRing) Since(ccid string, cursor uint64, limit int) (events []TelemetryEvent, truncated bool, hasMore bool, found bool) {
	if r == nil || ccid == "" {
		return nil, false, false, false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	cr := r.calls[ccid]
	if cr == nil {
		return nil, false, false, false
	}
	if limit <= 0 || limit > r.perCall {
		limit = r.perCall
	}
	// Retained window: seqs (total-count+1 .. total). A cursor below the
	// window's start means un-consumed events were already overwritten -
	// the ONLY truncated condition (the ring is append-ordered, so any
	// cursor above droppedEnd sees every retained match in full).
	droppedEnd := cr.total - uint64(cr.count)
	if cursor < droppedEnd {
		truncated = true
	}
	start := (cr.head - cr.count + r.perCall) % r.perCall
	out := make([]TelemetryEvent, 0, cr.count)
	for i := 0; i < cr.count; i++ {
		ev := cr.events[(start+i)%r.perCall]
		if ev.Seq <= cursor {
			continue
		}
		if len(out) >= limit {
			hasMore = true
			break
		}
		out = append(out, ev)
	}
	return out, truncated, hasMore, true
}

// HasRing reports whether ccid EVER recorded telemetry: a live ring (even
// an empty one) or an evicted one. It is the release fan-out's "never
// recorded anything" probe (C-20): a live-map miss alone cannot tell
// "FIFO-evicted" from "never existed" - both read false on calls - so the
// ever-created set is the authority. An evicted call's release must NOT
// resurrect a ring with a synthetic call_ended.
func (r *TelemetryRing) HasRing(ccid string) bool {
	if r == nil || ccid == "" {
		return false
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	_, seen := r.everSeen[ccid]
	return seen
}

// PreviewEventText truncates free text for a telemetry line (the same
// 40-rune rule the talk loop applies to prompt dispatches; exported so
// the daemon frame router records "heard" previews identically).
func PreviewEventText(text string) string {
	trimmed := strings.TrimSpace(text)
	if runes := []rune(trimmed); len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return trimmed
}
