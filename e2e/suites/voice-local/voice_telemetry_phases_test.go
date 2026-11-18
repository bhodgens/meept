//go:build e2e

package voicelocal

// voice_telemetry_phases_test.go — suite voice-local, per-phase telemetry:
// the three additive event kinds (stt_final / model_final / voice_play) proven
// hermetically end to end over the real voice.local.events RPC. A real local
// call's STT final, employee reply, and first spoken sentence each surface
// with FULL text and a non-negative ms, in addition to the original
// heard / prompt_dispatch / turn_final lifecycle kinds (pinned by
// voice_events_test.go). Extends the suite rather than adding a new package.

import (
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

func TestVoiceLocalPerPhaseTelemetry(t *testing.T) {
	_, modelDir := shimPath(t)
	modelPath := writePiperModel(t, modelDir)
	fake := newFakeTelnyx(t)
	s, ctrlPath := bootLocalStack(t, fake, modelPath)

	const (
		employee = "emp-telemetry-phases"
		label    = "phasesbox"
	)
	rpcCreateEmployee(t, s, employee)

	const (
		transcript = "local utterance one" // the whisper shim's fixed final
		reply      = "PIPERMARKER phase telemetry reply for the phases suite."
	)
	s.Fake.Script(
		harness.And(harness.IsExecutorRequest(), harness.LastUserMessageContains(transcript)),
		harness.TextResponse(reply),
	)

	const warmCcid = "cc-phases-warm"
	spawnChildViaPhoneWarmCall(t, s, fake, warmCcid)
	fake.connectChild(ctrlPath, 15*time.Second)

	open := rpcOpenLocalCall(t, s, employee, label)
	ccid, _ := open["call_id"].(string)
	awaitLogLine(t, s, "voice wiring: talk loop armed", 15*time.Second)

	eventsClient := harness.DialRPC(t, s.SocketPath)
	acc := newEventAccum(t, eventsClient, ccid)

	// Drive one real turn: client mic PCM through the whisper shim ->
	// last:true prompt -> talk loop dispatch -> fake LLM reply -> say.
	client := dialLocalClient(t, open)
	go client.speakMic()
	fake.childCommand(ctrlMsg{Ctrl: "speak-mic", CallID: ccid, Note: scriptedUtteranceB64()})

	// The original lifecycle kinds still land (additive contract).
	acc.await("heard", 25*time.Second)
	acc.await("prompt_dispatch", 20*time.Second)
	acc.await("turn_final", 20*time.Second)

	// Each per-phase kind carries FULL text and a non-negative ms.
	stt := acc.await("stt_final", 10*time.Second)
	if txt, _ := stt["text"].(string); txt != transcript {
		t.Fatalf("stt_final text = %q, want full transcript %q", txt, transcript)
	}
	if ms, _ := stt["ms"].(float64); ms < 0 {
		t.Fatalf("stt_final ms = %v, want >= 0", stt["ms"])
	}

	model := acc.await("model_final", 10*time.Second)
	if txt, _ := model["text"].(string); txt != reply {
		t.Fatalf("model_final text = %q, want full reply %q", txt, reply)
	}
	if ms, _ := model["ms"].(float64); ms < 0 {
		t.Fatalf("model_final ms = %v, want >= 0", model["ms"])
	}

	play := acc.await("voice_play", 10*time.Second)
	if txt, _ := play["text"].(string); txt == "" {
		t.Fatalf("voice_play carried no spoken text: %+v", play)
	}
	if ms, _ := play["ms"].(float64); ms < 0 {
		t.Fatalf("voice_play ms = %v, want >= 0", play["ms"])
	}
}
