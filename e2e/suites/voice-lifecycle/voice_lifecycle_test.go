//go:build e2e

// Suite voice-lifecycle (master Test Gate): the inbound call lifecycle
// against a scratch daemon with a FAKE Telnyx (signed webhooks + call-
// control API + media websocket client) and a FAKE child speaking C1/C2
// over the real unix sockets. The inbound registry Acquire the C1 setup
// router needs landed with leaf 15 (internal/daemon/voice_wiring.go spawn
// seam), so the hermetic loop now runs end to end: answer -> media start
// frame -> child setup -> talk loop armed -> greeting say -> C1 interrupt
// -> clear -> hangup -> call ended -> idle stop. No named skips remain:
// the last one (greeting AUDIO reaching the Telnyx media socket through
// the splice) was retired by voice-tierA leaf 07, whose
// TestVoiceKeysToProviders drives the REAL qwen3 TTS adapter (constructed
// from the daemon's keys line) and asserts decoded audio frames on the
// media socket; the local leg's client-audio path is pinned hermetically
// by the voice-local suite (local-leg leaf 11).
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
// down exactly once, and the idle child stops. Audio pacing onto the
// Telnyx socket is pinned LIVE by TestVoiceKeysToProviders
// (keys_providers_test.go, voice-tierA leaf 07).
func TestVoiceLifecycleInboundCall(t *testing.T) {
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootVoiceStack(t, fake, nil)

	const ccid = "cc-life-1"

	fake.deliverCallInitiated(s, ccid, "incoming")
	fake.awaitAnswered(ccid, 10*time.Second)

	// The manager answered the call at the fake call-control API.
	fake.awaitAction(ccid, "/answer", 10*time.Second)

	// voice-tierA leaf 07: the fake child now answers the daemon's open
	// frame with setup (mirroring the real child's handleOpen), so the
	// talk loop arms and the greeting say flows as soon as the media
	// splice starts. One throwaway call first spawns the child so the
	// ctrl mirror can attach before the primary call's greeting say
	// arrives (a late-attaching observer misses the mirrored frames).
	const warmCcid = "cc-life-warm"
	fake.dialMedia(s, warmCcid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
	fake.deliverHangup(s, warmCcid)
	fake.connectChild(ctrlPath, 15*time.Second)
	fake.dialMedia(s, ccid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
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

// TestVoiceLifecycleGreetingAudioAtTelnyx was RETIRED by voice-tierA leaf
// 07: the greeting-audio gap it documented (the splice pacing REAL PCM
// onto the Telnyx media socket needs the child's real TTS stage) is now
// covered LIVE by TestVoiceKeysToProviders (keys_providers_test.go),
// which synthesizes through the REAL qwen3 adapter constructed from the
// daemon's keys line and asserts decoded audio frames on the media
// socket. The named skip is gone because the gap is.
