// internal/configui/sections_orchestrator.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildOrchestratorFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Orchestrator
	return []Field{
		NewNumberField("max_plan_steps", "max plan steps", s.MaxPlanSteps),
		NewNumberField("max_research_steps", "max research steps", s.MaxResearchSteps),
		NewNumberField("planner_timeout", "planner timeout", s.PlannerTimeout),
		NewNumberField("token_budget_alert", "token budget alert", s.TokenBudgetAlert),
	}
}
