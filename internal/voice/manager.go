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
	release  func(ccid string, outcome Outcome)

	// answerMu guards answerHook. The hook is the answered-notification
	// seam (audit 2026-11-06 H1): the daemon wiring registers the dialer's
	// no-answer-timer cancel on it, so an outbound call picked up just
	// before timer expiry keeps its slot. Without this seam the hook is
	// unwired and a no-answer Fail can land on a live, answered call.
	answerMu   sync.Mutex
	answerHook func(ccid string)
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
		cfg:      cfg,
		tel:      tel,
		spawn:    spawn,
		calls:    make(map[string]context.CancelFunc),
		stream:   make(map[string]context.CancelFunc),
		outbound: make(map[string]struct{}),
		done:     make(map[string]chan struct{}),
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
// manager-side field before the wiring landed).
func (m *Manager) notifyAnswered(ccid string) {
	m.answerMu.Lock()
	hook := m.answerHook
	m.answerMu.Unlock()
	if hook != nil {
		hook(ccid)
	}
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
	_, wasOutbound := m.outbound[ccid]
	delete(m.outbound, ccid)

	ch, hadDone := m.done[ccid]
	delete(m.done, ccid)
	if hadDone {
		close(ch)
	}

	// Known call = anything we registered. An id we never saw is a no-op.
	fire = hadCall || hadStream || wasOutbound || hadDone
	return callCancel, streamCancel, fire
}

// fireRelease invokes the release hook outside the manager mutex. Hook
// errors cannot fail call handling (the hook signature has no error), and a
// missing hook is a no-op.
func (m *Manager) fireRelease(ccid string, outcome Outcome) {
	m.mu.Lock()
	hook := m.release
	m.mu.Unlock()
	if hook != nil {
		hook(ccid, outcome)
	}
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
					err = fmt.Errorf("voice: spawn failed (%v); carrier hangup also failed (%v)", err, hErr)
				}
				m.failUntracked(ev.CallControlID, fmt.Sprintf("inbound spawn failed: %v", err))
				return err
			}
			m.mu.Lock()
			m.calls[ev.CallControlID] = cancel
			m.mu.Unlock()
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
		if trackedOut {
			// Dial-initiated (outbound) call: Telnyx already answered the leg
			// it dialed; there is nothing to answer. AnswerCall here returns
			// 404 on a live call and aborts the session (uptractor live
			// drill). The answered event streams exactly like inbound;
			// answering happens at call.initiated only.
		}
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
			m.fireRelease(ev.CallControlID, OutcomeHangup)
		}
		return nil
	default:
		// Unknown event types are rejected by telnyx.ParseCallEvent before
		// reaching the manager; ignore defensively.
		return nil
	}
}

// OnMediaStart registers the media session for ccid and runs dial (the
// per-call bridge runner) under a live per-call context. The first start for
// an id wins; a second start for the same id is a no-op so a Telnyx re-send
// or a duplicated frame cannot spawn two bridges on one socket. The
// registered cancel is what a later hangup invokes to tear the bridge down.
func (m *Manager) OnMediaStart(ccid string, dial func(context.Context) error) error {
	m.mu.Lock()
	if _, ok := m.stream[ccid]; ok {
		m.mu.Unlock()
		return nil // already registered: idempotent
	}
	callCtx, cancel := context.WithCancel(context.Background())
	m.stream[ccid] = cancel
	m.mu.Unlock()

	return dial(callCtx)
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
		m.fireRelease(ccid, outcome)
	}
}

// WaitCall returns a channel that closes when the call's session ends (its
// call.hangup or Fail is processed). A call the manager is CURRENTLY tracking
// yields its done channel - already closed if the session already ended.
// IMPORTANT (audit 2026-11-06 M3): per-call channels are deleted from the
// manager state on release, so a call whose terminal event was processed
// BEFORE this call (or whose ccid was never tracked) yields a FRESH channel
// that never closes - retention is NOT guaranteed across teardown. A dialer
// that must observe completion of a call it already holds a channel for is
// fine (it kept the original channel; that channel is closed once and stays
// closed), but a caller re-resolving by ccid after teardown cannot rely on
// seeing closure. Ids that were never tracked return a channel that never
// closes.
func (m *Manager) WaitCall(ccid string) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.done[ccid]; ok {
		return ch
	}
	return make(chan struct{})
}
