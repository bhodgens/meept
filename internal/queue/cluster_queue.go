package queue

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// ClusterQueue wraps the standard queue with cluster-aware claim/timeout/reclaim logic.
// It coordinates with the gossip engine to replicate task events across nodes and
// handles node failure by reclaiming tasks from unreachable workers.
type ClusterQueue struct {
	Queue
	store       *Store
	localNodeID string
	logger      *slog.Logger
	cfg         ClusterQueueConfig
	bus         *bus.MessageBus

	mu        sync.RWMutex
	claimed   map[string]*ClaimRecord
	stopCh    chan struct{}
	closeOnce sync.Once
}

// ClusterQueueConfig holds configuration for the distributed queue.
type ClusterQueueConfig struct {
	DefaultClaimTimeout     time.Duration
	NodeReachabilityTimeout time.Duration
	FullPayloadReplication  bool
}

// ClaimRecord tracks metadata for a locally claimed job.
type ClaimRecord struct {
	TaskID      string
	ClaimedBy   string
	ClaimedAt   time.Time
	TimeoutAt   time.Time
	IsReplica   bool
	ManagingNode string
}

// DefaultClusterQueueConfig returns a config with sensible defaults.
func DefaultClusterQueueConfig() ClusterQueueConfig {
	return ClusterQueueConfig{
		DefaultClaimTimeout:     5 * time.Minute,
		NodeReachabilityTimeout: 2 * time.Minute,
		FullPayloadReplication:  false,
	}
}

// NewClusterQueue creates a new cluster-aware queue.
func NewClusterQueue(q Queue, store *Store, localNodeID string, logger *slog.Logger, cfg ...ClusterQueueConfig) *ClusterQueue {
	config := DefaultClusterQueueConfig()
	if len(cfg) > 0 {
		config = cfg[0]
	}

	return &ClusterQueue{
		Queue:       q,
		store:       store,
		localNodeID: localNodeID,
		logger:      logger,
		cfg:         config,
		claimed:     make(map[string]*ClaimRecord),
		stopCh:      make(chan struct{}),
	}
}

// WithMessageBus attaches a message bus to the cluster queue
// for publishing cluster-wide reclaim events.
func (cq *ClusterQueue) WithMessageBus(b *bus.MessageBus) {
	cq.bus = b
}

// Claim wraps the underlying queue's Claim with cluster-aware logic.
func (cq *ClusterQueue) Claim(ctx context.Context, workerID string, caps []string) (*Job, error) {
	// Check if this job was previously claimed by another node
	// If not, attempt local claim
	job, err := cq.Queue.Claim(ctx, workerID, caps)
	if err != nil {
		return nil, err
	}

	// Record the claim locally
	cq.mu.Lock()
	cq.claimed[job.ID] = &ClaimRecord{
		TaskID:       job.ID,
		ClaimedBy:    cq.localNodeID,
		ClaimedAt:    time.Now().UTC(),
		TimeoutAt:    time.Now().UTC().Add(cq.cfg.DefaultClaimTimeout),
		IsReplica:    false,
		ManagingNode: cq.localNodeID,
	}
	cq.mu.Unlock()

	return job, nil
}

// Complete marks a job as completed and synchronizes the event to the cluster.
func (cq *ClusterQueue) Complete(ctx context.Context, jobID string, result any) error {
	// Record the claim record before completing
	cq.mu.Lock()
	record, ok := cq.claimed[jobID]
	cq.mu.Unlock()

	if ok {
		// Write to cluster events store
		if cq.store != nil {
			if err := cq.store.RecordClaimEvent(ctx, jobID, record.ClaimedBy, "complete"); err != nil {
				cq.logger.Warn("cluster_queue: failed to record claim event", "job_id", jobID, "error", err)
			}
		}
	}

	return cq.Queue.Complete(ctx, jobID, result)
}

// Fail marks a job as failed and synchronizes the event to the cluster.
func (cq *ClusterQueue) Fail(ctx context.Context, jobID string, err error) error {
	cq.mu.Lock()
	record, ok := cq.claimed[jobID]
	cq.mu.Unlock()

	if ok && cq.store != nil {
		if err2 := cq.store.RecordClaimEvent(ctx, jobID, record.ClaimedBy, "fail"); err2 != nil {
			cq.logger.Warn("cluster_queue: failed to record claim event", "job_id", jobID, "error", err2)
		}
	}

	return cq.Queue.Fail(ctx, jobID, err)
}

// reclaimJobUnlocked reclaims a claimed job back to pending state.
// The caller must already hold cq.mu (write lock).
func (cq *ClusterQueue) reclaimJobUnlocked(ctx context.Context, jobID, reason string) error {
	// 1. Record TASK_RECLAIM event in cluster_events table
	if cq.store != nil {
		if err := cq.store.RecordClaimEvent(ctx, jobID, cq.localNodeID, "reclaim"); err != nil {
			cq.logger.Warn("cluster_queue: failed to record reclaim event",
				"job_id", jobID, "reason", reason, "error", err)
		}
	}

	// 2. Reset job state to PENDING in the store
	if cq.store != nil {
		if err := cq.store.ResetToPending(ctx, jobID); err != nil {
			cq.logger.Warn("cluster_queue: failed to reset job to pending",
				"job_id", jobID, "reason", reason, "error", err)
			// Non-fatal: other nodes still learned about the reclaim via the event.
		}
	} else {
		cq.logger.Warn("cluster_queue: store is nil, skipping ResetToPending",
			"job_id", jobID, "reason", reason)
	}

	// 3. Remove local claim record (caller holds the lock, no need to acquire)
	delete(cq.claimed, jobID)

	// 4. Publish bus event so subscribers across the cluster are notified
	if cq.bus != nil {
		msg, err := models.NewBusMessage(models.MessageTypeEvent, "cluster_queue", map[string]any{
			"job_id":  jobID,
			"reason":  reason,
			"node_id": cq.localNodeID,
		})
		if err == nil {
			cq.bus.Publish("event.cluster.task_reclaim", msg)
		}
	}

	cq.logger.Info("cluster_queue: job reclaimed",
		"job_id", jobID,
		"reason", reason,
		"reclaimed_by", cq.localNodeID,
	)

	return nil
}

// ReclaimJob reclaims a claimed job back to pending state due to node failure
// or claim timeout. It records the reclaim event, resets the job state in the
// store, removes the local claim record, and publishes a bus event so other
// nodes are aware the job needs to be re-handled.
func (cq *ClusterQueue) ReclaimJob(ctx context.Context, jobID, reason string) error {
	cq.mu.Lock()
	defer cq.mu.Unlock()
	return cq.reclaimJobUnlocked(ctx, jobID, reason)
}

// CheckNodeReachability checks if a node is reachable by querying the
// cluster_members table for a recent heartbeat. Returns true if the node
// has heartbeat within the configured reachability timeout.
func (cq *ClusterQueue) CheckNodeReachability(ctx context.Context, nodeID string) bool {
	if cq.store == nil {
		return false
	}

	var lastHeartbeat int64
	row := cq.store.db.QueryRowContext(ctx,
		`SELECT last_heartbeat FROM cluster_members WHERE node_id = ?`,
		nodeID,
	)
	if err := row.Scan(&lastHeartbeat); err != nil {
		// Node not found in cluster_members — treat as unreachable.
		return false
	}

	threshold := time.Now().UnixNano() - int64(cq.cfg.NodeReachabilityTimeout)
	return lastHeartbeat > threshold
}

// IsClaimed checks if a job is currently claimed by any node.
func (cq *ClusterQueue) IsClaimed(ctx context.Context, jobID string) (string, bool) {
	cq.mu.RLock()
	defer cq.mu.RUnlock()
	if record, ok := cq.claimed[jobID]; ok {
		return record.ClaimedBy, true
	}
	return "", false
}

// ReclaimIfStale checks all locally tracked claims and reclaims any whose
// timeout has expired. It collects stale job IDs under a brief RLock, releases
// the lock, then reclaims each job individually under its own Lock to avoid
// holding the write lock across slow I/O (store, bus publish).
func (cq *ClusterQueue) ReclaimIfStale(ctx context.Context) []*Job {
	now := time.Now()

	// Snapshot stale job IDs under RLock
	cq.mu.RLock()
	var staleIDs []string
	for jobID, record := range cq.claimed {
		if now.After(record.TimeoutAt) {
			staleIDs = append(staleIDs, jobID)
		}
	}
	cq.mu.RUnlock()

	// Reclaim each job individually under its own Lock
	var reclaiming []*Job
	for _, jobID := range staleIDs {
		reclaiming = append(reclaiming, &Job{ID: jobID})
		if err := cq.ReclaimJob(ctx, jobID, "claim_stale"); err != nil {
			cq.logger.Warn("cluster_queue: reclaim_if_stale: reclaim failed",
				"job_id", jobID, "error", err)
		}
	}

	return reclaiming
}

// Stats returns cluster-queue statistics.
func (cq *ClusterQueue) Stats(ctx context.Context) (*ClusterQueueStats, error) {
	cq.mu.RLock()
	defer cq.mu.RUnlock()

	return &ClusterQueueStats{
		LocalClaims: len(cq.claimed),
		LocalNode:   cq.localNodeID,
	}, nil
}

// Close releases resources held by the cluster queue. It is safe to call
// multiple times; only the first call closes the stop channel.
func (cq *ClusterQueue) Close() error {
	cq.closeOnce.Do(func() {
		close(cq.stopCh)
	})
	return nil
}

// ClusterQueueStats holds statistics about the distributed queue.
type ClusterQueueStats struct {
	LocalClaims int    `json:"local_claims"`
	LocalNode   string `json:"local_node"`
}

// RecordClaimEvent stores a claim lifecycle event in the cluster events table.
func (s *Store) RecordClaimEvent(ctx context.Context, jobID, nodeID, action string) error {
	query := `
		INSERT INTO cluster_events (event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	eventID := id.Generate("claim-")
	body := fmt.Sprintf(`{"job_id":"%s","action":"%s"}`, jobID, action)
	sig := []byte(action) // placeholder: real signatures via ed25519

	_, err := s.db.ExecContext(ctx, query,
		eventID, nodeID, "TASK_"+action,
		time.Now().UnixNano(),
		`{}`,
		[]byte(body), sig, time.Now().UnixNano(),
	)
	return err
}
