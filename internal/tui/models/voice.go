package models

// voice.go - the TUI voice session model (local-leg leaf 13a, T2/T3).
//
// /voice eve opens a local call for employee eve over the RPC control
// plane (voice.local.open / voice.local.end / voice.local.status, leaf
// 08) and the status line renders the live call + leg; /voice stop ends
// it; /voice list shows the both-legs table. NO AUDIO CODE LIVES HERE:
// internal/voice transitively imports internal/tui (activity -> rpc),
// so the concrete leaf 08 audio shim (voice.RawMicCapture /
// voice.SpeakerSink) cannot be constructed in this package. Instead the
// model takes a VoiceAudioDialer seam - the cmd/meept wiring injects a
// func that dials both pair sockets with the shim (the exact wiring
// cmd/meept/voice.go runVoiceLocalLive uses). A nil dialer is the
// control-only posture: open/end/status work with no audio attached
// (headless hosts, tests).
//
// Deviation note (leaf 13a T4): none - this file implements the spec
// verbatim; the dialer seam is the required consequence of the
// package cycle documented above. Spec: docs/plans/20261106-voice-local-leg/13a-tui-voice.md

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"
)

// VoiceAudioSession is one live audio attachment (both pair sockets
// dialed, mic + speaker running). Stop tears the shim down (capture
// closed, sink flushed and released); it must be safe to call once -
// the model calls it exactly once per session.
type VoiceAudioSession interface {
	Stop() error
}

// VoiceAudioDialer attaches the client audio shim to the open call's
// pair sockets. The daemon-side shape comes from voice.local.open
// (CL8): in_path carries the mic, out_path the speaker. cmd/meept
// builds the concrete dialer from voice.NewRawMicCapture +
// voice.NewSpeakerSink (leaf 08); tests inject fakes.
type VoiceAudioDialer func(inPath, outPath string) (VoiceAudioSession, error)

// voiceCallStatus mirrors the voice.local.status row shape (CL8,
// contact-display leaf 02 swap) for the /voice list table.
type voiceCallStatus struct {
	CallID        string `json:"call_id"`
	EmployeeID    string `json:"employee_id"`
	TargetName    string `json:"target_name"`
	TargetDisplay string `json:"target_display"`
	SinceRFC3339  string `json:"since_rfc3339"`
	Leg           string `json:"leg"`
	// Deprecated: the raw target. A contact-display daemon never fills
	// it for phone legs (O4) and local-leg rows carry their verbatim
	// "local:<label>" here too (leaf 02). Read ONLY as a render fallback
	// for old servers / local-leg labels — never for routing.
	Target string `json:"target"`
}

// targetDisplay picks the row's render string (contact-display leaf 03,
// master O1): the boundary-resolved display first, then the name, then
// the legacy raw target (non-empty = an old server or a local-leg
// label), else "unknown". NO client-side resolution or number
// formatting — this is pure field preference.
func (c voiceCallStatus) targetDisplay() string {
	if c.TargetDisplay != "" {
		return c.TargetDisplay
	}
	if c.TargetName != "" {
		return c.TargetName
	}
	if c.Target != "" {
		return c.Target
	}
	return "unknown"
}

// voiceStatusResult mirrors the voice.local.status envelope (CL8).
type voiceStatusResult struct {
	Calls []voiceCallStatus `json:"calls"`
}

// voiceOpenResult mirrors the voice.local.open result (CL8).
type voiceOpenResult struct {
	CallID    string `json:"call_id"`
	InPath    string `json:"in_path"`
	OutPath   string `json:"out_path"`
	SessionID string `json:"session_id"`
}

// VoiceSessionModel tracks the TUI's one local voice session. It is
// safe for concurrent use: RPC calls run on bubbletea command
// goroutines while the UI thread reads status.
type VoiceSessionModel struct {
	mu      sync.Mutex
	rpc     VoiceSessionRPC
	dialer  VoiceAudioDialer
	audio   VoiceAudioSession
	callID  string
	empID   string
	since   time.Time
	stopped bool
}

// VoiceSessionRPC is the RPC surface the model needs (satisfied by
// *tui.RPCClient; faked in tests).
type VoiceSessionRPC interface {
	Call(method string, params any) (json.RawMessage, error)
	IsConnected() bool
}

// NewVoiceSessionModel creates the model. A nil rpc is valid: every
// command reports the not-connected refusal (the same posture
// executeStatus uses for a nil client).
func NewVoiceSessionModel(rpc VoiceSessionRPC) *VoiceSessionModel {
	return &VoiceSessionModel{rpc: rpc}
}

// SetAudioDialer wires the audio shim dialer (leaf 08 reuse). Call
// once at construction time (App wiring); a nil dialer keeps the
// control-only posture.
func (m *VoiceSessionModel) SetAudioDialer(d VoiceAudioDialer) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.dialer = d
}

// Start opens a local session for employeeID over RPC and attaches the
// audio shim when the dialer is wired. An already-active session
// refuses (one live call per TUI). A dial failure rolls the open back
// with voice.local.end so the daemon never holds a ghost call. The RPC
// calls + JSON decode + audio attach are I/O and are done OUTSIDE the
// lock (mutexio rule): the needed handles (rpc, dialer) are snapshotted
// under the lock, the work runs unlocked, and committing the new call
// state re-takes the lock.
func (m *VoiceSessionModel) Start(employeeID, label string) error {
	m.mu.Lock()
	if m.callID != "" {
		m.mu.Unlock()
		return fmt.Errorf("voice: a session is already active (call %s); use /voice stop first", m.callID)
	}
	if employeeID == "" {
		m.mu.Unlock()
		return fmt.Errorf("voice: employee id is required (/voice eve)")
	}
	rpc := m.rpc
	dialer := m.dialer
	m.mu.Unlock()

	if rpc == nil || !rpc.IsConnected() {
		return fmt.Errorf("not connected to daemon")
	}
	req := map[string]any{"employee_id": employeeID}
	if label != "" {
		req["label"] = label
	}
	raw, err := rpc.Call("voice.local.open", req)
	if err != nil {
		return fmt.Errorf("voice.local.open: %w", err)
	}
	var open voiceOpenResult
	if err := json.Unmarshal(raw, &open); err != nil {
		// Roll the just-opened call back so the daemon never holds a
		// ghost call on a malformed envelope.
		_, _ = rpc.Call("voice.local.end", map[string]any{"call_id": open.CallID})
		return fmt.Errorf("voice.local.open: bad response: %w", err)
	}
	if open.CallID == "" {
		return fmt.Errorf("voice.local.open: daemon refused (no call id)")
	}
	// Audio attach (leaf 08 shim). Failure rolls the call back.
	var audio VoiceAudioSession
	if dialer != nil {
		audio, err = dialer(open.InPath, open.OutPath)
		if err != nil {
			_, _ = rpc.Call("voice.local.end", map[string]any{"call_id": open.CallID})
			return fmt.Errorf("voice: audio attach: %w", err)
		}
	}
	m.mu.Lock()
	m.audio = audio
	m.callID = open.CallID
	m.empID = employeeID
	m.since = time.Now()
	m.stopped = false
	m.mu.Unlock()
	return nil
}

// Stop ends the live session via voice.local.end and tears the audio
// shim down exactly once. Stopping an idle model is a no-op (the
// daemon's End is idempotent; the model does not even call it).
func (m *VoiceSessionModel) Stop() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.stopLocked()
}

// stopLocked is the shared teardown (caller holds mu).
func (m *VoiceSessionModel) stopLocked() error {
	if m.callID == "" {
		return nil
	}
	if !m.stopped {
		m.stopped = true
		if m.audio != nil {
			_ = m.audio.Stop()
			m.audio = nil
		}
	}
	callID := m.callID
	m.callID = ""
	m.empID = ""
	m.since = time.Time{}
	if m.rpc == nil || !m.rpc.IsConnected() {
		return nil
	}
	if _, err := m.rpc.Call("voice.local.end", map[string]any{"call_id": callID}); err != nil {
		// The daemon may have reaped the call already; surface the
		// refusal but keep the model clean.
		return fmt.Errorf("voice.local.end: %w", err)
	}
	return nil
}

// Teardown is the app-quit hook: ends a live call if one is active,
// no-op when idle.
func (m *VoiceSessionModel) Teardown() {
	m.mu.Lock()
	defer m.mu.Unlock()
	_ = m.stopLocked()
}

// Active reports whether a session is live.
func (m *VoiceSessionModel) Active() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callID != ""
}

// CallID returns the live call id ("" when idle).
func (m *VoiceSessionModel) CallID() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.callID
}

// Employee returns the live session's employee id ("" when idle).
func (m *VoiceSessionModel) Employee() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.empID
}

// StatusLine renders the status-bar token for a live session: the
// employee, the leg, and the call id, lowercase per the repo UI rule.
// Empty when idle (the status bar shows nothing extra).
func (m *VoiceSessionModel) StatusLine() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.callID == "" {
		return ""
	}
	return fmt.Sprintf("voice: %s (local, call %s)", m.empID, m.callID)
}

// List renders the /voice list table from the voice.local.status
// snapshot (both legs). The daemon's status handler never returns a
// nil calls list (CL8), and a nil fake still marshals to null which
// unmarshals to nil - both render the empty text.
func (m *VoiceSessionModel) List() (string, error) {
	if m.rpc == nil || !m.rpc.IsConnected() {
		return "", fmt.Errorf("not connected to daemon")
	}
	raw, err := m.rpc.Call("voice.local.status", map[string]any{})
	if err != nil {
		return "", fmt.Errorf("voice.local.status: %w", err)
	}
	var res voiceStatusResult
	if err := json.Unmarshal(raw, &res); err != nil {
		return "", fmt.Errorf("voice.local.status: bad response: %w", err)
	}
	if len(res.Calls) == 0 {
		return "no live voice calls.", nil
	}
	var b strings.Builder
	b.WriteString("leg      call       employee   target       since\n")
	b.WriteString("---------------------------------------------------\n")
	for _, c := range res.Calls {
		leg := c.Leg
		if leg == "" {
			leg = "phone"
		}
		since := c.SinceRFC3339
		if since == "" {
			since = "-"
		}
		fmt.Fprintf(&b, "%-8s %-10s %-10s %-12s %s\n", leg, c.CallID, c.EmployeeID, c.targetDisplay(), since)
	}
	return strings.TrimRight(b.String(), "\n"), nil
}
