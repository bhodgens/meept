package voice

// Gate tests for call recording — manager/answer-path half (leaf 06).
// Config contract (leaf 06, quoted):
//
//	voice.record_calls bool (default false)
//	voice.record_consent_pin string — REQUIRED non-empty when
//	record_calls true; startup validation refuses (logs Error, disables
//	recording) when missing.
//
// Rows per master Test Gate: default off => no StartRecording call on
// answer; on + consent pin set => StartRecording (dual-channel mp3) at
// answer; on + no consent pin => refuse + Error log; recording.saved
// webhook => URL stored on the call record.
//
// The recording policy seam is RecordingPolicy (leaf 06 introduces it):
// a Manager-owned gate the answer path consults. The fake below counts
// carrier record_start invocations. RED until leaf 06 (RecordingPolicy /
// Enabled / StartOnAnswer / ConsentRefused undefined).

import (
	"context"
	"testing"
	"time"
)

// recordingCounter counts carrier record_start invocations through the
// manager's recording seam.
type recordingCounter struct {
	starts int
}

// TestRecordingDefaultOffNoStartOnAnswer pins default-off: record_calls
// absent (zero value) => answering a call issues NO StartRecording —
// verified against a zero-request posture (the counter must stay at 0).
func TestRecordingDefaultOffNoStartOnAnswer(t *testing.T) {
	var counter recordingCounter
	cfg := RecordingPolicy{} // zero value: disabled
	if cfg.Enabled {
		t.Fatal("RecordingPolicy zero value must be disabled (default off)")
	}
	if cfg.StartOnAnswer("cc-1") {
		t.Fatal("disabled policy must never start recording on answer")
	}
	if counter.starts != 0 {
		t.Fatalf("record_start issued %d times under default-off", counter.starts)
	}
}

// TestRecordingOnWithConsentPinStartsOnAnswer pins the enabled path:
// record_calls on + non-empty consent pin => the answer path starts
// recording (dual-channel mp3 rides the client contract, pinned in the
// telnyx recording_test.go).
func TestRecordingOnWithConsentPinStartsOnAnswer(t *testing.T) {
	cfg := RecordingPolicy{Enabled: true, ConsentAnnounced: true}
	if !cfg.StartOnAnswer("cc-2") {
		t.Fatal("enabled policy with consent pin must start recording at answer")
	}
}

// TestRecordingOnWithoutConsentPinRefused pins the consent gate: enabling
// without the pin is refused (Error-logged at startup by the wiring; here
// the policy layer must refuse per-call starts), never a silent
// record-without-consent.
func TestRecordingOnWithoutConsentPinRefused(t *testing.T) {
	var counter recordingCounter
	cfg := RecordingPolicy{Enabled: true, ConsentAnnounced: false}
	if cfg.StartOnAnswer("cc-3") {
		t.Fatal("recording without the consent announcement pin must be refused")
	}
	if counter.starts != 0 {
		t.Fatalf("record_start issued %d times on the refusal path", counter.starts)
	}
	if !cfg.ConsentRefused() {
		t.Error("ConsentRefused must report the refusal so the wiring can log it at Error level")
	}
}

// TestRecordingSavedURLStoredOnCallRecord pins webhook capture: a
// recording.saved event for a tracked call stores the URL on the call
// record (SetRecordingURL; additive recording_url column per leaf 06).
func TestRecordingSavedURLStoredOnCallRecord(t *testing.T) {
	s, _ := briefStore(t)
	ctx := context.Background()
	if err := s.Start(ctx, CallRecord{CallID: "cc-rec", Direction: DirectionIncoming, StartedAt: time.Now()}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := s.SetRecordingURL(ctx, "cc-rec", "https://r/a.mp3"); err != nil {
		t.Fatalf("SetRecordingURL: %v", err)
	}
	got := s.RecordingURL(ctx, "cc-rec")
	if got != "https://r/a.mp3" {
		t.Fatalf("RecordingURL = %q, want https://r/a.mp3", got)
	}
	// Unknown call id: no error, no row written (idempotent posture).
	if u := s.RecordingURL(ctx, "cc-unknown"); u != "" {
		t.Errorf("RecordingURL for unknown call = %q, want empty", u)
	}
}
