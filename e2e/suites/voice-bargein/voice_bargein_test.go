//go:build e2e

// Suite voice-bargein (master Test Gate): interrupt mid-call; the clear
// frame is observed on THAT call's Telnyx media socket and the other
// concurrent call's socket is unaffected (per-call barge-in isolation,
// splice-owned per contract C1: RunSplice's bargeIn writes the clear frame
// on its own WS and touches only its own state).
//
// Reshaped from the parked leaf-00T sketch, which drove the interrupt
// through the child C1 stream into the talk loop. Both legs run today:
//   - the MEDIA-side leg (Telnyx sends {"event":"interrupt"} on the media
//     socket, the splice handles it itself) is pinned by
//     TestVoiceBargeinClearsOnlyInterruptedCall;
//   - the C1/talk-loop leg (child interrupt -> talk loop -> clear back on
//     the text socket) is pinned by TestVoiceBargeinViaTalkLoop, activatable
//     now that the inbound answer path Acquires its registry slot (leaf 15).
package voicebargein

import (
	"fmt"
	"testing"
	"time"
)

// TestVoiceBargeinClearsOnlyInterruptedCall runs two concurrent calls
// (both media splices armed) and interrupts call A at the media layer:
// A's Telnyx side receives {"event":"clear"} and call B's socket never
// does (per-splice state is structurally isolated).
func TestVoiceBargeinClearsOnlyInterruptedCall(t *testing.T) {
	fake := newFakeTelnyx(t)
	s, _ := bootVoiceStack(t, fake, map[string]any{
		"max_concurrent_calls": 4,
	})

	const ccidA = "cc-barge-a"

	// Call A live: accepted + splice armed. A second concurrent inbound
	// call for the SAME employee is refused busy (C8: one employee holds
	// one live call - pinned here; two-employee parallel calls need the
	// number->employee routing leaf and are unit-pinned by
	// TestRegistryLimits).
	fake.deliverCallInitiated(s, ccidA, "incoming")
	fake.awaitAnswered(ccidA, 10*time.Second)
	fake.dialMedia(s, ccidA)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)

	// Telnyx-side interrupt on A's media socket.
	fake.sendMediaFrame(ccidA, fmt.Sprintf(`{"event":"interrupt","call_id":%q}`, ccidA))

	// A's media socket observes the clear frame within the interval bound.
	fake.awaitMediaFrame(ccidA, `"event":"clear"`, 5*time.Second)

	// A second concurrent inbound call is refused busy (named body).
	status := fake.deliverCallInitiatedExpectStatus(s, "cc-barge-b", "incoming")
	if status < 400 {
		t.Fatalf("second concurrent inbound call for a busy employee must be refused, got %d", status)
	}
}

// TestVoiceBargeinViaTalkLoop pins the C1 leg of barge-in: the CHILD sends
// an interrupt frame, the talk loop drops the in-flight draft and writes a
// clear frame back on the text socket. This leg needed a live talk loop,
// which needed the inbound registry Acquire - landed with leaf 15, so the
// fake child's setup frame now arms the loop here. The MEDIA-side leg
// (Telnyx interrupt on the media socket -> splice clear to Telnyx) is
// pinned by TestVoiceBargeinClearsOnlyInterruptedCall above.
func TestVoiceBargeinViaTalkLoop(t *testing.T) {
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootVoiceStack(t, fake, map[string]any{
		"max_concurrent_calls": 4,
	})

	const ccid = "cc-barge-a"

	// Arm the call: answer + media start frame + child on C1, then the
	// child's setup frame registers the talk loop.
	fake.deliverCallInitiated(s, ccid, "incoming")
	fake.awaitAnswered(ccid, 10*time.Second)
	fake.dialMedia(s, ccid)
	awaitLogLine(t, s, "voice media: start frame", 15*time.Second)
	fake.connectChild(ctrlPath, 15*time.Second)
	fake.childDialAudio(ccid)
	fake.childSetup(ccid, "inbound")
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	// C1 interrupt -> talk-loop handleInterrupt -> clear back to the
	// child on the text socket, plus the loop's own barge-in log line.
	fake.childSendsInterrupt(ccid)
	fake.awaitClearFrame(ccid, 10*time.Second)
	awaitLogLine(t, s, "voice talk loop: barge-in", 10*time.Second)
}
