//go:build e2e

// Compile-pin for the fakeTelnyx helper methods. The full inbound-flow
// subtests went live once the leaf-15 inbound registry-acquire landed, so
// the C1 -> talk-loop helpers are exercised for real; the pin keeps the few
// still-unexercised ones (awaitChildFrame, sayFrameCountAfterClear,
// assertNoMediaFrame) reachable for U1000 without executing any.
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
