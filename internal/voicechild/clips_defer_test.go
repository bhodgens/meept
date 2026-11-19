package voicechild

// TTS branch gate tests (plan 20261118-voice-tts-stt-optimizations,
// 03-tts-path/00T-tts-tests.md T1 + F10/T10 gate).
//
//   TestClipPrewarmDeferred — C1 ClipPrewarmDelayMs selector exists on the
//     keys frame (int, legacy 0 = immediate render); legacy frame still
//     renders a queued clip right away. The NEW symbol is probed by
//     reflection so this package keeps compiling today and the gate fails
//     RED on the assertion, not on a build error.
//
//   TestRenderYieldsToLive — F10: while a live synthesis is in flight the
//     background clip worker must NOT start another Speak; the spec is
//     re-requested on a later pass and renders then. Today's worker has no
//     yield check, so a second Speak starts and this test is RED.
//
// No implementation symbols are referenced directly. No secrets, no I/O
// under playMu (tests read playMu-guarded fields under the lock where
// applicable), ASCII only.

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// ttsKeysFieldWant is the exact C1 selector contract for the clip deferral
// knob: an int field on KeysFrame and an additive, optional wire tag on
// keysWire (absent/0 = legacy immediate render).
const (
	clipDelayField = "ClipPrewarmDelayMs"
	clipDelayTag   = "clip_prewarm_delay_ms"
)

// hasIntField reports whether typ has an exported int field of the given
// name. Kind-checking (not just existence) pins the "int" part of the
// contract so an impl that lands the wrong type still reads RED.
func hasIntField(typ reflect.Type, name string) bool {
	f, ok := typ.FieldByName(name)
	return ok && f.Type.Kind() == reflect.Int
}

// wireTagField reports whether typ has a field whose json tag's first
// component equals want (accepts an optional ",omitempty"). Returns the
// field's kind so the caller can require an int or *int selector.
func wireTagField(typ reflect.Type, want string) (reflect.Kind, bool) {
	for i := 0; i < typ.NumField(); i++ {
		tag := typ.Field(i).Tag.Get("json")
		name := strings.Split(tag, ",")[0]
		if name == want {
			return typ.Field(i).Type.Kind(), true
		}
	}
	return reflect.Invalid, false
}

func TestClipPrewarmDeferred(t *testing.T) {
	// Part 1 (RED today): the new C1 selector must exist on both the
	// in-memory KeysFrame and the wire decoder. Missing symbols are named
	// in the failure text; t.Errorf (not Fatal) lets every contract arm
	// report in one run.
	kf := reflect.TypeFor[KeysFrame]()
	if !hasIntField(kf, clipDelayField) {
		t.Errorf("missing contract C1: KeysFrame.%s int (0=legacy immediate, >0=defer ms after played edge)", clipDelayField)
	}
	kind, ok := wireTagField(reflect.TypeFor[keysWire](), clipDelayTag)
	if !ok || (kind != reflect.Int && kind != reflect.Ptr) {
		t.Errorf("missing contract C1: keysWire json tag %q (int/*int, optional, absent=legacy 0); got kind=%v present=%v",
			clipDelayTag, kind, ok)
	}

	// Part 2 (legacy pin, GREEN today and after): with a keys frame that
	// carries no deferral selector, a queued clip must render immediately
	// (delay 0 == pre-change behavior). This is the R4 byte-identical arm.
	const ccid = "clip-defer-legacy"
	d, _, childConn, p, _, _ := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)
	sendKeys(t, childConn, ccid) // minimal keys line: no clip_prewarm_delay_ms

	const key = "d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1d1"
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventClipRender,
		Clips: []voiceframe.ClipSpec{{Key: key, Text: "legacy immediate"}},
	})
	waitFor(t, func() bool { return fake.count() == 1 })
	_ = d // fixture owns the daemon; frames are not needed for this pin
}

// TestRenderYieldsToLive pins F10 end to end through the child's real
// clip worker: a background render must yield while a live say is in
// flight, and proceed on a later pass once the live synth clears.
func TestRenderYieldsToLive(t *testing.T) {
	const ccid = "clip-yield-live"
	d, _, childConn, p, _, _ := clipFixture(t, ccid)
	gate := newSerialGateTTS() // every Speak blocks until unblockCurrent
	swapTTS(p, gate)
	sendKeys(t, childConn, ccid)

	// Live turn: a last:false say enters Speak and blocks -> the pipeline
	// has a live synth job. The fixture's playback worker drives it.
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: "live answer", Last: new(false),
	})
	waitFor(t, func() bool { entered, _ := gate.snapshot(); return entered >= 1 })

	const key = "e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2e2"
	requestRender := func() {
		d.sendLine(childConn, voiceframe.Frame{
			CallID: ccid, Event: voiceframe.EventClipRender,
			Clips: []voiceframe.ClipSpec{{Key: key, Text: "background filler"}},
		})
	}

	// While live synth is in flight, the render worker must not start a
	// second Speak. Today it does (no yield check) -> entered becomes 2.
	requestRender()
	time.Sleep(250 * time.Millisecond)
	enteredLive, _ := gate.snapshot()
	if enteredLive != 1 {
		t.Errorf("F10 yield missing: background render started %d extra Speak(s) while a live synth was in flight (want 0 extra, entered still 1)", enteredLive-1)
	}

	// Release the live synth, then simulate the planner's next pass. The
	// deferred spec must render now. Errorf above keeps this arm runnable
	// so a RED today still exercises the path the impl must satisfy.
	gate.unblockCurrent()
	requestRender()
	waitFor(t, func() bool { entered, _ := gate.snapshot(); return entered >= 2 })
	gate.unblockCurrent() // let the render's Speak finish before cleanup
}
