package llm

import "errors"

// NonRetryableError is an interface for errors that should not trigger
// automatic retry logic. Errors like budget exhaustion, invalid requests,
// or authentication failures should implement this interface to prevent
// wasteful retry attempts.
type NonRetryableError interface {
	error
	// NonRetryable returns true if this error should not be retried.
	NonRetryable() bool
}

// IsNonRetryable checks if an error (or any wrapped error in its chain)
// implements NonRetryableError and returns true. Uses errors.As to correctly
// handle wrapped errors (e.g. fmt.Errorf("context: %w", budgetErr)).
func IsNonRetryable(err error) bool {
	if err == nil {
		return false
	}
	var nre NonRetryableError
	if errors.As(err, &nre) {
		return nre.NonRetryable()
	}
	return false
}
