// Package employee — types.go contains the Employee wrapper type and
// shared package-level constants/helpers. The Employee wraps a
// bot.BotDefinition (embedded for field reuse) and attaches a
// Constitution (the new structured layer added by this package).
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md.
package employee

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// Employee is the runtime representation of an AI employee: a
// bot.BotDefinition (storage + execution shape, unchanged) plus the
// Constitution that constrains it.
//
// Embedding bot.BotDefinition means callers can access ID, Name,
// Triggers, Tools, etc. directly on an Employee value while the
// Constitution is available as a named field. The embedded value is
// exported so JSON round-trips cleanly: a serialized Employee looks
// like {"id":..., "name":..., ..., "constitution": {...}}.
type Employee struct {
	bot.BotDefinition
	// Constitution is required (no-constitution employees refuse to
	// load). Storing it as a named field keeps it out of the embedded
	// BotDefinition's JSON shape.
	Constitution Constitution `json:"constitution"`
}

// Validate runs both the embedded BotDefinition's validation and the
// Constitution's self-consistency validation (with the employee's own
// ID for self-escalation detection). Returns a joined error listing
// every problem found, so operators see the whole punch-list in one
// shot rather than fixing-and-retrying in a loop.
func (e *Employee) Validate() error {
	if e == nil {
		return errors.New("employee is nil")
	}
	var errs []error
	if err := e.BotDefinition.Validate(); err != nil {
		errs = append(errs, fmt.Errorf("bot definition: %w", err))
	}
	if err := e.Constitution.Validate(e.ID); err != nil {
		errs = append(errs, fmt.Errorf("constitution: %w", err))
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// HasConstitution reports whether the employee has a non-empty
// constitution. A constitution with an empty Purpose and zero
// ApprovedAt is treated as missing — this matches the "constitution
// required at load time" rule from the spec.
func (e *Employee) HasConstitution() bool {
	if e == nil {
		return false
	}
	return e.Constitution.Purpose != "" || !e.Constitution.ApprovedAt.IsZero()
}

// EmployeeStatus mirrors bot.BotStatus but is kept separate so the
// employee package can extend it later (e.g. with an "amending" state)
// without churning the bot package. The string values match
// bot.BotStatus values for compatibility.
type EmployeeStatus string

const (
	EmployeeStatusRunning EmployeeStatus = "running"
	EmployeeStatusPaused  EmployeeStatus = "paused"
	EmployeeStatusError   EmployeeStatus = "error"
	EmployeeStatusStopped EmployeeStatus = "stopped"
)

// FromBotStatus converts a bot.BotStatus to an EmployeeStatus. Unknown
// values fall through to EmployeeStatusStopped (the conservative
// default) — but this should never happen in practice because every
// BotStatus value has a matching EmployeeStatus.
func FromBotStatus(s bot.BotStatus) EmployeeStatus {
	switch s {
	case bot.BotStatusRunning:
		return EmployeeStatusRunning
	case bot.BotStatusPaused:
		return EmployeeStatusPaused
	case bot.BotStatusError:
		return EmployeeStatusError
	case bot.BotStatusStopped:
		return EmployeeStatusStopped
	default:
		return EmployeeStatusStopped
	}
}

// DefaultAssessmentInterval is the fallback cadence used when a tier 2+
// constitution omits Constraints.AssessmentInterval. Matches the
// default in the spec's config block ("15m").
const DefaultAssessmentInterval = 15 * time.Minute

// DefaultPeriodicAuditInterval is the fallback cadence for the periodic
// audit job. Matches the spec's "periodic_interval: 6h".
const DefaultPeriodicAuditInterval = 6 * time.Hour

// DefaultDriftPauseThreshold is the default drift score above which an
// employee is auto-paused. Matches the spec's
// "drift_pause_threshold: 0.3".
const DefaultDriftPauseThreshold = 0.3

// RolePrefix is the namespace prefix for escalation sinks that are
// roles rather than agent IDs. Any EscalatesTo entry starting with
// RolePrefix (e.g. "role:user", "role:oncall") is treated as a terminal
// leaf in the escalation graph — never resolved against the agent
// registry, never participates in cycle detection.
const RolePrefix = "role:"

// UserEscalationID is the canonical role-prefixed identifier for the
// human operator. New constitutions should use this; legacy
// constitutions using the bare string "user" are auto-normalised by
// NormalizeEscalatesTo at load time.
const UserEscalationID = "role:user"

// legacyRoleSentinels maps bare role names from the pre-prefix era to
// their canonical role:-prefixed forms. This lets old constitutions
// (using e.g. "user", "system", "operator") load without modification
// while new constitutions use the extensible "role:" prefix.
var legacyRoleSentinels = map[string]string{
	"user":      "role:user",
	"system":    "role:system",
	"operator":  "role:operator",
	"oncall":    "role:oncall",
	"admin":     "role:admin",
}

// IsRoleSentinel reports whether id is a terminal role rather than an
// agent ID. Returns true for:
//   - Any ID with the "role:" prefix (e.g. "role:user", "role:oncall").
//   - Legacy bare sentinels ("user", "system", "operator", "oncall", "admin").
//
// The escalation-graph DFS uses this to skip resolution against the
// agent registry and cycle detection for these terminal nodes.
func IsRoleSentinel(id string) bool {
	if strings.HasPrefix(id, RolePrefix) {
		return true
	}
	_, ok := legacyRoleSentinels[id]
	return ok
}

// NormalizeEscalatesTo maps legacy bare sentinels to their canonical
// "role:"-prefixed forms. Entries that already have the prefix or are
// agent IDs are passed through unchanged. Call this at constitution
// load time so the rest of the system only deals with canonical forms.
func NormalizeEscalatesTo(ids []string) []string {
	if len(ids) == 0 {
		return ids
	}
	out := make([]string, len(ids))
	for i, id := range ids {
		if canonical, ok := legacyRoleSentinels[id]; ok {
			out[i] = canonical
		} else {
			out[i] = id
		}
	}
	return out
}
