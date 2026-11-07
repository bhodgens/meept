//go:build e2e

// Suite voice-lifecycle (master Test Gate): the inbound call lifecycle
// against a scratch daemon with a FAKE Telnyx (signed webhooks + call-
// control API + media websocket client) and a FAKE child speaking C1/C2
// over the real unix sockets. The inbound registry Acquire the C1 setup
// router needs landed with leaf 15 (internal/daemon/voice_wiring.go spawn
// seam), so the hermetic loop now runs end to end: answer -> media start
// frame -> child setup -> talk loop armed -> greeting say -> C1 interrupt
// -> clear -> hangup -> call ended -> idle stop. Still a named skip:
// greeting AUDIO pacing onto the Telnyx media socket (needs the child's
// real TTS stage; the fake child speaks no PCM).
//
// Activated from the parked leaf-00T sketch; assertions were reshaped to
// the LANDED leaf-14 contracts (the parked guesses predated the wiring):
//   - voice.telnyx_base_url does not exist; the daemon takes the Telnyx
//     API base from MEEPT_TELNYX_TEST_BASE_URL (hermetic override) and the
//     API key from the secrets broker.
//   - the C1 setup frame is a child->daemon production per contract C1;
//     the fake child sends it (the daemon only consumes it).
//   - "hangup answered 200 then child idle-stops" is asserted via the
//     child control connection closing (the real observable), not a pid.
//
// What each test PROVES (honest scope):
//
//	TestVoiceLifecycleInboundCall: webhook acceptance, manager answer
//	path, C1 say delivery to the child, final-prompt -> employee turn ->
//	say round trip, hangup teardown, idle stop. Media-splice wiring is
//	exercised only as far as the WS dial + start frame (audio assertions
//	are the splice unit tests' job; no ONNX/jargo needed).
//	TestVoiceLifecycleFullLoopSkipped: documents the not-yet-wirable
//	residue (greeting audio reaching Telnyx through the splice) - split
//	out with a named skip per the leaf's split rule.
package voicelifecycle

import (
	"os"
	"testing"
	"time"
)

// TestVoiceLifecycleInboundCall runs the hermetic inbound loop end to end:
// the signed webhook is accepted, the manager answers at the call-control
// API, the media start frame is consumed, the fake child's C1 setup frame
// ARMS the talk loop (leaf-15 inbound Acquire makes the registry lookup
// succeed), the greeting say reaches the child, a final prompt produces
// another say, a C1 interrupt produces a clear, the hangup tears the call
// down once, and the idle child stops. Audio pacing onto the Telnyx socket
// is still the splice unit tests' job (see the named skip below).
func TestVoiceLifecycleInboundCall(t *testing.T) {
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootVoiceStack(t, fake, nil)

	const ccid = "cc-life-1"

	fake.deliverCallInitiated(s, ccid, "incoming")
	fake.awaitAnswered(ccid, 10*time.Second)

	// The manager answered the call at the fake call-control API.
	fake.awaitAction(ccid, "/answer", 10*time.Second)

	// Fake Telnyx dials the media route; the splice consumes the start
	// frame. The answer path spawned the fake child, so its control
	// socket now exists and the suite can drive C1.
	fake.dialMedia(s, ccid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
	fake.connectChild(ctrlPath, 15*time.Second)
	fake.childDialAudio(ccid)

	// C1 setup (a child->daemon production) arms the talk loop: the
	// registry instance the inbound spawner Acquired resolves, and the
	// greeting say (filler-class, never supervisor-gated) lands.
	fake.childSetup(ccid, "inbound")
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)
	fake.awaitSayContaining(ccid, 15*time.Second)

	// A final transcript becomes a prompt: the loop answers it with the
	// filler say at once (fact-free) and starts the employee turn.
	baseline := fake.sayFrameCount(ccid)
	fake.childSendsPrompt(ccid, "is the shipment still on friday", true)
	fake.awaitSayAdvance(ccid, baseline, 15*time.Second)

	// Barge-in over C1: the talk loop drops the in-flight draft and
	// clears THIS call on the text socket (the child flushes its TTS
	// queue). Per-call isolation is pinned by voice-bargein.
	fake.childSendsInterrupt(ccid)
	fake.awaitClearFrame(ccid, 10*time.Second)
	awaitLogLine(t, s, "voice talk loop: barge-in", 10*time.Second)

	// Hangup tears the call down exactly once and the idle child stops.
	fake.deliverHangup(s, ccid)
	awaitLogLine(t, s, "voice call ended", 15*time.Second)
	fake.awaitChildStop(15 * time.Second)
}

// TestVoiceWebhookAcceptedAndMediaArmed pins the runnable hermetic half of
// the inbound flow: the signed call.initiated webhook is accepted (200),
// the fake Telnyx media websocket dials IN and its start frame is consumed
// by the splice (the log names call_control_id + encoding), and the daemon
// answers the call-control API at the fake base URL.
func TestVoiceWebhookAcceptedAndMediaArmed(t *testing.T) {
	fake := newFakeTelnyx(t)
	s, _ := bootVoiceStack(t, fake, nil)

	const ccid = "cc-life-1"

	fake.deliverCallInitiated(s, ccid, "incoming")
	fake.awaitAnswered(ccid, 10*time.Second)

	// The manager answered the call at the fake call-control API.
	fake.awaitAction(ccid, "/answer", 10*time.Second)

	// Fake Telnyx dials the media route; the splice consumes the start
	// frame (observable in the daemon log: call_control_id + PCMU).
	fake.dialMedia(s, ccid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)

	fake.deliverHangup(s, ccid)
}

// TestVoiceChildIdleStopNotSpawnedAtBoot pins the lazy child posture: a
// booted, armed daemon with zero calls has NOT spawned the fake child
// (the control socket never appears until a call forces EnsureRunning).
func TestVoiceChildIdleStopNotSpawnedAtBoot(t *testing.T) {
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootVoiceStack(t, fake, nil)

	// No call: after a bounded watch window the control socket must still
	// be absent. Bounded poll ending in an assertion, not a sleep-for-timing.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ctrlPath); err == nil {
			t.Fatalf("fake child control socket %s exists with zero calls; child spawned eagerly", ctrlPath)
		}
		time.Sleep(50 * time.Millisecond)
	}
	_ = s
}

// TestVoiceLifecycleGreetingAudioAtTelnyx documents the split: the
// greeting SAY reaching the child is asserted above; the audio the splice
// paces onto the Telnyx media socket needs the child's REAL TTS stage
// (leaf-08 providers) to synthesize PCM on the .out socket - the fake
// child holds the socket open but speaks no audio, so a mid-call audio
// assertion would prove nothing. Skipped with the named gap per the leaf
// split rule.
func TestVoiceLifecycleGreetingAudioAtTelnyx(t *testing.T) {
	t.Skipf("not yet wirable hermetically: greeting AUDIO on the Telnyx media socket requires the child's real TTS stage (leaf-08 providers); the fake child holds the C2 pair open but synthesizes no PCM - the splice outbound path is pinned by internal/voice splice unit tests instead")
}
