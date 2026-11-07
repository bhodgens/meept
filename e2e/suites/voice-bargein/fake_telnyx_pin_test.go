//go:build e2e

// Compile-pin: the C1->talk-loop leg is live now (TestVoiceBargeinViaTalkLoop
// arms the talk loop and drives the child interrupt), so most helpers below
// are exercised for real. The pin keeps the still-unexercised ones reachable
// for U1000 without executing anything.
package voicebargein

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
)
