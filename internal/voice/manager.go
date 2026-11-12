// Package voice: the per-call state machine (C5). The Manager maps a Telnyx
// call_control_id to a live per-call session: it answers inbound calls,
// starts media streaming, and tears the session down on hangup without
// leaking. Borrowed from uptractor callsvc/manager.go with one addition: a
// Release hook fired exactly once per call so later leaves can unpark goals
// and free registry slots on ANY terminal outcome (hangup, no-answer,
// streaming failure) without this package importing internal/employee or
// internal/agent.
package voice

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/voice/telnyx"
)

// Outcome is the terminal state reported to the release hook.
type Outcome string

// Terminal outcomes for a call.
const (
	OutcomeAnswered Outcome = "answered"
	OutcomeNoAnswer Outcome = "no_answer"
	OutcomeFailed   Outcome = "failed"
	OutcomeHangup   Outcome = "hangup"
)

// outboundDoneCap bounds post-teardown WaitCall tombstones for outbound
// ccids (audit 2026-11-09 finding 2 residual); oldest-first eviction past
// the cap keeps the map bounded on long-running daemons.
const outboundDoneCap = 64

// ManagerConfig holds manager-wide call settings.
type ManagerConfig struct {
	// StreamURL is the full media-stream URL Telnyx should be told to dial on
	// call.answered. It is derived from telnyx.public_base_url (http->ws,
	// https->wss) plus telnyx.media_path. Empty disables answering inbound
	// calls (refusal happens BEFORE AnswerCall so the caller is never
	// stranded without a bridge).
	StreamURL string

	// Codec is the bidirectional audio codec requested on streaming_start.
	// Zero value is upgraded to telnyx.DefaultCodec (L16 @ 16000 Hz).
	Codec telnyx.Codec
}

// SessionSpawner builds the live session for an inbound call and returns a
// cancel func that tears it down. It is invoked on call.initiated so the
// session is tracked from the moment the call starts.
type SessionSpawner func(ctx context.Context, call telnyx.CallEvent) (context.CancelFunc, error)

// Manager tracks per-call sessions keyed by call_control_id. Telnyx webhooks
// (HandleCallEvent) and the media websocket handler (OnMediaStart) run
// concurrently, so all map access is mutex-guarded.
type Manager struct {
	cfg   ManagerConfig
	tel   telnyx.CallController
	spawn SessionSpawner

	mu       sync.Mutex
	calls    map[string]context.CancelFunc // spawn cancels, registered on call.initiated
	stream   map[string]context.CancelFunc // media bridge cancels, registered by OnMediaStart
	outbound map[string]struct{}           // registered by TrackOutbound; NEVER answered by us
	done     map[string]chan struct{}      // registered on track/initiate; closed once on hangup
	// doneTomb holds CLOSED done channels retained post-teardown for
	// outbound-tracked ccids (audit 2026-11-09 finding 2 residual), so a
	// later WaitCall by ccid observes closure instead of a fresh
	// never-closing channel. Bounded by outboundDoneCap. Inbound ids keep
	// the M3 no-retention posture.
	doneTomb map[string]chan struct{}
	release  func(ccid string, outcome Outcome)
	// spliceErrored marks ccids whose LAST OnMediaStart splice failed with
	// a non-cancellation error (audit 2026-11-09 voice finding 4): the
	// next media start for a marked id re-splices instead of hitting the
	// idempotent no-op. Guarded by mu; dropped on a clean splice result
	// and by releaseOnce with the rest of the per-call state (bounded:
	// at most one bool per live ccid).
	spliceErrored map[string]bool

	// answerMu guards answerHook. The hook is the answered-notification
	// seam (audit 2026-11-06 H1): the daemon wiring registers the dialer's
	// no-answer-timer cancel on it, so an outbound call picked up just
	// before timer expiry keeps its slot. Without this seam the hook is
	// unwired and a no-answer Fail can land on a live, answered call.
	answerMu   sync.Mutex
	answerHook func(ccid string)

	// termObsMu guards termObs: the ADDITIVE terminal-observer lane
	// (tree 20261110-voice-callleg leaf 01). releaseOnce + the release
	// hook stay the single terminal funnel (frozen 20261109); observers
	// are notified after the hook, outside the mutex, best-effort. The
	// CallLeg adapters use this lane for Events() without stealing the
	// single-occupancy release hook.
	termObsMu sync.Mutex
	termObs   []func(ccid string, outcome Outcome, reason string)

	// ansObsMu guards ansObs: the ADDITIVE answered-observer lane
	// (tree 20261110-voice-callleg leaf 02). answerHook stays the
	// single-occupancy timer-cancel seam (audit 2026-11-06 H1) — this
	// lane never replaces it; notifyAnswered invokes both (hook first,
	// then observers, outside the mutex). The phone CallLeg adapter uses
	// it for the Connected event.
	ansObsMu sync.Mutex
	ansObs   []func(ccid string)

	// actObsMu guards actObs: the ADDITIVE caller-activity observer lane
	// (leaf 02, plan 20261110-uptractor-ports): the dead-air call reaper
	// refreshes its own {ccid: lastActivity} map from it. The reaper keeps
	// NO state here — this lane only carries notifications, additive
	// exactly like ansObs/termObs; the Manager's per-ccid maps are
	// untouched.
	actObsMu sync.Mutex
	actObs   []func(ccid string)
}

// NewManager returns a Manager. spawn may be nil when the caller only uses
// OnMediaStart (the bridge path).
func NewManager(cfg ManagerConfig, tel telnyx.CallController, spawn SessionSpawner) *Manager {
	if cfg.Codec.BidirectionalCodec == "" {
		cfg.Codec.BidirectionalCodec = telnyx.DefaultCodec.BidirectionalCodec
	}
	if cfg.Codec.SamplingRate == 0 {
		cfg.Codec.SamplingRate = telnyx.DefaultCodec.SamplingRate
	}
	return &Manager{
		cfg:           cfg,
		tel:           tel,
		spawn:         spawn,
		calls:         make(map[string]context.CancelFunc),
		stream:        make(map[string]context.CancelFunc),
		outbound:      make(map[string]struct{}),
		done:          make(map[string]chan struct{}),
		spliceErrored: make(map[string]bool),
	}
}

// SetStreamURL replaces the media URL used by the next call event. Tunnel
// restarts change the public origin while the process is up; the webhook
// handler calls this before HandleCallEvent.
func (m *Manager) SetStreamURL(url string) {
	m.mu.Lock()
	m.cfg.StreamURL = url
	m.mu.Unlock()
}

// StreamURL returns the media URL the next call event will use.
func (m *Manager) StreamURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cfg.StreamURL
}

// SetReleaseHook installs the terminal-outcome callback. The hook fires
// exactly once per call_control_id, on teardown (hangup, timeout expiry, or
// Fail for streaming.failed / no-answer). A nil fn clears the hook. Hook
// invocations happen outside the manager mutex; errors inside the hook are
// the hook owner's problem and are never fatal to call handling.
func (m *Manager) SetReleaseHook(fn func(ccid string, outcome Outcome)) {
	m.mu.Lock()
	m.release = fn
	m.mu.Unlock()
}

// SetAnsweredHook installs the call.answered notification callback (audit
// 2026-11-06 H1). The webhook path invokes it for every call.answered on a
// tracked id - inbound or outbound - so the dialer can cancel the outbound
// no-answer timer the moment the call is picked up; without the wiring the
// timer can fire a late OutcomeNoAnswer on a live, answered call. A nil fn
// clears the hook. Hook invocations happen outside the manager mutex and
// never fail the webhook; unknown/tracked-refused ids do not invoke it.
func (m *Manager) SetAnsweredHook(fn func(ccid string)) {
	m.answerMu.Lock()
	m.answerHook = fn
	m.answerMu.Unlock()
}

// notifyAnswered invokes the answered hook outside the manager mutex. A
// missing hook is a no-op (the H1 defect shape: the seam existed only as a
// manager-side field before the wiring landed). The ADDITIVE answered-
// observer lane (leaf 02) is invoked after the hook — the hook stays
// the single-occupancy timer-cancel seam; observers never replace it.
func (m *Manager) notifyAnswered(ccid string) {
	m.answerMu.Lock()
	hook := m.answerHook
	m.answerMu.Unlock()
	if hook != nil {
		hook(ccid)
	}
	m.ansObsMu.Lock()
	if len(m.ansObs) == 0 {
		m.ansObsMu.Unlock()
		return
	}
	obs := make([]func(string), len(m.ansObs))
	copy(obs, m.ansObs)
	m.ansObsMu.Unlock()
	for _, fn := range obs {
		fn(ccid)
	}
}

// AddAnsweredObserver registers a best-effort observer on the manager's
// call.answered notification (ccid). Like AddTerminalObserver it is
// ADDITIVE — any number may register and the single-occupancy
// answerHook semantics are unchanged (the hook still runs first). The
// phone CallLeg adapter uses this lane for the Connected event without
// stealing the H1 timer-cancel seam.
func (m *Manager) AddAnsweredObserver(fn func(ccid string)) {
	if fn == nil {
		return
	}
	m.ansObsMu.Lock()
	m.ansObs = append(m.ansObs, fn)
	m.ansObsMu.Unlock()
}

// releaseOnce removes every per-call trace of ccid under the lock (so a
// concurrent duplicate terminal event sees nothing and no-ops) and returns
// the cancels plus whether the hook should fire. The done channel is closed
// exactly once here; unlike an earlier revision documented, it is deleted
// with the rest of the per-call state (audit 2026-11-06 M3), so a WaitCall
// AFTER teardown yields a fresh never-closing channel - retention is the
// original channel holder's job, not the manager's.
func (m *Manager) releaseOnce(ccid string) (callCancel, streamCancel context.CancelFunc, fire bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	callCancel, hadCall := m.calls[ccid]
	delete(m.calls, ccid)
	streamCancel, hadStream := m.stream[ccid]
	delete(m.stream, ccid)
	delete(m.spliceErrored, ccid) // re-splice marker dies with the call (finding 4)
	_, wasOutbound := m.outbound[ccid]
	delete(m.outbound, ccid)

	ch, hadDone := m.done[ccid]
	delete(m.done, ccid)
	if hadDone {
		close(ch)
	}
	// Outbound tombstone (audit 2026-11-09 finding 2 residual): an
	// outbound-tracked ccid re-resolved via WaitCall AFTER teardown must
	// observe closure — the dialer's acquire-failure path leaked exactly
	// because no post-terminal await was possible. The closed channel is
	// parked in doneTomb, NEVER back in done (releaseOnce closes what it
	// finds in done; re-storing a closed channel would double-close on a
	// late duplicate terminal). Inbound ids keep the M3 posture (no
	// retention) — the M3 pin uses an inbound ccid.
	if wasOutbound && hadDone {
		if m.doneTomb == nil {
			m.doneTomb = make(map[string]chan struct{}, outboundDoneCap)
		}
		if len(m.doneTomb) >= outboundDoneCap {
			// Arbitrary-entry eviction; the map is a bounded convenience,
			// not a queue (ccids resolve by exact key).
			for k := range m.doneTomb {
				delete(m.doneTomb, k)
				break
			}
		}
		m.doneTomb[ccid] = ch
	}

	// Known call = anything we registered. An id we never saw is a no-op.
	fire = hadCall || hadStream || wasOutbound || hadDone
	return callCancel, streamCancel, fire
}

// fireRelease invokes the release hook outside the manager mutex. Hook
// errors cannot fail call handling (the hook signature has no error), and a
// missing hook is a no-op. The additive terminal-observer lane (tree
// 20261110-voice-callleg leaf 01) is notified AFTER the hook with the
// outcome + reason: observers are best-effort consumers of the SAME
// once-per-ccid fire flag releaseOnce produced — they never add a second
// teardown path and an observer panic is the observer's problem, exactly
// like a hook error.
func (m *Manager) fireRelease(ccid string, outcome Outcome, reason string) {
	m.mu.Lock()
	hook := m.release
	m.mu.Unlock()
	if hook != nil {
		hook(ccid, outcome)
	}
	m.notifyTerminal(ccid, outcome, reason)
}

// AddTerminalObserver registers a best-effort terminal observer on the
// manager's release fan-out (ccid, outcome, reason). Unlike the release
// hook (single-occupancy, the frozen production funnel), observers are
// additive: any number may register and the hook semantics are
// unchanged. The CallLeg adapters use this lane for Events() (leaf 01).
func (m *Manager) AddTerminalObserver(fn func(ccid string, outcome Outcome, reason string)) {
	if fn == nil {
		return
	}
	m.termObsMu.Lock()
	m.termObs = append(m.termObs, fn)
	m.termObsMu.Unlock()
}

// notifyTerminal invokes every registered terminal observer outside both
// the manager mutex and the observer mutex (snapshot then call, the same
// pattern as notifyAnswered).
func (m *Manager) notifyTerminal(ccid string, outcome Outcome, reason string) {
	m.termObsMu.Lock()
	if len(m.termObs) == 0 {
		m.termObsMu.Unlock()
		return
	}
	obs := make([]func(string, Outcome, string), len(m.termObs))
	copy(obs, m.termObs)
	m.termObsMu.Unlock()
	for _, fn := range obs {
		fn(ccid, outcome, reason)
	}
}

// AddActivityObserver registers a best-effort observer on the manager's
// caller-activity notifications (ccid): every caller-side audio/turn edge
// (final prompt frames from the frame router). ADDITIVE like
// AddAnsweredObserver/AddTerminalObserver — any number may register and no
// existing seam changes. The dead-air reaper (leaf 02) refreshes its own
// {ccid: lastActivity} map from this lane; the manager holds no reaper
// state. Nil fn is ignored.
func (m *Manager) AddActivityObserver(fn func(ccid string)) {
	if fn == nil {
		return
	}
	m.actObsMu.Lock()
	m.actObs = append(m.actObs, fn)
	m.actObsMu.Unlock()
}

// NotifyCallActivity fires the caller-activity lane for ccid, outside all
// mutexes (snapshot then call). The frame router invokes it on every
// caller-side audio edge; a reaper not running costs one nil-check.
func (m *Manager) NotifyCallActivity(ccid string) {
	m.actObsMu.Lock()
	if len(m.actObs) == 0 {
		m.actObsMu.Unlock()
		return
	}
	obs := make([]func(string), len(m.actObs))
	copy(obs, m.actObs)
	m.actObsMu.Unlock()
	for _, fn := range obs {
		fn(ccid)
	}
}

// trackedCallIDs snapshots the ccids the manager currently tracks (the
// calls map + outbound set, which together cover inbound and outbound
// phone legs), excluding local/signal legs (their ids carry the
// "local:"/"signal:" prefixes; they have no Telnyx authority and are never
// probed). The reaper seeds its own dead-air map from this at startup;
// lock is held only for the copy.
func (m *Manager) trackedCallIDs() []string {
	m.mu.Lock()
	ids := make([]string, 0, len(m.calls)+len(m.outbound))
	for ccid := range m.calls {
		if isPhoneCCID(ccid) {
			ids = append(ids, ccid)
		}
	}
	for ccid := range m.outbound {
		if isPhoneCCID(ccid) {
			ids = append(ids, ccid)
		}
	}
	m.mu.Unlock()
	return ids
}

// isPhoneCCID reports whether ccid is a phone-leg id: NOT a local-leg
// ("local:<label>") or signal-leg ("signal:<uuid>") id.
func isPhoneCCID(ccid string) bool {
	return !strings.HasPrefix(ccid, "local:") && !strings.HasPrefix(ccid, "signal:")
}

// HandleCallEvent drives the call lifecycle:
//
//	call.initiated (incoming) -> AnswerCall, then build the session
//	call.answered            -> StartStreaming(ccid, StreamURL, Codec)
//	call.hangup              -> cancel the session (idempotent), fire release hook
//
// A hangup for an unknown call_control_id is nil: teardown is idempotent so
// a duplicate or late hangup after a call already ended never fails the
// webhook or re-fires the release hook.
func (m *Manager) HandleCallEvent(ctx context.Context, ev telnyx.CallEvent) error {
	switch ev.EventType {
	case "call.initiated":
		if ev.Direction != "incoming" {
			// Outbound legs are answered by Telnyx itself; nothing to do.
			return nil
		}
		if m.StreamURL() == "" {
			// No public media origin configured: answering would strand the
			// caller with no bridge, so refuse with a clear error and no side
			// effect (no AnswerCall).
			return errors.New("voice: public stream url not configured; cannot answer inbound calls")
		}
		if err := m.tel.AnswerCall(ctx, ev.CallControlID); err != nil {
			return err
		}
		m.mu.Lock()
		m.done[ev.CallControlID] = make(chan struct{})
		m.mu.Unlock()
		if m.spawn != nil {
			cancel, err := m.spawn(ctx, ev)
			if err != nil {
				// Spawn failed AFTER the carrier accepted the answer: the
				// session will never exist, so hang the leg up now
				// (audit 2026-11-06 H3) - otherwise the caller sits on a
				// dead line until some carrier-side timeout. The hangup is
				// best-effort; a failure folds into the surfaced error.
				// Either way the spawn failure is a terminal failed
				// outcome: failUntracked fires the release fan-out exactly
				// once so any registry/goal/effects state a spawn seam
				// variant acquired before failing still releases. A
				// redelivered initiated event for the now-dead leg fails
				// at AnswerCall, as any late event for an ended call does.
				if hErr := m.tel.HangupCall(ctx, ev.CallControlID); hErr != nil {
					err = fmt.Errorf("voice: spawn failed (%w); carrier hangup also failed (%w)", err, hErr)
				}
				m.failUntracked(ev.CallControlID, fmt.Sprintf("inbound spawn failed: %v", err))
				return err
			}
			// Spawn-completion re-check (bughunt 2026-11-10 A3, HIGH): the
			// done channel was registered BEFORE the spawn, so a hangup
			// racing the in-flight spawn ran releaseOnce (closed + deleted
			// done, fired the fan-out) before this late m.calls insert.
			// Registering the call anyway leaked its cancel forever — for
			// a phone leg with no media WS there is no spliceRunner/reaper
			// to ever tear it down, pinning the employee ErrBusy until
			// restart. If the done entry is gone the call was already
			// released: unwind immediately — cancel the session and push
			// the registry/goal/effects release through the SAME funnel
			// (FailOutcome -> releaseOnce -> fireRelease; the hook's
			// per-key once-guards make the second fire safe).
			m.mu.Lock()
			_, doneLive := m.done[ev.CallControlID]
			if doneLive {
				m.calls[ev.CallControlID] = cancel
			}
			m.mu.Unlock()
			if !doneLive {
				cancel()
				m.FailOutcome(ev.CallControlID, OutcomeFailed, "released during inbound spawn")
			}
		}
		return nil
	case "call.answered":
		m.mu.Lock()
		_, trackedOut := m.outbound[ev.CallControlID]
		_, trackedIn := m.calls[ev.CallControlID]
		m.mu.Unlock()
		if !trackedOut && !trackedIn {
			// No identity behind this answered event: refuse rather than
			// stream audio for a call we never initiated or answered.
			return fmt.Errorf("voice: call.answered for untracked call_control_id %q", ev.CallControlID)
		}
		// Answered on a tracked id: notify the answered hook (audit
		// 2026-11-06 H1 - the outbound no-answer timer cancel rides this).
		m.notifyAnswered(ev.CallControlID)
		// Dial-initiated (outbound) calls need no AnswerCall: Telnyx already
		// answered the leg it dialed, and AnswerCall returns 404 on a live call
		// and aborts the session (uptractor live drill). The answered event
		// streams exactly like inbound; answering happens at call.initiated only.
		m.mu.Lock()
		streamURL := m.cfg.StreamURL
		codec := m.cfg.Codec
		m.mu.Unlock()
		return m.tel.StartStreaming(ctx, ev.CallControlID, streamURL, codec)
	case "call.hangup":
		callCancel, streamCancel, fire := m.releaseOnce(ev.CallControlID)
		// Cancel the spawn session first (idempotent), then the media bridge
		// under its own per-call context. Unknown id hits neither: still nil.
		if callCancel != nil {
			callCancel()
		}
		if streamCancel != nil {
			streamCancel()
		}
		if fire {
			m.fireRelease(ev.CallControlID, OutcomeHangup, "call.hangup")
		}
		return nil
	default:
		// Unknown event types are rejected by telnyx.ParseCallEvent before
		// reaching the manager; ignore defensively.
		return nil
	}
}

// OnMediaStart registers the media session for ccid and runs dial (the
// per-call bridge runner) under a live per-call context. The first start
// for an id wins; a later start for the same id is an idempotent no-op so
// a Telnyx re-send or a duplicated frame cannot spawn two bridges on one
// socket — UNLESS the prior splice FAILED with a non-cancellation error
// (audit 2026-11-09 voice finding 4): a dead first bridge must not block
// the reconnect splice forever, so an errored (non-canceled) splice result
// is tracked per ccid and the next media start re-runs dial. A clean
// success keeps the strict no-op (error-aware, never count-aware — the
// review checklist pins that a legitimate duplicate after a CLEAN end
// must NOT re-splice). The registered cancel is what a later hangup
// invokes to tear the bridge down; the splice-error marker is dropped by
// releaseOnce (bounded: exactly one bool per live ccid, same lifetime as
// the stream entry).
func (m *Manager) OnMediaStart(ccid string, dial func(context.Context) error) error {
	m.mu.Lock()
	_, registered := m.stream[ccid]
	priorErrored := m.spliceErrored[ccid]
	if registered && !priorErrored {
		m.mu.Unlock()
		return nil // clean prior splice registered: idempotent
	}
	callCtx, cancel := context.WithCancel(context.Background())
	if m.stream == nil {
		m.stream = make(map[string]context.CancelFunc)
	}
	staleCancel := m.stream[ccid] // the dead bridge's cancel, only set on a re-splice
	m.stream[ccid] = cancel
	m.mu.Unlock()
	if staleCancel != nil {
		// Release the abandoned context (its dial already returned an
		// error, so nothing observes it — this only drops the leak).
		staleCancel()
	}

	err := dial(callCtx)

	m.mu.Lock()
	if err != nil && !errors.Is(err, context.Canceled) {
		// Non-cancellation splice failure: arm the ccid for re-splice.
		// A cancellation is teardown (hangup), not a dead bridge — the
		// release path already removed (or is removing) the entry, so
		// the clean-idempotent posture stays.
		if m.spliceErrored == nil {
			m.spliceErrored = make(map[string]bool)
		}
		m.spliceErrored[ccid] = true
	} else {
		delete(m.spliceErrored, ccid)
	}
	m.mu.Unlock()
	return err
}

// IsOutbound reports whether ccid is a leg WE dialed (registered by
// TrackOutbound). The open frame carries the call direction; the manager's
// outbound set is its own record of who placed the leg. A released ccid
// reports false (its entry is gone with the slot).
func (m *Manager) IsOutbound(ccid string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.outbound[ccid]
	return ok
}

// TrackOutbound registers a dial-initiated call_control_id so its later
// call.answered streams WITHOUT AnswerCall (answering an outbound leg 404s
// and aborts the session). Registering an empty id is an error;
// re-registering the same id is a no-op. Trust policy is unchanged: only the
// dial path reaches here, never an agent tool.
func (m *Manager) TrackOutbound(ccid string) error {
	if ccid == "" {
		return errors.New("voice: track outbound: empty call_control_id")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.outbound[ccid]; ok {
		return nil
	}
	m.outbound[ccid] = struct{}{}
	if _, ok := m.done[ccid]; !ok {
		m.done[ccid] = make(chan struct{})
	}
	return nil
}

// failUntracked tears down a ccid the manager never registered (H3
// fallback: the spawn seam failed before the call became tracked and the
// carrier hangup also failed). It creates the done channel, closes it, and
// fires the release hook with OutcomeFailed, so the registry slot the spawn
// seam acquired, the goal park, and any effects reservation release
// exactly once. A ccid that releaseOnce already claimed between check and
// here is a no-op: whichever teardown path runs first wins.
func (m *Manager) failUntracked(ccid, reason string) {
	m.mu.Lock()
	if _, ok := m.done[ccid]; !ok {
		m.done[ccid] = make(chan struct{})
	}
	m.mu.Unlock()
	m.FailOutcome(ccid, OutcomeFailed, reason)
}

// Fail tears down a tracked call on a non-hangup terminal path (streaming
// failed, no-answer timeout) and fires the release hook once with
// OutcomeFailed. An unknown ccid is a no-op, matching hangup idempotency.
func (m *Manager) Fail(ccid string, reason string) {
	m.FailOutcome(ccid, OutcomeFailed, reason)
}

// FailOutcome is Fail with an explicit outcome: leaf 11's no-answer timer
// reports OutcomeNoAnswer, streaming failures report OutcomeFailed. Same
// once-only release semantics as Fail. Unknown ccid is a no-op.
func (m *Manager) FailOutcome(ccid string, outcome Outcome, reason string) {
	callCancel, streamCancel, fire := m.releaseOnce(ccid)
	if callCancel != nil {
		callCancel()
	}
	if streamCancel != nil {
		streamCancel()
	}
	if fire {
		m.fireRelease(ccid, outcome, reason)
	}
}

// WaitCall returns a channel that closes when the call's session ends (its
// call.hangup or Fail is processed). A call the manager is CURRENTLY tracking
// yields its done channel - already closed if the session already ended.
// IMPORTANT (audit 2026-11-06 M3): per-call channels are deleted from the
// manager state on release, so a call whose terminal event was processed
// BEFORE this call (or whose ccid was never tracked) yields a FRESH channel
// that never closes - retention is NOT guaranteed across teardown - EXCEPT
// outbound-tracked ccids, whose closed channel is retained as a bounded
// tombstone (audit 2026-11-09 finding 2 residual: the dialer's
// acquire-failure path needs a post-terminal await). A dialer
// that must observe completion of a call it already holds a channel for is
// fine (it kept the original channel; that channel is closed once and stays
// closed). Inbound ids re-resolved after teardown and ids that were never
// tracked return a channel that never closes.
func (m *Manager) WaitCall(ccid string) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.done[ccid]; ok {
		return ch
	}
	if ch, ok := m.doneTomb[ccid]; ok {
		return ch
	}
	return make(chan struct{})
}
