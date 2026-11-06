//go:build e2e

// Compile-pin: helpers for subtests skipped pending the C1->talk-loop leg.
// Function values keep them reachable for U1000 without executing anything.
package voiceconcurrency

var ftPin = &fakeTelnyx{}

var (
	_ = ftPin.connectChild
	_ = ftPin.readChildFrames
	_ = ftPin.childCommand
	_ = ftPin.childSetup
	_ = ftPin.childDialAudio
	_ = ftPin.childSendsPrompt
	_ = ftPin.childSendsInterrupt
	_ = ftPin.awaitChildFrame
	_ = ftPin.awaitSayContaining
	_ = ftPin.awaitSayAdvance
	_ = ftPin.sayFrameCountAfterClear
	_ = ftPin.assertNoMediaFrame
	_ = ftPin.awaitMediaFrame
	_ = ftPin.findFrame
	_ = ftPin.deliverHangup
	_ = ftPin.assertNoAnswer
	_ = ftPin.awaitClearFrame
	_ = ftPin.awaitChildStop
	_ = ftPin.awaitAction
	_ = ftPin.streamURL
	_ = ftPin.deliverCallInitiatedExpectStatus
	_ = ftPin.dialMedia
	_ = ftPin.sendMediaFrame
)
