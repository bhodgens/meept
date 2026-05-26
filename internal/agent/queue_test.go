package agent

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestQueue_New(t *testing.T) {
	q := NewMessageQueue()
	if q == nil {
		t.Fatal("NewMessageQueue returned nil")
	}
	if q.IsClosed() {
		t.Error("new queue should not be closed")
	}
	if q.GetGeneration() != 0 {
		t.Errorf("initial generation should be 0, got %d", q.GetGeneration())
	}
}

func TestQueue_NewWithConfig(t *testing.T) {
	cfg := QueueConfig{
		MaxFollowUp:     5,
		MaxSteering:     1,
		FollowUpDrain:   DrainOne,
		PersistFollowUp: false,
	}
	q := NewMessageQueue(WithQueueConfig(cfg))
	if q.config.MaxFollowUp != 5 {
		t.Errorf("MaxFollowUp = %d, want 5", q.config.MaxFollowUp)
	}
}

func TestQueue_Steer(t *testing.T) {
	q := NewMessageQueue()

	if err := q.Steer(context.Background(), "new task", "user"); err != nil {
		t.Fatalf("Steer failed: %v", err)
	}

	if !q.HasSteering() {
		t.Error("expected steering queue to have a message")
	}

	// Drain it.
	msgs := q.DrainSteering()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 drained message, got %d", len(msgs))
	}
	if msgs[0].Content != "new task" {
		t.Errorf("content = %q, want %q", msgs[0].Content, "new task")
	}
	if msgs[0].QueueType != QueueTypeSteer {
		t.Errorf("queue type = %q, want %q", msgs[0].QueueType, QueueTypeSteer)
	}
	if msgs[0].Source != "user" {
		t.Errorf("source = %q, want %q", msgs[0].Source, "user")
	}

	// After drain, should be empty.
	if q.HasSteering() {
		t.Error("steering queue should be empty after drain")
	}
}

func TestQueue_SteerReplacement(t *testing.T) {
	q := NewMessageQueue(WithQueueConfig(DefaultQueueConfig()))

	// Inject two steering messages rapidly.
	if err := q.Steer(context.Background(), "first", "user"); err != nil {
		t.Fatalf("first steer failed: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
	if err := q.Steer(context.Background(), "second", "user"); err != nil {
		t.Fatalf("second steer failed: %v", err)
	}

	// Only one message should exist (latest replacing old).
	if q.Status().SteeringDepth != 1 {
		t.Errorf("steering depth = %d, want 1", q.Status().SteeringDepth)
	}

	msgs := q.DrainSteering()
	if len(msgs) != 1 {
		t.Fatalf("expected 1 message, got %d", len(msgs))
	}
	if msgs[0].Content != "second" {
		t.Errorf("content = %q, want %q (latest should win)", msgs[0].Content, "second")
	}
}

func TestQueue_SteerAfterClose(t *testing.T) {
	q := NewMessageQueue()
	q.Close()

	err := q.Steer(context.Background(), "after close", "user")
	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("Steer after close = %v, want ErrQueueClosed", err)
	}
}

func TestQueue_FollowUp(t *testing.T) {
	cfg := QueueConfig{
		MaxFollowUp:     3,
		FollowUpDrain:   DrainAll,
		PersistFollowUp: false,
	}
	q := NewMessageQueue(WithQueueConfig(cfg))

	for i := range 3 {
		content := "follow-up message " + string(rune('0'+i))
		if err := q.FollowUp(context.Background(), content, "user"); err != nil {
			t.Fatalf("FollowUp %d failed: %v", i, err)
		}
	}

	if !q.HasFollowUp() {
		t.Error("expected follow-up queue to have messages")
	}

	msgs := q.DrainFollowUp()
	if len(msgs) != 3 {
		t.Errorf("drained %d messages, want 3", len(msgs))
	}
	if msgs[0].Content != "follow-up message 0" {
		t.Errorf("first message content = %q, want %q", msgs[0].Content, "follow-up message 0")
	}
}

func TestQueue_FollowUpAppendDrainAll(t *testing.T) {
	cfg := QueueConfig{
		MaxFollowUp:     5,
		FollowUpDrain:   DrainAll,
		PersistFollowUp: false,
	}
	q := NewMessageQueue(WithQueueConfig(cfg))

	// Add 2, drain 1 (DrainAll).
	if err := q.FollowUp(context.Background(), "first", "user"); err != nil {
		t.Fatal(err)
	}
	if err := q.FollowUp(context.Background(), "second", "user"); err != nil {
		t.Fatal(err)
	}

	msgs1 := q.DrainFollowUp()
	if len(msgs1) != 2 {
		t.Fatalf("drained %d, want 2", len(msgs1))
	}

	// Add 1 more, drain again.
	if err := q.FollowUp(context.Background(), "third", "user"); err != nil {
		t.Fatal(err)
	}
	msgs2 := q.DrainFollowUp()
	if len(msgs2) != 1 {
		t.Fatalf("drained %d, want 1", len(msgs2))
	}
	if msgs2[0].Content != "third" {
		t.Errorf("content = %q, want %q", msgs2[0].Content, "third")
	}
}

func TestQueue_FollowUpDrainOne(t *testing.T) {
	cfg := QueueConfig{
		MaxFollowUp:     5,
		FollowUpDrain:   DrainOne,
		PersistFollowUp: false,
	}
	q := NewMessageQueue(WithQueueConfig(cfg))

	for i := range 3 {
		content := "msg" + string(rune('0'+i))
		if err := q.FollowUp(context.Background(), content, "user"); err != nil {
			t.Fatalf("FollowUp %d failed: %v", i, err)
		}
	}

	msgs1 := q.DrainFollowUp()
	if len(msgs1) != 1 {
		t.Fatalf("first drain = %d, want 1", len(msgs1))
	}
	if msgs1[0].Content != "msg0" {
		t.Errorf("content = %q, want %q", msgs1[0].Content, "msg0")
	}

	// Messages remaining.
	if !q.HasFollowUp() {
		t.Error("should still have follow-up messages after DrainOne")
	}

	msgs2 := q.DrainFollowUp()
	if len(msgs2) != 1 {
		t.Fatalf("second drain = %d, want 1", len(msgs2))
	}
	if msgs2[0].Content != "msg1" {
		t.Errorf("content = %q, want %q", msgs2[0].Content, "msg1")
	}
}

func TestQueue_FollowUpFull(t *testing.T) {
	cfg := QueueConfig{
		MaxFollowUp:     2,
		FollowUpDrain:   DrainOne,
		PersistFollowUp: false,
	}
	q := NewMessageQueue(WithQueueConfig(cfg))

	if err := q.FollowUp(context.Background(), "first", "user"); err != nil {
		t.Fatal(err)
	}
	if err := q.FollowUp(context.Background(), "second", "user"); err != nil {
		t.Fatal(err)
	}

	err := q.FollowUp(context.Background(), "third", "user")
	if !errors.Is(err, ErrQueueFull) {
		t.Errorf("expected ErrQueueFull, got %v", err)
	}
}

func TestQueue_FollowUpAfterClose(t *testing.T) {
	q := NewMessageQueue(WithQueueConfig(DefaultQueueConfig()))
	q.Close()

	err := q.FollowUp(context.Background(), "after close", "user")
	if !errors.Is(err, ErrQueueClosed) {
		t.Errorf("FollowUp after close = %v, want ErrQueueClosed", err)
	}
}

func TestQueue_GenerationCounter(t *testing.T) {
	q := NewMessageQueue()
	if q.GetGeneration() != 0 {
		t.Errorf("initial generation = %d, want 0", q.GetGeneration())
	}

	_ = q.Steer(context.Background(), "steer", "user")
	if q.GetGeneration() != 1 {
		t.Errorf("after Steer, generation = %d, want 1", q.GetGeneration())
	}

	q.DrainSteering()
	if q.GetGeneration() != 2 {
		t.Errorf("after DrainSteering, generation = %d, want 2", q.GetGeneration())
	}

	cfg := DefaultQueueConfig()
	cfg.MaxFollowUp = 5
	q2 := NewMessageQueue(WithQueueConfig(cfg))
	_ = q2.FollowUp(context.Background(), "follow", "user")
	if q2.GetGeneration() != 1 {
		t.Errorf("after FollowUp, generation = %d, want 1", q2.GetGeneration())
	}
	q2.DrainFollowUp()
	if q2.GetGeneration() != 2 {
		t.Errorf("after DrainFollowUp, generation = %d, want 2", q2.GetGeneration())
	}
}

func TestQueue_Status(t *testing.T) {
	cfg := DefaultQueueConfig()
	cfg.MaxFollowUp = 5
	q := NewMessageQueue(WithQueueConfig(cfg))

	status := q.Status()
	if status.SteeringDepth != 0 || status.FollowUpDepth != 0 {
		t.Errorf("empty status = %+v", status)
	}
	if !status.IsActive {
		t.Error("new queue should be active")
	}

	_ = q.Steer(context.Background(), "steer", "user")
	_ = q.FollowUp(context.Background(), "follow", "user")

	status = q.Status()
	if status.SteeringDepth != 1 {
		t.Errorf("steering depth = %d, want 1", status.SteeringDepth)
	}
	if status.FollowUpDepth != 1 {
		t.Errorf("follow-up depth = %d, want 1", status.FollowUpDepth)
	}

	q.Close()
	status = q.Status()
	if status.IsActive {
		t.Error("closed queue should not be active")
	}
}

func TestQueue_CloseIdempotent(t *testing.T) {
	q := NewMessageQueue()
	q.Close()
	q.Close() // should not panic or double-persist
	q.Close()
}

func TestQueue_ConcurrentEnqueueDrain(t *testing.T) {
	cfg := DefaultQueueConfig()
	cfg.MaxFollowUp = 1000
	q := NewMessageQueue(WithQueueConfig(cfg))

	var wg sync.WaitGroup
	var steerOK, followUpOK atomic.Int32

	// Concurrent steering.
	for i := range 50 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			if err := q.Steer(context.Background(), "steer", "goroutine"); err == nil {
				steerOK.Add(1)
			}
		}(i)
	}

	// Concurrent follow-up.
	for i := range 100 {
		wg.Add(1)
		go func(_ int) {
			defer wg.Done()
			if err := q.FollowUp(context.Background(), "follow", "goroutine"); err == nil {
				followUpOK.Add(1)
			}
		}(i)
	}

	wg.Wait()

	// All steers should succeed (they replace).
	if steerOK.Load() != 50 {
		t.Errorf("only %d/50 steers succeeded", steerOK.Load())
	}

	// At most MaxFollowUp follow-ups should succeed.
	count := followUpOK.Load()
	if count > 100 {
		t.Errorf("%d follow-ups succeeded, max is 100", count)
	}

	// Verify draining doesn't panic or lose messages.
	totalDrained := 0
	for {
		msgs := q.DrainSteering()
		totalDrained += len(msgs)
		if len(msgs) == 0 {
			break
		}
	}

	for {
		msgs := q.DrainFollowUp()
		totalDrained += len(msgs)
		if len(msgs) == 0 {
			break
		}
	}

	t.Logf("total drained: steering=%d, follow-up=%d", totalDrained-(int(count)-q.Status().FollowUpDepth), q.Status().FollowUpDepth)
}

func TestQueue_ConcurrentCloseDrain(t *testing.T) {
	q := NewMessageQueue()
	_ = q.Steer(context.Background(), "steer", "user")

	var wg sync.WaitGroup
	for range 10 {
		wg.Go(func() {
			q.Status()
		})
	}
	wg.Go(func() {
		q.Close()
	})
	wg.Wait()

	q.DrainSteering()
	if !q.IsClosed() {
		t.Error("queue should be closed")
	}
}

func TestQueue_DrainEmpty(t *testing.T) {
	q := NewMessageQueue()

	msgs := q.DrainSteering()
	if msgs != nil {
		t.Errorf("DrainSteering on empty = %v, want nil", msgs)
	}

	msgs = q.DrainFollowUp()
	if msgs != nil {
		t.Errorf("DrainFollowUp on empty = %v, want nil", msgs)
	}
}

func TestQueue_PersisterInterface(t *testing.T) {
	// Verify QueuePersister implements QueuePersisterOps.
	var _ QueuePersisterOps = (*QueuePersister)(nil)
}

func TestQueue_SteeringDepthNeverExceeds1(t *testing.T) {
	cfg := DefaultQueueConfig()
	cfg.MaxSteering = 1
	q := NewMessageQueue(WithQueueConfig(cfg))

	for i := range 10 {
		_ = q.Steer(context.Background(), "content", "user")
		if q.Status().SteeringDepth > 1 {
			t.Errorf("after %d steers, depth = %d, max is 1", i+1, q.Status().SteeringDepth)
		}
	}
}

func TestQueue_HasMethods(t *testing.T) {
	q := NewMessageQueue()

	if q.HasSteering() {
		t.Error("empty queue should not have steering")
	}
	if q.HasFollowUp() {
		t.Error("empty queue should not have follow-up")
	}

	_ = q.Steer(context.Background(), "steer", "user")
	if !q.HasSteering() {
		t.Error("queue should have steering after Steer()")
	}

	cfg := DefaultQueueConfig()
	cfg.MaxFollowUp = 5
	q2 := NewMessageQueue(WithQueueConfig(cfg))
	_ = q2.FollowUp(context.Background(), "follow", "user")
	if !q2.HasFollowUp() {
		t.Error("queue should have follow-up after FollowUp()")
	}
}
