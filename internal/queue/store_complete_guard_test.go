package queue

// B2 stale-completion guard tests: Store.Complete must only transition a job
// from 'claimed'/'processing' to 'completed'. Jobs keep the same ID across
// Retry/Requeue, so a stale or duplicate completion event for a requeued job
// (back in 'pending') must fail with the ErrJobNotClaimable sentinel instead
// of overwriting the earlier result or pretending a second success.

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// completeableJob inserts a job and moves it into the requested pre-complete
// state, mirroring the real lifecycle paths:
//   - claimed/processing: Claim (+ MarkProcessing)
//   - pending: fresh insert (or post-Requeue shape)
//   - failed: Fail
func completeableJob(t *testing.T, s *Store, state JobState) *Job {
	t.Helper()

	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "b2"})
	if err := s.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}

	switch state {
	case StateClaimed:
		if _, err := s.ClaimNextForAgent("worker-b2", nil, ""); err != nil {
			t.Fatalf("Claim failed: %v", err)
		}
	case StateProcessing:
		if _, err := s.ClaimNextForAgent("worker-b2", nil, ""); err != nil {
			t.Fatalf("Claim failed: %v", err)
		}
		if err := s.UpdateState(job.ID, StateProcessing); err != nil {
			t.Fatalf("MarkProcessing failed: %v", err)
		}
	case StateFailed:
		if _, err := s.ClaimNextForAgent("worker-b2", nil, ""); err != nil {
			t.Fatalf("Claim failed: %v", err)
		}
		if err := s.Fail(job.ID, "boom"); err != nil {
			t.Fatalf("Fail failed: %v", err)
		}
	case StatePending:
		// fresh insert (or post-Requeue shape)
	default:
		t.Fatalf("unsupported setup state: %s", state)
	}
	return job
}

// TestStoreComplete_StateGuard is table-driven over the legal and illegal
// pre-complete states.
func TestStoreComplete_StateGuard(t *testing.T) {
	tests := []struct {
		name    string
		state   JobState
		wantErr bool
	}{
		{"claimed is completable", StateClaimed, false},
		{"processing is completable", StateProcessing, false},
		{"pending is not completable", StatePending, true},
		{"failed is not completable", StateFailed, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newTestStore(t, "")
			job := completeableJob(t, store, tt.state)

			err := store.Complete(job.ID, map[string]string{"result": "done"})
			if tt.wantErr {
				if !errors.Is(err, ErrJobNotClaimable) {
					t.Fatalf("Complete from %s: err = %v, want ErrJobNotClaimable", tt.state, err)
				}
				return
			}
			if err != nil {
				t.Fatalf("Complete from %s failed: %v", tt.state, err)
			}
		})
	}
}

// TestStoreComplete_DuplicateCompletionFails: completing twice must fail the
// second call with the sentinel AND must not overwrite the first result.
func TestStoreComplete_DuplicateCompletionFails(t *testing.T) {
	store := newTestStore(t, "")
	job := completeableJob(t, store, StateClaimed)

	if err := store.Complete(job.ID, map[string]string{"result": "first"}); err != nil {
		t.Fatalf("first Complete failed: %v", err)
	}

	err := store.Complete(job.ID, map[string]string{"result": "STALE"})
	if !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("second Complete: err = %v, want ErrJobNotClaimable", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StateCompleted {
		t.Errorf("state = %q, want %q", got.State, StateCompleted)
	}
	if string(got.Result) != `{"result":"first"}` {
		t.Errorf("result = %s, want the first result preserved verbatim", got.Result)
	}
}

// TestStoreComplete_RequeuedJobRejectsStaleCompletion: the B2 scenario — a
// claimed job is requeued (same ID, back to pending); a late completion for
// it must be rejected.
func TestStoreComplete_RequeuedJobRejectsStaleCompletion(t *testing.T) {
	store := newTestStore(t, "")
	job := completeableJob(t, store, StateClaimed)

	notBefore := time.Now().UTC().Add(time.Minute)
	if err := store.Requeue(job.ID, notBefore); err != nil {
		t.Fatalf("Requeue failed: %v", err)
	}

	err := store.Complete(job.ID, map[string]string{"result": "stale attempt 1"})
	if !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("Complete on requeued job: err = %v, want ErrJobNotClaimable", err)
	}

	got, err := store.GetByID(job.ID)
	if err != nil {
		t.Fatalf("GetByID failed: %v", err)
	}
	if got.State != StatePending {
		t.Errorf("state = %q, want %q (requeued job must stay pending)", got.State, StatePending)
	}
	if got.Result != nil {
		t.Errorf("result = %s, want nil (stale result must not be stored)", got.Result)
	}
}

// TestPersistentQueue_DuplicateCompletePublishesOnce: the queue-level Complete
// must not publish queue.job.completed when store.Complete rejects the
// transition, so exactly one event reaches subscribers.
func TestPersistentQueue_DuplicateCompletePublishesOnce(t *testing.T) {
	msgBus := bus.New(nil, nil)
	q, err := NewPersistentQueue(filepath.Join(t.TempDir(), "b2.db"), msgBus, nil)
	if err != nil {
		t.Fatalf("NewPersistentQueue failed: %v", err)
	}
	t.Cleanup(func() { q.Close() })

	ctx := context.Background()
	job := mustNewJob(t, JobTypeOneOff, map[string]string{"prompt": "once"})
	if err := q.store.Insert(job); err != nil {
		t.Fatalf("Insert failed: %v", err)
	}
	if _, err := q.Claim(ctx, "worker-b2", nil, ""); err != nil {
		t.Fatalf("Claim failed: %v", err)
	}

	sub := msgBus.Subscribe("test-b2-once", "queue.job.completed")
	defer msgBus.Unsubscribe(sub)

	if err := q.Complete(ctx, job.ID, map[string]string{"result": "done"}); err != nil {
		t.Fatalf("first Complete failed: %v", err)
	}
	if err := q.Complete(ctx, job.ID, map[string]string{"result": "STALE"}); !errors.Is(err, ErrJobNotClaimable) {
		t.Fatalf("second Complete: err = %v, want ErrJobNotClaimable", err)
	}

	// Exactly one event.
	select {
	case <-sub.Channel:
		// first (correct) event received
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for the first queue.job.completed event")
	}
	select {
	case msg := <-sub.Channel:
		t.Fatalf("unexpected second queue.job.completed event: payload keys %+v", msg.Payload)
	case <-time.After(300 * time.Millisecond):
		// expected: no duplicate event
	}
}
