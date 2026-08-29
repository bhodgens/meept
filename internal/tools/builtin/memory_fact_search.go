package builtin

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/tools"
)

// MemoryFactSearchTool retrieves typed user-memory facts
// (harness-eval leaf 12): active preference/restriction/account/temporal
// facts from the memory_facts store. Retrieve-as-tool, not always-inject.
type MemoryFactSearchTool struct {
	tools.ToolDefaults
	manager *memory.Manager
}

// NewMemoryFactSearchTool builds the tool over the memory Manager's fact
// store. A nil store (store not opened) makes Execute return an honest
// empty result rather than an error.
func NewMemoryFactSearchTool(manager *memory.Manager) *MemoryFactSearchTool {
	return &MemoryFactSearchTool{manager: manager}
}

func (t *MemoryFactSearchTool) Name() string { return "memory_fact_search" }

func (t *MemoryFactSearchTool) Category() string { return "memory" }

func (t *MemoryFactSearchTool) Description() string {
	return "search the user's typed long-term facts (preferences, dietary restrictions, account identifiers, dated commitments). use for personalization questions like seat preference, allergies, loyalty numbers, or upcoming plans."
}

func (t *MemoryFactSearchTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"query": {
				Type:        schemaTypeString,
				Description: "substring to match against fact keys and values (case-insensitive). empty matches all.",
			},
			"kind": {
				Type:        schemaTypeString,
				Description: "optional fact kind filter.",
				Enum:        []string{"preference", "restriction", "account", "temporal"},
			},
		},
		Required: []string{},
	}
}

// MemoryFactSearchResult is the tool result envelope.
type MemoryFactSearchResult struct {
	Success bool                `json:"success"`
	Facts   []memory.MemoryFact `json:"facts"`
	Count   int                 `json:"count"`
	Message string              `json:"message"`
}

func (t *MemoryFactSearchTool) Execute(_ context.Context, args map[string]any) (any, error) {
	query, _ := args["query"].(string)
	kind, _ := args["kind"].(string)

	out := MemoryFactSearchResult{
		Success: true,
		Facts:   []memory.MemoryFact{},
		Count:   0,
	}

	store := t.manager.GetFactStore()
	if store == nil {
		// Store not opened: honest empty result, not an error.
		out.Message = "no fact store open"
		return out, nil
	}

	// Owner: daemon owner (empty) in multiuser-off. Identity wiring for
	// multiuser arrives with the session-ownership integration.
	facts, err := store.Search(context.Background(), "", query, kind)
	if err != nil {
		return nil, fmt.Errorf("memory_fact_search: %w", err)
	}
	if facts == nil {
		facts = []memory.MemoryFact{}
	}

	out.Facts = facts
	out.Count = len(facts)
	out.Message = fmt.Sprintf("%d fact(s)", len(facts))
	if strings.TrimSpace(query) == "" && kind == "" && len(facts) == 0 {
		out.Message = "no facts stored yet"
	}
	return out, nil
}
