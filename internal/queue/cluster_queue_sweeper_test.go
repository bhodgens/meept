package queue

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// TestStartReclaimSweeperReclaimsStale: a claim whose worker never completes
// is reclaimed by the sweeper after its timeout expires, and the job returns
// to claimable state.
func TestStartReclaimSweeperReclaimsStale(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "queue.db")

	pq, err := NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("persistent queue: %v", err)
	}
	t.Cleanup(func() { _ = pq.Close() })

	store, err := NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })

	cq := NewClusterQueue(pq, store, "node-test", logger, ClusterQueueConfig{
		DefaultClaimTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = cq.Close() })

	if err := pq.Enqueue(context.Background(), &Job{
		ID:       "stale-1",
		Type:     JobTypeOneOff,
		State:    StatePending,
		Priority: PriorityNormal,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	job, err := cq.Claim(context.Background(), "w-crash", nil, "")
	if err != nil {
		t.Fatalf("claim: %v", err)
	}
	if job.ID != "stale-1" {
		t.Fatalf("claimed %q", job.ID)
	}

	cq.StartReclaimSweeper()
	cq.StartReclaimSweeper() // idempotent: second call is a no-op

	// The sweeper ticks at DefaultClaimTimeout (50ms); allow several ticks.
	deadline := time.Now().Add(3 * time.Second)
	var reclaimed *Job
	for time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
		if got, err := pq.Get(context.Background(), "stale-1"); err == nil && got.State == StatePending {
			reclaimed = got
			break
		}
	}
	if reclaimed == nil {
		t.Fatal("sweeper did not reclaim the stale claim within 3s")
	}

	// The reclaimed job is claimable again.
	job2, err := cq.Claim(context.Background(), "w-2", nil, "")
	if err != nil {
		t.Fatalf("re-claim after reclaim: %v", err)
	}
	if job2.ID != "stale-1" {
		t.Fatalf("re-claimed %q, want stale-1", job2.ID)
	}
}
