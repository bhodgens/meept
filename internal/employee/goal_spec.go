package employee

import (
	"fmt"
	"time"
)

// ErrInvalidGoalSpec is returned (wrapped) by GoalSpec.Validate when a
// creation request fails structural validation.
var ErrInvalidGoalSpec = fmt.Errorf("employee: invalid goal spec")

// GoalSpec is the creation request shape for goals (SHARED-CONVENTIONS C1).
// It is the single wire type shared by the RPC handler, CLI, HTTP handler,
// and the employee definition-file parser.
//
// Owner: 01-goal-ingestion leaf. Consumers: RPC handler, CLI, HTTP handler,
// definition-file parser.
type GoalSpec struct {
	EmployeeID string        `json:"employee_id"`
	Title      string        `json:"title"`
	Mandate    string        `json:"mandate"`
	Source     string        `json:"source,omitempty"` // "user" | "trigger"; default "user"
	TriggerRef string        `json:"trigger_ref,omitempty"`
	Gate       *GoalGateSpec `json:"gate,omitempty"`
}

// GoalGateSpec is the per-goal quality-gate configuration in a GoalSpec.
type GoalGateSpec struct {
	Command           string `json:"command,omitempty"`
	TimeoutSeconds    int    `json:"timeout_seconds,omitempty"`
	SkipWhenUnchanged bool   `json:"skip_when_unchanged,omitempty"`
}

// Validate checks the spec's structural requirements: employee_id, title and
// mandate are required; source must be "" (defaults to user), "user" or
// "trigger". Tier-sensitive source checks (ValidateGoalSource) are the
// manager's job — they need the employee's constitution.
func (s GoalSpec) Validate() error {
	if s.EmployeeID == "" {
		return fmt.Errorf("%w: employee_id is required", ErrInvalidGoalSpec)
	}
	if s.Title == "" {
		return fmt.Errorf("%w: title is required", ErrInvalidGoalSpec)
	}
	if s.Mandate == "" {
		return fmt.Errorf("%w: mandate is required", ErrInvalidGoalSpec)
	}
	switch GoalSource(s.Source) {
	case "", SourceUser, SourceTrigger:
	default:
		return fmt.Errorf("%w: source must be %q or %q, got %q",
			ErrInvalidGoalSpec, SourceUser, SourceTrigger, s.Source)
	}
	return nil
}

// toGoal maps the spec onto a Goal ready for GoalStore.Create. An empty
// Source defaults to SourceUser; a nil gate or a gate with an empty Command
// maps to a nil *GateConfig (no gate). State is GoalActive and Health is
// GoalUnknown (never assessed).
func (s GoalSpec) toGoal(now time.Time) *Goal {
	source := GoalSource(s.Source)
	if source == "" {
		source = SourceUser
	}
	g := &Goal{
		EmployeeID: s.EmployeeID,
		Title:      s.Title,
		Mandate:    s.Mandate,
		State:      GoalActive,
		Source:     source,
		TriggerRef: s.TriggerRef,
		Health:     GoalUnknown,
		CreatedAt:  now,
	}
	if s.Gate != nil && s.Gate.Command != "" {
		g.Gate = &GateConfig{
			Command:           s.Gate.Command,
			TimeoutSeconds:    s.Gate.TimeoutSeconds,
			SkipWhenUnchanged: s.Gate.SkipWhenUnchanged,
		}
	}
	return g
}
