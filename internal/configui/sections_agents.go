// internal/configui/sections_agents.go
package configui

import (
	"sort"

	"github.com/caimlas/meept/internal/config"
)

func buildAgentsFields() []Field {
	agents, _ := config.LoadAgentDefinitionsDefault(nil)
	return []Field{
		NewDrilldownField("agents", "agent definitions", buildAgentItems(agents)),
	}
}

func buildAgentItems(agents map[string]*config.AgentDefinition) []DrilldownItem {
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
			NewTextField("model", "model", a.Model),
			NewToggleField("enabled", "enabled", a.Enabled),
			NewToggleField("can_delegate", "can delegate", a.CanDelegate),
		}
		items = append(items, DrilldownItem{Name: a.ID, Fields: fields})
	}
	return items
}
