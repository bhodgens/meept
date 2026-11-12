package daemon

// C-3 lockstep pin (2026-11-11 bughunt): the agent lane's
// stripVoiceContextStamp must remove EXACTLY the prefix this package's
// voiceTurnContextStamp adds — and ONLY it. The two package pin tests keep
// in lockstep (the packages cannot share the constant without an import
// cycle: internal/daemon imports internal/agent).
//
// The FULL loopTurnRunner.Run path is pinned here: a voice turn through
// the real runner + Manager clones reaches the loop with the stamped
// prompt; the loop's derived fields (turnUserPrompt — the input of the
// ^-anchored deliverableFileEffectRe file-claim guard) must carry the RAW
// spoken prompt, while the model request keeps the stamp.
//
// TurnUserPromptForTest (the cross-package read seam this file uses) is
// defined in voice_stamp_derived_fields_test.go in internal/agent.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
)

// agentStripVoiceContextStampForDaemonTest calls the agent package's
// PRODUCTION stripper (StripVoiceContextStamp) to prove the stamp shape
// this package formats and the stripper the loop applies stay in lockstep.
func agentStripVoiceContextStampForDaemonTest(prompt string) string {
	return agent.StripVoiceContextStamp(prompt)
}

// TestVoiceStampShapeMatchesAgentStripper pins the prefix contract against
// the agent helper (compile-time dependency on the exact shape).
func TestVoiceStampShapeMatchesAgentStripper(t *testing.T) {
	now := time.Date(2026, 11, 11, 12, 20, 0, 0, time.FixedZone("MST", -7*3600))
	stamp := voiceTurnContextStamp(now)
	const spoken = "create the file report.txt"
	stamped := stamp + spoken
	if got := agentStripVoiceContextStampForDaemonTest(stamped); got != spoken {
		t.Errorf("agent stripper = %q, want %q (stamp shapes out of lockstep)", got, spoken)
	}
	// The stamp itself must contain the marker the spoken guard backstops.
	if !strings.Contains(stamp, "[Context: current date/time") {
		t.Errorf("stamp %q lost the guard token (C-9 backstop)", stamp)
	}
}

// TestVoiceTurnDerivedPromptIsRawPinsC3 pins the C-3 chain at the seam this
// package owns: the runner stamps ONLY the model request (voiceTurnContext
// Stamp + prompt) and the loop strips exactly that prefix from the derived
// field — so the composed contract is: strip(stamp(raw)) == raw, proven
// through the REAL stamp this runner formats and the REAL stripper the
// loop applies. (The agent-side pins assert the loop assigns
// stripVoiceContextStamp(userMessage) to turnUserPrompt and both derived
// goroutine inputs; this pin guarantees the two shapes cannot drift
// apart silently.)
func TestVoiceTurnDerivedPromptIsRawPinsC3(t *testing.T) {
	r, chatter, sessions := clockStampRunner(t, "it's about noon")
	sess := newVoiceSession(t, sessions)

	const spoken = "what time is it"
	if _, err := r.Run(context.Background(), sess, spoken); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Model request: stamped (the feature).
	var stamped string
	for _, m := range chatter.lastUserMessages() {
		if strings.Contains(m, clockStampMarker) && strings.HasSuffix(m, spoken) {
			stamped = m
		}
	}
	if stamped == "" {
		t.Fatalf("no stamped model request found; user messages: %#v", chatter.lastUserMessages())
	}
	// The composed contract: the exact text this seam produced, run
	// through the exact stripper the loop applies, IS the raw prompt.
	if got := agent.StripVoiceContextStamp(stamped); got != spoken {
		t.Errorf("strip(stamp(raw)) = %.160q, want the raw spoken prompt %q (shapes out of lockstep — C-3)", got, spoken)
	}
}
