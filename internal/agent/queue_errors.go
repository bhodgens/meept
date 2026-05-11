package agent

import "errors"

// Queue-specific error types.
var (
	ErrQueueClosed        = errors.New("queue is closed")
	ErrQueueFull          = errors.New("queue is full")
	ErrQueueNotFound      = errors.New("queue not found")
	ErrGenerationMismatch = errors.New("generation mismatch")
)
