// internal/configui/sections_agent_loop.go
package configui

import (
	"strings"

	"github.com/caimlas/meept/internal/config"
)

func buildAgentLoopFields() []Field {
	cfg, _ := config.LoadDefault()
	s := &cfg.Agent
	return []Field{
		NewToggleField("progress_enabled", "progress enabled", s.ProgressEnabled),
		NewNumberField("progress_interval_seconds", "progress interval seconds", s.ProgressIntervalSeconds),
		NewDrilldownField("cache", "cache", []DrilldownItem{
			{Name: "cache", Fields: []Field{
				NewToggleField("cache.enabled", "enabled", s.Cache.Enabled),
				NewNumberField("cache.max_entries", "max entries", s.Cache.MaxEntries),
				NewNumberField("cache.default_ttl_seconds", "default ttl seconds", s.Cache.DefaultTTLSeconds),
				NewNumberField("cache.cleanup_freq_seconds", "cleanup freq seconds", s.Cache.CleanupFreqSeconds),
				NewTextField("cache.enabled_tools", "enabled tools", strings.Join(s.Cache.EnabledTools, ", ")),
			}},
		}),
		NewDrilldownField("errors", "errors", []DrilldownItem{
			{Name: "errors", Fields: []Field{
				NewToggleField("errors.detailed_errors", "detailed errors", s.Errors.DetailedErrors),
				NewToggleField("errors.include_examples", "include examples", s.Errors.IncludeExamples),
				NewNumberField("errors.max_suggestion_length", "max suggestion length", s.Errors.MaxSuggestionLength),
			}},
		}),
		NewDrilldownField("review", "review", []DrilldownItem{
			{Name: "review", Fields: []Field{
				NewToggleField("review.enabled", "enabled", s.Review.Enabled),
				NewTextField("review.require_review", "require review", strings.Join(s.Review.RequireReview, ",")),
				NewTextField("review.skip_review", "skip review", strings.Join(s.Review.SkipReview, ",")),
				NewNumberField("review.max_revision_cycles", "max revision cycles", s.Review.MaxRevisionCycles),
				NewTextField("review.auto_approve_patterns", "auto approve patterns", strings.Join(s.Review.AutoApprovePatterns, ",")),
			}},
		}),
		NewDrilldownField("validation", "validation", []DrilldownItem{
			{Name: "validation", Fields: []Field{
				NewToggleField("validation.enabled", "enabled", s.Validation.Enabled),
				NewTextField("validation.require_validation", "require validation", strings.Join(s.Validation.RequireValidation, ",")),
				NewTextField("validation.skip_validation", "skip validation", strings.Join(s.Validation.SkipValidation, ",")),
				NewTextField("validation.skip_validation_agents", "skip validation agents", strings.Join(s.Validation.SkipValidationAgents, ",")),
				NewNumberField("validation.max_validation_loops", "max validation loops", s.Validation.MaxValidationLoops),
			}},
		}),
		NewDrilldownField("watchdog", "watchdog", []DrilldownItem{
			{Name: "watchdog", Fields: []Field{
				NewToggleField("watchdog.enabled", "enabled", s.Watchdog.Enabled),
				NewNumberField("watchdog.timeout_minutes", "timeout minutes", s.Watchdog.TimeoutMinutes),
				NewNumberField("watchdog.heartbeat_interval_sec", "heartbeat interval sec", s.Watchdog.HeartbeatIntervalSec),
				NewNumberField("watchdog.max_iterations", "max iterations", s.Watchdog.MaxIterations),
				NewNumberField("watchdog.stuck_iteration_count", "stuck iteration count", s.Watchdog.StuckIterationCount),
			}},
		}),
		NewDrilldownField("queues", "queues", []DrilldownItem{
			{Name: "queues", Fields: []Field{
				NewToggleField("queues.enabled", "enabled", s.Queues.Enabled),
				NewSelectField("queues.steering_drain", "steering drain", s.Queues.SteeringDrain, []string{"one", "all"}),
				NewSelectField("queues.followup_drain", "followup drain", s.Queues.FollowUpDrain, []string{"one", "all"}),
				NewNumberField("queues.max_steering", "max steering", s.Queues.MaxSteering),
				NewNumberField("queues.max_followup", "max followup", s.Queues.MaxFollowUp),
				NewToggleField("queues.persist_followup", "persist followup", s.Queues.PersistFollowUp),
				NewNumberField("queues.flush_delay_ms", "flush delay ms", s.Queues.FlushDelayMs),
			}},
		}),
	}
}
