package bot

import (
	"fmt"
	"time"

	"github.com/robfig/cron/v3"
)

type TriggerType string

const (
	TriggerTypeCron     TriggerType = "cron"
	TriggerTypeBusEvent TriggerType = "bus_event"
	TriggerTypeWebhook  TriggerType = "webhook"
)

type MemoryScope string

const (
	MemoryScopePrivate  MemoryScope = "private"
	MemoryScopeShared   MemoryScope = "shared"
	MemoryScopeReadOnly MemoryScope = "read_only"
)

type BotTrigger struct {
	Type           TriggerType `json:"type"`
	Schedule       string      `json:"schedule,omitempty"`
	Topic          string      `json:"topic,omitempty"`
	PromptTemplate string      `json:"prompt_template,omitempty"`
	Enabled        bool        `json:"enabled"`
}

func (t *BotTrigger) Validate() error {
	switch t.Type {
	case TriggerTypeCron:
		if t.Schedule == "" {
			return fmt.Errorf("cron trigger requires schedule")
		}
		parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
		if _, err := parser.Parse(t.Schedule); err != nil {
			return fmt.Errorf("invalid cron schedule %q: %w", t.Schedule, err)
		}
	case TriggerTypeBusEvent:
		if t.Topic == "" {
			return fmt.Errorf("bus_event trigger requires topic")
		}
	case TriggerTypeWebhook:
	default:
		return fmt.Errorf("unknown trigger type: %q", t.Type)
	}
	return nil
}

type BotConstraints struct {
	MaxIterations        int           `json:"max_iterations"`
	Timeout              time.Duration `json:"timeout"`
	MaxTokensPerTurn     int           `json:"max_tokens_per_turn,omitempty"`
	DailyBudgetCents     int           `json:"daily_budget_cents,omitempty"`
	MaxInvocationsPerDay int           `json:"max_invocations_per_day,omitempty"`
}

type BotDefinition struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description"`
	Prompt      string         `json:"prompt"`
	Model       string         `json:"model,omitempty"`
	Triggers    []BotTrigger   `json:"triggers"`
	MemoryScope MemoryScope    `json:"memory_scope"`
	Tools       []string       `json:"tools"`
	Constraints BotConstraints `json:"constraints"`
	Enabled     bool           `json:"enabled"`
	// ExecutorAgent names a registry agent definition whose executor is
	// "acp" (docs/workflows/acp-employees-phase3.md D1-A). When set, the
	// employee's GoalLoop EXECUTE turn routes through that agent's loop
	// instead of the default local executor. The reference is validated
	// at daemon wiring time (the agent registry is only in scope there;
	// see selectEmployeeExecutor in internal/daemon). Empty = no ACP
	// executor; persistence is the JSON definition blob so the field
	// round-trips with no schema change.
	ExecutorAgent string    `json:"executor_agent,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (d *BotDefinition) Validate() error {
	if d.ID == "" {
		return fmt.Errorf("bot ID is required")
	}
	if d.Prompt == "" {
		return fmt.Errorf("bot prompt is required")
	}
	if len(d.Triggers) == 0 {
		return fmt.Errorf("at least one trigger is required")
	}
	for i, t := range d.Triggers {
		if err := t.Validate(); err != nil {
			return fmt.Errorf("trigger[%d]: %w", i, err)
		}
	}
	return nil
}

type BotStatus string

const (
	BotStatusRunning BotStatus = "running"
	BotStatusPaused  BotStatus = "paused"
	BotStatusError   BotStatus = "error"
	BotStatusStopped BotStatus = "stopped"
)

type BotState struct {
	DefinitionID        string     `json:"definition_id"`
	Status              BotStatus  `json:"status"`
	LastRunAt           *time.Time `json:"last_run_at,omitempty"`
	LastError           string     `json:"last_error,omitempty"`
	TotalRuns           int        `json:"total_runs"`
	TotalTokensUsed     int        `json:"total_tokens_used"`
	TotalCostCents      int        `json:"total_cost_cents"`
	ConsecutiveFailures int        `json:"consecutive_failures"`
	TodayRuns           int        `json:"today_runs"`
	TodayCostCents      int        `json:"today_cost_cents"`
	TodayDate           string     `json:"today_date"`
}
