package backup

import (
	"errors"
	"fmt"
)

// SyncError represents a structured error from the synchronization subsystem.
type SyncError struct {
	PeerID  string // peer node ID, empty if not peer-specific
	Op      string // operation name, e.g. "pull", "decompress", "merge"
	Err     error  // underlying error
	Message string // human-readable message
}

func (e *SyncError) Error() string {
	prefix := "sync"
	if e.PeerID != "" {
		prefix = "sync:" + e.PeerID
	}
	if e.Message != "" {
		return fmt.Sprintf("%s (%s): %s", prefix, e.Op, e.Message)
	}
	return fmt.Sprintf("%s (%s): %v", prefix, e.Op, e.Err)
}

func (e *SyncError) Unwrap() error {
	return e.Err
}

// Sentinel errors for sync operations.
var (
	ErrPeerNotFound = &SyncError{
		Op:      "find",
		Message: "peer backup not found in repo",
	}

	ErrPeerDBCorrupt = &SyncError{
		Op:      "decompress",
		Message: "peer database appears corrupt after decompression",
	}

	ErrSchemaMismatch = &SyncError{
		Op:      "merge",
		Message: "schema mismatch (peer version incompatible)",
	}

	ErrMergeTimeout = &SyncError{
		Op:      "merge",
		Message: "merge operation timed out",
	}

	ErrBackupNotCompressed = &SyncError{
		Op:      "find",
		Message: "peer backup is not in expected .db.zst format",
	}

	ErrGossipDBRequired = &SyncError{
		Op:      "init",
		Message: "gossipDB is required for sync puller",
	}
)

// IsSyncError reports whether err is a *SyncError.
func IsSyncError(err error) bool {
	var se *SyncError
	return errors.As(err, &se)
}

// IsRetryable checks if a sync error can be retried.
func IsSyncRetryable(err error) bool {
	if err == nil {
		return false
	}
	var se *SyncError
	if errors.As(err, &se) {
		switch se.Op {
		case "pull", "find":
			return true // network/transport failures are retryable
		case "decompress":
			return true // transient disk issues are retryable
		case "merge":
			return false // merge errors need manual intervention
		default:
			return true
		}
	}
	// Fall back to BackupError retry check
	return IsRetryable(err)
}

// SyncWrap creates a new SyncError with context.
func SyncWrap(op string, err error) error {
	if err == nil {
		return nil
	}
	var se *SyncError
	if errors.As(err, &se) {
		return &SyncError{
			PeerID:  se.PeerID,
			Op:      op + "->" + se.Op,
			Err:     se.Err,
			Message: se.Message,
		}
	}
	return &SyncError{
		Op:    op,
		Err:   err,
	}
}
