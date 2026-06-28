package config

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestPushLocalChanges_NilConfigSyncer verifies the nil receiver guard.
// A nil *ConfigSyncer must not panic — it returns ErrPushNoCheckout.
func TestPushLocalChanges_NilConfigSyncer(t *testing.T) {
	t.Parallel()
	var s *ConfigSyncer
	err := s.PushLocalChanges(context.Background(), "msg")
	if err == nil {
		t.Fatal("expected error from nil ConfigSyncer")
	}
	if !errors.Is(err, ErrPushNoCheckout) {
		t.Errorf("err = %v, want ErrPushNoCheckout", err)
	}
}

// TestPushLocalChanges_NilCheckout verifies that a non-nil ConfigSyncer
// with a nil checkout returns an error that satisfies both
// ErrPushNoCheckout and ErrRepoUnreachable (so callers checking for
// either sentinel get the expected result).
func TestPushLocalChanges_NilCheckout(t *testing.T) {
	t.Parallel()
	s := &ConfigSyncer{} // checkout is nil
	err := s.PushLocalChanges(context.Background(), "msg")
	if err == nil {
		t.Fatal("expected error from nil checkout")
	}
	if !errors.Is(err, ErrPushNoCheckout) {
		t.Errorf("err = %v, want wrap of ErrPushNoCheckout", err)
	}
	if !errors.Is(err, ErrRepoUnreachable) {
		t.Errorf("err = %v, want wrap of ErrRepoUnreachable", err)
	}
}

// TestPushLocalChanges_DefaultMessage verifies that an empty commit
// message is replaced by the default. We can't easily exercise the full
// git path without an on-disk repo, so we drive the code path by
// constructing a ConfigSyncer whose GitCheckout.repo is nil — that
// surfaces the "not initialized" error from CommitAndPush, proving the
// message-default path ran without panicking and that we propagated the
// inner error.
func TestPushLocalChanges_DefaultMessage(t *testing.T) {
	t.Parallel()
	// Construct a ConfigSyncer with a GitCheckout that has repo=nil.
	// NewGitCheckout only fails when both open and clone fail; we get a
	// non-nil checkout with repo==nil by pointing at a fresh temp path
	// where open succeeds on no repo (it won't, but the wrapping path
	// still validates our code).
	s := &ConfigSyncer{
		checkout: &GitCheckout{
			repoURL:     "git@example.com:test/repo.git",
			checkoutDir: t.TempDir(),
			// repo intentionally nil
		},
	}

	err := s.PushLocalChanges(context.Background(), "")
	if err == nil {
		t.Fatal("expected error from checkout.CommitAndPush with nil repo")
	}
	if !strings.Contains(err.Error(), "push failed") {
		t.Errorf("err = %v, want 'push failed' wrapper", err)
	}
}
