package telnyx

// Gate tests for contract C2 (master.md, quoted verbatim):
//
//	C2 IgnoredEvent: type IgnoredEventError struct{ EventType string };
//	func IsIgnoredEvent(err error) bool (errors.As). Only for `call.*`
//	non-lifecycle events; non-call.* remain plain errors.
//
// Ported verbatim in case-set from uptractor
// phone/core/internal/telnyx/callstatus_test.go
// (TestParseCallEventIgnoresOtherCallEventsButRefusesMisrouted) plus the
// leaf-01 T2 canonical case ("call.analyzed"). RED until leaf 01
// (IgnoredEventError / IsIgnoredEvent / ParseCallEvent classification
// exist).

import (
	"strings"
	"testing"
)

// TestIgnoredEventParsesCallAnalyzed pins the canonical leaf case: a
// call.analyzed webhook is real Telnyx call.* traffic this service does
// not drive — ParseCallEvent returns an error that IS an
// IgnoredEventError (so the webhook handler ACKs it, 2xx, and Telnyx
// never retries) and the error names the event type.
func TestIgnoredEventParsesCallAnalyzed(t *testing.T) {
	ev, err := ParseCallEvent([]byte(`{"data":{"event_type":"call.analyzed","payload":{}}}`))
	if !IsIgnoredEvent(err) {
		t.Fatalf("err = %v, want IgnoredEventError", err)
	}
	// The returned event must be the zero event: an ignored webhook is
	// never acted on as if it were a lifecycle transition.
	if ev != (CallEvent{}) {
		t.Errorf("event = %+v, want zero CallEvent for an ignored event", ev)
	}
	if err != nil && !strings.Contains(err.Error(), "call.analyzed") {
		t.Errorf("error should name the event, got %v", err)
	}
}

// TestIgnoredEventCoversNonLifecycleCallEvents ports the uptractor case
// set verbatim: every observed non-lifecycle call.* event classifies as
// IgnoredEventError (ack + debug log), while a misrouted non-call.*
// webhook stays a plain hard error so it is refused (non-2xx) and can
// never be acted on as a live call.
func TestIgnoredEventCoversNonLifecycleCallEvents(t *testing.T) {
	// call.* non-lifecycle: ignored-event classification.
	for _, typ := range []string{
		"call.conversation.created",
		"call.conversation.ended",
		"call.analyzed",
		"call.conversation_insights.generated",
	} {
		_, err := ParseCallEvent([]byte(`{"data":{"event_type":"` + typ + `","payload":{}}}`))
		if !IsIgnoredEvent(err) {
			t.Errorf("%s: err = %v, want IgnoredEventError", typ, err)
		}
		if err != nil && !strings.Contains(err.Error(), typ) {
			t.Errorf("%s: error should name the event, got %v", typ, err)
		}
	}
	// Non-call.* events stay plain errors: IsIgnoredEvent must be false
	// so a misrouted webhook is still refused.
	_, err := ParseCallEvent([]byte(`{"data":{"event_type":"message.received","payload":{}}}`))
	if err == nil || IsIgnoredEvent(err) {
		t.Errorf("a non-call event must stay a plain error, got %v", err)
	}
	// IsIgnoredEvent over a plain error (and nil) is false: no accidental
	// error-classification widening.
	if IsIgnoredEvent(nil) {
		t.Error("IsIgnoredEvent(nil) = true, want false")
	}
	if IsIgnoredEvent(ParseCallEventErrStub()) {
		t.Error("IsIgnoredEvent(plain error) = true, want false")
	}
}

// ParseCallEventErrStub returns a guaranteed-plain error (invalid JSON)
// for the IsIgnoredEvent negative case.
func ParseCallEventErrStub() error {
	_, err := ParseCallEvent([]byte(`{not json`))
	return err
}
