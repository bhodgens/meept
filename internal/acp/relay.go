// relay.go is the human permission relay: the pending-request registry and
// notifier seam for permission_mode "human" and the decision-lane
// escalation (acp-human-permission-relay leaf 02, master.md Contract B).
//
// The lane is FAIL-CLOSED like its parent decision lane: an unanswered or
// unroutable session/requestPermission is cancelled after
// [acp] relay_timeout (default 120 s). A notifier returning false (no
// listener right now) does NOT cancel early — the wait continues so a
// reconnecting surface can still answer (reconnect contract). When the
// relay or notifier seam is absent, requests cancel immediately with a
// WARN (pin: TestSession_HumanNilRelayFailsClosed).

package acp

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// Sentinel errors returned by Relay. Callers match with errors.Is
// (sentinel style per manager.go).
var (
	ErrRelayNotFound = errors.New("acp: relay request not found")
	ErrRelayAnswered = errors.New("acp: relay request already answered")
	ErrRelayFull     = errors.New("acp: relay pending queue full")
)

// maxPendingPermissions bounds the registry: when this many requests are
// outstanding, a NEW request is rejected with a WARN and cancelled
// immediately (fail-closed; the already-pending requests stay answerable).
// Drop-oldest was explicitly rejected in the plan (drift 8).
const maxPendingPermissions = 32

// defaultRelayTimeout is the safety net when SessionConfig.RelayTimeout is
// unset (the config layer enforces relay_timeout > 0 for human /
// escalate modes; leaf 01). Mirrors the [acp] relay_timeout default.
const defaultRelayTimeout = 120 * time.Second

// Relay-lane decision verbs (v1, frozen): "allow" maps to
// selected/allow-once on the wire, anything else (including "deny") maps to
// cancelled. No "always" option in v1 (optionId passthrough deferred).
const (
	relayDecisionAllow = "allow"
	relayDecisionDeny  = "deny"
)

// Relay-lane permission_resolved event texts and notifier reasons (frozen,
// master.md Contract B + drift 7): Text carries the decision on answered
// requests, "timeout" when the fail-closed timer fired.
const (
	relayTextTimeout   = "timeout"
	relayReasonAnswer  = "answered"
	relayReasonTimeout = "timeout"
	relayReasonNoWire  = "no_relay"
	relayReasonFull    = "full"
)

// Decision-lane escalation event texts (drift 6): when the model's
// non-allow outcome is overruled / confirmed by the human relay, the
// permission_decision text carries the escalated token; a relay timeout
// keeps the parent's "deny" semantics.
const (
	escalatedTextAllow  = "escalated_human_allow"
	escalatedTextDeny   = "escalated_human_deny"
	escalatedTextNoWire = "escalated_human_no_relay"
)

// PermissionNotifier is the daemon-side publish seam for the relay lane.
// Implementations must not block: calls run on a permission goroutine
// (Transport.dispatchPermission) with NO session lock held (mutexio).
type PermissionNotifier interface {
	// NotifyPermissionRequested publishes the pending record. It returns
	// false when no consumer is listening — the caller STILL waits for a
	// Resolve until the timeout (a GUI that reconnects can still answer).
	NotifyPermissionRequested(p PendingPermission) bool
	// NotifyPermissionResolved publishes the terminal shape after the
	// wait completes (decision "allow"/"deny"; reason "answered",
	// "timeout", "no_relay", "full").
	NotifyPermissionResolved(p PendingPermission, decision, reason string)
}

// PendingPermission is the surfaced record of one unanswered
// session/requestPermission. It is a pure value snapshot — the resolve
// channel and once-guard live in the registry entry, so consumers (bus /
// RPC / TUI / GUI, leaf 03+) can copy it freely.
type PendingPermission struct {
	ID        string // pkg/id.Generate("perm")
	AgentID   string // catalog agent id
	SessionID string // ACP sessionId
	RequestID any    // JSON-RPC id echoed for resolve calls
	Summary   string // bounded 300-rune render of the raw params
	// (renderDecisionState budget — shared render)
	CreatedAt time.Time // registration stamp
}

// relayDecision is one terminal answer delivered to the waiting session.
type relayDecision struct {
	text  string // "allow" | "deny"
	allow bool
}

// relayEntry is a registry row: the snapshot, the wait channel, and the
// once-guard that makes Resolve exactly-once.
type relayEntry struct {
	p        PendingPermission
	ch       chan relayDecision
	once     sync.Once
	answered bool // guarded by Relay.mu
}

// Relay is the manager-owned pending-permission registry keyed by id. The
// mutex guards MAP OPERATIONS ONLY — never across a channel send, notifier
// call, or session emit (drift 11).
type Relay struct {
	mu      sync.Mutex
	pending map[string]*relayEntry
}

// NewRelay constructs an empty registry.
func NewRelay() *Relay {
	return &Relay{pending: make(map[string]*relayEntry)}
}

// Register inserts p (minting an ID when empty) and returns the channel
// the waiting session selects on. It fails with ErrRelayFull once
// maxPendingPermissions requests are outstanding — rejection, not
// eviction (drift 8).
func (r *Relay) Register(p PendingPermission) (<-chan relayDecision, error) {
	if r == nil {
		return nil, fmt.Errorf("acp: register on nil relay")
	}
	if p.ID == "" {
		p.ID = id.Generate("perm")
	}
	if p.CreatedAt.IsZero() {
		p.CreatedAt = time.Now()
	}
	e := &relayEntry{p: p, ch: make(chan relayDecision, 1)}

	r.mu.Lock()
	if r.pending == nil {
		r.pending = make(map[string]*relayEntry)
	}
	if _, dup := r.pending[p.ID]; dup {
		r.mu.Unlock()
		return nil, fmt.Errorf("acp: relay id %s already registered", p.ID)
	}
	outstanding := 0
	for _, existing := range r.pending {
		if !existing.answered {
			outstanding++
		}
	}
	if outstanding >= maxPendingPermissions {
		r.mu.Unlock()
		return nil, ErrRelayFull
	}
	r.pending[p.ID] = e
	r.mu.Unlock()
	return e.ch, nil
}

// Resolve delivers a terminal answer for id exactly once. The first call
// wins; later calls see ErrRelayAnswered, unknown ids ErrRelayNotFound.
// Anything that is not "allow" normalizes to the fail-closed deny verb.
func (r *Relay) Resolve(id string, decision string) error {
	if r == nil {
		return ErrRelayNotFound
	}
	r.mu.Lock()
	e, ok := r.pending[id]
	if !ok {
		r.mu.Unlock()
		return ErrRelayNotFound
	}
	if e.answered {
		r.mu.Unlock()
		return ErrRelayAnswered
	}
	e.answered = true
	r.mu.Unlock()

	d := relayDecision{text: relayDecisionDeny, allow: false}
	if decision == relayDecisionAllow {
		d = relayDecision{text: relayDecisionAllow, allow: true}
	}
	// Buffered channel (cap 1): the send never blocks, so the once-guard
	// runs safely outside the registry lock.
	e.once.Do(func() { e.ch <- d })
	return nil
}

// Pending returns a snapshot of the still-answerable requests, oldest
// first. Nil-safe: a nil or empty relay reports none. Answered entries are
// excluded (the waiting session forgets them once consumed).
func (r *Relay) Pending() []PendingPermission {
	if r == nil {
		return nil
	}
	r.mu.Lock()
	out := make([]PendingPermission, 0, len(r.pending))
	for _, e := range r.pending {
		if e.answered {
			continue
		}
		out = append(out, e.p)
	}
	r.mu.Unlock()
	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.Before(out[j].CreatedAt) })
	return out
}

// remove drops an entry from the registry once its wait completed
// (answered or timed out). Idempotent.
func (r *Relay) remove(id string) {
	if r == nil {
		return
	}
	r.mu.Lock()
	delete(r.pending, id)
	r.mu.Unlock()
}

// relayVerdict is the shared wait's result: allow the request or cancel
// it, plus the permission_resolved event text.
type relayVerdict struct {
	allow bool
	text  string // "allow" | "deny" | "timeout"
}

// relayPermissionWait runs the Contract B human wait for one request and
// is the SINGLE code path shared by the "human" lane and the
// decision-lane escalation (leaf 02 task 3). It NEVER holds s.mu and
// calls the notifier with no locks held (mutexio); it runs on a dedicated
// permission goroutine (Transport.dispatchPermission) and reads s.cfg,
// which is immutable after Start. A Transport Close ends the wait early
// (fail-closed deny) via the ServeContext lifetime channel.
func (s *Session) relayPermissionWait(requestID any, rawParams []byte) relayVerdict {
	p := PendingPermission{
		ID:        id.Generate("perm"),
		AgentID:   s.cfg.AgentID,
		SessionID: s.getSessionID(),
		RequestID: requestID,
		// Reuse the decision lane's bounded render (drift 2) — one
		// 300-rune budget, not two.
		Summary:   renderDecisionState(s.cfg.AgentID, rawParams),
		CreatedAt: time.Now(),
	}
	ch, err := s.cfg.Relay.Register(p)
	if err != nil {
		slog.Default().Warn("acp permission denied: human relay rejected the request",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"pending", p.ID,
			"reason", relayReasonFull,
			"err", err,
		)
		// Nothing reached any surface, so no requested event/notification;
		// the resolved event still closes the audit loop.
		s.emit(SessionEvent{Kind: "permission_resolved", Text: relayDecisionDeny})
		return relayVerdict{text: relayDecisionDeny}
	}
	// Frozen event shape (drift 7): Text = the pending id; the summary
	// stays inside PendingPermission.
	s.emit(SessionEvent{Kind: "permission_requested", Text: p.ID})
	if !s.cfg.Notifier.NotifyPermissionRequested(p) {
		// Reconnect contract: false means NO listener right now — wait on.
		slog.Default().Info("acp human permission relay: no listener, waiting for reconnect",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"pending", p.ID,
		)
	}
	wait := s.cfg.RelayTimeout
	if wait <= 0 {
		wait = defaultRelayTimeout
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	// closed is the transport-lifetime channel: Close cancels it so an
	// in-flight human wait exits instead of lingering for the full relay
	// timeout (the reply could not be written anyway). nil-safe: a bare
	// Done() on a nil transport would panic, but the harness and Start
	// always set s.tr; the nil receiver is handled inside Transport.Done.
	closed := s.tr.Done()
	select {
	case d := <-ch:
		s.cfg.Relay.remove(p.ID)
		s.emit(SessionEvent{Kind: "permission_resolved", Text: d.text})
		s.cfg.Notifier.NotifyPermissionResolved(p, d.text, relayReasonAnswer)
		return relayVerdict{allow: d.allow, text: d.text}
	case <-timer.C:
		s.cfg.Relay.remove(p.ID)
		slog.Default().Warn("acp permission denied: human relay timed out",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"pending", p.ID,
		)
		s.emit(SessionEvent{Kind: "permission_resolved", Text: relayTextTimeout})
		s.cfg.Notifier.NotifyPermissionResolved(p, relayDecisionDeny, relayReasonTimeout)
		return relayVerdict{text: relayTextTimeout}
	case <-closed:
		// Transport closed mid-wait: fail closed and leave the registry.
		s.cfg.Relay.remove(p.ID)
		slog.Default().Warn("acp permission denied: transport closed during relay wait",
			"component", "acp",
			"agent", s.cfg.AgentID,
			"pending", p.ID,
		)
		s.emit(SessionEvent{Kind: "permission_resolved", Text: relayDecisionDeny})
		s.cfg.Notifier.NotifyPermissionResolved(p, relayDecisionDeny, relayReasonTimeout)
		return relayVerdict{text: relayDecisionDeny}
	}
}
