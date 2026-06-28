package backup

import (
	"errors"
	"fmt"
	"testing"
)

// TestSyncError_ErrorWithMessage verifies the formatting of (*SyncError).Error
// when Message is populated. The format is "sync (<op>): <message>".
func TestSyncError_ErrorWithMessage(t *testing.T) {
	t.Parallel()

	se := &SyncError{
		Op:      "merge",
		Message: "schema mismatch (peer version incompatible)",
	}
	got := se.Error()
	want := "sync (merge): schema mismatch (peer version incompatible)"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestSyncError_ErrorWithPeerID verifies that PeerID prefixes the error string
// as "sync:<peerID> (<op>): <message>".
func TestSyncError_ErrorWithPeerID(t *testing.T) {
	t.Parallel()

	se := &SyncError{
		PeerID:  "node-b",
		Op:      "pull",
		Message: "connection refused",
	}
	got := se.Error()
	want := "sync:node-b (pull): connection refused"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestSyncError_ErrorFallbackToErr verifies that when Message is empty, the
// underlying Err value is stringified.
func TestSyncError_ErrorFallbackToErr(t *testing.T) {
	t.Parallel()

	underlying := errors.New("disk write failed")
	se := &SyncError{
		Op:  "decompress",
		Err: underlying,
	}
	got := se.Error()
	want := "sync (decompress): disk write failed"
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

// TestSyncError_Unwrap verifies errors.Unwrap returns the inner Err.
func TestSyncError_Unwrap(t *testing.T) {
	t.Parallel()

	underlying := errors.New("root cause")
	se := &SyncError{Op: "pull", Err: underlying}

	if !errors.Is(se, underlying) {
		t.Errorf("errors.Is(SyncError, underlying) = false, want true")
	}
}

// TestSentinelSyncErrors_As verifies errors.As succeeds against each sentinel.
// This confirms the sentinels are *SyncError instances accessible via the
// standard errors.As interface.
func TestSentinelSyncErrors_As(t *testing.T) {
	t.Parallel()

	sentinels := []struct {
		name string
		err  error
	}{
		{"ErrPeerNotFound", ErrPeerNotFound},
		{"ErrPeerDBCorrupt", ErrPeerDBCorrupt},
		{"ErrSchemaMismatch", ErrSchemaMismatch},
		{"ErrMergeTimeout", ErrMergeTimeout},
		{"ErrBackupNotCompressed", ErrBackupNotCompressed},
		{"ErrGossipDBRequired", ErrGossipDBRequired},
	}

	for _, tc := range sentinels {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var se *SyncError
			if !errors.As(tc.err, &se) {
				t.Errorf("errors.As(%s, *SyncError) = false, want true", tc.name)
			}
			if se == nil {
				t.Fatalf("target *SyncError is nil after As(%s)", tc.name)
			}
			if se.Op == "" {
				t.Errorf("%s.Op is empty", tc.name)
			}
			if se.Message == "" {
				t.Errorf("%s.Message is empty", tc.name)
			}
		})
	}
}

// TestIsSyncError distinguishes SyncError from BackupError and plain errors.
func TestIsSyncError(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"plain SyncError", &SyncError{Op: "x"}, true},
		{"wrapped SyncError", fmt.Errorf("wrap: %w", &SyncError{Op: "x"}), true},
		{"BackupError is not SyncError", &BackupError{Op: "x"}, false},
		{"plain error", errors.New("plain"), false},
		{"nil", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := IsSyncError(tc.err)
			if got != tc.want {
				t.Errorf("IsSyncError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsSyncRetryable_Matrix exercises the IsSyncRetryable decision matrix
// for every Op branch plus nil and non-SyncError fallback.
func TestIsSyncRetryable_Matrix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		err  error
		want bool
	}{
		// Op branches that the implementation treats as retryable.
		{"pull is retryable", &SyncError{Op: "pull", Err: errors.New("net")}, true},
		{"find is retryable", &SyncError{Op: "find", Err: errors.New("missing")}, true},
		{"decompress is retryable", &SyncError{Op: "decompress", Err: errors.New("io")}, true},
		// Op branches that the implementation treats as non-retryable.
		{"merge is NOT retryable", &SyncError{Op: "merge", Err: errors.New("boom")}, false},
		// Unknown Op falls into default-retryable.
		{"unknown op is retryable", &SyncError{Op: "frobulate", Err: errors.New("x")}, true},
		// Nil is never retryable.
		{"nil", nil, false},
		// Non-SyncError falls through to BackupError retry check.
		{"BackupError retryable=true", &BackupError{Op: "git_push", Retryable: true}, true},
		{"BackupError retryable=false", &BackupError{Op: "disk", Retryable: false}, false},
		// A plain error is neither SyncError nor BackupError -> false.
		{"plain error", errors.New("plain"), false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got := IsSyncRetryable(tc.err)
			if got != tc.want {
				t.Errorf("IsSyncRetryable(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestSyncWrap_NilInput returns nil when wrapping nil — guards against
// accidental wrap-of-nil creating a phantom error.
func TestSyncWrap_NilInput(t *testing.T) {
	t.Parallel()

	if got := SyncWrap("anything", nil); got != nil {
		t.Errorf("SyncWrap(nil) = %v, want nil", got)
	}
}

// TestSyncWrap_PlainError wraps a plain error into a fresh *SyncError with the
// provided op label.
func TestSyncWrap_PlainError(t *testing.T) {
	t.Parallel()

	plain := errors.New("disk full")
	wrapped := SyncWrap("reserve", plain)

	se, ok := wrapped.(*SyncError)
	if !ok {
		t.Fatalf("SyncWrap returned %T, want *SyncError", wrapped)
	}
	if se.Op != "reserve" {
		t.Errorf("Op = %q, want %q", se.Op, "reserve")
	}
	if !errors.Is(wrapped, plain) {
		t.Errorf("errors.Is(wrapped, plain) = false, want true (cause must be preserved)")
	}
}

// TestSyncWrap_ChainedSyncError verifies that wrapping an existing *SyncError
// preserves the PeerID and Message from the inner error while concatenating
// the op labels with "->".
func TestSyncWrap_ChainedSyncError(t *testing.T) {
	t.Parallel()

	inner := &SyncError{
		PeerID:  "node-c",
		Op:      "decompress",
		Message: "truncated zstd frame",
	}
	wrapped := SyncWrap("reserve_peer_db", inner)

	se, ok := wrapped.(*SyncError)
	if !ok {
		t.Fatalf("SyncWrap returned %T, want *SyncError", wrapped)
	}
	if se.PeerID != "node-c" {
		t.Errorf("PeerID = %q, want %q", se.PeerID, "node-c")
	}
	if se.Message != "truncated zstd frame" {
		t.Errorf("Message = %q, want %q", se.Message, "truncated zstd frame")
	}
	wantOp := "reserve_peer_db->decompress"
	if se.Op != wantOp {
		t.Errorf("Op = %q, want %q", se.Op, wantOp)
	}
}
