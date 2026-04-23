package task

import (
	"testing"
	"time"
)

func TestInterruptToken_Trigger(t *testing.T) {
	tok := NewInterruptToken("task-1")

	// Should not be triggered initially
	if tok.IsTriggered() {
		t.Fatal("token should not be triggered initially")
	}

	// Trigger cancellation
	tok.Trigger(ReasonUserCancelled, "User changed their mind")

	// Should be triggered now
	if !tok.IsTriggered() {
		t.Fatal("token should be triggered after Trigger()")
	}

	// Reason and message should be set
	if tok.Reason() != ReasonUserCancelled {
		t.Errorf("got reason %v, want %v", tok.Reason(), ReasonUserCancelled)
	}
	if tok.Message() != "User changed their mind" {
		t.Errorf("got message %q, want %q", tok.Message(), "User changed their mind")
	}
}

func TestInterruptToken_ContextCancellation(t *testing.T) {
	tok := NewInterruptToken("task-1")
	ctx := tok.Context()

	// Context should not be done initially
	select {
	case <-ctx.Done():
		t.Fatal("context should not be done initially")
	default:
	}

	// Trigger cancellation
	tok.Trigger(ReasonUserAmended, "New direction provided")

	// Context should be done now
	select {
	case <-ctx.Done():
		// Expected
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context should be cancelled")
	}
}

func TestInterruptToken_DoubleTrigger(t *testing.T) {
	tok := NewInterruptToken("task-1")

	tok.Trigger(ReasonUserCancelled, "First reason")
	tok.Trigger(ReasonSuperseded, "Second reason") // Should be ignored

	if tok.Reason() != ReasonUserCancelled {
		t.Errorf("first trigger should win, got %v", tok.Reason())
	}
	if tok.Message() != "First reason" {
		t.Errorf("first message should win, got %q", tok.Message())
	}
}

func TestInterruptToken_TaskID(t *testing.T) {
	tok := NewInterruptToken("task-123")
	if tok.TaskID() != "task-123" {
		t.Errorf("got task ID %q, want %q", tok.TaskID(), "task-123")
	}
}

func TestInterruptToken_TriggeredAt(t *testing.T) {
	tok := NewInterruptToken("task-1")

	before := time.Now().UTC()
	tok.Trigger(ReasonUserCancelled, "test")
	after := time.Now().UTC()

	triggeredAt := tok.TriggeredAt()

	if triggeredAt.Before(before) || triggeredAt.After(after) {
		t.Errorf("triggeredAt %v not in expected range [%v, %v]", triggeredAt, before, after)
	}
}

func TestInterruptToken_Reset(t *testing.T) {
	tok := NewInterruptToken("task-1")

	// Trigger first
	tok.Trigger(ReasonUserCancelled, "test")
	if !tok.IsTriggered() {
		t.Fatal("token should be triggered")
	}

	// Reset
	tok.Reset()

	// Should not be triggered anymore
	if tok.IsTriggered() {
		t.Fatal("token should not be triggered after reset")
	}
	if tok.Reason() != "" {
		t.Errorf("reason should be cleared, got %v", tok.Reason())
	}
	if tok.Message() != "" {
		t.Errorf("message should be cleared, got %q", tok.Message())
	}

	// Context should work again
	ctx := tok.Context()
	select {
	case <-ctx.Done():
		t.Fatal("context should not be done after reset")
	default:
	}
}

func TestInterruptToken_ContextDone(t *testing.T) {
	tok := NewInterruptToken("task-1")

	// Start a goroutine waiting on the context
	ctx := tok.Context()
	done := make(chan struct{})
	go func() {
		<-ctx.Done()
		close(done)
	}()

	// Trigger after a short delay
	time.Sleep(10 * time.Millisecond)
	tok.Trigger(ReasonUserCancelled, "test")

	// Should be signaled within reasonable time
	select {
	case <-done:
		// Success
	case <-time.After(100 * time.Millisecond):
		t.Fatal("context.Done() should be signaled after trigger")
	}
}
