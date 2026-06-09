// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/tools"
)

// MemoryStoreTool stores information in long-term memory.
type MemoryStoreTool struct {
	manager *memory.Manager
}

// NewMemoryStoreTool creates a new memory store tool.
func NewMemoryStoreTool(manager *memory.Manager) *MemoryStoreTool {
	return &MemoryStoreTool{manager: manager}
}

func (t *MemoryStoreTool) Name() string { return "memory_store" }

func (t *MemoryStoreTool) Category() string { return "memory" }

func (t *MemoryStoreTool) Description() string {
	return "Store information in long-term memory for future reference. Use this to save important facts, decisions, learnings, or context that should be remembered across conversations."
}

func (t *MemoryStoreTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropContent: {
				Type:        schemaTypeString,
				Description: "The content to store in memory. Should be clear and self-contained.",
			},
			schemaPropType: {
				Type:        schemaTypeString,
				Description: "Memory type: 'episodic' for conversations/interactions, 'task' for technical knowledge.",
				Enum:        []string{schemaMemoryEpisodic, schemaMemoryTask},
			},
			schemaPropCategory: {
				Type:        schemaTypeString,
				Description: "Optional category to organize the memory (e.g., 'conversation', 'code', 'decision').",
			},
		},
		Required: []string{"content", "type"},
	}
}

func (t *MemoryStoreTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	memTypeStr, _ := args["type"].(string)
	if memTypeStr == "" {
		memTypeStr = schemaMemoryEpisodic
	}

	var memType memory.MemoryType
	switch memTypeStr {
	case schemaMemoryEpisodic:
		memType = memory.MemoryTypeEpisodic
	case schemaMemoryTask:
		memType = memory.MemoryTypeTask
	default:
		return nil, fmt.Errorf("invalid memory type: %s (use 'episodic' or 'task')", memTypeStr)
	}

	category, _ := args["category"].(string)

	mem := memory.Memory{
		Content:  content,
		Type:     memType,
		Category: category,
	}

	id, err := t.manager.Store(ctx, mem)
	if err != nil {
		return nil, fmt.Errorf("failed to store memory: %w", err)
	}

	return map[string]any{
		"success":          true,
		schemaPropMemoryID: id,
		schemaPropType:     memTypeStr,
		schemaPropCategory: category,
	}, nil
}

// MemorySearchTool searches memories by query.
type MemorySearchTool struct {
	manager *memory.Manager
}

// NewMemorySearchTool creates a new memory search tool.
func NewMemorySearchTool(manager *memory.Manager) *MemorySearchTool {
	return &MemorySearchTool{manager: manager}
}

func (t *MemorySearchTool) Name() string { return "memory_search" }

func (t *MemorySearchTool) Category() string { return "memory" }

func (t *MemorySearchTool) Description() string {
	return "Search memories for relevant past context. Use this to find information that was previously stored, such as past conversations, decisions, or learnings."
}

func (t *MemorySearchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropQuery: {
				Type:        schemaTypeString,
				Description: "Search query to find relevant memories.",
			},
			schemaPropType: {
				Type:        schemaTypeString,
				Description: "Optional: filter to 'episodic' or 'task' memories only.",
				Enum:        []string{schemaMemoryEpisodic, schemaMemoryTask, ""},
			},
			schemaPropLimit: {
				Type:        schemaTypeInteger,
				Description: "Maximum number of results to return (default 10, max 50).",
			},
			"min_relevance": {
				Type:        schemaTypeNumber,
				Description: "Minimum relevance score 0.0-1.0 (default 0.3).",
			},
		},
		Required: []string{"query"},
	}
}

func (t *MemorySearchTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	limit := 10
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = min(int(l), 50)
	}

	minRelevance := 0.3
	if mr, ok := args["min_relevance"].(float64); ok {
		minRelevance = mr
	}

	memQuery := memory.MemoryQuery{
		Query:        query,
		Limit:        limit,
		MinRelevance: minRelevance,
	}

	// Apply type filter if specified
	if memTypeStr, ok := args["type"].(string); ok && memTypeStr != "" {
		switch memTypeStr {
		case schemaMemoryEpisodic:
			memQuery.Type = memory.MemoryTypeEpisodic
		case schemaMemoryTask:
			memQuery.Type = memory.MemoryTypeTask
		}
	}

	// Use graph-aware search for better ranking (alpha=0.3 = 30% PageRank influence)
	results, err := t.manager.SearchWithGraph(ctx, memQuery, 0.3)
	if err != nil {
		return nil, fmt.Errorf("memory search failed: %w", err)
	}

	// Format results for output
	formatted := make([]map[string]any, 0, len(results))
	for _, r := range results {
		formatted = append(formatted, map[string]any{
			"id":               r.Memory.ID,
			schemaPropContent:  r.Memory.Content,
			schemaPropType:     string(r.Memory.Type),
			schemaPropCategory: r.Memory.Category,
			"relevance":        r.RelevanceScore,
			"source":           r.Source,
		})
	}

	return map[string]any{
		"results":       formatted,
		schemaPropCount: len(formatted),
		schemaPropQuery: query,
	}, nil
}

// MemoryGetContextTool retrieves contextually relevant memories.
type MemoryGetContextTool struct {
	manager *memory.Manager
}

// NewMemoryGetContextTool creates a new memory context tool.
func NewMemoryGetContextTool(manager *memory.Manager) *MemoryGetContextTool {
	return &MemoryGetContextTool{manager: manager}
}

func (t *MemoryGetContextTool) Name() string { return "memory_get_context" }

func (t *MemoryGetContextTool) Category() string { return "memory" }

func (t *MemoryGetContextTool) Description() string {
	return "Get contextually relevant memories for a query. This performs a smart search across all memory types to gather the most helpful context for understanding or responding to a topic."
}

func (t *MemoryGetContextTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropQuery: {
				Type:        schemaTypeString,
				Description: "The query or topic to get relevant context for.",
			},
			"max_items": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of context items to return (default 10, max 30).",
			},
		},
		Required: []string{"query"},
	}
}

func (t *MemoryGetContextTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	maxItems := 10
	if m, ok := args["max_items"].(float64); ok && m > 0 {
		maxItems = min(int(m), 30)
	}

	results, err := t.manager.GetRelevantContext(ctx, query, maxItems)
	if err != nil {
		return nil, fmt.Errorf("failed to get context: %w", err)
	}

	// Format results for output
	formatted := make([]map[string]any, 0, len(results))
	for _, r := range results {
		formatted = append(formatted, map[string]any{
			"id":               r.Memory.ID,
			schemaPropContent:  r.Memory.Content,
			schemaPropType:     string(r.Memory.Type),
			schemaPropCategory: r.Memory.Category,
			"relevance":        r.RelevanceScore,
		})
	}

	return map[string]any{
		"context":       formatted,
		schemaPropCount: len(formatted),
		schemaPropQuery: query,
	}, nil
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*MemoryStoreTool)(nil)
	_ tools.Tool = (*MemorySearchTool)(nil)
	_ tools.Tool = (*MemoryGetContextTool)(nil)
)

// TerminateHint implements tools.TerminatingTool for MemoryStoreTool.
// Memory store returns a simple confirmation that needs no LLM follow-up.
func (t *MemoryStoreTool) TerminateHint(args map[string]any) bool { return true }

// Ensure MemoryStoreTool implements TerminatingTool
var _ tools.TerminatingTool = (*MemoryStoreTool)(nil)

// MemoryGetVersionTool retrieves a specific version of a memory by ID.
type MemoryGetVersionTool struct {
	manager *memory.Manager
}

// NewMemoryGetVersionTool creates a new memory get version tool.
func NewMemoryGetVersionTool(manager *memory.Manager) *MemoryGetVersionTool {
	return &MemoryGetVersionTool{manager: manager}
}

func (t *MemoryGetVersionTool) Name() string { return "memory_get_version" }

func (t *MemoryGetVersionTool) Category() string { return "memory" }

func (t *MemoryGetVersionTool) Description() string {
	return "Retrieve a specific version of a memory by its ID. Use this to view the history of changes to a memory or to recover a previous version. If version is not specified, returns the current version."
}

func (t *MemoryGetVersionTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropMemoryID: {
				Type:        schemaTypeString,
				Description: "The ID of the memory to retrieve.",
			},
			"version": {
				Type:        schemaTypeInteger,
				Description: "Optional: specific version number to retrieve. If not specified, returns the current version.",
			},
		},
		Required: []string{"memory_id"},
	}
}

func (t *MemoryGetVersionTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	memoryID, ok := args["memory_id"].(string)
	if !ok || memoryID == "" {
		return nil, fmt.Errorf("memory_id is required")
	}

	// Get the memory by ID
	mem, err := t.manager.GetByID(ctx, memoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get memory: %w", err)
	}
	if mem == nil {
		return nil, fmt.Errorf("memory not found: %s", memoryID)
	}

	// Build result with version metadata
	result := map[string]any{
		"id":               mem.ID,
		schemaPropContent:  mem.Content,
		schemaPropType:     string(mem.Type),
		schemaPropCategory: mem.Category,
		"created_at":       mem.CreatedAt,
	}

	// Add version info if available in metadata
	if mem.Metadata != nil {
		if v, ok := mem.Metadata["version"]; ok {
			result["version"] = v
		}
		if parentID, ok := mem.Metadata["parent_id"]; ok {
			result["parent_id"] = parentID
		}
		if isCurrent, ok := mem.Metadata["is_current"]; ok {
			result["is_current"] = isCurrent
		}
	}

	return result, nil
}

// MemoryGetVersionHistoryTool retrieves the version history of a memory.
type MemoryGetVersionHistoryTool struct {
	manager *memory.Manager
}

// NewMemoryGetVersionHistoryTool creates a new memory get version history tool.
func NewMemoryGetVersionHistoryTool(manager *memory.Manager) *MemoryGetVersionHistoryTool {
	return &MemoryGetVersionHistoryTool{manager: manager}
}

func (t *MemoryGetVersionHistoryTool) Name() string { return "memory_get_version_history" }

func (t *MemoryGetVersionHistoryTool) Category() string { return "memory" }

func (t *MemoryGetVersionHistoryTool) Description() string {
	return "Retrieve the version history of a memory by its ID. Returns all versions of the memory in chronological order, showing how it evolved over time. Each version includes its content, timestamp, and version metadata."
}

func (t *MemoryGetVersionHistoryTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropMemoryID: {
				Type:        schemaTypeString,
				Description: "The ID of the memory to retrieve version history for.",
			},
		},
		Required: []string{"memory_id"},
	}
}

func (t *MemoryGetVersionHistoryTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	memoryID, ok := args["memory_id"].(string)
	if !ok || memoryID == "" {
		return nil, fmt.Errorf("memory_id is required")
	}

	// Get version history
	memories, err := t.manager.GetVersionHistory(ctx, memoryID)
	if err != nil {
		return nil, fmt.Errorf("failed to get version history: %w", err)
	}

	if len(memories) == 0 {
		return map[string]any{
			schemaPropMemoryID: memoryID,
			"versions":         []any{},
			schemaPropMessage:  "No version history found",
		}, nil
	}

	// Build version history result
	versions := make([]map[string]any, 0, len(memories))
	for _, mem := range memories {
		version := map[string]any{
			"id":              mem.ID,
			schemaPropContent: mem.Content,
			"created_at":      mem.CreatedAt,
		}

		// Add version metadata if available
		if mem.Metadata != nil {
			if v, ok := mem.Metadata["version"]; ok {
				version["version"] = v
			}
			if parentID, ok := mem.Metadata["parent_id"]; ok {
				version["parent_id"] = parentID
			}
			if isCurrent, ok := mem.Metadata["is_current"]; ok {
				version["is_current"] = isCurrent
			}
			if agentID, ok := mem.Metadata["agent_id"]; ok {
				version["agent_id"] = agentID
			}
		}

		versions = append(versions, version)
	}

	return map[string]any{
		schemaPropMemoryID: memoryID,
		"version_count":    len(versions),
		"versions":         versions,
		"current_version":  getCurrentVersionFromList(versions),
	}, nil
}

// getCurrentVersionFromList finds the current version from a list of versions.
func getCurrentVersionFromList(versions []map[string]any) int {
	for _, v := range slices.Backward(versions) {
		if isCurrent, ok := v["is_current"].(int); ok && isCurrent == 1 {
			if v, ok := v["version"].(int); ok {
				return v
			}
			if v, ok := v["version"].(float64); ok {
				return int(v)
			}
		}
	}
	return len(versions)
}

// ============================================================================
// Memory Curation Tools (Hindsight Bank Pattern)
// ============================================================================

// MemoryRetainTool queues facts into a "Hindsight bank" for later recall.
type MemoryRetainTool struct {
	manager *memory.Manager
}

// NewMemoryRetainTool creates a new memory retain tool.
func NewMemoryRetainTool(manager *memory.Manager) *MemoryRetainTool {
	return &MemoryRetainTool{manager: manager}
}

func (t *MemoryRetainTool) Name() string { return "memory_retain" }

func (t *MemoryRetainTool) Category() string { return "memory" }

func (t *MemoryRetainTool) Description() string {
	return "Queue a fact into the Hindsight bank for later recall. Use this to deliberately curate important knowledge that should be remembered, such as key decisions, learnings, preferences, or domain insights. Unlike automatic memory storage, retain is intentional and selective."
}

func (t *MemoryRetainTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropContent: {
				Type:        schemaTypeString,
				Description: "The fact to retain. Should be concise, self-contained, and clearly stated.",
			},
			"domain": {
				Type:        schemaTypeString,
				Description: "Optional domain label to organize retained facts (e.g., 'architecture', 'debugging', 'team-process').",
			},
			"importance": {
				Type:        schemaTypeString,
				Description: "Importance level: 'high' for critical knowledge, 'medium' for useful context, 'low' for nice-to-know.",
				Enum:        []string{"high", "medium", "low"},
			},
		},
		Required: []string{"content"},
	}
}

// MemoryRetainResult is returned after retaining a fact.
type MemoryRetainResult struct {
	Success      bool   `json:"success"`
	MemoryID     string `json:"memory_id,omitempty"`
	Domain       string `json:"domain,omitempty"`
	Importance   string `json:"importance,omitempty"`
	Message      string `json:"message"`
	HindsightLen int    `json:"hindsight_count,omitempty"`
}

func (t *MemoryRetainTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	domain, _ := args["domain"].(string)
	importance, _ := args["importance"].(string)
	if importance == "" {
		importance = "medium"
	}

	// Store as task memory with special category for retained facts
	mem := memory.Memory{
		Content:  content,
		Type:     memory.MemoryTypeTask,
		Category: "hindsight:" + importance,
		Metadata: map[string]any{
			"retained":   true,
			"domain":     domain,
			"importance": importance,
		},
	}

	id, err := t.manager.Store(ctx, mem)
	if err != nil {
		return nil, fmt.Errorf("failed to retain memory: %w", err)
	}

	// Count total retained facts for feedback
	stats := t.manager.Stats()

	return MemoryRetainResult{
		Success:      true,
		MemoryID:     id,
		Domain:       domain,
		Importance:   importance,
		Message:      fmt.Sprintf("Fact retained in hindsight bank (importance: %s)", importance),
		HindsightLen: stats.TaskCount,
	}, nil
}

// MemoryRecallTool searches the Hindsight bank for curated facts.
type MemoryRecallTool struct {
	manager *memory.Manager
}

// NewMemoryRecallTool creates a new memory recall tool.
func NewMemoryRecallTool(manager *memory.Manager) *MemoryRecallTool {
	return &MemoryRecallTool{manager: manager}
}

func (t *MemoryRecallTool) Name() string { return "memory_recall" }

func (t *MemoryRecallTool) Category() string { return "memory" }

func (t *MemoryRecallTool) Description() string {
	return "Search the Hindsight bank for curated facts. Use this to retrieve deliberately retained knowledge relevant to the current task. Recall returns facts that were intentionally saved, not automatic conversation history."
}

func (t *MemoryRecallTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"query": {
				Type:        schemaTypeString,
				Description: "Search query for finding relevant retained facts.",
			},
			"domain": {
				Type:        schemaTypeString,
				Description: "Optional domain filter (e.g., 'architecture', 'debugging').",
			},
			"min_importance": {
				Type:        schemaTypeString,
				Description: "Minimum importance level to include.",
				Enum:        []string{"high", "medium", "low"},
			},
			"limit": {
				Type:        schemaTypeInteger,
				Description: "Maximum number of results to return.",
			},
		},
		Required: []string{"query"},
	}
}

// MemoryRecallResult is returned after recalling facts.
type MemoryRecallResult struct {
	Success bool              `json:"success"`
	Results []MemoryFact      `json:"results,omitempty"`
	Count   int               `json:"count"`
	Message string            `json:"message"`
}

// MemoryFact represents a recalled fact from the Hindsight bank.
type MemoryFact struct {
	ID           string  `json:"id"`
	Content      string  `json:"content"`
	Domain       string  `json:"domain,omitempty"`
	Importance   string  `json:"importance"`
	RetrievedAt  string  `json:"retrieved_at"`
}

func (t *MemoryRecallTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	domain, _ := args["domain"].(string)
	minImportance, _ := args["min_importance"].(string)
	limitRaw, _ := args["limit"].(int)
	if limitRaw == 0 {
		limitRaw = 10
	}

	// Search memories with hindsight category
	results, err := t.manager.Search(ctx, memory.MemoryQuery{
		Query:    query,
		Type:     memory.MemoryTypeTask,
		Category: "hindsight",
		Limit:    limitRaw,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to recall memories: %w", err)
	}

	// Filter and convert results
	facts := make([]MemoryFact, 0, len(results))
	for _, r := range results {
		// Skip if importance filter applies
		if minImportance != "" && r.Memory.Metadata != nil {
			if imp, ok := r.Memory.Metadata["importance"].(string); ok {
				if importanceRank(imp) < importanceRank(minImportance) {
					continue
				}
			}
		}

		// Extract domain from metadata or category
		dom := domain
		if dom == "" && r.Memory.Metadata != nil {
			if d, ok := r.Memory.Metadata["domain"].(string); ok {
				dom = d
			}
		}

		// Extract importance
		imp := "medium"
		if r.Memory.Metadata != nil {
			if i, ok := r.Memory.Metadata["importance"].(string); ok {
				imp = i
			}
		}

		facts = append(facts, MemoryFact{
			ID:          r.Memory.ID,
			Content:     r.Memory.Content,
			Domain:      dom,
			Importance:  imp,
			RetrievedAt: time.Now().Format(time.RFC3339),
		})
	}

	return MemoryRecallResult{
		Success: true,
		Results: facts,
		Count:   len(facts),
		Message: fmt.Sprintf("Recalled %d facts from hindsight bank", len(facts)),
	}, nil
}

// importanceRank returns a numeric rank for importance (higher = more important).
func importanceRank(level string) int {
	switch level {
	case "high":
		return 3
	case "medium":
		return 2
	case "low":
		return 1
	default:
		return 0
	}
}

// MemoryReflectTool performs meta-cognition on stored memories to generate insights.
type MemoryReflectTool struct {
	manager   *memory.Manager
	llmClient *llm.Client
}

// NewMemoryReflectTool creates a new memory reflect tool.
func NewMemoryReflectTool(manager *memory.Manager, llmClient *llm.Client) *MemoryReflectTool {
	return &MemoryReflectTool{
		manager:   manager,
		llmClient: llmClient,
	}
}

func (t *MemoryReflectTool) Name() string { return "memory_reflect" }

func (t *MemoryReflectTool) Category() string { return "memory" }

func (t *MemoryReflectTool) Description() string {
	return "Perform meta-cognition on stored memories to generate insights, identify patterns, or synthesize learnings. Use this to reflect on accumulated knowledge and extract higher-level understanding from individual facts."
}

func (t *MemoryReflectTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"domain": {
				Type:        schemaTypeString,
				Description: "Domain to reflect on (e.g., 'architecture', 'debugging', 'team-process').",
			},
			"prompt": {
				Type:        schemaTypeString,
				Description: "Specific reflection prompt or question to guide the analysis.",
			},
			"min_importance": {
				Type:        schemaTypeString,
				Description: "Minimum importance level to include in reflection.",
				Enum:        []string{"high", "medium", "low"},
			},
		},
		Required: []string{"prompt"},
	}
}

// MemoryReflectResult contains reflection insights.
type MemoryReflectResult struct {
	Success    bool     `json:"success"`
	Insights   []string `json:"insights,omitempty"`
	Summary    string   `json:"summary"`
	FactsUsed  int      `json:"facts_used"`
	Message    string   `json:"message"`
}

func (t *MemoryReflectTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	prompt, _ := args["prompt"].(string)
	if prompt == "" {
		return nil, fmt.Errorf("prompt is required")
	}

	domain, _ := args["domain"].(string)
	minImportance, _ := args["min_importance"].(string)

	// Gather relevant memories
	queryStr := prompt
	if domain != "" {
		queryStr = domain + " " + prompt
	}

	results, err := t.manager.Search(ctx, memory.MemoryQuery{
		Query:    queryStr,
		Type:     memory.MemoryTypeTask,
		Category: "hindsight",
		Limit:    50,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to gather memories for reflection: %w", err)
	}

	// Filter by importance and domain
	var filteredMemories []memory.Memory
	for _, r := range results {
		// Filter by domain if specified
		if domain != "" && r.Memory.Metadata != nil {
			if d, ok := r.Memory.Metadata["domain"].(string); ok && d != domain {
				continue
			}
		}

		// Filter by importance
		if minImportance != "" && r.Memory.Metadata != nil {
			if imp, ok := r.Memory.Metadata["importance"].(string); ok {
				if importanceRank(imp) < importanceRank(minImportance) {
					continue
				}
			}
		}

		filteredMemories = append(filteredMemories, r.Memory)
	}

	if len(filteredMemories) == 0 {
		return MemoryReflectResult{
			Success:   false,
			Message:   "No relevant memories found for reflection",
			FactsUsed: 0,
		}, nil
	}

	// Build context from memories
	var factsContext strings.Builder
	factsContext.WriteString("The following facts have been retained in the hindsight bank:\n\n")
	for i, mem := range filteredMemories {
		dom := ""
		if mem.Metadata != nil {
			if d, ok := mem.Metadata["domain"].(string); ok {
				dom = d
			}
		}
		imp := "medium"
		if mem.Metadata != nil {
			if i2, ok := mem.Metadata["importance"].(string); ok {
				imp = i2
			}
		}
		factsContext.WriteString(fmt.Sprintf("[%d] (domain: %s, importance: %s) %s\n", i+1, dom, imp, mem.Content))
	}

	// Call LLM for reflection
	messages := []llm.ChatMessage{
		{
			Role: llm.RoleSystem,
			Content: "You are performing meta-cognition on retained memories. Analyze the provided facts and generate insights, identify patterns, or synthesize learnings based on the user's reflection prompt.",
		},
		{
			Role: llm.RoleUser,
			Content: fmt.Sprintf("%s\n\n---\n\nReflection prompt: %s\n\nGenerate 3-5 key insights and a brief summary.", factsContext.String(), prompt),
		},
	}

	response, err := t.llmClient.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to generate reflection: %w", err)
	}

	// Parse response into structured insights
	insights := parseInsightsFromResponse(response.Content)

	return MemoryReflectResult{
		Success:   true,
		Insights:  insights,
		Summary:   response.Content,
		FactsUsed: len(filteredMemories),
		Message:   fmt.Sprintf("Generated reflection from %d facts", len(filteredMemories)),
	}, nil
}

// parseInsightsFromResponse extracts bullet-point insights from LLM response.

// numberedInsightRegex matches numbered list items like "1." or "1)"
var numberedInsightRegex = regexp.MustCompile(`^\d+[\.\)]\s*`)

// parseInsightsFromResponse extracts bullet-point insights from LLM response.
func parseInsightsFromResponse(content string) []string {
	lines := strings.Split(content, "\n")
	insights := make([]string, 0, 5)
	
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for numbered or bulleted insights
		if strings.HasPrefix(line, "- ") || strings.HasPrefix(line, "* ") || strings.HasPrefix(line, "• ") {
			insights = append(insights, strings.TrimPrefix(strings.TrimPrefix(strings.TrimPrefix(line, "- "), "* "), "• "))
		} else if numberedInsightRegex.MatchString(line) {
			// Numbered list like "1." or "1)"
			insights = append(insights, numberedInsightRegex.ReplaceAllString(line, ""))
		}
	}
	
	if len(insights) == 0 {
		// Fall back to treating entire response as summary
		insights = append(insights, content)
	}
	
	return insights
}

// Ensure all curation tools implement the Tool interface.
var _ tools.Tool = (*MemoryRetainTool)(nil)
var _ tools.Tool = (*MemoryRecallTool)(nil)
var _ tools.Tool = (*MemoryReflectTool)(nil)
