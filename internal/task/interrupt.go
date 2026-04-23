package task

import (
	"context"
	"sync"
	"time"
)

// InterruptReason indicates why a task was interrupted.
type InterruptReason string

const (
	ReasonUserCancelled  InterruptReason = "user_cancelled"
	ReasonUserAmended    InterruptReason = "user_amended"
	ReasonSuperseded     InterruptReason = "superseded"
	ReasonResourceLimit  InterruptReason = "resource_limit"
	ReasonDependencyFail InterruptReason = "dependency_failed"
)

func (r InterruptReason) String() string {
	return string(r)
}

// InterruptToken represents a cancellable context for a task.
type InterruptToken struct {
	mu          sync.RWMutex
	ctx         context.Context
	cancel      context.CancelFunc
	taskID      string
	triggered   bool
	reason      InterruptReason
	message     string
	triggeredAt time.Time
}

// NewInterruptToken creates a new interrupt token.
func NewInterruptToken(taskID string) *InterruptToken {
	ctx, cancel := context.WithCancel(context.Background())
	return &InterruptToken{
		ctx:    ctx,
		cancel: cancel,
		taskID: taskID,
	}
}

// Context returns the underlying context for cancellation checking.
func (t *InterruptToken) Context() context.Context {
	return t.ctx
}

// Trigger cancels the context with a reason.
func (t *InterruptToken) Trigger(reason InterruptReason, message string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.triggered {
		return // Already triggered
	}
	t.triggered = true
	t.reason = reason
	t.message = message
	t.triggeredAt = time.Now().UTC()
	t.cancel()
}

// IsTriggered returns true if the interrupt has been triggered.
func (t *InterruptToken) IsTriggered() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.triggered
}

// Reason returns the interrupt reason.
func (t *InterruptToken) Reason() InterruptReason {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.reason
}

// Message returns the interrupt message.
func (t *InterruptToken) Message() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.message
}

// TriggeredAt returns when the interrupt was triggered.
func (t *InterruptToken) TriggeredAt() time.Time {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.triggeredAt
}

// TaskID returns the task ID this token belongs to.
func (t *InterruptToken) TaskID() string {
	return t.taskID
}

// Reset clears the interrupt for task reuse.
func (t *InterruptToken) Reset() {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.triggered {
		t.ctx, t.cancel = context.WithCancel(context.Background())
		t.triggered = false
		t.reason = ""
		t.message = ""
		t.triggeredAt = time.Time{}
	}
}
