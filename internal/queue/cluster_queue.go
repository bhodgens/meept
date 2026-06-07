package queue

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// GossipReachability provides node reachability info from the gossip engine.
type GossipReachability interface {
	IsNodeReachable(nodeID string) bool
}

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

	mu      sync.RWMutex
	claimed map[string]*ClaimRecord

	stopCh chan struct{}

	// gossipReachability provides node reachability info from the gossip engine.
	gossipReachability GossipReachability
}

// ClusterQueueConfig holds configuration for the distributed queue.
type ClusterQueueConfig struct {
	DefaultClaimTimeout     time.Duration
	NodeReachabilityTimeout time.Duration
	FullPayloadReplication  bool
}

// ClaimRecord tracks metadata for a locally claimed job.
type ClaimRecord struct {
	TaskID       string
	ClaimedBy    string
	ClaimedAt    time.Time
	TimeoutAt    time.Time
	IsReplica    bool
	ManagingNode string
}

// NodeReachabilityResult holds structured reachability info for a node.
type NodeReachabilityResult struct {
	NodeID      string
	IsReachable bool
	Reason      string
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

	cq := &ClusterQueue{
		Queue:       q,
		store:       store,
		localNodeID: localNodeID,
		logger:      logger,
		cfg:         config,
		claimed:     make(map[string]*ClaimRecord),
		stopCh:      make(chan struct{}),
	}

	// Apply optional functional options (e.g. WithGossipReachability).
	for _, o := range cfg {
		if ro, ok := any(o).(clusterQueueOpt); ok {
			ro.apply(cq)
		}
	}

	return cq
}

// clusterQueueOpt is a functional option for ClusterQueue.
type clusterQueueOpt interface {
	apply(*ClusterQueue)
}

// WithGossipReachability attaches a reachability provider to the cluster queue.
func WithGossipReachability(gr GossipReachability) clusterQueueOpt {
	return &reachabilityOpt{gr}
}

type reachabilityOpt struct{ gr GossipReachability }

func (o *reachabilityOpt) apply(cq *ClusterQueue) {
	cq.gossipReachability = o.gr
}

// WithMessageBus attaches a message bus to the cluster queue so that
// reclaim events can be published as cluster-wide TASK_RECLAIM bus events.
func WithMessageBus(b *bus.MessageBus) clusterQueueOpt {
	return &busOpt{b}
}

type busOpt struct{ b *bus.MessageBus }

func (o *busOpt) apply(cq *ClusterQueue) {
	cq.bus = o.b
}

// IsNodeReachable checks if a node is reachable using the gossip engine,
// falling back to assuming all nodes are reachable if no gossip provider is set.
func (cq *ClusterQueue) IsNodeReachable(nodeID string) bool {
	if nodeID == cq.localNodeID {
		return true
	}
	if cq.gossipReachability != nil {
		return cq.gossipReachability.IsNodeReachable(nodeID)
	}
	return true
}

// CheckNodeReachability checks the reachability of a single node and returns
// structured reachability info including the reason for unreachability.
func (cq *ClusterQueue) CheckNodeReachability(nodeID string) NodeReachabilityResult {
	if nodeID == cq.localNodeID {
		return NodeReachabilityResult{
			NodeID:         nodeID,
			IsReachable:    true,
			Reason:         "local node",
		}
	}
	if cq.gossipReachability != nil {
		reachable := cq.gossipReachability.IsNodeReachable(nodeID)
		reason := "reachable via gossip"
		if !reachable {
			reason = "unreachable via gossip"
		}
		return NodeReachabilityResult{
			NodeID:    nodeID,
			IsReachable: reachable,
			Reason:    reason,
		}
	}
	return NodeReachabilityResult{
		NodeID:    nodeID,
		IsReachable: true,
		Reason:    "no gossip provider - assuming reachable",
	}
}

// Claim wraps the underlying queue's Claim with cluster-aware logic.
func (cq *ClusterQueue) Claim(ctx context.Context, workerID string, caps []string) (*Job, error) {
	job, err := cq.Queue.Claim(ctx, workerID, caps)
	if err != nil {
		return nil, err
	}

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

// ClaimRemoteWithCheck is a reachability-aware claim variant. Before claiming
// a task managed by a remote node it verifies that the managing node is
// reachable; if it is not, the job is skipped (and eligible for reclaim by
// another node).
func (cq *ClusterQueue) ClaimRemoteWithCheck(ctx context.Context, workerID string, caps []string) (*Job, error) {
	pending, err := cq.Queue.(interface {
		ListByState(ctx context.Context, state JobState, limit int) ([]*Job, error)
	}).ListByState(ctx, StatePending, 50)
	if err != nil {
		return nil, err
	}

	for _, candidate := range pending {
		if !candidate.CanBeClaimedBy(caps) {
			continue
		}

		if candidate.ManagingNode != "" && candidate.ManagingNode != cq.localNodeID {
			if !cq.IsNodeReachable(candidate.ManagingNode) {
				cq.logger.Info("cluster_queue: skipping claim - managing node unreachable",
					"job_id", candidate.ID,
					"managing_node", candidate.ManagingNode,
				)
				cq.Reclaim(candidate.ID)
				continue
			}
		}

		claimed, err := cq.Queue.Claim(ctx, workerID, caps)
		if err != nil {
			continue
		}

		cq.mu.Lock()
		cq.claimed[claimed.ID] = &ClaimRecord{
			TaskID:       claimed.ID,
			ClaimedBy:    claimed.ClaimedBy,
			ClaimedAt:    time.Now().UTC(),
			TimeoutAt:    time.Now().UTC().Add(cq.cfg.DefaultClaimTimeout),
			IsReplica:    false,
			ManagingNode: candidate.ManagingNode,
		}
		cq.mu.Unlock()

		return claimed, nil
	}

	return nil, ErrNoJobAvailable
}

// Complete marks a job as completed and synchronizes the event to the cluster.
func (cq *ClusterQueue) Complete(ctx context.Context, jobID string, result any) error {
	cq.mu.Lock()
	record, ok := cq.claimed[jobID]
	cq.mu.Unlock()

	if ok {
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

// IsClaimed checks if a job is currently claimed by any node.
func (cq *ClusterQueue) IsClaimed(ctx context.Context, jobID string) (string, bool) {
	cq.mu.RLock()
	defer cq.mu.RUnlock()
	if record, ok := cq.claimed[jobID]; ok {
		return record.ClaimedBy, true
	}
	return "", false
}

// Reclaim re-enqueues a stale or unreachable-node job so another node can take it over.
// It records a TASK_RECLAIM cluster event and resets the job state to pending.
func (cq *ClusterQueue) Reclaim(jobID string) error {
	cq.mu.Lock()
	delete(cq.claimed, jobID)
	cq.mu.Unlock()

	return cq.reclaimInternal(jobID, "node_timeout", nil)
}

// Reclaim resets a stale job's state back to pending so that another node
// can claim it. It clears the local claim record, updates the store, and
// publishes a TASK_RECLAIM bus event for cluster-wide awareness.
func (cq *ClusterQueue) ReclaimJob(job *Job) error {
	cq.mu.Lock()
	delete(cq.claimed, job.ID)
	cq.mu.Unlock()

	return cq.reclaimInternal(job.ID, "stale_claim", job)
}

// reclaimInternal is the shared implementation that resets job state in the store,
// records a TASK_RECLAIM event, and publishes the event to the bus.
func (cq *ClusterQueue) reclaimInternal(jobID, reason string, job *Job) error {
	if cq.store != nil {
		if err := cq.store.RecordReclaimEvent(context.Background(), jobID, cq.localNodeID, reason); err != nil {
			cq.logger.Warn("cluster_queue: failed to record reclaim event", "job_id", jobID, "error", err)
		}
	}

	if cq.store != nil {
		if err := cq.store.UpdateState(jobID, StatePending); err != nil {
			cq.logger.Error("cluster_queue: failed to reset job state during reclaim",
				"job_id", jobID, "error", err)
			return err
		}
	}

	// Publish TASK_RECLAIM cluster event via the message bus.
	cq.publishReclaimEvent(jobID, reason, job)

	cq.logger.Info("cluster_queue: job reclaimed", "job_id", jobID, "node", cq.localNodeID)
	return nil
}

// publishReclaimEvent publishes a TASK_RECLAIM event to the message bus so
// that cluster-wide consumers (agent loop, other nodes) are notified of the reclaim.
func (cq *ClusterQueue) publishReclaimEvent(jobID, reason string, job *Job) {
	if cq.bus == nil {
		return
	}
	data := map[string]any{
		"job_id":       jobID,
		"reason":       reason,
		"reclaimed_by": cq.localNodeID,
	}
	if job != nil {
		data["job_type"] = string(job.Type)
		data["task_id"] = job.TaskID
		data["agent_id"] = job.AgentID
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "cluster_queue", data)
	if err != nil {
		cq.logger.Error("cluster_queue: failed to create bus message for reclaim",
			"job_id", jobID, "error", err)
		return
	}
	cq.bus.Publish("event.cluster.task_reclaim", msg)
}

// ErrNodeUnreachable is returned when a managing node is unreachable.
var ErrNodeUnreachable = fmt.Errorf("managing node is not reachable")

// IsStale reports whether a ClaimRecord has exceeded its timeout.
func (r *ClaimRecord) IsStale() bool {
	return time.Now().After(r.TimeoutAt)
}

// ReclaimIfStale detects locally tracked claims that have exceeded their
// timeout and reclaims them by calling [Reclaim].
//
// Returns the list of jobs that were successfully reclaimed.
func (cq *ClusterQueue) ReclaimIfStale(ctx context.Context) []*Job {
	cq.mu.Lock()
	collect := make([]*ClaimRecord, 0)
	for jobID, rec := range cq.claimed {
		if rec.IsStale() {
			collect = append(collect, rec)
			delete(cq.claimed, jobID)
		}
	}
	cq.mu.Unlock()

	var reclaimed []*Job
	for _, rec := range collect {
		if err := cq.Reclaim(rec.TaskID); err != nil {
			cq.logger.Error("cluster_queue: reclaim failed",
				"job_id", rec.TaskID,
				"claimed_by", rec.ClaimedBy,
				"error", err,
			)
			continue
		}
		cq.logger.Info("cluster_queue: reclaimed stale job",
			"job_id", rec.TaskID,
			"claimed_by", rec.ClaimedBy,
		)
		reclaimed = append(reclaimed, &Job{ID: rec.TaskID})
	}
	return reclaimed
}

// RunReclaimLoop starts a background goroutine that periodically:
//  1. Checks node reachability via the gossip engine.
//  2. Reclaims any jobs whose managing node is unreachable.
//  3. Reclaims any locally tracked claims whose timeout has expired.
//
// The loop stops when [Close] is called or the context is cancelled.
func (cq *ClusterQueue) RunReclaimLoop(ctx context.Context) {
	tickInterval := cq.cfg.NodeReachabilityTimeout / 2
	if tickInterval < 5*time.Second {
		tickInterval = 5 * time.Second
	}

	ticker := time.NewTicker(tickInterval)
	defer ticker.Stop()

	cq.logger.Info("cluster_queue: reclaim loop started", "interval", tickInterval)

	for {
		select {
		case <-ctx.Done():
			cq.logger.Info("cluster_queue: reclaim loop stopping", "reason", "context_done")
			return
		case <-cq.stopCh:
			cq.logger.Info("cluster_queue: reclaim loop stopping", "reason", "close_requested")
			return
		case <-ticker.C:
			cq.runReclaimTick(ctx)
		}
	}
}

func (cq *ClusterQueue) runReclaimTick(ctx context.Context) {
	// 1. Check stale claim timeouts.
	staleJobs := cq.ReclaimIfStale(ctx)
	for _, j := range staleJobs {
		cq.logger.Info("cluster_queue: stale claim reclaimed", "job_id", j.ID)
	}

	// 2. Check node reachability and reclaim jobs from unreachable nodes.
	if cq.store == nil {
		return
	}

	members, err := cq.store.GetInactiveNodes(ctx, cq.cfg.NodeReachabilityTimeout)
	if err != nil {
		cq.logger.Debug("cluster_queue: failed to fetch inactive nodes", "error", err)
		return
	}
	if len(members) == 0 {
		return
	}

	cq.mu.Lock()
	for nodeID := range members {
		cq.logger.Info("cluster_queue: node unreachable, scanning its jobs", "node", nodeID)
		for jobID, record := range cq.claimed {
			if record.ManagingNode == nodeID {
				cq.logger.Info("cluster_queue: reclaiming job from unreachable node",
					"job_id", jobID, "managing_node", nodeID)
				delete(cq.claimed, jobID)
				go func(id string) { _ = cq.Reclaim(id) }(jobID)
			}
		}
	}
	cq.mu.Unlock()

	// 3. Also scan remote-managed pending jobs in the store.
	for nodeID := range members {
		pending, err2 := cq.store.ListPendingByManagingNode(ctx, nodeID)
		if err2 != nil {
			continue
		}
		for _, j := range pending {
			cq.logger.Info("cluster_queue: reclaiming remote job from unreachable node",
				"job_id", j.ID, "managing_node", nodeID)
			go func(id string) { _ = cq.Reclaim(id) }(j.ID)
		}
	}
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

// Close releases resources held by the cluster queue.
func (cq *ClusterQueue) Close() error {
	close(cq.stopCh)
	return nil
}

// ClusterQueueStats holds statistics about the distributed queue.
type ClusterQueueStats struct {
	LocalClaims int    `json:"local_claims"`
	LocalNode   string `json:"local_node"`
}

// RecordClaimEvent stores a claim lifecycle event in the cluster events table.
func (s *Store) RecordClaimEvent(ctx context.Context, jobID, nodeID, action string) error {
	eventID := fmt.Sprintf("claim-%s-%s-%d", jobID, action, time.Now().UnixNano())
	body := fmt.Sprintf(`{"job_id":"%s","action":"%s"}`, jobID, action)
	sig := []byte(action) // placeholder: real signatures via ed25519

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cluster_events (event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		eventID, nodeID, "TASK_"+action,
		time.Now().UnixNano(),
		`{}`,
		[]byte(body), sig, time.Now().UnixNano(),
	)
	return err
}

// RecordReclaimEvent stores a TASK_RECLAIM cluster event in the cluster_events table.
func (s *Store) RecordReclaimEvent(ctx context.Context, jobID, reclaimerNode, reason string) error {
	payload, _ := json.Marshal(map[string]string{
		"job_id":     jobID,
		"reason":     reason,
		"reclaimed_by": reclaimerNode,
	})
	sig := []byte(reason) // placeholder: real signatures via ed25519

	_, err := s.db.ExecContext(ctx, `
		INSERT INTO cluster_events (event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		fmt.Sprintf("reclaim-%s-%d", jobID, time.Now().UnixNano()),
		reclaimerNode, "TASK_RECLAIM",
		time.Now().UnixNano(),
		`{}`,
		payload, sig, time.Now().UnixNano(),
	)
	return err
}

// GetInactiveNodes returns a map of node IDs from cluster_members that have not
// sent a heartbeat within the given timeout duration.
func (s *Store) GetInactiveNodes(ctx context.Context, timeout time.Duration) (map[string]struct{}, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT node_id FROM cluster_members
		WHERE status = 'active'
		  AND last_heartbeat > 0
		  AND datetime(last_heartbeat, 'unixepoch') <= datetime('now', ?)
	`, fmt.Sprintf("-%d seconds", int(timeout.Seconds())))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	inactive := make(map[string]struct{})
	for rows.Next() {
		var nodeID string
		if err := rows.Scan(&nodeID); err != nil {
			continue
		}
		inactive[nodeID] = struct{}{}
	}
	return inactive, rows.Err()
}

// ListPendingByManagingNode returns pending jobs whose managing_node matches the
// given node ID. Used to discover jobs eligible for reclaim when a node goes down.
func (s *Store) ListPendingByManagingNode(ctx context.Context, managingNode string) ([]*Job, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, type, payload, priority, managing_node, claimed_by_node, timeout_at
		FROM jobs
		WHERE state = 'pending' AND managing_node = ?
	`, managingNode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []*Job
	for rows.Next() {
		var j Job
		var payload, mgmt, claimedBy, timeoutAt string
		var priority int
		if err := rows.Scan(&j.ID, (*string)(&j.Type), &payload, &priority, &mgmt, &claimedBy, &timeoutAt); err != nil {
			continue
		}
		j.Priority = Priority(priority)
		j.State = StatePending
		j.Payload = json.RawMessage(payload)
		if mgmt != "" {
			j.ManagingNode = mgmt
		}
		jobs = append(jobs, &j)
	}
	return jobs, rows.Err()
}
