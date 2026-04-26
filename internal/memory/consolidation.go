package memory

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// Consolidator compacts and summarizes old memories.
// It performs:
// 1. Fetching old episodic memories (older than a configurable threshold)
// 2. Grouping them by date and topic
// 3. Creating summary memories and archiving the originals
// 4. Identifying and removing duplicate task memories
type Consolidator struct {
	manager  *Manager
	logger   *slog.Logger
	llm      llm.Chatter // optional: if nil, falls back to date-based grouping
	mu       sync.Mutex
	running  bool
	lastRun  *time.Time
	stopChan chan struct{}
	stopOnce sync.Once // Guards against double-close of stopChan
}

// ConsolidatorConfig holds configuration for the consolidator.
type ConsolidatorConfig struct {
	// Manager is the memory manager to consolidate.
	Manager *Manager
	// Logger for consolidation operations.
	Logger *slog.Logger
	// LLM is an optional chat client used for intelligent summarization.
	// If nil, the consolidator falls back to naive date-based grouping.
	LLM llm.Chatter
}

// NewConsolidator creates a new consolidator.
func NewConsolidator(cfg ConsolidatorConfig) *Consolidator {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	return &Consolidator{
		manager:  cfg.Manager,
		logger:   cfg.Logger,
		llm:      cfg.LLM,
		stopChan: make(chan struct{}),
	}
}

// Run performs a single consolidation pass.
func (c *Consolidator) Run(ctx context.Context, olderThanHours int) (*ConsolidationReport, error) {
	c.mu.Lock()
	if c.running {
		c.mu.Unlock()
		return nil, fmt.Errorf("consolidation already in progress")
	}
	c.running = true
	c.mu.Unlock()

	defer func() {
		c.mu.Lock()
		c.running = false
		now := time.Now()
		c.lastRun = &now
		c.mu.Unlock()
	}()

	start := time.Now()
	report := &ConsolidationReport{}

	// Access-based expiration (run before consolidation)
	cfg := c.manager.Config().Expiration
	if cfg.Enabled && cfg.AccessExpirationDays > 0 {
		accessReport, err := c.runAccessBasedExpiration(ctx)
		if err != nil {
			if report.Error != "" {
				report.Error += "; "
			}
			report.Error += err.Error()
			c.logger.Error("Access-based expiration failed", "error", err)
		} else {
			report.Expired = accessReport.Expired
		}
	}

	// Episodic consolidation
	if c.manager.episodic != nil {
		cutoff := time.Now().Add(-time.Duration(olderThanHours) * time.Hour)
		episodicReport, err := c.consolidateEpisodic(ctx, cutoff)
		if err != nil {
			report.Error = err.Error()
			c.logger.Error("Episodic consolidation failed", "error", err)
		} else {
			report.EpisodicArchived = episodicReport.archived
			report.SummariesCreated = episodicReport.created
		}
	}

	// Task deduplication
	if c.manager.task != nil {
		removed, err := c.deduplicateTasks(ctx)
		if err != nil {
			if report.Error != "" {
				report.Error += "; "
			}
			report.Error += err.Error()
			c.logger.Error("Task deduplication failed", "error", err)
		} else {
			report.DuplicatesRemoved = removed
		}
	}

	report.Duration = time.Since(start)

	c.logger.Info("Consolidation complete",
		"episodic_archived", report.EpisodicArchived,
		"summaries_created", report.SummariesCreated,
		"duplicates_removed", report.DuplicatesRemoved,
		"expired", report.Expired,
		"duration", report.Duration,
	)

	return report, nil
}

// runAccessBasedExpiration performs access-based memory expiration.
// MEM-11 FIX: accumulate Store/Delete error counts instead of silently
// discarding them.  The report's Expired field now only reflects cleanly
// removed memories; non-zero storeErrors/deleteErrors are logged at
// warn level above.
func (c *Consolidator) runAccessBasedExpiration(ctx context.Context) (*ConsolidationReport, error) {
	cfg := c.manager.Config().Expiration
	expiredMemories, err := c.manager.GetExpiredMemories(ctx, cfg.AccessExpirationDays)
	if err != nil {
		return nil, err
	}

	var expiredCount int
	var storeErrors, deleteErrors int
	for _, mem := range expiredMemories {
		if cfg.SummarizeBeforeDelete {
			// Create summary memory first
			summary := c.createSummary(mem)
			summary.Category = cfg.SummaryCategory
			if _, err := c.manager.Store(ctx, summary); err != nil {
				storeErrors++
				c.logger.Error("Failed to store summary before delete",
					"memory_id", mem.ID,
					"error", err,
				)
				// Continue with deletion even if summary fails
			}
		}
		// Delete expired memory
		if err := c.manager.Delete(ctx, mem.ID); err != nil {
			deleteErrors++
			c.logger.Error("Failed to delete expired memory",
				"memory_id", mem.ID,
				"error", err,
			)
			// Continue processing other memories
		} else {
			expiredCount++
		}
	}

	if storeErrors > 0 || deleteErrors > 0 {
		c.logger.Warn("Access-based expiration completed with errors",
			"total_candidates", len(expiredMemories),
			"successfully_expired", expiredCount,
			"store_errors", storeErrors,
			"delete_errors", deleteErrors,
		)
	}

	return &ConsolidationReport{Expired: expiredCount}, nil
}

// createSummary creates a summary memory from an expired memory.
func (c *Consolidator) createSummary(mem Memory) Memory {
	summaryContent := fmt.Sprintf("Summary of expired memory: %s", mem.Content)
	if len(summaryContent) > 500 {
		summaryContent = summaryContent[:500] + "..."
	}

	return Memory{
		Content:   summaryContent,
		Type:      mem.Type,
		Category:  "summary",
		Metadata:  mem.Metadata,
		CreatedAt: time.Now(),
	}
}

// episodicReport holds internal episodic consolidation results.
type episodicReport struct {
	archived int
	created  int
}

// consolidateEpisodic consolidates old episodic memories.
func (c *Consolidator) consolidateEpisodic(ctx context.Context, cutoff time.Time) (*episodicReport, error) {
	report := &episodicReport{}

	// Get old memories
	oldMemories, err := c.manager.episodic.GetOldMemories(ctx, cutoff, 500)
	if err != nil {
		return nil, fmt.Errorf("failed to get old memories: %w", err)
	}

	if len(oldMemories) == 0 {
		return report, nil
	}

	// Group memories — prefer LLM-based summarization, fall back to date-based.
	var summaries []Summary
	if c.llm != nil {
		var err error
		summaries, err = c.summarizeWithLLM(ctx, oldMemories)
		if err != nil {
			c.logger.Warn("LLM summarization failed, falling back to date-based grouping",
				"error", err,
			)
			summaries = c.summarizeByDate(oldMemories)
		}
	} else {
		summaries = c.summarizeByDate(oldMemories)
	}

	// Store summaries and collect IDs to archive
	var archivedIDs []string
	var createdIDs []string

	for _, summary := range summaries {
		// Store the summary as a new memory
		summaryID, err := c.manager.episodic.Store(ctx,
			summary.Summary,
			fmt.Sprintf("summary:%s", summary.Topic),
			map[string]any{
				"consolidated_from": summary.IDs,
				"type":              "summary",
			},
		)
		if err != nil {
			// Rollback: delete any summaries we've created
			c.logger.Error("Failed to store summary, rolling back",
				"topic", summary.Topic,
				"error", err,
			)
			if len(createdIDs) > 0 {
				_, _ = c.manager.episodic.DeleteByIDs(ctx, createdIDs)
			}
			return nil, fmt.Errorf("failed to store summary: %w", err)
		}

		createdIDs = append(createdIDs, summaryID)
		archivedIDs = append(archivedIDs, summary.IDs...)
		report.created++
	}

	// Delete archived memories
	if len(archivedIDs) > 0 {
		deleted, err := c.manager.episodic.DeleteByIDs(ctx, archivedIDs)
		if err != nil {
			// Log but don't fail - summaries are already created
			c.logger.Warn("Failed to delete some archived memories",
				"attempted", len(archivedIDs),
				"error", err,
			)
		}
		report.archived = deleted
	}

	return report, nil
}

// Summary represents a consolidated group of memories.
type Summary struct {
	Topic   string   `json:"topic"`
	Summary string   `json:"summary"`
	IDs     []string `json:"ids"`
}

// summarizeByDate groups memories by calendar date.
func (c *Consolidator) summarizeByDate(memories []MemoryResult) []Summary {
	groups := make(map[string][]MemoryResult)

	for _, mem := range memories {
		dayKey := mem.Memory.CreatedAt.Format("2006-01-02")
		groups[dayKey] = append(groups[dayKey], mem)
	}

	var summaries []Summary

	// Sort keys for deterministic output
	days := make([]string, 0, len(groups))
	for day := range groups {
		days = append(days, day)
	}
	sort.Strings(days)

	for _, day := range days {
		mems := groups[day]
		var ids []string // Use append to avoid zero-value slots

		// Build a compact summary from snippets
		var snippets []string
		totalChars := 0
		for _, m := range mems {
			// Filter out empty/zero-value IDs
			if m.Memory.ID != "" {
				ids = append(ids, m.Memory.ID)
			}
			snippet := m.Memory.Content
			if len(snippet) > 200 {
				snippet = snippet[:200]
			}
			snippet = strings.ReplaceAll(snippet, "\n", " ")
			snippet = strings.TrimSpace(snippet)
			if snippet != "" {
				snippets = append(snippets, snippet)
				totalChars += len(snippet)
			}
			if totalChars > 2000 {
				snippets = append(snippets, fmt.Sprintf("... and %d more", len(mems)-len(ids)))
				break
			}
		}

		summaryText := fmt.Sprintf("Consolidated memories from %s (%d items): %s",
			day, len(mems), strings.Join(snippets, "; "))

		summaries = append(summaries, Summary{
			Topic:   day,
			Summary: summaryText,
			IDs:     ids,
		})
	}

	return summaries
}

// MergeRelated groups memories into consolidated summaries.
//
// When an LLM client is configured, semantic grouping is used.
// Otherwise, the implementation groups strictly by date (calendar day).
//
// MEM-17 DEFERRED: Without LLM, only groups by calendar day rather than
// semantic similarity. Fix requires implementing topic-aware non-LLM
// grouping (e.g., keyword-based TF-IDF clustering).
func (c *Consolidator) MergeRelated(ctx context.Context, memories []MemoryResult) ([]Summary, error) {
	if c.llm != nil {
		summaries, err := c.summarizeWithLLM(ctx, memories)
		if err != nil {
			c.logger.Warn("LLM summarization in MergeRelated failed, falling back to date-based",
				"error", err,
			)
			return c.summarizeByDate(memories), nil
		}
		return summaries, nil
	}
	return c.summarizeByDate(memories), nil
}

// summarizeWithLLM sends memories to the LLM for intelligent topic-based
// grouping and summarization. It returns up to 5 summary groups.
func (c *Consolidator) summarizeWithLLM(ctx context.Context, memories []MemoryResult) ([]Summary, error) {
	// Build the user prompt from memory content.
	var b strings.Builder
	b.WriteString("Memories:\n")
	for _, m := range memories {
		content := m.Memory.Content
		if len(content) > 200 {
			content = content[:200]
		}
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Fprintf(&b, "- ID: %s, Content: %s\n", m.Memory.ID, strings.TrimSpace(content))
	}

	const systemPrompt = `Please summarize these memory snippets into coherent topics.
Return a JSON array where each element has these fields:
- "topic": a short label for the group
- "summary": a concise summary of the grouped memories
- "ids": an array of memory IDs that belong to this group

Group related memories together and create a concise summary for each group.
Maximum 5 summary groups. Return ONLY the JSON array, no other text.`

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: systemPrompt},
		{Role: llm.RoleUser, Content: b.String()},
	}

	resp, err := c.llm.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("LLM chat request failed: %w", err)
	}

	if resp.Content == "" {
		return nil, fmt.Errorf("LLM returned empty response")
	}

	summaries, err := ParseSummarizeResponse(resp.Content)
	if err != nil {
		return nil, fmt.Errorf("failed to parse LLM summarization response: %w", err)
	}

	// Validate: every returned ID must exist in the input set.
	inputIDs := make(map[string]struct{}, len(memories))
	for _, m := range memories {
		inputIDs[m.Memory.ID] = struct{}{}
	}
	for i := range summaries {
		var valid []string
		for _, id := range summaries[i].IDs {
			if _, ok := inputIDs[id]; ok {
				valid = append(valid, id)
			}
		}
		summaries[i].IDs = valid
	}

	// Drop summaries that ended up with no valid IDs.
	var filtered []Summary
	for _, s := range summaries {
		if len(s.IDs) > 0 {
			filtered = append(filtered, s)
		}
	}

	return filtered, nil
}

// deduplicateTasks removes duplicate task memories.
func (c *Consolidator) deduplicateTasks(ctx context.Context) (int, error) {
	// Find duplicate groups
	dupGroups, err := c.manager.task.FindDuplicates(ctx, 50)
	if err != nil {
		return 0, fmt.Errorf("failed to find duplicates: %w", err)
	}

	var idsToRemove []string
	for _, group := range dupGroups {
		// Keep the first (oldest), remove the rest
		if len(group) > 1 {
			idsToRemove = append(idsToRemove, group[1:]...)
		}
	}

	if len(idsToRemove) == 0 {
		return 0, nil
	}

	removed, err := c.manager.task.DeleteByIDs(ctx, idsToRemove)
	if err != nil {
		return 0, fmt.Errorf("failed to delete duplicates: %w", err)
	}

	c.logger.Info("Task deduplication complete",
		"groups_found", len(dupGroups),
		"duplicates_removed", removed,
	)

	return removed, nil
}

// PruneOld removes memories older than maxAge.
func (c *Consolidator) PruneOld(ctx context.Context, maxAge time.Duration) (int, error) {
	cutoff := time.Now().Add(-maxAge)
	pruned := 0

	// Prune episodic memories
	if c.manager.episodic != nil {
		oldMemories, err := c.manager.episodic.GetOldMemories(ctx, cutoff, 1000)
		if err != nil {
			return 0, fmt.Errorf("failed to get old episodic memories: %w", err)
		}

		if len(oldMemories) > 0 {
			ids := make([]string, len(oldMemories))
			for i, m := range oldMemories {
				ids[i] = m.Memory.ID
			}
			deleted, err := c.manager.episodic.DeleteByIDs(ctx, ids)
			if err != nil {
				return pruned, fmt.Errorf("failed to prune episodic memories: %w", err)
			}
			pruned += deleted
		}
	}

	return pruned, nil
}

// StartPeriodicConsolidation starts a background goroutine that runs
// consolidation at the specified interval.
func (c *Consolidator) StartPeriodicConsolidation(ctx context.Context, interval time.Duration, olderThanHours int) {
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case <-c.stopChan:
				return
			case <-ticker.C:
				_, err := c.Run(ctx, olderThanHours)
				if err != nil {
					c.logger.Error("Periodic consolidation failed", "error", err)
				}
			}
		}
	}()
}

// Stop stops the periodic consolidation.
// Safe to call multiple times - subsequent calls are no-ops.
func (c *Consolidator) Stop() {
	c.stopOnce.Do(func() {
		close(c.stopChan)
	})
}

// IsRunning returns true if consolidation is currently in progress.
func (c *Consolidator) IsRunning() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.running
}

// LastRun returns the time of the last consolidation run.
func (c *Consolidator) LastRun() *time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.lastRun
}

// SummarizeRequest represents a batch of memories to be summarized by the LLM.
type SummarizeRequest struct {
	Memories    []MemoryResult `json:"memories"`
	MaxSummaries int           `json:"max_summaries"`
}

type SummarizeResponse struct {
	Summaries []Summary `json:"summaries"`
}

// ToJSON converts a summarize request to JSON.
func (r *SummarizeRequest) ToJSON() string {
	data, _ := json.Marshal(r)
	return string(data)
}

// ParseSummarizeResponse parses an LLM response into summaries.
func ParseSummarizeResponse(content string) ([]Summary, error) {
	// Strip potential markdown fences
	content = strings.TrimSpace(content)
	if strings.HasPrefix(content, "```") {
		lines := strings.SplitN(content, "\n", 2)
		if len(lines) > 1 {
			content = lines[1]
		}
	}
	if strings.HasSuffix(content, "```") {
		content = strings.TrimSuffix(content, "```")
	}
	content = strings.TrimSpace(content)

	var summaries []Summary
	if err := json.Unmarshal([]byte(content), &summaries); err != nil {
		return nil, fmt.Errorf("failed to parse summaries: %w", err)
	}

	return summaries, nil
}
