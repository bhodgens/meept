// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
	"slices"

	"fmt"

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
				Enum:        []string{schemaMemoryEpisodic, "task"},
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
	case "task":
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
		"success":   true,
		schemaPropMemoryID: id,
		schemaPropType:      memTypeStr,
		schemaPropCategory:  category,
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
				Enum:        []string{schemaMemoryEpisodic, "task", ""},
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
		case "task":
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
			"id":        r.Memory.ID,
			schemaPropContent:   r.Memory.Content,
			schemaPropType:      string(r.Memory.Type),
			schemaPropCategory:  r.Memory.Category,
			"relevance": r.RelevanceScore,
			"source":    r.Source,
		})
	}

	return map[string]any{
		"results": formatted,
		schemaPropCount:   len(formatted),
		schemaPropQuery:   query,
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
			"id":        r.Memory.ID,
			schemaPropContent:   r.Memory.Content,
			schemaPropType:      string(r.Memory.Type),
			schemaPropCategory:  r.Memory.Category,
			"relevance": r.RelevanceScore,
		})
	}

	return map[string]any{
		"context": formatted,
		schemaPropCount:   len(formatted),
		schemaPropQuery:   query,
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
		"id":         mem.ID,
		schemaPropContent:    mem.Content,
		schemaPropType:       string(mem.Type),
		schemaPropCategory:   mem.Category,
		"created_at": mem.CreatedAt,
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
			"versions":  []any{},
			schemaPropMessage:   "No version history found",
		}, nil
	}

	// Build version history result
	versions := make([]map[string]any, 0, len(memories))
	for _, mem := range memories {
		version := map[string]any{
			"id":         mem.ID,
			schemaPropContent:    mem.Content,
			"created_at": mem.CreatedAt,
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
		schemaPropMemoryID:       memoryID,
		"version_count":   len(versions),
		"versions":        versions,
		"current_version": getCurrentVersionFromList(versions),
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
