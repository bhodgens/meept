// Package employee implements the AI Employee layer on top of internal/bot.
//
// An Employee is a bot.BotDefinition augmented with a Constitution: a
// structured set of purpose, tier, authority, hard constraints, and
// amendment policy. See
// docs/superpowers/specs/2026-06-23-ai-employee-design.md for the full
// design. Phase 1 ships only the data model + validation + escalation
// resolution helpers; the runtime (GoalLoop, enforcement engine) is added
// in subsequent phases.
package employee

import (
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"
)

// AutonomyTier expresses how independently an employee may act.
//
// Tier 1 (Reactive): only runs when triggered; no self-enqueued work.
// Tier 2 (Propose):  monitors and proposes Plans; approval required
//
//	before execution.
//
// Tier 3 (Autonomous): self-enqueues work within constitution
//
//	boundaries (phase 2; data model present, runtime not wired).
type AutonomyTier int

const (
	// Tier1Reactive triggers only; no self-enqueued work.
	Tier1Reactive AutonomyTier = iota
	// Tier2Propose monitors, proposes, then executes after approval.
	Tier2Propose
	// Tier3Autonomous self-enqueues within constitution (phase 2).
	Tier3Autonomous
)

// String returns the canonical JSON wire name for the tier. These names
// are stable: they appear in stored constitutions, TUI badges, and docs.
// The lowercase underscored form matches the spec exactly
// (e.g. "tier_1_reactive").
func (t AutonomyTier) String() string {
	switch t {
	case Tier1Reactive:
		return "tier_1_reactive"
	case Tier2Propose:
		return "tier_2_propose"
	case Tier3Autonomous:
		return "tier_3_autonomous"
	default:
		return fmt.Sprintf("tier_unknown(%d)", int(t))
	}
}

// ParseAutonomyTier parses the canonical wire name. Unknown values fall
// back to Tier1Reactive (the most conservative tier) with an error so
// callers can decide whether to reject or clamp.
func ParseAutonomyTier(s string) (AutonomyTier, error) {
	switch s {
	case "tier_1_reactive", "":
		return Tier1Reactive, nil
	case "tier_2_propose":
		return Tier2Propose, nil
	case "tier_3_autonomous":
		return Tier3Autonomous, nil
	default:
		return Tier1Reactive, fmt.Errorf("unknown autonomy tier %q", s)
	}
}

// MarshalJSON renders the tier as its canonical wire name so constitutions
// round-trip through JSON with human-friendly values rather than raw ints.
func (t AutonomyTier) MarshalJSON() ([]byte, error) {
	return []byte(`"` + t.String() + `"`), nil
}

// UnmarshalJSON accepts the canonical wire name. Empty input is treated
// as Tier1Reactive for forward compatibility with older constitution
// files that omit the field.
func (t *AutonomyTier) UnmarshalJSON(b []byte) error {
	s := strings.Trim(strings.Trim(string(b), `"`), " ")
	parsed, err := ParseAutonomyTier(s)
	if err != nil {
		return err
	}
	*t = parsed
	return nil
}

// EscalationOn enumerates the condition kinds an EscalationTrigger may
// match against. The values are the canonical wire strings used in
// constitutions and audit logs.
type EscalationOn string

const (
	// EscalateOnRiskLevel matches the security engine's computed risk.
	EscalateOnRiskLevel EscalationOn = "risk_level"
	// EscalateOnTool matches a tool name.
	EscalateOnTool EscalationOn = "tool"
	// EscalateOnAction matches a logical action label.
	EscalateOnAction EscalationOn = "action"
	// EscalateOnCost matches a cost threshold (cents).
	EscalateOnCost EscalationOn = "cost"
)

// Valid reports whether the EscalationOn value is one of the recognized
// kinds. Unknown values are rejected at constitution validation time.
func (e EscalationOn) Valid() bool {
	switch e {
	case EscalateOnRiskLevel, EscalateOnTool, EscalateOnAction, EscalateOnCost:
		return true
	default:
		return false
	}
}

// RiskLevelCeiling enumerates the allowed values for
// ConstitutionalConstraints.RiskCeiling. These match the risk bands
// produced by the security engine.
type RiskLevelCeiling string

const (
	RiskCeilingSafe     RiskLevelCeiling = "safe"
	RiskCeilingLow      RiskLevelCeiling = "low"
	RiskCeilingMedium   RiskLevelCeiling = "medium"
	RiskCeilingHigh     RiskLevelCeiling = "high"
	RiskCeilingCritical RiskLevelCeiling = "critical"
)

// Valid reports whether the risk ceiling string is one of the supported
// bands. Empty is allowed (meaning "no ceiling / inherit default").
func (r RiskLevelCeiling) Valid() bool {
	if r == "" {
		return true
	}
	switch r {
	case RiskCeilingSafe, RiskCeilingLow, RiskCeilingMedium, RiskCeilingHigh, RiskCeilingCritical:
		return true
	default:
		return false
	}
}

// Constitution binds one employee to a structured set of purpose,
// authority, hard constraints, and self-modification rules.
//
// The struct serialises to JSON for storage inside the existing
// bot_definitions.data column alongside the bot.BotDefinition fields.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md
// lines 126-194 for the canonical schema.
type Constitution struct {
	// Identity
	Purpose string `json:"purpose"` // 1-sentence "why this employee exists"
	Role    string `json:"role"`    // e.g. "CI Reliability Engineer"
	Charter string `json:"charter"` // free-form markdown for nuance/values/tone

	// Autonomy
	AutonomyTier AutonomyTier `json:"autonomy_tier"` // tier_1_reactive | tier_2_propose | tier_3_autonomous

	// Authority: who this employee escalates to. Entries are agent IDs
	// or role sentinels ("role:user", "role:oncall", or legacy bare
	// names like "user" which are auto-normalised at load time). Empty
	// means no escalation path; tier 2 employees with empty EscalatesTo
	// will log a warning at load time because plans cannot be approved.
	EscalatesTo []string `json:"escalates_to"`

	// Hard constraints (machine-enforced)
	Constraints ConstitutionalConstraints `json:"constraints"`

	// Self-modification
	AmendmentPolicy AmendmentPolicy `json:"amendment_policy"`

	// Provenance
	Version    int       `json:"version"`     // bumped on each approved amendment
	AuthoredBy string    `json:"authored_by"` // "user" | agent ID that proposed
	ApprovedAt time.Time `json:"approved_at"`
}

// ConstitutionalConstraints is the machine-enforceable subset of the
// constitution. Anything that cannot be structurally enforced belongs
// in Constitution.Charter as free-form prose instead.
type ConstitutionalConstraints struct {
	// Tool gating. ToolsAllowed is an allowlist (empty = inherit
	// default toolset); ToolsForbidden is applied after the allowlist
	// and wins on conflict.
	ToolsAllowed   []string `json:"tools_allowed"`
	ToolsForbidden []string `json:"tools_forbidden"`

	// RiskCeiling is the hard upper bound on the security engine's
	// computed risk. One of RiskCeiling*; empty = no ceiling.
	RiskCeiling RiskLevelCeiling `json:"risk_ceiling"`

	// Resource envelope (accountability). Zero values are treated as
	// "no limit" by the enforcement engine.
	MaxTokensPerTurn      int `json:"max_tokens_per_turn"`
	MaxConversationTokens int `json:"max_conversation_tokens"`
	DailyBudgetCents      int `json:"daily_budget_cents"`
	MaxInvocationsPerDay  int `json:"max_invocations_per_day"`

	// EscalationTriggers lists the conditions under which this employee
	// MUST escalate (route to EscalatesTo via Plan signoff) instead of
	// executing directly.
	EscalationTriggers []EscalationTrigger `json:"escalation_triggers"`

	// Never is a list of hard "never do this" rules. The enforcement
	// engine pattern-matches these where possible (shell command scan,
	// path scan); the post-turn audit always scans for Never violations
	// in LLM output.
	Never []string `json:"never"`

	// AssessmentInterval is the cadence at which the GoalLoop ASSESS
	// step runs for tier 2+ employees. Accepts a Go duration ("15m",
	// "1h") or a 5-field cron expression. Empty = no scheduled assess
	// (tier 1 employees: trigger-driven only).
	AssessmentInterval string `json:"assessment_interval"`
}

// EscalationTrigger declares a condition that forces an escalation.
type EscalationTrigger struct {
	// On identifies the kind of condition.
	On EscalationOn `json:"on"` // risk_level | tool | action | cost
	// Match is the value to compare against. Semantics depend on On:
	//   risk_level: a risk band ("critical")
	//   tool:       a tool name ("shell_execute")
	//   action:     a logical action label ("file_delete")
	//   cost:       a cents threshold as a decimal string ("50")
	Match string `json:"match"`
	// Reason is a human/LLM-readable explanation of why this trigger
	// exists. Surfaced in the Plan signoff request so the approver has
	// context.
	Reason string `json:"reason"`
}

// AmendmentPolicy controls how (and whether) the constitution itself
// may be changed.
type AmendmentPolicy struct {
	// SelfProposeAllowed controls whether the employee may propose
	// amendments to its own constitution. Regardless of this flag,
	// amendments always require approval (RequiresApproval is true).
	SelfProposeAllowed bool `json:"self_propose_allowed"`
	// RequiresApproval is always true per design but is explicit so
	// constitutions cannot accidentally disable the approval gate.
	RequiresApproval bool `json:"requires_approval"`
	// FrozenFields lists JSON field paths that cannot be amended even
	// with approval. Each entry is either a top-level field name
	// ("purpose", "autonomy_tier") or a dotted path into Constraints
	// ("constraints.risk_ceiling", "constraints.never"). The validator
	// checks that each entry references a real field.
	FrozenFields []string `json:"frozen_fields"`
}

// Validate checks the constitution for self-consistency. It does NOT
// perform load-time-only checks (unknown agent IDs in EscalatesTo,
// unknown tool names) — those live in CheckEscalationReferences and
// CheckToolReferences so the manager can run them with access to the
// agent registry and tool registry respectively.
//
// The validations performed here are:
//   - Direct self-escalation (this ID appears in EscalatesTo).
//   - Tier value is known.
//   - RiskCeiling is one of the recognised bands (or empty).
//   - Each EscalationTrigger.On is a known kind.
//   - Each AmendmentPolicy.FrozenFields entry references a real field
//     on Constitution or ConstitutionalConstraints.
//   - RequiresApproval is true (design invariant).
//
// Transitive cycle detection across multiple constitutions requires the
// full employee set and lives in authority.go (DetectEscalationCycles).
// The selfSelf parameter is the employee's own ID; pass "" to skip the
// self-escalation check (e.g. when validating a constitution before its
// owning employee ID is known).
func (c *Constitution) Validate(selfID string) error {
	if c == nil {
		return errors.New("constitution is nil")
	}

	// Normalize legacy role sentinels ("user" → "role:user", etc.)
	// so the rest of the system only deals with canonical forms.
	c.EscalatesTo = NormalizeEscalatesTo(c.EscalatesTo)

	var errs []error

	// Autonomy tier sanity. Unknown tiers can sneak in via JSON with a
	// garbage string; UnmarshalJSON falls back to Tier1Reactive but
	// also returns an error, so this is mostly belt-and-braces.
	if c.AutonomyTier < Tier1Reactive || c.AutonomyTier > Tier3Autonomous {
		errs = append(errs, fmt.Errorf("autonomy_tier out of range: %d", int(c.AutonomyTier)))
	}

	// Risk ceiling must be a known band.
	if !c.Constraints.RiskCeiling.Valid() {
		errs = append(errs, fmt.Errorf("constraints.risk_ceiling: unknown value %q", c.Constraints.RiskCeiling))
	}

	// Escalation triggers must reference known condition kinds.
	for i, tr := range c.Constraints.EscalationTriggers {
		if !tr.On.Valid() {
			errs = append(errs, fmt.Errorf("constraints.escalation_triggers[%d].on: unknown value %q", i, tr.On))
		}
		if tr.Match == "" {
			errs = append(errs, fmt.Errorf("constraints.escalation_triggers[%d].match: required", i))
		}
	}

	// Direct self-escalation: the employee's own ID must not appear in
	// its EscalatesTo list. Transitive cycles (X→Y→X) require the full
	// graph and are handled by DetectEscalationCycles in authority.go.
	if selfID != "" {
		for _, id := range c.EscalatesTo {
			if id == selfID {
				errs = append(errs, fmt.Errorf("escalates_to: direct self-escalation to %q", id))
				break // one report is enough
			}
		}
	}

	// Amendment policy invariants.
	if !c.AmendmentPolicy.RequiresApproval {
		// RequiresApproval is always true per the spec. We reject the
		// constitution rather than silently flipping the flag: the
		// operator should know their file is wrong.
		errs = append(errs, errors.New("amendment_policy.requires_approval must be true (design invariant)"))
	}

	// Frozen fields must reference real JSON field paths.
	badFrozen := c.ValidateFrozenFields()
	for _, b := range badFrozen {
		errs = append(errs, fmt.Errorf("amendment_policy.frozen_fields: %s", b))
	}

	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return nil
}

// constitutionFieldNames is the set of valid top-level JSON field names
// on Constitution. Used by ValidateFrozenFields.
var constitutionFieldNames = map[string]struct{}{
	"purpose":           {},
	"role":              {},
	"charter":           {},
	"autonomy_tier":     {},
	"escalates_to":      {},
	"constraints":       {},
	"amendment_policy":  {},
	"version":           {},
	"authored_by":       {},
	"approved_at":       {},
}

// constraintsFieldNames is the set of valid JSON field names on
// ConstitutionalConstraints, used when a FrozenFields entry is dotted
// into the constraints sub-struct.
var constraintsFieldNames = map[string]struct{}{
	"tools_allowed":          {},
	"tools_forbidden":        {},
	"risk_ceiling":           {},
	"max_tokens_per_turn":    {},
	"max_conversation_tokens": {},
	"daily_budget_cents":     {},
	"max_invocations_per_day": {},
	"escalation_triggers":    {},
	"never":                  {},
	"assessment_interval":    {},
}

// ValidateFrozenFields returns a list of human-readable problems with
// AmendmentPolicy.FrozenFields. An empty return means all entries are
// valid. Each entry in FrozenFields must be either:
//   - a top-level Constitution field name (see constitutionFieldNames)
//   - "constraints.<subfield>" where <subfield> is in constraintsFieldNames
//
// Unknown entries are returned as errors. Entries are lower-cased
// before lookup so casing typos don't trip up operators, but the
// canonical form is snake_case.
func (c *Constitution) ValidateFrozenFields() []string {
	if c == nil {
		return nil
	}
	seen := make(map[string]struct{}, len(c.AmendmentPolicy.FrozenFields))
	var bad []string
	for _, raw := range c.AmendmentPolicy.FrozenFields {
		key := strings.ToLower(strings.TrimSpace(raw))
		if key == "" {
			bad = append(bad, "empty frozen field entry")
			continue
		}
		if _, dup := seen[key]; dup {
			continue // de-duplicate silently
		}
		seen[key] = struct{}{}

		if _, ok := constitutionFieldNames[key]; ok {
			continue
		}

		if strings.HasPrefix(key, "constraints.") {
			sub := strings.TrimPrefix(key, "constraints.")
			if _, ok := constraintsFieldNames[sub]; ok {
				continue
			}
		}

		bad = append(bad, fmt.Sprintf("unknown field %q (expected a top-level constitution field or constraints.<subfield>)", raw))
	}
	return bad
}

// CheckEscalationReferences validates that every ID in EscalatesTo
// resolves to a known agent (or is a role sentinel like "role:user").
// This is a load-time check: it needs the full agent registry, so it
// lives outside Validate() which must be runnable in isolation.
//
// Role sentinels (any "role:"-prefixed ID, or legacy bare names like
// "user", "system") are always accepted — they route to the human
// operator or an external system via the Plan signoff queue.
// UnknownIDs (if provided) is populated with the IDs that failed to
// resolve, so the caller can produce a helpful error. Returns nil on
// success.
func (c *Constitution) CheckEscalationReferences(knownAgentIDs map[string]struct{}) (unknownIDs []string, err error) {
	if c == nil {
		return nil, nil
	}
	for _, id := range c.EscalatesTo {
		if IsRoleSentinel(id) {
			continue
		}
		if _, ok := knownAgentIDs[id]; !ok {
			unknownIDs = append(unknownIDs, id)
		}
	}
	if len(unknownIDs) > 0 {
		// Sort for deterministic error messages.
		sort.Strings(unknownIDs)
		return unknownIDs, fmt.Errorf("escalates_to: unknown agent IDs: %s", strings.Join(unknownIDs, ", "))
	}
	return nil, nil
}

// CheckToolReferences validates that every tool name in
// Constraints.ToolsAllowed and ToolsForbidden resolves to a registered
// tool. This is a load-time check: unknown tools are not fatal (the
// spec says warn and strip), so this method returns the unknown names
// and a list of known names for the caller to log. It never returns an
// error for unknown tools — only for structural problems like a nil
// knownTools map passed by the caller.
//
// knownToolNames is treated as a set; pass nil to skip the check.
func (c *Constitution) CheckToolReferences(knownToolNames map[string]struct{}) (unknownAllowed, unknownForbidden []string) {
	if c == nil {
		return nil, nil
	}
	if knownToolNames == nil {
		return nil, nil
	}
	for _, name := range c.Constraints.ToolsAllowed {
		if _, ok := knownToolNames[name]; !ok {
			unknownAllowed = append(unknownAllowed, name)
		}
	}
	for _, name := range c.Constraints.ToolsForbidden {
		if _, ok := knownToolNames[name]; !ok {
			unknownForbidden = append(unknownForbidden, name)
		}
	}
	return unknownAllowed, unknownForbidden
}

// LogWarnings writes human-readable warnings for soft validation
// findings (unknown tools, tier 2 with empty EscalatesTo, etc.) to the
// given logger. Pass slog.Default() if you don't have one handy. Nil
// logger is a no-op (defence in depth).
func (c *Constitution) LogWarnings(logger *slog.Logger, employeeID string, knownToolNames map[string]struct{}) {
	if c == nil || logger == nil {
		return
	}
	if c.AutonomyTier >= Tier2Propose && len(c.EscalatesTo) == 0 {
		logger.Warn("employee constitution: tier >=2 with empty escalates_to; plans will stall in pending approval",
			"employee_id", employeeID)
	}
	unknownA, unknownF := c.CheckToolReferences(knownToolNames)
	if len(unknownA) > 0 {
		logger.Warn("employee constitution: tools_allowed references unknown tools; these will be stripped at enforcement time",
			"employee_id", employeeID, "unknown_tools", unknownA)
	}
	if len(unknownF) > 0 {
		logger.Warn("employee constitution: tools_forbidden references unknown tools; these will be stripped at enforcement time",
			"employee_id", employeeID, "unknown_tools", unknownF)
	}
}

// Ensure Constitution satisfies the validator interface used elsewhere
// (defensive: catches signature drift at compile time).
var _ interface{ Validate(string) error } = (*Constitution)(nil)
