package services

import (
	"errors"

	"github.com/caimlas/meept/internal/errcls"
)

// Register service sentinels with errcls so that errcls.IsParameterError and
// errcls.IsAuthError can recognize them without importing services directly
// (avoids import cycle: services -> scheduler -> rpc -> errcls -> services).
func init() {
	errcls.RegisterParameterSentinels(ErrInvalidInput)
	errcls.RegisterAuthSentinels(ErrUnauthorized)
}

// Standard service errors for consistent cross-transport handling.
var (
	ErrNotFound      = errors.New("resource not found")
	ErrAlreadyExists = errors.New("resource already exists")
	ErrInvalidInput  = errors.New("invalid input")
	ErrUnauthorized  = errors.New("unauthorized")
	ErrInternal      = errors.New("internal error")
	ErrTimeout       = errors.New("operation timed out")
	ErrUnavailable   = errors.New("service unavailable")
)

// ServiceError wraps errors with service context.
//
//nolint:revive // stutter with package name is intentional for API clarity
type ServiceError struct {
	Service string
	Op      string
	Err     error
}

func (e *ServiceError) Error() string {
	return e.Service + "." + e.Op + ": " + e.Err.Error()
}

func (e *ServiceError) Unwrap() error {
	return e.Err
}

// wrapError wraps an error with service and operation context.
func wrapError(service, op string, err error) error {
	if err == nil {
		return nil
	}
	return &ServiceError{Service: service, Op: op, Err: err}
}
