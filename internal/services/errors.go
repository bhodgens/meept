package services

import "errors"

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
