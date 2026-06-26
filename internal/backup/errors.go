package backup

import (
	"errors"
	"fmt"
)

// BackupError represents a structured error from the backup subsystem.
type BackupError struct {
	Op        string // operation name, e.g. "compress", "git_push", "manifest"
	Err       error  // underlying error
	Retryable bool   // whether the error can be retried
	Message   string // human-readable message
}

func (e *BackupError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("backup (%s): %s", e.Op, e.Message)
	}
	return fmt.Sprintf("backup (%s): %v", e.Op, e.Err)
}

func (e *BackupError) Unwrap() error {
	return e.Err
}

// IsRetryable reports whether this error is retryable.
func (e *BackupError) IsRetryable() bool {
	return e.Retryable
}

// IsRetryable reports whether the error is retryable.
func IsRetryable(err error) bool {
	var berr *BackupError
	return errors.As(err, &berr) && berr.Retryable
}

// As wraps err in a *BackupError with the given op.
func As(op string, err error) *BackupError {
	if err == nil {
		return nil
	}
	var berr *BackupError
	if errors.As(err, &berr) {
		return &BackupError{
			Op:        op + "->" + berr.Op,
			Err:       berr.Err,
			Retryable: berr.Retryable,
			Message:   berr.Message,
		}
	}
	return &BackupError{
		Op:        op,
		Err:       err,
		Retryable: true, // default to retryable for unknown errors
	}
}

// Predefined sentinel errors.
var (
	// ErrGitConflict indicates the remote has a newer backup that conflicts.
	ErrGitConflict = &BackupError{
		Op:        "git_push",
		Retryable: true,
		Message:   "git conflict (remote has newer backup); rebase and retry",
	}

	// ErrDiskFull indicates insufficient disk space for the backup.
	ErrDiskFull = &BackupError{
		Op:        "compress",
		Retryable: false,
		Message:   "disk full: insufficient space for backup",
	}

	// ErrCompression indicates compression failed.
	ErrCompression = &BackupError{
		Op:        "compress",
		Retryable: false,
		Message:   "compression failed",
	}

	// ErrNoDatabases indicates no database paths were found to back up.
	ErrNoDatabases = &BackupError{
		Op:        "backup",
		Retryable: false,
		Message:   "no database paths found to back up",
	}

	// ErrManifestMissing indicates a required manifest could not be loaded.
	ErrManifestMissing = &BackupError{
		Op:        "manifest",
		Retryable: false,
		Message:   "manifest file not found or unreadable",
	}

	// ErrConfigInvalid indicates backup configuration is invalid.
	ErrConfigInvalid = &BackupError{
		Op:        "config",
		Retryable: false,
		Message:   "backup configuration is invalid",
	}
)

// Wrap creates a new BackupError with context.
func Wrap(op string, err error) error {
	if err == nil {
		return nil
	}
	return As(op, err)
}

// IsBackupError returns true if err is a BackupError of any kind.
func IsBackupError(err error) bool {
	var berr *BackupError
	return errors.As(err, &berr)
}
