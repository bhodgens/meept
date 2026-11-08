package worker

// SetQueue tests: the cluster wiring swaps the pool's queue onto the
// ClusterQueue after pool construction (daemon.go ordering: NewComponents
// creates the pool, wireClusterResources creates the ClusterQueue). See
// Pool.SetQueue and internal/queue/cluster_queue.go for the contract.

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/queue"
)

func TestSetQueueSwapsFutureWorkerClaims(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "queue.db")

	pq, err := queue.NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("persistent queue: %v", err)
	}
	t.Cleanup(func() { _ = pq.Close() })

	pool, err := NewPool(PoolConfig{
		Queue:     pq,
		Processor: noopProcessor{},
		Logger:    logger,
	})
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(func() { _ = pool.Stop(context.Background()) })

	// Precondition: pool starts on the raw persistent queue.
	if pool.queue != queue.Queue(pq) {
		t.Fatal("precondition: pool queue should be the persistent queue")
	}

	clusterQ := queue.NewClusterQueue(pq, nil, "node-test", logger)
	pool.SetQueue(clusterQ)

	if pool.queue != queue.Queue(clusterQ) {
		t.Fatal("SetQueue did not swap the pool queue")
	}

	// A job claimed AFTER the swap goes through the ClusterQueue path:
	// the claim must be recorded in the cluster claim set.
	job := &queue.Job{
		ID:       "job-1",
		Type:     queue.JobTypeOneOff,
		State:    queue.StatePending,
		Priority: queue.PriorityNormal,
	}
	if err := pq.Enqueue(context.Background(), job); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err2 := clusterQ.Claim(context.Background(), "w-post-swap", nil, "")
	if err2 != nil {
		t.Fatalf("claim via cluster queue: %v", err2)
	}
	if job.ID != "job-1" {
		t.Fatalf("claimed %q, want job-1", job.ID)
	}
	stats, err := clusterQ.ClusterStats(context.Background())
	if err != nil {
		t.Fatalf("cluster stats: %v", err)
	}
	if stats.LocalClaims != 1 {
		t.Fatalf("LocalClaims = %d, want 1 (claim bookkeeping active)", stats.LocalClaims)
	}

	// Nil and same-queue calls are no-ops.
	pool.SetQueue(nil)
	if pool.queue != queue.Queue(clusterQ) {
		t.Fatal("SetQueue(nil) must be a no-op")
	}
	pool.SetQueue(clusterQ)
	if pool.queue != queue.Queue(clusterQ) {
		t.Fatal("SetQueue(same) must be a no-op")
	}
}

// TestSetQueueClusterQueueSatisfiesQueue pins the interface swap the wiring
// depends on: ClusterQueue must satisfy queue.Queue (the pre-wiring code
// shadowed Stats with a mismatched signature, which silently broke this).
func TestClusterQueueSatisfiesQueueInterface(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	pq, err := queue.NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("persistent queue: %v", err)
	}
	t.Cleanup(func() { _ = pq.Close() })

	var q queue.Queue = queue.NewClusterQueue(pq, nil, "node-test", logger)
	cq, concrete := q.(*queue.ClusterQueue)
	if !concrete || cq == nil {
		t.Fatal("ClusterQueue does not satisfy queue.Queue")
	}
}
