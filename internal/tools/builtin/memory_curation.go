package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/tools"
)

// RetainTool explicitly queues a fact into the "hindsight bank" for long-term
// retention. Unlike memory_store, this is agent-curated: the agent decides
// the fact is worth remembering permanently, not just recording an event.
type RetainTool struct {
	manager *memory.Manager
}

// NewRetainTool creates a new retain tool.
func NewRetainTool(manager *memory.Manager) *RetainTool {
	return &RetainTool{manager: manager}
}

func (t *RetainTool) Name() string                      { return "retain" }
func (t *RetainTool) Category() string                  { return "memory" }
func (t *RetainTool) TerminateHint(map[string]any) bool { return true }

func (t *RetainTool) Description() string {
	return "Retain a fact, decision, or insight for long-term recall. Use this when you discover something important that should be remembered across all future sessions, such as architectural decisions, bug root causes, or verified facts."
}

func (t *RetainTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropContent: {
				Type:        schemaTypeString,
				Description: "The concise, self-contained fact to retain. Write in third person and avoid references to 'today' or 'recently'.",
			},
			"importance": {
				Type:        schemaTypeString,
				Description: "Importance level: 'critical' (blocks work if forgotten), 'high' (major time saver), 'normal' (useful context).",
				Enum:        []string{"critical", "high", "normal"},
			},
			schemaPropCategory: {
				Type:        schemaTypeString,
				Description: "Category for organization (e.g., 'architecture', 'bugfix', 'api_behavior', 'decision').",
			},
		},
		Required: []string{"content"},
	}
}

func (t *RetainTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	content, _ := args["content"].(string)
	if content == "" {
		return nil, fmt.Errorf("content is required")
	}

	importance, _ := args["importance"].(string)
	if importance == "" {
		importance = "normal"
	}
	category, _ := args["category"].(string)
	if category == "" {
		category = "curated"
	}

	// Add importance metadata to content for richer retrieval
	annotated := fmt.Sprintf("[%s] %s", strings.ToUpper(importance), content)

	mem := memory.Memory{
		Content:  annotated,
		Type:     memory.MemoryTypeTask,
		Category: category,
		Metadata: map[string]any{
			"source":     "retain_tool",
			"importance": importance,
		},
	}

	id, err := t.manager.Store(ctx, mem)
	if err != nil {
		return nil, fmt.Errorf("failed to retain: %w", err)
	}

	return map[string]any{
		"success":    true,
		"memory_id":  id,
		"importance": importance,
		"category":   category,
	}, nil
}

// RecallTool searches the hindsight bank for previously retained facts.
// It is a semantic wrapper around memory_search optimized for recalling
// explicitly curated knowledge.
type RecallTool struct {
	manager *memory.Manager
}

// NewRecallTool creates a new recall tool.
func NewRecallTool(manager *memory.Manager) *RecallTool {
	return &RecallTool{manager: manager}
}

func (t *RecallTool) Name() string     { return "recall" }
func (t *RecallTool) Category() string { return "memory" }

func (t *RecallTool) Description() string {
	return "Recall previously retained facts, decisions, or insights. Use this when you need to verify something you learned earlier or want to avoid re-learning known behavior."
}

func (t *RecallTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			schemaPropQuery: {
				Type:        schemaTypeString,
				Description: "What do you want to recall? Use keywords or a short question.",
			},
			schemaPropLimit: {
				Type:        schemaTypeInteger,
				Description: "Maximum results (default 5, max 20).",
			},
		},
		Required: []string{"query"},
	}
}

func (t *RecallTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	query, _ := args["query"].(string)
	if query == "" {
		return nil, fmt.Errorf("query is required")
	}

	limit := 5
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = min(int(l), 20)
	}

	memQuery := memory.MemoryQuery{
		Query:        query,
		Limit:        limit,
		MinRelevance: 0.2, // slightly lower for recall breadth
	}

	results, err := t.manager.SearchWithGraph(ctx, memQuery, 0.3)
	if err != nil {
		return nil, fmt.Errorf("recall failed: %w", err)
	}

	formatted := make([]map[string]any, 0, len(results))
	for _, r := range results {
		formatted = append(formatted, map[string]any{
			"id":        r.Memory.ID,
			"content":   r.Memory.Content,
			"category":  r.Memory.Category,
			"relevance": r.RelevanceScore,
		})
	}

	return map[string]any{
		"results": formatted,
		"count":   len(formatted),
		"query":   query,
	}, nil
}

// ReflectTool reviews recent retained facts and produces a concise summary
// of themes, conflicts, and actionable insights. This helps the agent
// periodically consolidate curated knowledge.
type ReflectTool struct {
	manager *memory.Manager
}

// NewReflectTool creates a new reflect tool.
func NewReflectTool(manager *memory.Manager) *ReflectTool {
	return &ReflectTool{manager: manager}
}

func (t *ReflectTool) Name() string                      { return "reflect" }
func (t *ReflectTool) Category() string                  { return "memory" }
func (t *ReflectTool) TerminateHint(map[string]any) bool { return true }

func (t *ReflectTool) Description() string {
	return "Reflect on recently retained facts to identify patterns, conflicts, or gaps. Use this at natural breakpoints (end of task, before planning) to consolidate what you've learned."
}

func (t *ReflectTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"topic": {
				Type:        schemaTypeString,
				Description: "Optional topic filter. If empty, reflects on all recent retained facts.",
			},
			"limit": {
				Type:        schemaTypeInteger,
				Description: "Number of recent facts to reflect on (default 20, max 50).",
			},
		},
		Required: []string{},
	}
}

func (t *ReflectTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.manager == nil {
		return nil, fmt.Errorf("memory manager not configured")
	}

	topic, _ := args["topic"].(string)
	limit := 20
	if l, ok := args["limit"].(float64); ok && l > 0 {
		limit = min(int(l), 50)
	}

	// Query for retained facts
	query := ""
	if topic != "" {
		query = topic
	} else {
		query = "recent retained facts"
	}

	memQuery := memory.MemoryQuery{
		Query:        query,
		Limit:        limit,
		MinRelevance: 0.0,
	}

	results, err := t.manager.SearchWithGraph(ctx, memQuery, 0.1)
	if err != nil {
		return nil, fmt.Errorf("reflect search failed: %w", err)
	}

	if len(results) == 0 {
		return map[string]any{
			"facts_count": 0,
			"themes":      []string{},
			"summary":     "No retained facts found to reflect on.",
		}, nil
	}

	// Simple theme extraction by category
	themeMap := make(map[string][]string)
	for _, r := range results {
		cat := r.Memory.Category
		if cat == "" {
			cat = "uncategorized"
		}
		themeMap[cat] = append(themeMap[cat], r.Memory.Content)
	}

	themes := make([]map[string]any, 0, len(themeMap))
	for cat, items := range themeMap {
		themes = append(themes, map[string]any{
			"category": cat,
			"count":    len(items),
		})
	}

	return map[string]any{
		"facts_count": len(results),
		"themes":      themes,
		"summary":     fmt.Sprintf("Reflected on %d retained facts across %d categories.", len(results), len(themeMap)),
	}, nil
}

// Ensure tools implement the Tool interface
var (
	_ tools.Tool            = (*RetainTool)(nil)
	_ tools.Tool            = (*RecallTool)(nil)
	_ tools.Tool            = (*ReflectTool)(nil)
	_ tools.TerminatingTool = (*RetainTool)(nil)
	_ tools.TerminatingTool = (*ReflectTool)(nil)
)
