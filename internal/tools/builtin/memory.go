// Package builtin provides built-in tool implementations for meept.
package builtin

import (
	"context"
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
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"content": {
				Type:        "string",
				Description: "The content to store in memory. Should be clear and self-contained.",
			},
			"type": {
				Type:        "string",
				Description: "Memory type: 'episodic' for conversations/interactions, 'task' for technical knowledge.",
				Enum:        []string{"episodic", "task"},
			},
			"category": {
				Type:        "string",
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
		memTypeStr = "episodic"
	}

	var memType memory.MemoryType
	switch memTypeStr {
	case "episodic":
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
		"memory_id": id,
		"type":      memTypeStr,
		"category":  category,
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
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"query": {
				Type:        "string",
				Description: "Search query to find relevant memories.",
			},
			"type": {
				Type:        "string",
				Description: "Optional: filter to 'episodic' or 'task' memories only.",
				Enum:        []string{"episodic", "task", ""},
			},
			"limit": {
				Type:        "integer",
				Description: "Maximum number of results to return (default 10, max 50).",
			},
			"min_relevance": {
				Type:        "number",
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
		limit = int(l)
		if limit > 50 {
			limit = 50
		}
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
		case "episodic":
			memQuery.Type = memory.MemoryTypeEpisodic
		case "task":
			memQuery.Type = memory.MemoryTypeTask
		}
	}

	results, err := t.manager.Search(ctx, memQuery)
	if err != nil {
		return nil, fmt.Errorf("memory search failed: %w", err)
	}

	// Format results for output
	formatted := make([]map[string]any, 0, len(results))
	for _, r := range results {
		formatted = append(formatted, map[string]any{
			"id":        r.Memory.ID,
			"content":   r.Memory.Content,
			"type":      string(r.Memory.Type),
			"category":  r.Memory.Category,
			"relevance": r.RelevanceScore,
			"source":    r.Source,
		})
	}

	return map[string]any{
		"results": formatted,
		"count":   len(formatted),
		"query":   query,
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
		Type: "object",
		Properties: map[string]llm.ParameterProperty{
			"query": {
				Type:        "string",
				Description: "The query or topic to get relevant context for.",
			},
			"max_items": {
				Type:        "integer",
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
		maxItems = int(m)
		if maxItems > 30 {
			maxItems = 30
		}
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
			"content":   r.Memory.Content,
			"type":      string(r.Memory.Type),
			"category":  r.Memory.Category,
			"relevance": r.RelevanceScore,
		})
	}

	return map[string]any{
		"context": formatted,
		"count":   len(formatted),
		"query":   query,
	}, nil
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool = (*MemoryStoreTool)(nil)
	_ tools.Tool = (*MemorySearchTool)(nil)
	_ tools.Tool = (*MemoryGetContextTool)(nil)
)
