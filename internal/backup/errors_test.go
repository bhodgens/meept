package backup

import (
	"errors"
	"fmt"
	"testing"
)

func TestBackupError_Error(t *testing.T) {
	err := &BackupError{
		Op:      "compress",
		Message: "disk full",
	}
	expected := "backup (compress): disk full"
	if err.Error() != expected {
		t.Errorf("Error: got %q, want %q", err.Error(), expected)
	}
}

func TestBackupError_ErrorWithUnderlying(t *testing.T) {
	inner := fmt.Errorf("underlying io error")
	err := &BackupError{
		Op:  "git_push",
		Err: inner,
	}
	expected := "backup (git_push): underlying io error"
	if err.Error() != expected {
		t.Errorf("Error: got %q, want %q", err.Error(), expected)
	}
}

func TestBackupError_Unwrap(t *testing.T) {
	inner := fmt.Errorf("inner error")
	err := &BackupError{
		Op:  "compress",
		Err: inner,
	}
	if !errors.Is(err, inner) {
		t.Error("expected Is to find inner error via Unwrap")
	}
}

func TestBackupError_IsRetryable(t *testing.T) {
	retryableErr := &BackupError{Op: "git_push", Retryable: true}
	nonRetryable := &BackupError{Op: "compress", Retryable: false}

	if !retryableErr.IsRetryable() {
		t.Error("expected retryable error to be retryable")
	}
	if nonRetryable.IsRetryable() {
		t.Error("expected non-retryable error to not be retryable")
	}
}

func TestIsRetryable(t *testing.T) {
	if !IsRetryable(&BackupError{Op: "git", Retryable: true}) {
		t.Error("IsRetryable(true) should return true")
	}
	if IsRetryable(&BackupError{Op: "disk", Retryable: false}) {
		t.Error("IsRetryable(false) should return false")
	}
	if IsRetryable(nil) {
		t.Error("IsRetryable(nil) should return false")
	}
	if IsRetryable(fmt.Errorf("plain error")) {
		t.Error("IsRetryable(plain error) should return false")
	}
}

func TestIsBackupError(t *testing.T) {
	if !IsBackupError(&BackupError{Op: "compress"}) {
		t.Error("IsBackupError(true) should return true")
	}
	if IsBackupError(fmt.Errorf("plain error")) {
		t.Error("IsBackupError(plain) should return false")
	}
	if IsBackupError(nil) {
		t.Error("IsBackupError(nil) should return false")
	}
}

func TestWrap(t *testing.T) {
	inner := fmt.Errorf("original error")
	wrapped := Wrap("outer->inner", inner)

	if !IsBackupError(wrapped) {
		t.Error("wrapped error should be a BackupError")
	}
	if !errors.Is(wrapped, inner) {
		t.Error("wrapped error should unwrap to original")
	}

	// Wrap nil returns nil
	if Wrap("test", nil) != nil {
		t.Error("Wrap(nil) should return nil")
	}
}

func TestWrap_Chain(t *testing.T) {
	inner := fmt.Errorf("root cause")
	wrapped := Wrap("level1", Wrap("level2", inner))

	// The chain should still unwrap to inner
	if !errors.Is(wrapped, inner) {
		t.Error("chained wrap should preserve unwrap chain")
	}
}

func TestSentinelErrors(t *testing.T) {
	// Verify predefined errors are actually *BackupError
	sentinals := []struct {
		name string
		err  error
	}{
		{"ErrGitConflict", ErrGitConflict},
		{"ErrDiskFull", ErrDiskFull},
		{"ErrCompression", ErrCompression},
		{"ErrNoDatabases", ErrNoDatabases},
		{"ErrManifestMissing", ErrManifestMissing},
		{"ErrConfigInvalid", ErrConfigInvalid},
	}

	for _, s := range sentinals {
		if !IsBackupError(s.err) {
			t.Errorf("%s should be a BackupError", s.name)
		}
	}

	// Specific checks
	if !ErrGitConflict.Retryable {
		t.Error("ErrGitConflict should be retryable")
	}
	if ErrDiskFull.Retryable {
		t.Error("ErrDiskFull should not be retryable")
	}
	if ErrNoDatabases.Retryable {
		t.Error("ErrNoDatabases should not be retryable")
	}
}
