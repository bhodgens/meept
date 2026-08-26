package daemon

import (
	"context"
	"sync/atomic"
	"testing"
	"time"
)

// TestDrainCompletesBeforeDeadline: a long job that finishes mid-drain lets
// the drain return well before the deadline.
func TestDrainCompletesBeforeDeadline(t *testing.T) {
	var done atomic.Bool
	go func() {
		time.Sleep(50 * time.Millisecond)
		done.Store(true)
	}()
	count := func(context.Context) (int, error) {
		if done.Load() {
			return 0, nil
		}
		return 1, nil
	}
	start := time.Now()
	DrainRunningJobs(context.Background(), count, 5*time.Second)
	if elapsed := time.Since(start); elapsed > 4*time.Second {
		t.Fatalf("drain did not exit early after completion: %v", elapsed)
	}
}

// TestDrainKillsAtDeadline: a job that never completes forces the drain to
// give up at the deadline.
func TestDrainKillsAtDeadline(t *testing.T) {
	count := func(context.Context) (int, error) { return 1, nil }
	start := time.Now()
	DrainRunningJobs(context.Background(), count, 100*time.Millisecond)
	if elapsed := time.Since(start); elapsed < 90*time.Millisecond || elapsed > 3*time.Second {
		t.Fatalf("drain did not respect deadline: %v", elapsed)
	}
}

// TestDrainRespectsContextCancellation verifies the review-checklist item.
func TestDrainRespectsContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	count := func(context.Context) (int, error) { return 1, nil }
	go func() {
		time.Sleep(30 * time.Millisecond)
		cancel()
	}()
	start := time.Now()
	DrainRunningJobs(ctx, count, 30*time.Second)
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("drain ignored context cancellation: %v", elapsed)
	}
}
