// internal/configui/sections_agents.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildAgentsFields() []Field {
	agents, _ := config.LoadAgentDefinitionsDefault(nil)
	return []Field{
		NewDrilldownField("agents", "agent definitions", len(agents)),
	}
}
