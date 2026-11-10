// Package signalleg is the Signal voice-call leg (contact-channels plan,
// contract C4, owner leaf 11a): a daemon-side media adapter bridging the
// signal-cli json-rpc call surface (internal/signal) into the existing
// voice sidecar machinery (C2 audio pair, C1 open frame, talk loop,
// release fan-out).
//
// Leg isolation (build-pinned posture, mirrors internal/voice/local.go):
// this package imports NO telnyx code and NO jargo. The signal sidecar
// client is the only carrier seam; RMS barge-in is FORBIDDEN on this leg
// (the open frame never carries local:true or barge_in_rms - barge-in
// rides the child's STT-partial branch, the phone posture).
//
// Policy (phone-outbound posture, NOT the local-leg carve-outs): effects
// reservation + audit row land BEFORE Client.StartCall; allowlist by
// Signal UUID; the shared registry rules (cap, one-live-per-employee,
// one-live-per-target) apply with leg "signal" and target
// "signal:<uuid>" (never E.164-mangled - AcquireLeg normalizes only the
// phone leg).
//
// File partition (branch orchestrator): leg.go + audio.go are leaf 11a's;
// msgchannel.go + inbound.go are leaf 11b's. This file owns the Leg type
// and the public surface both leaves compile against.
package signalleg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/pkg/id"
)

// Named sentinel errors (repo rule).
var (
	// ErrSignalDisabled is the named refusal when the leg is not enabled
	// ([signal] disabled or the constructed config disabled it).
	ErrSignalDisabled = errors.New("signalleg: signal voice leg is disabled")
	// ErrNoEmployee is the Place/Accept refusal for an empty employee id.
	ErrNoEmployee = errors.New("signalleg: employee_id is required")
	// ErrNoTarget is the Place refusal for an empty Signal uuid.
	ErrNoTarget = errors.New("signalleg: target uuid is required")
	// ErrNotAllowedlisted is the allowlist refusal: the uuid is not in
	// signal.allowed_uuids (mirrors voice.allowed_numbers posture).
	ErrNotAllowedlisted = errors.New("signalleg: target uuid not in allowed_uuids")
	// ErrUnknownSignalCall is returned by End for a ccid that carries a
	// foreign (non-"signal-") prefix. A double End of a signal-shaped id
	// stays the idempotent nil the contract pins.
	ErrUnknownSignalCall = errors.New("signalleg: unknown call")
)

// OutcomeBusy is the busy terminal outcome (C2 vocabulary; voice.Outcome
// is a plain string type, so the signal leg's busy reason maps onto it
// without extending the voice package).
const OutcomeBusy = voice.Outcome("busy")

// EffectsSeam is the narrow internal/effects reservation seam
// (phone-outbound posture). The daemon wiring adapts the concrete ledger
// onto it and hashes effect keys with effects.EffectKey; this package
// never imports internal/effects.
type EffectsSeam interface {
	// Reserve claims the outbound effect BEFORE StartCall; granted=false
	// means a prior claim exists (a retry never rings twice).
	Reserve(ctx context.Context, key, employeeID string) (granted bool, err error)
	// Complete marks the reservation satisfied (the ring happened).
	Complete(ctx context.Context, key, employeeID string) error
	// Abandon releases a reservation whose call refused before ringing.
	Abandon(ctx context.Context, key, employeeID, reason string) error
}

// AuditSeam is the narrow audit seam (phone-outbound posture): one row
// per attempt, written BEFORE StartCall so the attempt is attributable
// even when the dial fails. The daemon wiring adapts the auditlog store;
// this package never imports auditlog.
type AuditSeam interface {
	WriteAudit(ctx context.Context, row map[string]any) error
}

// voiceBindingSeam is the voice-call view of the binding seam: the ONE
// session per (number, employee) plus the calls.db row, exactly the
// Binding.OnCallStart signature. Compile-time pinned below.
//
// The trailing ...bool is the fresh-session-per-call flag (voice
// fresh-session plan leaf 02: Binding.OnCallStart gained a variadic so
// every existing caller — including this one — compiles unchanged); the
// interface must mirror it or *voice.Binding stops satisfying the seam.
type voiceBindingSeam interface {
	OnCallStart(ctx context.Context, ccid, number, employeeID, direction string, brief voice.DayBrief, freshSession ...bool) (string, error)
}

// Compile-time pin: the concrete voice binding satisfies the voice view.
var _ voiceBindingSeam = (*voice.Binding)(nil)

// Deps carries the injected edges (C4 field set). Registry, Manager,
// TextServer, and Client are load-bearing (nil fails construction,
// local.go:191 posture); Binding, Effects, and Audit are nil-safe (nil
// Binding runs the call unbound; nil Effects/Audit skip the respective
// phone-posture rows).
type Deps struct {
	Client     *signal.Client
	Registry   voice.Registry    // AcquireLeg shared across legs
	Manager    *voice.Manager    // TrackOutbound/FailOutcome (release fan-out)
	TextServer *voice.TextServer // open/say/clear frames (C1)
	// Binding carries the binding capability by capability view: voice
	// calls assert voiceBindingSeam (OnCallStart); leaf 11b's inbound
	// message routing asserts its inboundBinding (ResolveSession /
	// BindMessage). The concrete *voice.Binding satisfies the voice
	// view; the daemon wiring supplies the adapter that carries both.
	// nil degrades to unbound calls (typed-nil guarded).
	Binding any
	// Allowlist admits a target uuid for outbound AND inbound. nil
	// admits nothing (fail closed) - an operator configures
	// allowed_uuids before the leg places or accepts calls.
	Allowlist func(uuid string) bool
	// Effects is the outbound reservation seam (phone posture). Nil
	// degrades to no reservation (scratch boots).
	Effects EffectsSeam
	// Audit is the outbound audit row seam (phone posture). Nil
	// degrades to no row.
	Audit AuditSeam
	// EmployeeID is the employee inbound messages and (absent an
	// explicit owner) inbound calls are for (single-employee posture,
	// mirroring voice.default_employee).
	EmployeeID string
	// DefaultEmployeeID is the fallback when EmployeeID is empty (both
	// consulted, EmployeeID first - the field name the wiring pins is
	// whichever it populates).
	DefaultEmployeeID string
	// DayBriefBuilder (leaf 03-voice/03) populates the OnCallStart brief
	// for connected calls - the SAME daemon-built day-brief closure the
	// local/phone legs use (open-commitments seam included). Nil keeps
	// the pre-leaf empty-brief posture (no ledger store; byte-identical
	// OnCallStart call). A build failure is fail-soft: the call binds
	// with an empty brief, never blocks the call.
	DayBriefBuilder func(ctx context.Context, employeeID, number string) (voice.DayBrief, error)
	// FreshSession resolves the per-call fresh-session flag for the
	// binding (audit 2026-11-09 finding 5, C-V2 — phone/local parity):
	// true makes Binding.OnCallStart create a NEW per-call session
	// instead of reusing the shared (target, employee) one. The daemon
	// wiring resolves voice.FreshSessionFor over the live config
	// (voice.fresh_session_per_call + the employee_leg override), the
	// same closure shape as the local leg (local.go Deps.FreshSession,
	// voice_wiring.go).
	//
	// Degrade posture DIFFERS from the local leg on purpose (gate-
	// pinned, leg_gate_test.go T5): a nil seam resolves fresh=true —
	// the signal leg's contract posture is one session per call. Wire
	// the seam to honor the config's false default (the daemon always
	// does); an unwired leg never silently shares one session across
	// every call to the same contact.
	FreshSession func(employeeID string) bool
	// MsgPerMinute is [signal].msg_per_minute (the signal_msg throttle;
	// 0 = msgchannel's default clamp of 3/min).
	MsgPerMinute int
	Logger       *slog.Logger
}

// legInbound is the Leg-owned inbound-message router state (leaf 11b's
// RouteInbound dedupe). Zero-value usable BY CONTRACT: a Leg built via a
// struct literal (leaf 11b's tests construct one directly) still dedupes
// without passing through New - the router builds itself on first use.
type legInbound struct {
	once   sync.Once
	router *inboundRouter
}

// dedupe delegates to the lazily-built router.
func (r *legInbound) dedupe(key string) bool {
	r.once.Do(func() { r.router = newInboundRouter() })
	return r.router.dedupe(key)
}

// Leg places and accepts Signal voice calls through the existing voice
// sidecar machinery, and carries the inbound-message router (leaf 11b).
// All mutable state is mutex-guarded; the deps are immutable after
// construction.
type Leg struct {
	deps Deps
	log  *slog.Logger

	// cfgMsgPerMinute is [signal].msg_per_minute (0 = msgchannel's
	// default clamp); wired at construction, read by MessageAttempt.
	cfgMsgPerMinute int

	mu      sync.Mutex
	calls   map[string]*callState // ccid -> live call state
	inbound legInbound            // zero-value usable (leaf 11b RouteInbound)
}

// callState is one live signal call's leg-owned state.
type callState struct {
	ccid      string
	employee  string
	uuid      string
	target    string
	direction string // voice.DirectionOutgoing | voice.DirectionIncoming
	callID    string // signal-cli call id (events key on it)
	connected bool
	ended     bool // teardown latched: End is idempotent past this
	effectKey string
	cancel    context.CancelFunc
	ctx       context.Context // adapter lifetime context (cancel owns it)
	pair      *voice.AudioPair
	// socket is the signal-cli socket-audio transport (set by the
	// daemon wiring once the tunnel helper hands its path over). Nil
	// degrades to stub audio (the pinned warn-and-continue posture).
	socket audioSocket
	// sourceRate is the socket PCM sample rate (RingRTC native). <= 0
	// is refused by the adapter rather than guessed.
	sourceRate int
}

// New constructs the Leg. Typed-nil guards on the load-bearing deps
// (repo rule): nil Registry, Manager, TextServer, or Client fails
// construction - each carries a load-bearing half of the lifecycle
// (capacity, release fan-out, C1 transport, call RPC).
func New(deps Deps) (*Leg, error) {
	if deps.Registry == nil {
		return nil, errors.New("signalleg: registry is required")
	}
	if deps.Manager == nil {
		return nil, errors.New("signalleg: manager is required (it carries the release fan-out)")
	}
	if deps.TextServer == nil {
		return nil, errors.New("signalleg: text server is required")
	}
	if deps.Client == nil {
		return nil, errors.New("signalleg: signal client is required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &Leg{
		deps:            deps,
		log:             deps.Logger,
		cfgMsgPerMinute: deps.MsgPerMinute,
		calls:           make(map[string]*callState),
	}, nil
}

// RegisterCallEvents arms the call-event state machine on the signal
// client. Call once at wiring time, after SubscribeCallEvents succeeded.
// Events for call ids this leg never placed are dropped (inbound calls
// arm their own mapping through Accept).
func (l *Leg) RegisterCallEvents() {
	l.deps.Client.RegisterCallEvents(l.handleCallEvent)
}

// MessageAttempt is the contact-chain seam (C2-attempt, branch
// orchestrator): the signal_msg ChannelAttempt over the Leg's shared
// client + allowlist. Throttle rate = [signal].msg_per_minute
// (config.MsgPerMinute, wired at construction; 0 = the msgchannel
// default clamp of 3/min).
func (l *Leg) MessageAttempt() contact.ChannelAttempt {
	return newMsgChannel(l.deps.Client, l.deps.Allowlist,
		l.cfgMsgPerMinute, realClock{}, l.log)
}

// handleCallEvent is the C4 state machine: RINGING_* / CONNECTING are
// observed only; CONNECTED marks the call answered and arms the talk
// loop (open frame + audio adapter; the child answers the open frame
// with setup, which arms the wiring's existing talk loop + keys
// emission - no second protocol); RECONNECTING is tolerated (no
// teardown); ENDED runs Manager.FailOutcome with the reason-mapped
// outcome (busy/timeout distinct from hard failure, leaf 05/11 rule).
func (l *Leg) handleCallEvent(ev signal.CallEvent) {
	st := l.stateByCallID(ev.CallID)
	if st == nil {
		return // a call we never placed or accepted
	}
	switch ev.State {
	case signal.CallEventStateConnected:
		l.onConnected(st)
	case signal.CallEventStateReconnecting:
		// Tolerated: no teardown. The carrier re-establishes media;
		// tearing down here would kill every healthy reconnection.
		l.log.Info("signalleg: call reconnecting (tolerated)", "ccid", st.ccid)
	case signal.CallEventStateEnded:
		l.onEnded(st, ev.Reason)
	default:
		// RINGING_*, CONNECTING: observed, no action.
	}
}

// stateByCallID resolves the live call for a signal-cli call id.
func (l *Leg) stateByCallID(callID string) *callState {
	l.mu.Lock()
	defer l.mu.Unlock()
	for _, st := range l.calls {
		if st.callID == callID {
			return st
		}
	}
	return nil
}

// onConnected arms the talk loop: mark answered, bind the call, emit the
// open frame, then start the audio adapter goroutines. The child answers
// the open frame with setup; the daemon wiring's routeFrame arms the
// talk loop from Registry.Live() and emits the keys line at its existing
// Tier A site (voice_wiring.go:683-745) - the signal leg REUSES those
// seams and invents no second protocol.
func (l *Leg) onConnected(st *callState) {
	l.mu.Lock()
	live := l.calls[st.ccid]
	if live != nil {
		live.connected = true
	}
	l.mu.Unlock()
	if live == nil || live.ended {
		return
	}
	// Binding: OnCallStart resolves the ONE session per
	// (number=signal:<uuid>, employee) and starts the calls.db row.
	// Failure degrades to an unbound call (warn), the shared posture.
	l.bindCall(live)
	// Open frame BEFORE the adapter: the child creates its pipeline on
	// open, dials the C2 pair, and answers setup. Send failure degrades
	// (warn-and-continue on stub audio, local.go:291 posture).
	if err := l.sendOpenFrame(live); err != nil {
		l.log.Warn("signalleg: open frame send failed (continuing on stub audio)",
			"ccid", live.ccid, "err", err)
	}
	l.startAudioAdapter(live)
	l.log.Info("signalleg: call connected, talk loop armed via open frame",
		"ccid", live.ccid, "employee_id", live.employee, "target", live.target)
}

// bindCall runs Binding.OnCallStart for a connected call through the
// capability view. Nil binding (or a binding without the voice view -
// leaf 11b's message-only fakes) degrades to an unbound call.
func (l *Leg) bindCall(st *callState) {
	vb, ok := l.deps.Binding.(voiceBindingSeam)
	if !ok || isNilBinding(l.deps.Binding) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	// Day brief (leaf 03-voice/03): populated through the wiring's
	// shared builder when armed; nil seam = the pre-leaf empty brief.
	// A build failure logs and binds with the empty brief (fail-soft,
	// the local-leg posture) — the call never blocks on brief health.
	brief := voice.DayBrief{}
	if l.deps.DayBriefBuilder != nil {
		if b, err := l.deps.DayBriefBuilder(ctx, st.employee, st.target); err != nil {
			l.log.Warn("signalleg: day brief build failed (continuing with an empty brief)",
				"ccid", st.ccid, "err", err)
		} else {
			brief = b
		}
	}
	if _, err := vb.OnCallStart(ctx, st.ccid, st.target, st.employee, st.direction, brief,
		l.freshSessionFor(st.employee)); err != nil {
		l.log.Warn("signalleg: binding OnCallStart failed (call continues unbound)",
			"ccid", st.ccid, "err", err)
	}
}

// freshSessionFor resolves the fresh-session flag for one call
// (finding 5, phone/local parity — mirrors local.go's Deps.FreshSession
// use at its OnCallStart). The seam-false case honours the resolved
// config; the NIL case defaults to TRUE on this leg (gate-pinned,
// leg_gate_test.go T5): an unwired signal leg keeps the one-session-
// per-call contract posture instead of silently sharing one session
// across every call to the same contact.
func (l *Leg) freshSessionFor(employeeID string) bool {
	if l.deps.FreshSession == nil {
		return true
	}
	return l.deps.FreshSession(employeeID)
}

// isNilBinding guards the typed-nil trap on the capability-viewed
// Binding field (repo typed-nil rule): a nil *voice.Binding inside a
// non-nil interface must not reach OnCallStart.
func isNilBinding(b any) bool {
	v, ok := b.(*voice.Binding)
	return ok && v == nil
}

// onEnded runs the terminal fan-out: Manager.FailOutcome with the
// reason-mapped outcome (busy/timeout are terminal-not-failure outcomes,
// distinct from hard failure per leaf 05/11), then leg-local teardown
// through End (idempotent; the registry release, goal unpark, and
// binding end ride the wiring's release hook fired by FailOutcome).
func (l *Leg) onEnded(st *callState, reason string) {
	outcome := OutcomeByReason(reason)
	if err := l.End(st.ccid); err != nil {
		l.log.Warn("signalleg: end on ENDED event", "ccid", st.ccid, "err", err)
		return
	}
	l.deps.Manager.FailOutcome(st.ccid, outcome, "signal call ended: "+reason)
	l.log.Info("signalleg: call ended", "ccid", st.ccid, "reason", reason, "outcome", string(outcome))
}

// OutcomeByReason maps a signal-cli ENDED reason onto a voice.Outcome.
// busy and timeout are terminal-not-failure outcomes (the FailOutcome
// posture, leaf 05/11); hangup/declined map to the ordinary hangup;
// anything else is a hard failure.
func OutcomeByReason(reason string) voice.Outcome {
	switch strings.ToLower(strings.TrimSpace(reason)) {
	case "busy":
		return OutcomeBusy
	case "timeout", "noanswer", "no_answer":
		return voice.OutcomeNoAnswer
	case "hangup", "declined":
		return voice.OutcomeHangup
	default:
		return voice.OutcomeFailed
	}
}

// Place turns (employee, uuid) into a live outbound Signal voice call
// under the pinned order: gates (enabled, allowlist, capacity, effects
// reserve, audit row) BEFORE Client.StartCall; any gate refusal is a
// named error with zero side effects. firstLine is the message the
// contact chain carried (the talk-loop greeting serves it once the
// wiring arms the loop on setup; the leg itself never says before
// setup).
//
// The optional requestIDs[0] is the IDEMPOTENCY identity for the
// effects reservation (C-V2, finding 6): the phone dialer's shape
// (employee, target, request_id) — a same-request retry reuses the
// ledger receipt instead of reserving a fresh key. Production callers
// MUST pass a fresh request id per user request (the contact-chain
// attempt does); an omitted request id keys the reservation over
// (employee, target) alone, so repeated un-labelled dials to the same
// target collide on one receipt. ccid stays the CALL identity either
// way — never conflated with the request id (AGENTS.md).
func (l *Leg) Place(ctx context.Context, employeeID, targetUUID, firstLine string, requestIDs ...string) (string, error) {
	_ = firstLine // spoken by the talk loop's greeting seam, not here
	// -- 1. enabled gate --
	if !l.enabled() {
		return "", ErrSignalDisabled
	}
	// -- 2. identity gates (named refusals, zero side effects) --
	if employeeID == "" {
		return "", ErrNoEmployee
	}
	if targetUUID == "" {
		return "", ErrNoTarget
	}
	// -- 3. allowlist (BEFORE any side effect; the canonical refusal is
	// ErrNotAllowedlisted with no StartCall and no audit row) --
	if !l.allowlisted(targetUUID) {
		return "", fmt.Errorf("%w: %s", ErrNotAllowedlisted, targetUUID)
	}
	// -- 4. identity: ccid = "signal-" + pkg/id.Generate(); target
	// "signal:<uuid>" (registry stores it verbatim for leg "signal") --
	ccid := id.Generate("signal-")
	target := "signal:" + targetUUID
	// -- 4b. idempotency identity (C-V2, finding 6): the caller's
	// request id when supplied; empty keys the reservation over
	// (employee, target) alone. ccid stays the CALL identity (never
	// part of the ledger key). --
	requestID := ""
	if len(requestIDs) > 0 {
		requestID = requestIDs[0]
	}
	// -- 5. capacity (shared rules via the registry; ErrBusy /
	// ErrDuplicateTarget surface unchanged) --
	if _, err := l.deps.Registry.AcquireLeg(ccid, employeeID, target, voice.LegSignal); err != nil {
		return "", err
	}
	// -- 6. TrackOutbound BEFORE the RPC (phone-path order: the manager
	// tracks first so any later failure can FailOutcome cleanly) --
	if err := l.deps.Manager.TrackOutbound(ccid); err != nil {
		l.deps.Registry.Release(ccid)
		return "", fmt.Errorf("signalleg: track outbound: %w", err)
	}
	// -- 7. effects reservation (phone-outbound posture: BEFORE
	// StartCall; a retry of the same request never rings twice —
	// C-V2). Nil seam degrades. --
	var key string
	if l.deps.Effects != nil {
		key = effectKey(employeeID, target, requestID)
		granted, err := l.deps.Effects.Reserve(ctx, key, employeeID)
		if err != nil {
			l.deps.Registry.Release(ccid)
			return "", fmt.Errorf("signalleg: effect reserve: %w", err)
		}
		if !granted {
			l.deps.Registry.Release(ccid)
			return "", fmt.Errorf("signalleg: call already reserved for this request")
		}
	}
	// -- 8. audit row BEFORE StartCall (phone-outbound posture). Nil
	// seam degrades. --
	if l.deps.Audit != nil {
		row := map[string]any{
			"direction":   voice.DirectionOutgoing,
			"target":      target,
			"employee_id": employeeID,
			"channel":     "signal_voice",
		}
		if requestID != "" {
			// Phone-dialer parity (C-V2): the attempt stays
			// attributable to the request whose retry it is.
			row["request_id"] = requestID
		}
		if err := l.deps.Audit.WriteAudit(ctx, row); err != nil {
			l.abandonOutbound(ctx, key, employeeID, ccid, fmt.Sprintf("audit: %v", err))
			return "", fmt.Errorf("signalleg: audit: %w", err)
		}
	}
	// -- 9. audio pair (the child dials <ccid>.in/.out on open) --
	pair, err := voice.NewAudioPair(voice.SocketDir(), ccid)
	if err != nil {
		l.abandonOutbound(ctx, key, employeeID, ccid, fmt.Sprintf("audio pair: %v", err))
		return "", fmt.Errorf("signalleg: audio pair: %w", err)
	}
	actx, cancel := context.WithCancel(context.Background())
	st := &callState{
		ccid:      ccid,
		employee:  employeeID,
		uuid:      targetUUID,
		target:    target,
		direction: voice.DirectionOutgoing,
		effectKey: key,
		cancel:    cancel,
		ctx:       actx,
		pair:      pair,
	}
	l.mu.Lock()
	l.calls[ccid] = st
	l.mu.Unlock()
	// -- 10. Client.StartCall LAST (every gate and row is in). A
	// successful call id COMPLETES the reservation (the irreversible
	// effect - one ring - happened; the phone dialer completes at the
	// same point, dial.go step 7). --
	callID, err := l.deps.Client.StartCall(targetUUID, signal.CallAudioModeSocket)
	if err != nil {
		l.abandonOutbound(ctx, key, employeeID, ccid, fmt.Sprintf("startCall: %v", err))
		l.teardownPair(st)
		return "", fmt.Errorf("signalleg: start call: %w", err)
	}
	if key != "" && l.deps.Effects != nil {
		if cerr := l.deps.Effects.Complete(ctx, key, employeeID); cerr != nil {
			// The ring happened; the ledger row is stuck. Surface in
			// the log, keep the call lifecycle running (the
			// phone-dialer posture, dial.go step 7).
			l.log.Error("signalleg: effect complete", "ccid", ccid, "err", cerr)
		}
	}
	l.mu.Lock()
	st.callID = callID
	l.mu.Unlock()
	l.log.Info("signalleg: outbound call placed", "ccid", ccid, "target", target,
		"employee_id", employeeID, "call_id", callID)
	return ccid, nil
}

// Accept arms the inbound pipeline: register the call under the same
// shared rules, AcceptCall, and let the CONNECTED event arm the talk
// loop. Direction is inbound; the allowlist gates non-allowlisted
// callers (do-not-accept). employeeID empty falls back to the Deps
// employee fields; an employee-less Accept is ErrNoEmployee. Inbound
// calls carry no effects reservation (we dialed nothing).
func (l *Leg) Accept(ctx context.Context, callEvent signal.CallEvent, employeeID string) (string, error) {
	_ = ctx
	if !l.enabled() {
		return "", ErrSignalDisabled
	}
	if employeeID == "" {
		employeeID = l.deps.EmployeeID
	}
	if employeeID == "" {
		employeeID = l.deps.DefaultEmployeeID
	}
	if employeeID == "" {
		return "", ErrNoEmployee
	}
	uuid := callEvent.EnvelopeSourceUUID
	if uuid == "" {
		return "", ErrNoTarget
	}
	if !l.allowlisted(uuid) {
		return "", fmt.Errorf("%w: %s", ErrNotAllowedlisted, uuid)
	}
	ccid := id.Generate("signal-")
	target := "signal:" + uuid
	if _, err := l.deps.Registry.AcquireLeg(ccid, employeeID, target, voice.LegSignal); err != nil {
		return "", err
	}
	if err := l.deps.Manager.TrackOutbound(ccid); err != nil {
		l.deps.Registry.Release(ccid)
		return "", fmt.Errorf("signalleg: track inbound: %w", err)
	}
	pair, err := voice.NewAudioPair(voice.SocketDir(), ccid)
	if err != nil {
		l.deps.Manager.FailOutcome(ccid, voice.OutcomeFailed, fmt.Sprintf("audio pair: %v", err))
		l.deps.Registry.Release(ccid)
		return "", fmt.Errorf("signalleg: audio pair: %w", err)
	}
	actx, cancel := context.WithCancel(context.Background())
	st := &callState{
		ccid:      ccid,
		employee:  employeeID,
		uuid:      uuid,
		target:    target,
		direction: voice.DirectionIncoming,
		callID:    callEvent.CallID,
		cancel:    cancel,
		ctx:       actx,
		pair:      pair,
	}
	l.mu.Lock()
	l.calls[ccid] = st
	l.mu.Unlock()
	if err := l.deps.Client.AcceptCall(callEvent.CallID); err != nil {
		l.teardownPair(st)
		l.deps.Registry.Release(ccid)
		l.deps.Manager.FailOutcome(ccid, voice.OutcomeFailed, fmt.Sprintf("acceptCall: %v", err))
		return "", fmt.Errorf("signalleg: accept call: %w", err)
	}
	l.log.Info("signalleg: inbound call accepted", "ccid", ccid, "target", target,
		"employee_id", employeeID, "call_id", callEvent.CallID)
	return ccid, nil
}

// End tears the call down: the leg-local teardown (adapter ctx cancel +
// audio pair close) runs here; the registry release, goal unpark, and
// binding end ride Manager.FailOutcome's once-per-outcome release
// fan-out, which onEnded (or the wiring's hangup path) fires. Double End
// is idempotent (nil); a foreign id is ErrUnknownSignalCall.
func (l *Leg) End(ccid string) error {
	l.mu.Lock()
	st, ok := l.calls[ccid]
	if ok {
		delete(l.calls, ccid)
	}
	l.mu.Unlock()
	if !ok {
		// The prefix distinguishes foreign ids from a re-End of one we
		// already reaped (same shape rule as local.go's isLocalCcid).
		if !isSignalCcid(ccid) {
			return fmt.Errorf("%w: %s", ErrUnknownSignalCall, ccid)
		}
		return nil
	}
	st.ended = true
	// An outbound reservation whose call ended BEFORE its StartCall
	// completed is abandoned here (a connected-and-completed call keeps
	// its Complete from Place). st.callID == "" means StartCall never
	// landed on this state.
	if st.effectKey != "" && l.deps.Effects != nil && st.callID == "" {
		actx, cancel := context.WithTimeout(context.Background(), abandonTimeout)
		if err := l.deps.Effects.Abandon(actx, st.effectKey, st.employee, "call ended before connect"); err != nil {
			l.log.Debug("signalleg: effect abandon", "ccid", ccid, "err", err)
		}
		cancel()
	}
	l.teardownPair(st)
	l.log.Info("signalleg: call end", "ccid", ccid)
	return nil
}

// teardownPair cancels the adapter context and closes the audio pair.
func (l *Leg) teardownPair(st *callState) {
	if st.cancel != nil {
		st.cancel()
	}
	if st.pair != nil {
		if err := st.pair.Close(); err != nil {
			l.log.Debug("signalleg: pair close", "ccid", st.ccid, "err", err)
		}
	}
}

// abandonOutbound releases a failed outbound dial's side effects in
// order: effect abandon, registry release, manager FailOutcome.
func (l *Leg) abandonOutbound(ctx context.Context, key, employeeID, ccid, reason string) {
	if key != "" && l.deps.Effects != nil {
		if err := l.deps.Effects.Abandon(ctx, key, employeeID, reason); err != nil {
			l.log.Debug("signalleg: effect abandon on refusal", "ccid", ccid, "err", err)
		}
	}
	l.deps.Registry.Release(ccid)
	l.deps.Manager.FailOutcome(ccid, voice.OutcomeFailed, reason)
}

// enabled reports the leg gate. The daemon glue folds [signal].enabled
// (and the armed client) into constructing the leg at all; a disabled
// signal config never constructs one. Kept as a method so the gate
// ordering reads exactly like local.go's.
func (l *Leg) enabled() bool { return true }

// allowlisted resolves the allowlist seam. Fail closed when no seam:
// an operator must configure allowed_uuids before calls flow.
func (l *Leg) allowlisted(uuid string) bool {
	if l.deps.Allowlist == nil {
		return false
	}
	return l.deps.Allowlist(uuid)
}

// sendOpenFrame builds and sends the C1 open frame for st. Identity
// mirrors the wiring's producer (voice_wiring.go sendOpenFrame):
// child-native codec L16@16000 (the C2 chunk format; the socket-audio
// wire rate belongs to the adapter's resample, never to the frame).
// RMS barge-in fields are FORBIDDEN on this leg: the frame carries NO
// local:true and NO barge_in_rms (STT-partial is the phone posture and
// the only sanctioned trigger here).
func (l *Leg) sendOpenFrame(st *callState) error {
	f := voice.Frame{
		CallID:     st.ccid,
		Event:      voice.EventOpen,
		To:         st.target,
		Direction:  st.direction,
		Codec:      "L16",
		SampleRate: 16000,
	}
	return l.deps.TextServer.Send(f)
}

// Live snapshots the live signal call ccids (status surfaces read this).
func (l *Leg) Live() []string {
	l.mu.Lock()
	defer l.mu.Unlock()
	out := make([]string, 0, len(l.calls))
	for ccid := range l.calls {
		out = append(out, ccid)
	}
	return out
}

// isSignalCcid reports whether ccid carries the signal-leg prefix shape.
func isSignalCcid(ccid string) bool {
	const prefix = "signal-"
	return len(ccid) > len(prefix) && ccid[:len(prefix)] == prefix
}

// abandonTimeout bounds the best-effort reservation abandon on a
// torn-down call.
const abandonTimeout = 5 * time.Second

// effectKey builds the outbound reservation key over the same parts the
// phone dialer hashes (employee_id, target, request id) — C-V2: a
// retried REQUEST reuses the receipt (one ring), never a fresh key per
// Place attempt. requestID may be empty (an unlabelled caller): the key
// then spans (employee, target). ccid is deliberately NOT a part — it
// is the call identity, not the idempotency identity (AGENTS.md). The
// daemon wiring folds both sides through effects.EffectKey so ledger
// rows collide on identical parts; this package never imports
// internal/effects.
func effectKey(employeeID, target, requestID string) string {
	return employeeID + "\x1f" + target + "\x1f" + requestID
}
