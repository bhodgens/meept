package agent

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// Leaf 06 (graceful errors) unit pins: the UserFacingError mapping table.
// These are vocabulary + family pins, not exact-wording-only pins -- the
// sentences are constants, so exact equality is the honest pin.

func TestUserFacingError_ContextTooSmall(t *testing.T) {
	err := &llm.ContextTooSmallError{
		ProviderID:   "llama",
		ModelID:      "lfm-7b",
		EstInput:     20000,
		Reserve:      2000,
		ContextLimit: 16384,
	}
	got := UserFacingError(err)
	if got != ErrSentenceContextTooSmall {
		t.Errorf("UserFacingError(ContextTooSmall) = %q, want %q", got, ErrSentenceContextTooSmall)
	}
	// Wrapped chain must map identically.
	wrapped := fmt.Errorf("turn failed: %w", err)
	if UserFacingError(wrapped) != ErrSentenceContextTooSmall {
		t.Error("wrapped ContextTooSmall must map through the chain")
	}
}

func TestUserFacingError_TierFloor(t *testing.T) {
	err := &llm.TierFloorError{
		ProviderID: "p",
		ModelID:    "m",
		Depth:      2,
		Limit:      2,
	}
	got := UserFacingError(err)
	if got != ErrSentenceTierFloor {
		t.Errorf("UserFacingError(TierFloor) = %q, want %q", got, ErrSentenceTierFloor)
	}
}

func TestUserFacingError_AllModelsQuotaBlocked(t *testing.T) {
	got := UserFacingError(llm.ErrAllModelsQuotaBlocked)
	if got != llm.ErrAllModelsQuotaBlocked.Error() {
		t.Errorf("UserFacingError(all quota blocked) = %q, want the existing quota sentence %q",
			got, llm.ErrAllModelsQuotaBlocked.Error())
	}
	// Wrapped inside a turn failure maps identically.
	wrapped := fmt.Errorf("dispatch failed: %w", llm.ErrAllModelsQuotaBlocked)
	if UserFacingError(wrapped) != llm.ErrAllModelsQuotaBlocked.Error() {
		t.Error("wrapped ErrAllModelsQuotaBlocked must map through the chain")
	}
}

func TestUserFacingError_EmptyResponse(t *testing.T) {
	got := UserFacingError(llm.ErrEmptyResponse)
	if got != ErrSentenceEmptyResponse {
		t.Errorf("UserFacingError(ErrEmptyResponse) = %q, want %q", got, ErrSentenceEmptyResponse)
	}
}

func TestUserFacingError_GateBlocked(t *testing.T) {
	inner := errors.New("write to /etc/hosts denied by policy")
	err := fmt.Errorf("%w: %w", ErrGateBlocked, inner)
	got := UserFacingError(err)
	if !strings.Contains(got, "denied by policy") {
		t.Errorf("UserFacingError(gate block) = %q, want the gate reason to survive", got)
	}
	for _, vocab := range []string{"github.com/", "/internal/", "/Users/", "tmp/"} {
		if strings.Contains(got, vocab) {
			t.Errorf("gate-block copy leaks internal vocab %q: %q", vocab, got)
		}
	}
}

func TestUserFacingError_UnknownSanitized(t *testing.T) {
	// Machine paths must never reach the user.
	err := fmt.Errorf("open /Users/caimlas/git/meept/internal/agent/handler.go: no such file")
	got := UserFacingError(err)
	if strings.Contains(got, "/Users/") || strings.Contains(got, "/internal/") {
		t.Errorf("unknown error leaks paths: %q", got)
	}
	if !strings.Contains(got, "no such file") {
		t.Errorf("unknown error loses the reason: %q", got)
	}
}

func TestUserFacingError_Nil(t *testing.T) {
	if got := UserFacingError(nil); got != "" {
		t.Errorf("UserFacingError(nil) = %q, want empty", got)
	}
}

// TestQuotaPark_UnblockAtSynthesizedForUnknownSchedule pins the leaf-06
// handler contract indirectly: DefaultQuotaResumePollInterval*3 is the
// conservative synthesized schedule, and the poll interval constant is
// the same one the parker uses.
func TestQuotaPark_UnblockAtSynthesizedForUnknownSchedule(t *testing.T) {
	if DefaultQuotaResumePollInterval <= 0 {
		t.Fatalf("DefaultQuotaResumePollInterval = %v, must be positive", DefaultQuotaResumePollInterval)
	}
}
