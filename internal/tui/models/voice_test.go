package models

// voice_test.go - leaf 13a T2/T3 tests for the TUI voice session model.
// Everything runs against fakes: no daemon, no RPC socket, no audio
// device (the audio shim rides the VoiceAudioDialer seam behind a fake).

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

// voiceRPCCall records one fake RPC invocation.
type voiceRPCCall struct {
	method string
	params map[string]any
}

// fakeVoiceRPC is the fake RPC seam (T2: "fake RPC" per the spec's
// Verify column). It records every Call and answers the three
// voice.local.* methods from canned fields.
type fakeVoiceRPC struct {
	connected  bool
	calls      []voiceRPCCall
	openResult map[string]any
	openErr    error
	endErr     error
	statusRows []map[string]any
}

func (f *fakeVoiceRPC) Call(method string, params any) (json.RawMessage, error) {
	p, _ := params.(map[string]any)
	f.calls = append(f.calls, voiceRPCCall{method: method, params: p})
	switch method {
	case "voice.local.open":
		if f.openErr != nil {
			return nil, f.openErr
		}
		return json.Marshal(f.openResult)
	case "voice.local.end":
		if f.endErr != nil {
			return nil, f.endErr
		}
		return json.Marshal(map[string]any{"ok": true})
	case "voice.local.status":
		return json.Marshal(map[string]any{"calls": f.statusRows})
	}
	return nil, errors.New("fakeVoiceRPC: unexpected method " + method)
}

func (f *fakeVoiceRPC) IsConnected() bool { return f.connected }

// fakeVoiceAudio records the audio shim lifecycle (T3: start/stop
// behind a fake - no real capture/sink in tests).
type fakeVoiceAudio struct {
	stopCalls int
}

func (a *fakeVoiceAudio) Stop() error {
	a.stopCalls++
	return nil
}

// newOpenFake returns a fake RPC whose open answers the canonical CL8
// shape for employee eve.
func newOpenFake() *fakeVoiceRPC {
	return &fakeVoiceRPC{
		connected: true,
		openResult: map[string]any{
			"call_id":    "call-1",
			"in_path":    "/tmp/meept-test/in.sock",
			"out_path":   "/tmp/meept-test/out.sock",
			"session_id": "sess-1",
		},
	}
}

// T2: Start opens the call over RPC with the employee id.
func TestVoiceModelStartOpensRPC(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	if err := m.Start("eve", ""); err != nil {
		t.Fatalf("Start(eve) = %v, want nil", err)
	}
	if len(fake.calls) != 1 {
		t.Fatalf("rpc calls = %d, want 1", len(fake.calls))
	}
	c := fake.calls[0]
	if c.method != "voice.local.open" {
		t.Fatalf("first call method = %q, want voice.local.open", c.method)
	}
	if c.params["employee_id"] != "eve" {
		t.Fatalf("employee_id = %v, want eve", c.params["employee_id"])
	}
	if !m.Active() {
		t.Fatal("model not active after successful open")
	}
	if m.CallID() != "call-1" {
		t.Fatalf("CallID = %q, want call-1", m.CallID())
	}
	if m.Employee() != "eve" {
		t.Fatalf("Employee = %q, want eve", m.Employee())
	}
}

// T2: the status line shows the live call + leg.
func TestVoiceModelStatusLineShowsCallAndLeg(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	if err := m.Start("eve", ""); err != nil {
		t.Fatalf("Start(eve) = %v", err)
	}
	line := m.StatusLine()
	if !strings.Contains(line, "eve") {
		t.Fatalf("status line %q missing employee eve", line)
	}
	if !strings.Contains(line, "local") {
		t.Fatalf("status line %q missing leg local", line)
	}
	if !strings.Contains(line, "call-1") {
		t.Fatalf("status line %q missing call id", line)
	}
}

// T2: a start refusal (unknown employee) leaves no session state.
func TestVoiceModelStartRefused(t *testing.T) {
	fake := newOpenFake()
	fake.openErr = errors.New("voice: unknown employee: nobody")
	m := NewVoiceSessionModel(fake)
	if err := m.Start("nobody", ""); err == nil {
		t.Fatal("Start(nobody) = nil, want error")
	}
	if m.Active() {
		t.Fatal("model active after refused open")
	}
	if m.CallID() != "" {
		t.Fatalf("CallID = %q after refused open, want empty", m.CallID())
	}
}

// T2: a second start while a call is live is refused.
func TestVoiceModelStartWhileActiveRefused(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	if err := m.Start("eve", ""); err != nil {
		t.Fatalf("first Start = %v", err)
	}
	if err := m.Start("adam", ""); err == nil {
		t.Fatal("second Start = nil, want error")
	}
	if m.Employee() != "eve" {
		t.Fatalf("Employee = %q, want eve (state unchanged)", m.Employee())
	}
	if len(fake.calls) != 1 {
		t.Fatalf("rpc calls = %d, want 1 (no second open)", len(fake.calls))
	}
}

// T2: Stop ends the call via RPC and is idempotent.
func TestVoiceModelStopEndsRPC(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	if err := m.Start("eve", ""); err != nil {
		t.Fatalf("Start(eve) = %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop = %v, want nil", err)
	}
	if m.Active() {
		t.Fatal("model still active after Stop")
	}
	if m.StatusLine() != "" {
		t.Fatalf("status line = %q after Stop, want empty", m.StatusLine())
	}
	if len(fake.calls) != 2 {
		t.Fatalf("rpc calls = %d, want 2 (open, end)", len(fake.calls))
	}
	end := fake.calls[1]
	if end.method != "voice.local.end" {
		t.Fatalf("second call method = %q, want voice.local.end", end.method)
	}
	if end.params["call_id"] != "call-1" {
		t.Fatalf("end call_id = %v, want call-1", end.params["call_id"])
	}
	// Double Stop is the idempotent shape: no new RPC.
	if err := m.Stop(); err != nil {
		t.Fatalf("double Stop = %v, want nil", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("rpc calls after double Stop = %d, want 2", len(fake.calls))
	}
}

// T2: teardown on app quit ends a live call and does nothing when idle.
func TestVoiceModelTeardownOnQuit(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	m.Teardown() // idle: must be a no-op with zero RPC
	if len(fake.calls) != 0 {
		t.Fatalf("idle teardown made %d rpc calls, want 0", len(fake.calls))
	}
	if err := m.Start("eve", ""); err != nil {
		t.Fatalf("Start(eve) = %v", err)
	}
	m.Teardown()
	if m.Active() {
		t.Fatal("model active after Teardown")
	}
	if len(fake.calls) != 2 {
		t.Fatalf("rpc calls after teardown = %d, want 2", len(fake.calls))
	}
}

// T2/T6: /voice list renders the both-legs status table from the
// voice.local.status snapshot.
func TestVoiceModelListRendersTable(t *testing.T) {
	fake := &fakeVoiceRPC{
		connected: true,
		statusRows: []map[string]any{
			{
				"call_id":       "call-1",
				"employee_id":   "eve",
				"since_rfc3339": "2026-11-07T10:00:00Z",
				"leg":           "local",
				// contact-display leaf 03 (O1): an old-server fixture
				// carries only the raw target — the renderer's legacy
				// fallback must still print it (local-leg labels).
				"target": "eve",
			},
			{
				"call_id":       "call-2",
				"employee_id":   "adam",
				"since_rfc3339": "2026-11-07T10:05:00Z",
				"leg":           "phone",
				// contact-display leaf 03: the leaf 02 wire shape —
				// the display pair renders, no raw target on the row.
				"target_name":    "Jane Doe",
				"target_display": "Jane Doe (•••5567)",
			},
			{
				"call_id":       "call-3",
				"employee_id":   "zoe",
				"since_rfc3339": "2026-11-07T10:10:00Z",
				"leg":           "phone",
				// contact-display leaf 03: a row with NO target info
				// at all renders the "unknown" fallback.
			},
		},
	}
	m := NewVoiceSessionModel(fake)
	out, err := m.List()
	if err != nil {
		t.Fatalf("List = %v", err)
	}
	if !strings.Contains(out, "eve") || !strings.Contains(out, "call-1") {
		t.Fatalf("list output missing local row:\n%s", out)
	}
	// leaf 03: the display form renders (name + last-4), never a raw
	// number (the fixture has none to leak).
	if !strings.Contains(out, "Jane Doe (•••5567)") {
		t.Fatalf("list output missing display pair on phone row:\n%s", out)
	}
	if !strings.Contains(out, "adam") || !strings.Contains(out, "phone") {
		t.Fatalf("list output missing phone row:\n%s", out)
	}
	if !strings.Contains(out, "local") {
		t.Fatalf("list output missing leg column value:\n%s", out)
	}
	if !strings.Contains(out, "unknown") {
		t.Fatalf("list output missing unknown fallback for infoless row:\n%s", out)
	}
}

// T2: an empty status snapshot renders the lowercase empty text.
func TestVoiceModelListEmpty(t *testing.T) {
	fake := &fakeVoiceRPC{connected: true, statusRows: []map[string]any{}}
	m := NewVoiceSessionModel(fake)
	out, err := m.List()
	if err != nil {
		t.Fatalf("List = %v", err)
	}
	if !strings.Contains(out, "no live voice calls") {
		t.Fatalf("empty list output = %q, want the no-calls text", out)
	}
}

// T3: the audio shim rides the dialer seam - Start dials with the open
// result's pair paths, Stop tears the shim down exactly once.
func TestVoiceModelAudioDialerLifecycle(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	audio := &fakeVoiceAudio{}
	var dialedIn, dialedOut string
	m.SetAudioDialer(func(inPath, outPath string) (VoiceAudioSession, error) {
		dialedIn = inPath
		dialedOut = outPath
		return audio, nil
	})
	if err := m.Start("eve", ""); err != nil {
		t.Fatalf("Start(eve) with dialer = %v", err)
	}
	if dialedIn != "/tmp/meept-test/in.sock" || dialedOut != "/tmp/meept-test/out.sock" {
		t.Fatalf("dialer got (%q, %q), want the open result pair paths", dialedIn, dialedOut)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if audio.stopCalls != 1 {
		t.Fatalf("audio stop calls = %d, want 1", audio.stopCalls)
	}
	// Double Stop must not re-stop the audio shim either.
	if err := m.Stop(); err != nil {
		t.Fatalf("double Stop = %v", err)
	}
	if audio.stopCalls != 1 {
		t.Fatalf("audio stop calls after double Stop = %d, want 1", audio.stopCalls)
	}
}

// T3: a failed dial leaves no session (the open is rolled back with a
// voice.local.end so the daemon never holds a ghost call).
func TestVoiceModelAudioDialFailureRollsBack(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	m.SetAudioDialer(func(inPath, outPath string) (VoiceAudioSession, error) {
		return nil, errors.New("voice: mic capture needs ffmpeg on PATH")
	})
	if err := m.Start("eve", ""); err == nil {
		t.Fatal("Start with failing dialer = nil, want error")
	}
	if m.Active() {
		t.Fatal("model active after dial failure")
	}
	if len(fake.calls) != 2 {
		t.Fatalf("rpc calls = %d, want 2 (open + rollback end)", len(fake.calls))
	}
	if fake.calls[1].method != "voice.local.end" {
		t.Fatalf("rollback method = %q, want voice.local.end", fake.calls[1].method)
	}
}

// T3: a nil dialer is the control-only posture - the session opens and
// stops cleanly with no audio attached (headless smoke).
func TestVoiceModelNilDialerControlOnly(t *testing.T) {
	fake := newOpenFake()
	m := NewVoiceSessionModel(fake)
	if err := m.Start("eve", ""); err != nil {
		t.Fatalf("Start(eve) with nil dialer = %v", err)
	}
	if err := m.Stop(); err != nil {
		t.Fatalf("Stop = %v", err)
	}
	if len(fake.calls) != 2 {
		t.Fatalf("rpc calls = %d, want 2", len(fake.calls))
	}
}
