// internal/configui/sections_orchestrator.go
package configui

func buildOrchestratorFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Orchestrator
	return []Field{
		NewNumberField("max_plan_steps", "max plan steps", s.MaxPlanSteps),
		NewNumberField("max_research_steps", "max research steps", s.MaxResearchSteps),
		NewNumberField("planner_timeout", "planner timeout", s.PlannerTimeout),
		NewNumberField("token_budget_alert", "token budget alert", s.TokenBudgetAlert),
		NewNumberField("max_handoff_steps", "max handoff steps per task", s.MaxHandoffSteps),
		NewToggleField("handoff_use_amendment", "use amendment system for handoffs", s.HandoffUseAmendment),
	}
}
