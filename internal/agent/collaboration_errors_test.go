package agent

import (
	"errors"
	"testing"
)

func TestCollaborationError_Error(t *testing.T) {
	e := NewCollaborationError(ErrCodeBudgetExceeded, "sess-1", "fork", "out of tokens")
	got := e.Error()
	want := "collaboration error [budget_exceeded] session=sess-1 phase=fork: out of tokens"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestErrBudgetExceeded(t *testing.T) {
	if !errors.Is(ErrBudgetExceeded, ErrBudgetExceeded) {
		t.Error("ErrBudgetExceeded should match itself")
	}
	if ErrBudgetExceeded.Code != ErrCodeBudgetExceeded {
		t.Errorf("code = %q, want %q", ErrBudgetExceeded.Code, ErrCodeBudgetExceeded)
	}
}

func TestErrDepthExceeded(t *testing.T) {
	if ErrDepthExceeded.Code != ErrCodeDepthExceeded {
		t.Errorf("code = %q, want %q", ErrDepthExceeded.Code, ErrCodeDepthExceeded)
	}
}
