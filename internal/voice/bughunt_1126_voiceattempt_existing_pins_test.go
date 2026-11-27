package voice

// Bughunt 2026-11-25 A5-1 leg pin: the contact chain's outcome wait runs
// under the wiring's call cap (the daemon chain runner's 10-minute context).
// The OutcomeRegistry only records a terminal outcome at call TEARDOWN, so an
// over-cap call — frequently one the callee already ANSWERED — used to be
// misreported as no_answer, which made the chain fall through to the next
// channel (SMS/Signal) while the person was still on the phone. The attempt
// now reports the DISTINCT 'timeout' outcome with an honest error text
// (contact_wiring.go is sibling-dirty, so the ctx-threading half of A6-12 is
// a paired patch; this pin covers the misreport leg).

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/contact"
)

func TestTelnyxVoiceAttempt_CallCapReportsTimeoutNotNoAnswer(t *testing.T) {
	dial := func(_ context.Context, _, _, _ string) (string, error) {
		return "ccid-cap-1", nil
	}
	// Observer with NOTHING ever recorded (the answered-but-still-live call
	// at cap: no teardown record exists yet).
	outcomes := NewOutcomeRegistry()
	attempt := NewTelnyxVoiceAttempt(dial, outcomes.Lookup)

	ctx, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	outcome, err := attempt.Attempt(ctx, contact.Record{Phone: "+155****0001"}, "hello")
	if outcome == string(OutcomeNoAnswer) {
		t.Fatalf("over-cap call reported no_answer — the A5-1 misreport: the chain would fall through to SMS mid-call (err=%v)", err)
	}
	if outcome != string(OutcomeTimeout) {
		t.Fatalf("outcome = %q, want %q", outcome, string(OutcomeTimeout))
	}
	if err == nil {
		t.Fatal("non-terminal outcome must carry an error (C2 contract)")
	}
	if !errors.Is(err, errOutcomeWaitTimeout) {
		t.Fatalf("err = %v, want errOutcomeWaitTimeout", err)
	}
	// The error text must NOT claim the call went unanswered.
	if msg := strings.ToLower(err.Error()); strings.Contains(msg, "no answer") || strings.Contains(msg, "unanswered") {
		t.Errorf("timeout error text misreports the call state: %q", msg)
	}
	// Non-vacuity: the same registry WITH a recorded answered outcome still
	// stops the chain (answered, nil err) — only the no-record cap leg
	// changed.
	rec := NewOutcomeRegistry()
	rec.Record("ccid-cap-1", OutcomeAnswered)
	attempt2 := NewTelnyxVoiceAttempt(dial, rec.Lookup)
	outcome2, err2 := attempt2.Attempt(context.Background(), contact.Record{Phone: "+155****0001"}, "hello")
	if outcome2 != string(OutcomeAnswered) || err2 != nil {
		t.Fatalf("answered leg regressed: outcome=%q err=%v", outcome2, err2)
	}
	// And a recorded no_answer still reports no_answer (unchanged).
	rec2 := NewOutcomeRegistry()
	rec2.Record("ccid-cap-1", OutcomeNoAnswer)
	attempt3 := NewTelnyxVoiceAttempt(dial, rec2.Lookup)
	outcome3, err3 := attempt3.Attempt(context.Background(), contact.Record{Phone: "+155****0001"}, "hello")
	if outcome3 != string(OutcomeNoAnswer) || err3 == nil {
		t.Fatalf("recorded no_answer leg changed: outcome=%q err=%v", outcome3, err3)
	}
}

func TestTelnyxVoiceAttempt_DialFailureStillFailed(t *testing.T) {
	dialErr := errors.New("allowlist refused")
	attempt := NewTelnyxVoiceAttempt(func(_ context.Context, _, _, _ string) (string, error) {
		return "", dialErr
	}, NewOutcomeRegistry().Lookup)
	outcome, err := attempt.Attempt(context.Background(), contact.Record{Phone: "+155****0001"}, "hi")
	if outcome != string(OutcomeFailed) || !errors.Is(err, dialErr) {
		t.Fatalf("dial failure shape changed: outcome=%q err=%v", outcome, err)
	}
}
