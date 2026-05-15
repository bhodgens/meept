package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/memvid"
	"github.com/caimlas/meept/pkg/models"
)

// Operation type constants.
const (
	OperationDistill = "distill"
)

// SharedZone is the memvid zone for cross-agent shared memories.
const SharedZone = "shared"

// SyncManager orchestrates memory synchronization between local SQLite storage
// and shared memvid storage. It handles:
// - Hydration: Fetching relevant memories from shared storage when a job is claimed
// - Distillation: Promoting high-value local memories to shared storage on job completion
type SyncManager struct { //nolint:revive // stutter is intentional
	config    config.DistributedMemoryConfig
	localMgr  *memory.Manager
	memvid    *memvid.Client
	policy    *DistillationPolicy
	edgeCodec *EdgeCodec
	bus       *bus.MessageBus
	logger    *slog.Logger

	// Retry queue for failed operations
	retryQueue []RetryItem
	retryMu    sync.Mutex

	// Statistics
	stats   SyncStatus
	statsMu sync.RWMutex

	// Background ticker for periodic distillation
	periodicStop chan struct{}
	periodicWg   sync.WaitGroup
}

// SyncManagerConfig holds configuration for creating a SyncManager.
type SyncManagerConfig struct { //nolint:revive // stutter is intentional
	Config       config.DistributedMemoryConfig
	LocalManager *memory.Manager
	MemvidClient *memvid.Client
	MessageBus   *bus.MessageBus
	Logger       *slog.Logger
}

// NewSyncManager creates a new sync manager.
func NewSyncManager(cfg SyncManagerConfig) (*SyncManager, error) {
	if cfg.LocalManager == nil {
		return nil, errors.New("local memory manager required")
	}
	if cfg.MemvidClient == nil {
		return nil, errors.New("memvid client required")
	}
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	// Create distillation policy
	policy := NewDistillationPolicy(
		cfg.Config.Distillation,
		cfg.LocalManager.Graph(),
		cfg.Logger.With("component", "distiller"),
	)

	sm := &SyncManager{
		config:     cfg.Config,
		localMgr:   cfg.LocalManager,
		memvid:     cfg.MemvidClient,
		policy:     policy,
		edgeCodec:  NewEdgeCodec(),
		bus:        cfg.MessageBus,
		logger:     cfg.Logger,
		retryQueue: make([]RetryItem, 0),
		stats: SyncStatus{
			Enabled: cfg.Config.Enabled,
			Mode:    cfg.Config.Mode,
		},
	}

	return sm, nil
}

// Start begins background processing (periodic distillation, retry queue).
func (s *SyncManager) Start(ctx context.Context) error {
	// Check memvid availability
	s.statsMu.Lock()
	s.stats.MemvidAvailable = s.memvid.IsAvailable(ctx)
	s.statsMu.Unlock()

	if !s.stats.MemvidAvailable {
		s.logger.Warn("Memvid service not available, sync will operate in degraded mode")
	}

	// Start periodic distillation if configured
	if s.config.Sync.PeriodicDistillIntervalMinutes > 0 {
		s.periodicStop = make(chan struct{})
		s.periodicWg.Add(1)
		go s.runPeriodicDistillation(ctx)
	}

	s.logger.Info("SyncManager started",
		"mode", s.config.Mode,
		"memvid_available", s.stats.MemvidAvailable,
		"periodic_interval_min", s.config.Sync.PeriodicDistillIntervalMinutes,
	)

	return nil
}

// Stop shuts down background processing.
func (s *SyncManager) Stop() error {
	if s.periodicStop != nil {
		close(s.periodicStop)
		s.periodicWg.Wait()
	}
	return nil
}

// Hydrate fetches relevant memories from shared storage and imports them locally.
// Called when a job is claimed to provide context for the agent.
func (s *SyncManager) Hydrate(ctx context.Context, req HydrationRequest) (*HydrationResult, error) {
	start := time.Now()
	result := &HydrationResult{}

	// Check memvid availability
	if !s.memvid.IsAvailable(ctx) {
		s.logger.Warn("Memvid unavailable for hydration", "job_id", req.JobID)
		result.Error = "memvid service unavailable"
		result.Duration = time.Since(start)
		return result, nil // Graceful degradation
	}

	// Fetch explicit memory references first
	if len(req.MemoryRefs) > 0 {
		hydrated, edges, err := s.hydrateByIDs(ctx, req.MemoryRefs)
		if err != nil {
			s.logger.Warn("Failed to hydrate by IDs", "error", err)
		} else {
			result.MemoriesHydrated += hydrated
			result.EdgesRestored += edges
		}
	}

	// Search for relevant memories by context query
	if req.ContextQuery != "" {
		limit := req.Limit
		if limit <= 0 {
			limit = s.config.Sync.HydrationLimit
		}
		if limit <= 0 {
			limit = 20
		}

		hydrated, edges, err := s.hydrateByQuery(ctx, req.ContextQuery, limit)
		if err != nil {
			s.logger.Warn("Failed to hydrate by query", "error", err)
		} else {
			result.MemoriesHydrated += hydrated
			result.EdgesRestored += edges
		}
	}

	result.Duration = time.Since(start)

	// Update stats
	s.statsMu.Lock()
	now := time.Now()
	s.stats.LastHydration = &now
	s.stats.TotalHydrations++
	s.statsMu.Unlock()

	s.logger.Info("Hydration completed",
		"job_id", req.JobID,
		"memories_hydrated", result.MemoriesHydrated,
		"edges_restored", result.EdgesRestored,
		"duration_ms", result.Duration.Milliseconds(),
	)

	s.publishEvent("memory.sync.hydrated", result)

	return result, nil
}

// hydrateByIDs fetches specific memories by ID and imports them locally.
func (s *SyncManager) hydrateByIDs(ctx context.Context, ids []string) (requested, stored int, err error) {
	client := s.memvid.WithZone(SharedZone)
	memories, err := client.GetByIDs(ctx, ids)
	if err != nil {
		return 0, 0, fmt.Errorf("failed to fetch memories: %w", err)
	}

	memoriesStored := 0
	edgesRestored := 0

	for _, mv := range memories {
		stored, edges, err := s.importMemory(ctx, mv)
		if err != nil {
			s.logger.Warn("Failed to import memory", "id", mv.ID, "error", err)
			continue
		}
		if stored {
			memoriesStored++
		}
		edgesRestored += edges
	}

	return memoriesStored, edgesRestored, nil
}

// hydrateByQuery searches shared storage and imports relevant memories.
func (s *SyncManager) hydrateByQuery(ctx context.Context, query string, limit int) (fetched, stored int, err error) {
	client := s.memvid.WithZone(SharedZone)
	results, err := client.Search(ctx, query, limit)
	if err != nil {
		return 0, 0, fmt.Errorf("search failed: %w", err)
	}

	memoriesStored := 0
	edgesRestored := 0

	for _, mr := range results {
		stored, edges, err := s.importMemory(ctx, mr.Memory)
		if err != nil {
			s.logger.Warn("Failed to import memory", "id", mr.Memory.ID, "error", err)
			continue
		}
		if stored {
			memoriesStored++
		}
		edgesRestored += edges
	}

	return memoriesStored, edgesRestored, nil
}

// importMemory stores a memvid memory locally and restores its edges.
func (s *SyncManager) importMemory(ctx context.Context, mv memvid.Memory) (ok bool, n int, err error) {
	// Extract edges from metadata
	edgesOut, edgesIn, err := s.edgeCodec.ExtractEdgesFromMetadata(mv.Metadata)
	if err != nil {
		s.logger.Warn("Failed to extract edges from metadata", "id", mv.ID, "error", err)
	}

	// Clean metadata (remove edge refs, they'll be in graph)
	cleanMeta := s.edgeCodec.CleanEdgesFromMetadata(mv.Metadata)

	// Determine memory type from metadata
	memType := memory.MemoryTypeEpisodic
	if typeStr, ok := cleanMeta["type"].(string); ok {
		memType = memory.MemoryType(typeStr)
	}

	// Determine category
	category := ""
	if cat, ok := cleanMeta["category"].(string); ok {
		category = cat
	}

	// Store locally
	mem := memory.Memory{
		ID:        mv.ID,
		Content:   mv.Content,
		Type:      memType,
		Category:  category,
		Metadata:  cleanMeta,
		CreatedAt: mv.CreatedAt,
	}

	// Extract attribution
	if aid, ok := cleanMeta["agent_id"].(string); ok {
		mem.AgentID = aid
	}
	if sid, ok := cleanMeta["session_id"].(string); ok {
		mem.SessionID = sid
	}
	if tid, ok := cleanMeta["task_id"].(string); ok {
		mem.TaskID = tid
	}

	_, err = s.localMgr.Store(ctx, mem)
	if err != nil {
		return false, 0, fmt.Errorf("failed to store locally: %w", err)
	}

	// Restore edges in graph
	edgesRestored := 0
	if s.localMgr.Graph() != nil {
		edges := s.edgeCodec.DecodeEdges(mv.ID, edgesOut, edgesIn)
		for _, edge := range edges {
			if err := s.localMgr.Graph().AddEdge(ctx, edge); err != nil {
				s.logger.Debug("Failed to restore edge", "edge_id", edge.ID, "error", err)
			} else {
				edgesRestored++
			}
		}
	}

	return true, edgesRestored, nil
}

// Distill evaluates local memories and promotes high-value ones to shared storage.
// Called when a job completes.
func (s *SyncManager) Distill(ctx context.Context, taskID, agentID string) (*DistillationResult, error) {
	start := time.Now()
	result := &DistillationResult{}

	// Check memvid availability
	if !s.memvid.IsAvailable(ctx) {
		s.logger.Warn("Memvid unavailable for distillation", "task_id", taskID)
		result.Error = "memvid service unavailable"
		result.Duration = time.Since(start)

		// Queue for retry if configured
		if s.config.Sync.RetryOnFailure {
			s.queueRetry(RetryItem{
				ID:          fmt.Sprintf("distill-%s-%d", taskID, time.Now().UnixNano()),
				Operation:   OperationDistill,
				TaskID:      taskID,
				AgentID:     agentID,
				Attempts:    0,
				LastAttempt: time.Now(),
				NextAttempt: time.Now().Add(30 * time.Second),
				Error:       "memvid unavailable",
			})
		}
		return result, nil
	}

	// Recompute PageRank for accurate scoring
	if s.localMgr.Graph() != nil {
		if err := s.localMgr.UpdateGraphMetrics(ctx); err != nil {
			s.logger.Warn("Failed to update graph metrics", "error", err)
		}
	}

	// Query task-related memories
	var memories []memory.MemoryResult
	var err error

	if taskID != "" {
		// Search by task_id in metadata
		memories, err = s.localMgr.Search(ctx, memory.MemoryQuery{
			Query: fmt.Sprintf("task_id:%s", taskID),
			Limit: 100,
		})
	} else {
		// Get recent memories for agent
		memories, err = s.localMgr.GetRecent(ctx, 50)
	}

	if err != nil {
		result.Error = fmt.Sprintf("failed to query memories: %v", err)
		result.Duration = time.Since(start)
		return result, nil
	}

	result.MemoriesEvaluated = len(memories)

	// Apply distillation policy
	var candidates []PromotionCandidate
	if taskID != "" {
		candidates = s.policy.EvaluateTaskMemories(ctx, taskID, memories)
	} else {
		candidates = s.policy.SelectForPromotion(ctx, memories)
	}

	// Promote selected memories
	for _, candidate := range candidates {
		distilled, err := s.prepareDistilledMemory(ctx, candidate, agentID)
		if err != nil {
			s.logger.Warn("Failed to prepare memory for distillation",
				"memory_id", candidate.MemoryID,
				"error", err,
			)
			result.Failures = append(result.Failures, candidate.MemoryID)
			continue
		}

		if err := s.promoteToShared(ctx, distilled); err != nil {
			s.logger.Warn("Failed to promote memory",
				"memory_id", candidate.MemoryID,
				"error", err,
			)
			result.Failures = append(result.Failures, candidate.MemoryID)

			// Queue for retry
			if s.config.Sync.RetryOnFailure {
				s.queueRetry(RetryItem{
					ID:          fmt.Sprintf("promote-%s-%d", candidate.MemoryID, time.Now().UnixNano()),
					Operation:   OperationDistill,
					MemoryID:    candidate.MemoryID,
					Memory:      distilled,
					Attempts:    0,
					LastAttempt: time.Now(),
					NextAttempt: time.Now().Add(30 * time.Second),
					Error:       err.Error(),
				})
			}
			continue
		}

		result.MemoriesPromoted++
		result.EdgesPreserved += len(distilled.EdgesOut) + len(distilled.EdgesIn)
	}

	result.Duration = time.Since(start)

	// Update stats
	s.statsMu.Lock()
	now := time.Now()
	s.stats.LastDistillation = &now
	s.stats.TotalDistillations++
	s.statsMu.Unlock()

	s.logger.Info("Distillation completed",
		"task_id", taskID,
		"evaluated", result.MemoriesEvaluated,
		"promoted", result.MemoriesPromoted,
		"edges_preserved", result.EdgesPreserved,
		"failures", len(result.Failures),
		"duration_ms", result.Duration.Milliseconds(),
	)

	s.publishEvent("memory.sync.distilled", result)

	return result, nil
}

// prepareDistilledMemory creates a DistilledMemory from a promotion candidate.
func (s *SyncManager) prepareDistilledMemory(ctx context.Context, candidate PromotionCandidate, agentID string) (*DistilledMemory, error) {
	// Fetch the full memory by ID
	var mem memory.Memory

	fullMemories, err := s.localMgr.GetByIDs(ctx, []string{candidate.MemoryID})
	if err == nil && len(fullMemories) > 0 {
		mem = fullMemories[0]
	} else {
		// Fallback to search (e.g. when memvid is not the primary backend)
		results, searchErr := s.localMgr.Search(ctx, memory.MemoryQuery{
			Query: candidate.MemoryID,
			Limit: 5,
		})
		if searchErr != nil || len(results) == 0 {
			return nil, fmt.Errorf("memory not found: %s", candidate.MemoryID)
		}
		// Find exact ID match in search results
		found := false
		for _, r := range results {
			if r.Memory.ID == candidate.MemoryID {
				mem = r.Memory
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("memory not found by ID: %s", candidate.MemoryID)
		}
	}

	// Get edges from graph
	var edgesOut, edgesIn []EdgeRef
	if s.localMgr.Graph() != nil {
		edges, err := s.localMgr.Graph().GetEdges(ctx, mem.ID)
		if err == nil {
			edgesOut, edgesIn = s.edgeCodec.EncodeEdges(mem.ID, edges)
		}
	}

	// Use provided agentID or fall back to memory's agentID
	effectiveAgentID := agentID
	if effectiveAgentID == "" {
		effectiveAgentID = mem.AgentID
	}

	return &DistilledMemory{
		ID:              mem.ID,
		Content:         mem.Content,
		Type:            string(mem.Type),
		Category:        mem.Category,
		Metadata:        mem.Metadata,
		PageRank:        candidate.PageRank,
		PromotionReason: candidate.Reason,
		EdgesOut:        edgesOut,
		EdgesIn:         edgesIn,
		AgentID:         effectiveAgentID,
		TaskID:          candidate.TaskID,
		CreatedAt:       mem.CreatedAt,
		DistilledAt:     time.Now(),
	}, nil
}

// promoteToShared stores a distilled memory in shared memvid storage.
func (s *SyncManager) promoteToShared(ctx context.Context, dm *DistilledMemory) error {
	// Build metadata with edges
	metadata := s.edgeCodec.BuildDistilledMetadata(
		dm.Metadata,
		dm.EdgesOut,
		dm.EdgesIn,
		dm.PageRank,
		dm.PromotionReason,
		dm.DistilledAt.Format(time.RFC3339),
	)

	// Add attribution
	metadata["type"] = dm.Type
	metadata["category"] = dm.Category
	if dm.AgentID != "" {
		metadata["agent_id"] = dm.AgentID
	}
	if dm.TaskID != "" {
		metadata["task_id"] = dm.TaskID
	}

	// Store in shared zone
	client := s.memvid.WithZone(SharedZone)
	_, err := client.Store(ctx, dm.Content, metadata)
	if err != nil {
		return fmt.Errorf("memvid store failed: %w", err)
	}

	return nil
}

// HandleJobClaimed processes a job claimed event for hydration.
func (s *SyncManager) HandleJobClaimed(ctx context.Context, jobID, taskID string) error {
	if !s.config.Sync.HydrateOnClaim {
		return nil
	}

	req := HydrationRequest{
		JobID:  jobID,
		TaskID: taskID,
		Limit:  s.config.Sync.HydrationLimit,
	}

	// Try to build context query from task
	if taskID != "" {
		req.ContextQuery = fmt.Sprintf("task:%s", taskID)
	}

	_, err := s.Hydrate(ctx, req)
	return err
}

// HandleJobCompleted processes a job completed event for distillation.
func (s *SyncManager) HandleJobCompleted(ctx context.Context, jobID, taskID, agentID string) error {
	if !s.config.Sync.DistillOnComplete {
		return nil
	}

	_, err := s.Distill(ctx, taskID, agentID)
	return err
}

// Status returns the current sync status.
func (s *SyncManager) Status(ctx context.Context) SyncStatus {
	// Acquire retryMu first to match lock ordering in queueRetry
	s.retryMu.Lock()
	pendingRetries := len(s.retryQueue)
	s.retryMu.Unlock()

	s.statsMu.RLock()
	status := s.stats
	s.statsMu.RUnlock()

	// These don't require locks
	status.MemvidAvailable = s.memvid.IsAvailable(ctx)
	status.PendingRetries = pendingRetries

	return status
}

// queueRetry adds a failed operation to the retry queue.
func (s *SyncManager) queueRetry(item RetryItem) {
	s.retryMu.Lock()
	// Limit queue size
	if len(s.retryQueue) >= 100 {
		// Remove oldest item
		s.retryQueue = s.retryQueue[1:]
	}
	s.retryQueue = append(s.retryQueue, item)
	count := len(s.retryQueue)
	s.retryMu.Unlock()

	s.statsMu.Lock()
	s.stats.PendingRetries = count
	s.statsMu.Unlock()
}

// processRetryQueue attempts to process pending retries.
func (s *SyncManager) processRetryQueue(ctx context.Context) {
	s.retryMu.Lock()
	if len(s.retryQueue) == 0 {
		s.retryMu.Unlock()
		return
	}

	// Get items ready to retry
	now := time.Now()
	var ready []RetryItem
	var remaining []RetryItem

	for _, item := range s.retryQueue {
		if item.NextAttempt.Before(now) && item.Attempts < s.config.Sync.MaxRetries {
			ready = append(ready, item)
		} else if item.Attempts < s.config.Sync.MaxRetries {
			remaining = append(remaining, item)
		}
		// Drop items that exceeded max retries
	}

	s.retryQueue = remaining
	s.retryMu.Unlock()

	// Process ready items
	for _, item := range ready {
		item.Attempts++
		item.LastAttempt = now

		var err error
		switch {
		case item.Operation == OperationDistill && item.Memory != nil:
			// Retry promoting a specific memory
			err = s.promoteToShared(ctx, item.Memory)
		case item.Operation == OperationDistill && (item.TaskID != "" || item.AgentID != ""):
			// Replay full distillation (queued when memvid was entirely unavailable)
			_, err = s.Distill(ctx, item.TaskID, item.AgentID)
		default:
			// Unknown or unhandled retry item, skip
			s.logger.Warn("Dropping unhandled retry item", "id", item.ID, "operation", item.Operation)
			continue
		}

		if err != nil {
			item.Error = err.Error()
			// Exponential backoff
			backoff := min(time.Duration(1<<item.Attempts)*30*time.Second, 10*time.Minute)
			item.NextAttempt = now.Add(backoff)
			s.queueRetry(item)
		}
	}
}

// runPeriodicDistillation runs background periodic distillation.
func (s *SyncManager) runPeriodicDistillation(ctx context.Context) {
	defer s.periodicWg.Done()

	interval := time.Duration(s.config.Sync.PeriodicDistillIntervalMinutes) * time.Minute
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	retryTicker := time.NewTicker(time.Minute)
	defer retryTicker.Stop()

	for {
		select {
		case <-s.periodicStop:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.logger.Debug("Running periodic distillation")
			_, err := s.Distill(ctx, "", "")
			if err != nil {
				s.logger.Warn("Periodic distillation failed", "error", err)
			}
		case <-retryTicker.C:
			s.processRetryQueue(ctx)
		}
	}
}

// publishEvent publishes a sync lifecycle event to the message bus.
// This allows other components (UI, agents, monitoring) to observe sync operations.
func (s *SyncManager) publishEvent(topic string, data any) {
	if s.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "sync-manager", data)
	if err != nil {
		s.logger.Debug("Failed to create bus message", "topic", topic, "error", err)
		return
	}

	s.bus.Publish(topic, msg)
}
