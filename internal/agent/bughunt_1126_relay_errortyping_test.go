package agent

import (
	"errors"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// Bughunt 2026-11-24 M8 (issue #185): the task-relay boundary funneled a
// STRING through UserFacingError — handleTaskCompleted did
// UserFacingError(errors.New(relayError)) on text taken off JSON — so every
// typed classifier lane (llm.IsContextTooSmall / llm.IsTierFloorError use
// errors.As; quota and empty use errors.Is) saw a bare errorString and
// died. The user saw the sanitized machine text instead of the actionable
// sentence. RelayError+relayCause restore the typed cause from the
// machine-sentinel prefix so the classifiers fire across the string hop.

func TestRelayErrorRestoresTypedCauseForClassifiers(t *testing.T) {
	// A real verdict rendered exactly as llm.ContextTooSmallError renders it.
	verdict := (&llm.ContextTooSmallError{
		ProviderID:   "openai",
		ModelID:      "gpt-x",
		EstInput:     90000,
		Reserve:      4096,
		ContextLimit: 8192,
	}).Error()

	// RED at HEAD: errors.New(verdict) is a bare errorString, so
	// IsContextTooSmall is false and UserFacingError returns the raw
	// machine text, not ErrSentenceContextTooSmall.
	if llm.IsContextTooSmall(errors.New(verdict)) {
		t.Fatal("premise broken: a bare errorString classifies as context-too-small")
	}

	// GREEN: the relay wrapper restores the type.
	re := &RelayError{Text: verdict, Cause: relayCause(verdict)}
	if !llm.IsContextTooSmall(re) {
		t.Fatalf("IsContextTooSmall = false for relayed verdict %q — M8 typing still lost", verdict)
	}
	var te *llm.ContextTooSmallError
	if !errors.As(re, &te) {
		t.Fatal("errors.As could not recover the typed verdict through RelayError")
	}
	if got, _ := llm.AsContextTooSmall(re); got == nil {
		t.Fatal("llm.AsContextTooSmall returned nil through RelayError")
	}

	// User copy is the actionable sentence, never the machine internals.
	sentence := UserFacingError(re)
	if sentence != ErrSentenceContextTooSmall {
		t.Errorf("UserFacingError = %q, want the context-too-small sentence", sentence)
	}
	if !strings.Contains(sentence, "session") {
		t.Errorf("sentence %q does not read as actionable copy", sentence)
	}
}

// TestRelayErrorTierFloorQuotaEmpty: the other three sentinel shapes
// restore typing and their user sentences.
func TestRelayErrorTierFloorQuotaEmpty(t *testing.T) {
	tier := (&llm.TierFloorError{ProviderID: "p", ModelID: "m", Depth: 3, Limit: 2}).Error()
	re := &RelayError{Text: tier, Cause: relayCause(tier)}
	if !llm.IsTierFloorError(re) {
		t.Errorf("IsTierFloorError = false for relayed tier-floor text %q", tier)
	}
	if got := UserFacingError(re); got != ErrSentenceTierFloor {
		t.Errorf("UserFacingError(tier) = %q, want the tier-floor sentence", got)
	}

	quota := llm.ErrAllModelsQuotaBlocked.Error()
	reQ := &RelayError{Text: quota, Cause: relayCause(quota)}
	if !errors.Is(reQ, llm.ErrAllModelsQuotaBlocked) {
		t.Errorf("errors.Is(ErrAllModelsQuotaBlocked) = false for relayed text %q", quota)
	}

	empty := llm.ErrEmptyResponse.Error()
	reE := &RelayError{Text: empty, Cause: relayCause(empty)}
	if !errors.Is(reE, llm.ErrEmptyResponse) {
		t.Errorf("errors.Is(ErrEmptyResponse) = false for relayed text %q", empty)
	}
}

// TestRelayCauseIgnoresGarbage: prose that merely contains the word "model"
// must NOT be mistyped — the classifier must stay false.
func TestRelayCauseIgnoresGarbage(t *testing.T) {
	for _, text := range []string{
		"the task failed: network unreachable while calling model",
		"model= is not a real sentinel",
		"",
		"permission denied: /etc/shadow",
	} {
		if cause := relayCause(text); cause != nil {
			t.Errorf("relayCause(%q) = %v, want nil (no false typing on prose)", text, cause)
		}
	}
}

// TestRelayErrorWireShapeUnchanged: Error() returns the text verbatim, so
// any consumer that only reads strings sees the same bytes as pre-fix.
func TestRelayErrorWireShapeUnchanged(t *testing.T) {
	re := &RelayError{Text: "hello", Cause: errBoomSentinel}
	if re.Error() != "hello" {
		t.Errorf("Error() = %q, want verbatim text", re.Error())
	}
	if !errors.Is(re, errBoomSentinel) {
		t.Error("Unwrap chain lost the cause")
	}
	var nilRe *RelayError
	if nilRe.Error() != "" {
		t.Error("nil RelayError must render empty, not panic")
	}
}

var errBoomSentinel = errors.New("boom")
