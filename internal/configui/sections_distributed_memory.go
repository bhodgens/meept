// internal/configui/sections_distributed_memory.go
package configui

import "github.com/caimlas/meept/internal/config"

func buildDistributedMemoryFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.DistributedMemory
	return []Field{
		NewToggleField("enabled", "enabled", s.Enabled),
		NewSelectField("mode", "mode", s.Mode, []string{"local", "distributed"}),
		NewDrilldownField("sync", "sync", []DrilldownItem{
			{Name: "sync", Fields: []Field{
				NewToggleField("sync.hydrate_on_claim", "hydrate on claim", s.Sync.HydrateOnClaim),
				NewNumberField("sync.hydration_limit", "hydration limit", s.Sync.HydrationLimit),
				NewToggleField("sync.distill_on_complete", "distill on complete", s.Sync.DistillOnComplete),
				NewNumberField("sync.periodic_distill_interval_minutes", "periodic distill interval minutes", s.Sync.PeriodicDistillIntervalMinutes),
				NewToggleField("sync.retry_on_failure", "retry on failure", s.Sync.RetryOnFailure),
				NewNumberField("sync.max_retries", "max retries", s.Sync.MaxRetries),
			}},
		}),
		NewDrilldownField("distillation", "distillation", []DrilldownItem{
			{Name: "distillation", Fields: []Field{
				NewFloatField("distillation.pagerank_threshold", "pagerank threshold", s.Distillation.PageRankThreshold),
				NewNumberField("distillation.hub_connectivity_threshold", "hub connectivity threshold", s.Distillation.HubConnectivityThreshold),
				NewToggleField("distillation.promote_task_completions", "promote task completions", s.Distillation.PromoteTaskCompletions),
				NewNumberField("distillation.cross_agent_references_min", "cross agent references min", s.Distillation.CrossAgentReferencesMin),
				NewNumberField("distillation.min_memory_age_minutes", "min memory age minutes", s.Distillation.MinMemoryAgeMinutes),
			}},
		}),
	}
}
