//go:build e2e

// Compile-pin for the fakeTelnyx helper methods used by the SKIPPED full
// inbound-flow subtests (TestVoiceLifecycleInboundCall etc.): those tests
// are skipped until the leaf-14 inbound registry-acquire wiring gap is
// closed and the C1 -> talk-loop leg is activatable, but the helpers are
// the activation surface - deleting them would mean rewriting the skipped
// tests from scratch. Function-value variables keep every helper reachable
// (U1000 counts a use in a var initializer) without executing any.
package voicelifecycle

var ftPin = &fakeTelnyx{}

// Pins: one function value per skipped-subtest helper. Compile-time only.
var (
	_ = ftPin.connectChild
	_ = ftPin.readChildFrames
	_ = ftPin.childCommand
	_ = ftPin.childSetup
	_ = ftPin.childDialAudio
	_ = ftPin.childSendsPrompt
	_ = ftPin.childSendsInterrupt
	_ = ftPin.awaitChildFrame
	_ = ftPin.findFrame
	_ = ftPin.awaitSayContaining
	_ = ftPin.awaitClearFrame
	_ = ftPin.awaitChildStop
	_ = ftPin.sayFrameCount
	_ = ftPin.awaitSayAdvance
	_ = ftPin.sayFrameCountAfterClear
	_ = ftPin.assertNoMediaFrame
	_ = ftPin.deliverCallInitiatedExpectStatus
	_ = ftPin.assertNoAnswer
	_ = ftPin.sendMediaFrame
	_ = ftPin.awaitMediaFrame
)
