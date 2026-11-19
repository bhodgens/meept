package daemon

// voice_timing_override_test.go - per-call timing override store + its
// application to the C1 keys frame. Overrides are per-call, mutex-guarded,
// registered before the open frame is sent (the child answers open with
// setup, which triggers keys emission — registering after leg.Open returns
// races the setup frame), and deleted on release so nothing leaks into a
// later call. Absent overrides leave the keys wire byte-identical.

import (
	"encoding/json"
	"testing"

	"github.com/caimlas/meept/internal/voice"
)

func TestCallTimingOverridesStore(t *testing.T) {
	w := &VoiceWiring{}
	tc, stm, dg := 777, 1, 333
	o := &callTimingOverrides{TurnConfirmMs: &tc, SmartTurnMode: &stm, DeepgramEndpointMs: &dg}

	if got := w.callTimingOverride("cc-1"); got != nil {
		t.Fatalf("fresh store: want nil, got %+v", got)
	}
	w.setCallTimingOverride("cc-1", o)
	got := w.callTimingOverride("cc-1")
	if got == nil || got.TurnConfirmMs == nil || *got.TurnConfirmMs != tc ||
		got.SmartTurnMode == nil || *got.SmartTurnMode != stm ||
		got.DeepgramEndpointMs == nil || *got.DeepgramEndpointMs != dg {
		t.Fatalf("stored override mismatch: %+v", got)
	}
	// A second call is isolated.
	w.setCallTimingOverride("cc-2", &callTimingOverrides{})
	if g2 := w.callTimingOverride("cc-2"); g2 == nil || g2.TurnConfirmMs != nil {
		t.Fatalf("call isolation failed: %+v", g2)
	}
	if g1 := w.callTimingOverride("cc-1"); g1 == nil || g1.TurnConfirmMs == nil || *g1.TurnConfirmMs != tc {
		t.Fatalf("cc-1 clobbered by cc-2: %+v", g1)
	}
	// Delete is idempotent and scoped to one call.
	w.deleteCallTimingOverride("cc-1")
	if got := w.callTimingOverride("cc-1"); got != nil {
		t.Fatalf("after delete: want nil, got %+v", got)
	}
	w.deleteCallTimingOverride("cc-1") // idempotent
	if g2 := w.callTimingOverride("cc-2"); g2 == nil {
		t.Fatal("cc-2 lost on cc-1 delete")
	}
}

// TestVoiceKeysEmissionTimingOverride drives a real keys emission through
// routeFrame/EventSetup with a registered override and checks the wire.
func TestVoiceKeysEmissionTimingOverride(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-override"
	acquireFor(f.w, ccid)
	tc, stm, dg := 1111, 2, 444
	f.w.setCallTimingOverride(f.w.cfg.DefaultEmployee, &callTimingOverrides{
		TurnConfirmMs: &tc, SmartTurnMode: &stm, DeepgramEndpointMs: &dg,
	})

	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line)
	}
	for key, want := range map[string]float64{
		"turn_confirm_ms":      float64(tc),
		"smart_turn_mode":      float64(stm),
		"deepgram_endpoint_ms": float64(dg),
	} {
		if got, ok := wire[key].(float64); !ok || got != want {
			t.Errorf("override wire %s = %v, want %v", key, wire[key], want)
		}
	}
	t.Cleanup(func() { f.w.releaseCall(ccid, voice.OutcomeHangup) })
}

// TestVoiceKeysEmissionNoOverrideUnchanged pins the default: with no
// override registered the wire keeps config values (zero for a hand-built
// fixture), byte-identical to pre-change.
func TestVoiceKeysEmissionNoOverrideUnchanged(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-no-override"
	acquireFor(f.w, ccid)
	f.w.cfg.TurnConfirmMs = 900
	f.w.cfg.SmartTurnMode = 0

	f.w.routeFrame(voice.Frame{CallID: ccid, Event: voice.EventSetup})
	line := readKeysLine(t, f.lines)
	var wire map[string]any
	if err := json.Unmarshal([]byte(line), &wire); err != nil {
		t.Fatalf("keys line not JSON: %v: %s", err, line)
	}
	if got, _ := wire["turn_confirm_ms"].(float64); got != 900 {
		t.Errorf("turn_confirm_ms = %v, want 900 (config value, no override)", got)
	}
	if got, present := wire["smart_turn_mode"]; present && got != float64(0) {
		t.Errorf("smart_turn_mode = %v, want 0 (config value, no override)", got)
	}
	t.Cleanup(func() { f.w.releaseCall(ccid, voice.OutcomeHangup) })
}

// TestReleaseDeletesTimingOverride ensures the store does not leak past a
// call's end (a later call with the same ccid never inherits an override).
func TestReleaseDeletesTimingOverride(t *testing.T) {
	f := newKeysEmitFixture(t)
	const ccid = "cc-keys-release"
	acquireFor(f.w, ccid)
	tc := 999
	f.w.setCallTimingOverride(f.w.cfg.DefaultEmployee, &callTimingOverrides{TurnConfirmMs: &tc})
	if got := f.w.callTimingOverride(f.w.cfg.DefaultEmployee); got == nil {
		t.Fatal("override not registered")
	}
	f.w.releaseCall(ccid, voice.OutcomeHangup)
	if got := f.w.callTimingOverride(f.w.cfg.DefaultEmployee); got != nil {
		t.Fatalf("override leaked past release: %+v", got)
	}
}
