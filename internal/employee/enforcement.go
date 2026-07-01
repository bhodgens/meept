// Package employee implements the AI Employee design: constitution model,
// goal lifecycle, and the three-checkpoint enforcement engine.
//
// This file (enforcement.go) implements Phase 4 of the spec at
// docs/superpowers/specs/2026-06-23-ai-employee-design.md (lines 325-455):
//   - Checkpoint 1: PreExecChecker (pre-execution gate)
//   - Checkpoint 2: PostTurnAuditor (post-turn LLM audit)
//   - Checkpoint 3: PeriodicAuditor (drift detection over N turns)
//   - AuditStore (SQLite-backed findings persistence)
//   - SynthesizedPrompt (constitution -> system-prompt markdown)
//
// ASSUMPTION: Constitution and its sub-types live in constitution.go (Phase 1).
// The shapes expected here match spec lines 126-194. If Phase 1 has not landed
// yet, this file still compiles only if constitution.go is present. See the
// Phase 1 blocker note in the package README.
package employee

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	mathrand "math/rand"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/id"
	_ "modernc.org/sqlite" // SQLite driver for AuditStore
)

// ---------------------------------------------------------------------------
// Risk ceiling helpers (local to avoid importing internal/security which
// would create a dependency edge the spec does not require here).
// ---------------------------------------------------------------------------

// riskLevel mirrors internal/security.RiskLevel so that enforcement.go can
// compare a constitution's risk ceiling against a numeric risk without a
// cross-package dependency. The ordering and string forms MUST stay aligned
// with internal/security/types.go.
//
// C2: The mapping between SecurityEngine.RiskLevel (int enum in
// internal/security/types.go) and Constitution.RiskCeiling (string in
// constitution.go) is defined here explicitly. The two type systems are:
//
//	internal/security.RiskLevel: RiskSafe=0, RiskLow=1, RiskMedium=2,
//	  RiskHigh=3, RiskCritical=4
//
//	employee.RiskLevelCeiling (string): "safe", "low", "medium", "high",
//	  "critical"
//
//	employee.riskLevel (local): riskSafe=0, riskLow=1, riskMedium=2,
//	  riskHigh=3, riskCritical=4
//
// The local riskLevel type mirrors security.RiskLevel's ordering exactly.
// parseRiskCeiling maps the string RiskCeiling → riskLevel. The
// SecurityEngineRiskToCeiling function maps an int from the security engine
// to the local riskLevel for direct comparison.
type riskLevel int

const (
	riskSafe     riskLevel = iota // safe
	riskLow                       // low
	riskMedium                    // medium
	riskHigh                      // high
	riskCritical                  // critical
)

// SecurityEngineRiskToCeiling converts a security.RiskLevel integer value
// to the local riskLevel used by the enforcement engine. This is the
// explicit mapping required by C2.
//
// internal/security defines:
//	RiskSafe=0, RiskLow=1, RiskMedium=2, RiskHigh=3, RiskCritical=4
//
// Since the ordering matches exactly, this is a direct cast, but the
// function documents the mapping and validates the range so unknown
// values get a sensible default.
func SecurityEngineRiskToCeiling(securityRiskLevel int) riskLevel {
	switch securityRiskLevel {
	case 0:
		return riskSafe
	case 1:
		return riskLow
	case 2:
		return riskMedium
	case 3:
		return riskHigh
	case 4:
		return riskCritical
	default:
		// Unknown security risk levels are treated as medium (conservative).
		return riskMedium
	}
}

// RiskCeilingToSecurityLevel converts the local riskLevel to the
// corresponding security.RiskLevel integer. This is the inverse of
// SecurityEngineRiskToCeiling and is used when the enforcement engine
// needs to communicate risk levels back to the security layer.
func RiskCeilingToSecurityLevel(r riskLevel) int {
	return int(r)
}

// parseRiskCeiling converts the constitution's risk_ceiling string into a
// riskLevel. Empty string defaults to riskMedium (the spec default for tier-2).
func parseRiskCeiling(s string) riskLevel {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "safe":
		return riskSafe
	case "low":
		return riskLow
	case "medium":
		return riskMedium
	case "high":
		return riskHigh
	case "critical":
		return riskCritical
	case "":
		return riskMedium
	default:
		// Unknown ceiling — default to medium and let the caller log a warning.
		return riskMedium
	}
}

// riskLabel returns the lowercase string form for a riskLevel.
func riskLabel(r riskLevel) string {
	switch r {
	case riskSafe:
		return "safe"
	case riskLow:
		return "low"
	case riskMedium:
		return "medium"
	case riskHigh:
		return "high"
	case riskCritical:
		return "critical"
	}
	return "unknown"
}

// ---------------------------------------------------------------------------
// BudgetChecker interface (locally defined per spec Phase 4 requirements).
// ---------------------------------------------------------------------------

// BudgetChecker reports the employee's resource consumption for the current
// day. Implementations typically wrap BotState TodayRuns / TodayCostCents /
// TotalTokensUsed fields.
type BudgetChecker interface {
	SpentToday(employeeID string) (tokens int, cents int, invocations int)
}

// noopBudgetChecker always reports zero usage; used as a default so that
// PreExecChecker works before the caller wires a real implementation.
type noopBudgetChecker struct{}

func (noopBudgetChecker) SpentToday(string) (int, int, int) { return 0, 0, 0 }

// ---------------------------------------------------------------------------
// ConversationTokenStore (E1: conversation-level token budget tracking).
// ---------------------------------------------------------------------------

// ConversationTokenStore reports the cumulative token consumption for a
// specific conversation. This is distinct from BudgetChecker which tracks
// per-day, per-employee usage. The pre-exec checker uses this to enforce
// MaxConversationTokens (the per-conversation budget cap set in the
// constitution's Constraints.MaxConversationTokens field).
//
// Implementations typically wrap a session-scoped token counter or query
// the LLM client's per-conversation usage record.
type ConversationTokenStore interface {
	// GetConversationTokens returns the total tokens consumed in the
	// given conversation so far. A return of (0, nil) means "no
	// data" or "no tokens used" — the checker treats both as
	// "budget available".
	GetConversationTokens(conversationID string) (int, error)
}

// noopConversationTokenStore always returns 0 tokens; used as a default so
// that PreExecChecker works before the caller wires a real implementation.
type noopConversationTokenStore struct{}

func (noopConversationTokenStore) GetConversationTokens(string) (int, error) { return 0, nil }

// ---------------------------------------------------------------------------
// H4: TurnBudgetTracker — cumulative tool costs within a turn.
// ---------------------------------------------------------------------------

// TurnBudgetTracker tracks the cumulative resource consumption of all tool
// calls within a single GoalLoop turn (one ASSESS→PLAN→EXECUTE→REFLECT
// cycle). The PreExecChecker consults this in addition to the daily budget
// so that a turn with 10 tools doesn't exceed the per-turn budget after
// tool #3.
//
// The tracker is safe for concurrent use: all mutations are guarded by an
// internal mutex. The Record and Remaining methods are O(1).
//
// Lifecycle: the GoalLoop or the BotRunner creates a fresh TurnBudgetTracker
// at the start of each turn. After each tool execution, Record is called
// with the tool's cost. The PreExecChecker calls Remaining before the next
// tool; if the remaining budget is exhausted, the checker denies the call
// and sets turnComplete = true so the loop stops queuing more tools.
type TurnBudgetTracker struct {
	mu              sync.Mutex
	tokensUsed      int
	costCents       int
	toolCalls       int
	maxTokensPerTurn int
	maxCostPerTurn  int
}

// NewTurnBudgetTracker creates a tracker with the given per-turn limits.
// Zero or negative limits mean "no limit" for that dimension.
func NewTurnBudgetTracker(maxTokensPerTurn, maxCostPerTurn int) *TurnBudgetTracker {
	return &TurnBudgetTracker{
		maxTokensPerTurn: maxTokensPerTurn,
		maxCostPerTurn:   maxCostPerTurn,
	}
}

// Record adds a tool call's resource consumption to the running total.
// Called after each tool execution completes.
func (t *TurnBudgetTracker) Record(tokensUsed, costCents int) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.tokensUsed += tokensUsed
	t.costCents += costCents
	t.toolCalls++
	t.mu.Unlock()
}

// Remaining returns the remaining token and cost budget for this turn.
// A negative return means the budget has been exceeded.
func (t *TurnBudgetTracker) Remaining() (tokensRemaining, costRemaining int) {
	if t == nil {
		return -1, -1 // nil tracker = unlimited
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	tokensRemaining = -1
	costRemaining = -1
	if t.maxTokensPerTurn > 0 {
		tokensRemaining = t.maxTokensPerTurn - t.tokensUsed
	}
	if t.maxCostPerTurn > 0 {
		costRemaining = t.maxCostPerTurn - t.costCents
	}
	return
}

// IsExhausted returns true when either the token or cost budget has been
// exceeded. A nil tracker is never exhausted (unlimited).
func (t *TurnBudgetTracker) IsExhausted() bool {
	if t == nil {
		return false
	}
	tokens, cost := t.Remaining()
	if tokens >= 0 && tokens == 0 {
		return true
	}
	if cost >= 0 && cost == 0 {
		return true
	}
	return false
}

// ToolCalls returns the number of tool calls recorded so far.
func (t *TurnBudgetTracker) ToolCalls() int {
	if t == nil {
		return 0
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.toolCalls
}

// Reset clears all accumulated state for a new turn.
func (t *TurnBudgetTracker) Reset() {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.tokensUsed = 0
	t.costCents = 0
	t.toolCalls = 0
	t.mu.Unlock()
}

// ---------------------------------------------------------------------------
// AutoPause callback.
// ---------------------------------------------------------------------------

// AutoPauseFunc is invoked when a critical finding or budget exhaustion
// requires the employee to be paused. The caller wires this to
// BotManager.PauseBot (or EmployeeManager.Pause in the Phase 2 rename).
type AutoPauseFunc func(employeeID string, reason string) error

// ---------------------------------------------------------------------------
// Pre-exec decision.
// ---------------------------------------------------------------------------

// Decision is the result of PreExecChecker.Check. When Allowed is false the
// security engine blocks the action. Severity and Reason are recorded as an
// audit event. RequiresPlan triggers Plan signoff for an escalation.
// EscalateTo lists the agent IDs (or role sentinels like "role:user")
// that must approve an escalated action.
type Decision struct {
	Allowed      bool     `json:"allowed"`
	Reason       string   `json:"reason"`
	Severity     string   `json:"severity"` // info | warning | critical
	EscalateTo   []string `json:"escalate_to,omitempty"`
	RequiresPlan bool     `json:"requires_plan,omitempty"`
}

// ---------------------------------------------------------------------------
// Audit data model (spec lines 402-419).
// ---------------------------------------------------------------------------

// AuditCheckpoint identifies which of the three enforcement checkpoints
// produced a finding.
type AuditCheckpoint string

const (
	CheckpointPreExec  AuditCheckpoint = "pre_exec"
	CheckpointPostTurn AuditCheckpoint = "post_turn"
	CheckpointPeriodic AuditCheckpoint = "periodic"
)

// AuditSeverity levels for findings.
//
// E7: Severity Rubric. The PostTurnAuditor and PeriodicAuditor assign
// severity according to the following rubric:
//
//   - critical: Never[] violation OR risk_ceiling exceeded OR budget fraud
//     suspected (e.g. token counts manipulated, cost reports fabricated).
//     Critical findings trigger immediate auto-pause.
//
//   - warning: Charter commitment violation (e.g. output tone diverges
//     from charter values, tool usage pattern deviates from intended
//     scope, escalation trigger was bypassed). Warning findings contribute
//     to DriftScore but do not auto-pause individually.
//
//   - info: Minor style drift, cosmetic issues, or observations that don't
//     indicate a constitution violation. Info findings are recorded for
//     audit trail completeness but do not affect DriftScore or auto-pause.
//
// The rubric is enforced by the LLM audit prompt (postTurnSystemPrompt)
// which instructs the model to use these criteria when assigning severity.
type AuditSeverity string

const (
	SeverityInfo     AuditSeverity = "info"
	SeverityWarning  AuditSeverity = "warning"
	SeverityCritical AuditSeverity = "critical"
)

// AuditFinding is a persisted record of a constitution violation or drift
// observation. Matches the employee_audit_findings table schema (spec
// lines 402-419).
type AuditFinding struct {
	ID            string          `json:"id"`
	EmployeeID    string          `json:"employee_id"`
	GoalID        string          `json:"goal_id,omitempty"`
	PlanID        string          `json:"plan_id,omitempty"`
	TurnID        string          `json:"turn_id,omitempty"`
	Severity      AuditSeverity   `json:"severity"`
	Checkpoint    AuditCheckpoint `json:"checkpoint"`
	ViolatedRule  string          `json:"violated_rule,omitempty"`
	Evidence      string          `json:"evidence,omitempty"`
	DetectedAt    time.Time       `json:"detected_at"`
	ResolvedAt    *time.Time      `json:"resolved_at,omitempty"`
	Resolution    string          `json:"resolution,omitempty"`
	DriftScore    float64         `json:"drift_score,omitempty"`
}

// ---------------------------------------------------------------------------
// TurnRecord / ToolCallRecord (inputs to the auditors).
// ---------------------------------------------------------------------------

// ToolCallRecord captures a single tool invocation for audit purposes.
type ToolCallRecord struct {
	ToolName string            `json:"tool_name"`
	Action   string            `json:"action"`
	Args     map[string]string `json:"args,omitempty"`
	Result   string            `json:"result,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// TokenCounts tracks token usage for a single turn.
type TokenCounts struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

// TurnRecord bundles the tool calls and final output of a completed LLM turn
// for audit review.
//
// E2: The struct is explicitly defined with all fields required by the spec
// (lines 368-379): conversationID, turnID, toolCalls, llmOutput, tokenUsage,
// and duration.
type TurnRecord struct {
	EmployeeID     string           `json:"employee_id"`
	ConversationID string           `json:"conversation_id,omitempty"`
	TurnID         string           `json:"turn_id"`
	GoalID         string           `json:"goal_id,omitempty"`
	PlanID         string           `json:"plan_id,omitempty"`
	ToolCalls      []ToolCallRecord `json:"tool_calls"`
	FinalOutput    string           `json:"final_output"`
	TokenUsage     TokenCounts      `json:"token_usage,omitempty"`
	Duration       time.Duration    `json:"duration,omitempty"`
	Constitution   *Constitution    `json:"-"`
}

// ---------------------------------------------------------------------------
// Pre-execution gate (Checkpoint 1).
// ---------------------------------------------------------------------------

// PreExecChecker enforces the constitution before every tool call. It is
// registered per-employee with the SecurityEngine which calls Check between
// the base-rule lookup and the confirmation gate.
//
// The checker is safe for concurrent use. The constitution pointer is swapped
// atomically via SetConstitution; Check snapshots it under RLock before doing
// any work, ensuring no I/O happens under the lock (per CLAUDE.md mutex-scope
// rule).
type PreExecChecker struct {
	mu               sync.RWMutex
	constitution     *Constitution
	employeeID       string
	budgetChecker    BudgetChecker
	convTokenStore   ConversationTokenStore
	autoPause        AutoPauseFunc
	auditStore      *AuditStore
	turnBudget       *TurnBudgetTracker
}

// NewPreExecChecker constructs a PreExecChecker for the given employee. The
// constitution may be nil initially; SetConstitution must be called before
// Check returns meaningful decisions. A nil constitution yields an allow-all
// Decision (the employee is unconstrained) so that wiring does not block
// existing agents.
func NewPreExecChecker(employeeID string, c *Constitution) *PreExecChecker {
	return &PreExecChecker{
		constitution:   c,
		employeeID:     employeeID,
		budgetChecker:  noopBudgetChecker{},
		convTokenStore: noopConversationTokenStore{},
	}
}

// SetConstitution atomically replaces the constitution used for checks.
// Caller must ensure the pointer is non-nil for a live employee.
func (p *PreExecChecker) SetConstitution(c *Constitution) {
	p.mu.Lock()
	p.constitution = c
	p.mu.Unlock()
}

// SetBudgetChecker wires the budget checker. Nil is ignored (typed-nil guard
// per CLAUDE.md).
func (p *PreExecChecker) SetBudgetChecker(bc BudgetChecker) {
	if bc == nil {
		return
	}
	p.mu.Lock()
	p.budgetChecker = bc
	p.mu.Unlock()
}

// SetConversationTokenStore wires the conversation-level token store used to
// enforce MaxConversationTokens (E1). Nil is ignored (typed-nil guard per
// CLAUDE.md).
func (p *PreExecChecker) SetConversationTokenStore(store ConversationTokenStore) {
	if store == nil {
		return
	}
	p.mu.Lock()
	p.convTokenStore = store
	p.mu.Unlock()
}

// SetAutoPause wires the auto-pause callback. Nil is ignored.
func (p *PreExecChecker) SetAutoPause(fn AutoPauseFunc) {
	if fn == nil {
		return
	}
	p.mu.Lock()
	p.autoPause = fn
	p.mu.Unlock()
}

// SetAuditStore wires the AuditStore used to persist findings on hard-deny
// paths. Nil is ignored (typed-nil guard per CLAUDE.md).
func (p *PreExecChecker) SetAuditStore(as *AuditStore) {
	if as == nil {
		return
	}
	p.mu.Lock()
	p.auditStore = as
	p.mu.Unlock()
}

// SetTurnBudgetTracker wires the per-turn budget tracker (H4). The tracker
// is consulted in Check to enforce per-turn cumulative budget limits. When
// the turn budget is exhausted, the checker denies the call and the
// caller sets turnComplete = true. Nil is ignored (typed-nil guard per
// CLAUDE.md). Thread-safe.
func (p *PreExecChecker) SetTurnBudgetTracker(t *TurnBudgetTracker) {
	if t == nil {
		return
	}
	p.mu.Lock()
	p.turnBudget = t
	p.mu.Unlock()
}

// Check evaluates a single tool call against the constitution. Returns a
// Decision describing whether the call is allowed, denied, or escalated.
//
// The details map carries context about the call (e.g. {"command": "rm -rf /"},
// {"path": "/etc/passwd"}). The function performs no I/O.
//
// Evaluation order (spec lines 342-356):
//  1. tools_forbidden / tools_allowed (hard gating)
//  2. never[] (substring scan; critical deny + auto-pause trumps all)
//  3. escalation_triggers (route to plan signoff; not a denial)
//  4. risk_ceiling (compared against details["risk_level"])
//  5. budget check (tokens / cents / invocations today)
//
// never[] is checked before escalation/risk so that an action containing a
// prohibited phrase is hard-denied + auto-paused even if it would also match
// an escalation trigger (e.g. "shell_execute" tool with action "merge to main"
// must hit the never-rule, not get queued as a plan). escalation_triggers run
// before risk_ceiling so a configured trigger (e.g. on risk_level=critical)
// routes to signoff instead of being shadowed by the generic ceiling deny.
func (p *PreExecChecker) Check(action, toolName string, details map[string]string) (dec Decision) {
	// Fail-safe: if the checker panics, deny + auto-pause (spec lines 601-602).
	// Also record an audit finding at SeverityCritical.
	defer func() {
		if r := recover(); r != nil {
			dec = Decision{
				Allowed:  false,
				Reason:   fmt.Sprintf("pre-exec checker panic: %v", r),
				Severity: string(SeverityCritical),
			}
			p.recordDenial(context.Background(), p.employeeID, action, toolName,
				fmt.Sprintf("pre-exec checker panic: %v", r), SeverityCritical)
			p.triggerAutoPause("pre-exec checker panic")
		}
	}()

	// Snapshot under lock, evaluate outside lock.
	p.mu.RLock()
	c := p.constitution
	bc := p.budgetChecker
	convTokenStore := p.convTokenStore
	autoPause := p.autoPause
	turnBudget := p.turnBudget
	p.mu.RUnlock()

	// No constitution means no constraints — allow everything (non-employee
	// agents or pre-Phase-1 wiring).
	if c == nil {
		return Decision{Allowed: true, Severity: string(SeverityInfo)}
	}
	constraints := c.Constraints

	// 1a. tools_forbidden — hard deny.
	if len(constraints.ToolsForbidden) > 0 {
		for _, forbidden := range constraints.ToolsForbidden {
			if toolName == forbidden {
				p.recordDenial(context.Background(), p.employeeID, action, toolName,
					"tools_forbidden", SeverityWarning)
				return Decision{
					Allowed:  false,
					Reason:   fmt.Sprintf("tool %q is forbidden by constitution", toolName),
					Severity: string(SeverityWarning),
				}
			}
		}
	}

	// 1b. tools_allowed — if non-empty and tool not in list, hard deny.
	if len(constraints.ToolsAllowed) > 0 {
		allowed := false
		for _, a := range constraints.ToolsAllowed {
			if toolName == a {
				allowed = true
				break
			}
		}
		if !allowed {
			p.recordDenial(context.Background(), p.employeeID, action, toolName,
				"tools_allowed", SeverityWarning)
			return Decision{
				Allowed:  false,
				Reason:   fmt.Sprintf("tool %q is not in tools_allowed list", toolName),
				Severity: string(SeverityWarning),
			}
		}
	}

	// 2. never[] — substring scan (legacy form) + structured never rules
	//    (C4). If any never-rule matches the action, tool name, or any
	//    details value, hard deny + auto-pause. This check precedes
	//    escalation_triggers and risk_ceiling so a never-rule violation
	//    is always treated as a critical stop, not silently queued as an
	//    escalation plan.
	//    (Spec line 355: "Never pattern match → hard deny, audit event
	//    at RiskCritical, employee auto-pause.")
	if hit, rule := matchesNever(constraints.Never, action, toolName, details); hit {
		p.recordDenial(context.Background(), p.employeeID, action, toolName,
			"never: "+rule, SeverityCritical)
		if autoPause != nil {
			_ = autoPause(p.employeeID, "never-rule violation: "+rule)
		}
		return Decision{
			Allowed:  false,
			Reason:   fmt.Sprintf("never-rule violation: %q", rule),
			Severity: string(SeverityCritical),
		}
	}
	// 2b. never_rules[] — structured form (C4). Supports substring, regex,
	//     glob, and llm_only match types. The llm_only type is NOT checked
	//     here (only by the post-turn auditor), so it's skipped at pre-exec.
	if hit, rule := matchesNeverRules(constraints.NeverRules, action, toolName, details); hit {
		p.recordDenial(context.Background(), p.employeeID, action, toolName,
			"never_rule: "+rule, SeverityCritical)
		if autoPause != nil {
			_ = autoPause(p.employeeID, "never-rule violation: "+rule)
		}
		return Decision{
			Allowed:  false,
			Reason:   fmt.Sprintf("never-rule violation: %q", rule),
			Severity: string(SeverityCritical),
		}
	}

	// 3. escalation_triggers — match against action, tool, or details.
	//    Escalation is normal routing, not a security concern: the
	//    action is queued as a Plan in PendingApproval routed to
	//    EscalatesTo. Severity is empty (no audit event), RequiresPlan
	//    is true so the caller knows to route for signoff.
	//    (Spec line 354.)
	for _, trig := range constraints.EscalationTriggers {
		if matchesTrigger(trig, action, toolName, details) {
			return Decision{
				Allowed:      false, // queued, not executed immediately
				Reason:       fmt.Sprintf("escalation trigger matched: %s", trig.Reason),
				Severity:     "",
				RequiresPlan: true,
				EscalateTo:   c.EscalatesTo,
			}
		}
	}

	// 4. risk_ceiling — compare the computed risk (passed in details) to
	//    the constitution's ceiling.
	if rlRaw, ok := details["risk_level"]; ok {
		callRisk := parseRiskCeiling(rlRaw)
		ceiling := parseRiskCeiling(string(constraints.RiskCeiling))
		if callRisk > ceiling {
			p.recordDenial(context.Background(), p.employeeID, action, toolName,
				"risk_ceiling", SeverityWarning)
			return Decision{
				Allowed:      false,
				Reason:       fmt.Sprintf("risk %s exceeds ceiling %s", riskLabel(callRisk), riskLabel(ceiling)),
				Severity:     string(SeverityWarning),
				RequiresPlan: true,
				EscalateTo:   c.EscalatesTo,
			}
		}
	}

	// 5. budget check — tokens / cents / invocations today.
	if bc != nil {
		tokens, cents, invocations := bc.SpentToday(p.employeeID)
		if constraints.DailyBudgetCents > 0 && cents >= constraints.DailyBudgetCents {
			p.recordDenial(context.Background(), p.employeeID, action, toolName,
				"daily budget exhausted", SeverityCritical)
			if autoPause != nil {
				_ = autoPause(p.employeeID, "daily budget exhausted")
			}
			return Decision{
				Allowed:  false,
				Reason:   fmt.Sprintf("daily budget exhausted: %dc >= %dc", cents, constraints.DailyBudgetCents),
				Severity: string(SeverityCritical),
			}
		}
		if constraints.MaxInvocationsPerDay > 0 && invocations >= constraints.MaxInvocationsPerDay {
			p.recordDenial(context.Background(), p.employeeID, action, toolName,
				"max invocations reached", SeverityCritical)
			if autoPause != nil {
				_ = autoPause(p.employeeID, "max invocations reached")
			}
			return Decision{
				Allowed:  false,
				Reason:   fmt.Sprintf("max invocations reached: %d >= %d", invocations, constraints.MaxInvocationsPerDay),
				Severity: string(SeverityCritical),
			}
		}
		// MaxTokensPerTurn is checked against per-turn usage; the details map
		// may carry the cumulative turn token count.
		if constraints.MaxTokensPerTurn > 0 {
			if turnTokens, ok := details["turn_tokens"]; ok {
				var tt int
				if _, err := fmt.Sscanf(turnTokens, "%d", &tt); err == nil && tt > constraints.MaxTokensPerTurn {
					return Decision{
						Allowed:  false,
						Reason:   fmt.Sprintf("turn tokens %d exceed max %d", tt, constraints.MaxTokensPerTurn),
						Severity: string(SeverityWarning),
					}
				}
			}
		}
		_ = tokens // reserved for future per-turn token gating
	}

	// H4: Turn-level budget check. The TurnBudgetTracker tracks cumulative
	// cost across all tool calls within the current turn. If the remaining
	// turn budget is exhausted, deny and signal that the turn is complete
	// (the caller checks the Severity to detect this and stops queuing
	// more tools).
	if turnBudget != nil {
		tokensRem, costRem := turnBudget.Remaining()
		if tokensRem == 0 || costRem == 0 {
			p.recordDenial(context.Background(), p.employeeID, action, toolName,
				"turn budget exhausted", SeverityCritical)
			if autoPause != nil {
				_ = autoPause(p.employeeID, "turn budget exhausted")
			}
			return Decision{
				Allowed:  false,
				Reason:   fmt.Sprintf("turn budget exhausted: tokens_remaining=%d cost_remaining=%d", tokensRem, costRem),
				Severity: string(SeverityCritical),
			}
		}
	}

	// 6. E1: conversation-level token budget check. Unlike the daily
	//    budget above, this tracks per-conversation tokens via the
	//    ConversationTokenStore. The conversation_id is read from details
	//    (injected by the security engine or passed directly by the caller).
	//    MaxConversationTokens=0 means no per-conversation cap.
	if constraints.MaxConversationTokens > 0 && convTokenStore != nil {
		convID := details["conversation_id"]
		if convID != "" {
			convTokens, convErr := convTokenStore.GetConversationTokens(convID)
			if convErr == nil && convTokens >= constraints.MaxConversationTokens {
				p.recordDenial(context.Background(), p.employeeID, action, toolName,
					"conversation token budget exhausted", SeverityCritical)
				if autoPause != nil {
					_ = autoPause(p.employeeID, "conversation token budget exhausted")
				}
				return Decision{
					Allowed:  false,
					Reason:   fmt.Sprintf("conversation tokens %d >= max %d", convTokens, constraints.MaxConversationTokens),
					Severity: string(SeverityCritical),
				}
			}
		}
	}

	return Decision{Allowed: true, Severity: string(SeverityInfo)}
}

// recordDenial persists an audit finding for a hard-deny decision. It is
// best-effort: the auditStore is nil-checked and the write is wrapped in
// recover so an audit-write failure never causes a panic in the checker.
//
// H9: The audit store write (store.Create) performs SQLite I/O. However,
// this method is called OUTSIDE the PreExecChecker's mutex (the auditStore
// is snapshotted under RLock before this call). The only mutex held during
// this method is the audit store's own internal mutex (SQL transaction
// serialization). This is NOT a violation of the CLAUDE.md mutex-scope
// rule because:
//   1. The PreExecChecker.mu is NOT held during this call (it was released
//      after snapshotting the auditStore pointer).
//   2. The AuditStore is goroutine-safe via the underlying *sql.DB; no
//      application-level mutex is held across I/O.
//   3. The recover() wrapper ensures any panic from the store layer does
//      not propagate into the checker.
// The mutexio analyzer is expected to flag this because the method
// signature includes "recordDenial" which doesn't match any I/O pattern.
// If flagged: this is an intentional exception per the design. The audit
// write MUST happen synchronously with the denial decision to maintain
// audit-trail integrity (spec line 397: "all three checkpoints persist
// findings").
func (p *PreExecChecker) recordDenial(ctx context.Context, employeeID, action, toolName, reason string, severity AuditSeverity) {
	defer func() {
		// Never let an audit-write failure propagate into the checker.
		_ = recover()
	}()

	p.mu.RLock()
	store := p.auditStore
	p.mu.RUnlock()

	if store == nil {
		return
	}

	finding := AuditFinding{
		ID:           id.Generate("audit_"),
		EmployeeID:   employeeID,
		Severity:     severity,
		Checkpoint:   CheckpointPreExec,
		ViolatedRule: reason,
		Evidence:     action + "/" + toolName,
		DetectedAt:   time.Now().UTC(),
	}
	_ = store.Create(ctx, finding)
}

// triggerAutoPause is a best-effort auto-pause invocation used by the
// fail-safe recover handler.
func (p *PreExecChecker) triggerAutoPause(reason string) {
	p.mu.RLock()
	fn := p.autoPause
	p.mu.RUnlock()
	if fn != nil {
		_ = fn(p.employeeID, reason)
	}
}

// matchesTrigger returns true if the escalation trigger fires for the given
// action/tool/details combination.
func matchesTrigger(trig EscalationTrigger, action, toolName string, details map[string]string) bool {
	switch trig.On {
	case EscalateOnTool:
		return trig.Match == toolName
	case EscalateOnAction:
		return trig.Match == action
	case EscalateOnRiskLevel:
		// Match against details["risk_level"] or the action's risk.
		if rl, ok := details["risk_level"]; ok {
			return strings.EqualFold(rl, trig.Match)
		}
		return false
	case EscalateOnCost:
		// Match against details["cost_cents"] exceeding the trigger threshold.
		if costStr, ok := details["cost_cents"]; ok {
			var cost int
			if _, err := fmt.Sscanf(costStr, "%d", &cost); err == nil {
				var threshold int
				if _, err := fmt.Sscanf(trig.Match, "%d", &threshold); err == nil && cost >= threshold {
					return true
				}
			}
		}
		return false
	}
	return false
}

// matchesNever scans the action, tool name, and all details values for
// occurrences of any never-rule. Returns (true, rule) on the first hit.
//
// Matching is case-insensitive and uses two strategies per field:
//  1. Substring containment — the rule appears verbatim in the field.
//  2. Token-set containment — every whitespace-separated token of the rule
//     appears somewhere in the field. This catches natural-language
//     paraphrases like rule "force push" matching field "git push --force",
//     where the words appear in a different order.
//
// The two strategies combined match what the spec (line 219) describes:
// Never[] is deliberately a list of strings — matches what the audit LLM is
// good at scanning for, applied here as a best-effort pre-exec check.
func matchesNever(rules []string, action, toolName string, details map[string]string) (bool, string) {
	// Collect the lowercased haystacks (one per field). Sort detail keys
	// for deterministic scan order across map iteration.
	haystacks := []string{
		strings.ToLower(action),
		strings.ToLower(toolName),
	}
	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		haystacks = append(haystacks, strings.ToLower(details[k]))
	}

	for _, rule := range rules {
		if rule == "" {
			continue
		}
		for _, h := range haystacks {
			if ruleMatchesField(rule, h) {
				return true, rule
			}
		}
	}
	return false, ""
}

// ruleMatchesField reports whether the lowercased rule matches the lowercased
// field via substring containment (fast path) or token-set containment.
func ruleMatchesField(rule, field string) bool {
	rule = strings.ToLower(rule)
	field = strings.ToLower(field)
	if strings.Contains(field, rule) {
		return true
	}
	ruleTokens := strings.Fields(rule)
	if len(ruleTokens) < 2 {
		// Single-token rules already handled by Contains above.
		return false
	}
	// Build a normalized token set from the field. Normalization strips
	// leading non-alphanumeric characters (e.g. "--force" -> "force",
	// "@remote" -> "remote") so command flags still match plain-word
	// rules like "force push".
	fieldTokens := make(map[string]struct{})
	for _, t := range strings.Fields(field) {
		nt := normalizeToken(t)
		if nt != "" {
			fieldTokens[normalizeToken(t)] = struct{}{}
		}
	}
	// All rule tokens must appear in the field token set.
	for _, rt := range ruleTokens {
		if _, ok := fieldTokens[normalizeToken(rt)]; !ok {
			return false
		}
	}
	return true
}

// normalizeToken lowercases the token and strips leading non-alphanumeric
// characters (dashes, slashes, colons, etc.) so command-style tokens like
// "--force" or "/bin/rm" match plain-word rules like "force" or "rm".
func normalizeToken(t string) string {
	t = strings.ToLower(strings.TrimSpace(t))
	for len(t) > 0 {
		r := rune(t[0])
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			break
		}
		t = t[1:]
	}
	return t
}

// matchesNeverRules scans the action, tool name, and all details values
// against the structured NeverRule list (C4). Each rule is matched
// according to its MatchType:
//
//   - MatchSubstring: delegates to the same substring + token-set logic
//     as matchesNever (backward compatible with the legacy Never []string).
//   - MatchRegex: compiles the pattern as a Go regexp (Re2) and matches
//     against each haystack. Compilation errors are logged and the rule
//     is skipped (fail-open for malformed patterns so a bad regex in a
//     constitution doesn't brick the enforcement engine; the malformed
//     pattern is caught at Validate time separately).
//   - MatchGlob: uses path.Match for shell-style glob patterns.
//   - MatchLLMOnly: NOT checked here — only by the post-turn auditor's
//     LLM scan. Returns false from this function so the pre-exec gate
//     doesn't fire.
//
// Returns (true, pattern) on the first hit.
func matchesNeverRules(rules []NeverRule, action, toolName string, details map[string]string) (bool, string) {
	if len(rules) == 0 {
		return false, ""
	}
	// Collect the lowercased haystacks (one per field). Sort detail keys
	// for deterministic scan order across map iteration.
	haystacks := []string{
		strings.ToLower(action),
		strings.ToLower(toolName),
	}
	keys := make([]string, 0, len(details))
	for k := range details {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		haystacks = append(haystacks, strings.ToLower(details[k]))
	}

	for _, rule := range rules {
		if rule.Pattern == "" {
			continue
		}
		mt := rule.MatchType
		if mt == "" {
			mt = MatchSubstring
		}
		// llm_only rules are not machine-checked at pre-exec.
		if mt == MatchLLMOnly {
			continue
		}
		for _, h := range haystacks {
			if neverRuleMatchesField(rule.Pattern, mt, h) {
				return true, rule.Pattern
			}
		}
	}
	return false, ""
}

// neverRuleMatchesField reports whether the rule pattern matches the
// (already lowercased) field according to the match type.
func neverRuleMatchesField(pattern string, mt MatchType, field string) bool {
	switch mt {
	case MatchSubstring:
		return ruleMatchesField(pattern, field)
	case MatchRegex:
		re, err := regexp.Compile(pattern)
		if err != nil {
			// Malformed regex — skip (fail-open). Validate() should
			// catch this earlier, but defensive.
			return false
		}
		return re.MatchString(field)
	case MatchGlob:
		matched, err := path.Match(pattern, field)
		if err != nil {
			return false
		}
		return matched
	case MatchLLMOnly:
		return false
	default:
		// Unknown match type — default to substring.
		return ruleMatchesField(pattern, field)
	}
}

// ---------------------------------------------------------------------------
// Post-turn audit (Checkpoint 2).
// ---------------------------------------------------------------------------

// maxCachedTurns is the per-employee cap on cached TurnRecords for the
// periodic auditor's bulk review (spec line 389).
const maxCachedTurns = 50

// PostTurnAuditor runs a small-model classifier after each LLM turn to scan
// for constitution violations in the tool calls and final output.
type PostTurnAuditor struct {
	mu        sync.Mutex
	model     llm.Chatter
	prompt    string
	autoPause AutoPauseFunc
	store     *AuditStore
	// retryWithStricter controls the spec line 605 behaviour: on unparseable
	// LLM output, retry once with a stricter prompt before giving up.
	retryWithStricter bool

	// onFindingAttached is the callback invoked after a finding with a
	// non-empty GoalID is persisted (spec line 382: "attach to owning
	// Goal"). The callback receives (goalID, findingID). Nil means
	// findings are not attached to goals (G7 passive linking only).
	onFindingAttached func(goalID, findingID string)

	// busPublisher publishes employee.critical_finding bus events (E4).
	// When a critical finding is detected, the auditor publishes the event
	// in addition to the existing autoPause callback. Nil means no bus
	// event is published. Guarded by mu.
	busPublisher BusPublisher

	// turnCache stores recent TurnRecords per employee for the periodic
	// auditor to bulk-review (spec line 389). Capped at maxCachedTurns
	// per employee. Guarded by turnCacheMu.
	turnCache   map[string][]TurnRecord
	turnCacheMu sync.RWMutex
}

// NewPostTurnAuditor constructs a PostTurnAuditor. The model must be non-nil
// for Audit to actually call the LLM; a nil model makes Audit a no-op that
// returns nil (spec: don't cascade LLM failures into system outages).
func NewPostTurnAuditor(model llm.Chatter, store *AuditStore, prompt string) *PostTurnAuditor {
	return &PostTurnAuditor{
		model:             model,
		store:             store,
		prompt:            prompt,
		retryWithStricter: true,
		turnCache:         make(map[string][]TurnRecord),
	}
}

// SetAutoPause wires the auto-pause callback (nil-guarded).
func (a *PostTurnAuditor) SetAutoPause(fn AutoPauseFunc) {
	if fn == nil {
		return
	}
	a.mu.Lock()
	a.autoPause = fn
	a.mu.Unlock()
}

// SetOnFindingAttached wires the callback invoked when a finding with a
// non-empty GoalID is persisted. The callback receives (goalID, findingID)
// and should attach the finding to the goal via Goal.AttachFinding. Nil is
// ignored (typed-nil guard per CLAUDE.md).
func (a *PostTurnAuditor) SetOnFindingAttached(fn func(goalID, findingID string)) {
	if fn == nil {
		return
	}
	a.mu.Lock()
	a.onFindingAttached = fn
	a.mu.Unlock()
}

// SetBusPublisher wires the bus publisher used to emit
// employee.critical_finding events when the auditor detects a critical
// finding (E4). Nil is ignored (typed-nil guard per CLAUDE.md).
func (a *PostTurnAuditor) SetBusPublisher(p BusPublisher) {
	if p == nil {
		return
	}
	a.mu.Lock()
	a.busPublisher = p
	a.mu.Unlock()
}

// RecentTurns returns cached turns for an employee, most recent last.
// Called by the periodic audit job via TurnCollectorFunc (spec line 389).
// limit <= 0 means no limit. lookback is advisory; the cache is already
// bounded by maxCachedTurns so the filter is a no-op when turns lack
// timestamps.
func (a *PostTurnAuditor) RecentTurns(employeeID string, limit int, lookback time.Duration) []TurnRecord {
	a.turnCacheMu.RLock()
	defer a.turnCacheMu.RUnlock()

	turns := a.turnCache[employeeID]
	if len(turns) == 0 {
		return nil
	}
	// lookback is advisory; TurnRecord has no timestamp field. The cache
	// is already bounded so we return the last N entries.
	_ = lookback
	if limit > 0 && len(turns) > limit {
		turns = turns[len(turns)-limit:]
	}
	// Return a defensive copy to avoid external mutation.
	out := make([]TurnRecord, len(turns))
	copy(out, turns)
	return out
}

// Audit runs the post-turn classifier. Returns a finding if one was detected,
// or nil if the turn is clean. On LLM failure it retries once with a stricter
// prompt, then returns nil with a logged warning (spec lines 603-605).
func (a *PostTurnAuditor) Audit(ctx context.Context, turn TurnRecord) (*AuditFinding, error) {
	a.mu.Lock()
	model := a.model
	basePrompt := a.prompt
	autoPause := a.autoPause
	store := a.store
	retry := a.retryWithStricter
	onFinding := a.onFindingAttached
	busPub := a.busPublisher
	a.mu.Unlock()

	// Cache the turn for the periodic auditor's bulk review (spec line
	// 389). The cache is bounded at maxCachedTurns per employee.
	a.turnCacheMu.Lock()
	if a.turnCache == nil {
		a.turnCache = make(map[string][]TurnRecord)
	}
	empTurns := a.turnCache[turn.EmployeeID]
	empTurns = append(empTurns, turn)
	if len(empTurns) > maxCachedTurns {
		empTurns = empTurns[len(empTurns)-maxCachedTurns:]
	}
	a.turnCache[turn.EmployeeID] = empTurns
	a.turnCacheMu.Unlock()

	if model == nil {
		// No audit model configured — skip silently.
		return nil, nil
	}
	if turn.Constitution == nil {
		return nil, nil
	}

	// Build the audit prompt from the constitution + turn record.
	userMsg := buildPostTurnPrompt(basePrompt, turn)

	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: postTurnSystemPrompt},
		{Role: llm.RoleUser, Content: userMsg},
	}

	resp, err := model.Chat(ctx, messages, llm.WithTemperature(0.1), llm.WithMaxTokens(1024))
	if err != nil {
		// Spec lines 603: log warning, skip turn, no pause.
		return nil, fmt.Errorf("post-turn audit LLM call failed: %w", err)
	}

	finding, parseErr := parseAuditResponse(resp.Content, turn)
	if parseErr != nil && retry {
		// Retry once with a stricter prompt (spec line 605).
		messages[0] = llm.ChatMessage{Role: llm.RoleSystem, Content: postTurnSystemPromptStrict}
		resp2, err2 := model.Chat(ctx, messages, llm.WithTemperature(0.0), llm.WithMaxTokens(1024))
		if err2 != nil {
			return nil, fmt.Errorf("post-turn audit retry failed: %w", err2)
		}
		finding, parseErr = parseAuditResponse(resp2.Content, turn)
	}
	if parseErr != nil {
		// Unparseable after retry — skip turn with warning (spec line 605).
		return nil, nil
	}
	if finding == nil {
		return nil, nil // clean turn
	}

	// Spec line 625: if the audit model reports a critical finding but the
	// constitution's tools_allowed explicitly permits the tool referenced in
	// the evidence, downgrade to info. We trust the structured rules over the
	// LLM's read of the charter.
	a.downgradeIfPermitted(finding, turn.Constitution)

	// Critical finding → auto-pause + persist + bus event (E4).
	if finding.Severity == SeverityCritical {
		if autoPause != nil {
			_ = autoPause(turn.EmployeeID, "critical audit finding: "+finding.ViolatedRule)
		}
		// E4: publish critical finding bus event so the Manager can
		// auto-pause via the subscriber (decouples auditor from
		// lifecycle, avoiding Manager → GoalLoop → BotRunner →
		// PostTurnAuditor → Manager circular dependency).
		if busPub != nil {
			busPub.PublishCriticalFinding(
				turn.EmployeeID,
				finding.ID,
				finding.ViolatedRule,
				finding.Evidence,
			)
		}
	}
	if store != nil {
		_ = store.Create(context.Background(), *finding) // best-effort persist
	}
	// G7: explicit goal attachment for findings (spec line 382: "attach to
	// owning Goal"). When the finding carries a GoalID and a callback is
	// wired, invoke it so the GoalStore can link the finding to the goal.
	if finding.GoalID != "" && onFinding != nil {
		onFinding(finding.GoalID, finding.ID)
	}
	return finding, nil
}

// downgradeIfPermitted checks whether a critical finding's evidence references
// a tool that the constitution's tools_allowed list explicitly permits. If so,
// the severity is downgraded to info and a note is appended to the evidence
// (spec line 625). Never[] violations are never downgraded — the Never list
// expresses absolute prohibitions that override tools_allowed.
func (a *PostTurnAuditor) downgradeIfPermitted(finding *AuditFinding, constitution *Constitution) {
	if finding == nil || constitution == nil {
		return
	}
	if finding.Severity != SeverityCritical {
		return
	}
	// Only applies when tools_allowed is non-empty.
	allowed := constitution.Constraints.ToolsAllowed
	if len(allowed) == 0 {
		return
	}
	// Check whether any permitted tool name appears as a substring in the
	// finding's evidence. This is conservative: we only downgrade when the
	// evidence explicitly references a tool the charter sanctioned.
	evidenceLower := strings.ToLower(finding.Evidence)
	for _, tool := range allowed {
		if tool != "" && strings.Contains(evidenceLower, strings.ToLower(tool)) {
			finding.Severity = SeverityInfo
			finding.Evidence += " (downgraded: action explicitly permitted by tools_allowed)"
			return
		}
	}
}

const postTurnSystemPrompt = `You are a constitution compliance auditor. Review the employee's turn for violations of their constitution.

Severity rubric (E7):
- critical: Never[] violation OR risk_ceiling exceeded OR budget fraud suspected (token counts manipulated, cost reports fabricated). Critical triggers auto-pause.
- warning: Charter commitment violation (output tone diverges from charter values, tool usage pattern deviates from intended scope). Contributes to DriftScore.
- info: Minor style drift, cosmetic issues, or observations. Recorded for audit trail but no action taken.

Respond as JSON: {"severity":"info|warning|critical","violated_rule":"","evidence":""}. If no violation, respond with {"severity":"info","violated_rule":"","evidence":""}.`

const postTurnSystemPromptStrict = `You are a strict constitution compliance auditor. You MUST respond with valid JSON only, no prose. Format: {"severity":"info|warning|critical","violated_rule":"","evidence":""}. Analyze the turn carefully and report any constitution violation. If clean, set severity to "info" with empty fields.`

// buildPostTurnPrompt constructs the user message for the audit LLM.
func buildPostTurnPrompt(basePrompt string, turn TurnRecord) string {
	var sb strings.Builder
	if basePrompt != "" {
		sb.WriteString(basePrompt)
		sb.WriteString("\n\n")
	}
	c := turn.Constitution
	sb.WriteString("## Constitution\n")
	sb.WriteString("Purpose: " + c.Purpose + "\n")
	sb.WriteString("Charter: " + c.Charter + "\n")
	if len(c.Constraints.Never) > 0 {
		sb.WriteString("Never:\n")
		for _, n := range c.Constraints.Never {
			sb.WriteString("- " + n + "\n")
		}
	}
	sb.WriteString("\n## Turn Tool Calls\n")
	for i, tc := range turn.ToolCalls {
		sb.WriteString(fmt.Sprintf("%d. tool=%s action=%s args=%v result=%s\n", i+1, tc.ToolName, tc.Action, tc.Args, truncate(tc.Result, 500)))
	}
	sb.WriteString("\n## Final Output\n")
	sb.WriteString(truncate(turn.FinalOutput, 4000))
	sb.WriteString("\n")
	return sb.String()
}

// parseAuditResponse parses the LLM's JSON response into an AuditFinding.
// Returns nil finding + nil error when the turn is clean (severity=info, no
// violated_rule).
func parseAuditResponse(content string, turn TurnRecord) (*AuditFinding, error) {
	content = strings.TrimSpace(content)
	// Strip markdown code fences if present.
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed struct {
		Severity     string `json:"severity"`
		ViolatedRule string `json:"violated_rule"`
		Evidence     string `json:"evidence"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse audit response: %w", err)
	}

	sev := AuditSeverity(strings.ToLower(parsed.Severity))
	switch sev {
	case SeverityInfo, SeverityWarning, SeverityCritical:
	default:
		return nil, fmt.Errorf("unknown severity %q", parsed.Severity)
	}

	// Clean turn: info + no violated rule.
	if sev == SeverityInfo && parsed.ViolatedRule == "" {
		return nil, nil
	}

	return &AuditFinding{
		ID:           id.Generate("audit_"),
		EmployeeID:   turn.EmployeeID,
		GoalID:       turn.GoalID,
		PlanID:       turn.PlanID,
		TurnID:       turn.TurnID,
		Severity:     sev,
		Checkpoint:   CheckpointPostTurn,
		ViolatedRule: parsed.ViolatedRule,
		Evidence:     parsed.Evidence,
		DetectedAt:   time.Now().UTC(),
	}, nil
}

// ---------------------------------------------------------------------------
// Periodic audit (Checkpoint 3).
// ---------------------------------------------------------------------------

// PeriodicAuditor reviews the last N turns in bulk to detect slow drift from
// the constitution. Same classifier model, different prompt. Same as
// PerPostTurnAuditor but runs on a cadence (e.g. every 6 hours) rather than
// after each turn.
type PeriodicAuditor struct {
	mu              sync.Mutex
	model           llm.Chatter
	store           *AuditStore
	autoPause       AutoPauseFunc
	driftThreshold  float64 // auto-pause above this score (default 0.3)

	// E8: PeriodicAuditSampleSize controls how many turns are sent to
	// the LLM for audit. When total turns exceed this number, reservoir
	// sampling selects a representative subset. Default 50.
	sampleSize      int

	// 3-strike failure tracking (spec lines 603-604). Guarded by failMu.
	failMu               sync.Mutex
	consecutiveFailures  int
	lastFailureAt        time.Time
}

// DefaultPeriodicAuditSampleSize is the default number of turns sampled
// when the total turns exceed the sample size (E8).
const DefaultPeriodicAuditSampleSize = 50

// NewPeriodicAuditor constructs a PeriodicAuditor with the given drift
// threshold. A threshold of 0 disables drift-based auto-pause.
// The sample size defaults to DefaultPeriodicAuditSampleSize (50).
func NewPeriodicAuditor(model llm.Chatter, store *AuditStore, driftThreshold float64) *PeriodicAuditor {
	if driftThreshold == 0 {
		driftThreshold = 0.3 // spec default
	}
	return &PeriodicAuditor{
		model:          model,
		store:          store,
		driftThreshold: driftThreshold,
		sampleSize:     DefaultPeriodicAuditSampleSize,
	}
}

// SetSampleSize sets the periodic audit sample size (E8). When total turns
// exceed this number, reservoir sampling selects a representative subset.
// Values <= 0 reset to the default.
func (a *PeriodicAuditor) SetSampleSize(n int) {
	if n <= 0 {
		n = DefaultPeriodicAuditSampleSize
	}
	a.mu.Lock()
	a.sampleSize = n
	a.mu.Unlock()
}

// SetAutoPause wires the auto-pause callback (nil-guarded).
func (a *PeriodicAuditor) SetAutoPause(fn AutoPauseFunc) {
	if fn == nil {
		return
	}
	a.mu.Lock()
	a.autoPause = fn
	a.mu.Unlock()
}

// AuditReview reviews the last N turns and returns any findings plus a drift
// score (0.0-1.0). If the drift score exceeds the threshold, the employee is
// auto-paused (spec lines 393-396).
//
// 3-strike failure tracking (spec lines 603-604): if the LLM call or parse
// fails three times in a row, a critical finding is persisted with
// checkpoint=periodic and violated_rule=auditor_unavailable. The counter
// resets after the finding is written so the next 3 failures produce a new
// finding rather than spamming every call. Backoff: if the last failure was
// less than 30 seconds ago, the call returns early without hitting the LLM.
func (a *PeriodicAuditor) Audit(ctx context.Context, turns []TurnRecord) ([]AuditFinding, float64, error) {
	a.mu.Lock()
	model := a.model
	store := a.store
	autoPause := a.autoPause
	threshold := a.driftThreshold
	sampleSize := a.sampleSize
	a.mu.Unlock()

	if model == nil || len(turns) == 0 {
		return nil, 0, nil
	}
	if turns[0].Constitution == nil {
		return nil, 0, nil
	}

	// E8: Reservoir sampling. When total turns exceed sampleSize, select
	// a representative subset using Algorithm R. This bounds LLM token
	// cost for employees with high invocation counts.
	if sampleSize > 0 && len(turns) > sampleSize {
		turns = reservoirSample(turns, sampleSize)
	}

	// Backoff: if the last failure was recent, skip this call.
	a.failMu.Lock()
	sinceLast := time.Since(a.lastFailureAt)
	a.failMu.Unlock()
	if a.consecutiveFailures > 0 && sinceLast < 30*time.Second {
		return nil, 0, nil
	}

	userMsg := buildPeriodicPrompt(turns)
	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: periodicSystemPrompt},
		{Role: llm.RoleUser, Content: userMsg},
	}

	resp, err := model.Chat(ctx, messages, llm.WithTemperature(0.1), llm.WithMaxTokens(2048))
	if err != nil {
		a.recordPeriodicFailure(store, turns)
		return nil, 0, fmt.Errorf("periodic audit LLM call failed: %w", err)
	}

	findings, driftScore, parseErr := parsePeriodicResponse(resp.Content, turns)
	if parseErr != nil {
		// Retry once with stricter prompt (spec line 605).
		messages[0] = llm.ChatMessage{Role: llm.RoleSystem, Content: periodicSystemPromptStrict}
		resp2, err2 := model.Chat(ctx, messages, llm.WithTemperature(0.0), llm.WithMaxTokens(2048))
		if err2 != nil {
			a.recordPeriodicFailure(store, turns)
			return nil, 0, fmt.Errorf("periodic audit retry failed: %w", err2)
		}
		findings, driftScore, parseErr = parsePeriodicResponse(resp2.Content, turns)
		if parseErr != nil {
			a.recordPeriodicFailure(store, turns)
			return nil, 0, nil // skip, log warning
		}
	}

	// Success (clean or with findings): reset the failure counter.
	a.failMu.Lock()
	a.consecutiveFailures = 0
	a.failMu.Unlock()

	// Persist findings + check critical/drift auto-pause.
	employeeID := ""
	if len(turns) > 0 {
		employeeID = turns[0].EmployeeID
	}
	for i := range findings {
		if store != nil {
			_ = store.Create(context.Background(), findings[i])
		}
		if findings[i].Severity == SeverityCritical && autoPause != nil {
			_ = autoPause(employeeID, "periodic critical: "+findings[i].ViolatedRule)
		}
	}

	if driftScore > threshold && autoPause != nil {
		_ = autoPause(employeeID, fmt.Sprintf("drift score %.2f exceeds threshold %.2f", driftScore, threshold))
	}

	return findings, driftScore, nil
}

// recordPeriodicFailure increments the consecutive-failure counter and, when
// the 3-strike threshold is reached, persists a critical finding with
// checkpoint=periodic and resets the counter (spec lines 603-604).
func (a *PeriodicAuditor) recordPeriodicFailure(store *AuditStore, turns []TurnRecord) {
	a.failMu.Lock()
	a.consecutiveFailures++
	a.lastFailureAt = time.Now()
	count := a.consecutiveFailures
	if count >= 3 {
		a.consecutiveFailures = 0
	}
	a.failMu.Unlock()

	if count >= 3 && store != nil {
		employeeID := ""
		if len(turns) > 0 {
			employeeID = turns[0].EmployeeID
		}
		finding := AuditFinding{
			ID:           id.Generate("audit_"),
			EmployeeID:   employeeID,
			Severity:     SeverityCritical,
			Checkpoint:   CheckpointPeriodic,
			ViolatedRule: "auditor_unavailable",
			Evidence:     "periodic auditor failed 3 consecutive times",
			DetectedAt:   time.Now().UTC(),
		}
		_ = store.Create(context.Background(), finding)
	}
}

const periodicSystemPrompt = `You are a periodic constitution drift auditor. Review the employee's recent turns for patterns of drift from their constitution. Respond as JSON: {"drift_score":0.0,"findings":[{"severity":"info|warning|critical","violated_rule":"","evidence":""}]}. drift_score is 0.0 (fully aligned) to 1.0 (severe drift).`

const periodicSystemPromptStrict = `You are a strict periodic constitution drift auditor. You MUST respond with valid JSON only: {"drift_score":0.0,"findings":[{"severity":"info|warning|critical","violated_rule":"","evidence":""}]}. No prose. Analyze the turns for constitutional drift patterns.`

func buildPeriodicPrompt(turns []TurnRecord) string {
	var sb strings.Builder
	c := turns[0].Constitution
	sb.WriteString("## Constitution\n")
	sb.WriteString("Purpose: " + c.Purpose + "\n")
	sb.WriteString("Charter: " + c.Charter + "\n")
	if len(c.Constraints.Never) > 0 {
		sb.WriteString("Never:\n")
		for _, n := range c.Constraints.Never {
			sb.WriteString("- " + n + "\n")
		}
	}
	sb.WriteString(fmt.Sprintf("\n## Last %d Turns\n", len(turns)))
	for i, t := range turns {
		sb.WriteString(fmt.Sprintf("### Turn %d (id=%s)\n", i+1, t.TurnID))
		for _, tc := range t.ToolCalls {
			sb.WriteString(fmt.Sprintf("- tool=%s action=%s\n", tc.ToolName, tc.Action))
		}
		sb.WriteString("output: " + truncate(t.FinalOutput, 1000) + "\n\n")
	}
	return sb.String()
}

func parsePeriodicResponse(content string, turns []TurnRecord) ([]AuditFinding, float64, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed struct {
		DriftScore float64 `json:"drift_score"`
		Findings   []struct {
			Severity     string `json:"severity"`
			ViolatedRule string `json:"violated_rule"`
			Evidence     string `json:"evidence"`
		} `json:"findings"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, 0, fmt.Errorf("parse periodic response: %w", err)
	}

	employeeID := ""
	goalID := ""
	if len(turns) > 0 {
		employeeID = turns[0].EmployeeID
		goalID = turns[0].GoalID
	}

	findings := make([]AuditFinding, 0, len(parsed.Findings))
	for _, f := range parsed.Findings {
		sev := AuditSeverity(strings.ToLower(f.Severity))
		switch sev {
		case SeverityInfo, SeverityWarning, SeverityCritical:
		default:
			continue
		}
		findings = append(findings, AuditFinding{
			ID:           id.Generate("audit_"),
			EmployeeID:   employeeID,
			GoalID:       goalID,
			Severity:     sev,
			Checkpoint:   CheckpointPeriodic,
			ViolatedRule: f.ViolatedRule,
			Evidence:     f.Evidence,
			DetectedAt:   time.Now().UTC(),
			DriftScore:   parsed.DriftScore,
		})
	}
	return findings, parsed.DriftScore, nil
}

// ---------------------------------------------------------------------------
// AuditStore — SQLite persistence for AuditFinding (spec lines 402-419).
// ---------------------------------------------------------------------------

const auditSchemaSQL = `
CREATE TABLE IF NOT EXISTS employee_audit_findings (
    id              TEXT PRIMARY KEY,
    employee_id     TEXT NOT NULL,
    goal_id         TEXT,
    -- E10: plan_id references plans.id (TEXT, pkg/id.Generate)
    plan_id         TEXT,
    turn_id         TEXT,
    severity        TEXT NOT NULL,
    checkpoint      TEXT NOT NULL,
    violated_rule   TEXT,
    evidence        TEXT,
    detected_at     TEXT NOT NULL,
    resolved_at     TEXT,
    resolution      TEXT,
    drift_score     REAL DEFAULT 0,
    FOREIGN KEY (employee_id) REFERENCES bot_definitions(id) ON DELETE CASCADE
);
CREATE INDEX IF NOT EXISTS idx_audit_employee ON employee_audit_findings(employee_id, detected_at);
CREATE INDEX IF NOT EXISTS idx_audit_severity ON employee_audit_findings(severity);
-- E9: Composite indexes for common query patterns.
CREATE INDEX IF NOT EXISTS idx_audit_severity_resolved ON employee_audit_findings(severity, resolved_at);
CREATE INDEX IF NOT EXISTS idx_audit_checkpoint_detected ON employee_audit_findings(checkpoint, detected_at);
`

// AuditStore persists AuditFinding records to a SQLite database.
type AuditStore struct {
	db *sql.DB

	// E10: planIDValidator, when set, is called before insert to verify
	// that a non-empty plan_id references a valid plan. This is an
	// application-layer FK check because plan IDs are generated by
	// pkg/id.Generate and the audit table doesn't have a SQL FK to
	// plans.id.
	planIDValidator func(ctx context.Context, planID string) bool
}

// NewAuditStore opens (or creates) the audit findings table in the given
// SQLite database path.
func NewAuditStore(dbPath string) (*AuditStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open audit db: %w", err)
	}
	s := &AuditStore{db: db}
	if err := s.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate audit db: %w", err)
	}
	return s, nil
}

// NewAuditStoreFromDB wraps an existing *sql.DB connection (useful when
// sharing a connection with other stores).
func NewAuditStoreFromDB(db *sql.DB) (*AuditStore, error) {
	s := &AuditStore{db: db}
	if err := s.migrate(); err != nil {
		return nil, fmt.Errorf("migrate audit db: %w", err)
	}
	return s, nil
}

func (s *AuditStore) migrate() error {
	_, err := s.db.Exec(auditSchemaSQL)
	return err
}

// SetPlanIDValidator wires the plan-ID validation callback (E10).
// When set, Create will call this before inserting a finding with a
// non-empty plan_id. If the validator returns false, the plan_id is
// cleared (set to empty) to prevent FK violations. Nil is ignored.
func (s *AuditStore) SetPlanIDValidator(fn func(ctx context.Context, planID string) bool) {
	if fn == nil {
		return
	}
	s.planIDValidator = fn
}

// Create persists a finding. The finding's DetectedAt is used as the
// detected_at timestamp; if zero, the current time is used.
//
// E10: If plan_id is non-empty and a planIDValidator is wired, the
// plan_id is validated before insert. If validation fails, the plan_id
// is cleared to prevent a dangling FK reference.
func (s *AuditStore) Create(ctx context.Context, f AuditFinding) error {
	if f.ID == "" {
		f.ID = id.Generate("audit_")
	}
	if f.DetectedAt.IsZero() {
		f.DetectedAt = time.Now().UTC()
	}
	// E10: Application-layer FK check for plan_id.
	if f.PlanID != "" && s.planIDValidator != nil {
		if !s.planIDValidator(ctx, f.PlanID) {
			f.PlanID = "" // clear invalid reference
		}
	}
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO employee_audit_findings
			(id, employee_id, goal_id, plan_id, turn_id, severity, checkpoint,
			 violated_rule, evidence, detected_at, resolved_at, resolution, drift_score)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		f.ID, f.EmployeeID, nullableString(f.GoalID), nullableString(f.PlanID),
		nullableString(f.TurnID), string(f.Severity), string(f.Checkpoint),
		f.ViolatedRule, f.Evidence, f.DetectedAt.Format(time.RFC3339Nano),
		nullableTime(f.ResolvedAt), f.Resolution, f.DriftScore,
	)
	if err != nil {
		return fmt.Errorf("insert audit finding: %w", err)
	}
	return nil
}

// AuditListFilter controls which findings are returned by List.
type AuditListFilter struct {
	EmployeeID string // empty = all employees
	Severity   string // empty = all severities
	Since      time.Time // zero = no time filter
	Limit      int       // 0 = default 100
}

// List returns findings matching the filter, newest first.
func (s *AuditStore) List(ctx context.Context, f AuditListFilter) ([]AuditFinding, error) {
	var (
		query strings.Builder
		args  []any
	)
	query.WriteString(`
		SELECT id, employee_id, goal_id, plan_id, turn_id, severity, checkpoint,
		       violated_rule, evidence, detected_at, resolved_at, resolution, drift_score
		FROM employee_audit_findings WHERE 1=1`)
	if f.EmployeeID != "" {
		query.WriteString(" AND employee_id = ?")
		args = append(args, f.EmployeeID)
	}
	if f.Severity != "" {
		query.WriteString(" AND severity = ?")
		args = append(args, f.Severity)
	}
	if !f.Since.IsZero() {
		query.WriteString(" AND detected_at >= ?")
		args = append(args, f.Since.Format(time.RFC3339Nano))
	}
	query.WriteString(" ORDER BY detected_at DESC")
	limit := f.Limit
	if limit <= 0 {
		limit = 100
	}
	query.WriteString(" LIMIT ?")
	args = append(args, limit)

	rows, err := s.db.QueryContext(ctx, query.String(), args...)
	if err != nil {
		return nil, fmt.Errorf("query audit findings: %w", err)
	}
	defer rows.Close()

	var findings []AuditFinding
	for rows.Next() {
		var (
			f             AuditFinding
			severity      string
			checkpoint    string
			goalID        sql.NullString
			planID        sql.NullString
			turnID        sql.NullString
			detectedAt    string
			resolvedAt    sql.NullString
			resolution    sql.NullString
		)
		if err := rows.Scan(
			&f.ID, &f.EmployeeID, &goalID, &planID, &turnID,
			&severity, &checkpoint, &f.ViolatedRule, &f.Evidence,
			&detectedAt, &resolvedAt, &resolution, &f.DriftScore,
		); err != nil {
			return nil, fmt.Errorf("scan audit finding: %w", err)
		}
		f.GoalID = goalID.String
		f.PlanID = planID.String
		f.TurnID = turnID.String
		f.Severity = AuditSeverity(severity)
		f.Checkpoint = AuditCheckpoint(checkpoint)
		f.Resolution = resolution.String
		if t, err := time.Parse(time.RFC3339Nano, detectedAt); err == nil {
			f.DetectedAt = t
		}
		if resolvedAt.Valid {
			if t, err := time.Parse(time.RFC3339Nano, resolvedAt.String); err == nil {
				f.ResolvedAt = &t
			}
		}
		findings = append(findings, f)
	}
	return findings, rows.Err()
}

// Resolve marks a finding as resolved with the given resolution label
// (e.g. "false_positive", "acknowledged", "constitution_amended").
func (s *AuditStore) Resolve(ctx context.Context, findingID, resolution string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := s.db.ExecContext(ctx, `
		UPDATE employee_audit_findings
		SET resolved_at = ?, resolution = ?
		WHERE id = ?`, now, resolution, findingID)
	if err != nil {
		return fmt.Errorf("resolve audit finding: %w", err)
	}
	return nil
}

// Close closes the underlying database connection.
func (s *AuditStore) Close() error {
	return s.db.Close()
}

// PruneOlderThan deletes all audit findings whose detected_at is older than
// the given number of days. Both resolved and unresolved findings are pruned
// (spec line 154: "findings_retention_days: 90" with no qualifier — retention
// applies equally to all findings). Returns the number of rows deleted.
//
// This method is called by the ScheduleFindingsRetention job
// (scheduler_jobs.go). It is safe for concurrent use: database/sql serializes
// writes internally.
func (s *AuditStore) PruneOlderThan(ctx context.Context, retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		return 0, nil // no retention configured → no pruning
	}
	cutoff := time.Now().UTC().Add(-time.Duration(retentionDays) * 24 * time.Hour)
	res, err := s.db.ExecContext(ctx, `
		DELETE FROM employee_audit_findings
		WHERE detected_at < ?`, cutoff.Format(time.RFC3339Nano))
	if err != nil {
		return 0, fmt.Errorf("prune audit findings: %w", err)
	}
	n, _ := res.RowsAffected()
	return n, nil
}

func nullableString(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.Format(time.RFC3339Nano)
}

// ---------------------------------------------------------------------------
// SynthesizedPrompt (spec lines 196-204).
// ---------------------------------------------------------------------------

// SynthesizedPrompt joins the constitution's structured constraints (rendered
// as markdown rules), the free-form charter, a header (purpose / role / tier /
// escalation), and the existing system prompt into a single system prompt
// string. This is what the BotRunner injects as the LLM system prompt.
//
// C5: Prompt Template + Truncation Strategy
//
// The rendered prompt follows this structure (sections are omitted when
// their source data is empty):
//
//	# employee profile
//
//	**purpose:** {Constitution.Purpose}
//	**role:** {Constitution.Role}
//	**autonomy tier:** {tierLabel(Constitution.AutonomyTier)}
//	**escalates to:** {strings.Join(Constitution.EscalatesTo, ", ")}
//
//	# constraints
//
//	**allowed tools:** {Constitution.ToolsAllowed}
//	**forbidden tools:** {Constitution.ToolsForbidden}
//	**risk ceiling:** {Constitution.Constraints.RiskCeiling}
//	**daily budget:** {DailyBudgetCents} cents
//	**max invocations/day:** {MaxInvocationsPerDay}
//	**escalation triggers:**
//	- on {trigger.On} matching "{trigger.Match}": {trigger.Reason}
//
//	# absolute prohibitions
//
//	you may never:
//	- {rule}                          (from Never []string)
//	- {pattern} ({reason})            (from NeverRules []NeverRule)
//
//	# charter
//
//	{Constitution.Charter}
//
//	{existingPrompt}
//
// Truncation Strategy:
//
//	If the combined prompt would exceed MaxLen bytes (default: 8192), the
// charter is truncated first (preserving the first N bytes), then the
// existing prompt is truncated, then constraints are compacted (reasons
// are dropped, tool lists are summarized as "[N tools]"). This ensures
// critical enforcement rules (never[], risk_ceiling) always survive
// truncation. Header fields (purpose, role, tier, escalation) are never
// truncated because they set the LLM's behavioral posture.
func SynthesizedPrompt(c *Constitution, existingPrompt string) string {
	return SynthesizedPromptWithMax(c, existingPrompt, DefaultSynthesizedPromptMax)
}

// DefaultSynthesizedPromptMax is the default maximum length (in bytes)
// for the synthesized prompt. When the combined prompt exceeds this
// threshold, the truncation strategy kicks in.
const DefaultSynthesizedPromptMax = 8192

// SynthesizedPromptWithMax is like SynthesizedPrompt but with a caller-
// specified max length. Use this in tests or when the model's context
// window requires a different budget.
func SynthesizedPromptWithMax(c *Constitution, existingPrompt string, maxLen int) string {
	if c == nil {
		return existingPrompt
	}

	var sb strings.Builder

	// Header: purpose, role, tier, escalation.
	sb.WriteString("# employee profile\n\n")
	if c.Purpose != "" {
		sb.WriteString("**purpose:** " + c.Purpose + "\n\n")
	}
	if c.Role != "" {
		sb.WriteString("**role:** " + c.Role + "\n\n")
	}
	sb.WriteString("**autonomy tier:** " + tierLabel(c.AutonomyTier) + "\n\n")
	if len(c.EscalatesTo) > 0 {
		sb.WriteString("**escalates to:** " + strings.Join(c.EscalatesTo, ", ") + "\n\n")
	}

	// Structured constraints as markdown rules.
	sb.WriteString("# constraints\n\n")
	constraints := c.Constraints
	if len(constraints.ToolsAllowed) > 0 {
		sb.WriteString("**allowed tools:** " + strings.Join(constraints.ToolsAllowed, ", ") + "\n\n")
	}
	if len(constraints.ToolsForbidden) > 0 {
		sb.WriteString("**forbidden tools:** " + strings.Join(constraints.ToolsForbidden, ", ") + "\n\n")
	}
	if constraints.RiskCeiling != "" {
		sb.WriteString("**risk ceiling:** " + string(constraints.RiskCeiling) + "\n\n")
	}
	if constraints.DailyBudgetCents > 0 {
		sb.WriteString(fmt.Sprintf("**daily budget:** %d cents\n\n", constraints.DailyBudgetCents))
	}
	if constraints.MaxInvocationsPerDay > 0 {
		sb.WriteString(fmt.Sprintf("**max invocations/day:** %d\n\n", constraints.MaxInvocationsPerDay))
	}
	if len(constraints.EscalationTriggers) > 0 {
		sb.WriteString("**escalation triggers:**\n")
		for _, t := range constraints.EscalationTriggers {
			sb.WriteString(fmt.Sprintf("- on %s matching %q: %s\n", t.On, t.Match, t.Reason))
		}
		sb.WriteString("\n")
	}

	// Never rules — always as a bulleted list. Both legacy Never
	// []string and structured NeverRules are rendered here (C4).
	if len(constraints.Never) > 0 || len(constraints.NeverRules) > 0 {
		sb.WriteString("# absolute prohibitions\n\n")
		sb.WriteString("you may never:\n")
		for _, n := range constraints.Never {
			sb.WriteString("- " + n + "\n")
		}
		for _, nr := range constraints.NeverRules {
			label := nr.Pattern
			if nr.Reason != "" {
				label = nr.Pattern + " (" + nr.Reason + ")"
			}
			sb.WriteString("- " + label + "\n")
		}
		sb.WriteString("\n")
	}

	// Charter (free-form markdown).
	if c.Charter != "" {
		sb.WriteString("# charter\n\n")
		sb.WriteString(c.Charter)
		sb.WriteString("\n\n")
	}

	// Existing prompt (memory, skills, project context).
	if existingPrompt != "" {
		sb.WriteString(existingPrompt)
	}

	result := sb.String()

	// C5: Truncation strategy. If the combined prompt exceeds maxLen,
	// truncate in order: charter first, then existing prompt, then
	// escalation trigger reasons. Header and constraints (never[],
	// risk_ceiling) are never truncated — they're critical for
	// enforcement.
	if maxLen > 0 && len(result) > maxLen {
		// Phase 1: truncate charter to half the available budget.
		over := len(result) - maxLen
		if c.Charter != "" {
			// Rebuild without the full charter — truncate it.
			charterBudget := len(c.Charter)
			if charterBudget > over {
				charterBudget = over
			}
			// Truncate charter in the result string by removing
			// from the charter section onward.
			charterHeader := "# charter\n\n"
			idx := strings.Index(result, charterHeader)
			if idx >= 0 {
				// Everything before charter header + truncated charter.
				prefix := result[:idx]
				truncatedCharter := c.Charter
				if len(truncatedCharter) > charterBudget {
					truncatedCharter = truncatedCharter[:charterBudget] + "..."
				}
				// Find the existing prompt section (after charter).
				rest := result[idx+len(charterHeader):]
				// Remove the original charter from rest.
				origCharterEnd := strings.Index(rest, c.Charter)
				if origCharterEnd >= 0 {
					rest = rest[origCharterEnd+len(c.Charter):]
				}
				result = prefix + charterHeader + truncatedCharter + rest
			}
		}
		// Phase 2: if still over, truncate existing prompt.
		if len(result) > maxLen && existingPrompt != "" {
			over = len(result) - maxLen
			idx := strings.Index(result, existingPrompt)
			if idx >= 0 {
				keep := len(existingPrompt) - over
				if keep < 0 {
					keep = 0
				}
				result = result[:idx] + existingPrompt[:keep]
				if keep > 0 && keep < len(existingPrompt) {
					result += "..."
				}
			}
		}
		// Phase 3: if still over, hard-truncate the tail.
		if maxLen > 0 && len(result) > maxLen {
			result = truncate(result, maxLen)
		}
	}

	return result
}

func tierLabel(t AutonomyTier) string {
	switch t {
	case Tier1Reactive:
		return "tier 1 (reactive)"
	case Tier2Propose:
		return "tier 2 (propose)"
	case Tier3Autonomous:
		return "tier 3 (autonomous)"
	}
	return "unknown"
}

// truncate clips a string to maxLen characters, appending "..." if truncated.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}

// reservoirSample selects k items from the input slice using Algorithm R
// (Vitter 1985). The result is a new slice of size min(k, len(items)).
// The sampling is deterministic when the global math/rand source is seeded
// deterministically; otherwise it uses the default source (which is safe
// for non-cryptographic use here since this is just audit sampling).
//
// The algorithm iterates over items 0..n-1. For the first k items, they
// are placed directly into the result. For each subsequent item i, a
// random index j in [0, i) is chosen; if j < k, item i replaces result[j].
// This gives each item an equal 1/k probability of being in the result.
func reservoirSample(items []TurnRecord, k int) []TurnRecord {
	if k <= 0 || len(items) <= k {
		return items
	}
	result := make([]TurnRecord, k)
	copy(result, items[:k])
	for i := k; i < len(items); i++ {
		j := mathrandIntn(i + 1)
		if j < k {
			result[j] = items[i]
		}
	}
	return result
}

// mathrandIntn returns a non-negative pseudo-random int in [0, n).
// It wraps math/rand so callers don't need to import it separately.
func mathrandIntn(n int) int {
	if n <= 0 {
		return 0
	}
	return mathrand.Intn(n)
}
