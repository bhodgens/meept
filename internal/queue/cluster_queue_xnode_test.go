package queue

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"
)

// TestReclaimRemoteStale: node-b claims a job and dies (no heartbeat); when
// the claim timeout expires, node-a's ReclaimRemoteStale resets it to
// pending. This is the shared-store half of the cross-node reclaim path the
// daemon sweeper drives (cluster_queue.go StartReclaimSweeper).
func TestReclaimRemoteStale(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "queue.db")

	// Both "nodes" open the SAME store (the daemon wiring shares queue.db
	// across cluster nodes).
	store, err := NewStore(dbPath, logger)
	if err != nil {
		t.Fatalf("store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	pq, err := NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("persistent queue: %v", err)
	}
	t.Cleanup(func() { _ = pq.Close() })

	cqB := NewClusterQueue(pq, store, "node-b", logger, ClusterQueueConfig{
		DefaultClaimTimeout: 50 * time.Millisecond,
	})
	t.Cleanup(func() { _ = cqB.Close() })

	if err := pq.Enqueue(context.Background(), &Job{
		ID:       "remote-1",
		Type:     JobTypeOneOff,
		State:    StatePending,
		Priority: PriorityNormal,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}

	// node-b claims via the clustered path.
	job, err := cqB.Claim(context.Background(), "w-b", nil, "")
	if err != nil {
		t.Fatalf("node-b claim: %v", err)
	}
	if job.ClaimedByNode != "node-b" {
		t.Fatalf("claimed_by_node = %q, want node-b", job.ClaimedByNode)
	}

	// The row must carry the cluster ownership stamp.
	row := store.db.QueryRow(`SELECT claimed_by_node, timeout_at FROM jobs WHERE id = ?`, "remote-1")
	var node, timeoutAt string
	if err := row.Scan(&node, &timeoutAt); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if node != "node-b" || timeoutAt == "" {
		t.Fatalf("claim row not stamped: node=%q timeout_at=%q", node, timeoutAt)
	}

	// node-b dies: no heartbeat. Wait past timeout_at.
	time.Sleep(80 * time.Millisecond)

	// node-a's sweeper pass reclaims the remote stale claim.
	cqA := NewClusterQueue(pq, store, "node-a", logger)
	t.Cleanup(func() { _ = cqA.Close() })
	reclaimed, err := store.ReclaimRemoteStale(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("reclaim remote stale: %v", err)
	}
	if len(reclaimed) != 1 || reclaimed[0] != "remote-1" {
		t.Fatalf("reclaimed = %v, want [remote-1]", reclaimed)
	}

	// Job is claimable again - by node-a this time.
	job2, err := cqA.Claim(context.Background(), "w-a", nil, "")
	if err != nil {
		t.Fatalf("node-a re-claim: %v", err)
	}
	if job2.ID != "remote-1" || job2.ClaimedByNode != "node-a" {
		t.Fatalf("re-claimed %q by %q, want remote-1 by node-a", job2.ID, job2.ClaimedByNode)
	}

	// HeartbeatClaim keeps a live claim alive: extend, then verify the
	// remote sweep does NOT touch it.
	ok, err := store.HeartbeatClaim(context.Background(), "node-a", "remote-1", time.Second)
	if err != nil || !ok {
		t.Fatalf("heartbeat: ok=%v err=%v", ok, err)
	}
	reclaimed2, err := store.ReclaimRemoteStale(context.Background(), "node-a")
	if err != nil {
		t.Fatalf("second sweep: %v", err)
	}
	if len(reclaimed2) != 0 {
		t.Fatalf("heartbeat-kept claim was reclaimed: %v", reclaimed2)
	}
}

// TestClusteredClaimFallsBackWithoutStore: with a nil store, Claim still
// works via the embedded queue (single-node degraded mode).
func TestClusteredClaimFallsBackWithoutStore(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	dbPath := filepath.Join(t.TempDir(), "queue.db")
	pq, err := NewPersistentQueue(dbPath, nil, logger)
	if err != nil {
		t.Fatalf("persistent queue: %v", err)
	}
	t.Cleanup(func() { _ = pq.Close() })

	cq := NewClusterQueue(pq, nil, "solo", logger)
	t.Cleanup(func() { _ = cq.Close() })

	if err := pq.Enqueue(context.Background(), &Job{
		ID:       "solo-1",
		Type:     JobTypeOneOff,
		State:    StatePending,
		Priority: PriorityNormal,
	}); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	job, err := cq.Claim(context.Background(), "w", nil, "")
	if err != nil {
		t.Fatalf("claim without store: %v", err)
	}
	if job.ID != "solo-1" {
		t.Fatalf("claimed %q", job.ID)
	}
}
