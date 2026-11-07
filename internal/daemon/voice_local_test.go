package daemon

// Leaf 07 daemon-glue tests (local-leg plan docs/plans/20261106-voice-
// local-leg/07-local-leg.md T5/T6): WireVoice constructs a LocalLeg when
// cfg.Local.Enabled (nil otherwise), and the leg's Open routes the day
// brief with direction "local" / number "local:<label>" through the
// binding's OnCallStart - the same call-record row shape a phone call
// writes.

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// TestWireVoiceLocalLegNilWhenDisabled pins T6: without voice.local.enabled
// the wiring carries NO LocalLeg (leaf 08 refuses opens on nil).
func TestWireVoiceLocalLegNilWhenDisabled(t *testing.T) {
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{Enabled: false}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	defer w.Employee.Close()
	if w.Local != nil {
		t.Fatal("Local leg constructed with voice.local.enabled=false; want nil")
	}
}

// TestWireVoiceLocalLegConstructedWhenEnabled pins T6: with
// voice.local.enabled the wiring exposes a usable LocalLeg whose Live()
// starts empty, wired onto the SHARED registry, manager, and child.
func TestWireVoiceLocalLegConstructedWhenEnabled(t *testing.T) {
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{
		Enabled:     true,
		STTProvider: "whisper",
		TTSProvider: "piper",
		TTSModel:    "en_US-test-medium",
		TTSVoice:    "test",
		BargeInRms:  3000,
		Name:        "wirebox",
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	if w == nil {
		t.Fatal("WireVoice returned nil wiring")
	}
	defer func() { w.Employee.Close() }()
	if w.Local == nil {
		t.Fatal("Local leg nil with voice.local.enabled=true")
	}
	if live := w.Local.Live(); len(live) != 0 {
		t.Errorf("fresh leg Live() = %v, want empty", live)
	}
}

// TestWireVoiceLocalLegBrief pins T5: a local Open runs the day brief +
// binding with direction "local" and number "local:<label>" - the call
// row in the call store carries direction=local and the local: target,
// byte-shape identical to a phone call row.
func TestWireVoiceLocalLegBrief(t *testing.T) {
	t.Setenv("MEEPT_HOME", shortMeeptHomeForTest(t))
	deps := wireVoiceTestDeps(t, "")
	deps.Config.Voice.Local = config.LocalConfig{
		Enabled:     true,
		STTProvider: "whisper",
		TTSProvider: "piper",
		BargeInRms:  3000,
		Name:        "briefbox",
	}
	w, err := WireVoice(context.Background(), deps)
	if err != nil {
		t.Fatalf("WireVoice: %v", err)
	}
	defer func() { w.Employee.Close() }()

	call, err := w.Local.Open(context.Background(), "emp-local-brief", "briefbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if call.SessionID == "" {
		t.Fatal("Open returned an empty SessionID (binding.OnCallStart did not resolve a session)")
	}

	// The binding row: today's transcript for the local number exists and
	// the call store resolves the session for (local:briefbox, employee).
	sid, ok := w.Employee.store.SessionIDFor("local:briefbox", "emp-local-brief")
	if !ok || sid == "" {
		t.Fatal("call store has no session for (local:briefbox, emp-local-brief); OnCallStart did not record the row with the local number")
	}
	if sid != call.SessionID {
		t.Errorf("store session %q != LocalCall.SessionID %q", sid, call.SessionID)
	}

	// TodayTranscripts for the local target proves the row's number
	// shape: a phone-shaped number would NOT match "local:briefbox".
	transcripts, err := w.Employee.store.TodayTranscripts(context.Background(), "local:briefbox")
	if err != nil {
		t.Fatalf("TodayTranscripts: %v", err)
	}
	if len(transcripts) != 1 {
		t.Fatalf("transcripts for local:briefbox = %d, want 1 (the call row carries the local: number)", len(transcripts))
	}
	if !strings.HasPrefix(transcripts[0].Number, "local:") {
		t.Errorf("transcript number = %q, want the local: prefix (direction local, number local:<label>)", transcripts[0].Number)
	}

	// Terminal fan-out: End closes the row (outcome recorded) and frees
	// the registry slot.
	if err := w.Local.End(call.CallID, "brief test done"); err != nil {
		t.Errorf("End: %v", err)
	}
	for _, inst := range w.Registry.Live() {
		if inst.CallControlID == call.CallID {
			t.Errorf("registry still holds %s after End", call.CallID)
		}
	}
}
