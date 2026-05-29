// internal/configui/sections_agents.go
package configui

import (
	"slices"
	"sort"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

func buildAgentsFields() []Field {
	agents, _ := config.LoadAgentDefinitionsDefault(nil)
	return []Field{
		NewDrilldownField("agents", "agent definitions", buildAgentItems(agents)),
	}
}

func buildAgentItems(agents map[string]*config.AgentDefinition) []DrilldownItem {
	// Load models once for all agents
	modelsCfg, _ := llm.LoadProvidersConfigDefault()
	if modelsCfg == nil {
		modelsCfg = &llm.ProvidersConfig{}
	}
	modelOptions := buildModelOptions(modelsCfg)

	ids := make([]string, 0, len(agents))
	for id := range agents {
		ids = append(ids, id)
	}
	sort.Strings(ids)

	items := make([]DrilldownItem, 0, len(ids))
	for _, id := range ids {
		a := agents[id]
		fields := []Field{
			NewTextField("id", "id", a.ID),
			NewTextField("name", "name", a.Name),
			NewSelectField("role", "role", a.Role, []string{"dispatcher", "executor", "conversational", "reviewer"}),
			NewTextField("description", "description", a.Description),
			NewSelectField("model", "model", a.Model, modelOptions),
			NewToggleField("enabled", "enabled", a.Enabled),
			NewToggleField("can_delegate", "can delegate", a.CanDelegate),
			NewTextField("additional_tools", "additional tools", joinStrings(a.AdditionalTools)),
			NewTextField("capabilities", "capabilities", joinStrings(a.Capabilities)),
			NewTextField("prompt_components", "prompt components", joinStrings(a.PromptComponents)),
		}
		items = append(items, DrilldownItem{Name: a.ID, Fields: fields})
	}
	return items
}

// buildModelOptions returns all available models from the configuration
// as "provider-id/model-id" strings, sorted alphabetically.
// Disabled providers are excluded.
func buildModelOptions(cfg *llm.ProvidersConfig) []string {
	if cfg == nil {
		cfg = &llm.ProvidersConfig{}
	}

	var options []string

	// Iterate providers in sorted order for deterministic output
	providerIDs := make([]string, 0, len(cfg.Providers))
	for pid := range cfg.Providers {
		if !slices.Contains(cfg.DisabledProviders, pid) {
			providerIDs = append(providerIDs, pid)
		}
	}
	sort.Strings(providerIDs)

	for _, providerID := range providerIDs {
		provider := cfg.Providers[providerID]
		// Get model IDs in sorted order
		modelIDs := make([]string, 0, len(provider.Models))
		for mid := range provider.Models {
			modelIDs = append(modelIDs, mid)
		}
		sort.Strings(modelIDs)
		for _, modelID := range modelIDs {
			options = append(options, providerID+"/"+modelID)
		}
	}

	sort.Strings(options)
	return options
}
