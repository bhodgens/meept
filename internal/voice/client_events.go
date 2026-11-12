package voice

// client_events.go - the daemon->client CONTROL lane for one local call
// (bughunt 2026-11-11 defect 1: the ~750 ms ghost-tail on barge-in).
//
// WHY A SEPARATE SOCKET: the client pair (<ccid>.c.in / <ccid>.c.out,
// local.go) carries RAW PCM ONLY by contract (AGENTS.md "Sockets (C2)") -
// a JSON control frame interleaved there would play as garbage samples.
// So the clear signal rides its own unix LISTENER beside the pair,
// <ccid>.c.ev, owned by the daemon and consumed by the CLI's live
// speaker drain. The design mirrors the MONITOR TAP precedent (link.go
// monitorListener / monitorTap): same accept discipline (one retained
// conn, extras closed fast), same owner-only chmod, same drop-never-block
// write posture - a control write must NEVER gate the splice, the frame
// router, or teardown.
//
// PATH LENGTH: ".c.ev" is the same 5 bytes as ".c.in"/".c.out", so the
// ev path fits the 103-byte macOS sun_path limit exactly when the client
// pair (created first) did. The defensive cap below mirrors the monitor
// tap's 100-byte length check; a refusal here DEGRADES the call (no ev
// lane, old CLI-shaped behavior), never fails it.
//
// PROTOCOL: one JSON object per line, newline-delimited (the CLI reads
// it with a bufio.Scanner; the same shape as the C1 text plane):
//
//	{"event":"clear"}   - a barge-in latch happened: the client must drop
//	                       its buffered PCM and silence the live burst
//	                       (sink.Clear) NOW, not when the next byte lands.
//
// Future kinds are ALLOWED by design: a reader that ignores unknown
// "event" values keeps working (mirrors the C1 unknown-key tolerance).

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"
)

// ErrClientEventPathTooLong is the named refusal when a derived ev
// socket path would exceed the sun_path cap the monitor tap uses. Local
// leg call sites treat it as a degrade (warn + empty EvPath), never a
// call failure.
var ErrClientEventPathTooLong = errors.New("voice: client event socket path exceeds the unix sun_path limit")

// clientEventWriteDeadline bounds one ev write. SHORTER than the
// monitor tap's 250 ms because the emit site sits on the frame-router
// goroutine (VoiceWiring.routeFrame): a wedged CLI ev reader must steal
// at most 100 ms of C1 frame routing, per drop attempt.
const clientEventWriteDeadline = 100 * time.Millisecond

// ClientEvent is one control event on the daemon->client ev lane. The
// JSON key is byte-exact ("event"): the CLI's drain loop decodes it.
type ClientEvent struct {
	Event string `json:"event"`
}

// ClientEventClear names the barge-in clear event. It is the only kind
// today; readers must tolerate unknown kinds (the protocol is additive).
const ClientEventClear = "clear"

// ClientEventServer is the daemon side of one local call's ev listener.
// It reuses the monitorListener's accept/chmod/stale-file discipline
// (link.go) - the two lanes share every hard-won socket detail (stale
// file removal, one retained conn, extras closed, 0600).
//
// Concurrency: Emit is called from the frame router; Close from the
// leg's End. Both are safe here because the state machine rides the
// monitorListener's own mutex and Emit's own dead/fails atomics.
type ClientEventServer struct {
	ml    *monitorListener
	dead  atomic.Bool
	fails atomic.Uint32

	log *slog.Logger
}

// NewClientEventServer listens on path. The caller owns the lifecycle
// (Close at call teardown); a nil *slog.Logger falls back to slog.Default.
//
// The cap is the kernel's own sun_path limit (103 bytes usable on
// macOS - the same bar pairInPath/pairOutPath files must clear, see
// TestLocalLegOpen's len>=103 check), NOT the monitor tap's stricter
// 100-byte link-id cap: the ev suffix ".c.ev" is exactly as long as
// the client pair's ".c.in"/".c.out", so whenever the pair listened,
// the ev path fits by construction. Anything longer is refused with
// the named error so the LocalLeg can degrade instead.
func NewClientEventServer(path string, log *slog.Logger) (*ClientEventServer, error) {
	if len(path) >= 103 {
		return nil, fmt.Errorf("%w: %s", ErrClientEventPathTooLong, path)
	}
	if log == nil {
		log = slog.Default()
	}
	ml, err := newMonitorListener(path)
	if err != nil {
		return nil, err
	}
	return &ClientEventServer{ml: ml, log: log}, nil
}

// Emit writes one event line to the retained client conn. NEVER blocks
// the caller beyond clientEventWriteDeadline and NEVER returns an error:
//   - no client dialed yet: drop silently (the lane is live-only, like
//     the monitor tap - a CLI that dials late simply plays the old way,
//     which is exactly the pre-fix behavior, never a crash);
//   - write failure: drop THIS batch, disarm only after a run of
//     consecutive failures (a transient hiccup must not silence every
//     later clear; the CLI reader is trivial and a permanently dead
//     socket keeps failing and disarms via the threshold).
//
// Nil receiver is a no-op (every degraded call carries a nil server).
func (s *ClientEventServer) Emit(ev ClientEvent) {
	if s == nil || s.dead.Load() {
		return
	}
	conn := s.ml.current()
	if conn == nil {
		return
	}
	line, err := json.Marshal(ev)
	if err != nil {
		s.log.Warn("voice: client event encode failed", "err", err)
		return
	}
	line = append(line, '\n')
	_ = conn.SetWriteDeadline(time.Now().Add(clientEventWriteDeadline))
	if _, err := conn.Write(line); err != nil {
		if s.fails.Add(1) >= monitorFailThreshold {
			s.dead.Store(true)
			s.log.Debug("voice: client event lane disarmed after repeated write failures", "err", err)
		}
		return
	}
	s.fails.Store(0)
}

// Path returns the listener socket path (the value surfaced as the open
// RPC's ev_path).
func (s *ClientEventServer) Path() string {
	if s == nil {
		return ""
	}
	return s.ml.path
}

// Close tears the listener + accepted conn down and removes the socket
// file. Idempotent. Nil receiver is a no-op.
func (s *ClientEventServer) Close() {
	if s == nil {
		return
	}
	s.ml.close()
}

// Package-level registry of live ev servers by ccid, the same shape as
// localSpliceByCcid: the frame router (VoiceWiring.routeFrame) reaches
// one local call's control lane by ccid without a second manager seam.
// LocalLeg.Open registers, LocalLeg.End unregisters (and Closes).
var (
	clientEventsMu     sync.Mutex
	clientEventsByCcid = map[string]*ClientEventServer{}
)

// registerClientEvents installs st's ev server for ccid (LocalLeg.Open).
func registerClientEvents(ccid string, srv *ClientEventServer) {
	if srv == nil {
		return
	}
	clientEventsMu.Lock()
	clientEventsByCcid[ccid] = srv
	clientEventsMu.Unlock()
}

// unregisterClientEvents drops AND closes ccid's ev server (LocalLeg.End
// / refused Open). Idempotent; a degraded call (never registered) is a
// silent no-op.
func unregisterClientEvents(ccid string) {
	clientEventsMu.Lock()
	srv := clientEventsByCcid[ccid]
	delete(clientEventsByCcid, ccid)
	clientEventsMu.Unlock()
	srv.Close()
}

// ClientEventsFor returns the live ev server registered under ccid, or
// nil. VoiceWiring.routeFrame calls Emit(EventClear) on the result after
// a SUCCESSFUL LatchBargeIn transition (nil-safe by contract: phone legs
// and degraded local lanes never reach a non-nil server).
func ClientEventsFor(ccid string) *ClientEventServer {
	clientEventsMu.Lock()
	defer clientEventsMu.Unlock()
	return clientEventsByCcid[ccid]
}
