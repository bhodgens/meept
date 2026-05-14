package sync

import (
	"context"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

// DistillationPolicy determines which memories should be promoted to shared storage.
// It evaluates memories based on PageRank importance, hub connectivity,
// cross-agent references, and task completion status.
type DistillationPolicy struct {
	config config.DistillationConfig
	graph  *memory.KnowledgeGraph
	logger *slog.Logger
}

// NewDistillationPolicy creates a new distillation policy.
func NewDistillationPolicy(cfg config.DistillationConfig, graph *memory.KnowledgeGraph, logger *slog.Logger) *DistillationPolicy {
	if logger == nil {
		logger = slog.Default()
	}

	return &DistillationPolicy{
		config: cfg,
		graph:  graph,
		logger: logger,
	}
}

// promotionMetrics holds graph metrics computed during promotion evaluation.
type promotionMetrics struct {
	pageRank  float64
	inDegree  int
	outDegree int
}

// ShouldPromote evaluates whether a single memory should be promoted.
// Returns: should promote, reason, computed score.
func (p *DistillationPolicy) ShouldPromote(ctx context.Context, mem memory.MemoryResult) (ok bool, reason string, score float64) {
	_, promote, reason, score := p.shouldPromoteWithMetrics(ctx, mem)
	return promote, reason, score
}

// shouldPromoteWithMetrics evaluates promotion and also returns the computed metrics
// to avoid redundant graph queries.
func (p *DistillationPolicy) shouldPromoteWithMetrics(ctx context.Context, mem memory.MemoryResult) (metrics promotionMetrics, ok bool, reason string, score float64) {
	var m promotionMetrics

	// Check minimum age
	minAge := time.Duration(p.config.MinMemoryAgeMinutes) * time.Minute
	if minAge > 0 && time.Since(mem.Memory.CreatedAt) < minAge {
		return m, false, "memory too recent", 0
	}

	// Get graph metrics if available
	if p.graph != nil {
		var err error
		m.pageRank, err = p.graph.GetPageRank(ctx, mem.Memory.ID)
		if err != nil {
			p.logger.Debug("Failed to get PageRank", "memory_id", mem.Memory.ID, "error", err)
		}

		// Get degree counts
		edges, err := p.graph.GetEdges(ctx, mem.Memory.ID)
		if err == nil {
			for _, e := range edges {
				if e.SourceID == mem.Memory.ID {
					m.outDegree++
				} else {
					m.inDegree++
				}
			}
		}
	}

	// Check PageRank threshold
	if p.config.PageRankThreshold > 0 && m.pageRank >= p.config.PageRankThreshold {
		return m, true, "high PageRank importance", m.pageRank
	}

	// Check hub connectivity (high degree nodes are important for graph structure)
	totalDegree := m.inDegree + m.outDegree
	if p.config.HubConnectivityThreshold > 0 && totalDegree >= p.config.HubConnectivityThreshold {
		score := float64(totalDegree) / float64(p.config.HubConnectivityThreshold)
		return m, true, "hub node with high connectivity", score
	}

	// Check cross-agent references (memory referenced by multiple agents)
	if p.config.CrossAgentReferencesMin > 0 {
		crossAgentRefs := p.countCrossAgentReferences(ctx, mem.Memory.ID, mem.Memory.AgentID)
		if crossAgentRefs >= p.config.CrossAgentReferencesMin {
			return m, true, "cross-agent knowledge sharing", float64(crossAgentRefs)
		}
	}

	// Check if this is a task completion summary
	if p.config.PromoteTaskCompletions && isTaskCompletion(mem.Memory) {
		return m, true, "task completion summary", 0.8
	}

	return m, false, "", 0
}

// SelectForPromotion filters a list of memories to those eligible for promotion.
// Returns memories that pass the policy criteria, sorted by promotion score.
func (p *DistillationPolicy) SelectForPromotion(ctx context.Context, memories []memory.MemoryResult) []PromotionCandidate {
	candidates := make([]PromotionCandidate, 0)

	for _, mem := range memories {
		metrics, shouldPromote, reason, score := p.shouldPromoteWithMetrics(ctx, mem)
		if !shouldPromote {
			continue
		}

		candidates = append(candidates, PromotionCandidate{
			MemoryID:  mem.Memory.ID,
			PageRank:  metrics.pageRank,
			InDegree:  metrics.inDegree,
			OutDegree: metrics.outDegree,
			TaskID:    mem.Memory.TaskID,
			AgentID:   mem.Memory.AgentID,
			Score:     score,
			Reason:    reason,
		})
	}

	// Sort by score descending
	sortCandidatesByScore(candidates)

	return candidates
}

// EvaluateTaskMemories evaluates all memories associated with a task.
// This is called when a task completes to determine what to promote.
func (p *DistillationPolicy) EvaluateTaskMemories(ctx context.Context, taskID string, memories []memory.MemoryResult) []PromotionCandidate {
	candidates := make([]PromotionCandidate, 0)

	for _, mem := range memories {
		if isTaskCompletion(mem.Memory) {
			// Always promote task completion summaries at high priority
			pageRank := 0.0
			if p.graph != nil {
				pageRank, _ = p.graph.GetPageRank(ctx, mem.Memory.ID)
			}
			candidates = append(candidates, PromotionCandidate{
				MemoryID: mem.Memory.ID,
				PageRank: pageRank,
				TaskID:   taskID,
				AgentID:  mem.Memory.AgentID,
				Score:    1.0,
				Reason:   "task completion summary",
			})
			continue
		}

		// Evaluate other memories using the policy
		metrics, shouldPromote, reason, score := p.shouldPromoteWithMetrics(ctx, mem)
		if shouldPromote {
			candidates = append(candidates, PromotionCandidate{
				MemoryID:  mem.Memory.ID,
				PageRank:  metrics.pageRank,
				InDegree:  metrics.inDegree,
				OutDegree: metrics.outDegree,
				TaskID:    taskID,
				AgentID:   mem.Memory.AgentID,
				Score:     score,
				Reason:    reason,
			})
		}
	}

	sortCandidatesByScore(candidates)
	return candidates
}

// countCrossAgentReferences counts how many different agents have edges to this memory.
func (p *DistillationPolicy) countCrossAgentReferences(ctx context.Context, memoryID, selfAgentID string) int {
	if p.graph == nil {
		return 0
	}

	edges, err := p.graph.GetEdges(ctx, memoryID)
	if err != nil {
		return 0
	}

	// Count unique agents from incoming edges
	agents := make(map[string]bool)
	for _, edge := range edges {
		if edge.TargetID == memoryID {
			// This is an incoming edge; check the source's agent
			if agentID, ok := edge.Metadata["agent_id"].(string); ok && agentID != selfAgentID {
				agents[agentID] = true
			}
		}
	}

	return len(agents)
}

// isTaskCompletion checks if a memory represents a task completion summary.
func isTaskCompletion(mem memory.Memory) bool {
	if mem.Metadata == nil {
		return false
	}

	// Check for explicit task completion marker
	if completed, ok := mem.Metadata["task_completed"].(bool); ok && completed {
		return true
	}

	// Check category
	if mem.Category == "task_completion" || mem.Category == "completion_summary" {
		return true
	}

	// Check for completion type
	if memType, ok := mem.Metadata["memory_type"].(string); ok {
		if memType == "task_completion" || memType == "completion_summary" {
			return true
		}
	}

	return false
}

// sortCandidatesByScore sorts candidates by score in descending order.
func sortCandidatesByScore(candidates []PromotionCandidate) {
	// Simple insertion sort (typically small lists)
	for i := 1; i < len(candidates); i++ {
		j := i
		for j > 0 && candidates[j].Score > candidates[j-1].Score {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
			j--
		}
	}
}

// DefaultDistillationConfig returns sensible defaults for distillation.
func DefaultDistillationConfig() config.DistillationConfig {
	return config.DistillationConfig{
		PageRankThreshold:        0.3,
		HubConnectivityThreshold: 5,
		PromoteTaskCompletions:   true,
		CrossAgentReferencesMin:  2,
		MinMemoryAgeMinutes:      5,
	}
}
