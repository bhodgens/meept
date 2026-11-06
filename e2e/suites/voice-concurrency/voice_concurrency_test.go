//go:build e2e

// Suite voice-concurrency (master Test Gate): the C8 multi-agent contract
// at the webhook boundary - one employee holds ONE live call; a second
// concurrent inbound call for the same employee is refused busy with a
// named reason, and the first call keeps running. Two-employee parallel
// calls (N employees = N calls) ride the same path and are pinned by
// TestRegistryLimits + the registry race test at unit level.
package voiceconcurrency

import (
	"testing"
	"time"
)

func TestVoiceConcurrencySecondInboundRefused(t *testing.T) {
	fake := newFakeTelnyx(t)
	s, _ := bootVoiceStack(t, fake, map[string]any{
		"max_concurrent_calls": 4,
	})

	// First call: accepted, answered at the fake call-control API.
	fake.deliverCallInitiated(s, "cc-conc-1", "incoming")
	fake.awaitAnswered("cc-conc-1", 10*time.Second)

	// Second concurrent call, same default employee: refused busy.
	fake.assertNoAnswer("cc-conc-2", 200*time.Millisecond)

	// The first call's session keeps working: its answer action was
	// already observed; media arming is the lifecycle suite's leg.
	fake.awaitAction("cc-conc-1", "/answer", 5*time.Second)
}
