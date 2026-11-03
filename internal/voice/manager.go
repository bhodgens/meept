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

// releaseOnce removes every per-call trace of ccid under the lock (so a
// concurrent duplicate terminal event sees nothing and no-ops) and returns
// the cancels plus whether the hook should fire. The done channel is closed
// exactly once here and retained for late WaitCall observers.
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
// call.hangup or Fail is processed). A tracked call always yields a channel
// - already closed if the session already ended - so a dialer can block on
// it and return when the call is done. Ids that were never tracked return a
// channel that never closes. The channel is retained after close, so late
// WaitCall callers still observe completion.
func (m *Manager) WaitCall(ccid string) <-chan struct{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	if ch, ok := m.done[ccid]; ok {
		return ch
	}
	return make(chan struct{})
}
