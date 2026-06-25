// Package employee implements the AI Employee design (see
// docs/superpowers/specs/2026-06-23-ai-employee-design.md).
//
// This file implements Phase 3 of the spec: the GoalLoop state machine.
// The GoalLoop is the per-tier runtime that decides what the employee should
// do next. Three operations form the core cycle:
//
//	ASSESS → PLAN → EXECUTE → REFLECT
//	   ↑                        │
//	   └────────────────────────┘
//
// Tier 1 (reactive) skips PLAN and runs ASSESS → EXECUTE → REFLECT directly.
// Tier 2 (propose) runs the full cycle but pauses for human signoff between
// PLAN and EXECUTE. Tier 3 (autonomous, phase 2) would run the full cycle
// without the signoff gate.
//
// See spec lines 277-323 for the state-machine diagram and tier behaviour.
//
// BLOCKER NOTE: This file references Constitution, AutonomyTier,
// Tier1Reactive/Tier2Propose/Tier3Autonomous, ConstitutionalConstraints,
// EscalationTrigger, and EscalationOn* types defined in constitution.go
// (Phase 1). Those types are also referenced by enforcement.go (Phase 4) and
// enforcement_test.go. Until Phase 1 lands, this file will not compile.
// The shapes used here match spec lines 126-194 and the test fixture in
// enforcement_test.go (testConstitution).
package employee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/id"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

// DefaultMaxConsecutiveFailures is the spec default for the consecutive-failure
// threshold that triggers goal at_risk/broken marking and employee auto-pause
// (spec lines 588-591).
const DefaultMaxConsecutiveFailures = 3

// goalLoopIDPrefix is the prefix for goal-loop run IDs (used in audit trails).
const goalLoopIDPrefix = "gloop_"

// ---------------------------------------------------------------------------
// Trigger + candidate types
// ---------------------------------------------------------------------------

// TriggerEvent represents an external stimulus that wakes the GoalLoop. Sources
// are typically "cron", "webhook", "bus", or "manual". Topic is the cron
// schedule, webhook path, or bus topic that fired. Payload is the opaque
// event body (webhook JSON, bus message, etc.).
type TriggerEvent struct {
	Source  string    `json:"source"`
	Topic   string    `json:"topic,omitempty"`
	Payload []byte    `json:"payload,omitempty"`
	FiredAt time.Time `json:"fired_at"`
}

// CandidatePlan is a potential plan produced by the ASSESS step. The LLM in
// ASSESS proposes one or more candidates; each becomes a Plan via PlanCreator
// (tier 2+) or is executed inline (tier 1).
type CandidatePlan struct {
	Title       string `json:"title"`
	Description string `json:"description"`
	Prompt      string `json:"prompt"`
}

// PlanRef is a lightweight handle to a created Plan. It decouples GoalLoop
// from internal/plan.Plan (avoiding a direct import cycle) while carrying the
// fields the loop needs for execution and reflection.
type PlanRef struct {
	ID     string `json:"id"`
	State  string `json:"state"`
	Prompt string `json:"prompt,omitempty"`
}

// ---------------------------------------------------------------------------
// Dependency interfaces
// ---------------------------------------------------------------------------

// PlanCreator abstracts plan creation. The concrete implementation wraps
// internal/plan.PlanManager.CreatePlan so this package does not import
// internal/plan directly (cycle risk via internal/config → internal/bus →
// ...). Implementations should translate the parameters into the
// PlanManager.CreatePlan signature.
//
// The context may carry a timeout for approval-expiry semantics (spec line
// 592). If the underlying PlanManager does not yet support approval_timeout,
// implementations should document this as future work.
type PlanCreator interface {
	CreatePlan(ctx context.Context, title, description, projectID, sessionID string) (PlanRef, error)
}

// Reflector is the LLM chatter used for ASSESS and REFLECT prompts. It is
// satisfied by llm.Chatter; the indirection allows tests to inject a stub.
type Reflector interface {
	Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error)
}

// PauseFunc pauses the employee when called. Injected by the caller (typically
// wired to EmployeeManager.Pause or BotManager.PauseBot). Used by the failure
// counter after N consecutive failures (spec lines 588-591).
type PauseFunc func(employeeID string, reason string) error

// GoalLookup retrieves the active goal for an employee, if any. The GoalLoop
// uses this to attach health updates to the right goal. Returning (nil, nil)
// is valid: the loop runs without a goal (pure tier-1 reactive mode).
type GoalLookup interface {
	ActiveGoal(ctx context.Context, employeeID string) (*Goal, error)
}

// ---------------------------------------------------------------------------
// GoalLoop
// ---------------------------------------------------------------------------

// GoalLoop is the per-employee runtime that drives the ASSESS → PLAN →
// EXECUTE → REFLECT cycle. One loop per employee, driven by the existing
// scheduler.
//
// All dependencies are injected via the constructor or With* options, making
// the loop fully mock-friendly for table-driven tests.
//
// Concurrency: the loop holds a mutex only over in-memory snapshot fields
// (consecutive failures, last result). All I/O (LLM calls, plan creation,
// bot execution) happens outside the lock per CLAUDE.md mutex-scope rule.
type GoalLoop struct {
	employeeID string
	constitution *Constitution
	goalStore   *GoalStore
	runner      bot.BotExecutor
	planner     PlanCreator
	auditor     *PostTurnAuditor
	reflector   Reflector
	logger      *slog.Logger

	// Configurable thresholds.
	maxConsecutiveFailures int

	// External callbacks.
	pauseFn PauseFunc
	goalLookup GoalLookup
	statusFn func() string // returns "running"|"paused"|"error"|"" (empty=unknown)

	// emitMetricFn is the telemetry callback injected by the Manager
	// (same pattern as pauseFn). When non-nil, Reflect emits the
	// employee.goal.health gauge (spec line 673) tagged by goal_id and
	// employee_id. Nil means telemetry is disabled — the loop runs fine
	// without it.
	emitMetricFn EmitMetricFunc

	// Mutable state (guarded by mu).
	mu                   sync.Mutex
	consecutiveFailures  int
	lastResult           *bot.BotExecutionResult
	lastAssessmentTime   time.Time
}

// EmitMetricFunc is the callback signature for emitting telemetry from the
// GoalLoop. It mirrors Manager.emitMetric so the loop can delegate without
// holding a direct Manager reference.
type EmitMetricFunc func(name string, value float64, tags map[string]string)

// NewGoalLoop constructs a GoalLoop for the given employee. The constitution
// may be nil during pre-wiring; Decide will return an error until one is set.
// The goalStore may be nil for pure reactive (tier-1) employees that do not
// track goals. The logger defaults to slog.Default() if nil.
func NewGoalLoop(employeeID string, c *Constitution, store *GoalStore, logger *slog.Logger) *GoalLoop {
	if logger == nil {
		logger = slog.Default()
	}
	return &GoalLoop{
		employeeID:             employeeID,
		constitution:           c,
		goalStore:              store,
		logger:                 logger.With("component", "goal-loop", "employee_id", employeeID),
		maxConsecutiveFailures: DefaultMaxConsecutiveFailures,
	}
}

// WithExecutor injects the BotExecutor used by Execute. Required for the loop
// to run; Decide returns an error if the executor is unset.
func (l *GoalLoop) WithExecutor(r bot.BotExecutor) *GoalLoop {
	if r != nil {
		l.runner = r
	}
	return l
}

// WithPlanner injects the PlanCreator used by Plan. Required for tier 2+
// employees.
func (l *GoalLoop) WithPlanner(p PlanCreator) *GoalLoop {
	if p != nil {
		l.planner = p
	}
	return l
}

// WithAuditor injects the PostTurnAuditor used by Reflect. Optional; if unset,
// Reflect skips the post-turn audit checkpoint.
func (l *GoalLoop) WithAuditor(a *PostTurnAuditor) *GoalLoop {
	if a != nil {
		l.auditor = a
	}
	return l
}

// WithReflector injects the LLM chatter used for ASSESS and REFLECT prompts.
// Required for ASSESS to invoke the LLM.
func (l *GoalLoop) WithReflector(r Reflector) *GoalLoop {
	if r != nil {
		l.reflector = r
	}
	return l
}

// WithGoalLookup injects a goal-lookup strategy. Optional; if unset, the loop
// queries goalStore directly (if non-nil).
func (l *GoalLoop) WithGoalLookup(gl GoalLookup) *GoalLoop {
	if gl != nil {
		l.goalLookup = gl
	}
	return l
}

// WithMaxConsecutiveFailures overrides the default failure threshold (spec
// default: 3). Setting to 0 disables auto-pause on consecutive failures.
func (l *GoalLoop) WithMaxConsecutiveFailures(n int) *GoalLoop {
	if n > 0 {
		l.maxConsecutiveFailures = n
	}
	return l
}

// WithPauseFunc injects the auto-pause callback used when the consecutive
// failure counter reaches the threshold. Optional; if unset, the counter
// increments but no pause action is taken (the goal is still marked broken).
func (l *GoalLoop) WithPauseFunc(fn PauseFunc) *GoalLoop {
	if fn != nil {
		l.pauseFn = fn
	}
	return l
}

// SetStatusFunc wires the status-checking callback used by Reflect to detect
// operator pauses mid-flight (spec line 615). The callback must return the
// employee's current status string ("running", "paused", "error", or "" for
// unknown). Nil is ignored (typed-nil guard per CLAUDE.md).
func (l *GoalLoop) SetStatusFunc(fn func() string) {
	if fn == nil {
		return
	}
	l.mu.Lock()
	l.statusFn = fn
	l.mu.Unlock()
}

// SetEmitMetricFunc wires the telemetry callback used by Reflect to emit
// employee.goal.health (spec line 673). Nil is ignored (typed-nil guard per
// CLAUDE.md). The callback is invoked outside the loop's mutex.
func (l *GoalLoop) SetEmitMetricFunc(fn EmitMetricFunc) {
	if fn == nil {
		return
	}
	l.mu.Lock()
	l.emitMetricFn = fn
	l.mu.Unlock()
}

// SetConstitution atomically replaces the constitution. Safe to call
// concurrently with Decide; the next Assess/Plan/Execute/Reflect invocation
// will pick up the new constitution.
func (l *GoalLoop) SetConstitution(c *Constitution) {
	l.mu.Lock()
	l.constitution = c
	l.mu.Unlock()
}

// ---------------------------------------------------------------------------
// State machine: Assess / Plan / Execute / Reflect
// ---------------------------------------------------------------------------

// assessSystemPrompt is the system message for the ASSESS LLM call. It
// instructs the model to evaluate the current state and propose actions.
const assessSystemPrompt = `You are the assessment module of an AI employee. Given the trigger event and your constitution, decide what actions (if any) should be taken.

Respond as JSON:
{
  "candidates": [
    {"title": "...", "description": "...", "prompt": "the instruction for the executor"}
  ]
}

If no action is needed, return {"candidates": []}.`

// reflectSystemPrompt is the system message for the REFLECT LLM call. It
// instructs the model to evaluate the execution outcome and update goal
// health.
const reflectSystemPrompt = `You are the reflection module of an AI employee. Given the execution outcome and your goal, assess whether the mandate is being met.

Respond as JSON:
{
  "health": "healthy" | "at_risk" | "broken",
  "reasoning": "..."
}`

// Assess is the first step of the GoalLoop cycle. It reads the current state,
// invokes the LLM with the trigger context, and returns one or more candidate
// plans representing what the employee should do (spec lines 287-296).
//
// For tier-1 employees, the LLM's response becomes an implicit single-step
// plan. For tier-2+, each candidate becomes a Plan via PlanCreator.
//
// ASSESS JSON parse failure (spec line 590): if the LLM produces invalid
// JSON, Assess falls back to tier-1 behaviour — wraps the raw output as a
// single candidate with the LLM text as the prompt, logs a warning finding,
// and returns successfully. The loop never crashes on a hallucinated schema.
func (l *GoalLoop) Assess(ctx context.Context, trigger TriggerEvent) ([]CandidatePlan, error) {
	l.mu.Lock()
	constitution := l.constitution
	reflector := l.reflector
	logger := l.logger
	l.mu.Unlock()

	if reflector == nil {
		return nil, errors.New("assess: no reflector (LLM) configured")
	}
	if constitution == nil {
		return nil, errors.New("assess: no constitution configured")
	}

	// Build the ASSESS prompt: constitution context + trigger payload.
	userMsg := buildAssessUserPrompt(constitution, trigger)
	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: assessSystemPrompt},
		{Role: llm.RoleUser, Content: userMsg},
	}

	resp, err := reflector.Chat(ctx, messages, llm.WithTemperature(0.2), llm.WithMaxTokens(2048))
	if err != nil {
		return nil, fmt.Errorf("assess LLM call failed: %w", err)
	}

	candidates, parseErr := parseAssessResponse(resp.Content)
	if parseErr != nil {
		// Spec line 590: fall back to tier-1 behaviour — wrap the raw LLM
		// output as a single candidate and log a warning.
		logger.Warn("assess: LLM returned unparseable JSON, falling back to tier-1 behaviour",
			"error", parseErr,
			"raw_length", len(resp.Content))
		candidates = []CandidatePlan{{
			Title:       "reactive response (assess fallback)",
			Description: fmt.Sprintf("LLM produced unparseable assessment: %v", parseErr),
			Prompt:      resp.Content,
		}}
	}

	logger.Debug("assess complete",
		"candidates", len(candidates),
		"trigger_source", trigger.Source,
		"trigger_topic", trigger.Topic)
	return candidates, nil
}

// Plan creates a pending plan from a candidate via the PlanCreator. The plan
// enters PendingApproval state and routes to the employee's escalates_to for
// signoff (tier 2+ only). Returns the PlanRef on success.
func (l *GoalLoop) Plan(ctx context.Context, candidate CandidatePlan) (PlanRef, error) {
	l.mu.Lock()
	planner := l.planner
	logger := l.logger
	l.mu.Unlock()

	if planner == nil {
		return PlanRef{}, errors.New("plan: no PlanCreator configured (tier 2+ requires planner)")
	}

	// Build a session/project identifier for plan tracking. The GoalLoop does
	// not own a project context; we use the employee ID as a logical grouping.
	projectID := l.employeeID
	sessionID := id.Generate(goalLoopIDPrefix)

	ref, err := planner.CreatePlan(ctx, candidate.Title, candidate.Description, projectID, sessionID)
	if err != nil {
		return PlanRef{}, fmt.Errorf("create plan: %w", err)
	}

	// Thread the candidate's prompt through the PlanRef so Execute can use it
	// as the user message to the BotExecutor.
	if candidate.Prompt != "" {
		ref.Prompt = candidate.Prompt
	} else {
		// Fall back to the title as a synthetic prompt if the candidate has no
		// explicit prompt field.
		ref.Prompt = candidate.Title
	}

	logger.Info("plan created",
		"plan_id", ref.ID,
		"plan_state", ref.State,
		"title", candidate.Title)
	return ref, nil
}

// Execute runs the BotExecutor with the plan's prompt. For tier-1, the prompt
// comes directly from the ASSESS candidate. For tier-2+, the prompt is the
// approved plan's instruction.
//
// The runner.BuildSystemPrompt path is not used here; the GoalLoop constructs
// the system prompt from the constitution via SynthesizedPrompt so that
// constraints and charter are injected.
func (l *GoalLoop) Execute(ctx context.Context, plan PlanRef) (*bot.BotExecutionResult, error) {
	l.mu.Lock()
	runner := l.runner
	constitution := l.constitution
	logger := l.logger
	l.mu.Unlock()

	if runner == nil {
		return nil, errors.New("execute: no BotExecutor configured")
	}

	systemPrompt := SynthesizedPrompt(constitution, "")
	// Use the plan's prompt as the user message if available; fall back to the
	// generic mandate instruction.
	userMessage := plan.Prompt
	if userMessage == "" {
		userMessage = fmt.Sprintf("[plan %s] execute per your mandate", plan.ID)
	}

	start := time.Now()
	output, tokens, err := runner.ExecuteBot(ctx, systemPrompt, userMessage)
	duration := time.Since(start)

	result := &bot.BotExecutionResult{
		BotID:      l.employeeID,
		Output:     output,
		TokensUsed: tokens,
		Duration:   duration,
	}
	if err != nil {
		result.Success = false
		result.Error = err.Error()
		logger.Error("execute failed",
			"plan_id", plan.ID,
			"error", err,
			"duration", duration)
	} else {
		result.Success = true
		logger.Info("execute succeeded",
			"plan_id", plan.ID,
			"tokens", tokens,
			"duration", duration)
	}
	return result, nil
}

// Reflect evaluates the execution outcome and updates the goal's health
// (spec line 296). Failed executions mark the goal at_risk or broken after N
// consecutive failures (configurable, default 3).
//
// The reflection LLM is asked "did this help?" with the execution output and
// the goal mandate. The health verdict is applied to the active goal (if any)
// via the GoalStore.
func (l *GoalLoop) Reflect(ctx context.Context, plan PlanRef, result *bot.BotExecutionResult) (GoalHealth, error) {
	l.mu.Lock()
	constitution := l.constitution
	reflector := l.reflector
	store := l.goalStore
	logger := l.logger
	threshold := l.maxConsecutiveFailures
	pauseFn := l.pauseFn
	statusFn := l.statusFn
	emitMetricFn := l.emitMetricFn
	l.mu.Unlock()

	// Spec line 615: operator pauses while invocation in flight → in-flight
	// invocation completes, but the post-turn REFLECT step is skipped and no
	// new invocations start until resumed.
	if statusFn != nil {
		if strings.EqualFold(statusFn(), "paused") {
			logger.Debug("skipping reflect: employee paused mid-flight")
			// Set goal health to unknown via best-effort persistence.
			if store != nil {
				if goal, err := l.lookupActiveGoal(ctx); err == nil && goal != nil {
					goal.Assess(GoalUnknown, time.Now().UTC())
					if updateErr := store.Update(ctx, goal); updateErr != nil {
						logger.Warn("failed to persist goal health during pause",
							"goal_id", goal.ID, "error", updateErr)
					}
					// Emit goal.health metric (spec line 673).
					if emitMetricFn != nil {
						emitMetricFn("employee.goal.health", float64(GoalUnknown), map[string]string{
							"goal_id":     goal.ID,
							"employee_id": l.employeeID,
						})
					}
				}
			}
			return GoalUnknown, nil
		}
	}

	// Update consecutive failure counter (spec lines 588-591).
	if result != nil && !result.Success {
		l.mu.Lock()
		l.consecutiveFailures++
		count := l.consecutiveFailures
		l.mu.Unlock()
		logger.Warn("execution failed",
			"plan_id", plan.ID,
			"consecutive_failures", count,
			"threshold", threshold)
	} else {
		// Reset counter on success.
		l.mu.Lock()
		l.consecutiveFailures = 0
		l.mu.Unlock()
	}

	// Run the post-turn auditor if configured (Checkpoint 2).
	if l.auditor != nil && result != nil {
		turn := TurnRecord{
			EmployeeID:  l.employeeID,
			PlanID:      plan.ID,
			FinalOutput: result.Output,
			Constitution: constitution,
		}
		if _, auditErr := l.auditor.Audit(ctx, turn); auditErr != nil {
			logger.Warn("post-turn audit failed (non-fatal)", "error", auditErr)
		}
	}

	// Determine goal health.
	health := GoalHealthy
	if result == nil || !result.Success {
		// Failure path: determine at_risk vs broken based on consecutive count.
		l.mu.Lock()
		count := l.consecutiveFailures
		l.mu.Unlock()
		if threshold > 0 && count >= threshold {
			health = GoalBroken
			// Auto-pause the employee (spec line 588).
			if pauseFn != nil {
				reason := fmt.Sprintf("consecutive failures: %d (threshold %d)", count, threshold)
				if pauseErr := pauseFn(l.employeeID, reason); pauseErr != nil {
					logger.Error("auto-pause failed", "error", pauseErr)
				}
			}
		} else {
			health = GoalAtRisk
		}
	} else if reflector != nil {
		// Success path: ask the LLM "did this help?".
		assessed, err := l.reflectViaLLM(ctx, reflector, constitution, result)
		if err != nil {
			logger.Warn("reflect LLM call failed, defaulting to healthy", "error", err)
			health = GoalHealthy
		} else {
			health = assessed
		}
	}

	// Update the active goal's health in the store.
	if store != nil {
		if goal, err := l.lookupActiveGoal(ctx); err == nil && goal != nil {
			now := time.Now().UTC()
			goal.Assess(health, now)
			if updateErr := store.Update(ctx, goal); updateErr != nil {
				logger.Warn("failed to persist goal health update", "goal_id", goal.ID, "error", updateErr)
			}
			// Emit goal.health metric (spec line 673).
			if emitMetricFn != nil {
				emitMetricFn("employee.goal.health", float64(health), map[string]string{
					"goal_id":     goal.ID,
					"employee_id": l.employeeID,
				})
			}
		}
	}

	l.mu.Lock()
	l.lastAssessmentTime = time.Now().UTC()
	l.lastResult = result
	l.mu.Unlock()

	logger.Info("reflect complete", "health", health.String(), "plan_id", plan.ID)
	return health, nil
}

// reflectViaLLM asks the reflector LLM to assess the execution outcome.
func (l *GoalLoop) reflectViaLLM(ctx context.Context, reflector Reflector, c *Constitution, result *bot.BotExecutionResult) (GoalHealth, error) {
	userMsg := buildReflectUserPrompt(c, result)
	messages := []llm.ChatMessage{
		{Role: llm.RoleSystem, Content: reflectSystemPrompt},
		{Role: llm.RoleUser, Content: userMsg},
	}
	resp, err := reflector.Chat(ctx, messages, llm.WithTemperature(0.1), llm.WithMaxTokens(1024))
	if err != nil {
		return GoalUnknown, fmt.Errorf("reflect LLM call: %w", err)
	}
	health, err := parseReflectResponse(resp.Content)
	if err != nil {
		// Unparseable: default to GoalHealthy (don't punish for an LLM hiccup).
		return GoalHealthy, nil
	}
	return health, nil
}

// lookupActiveGoal retrieves the active goal for this employee, preferring
// the injected GoalLookup, then falling back to GoalStore.ListActive.
func (l *GoalLoop) lookupActiveGoal(ctx context.Context) (*Goal, error) {
	l.mu.Lock()
	lookup := l.goalLookup
	store := l.goalStore
	l.mu.Unlock()

	if lookup != nil {
		return lookup.ActiveGoal(ctx, l.employeeID)
	}
	if store == nil {
		return nil, nil
	}
	goals, err := store.ListActive(ctx, l.employeeID)
	if err != nil {
		return nil, err
	}
	if len(goals) == 0 {
		return nil, nil
	}
	return goals[0], nil
}

// ---------------------------------------------------------------------------
// Top-level dispatch: Decide
// ---------------------------------------------------------------------------

// Decide is the top-level entry point for the GoalLoop. It dispatches based on
// the employee's AutonomyTier (spec lines 287-301):
//
//   - Tier1Reactive: Assess(trigger) → if candidates, Execute immediately
//     (no Plan signoff). The LLM's response becomes an implicit single-step
//     plan.
//   - Tier2Propose: Assess → for each candidate, Plan (creates pending plan)
//     → returns. Execute is only called from ApproveAndExecute when the plan
//     is approved via signoff.
//   - Tier3Autonomous: same as Tier 2 but Execute immediately after Assess
//     (no approval). Phase 2 — returns an error.
//
// Decide returns nil on successful dispatch (including when ASSESS produces
// no candidates — a no-op is a valid outcome).
func (l *GoalLoop) Decide(ctx context.Context, trigger TriggerEvent) error {
	l.mu.Lock()
	constitution := l.constitution
	logger := l.logger
	l.mu.Unlock()

	if constitution == nil {
		return errors.New("decide: no constitution configured")
	}

	tier := constitution.AutonomyTier
	switch tier {
	case Tier1Reactive:
		return l.decideTier1(ctx, trigger, logger)
	case Tier2Propose:
		return l.decideTier2(ctx, trigger, logger)
	case Tier3Autonomous:
		// Phase 2 (spec line 298-300): not yet implemented.
		return fmt.Errorf("tier 3 not yet implemented")
	default:
		return fmt.Errorf("unknown autonomy tier: %d", tier)
	}
}

// decideTier1 implements tier-1 reactive behaviour: Assess → Execute (no Plan).
func (l *GoalLoop) decideTier1(ctx context.Context, trigger TriggerEvent, logger *slog.Logger) error {
	candidates, err := l.Assess(ctx, trigger)
	if err != nil {
		return fmt.Errorf("tier1 assess: %w", err)
	}
	if len(candidates) == 0 {
		logger.Debug("tier1 assess produced no candidates; no-op")
		return nil
	}

	// Execute the first candidate directly. Tier-1 is reactive: single-step.
	candidate := candidates[0]
	planRef := PlanRef{
		ID:     id.Generate(goalLoopIDPrefix),
		State:  "executing",
		Prompt: candidate.Prompt,
	}
	result, execErr := l.Execute(ctx, planRef)
	if execErr != nil {
		// Build a synthetic failure result so Reflect can track the counter.
		result = &bot.BotExecutionResult{
			BotID:   l.employeeID,
			Success: false,
			Error:   execErr.Error(),
		}
	}

	if _, reflectErr := l.Reflect(ctx, planRef, result); reflectErr != nil {
		logger.Warn("tier1 reflect failed (non-fatal)", "error", reflectErr)
	}
	return nil
}

// decideTier2 implements tier-2 propose behaviour: Assess → Plan (pending).
// Execute is deferred until ApproveAndExecute is called after signoff.
func (l *GoalLoop) decideTier2(ctx context.Context, trigger TriggerEvent, logger *slog.Logger) error {
	candidates, err := l.Assess(ctx, trigger)
	if err != nil {
		return fmt.Errorf("tier2 assess: %w", err)
	}
	if len(candidates) == 0 {
		logger.Debug("tier2 assess produced no candidates; no-op")
		return nil
	}

	if l.planner == nil {
		return errors.New("tier2: no PlanCreator configured")
	}

	// Warn if escalates_to is empty (spec edge case, line 620).
	l.mu.Lock()
	constitution := l.constitution
	l.mu.Unlock()
	if len(constitution.EscalatesTo) == 0 {
		logger.Warn("tier2 employee has empty escalates_to; plans will sit in pending_approval",
			"employee_id", l.employeeID)
	}

	for _, candidate := range candidates {
		_, planErr := l.Plan(ctx, candidate)
		if planErr != nil {
			logger.Error("tier2 plan creation failed",
				"candidate", candidate.Title,
				"error", planErr)
			// Continue to next candidate; one failure shouldn't block others.
			continue
		}
	}
	return nil
}

// ---------------------------------------------------------------------------
// ApproveAndExecute
// ---------------------------------------------------------------------------

// ApproveAndExecute is called when a plan is approved via signoff. It runs
// Execute + Reflect and updates the active Goal's health (spec line 295).
//
// The planRef must carry the ID of the approved plan. After execution, the
// goal's ActivePlanID is cleared and the plan ID is appended to PlanHistory.
func (l *GoalLoop) ApproveAndExecute(ctx context.Context, planRef PlanRef) (*bot.BotExecutionResult, GoalHealth, error) {
	l.mu.Lock()
	logger := l.logger
	store := l.goalStore
	l.mu.Unlock()

	// Set the active plan ID BEFORE execution so concurrent observers see the
	// correct in-flight plan (spec: record ActivePlanID during execution, not
	// after).
	if store != nil {
		if goal, lookupErr := l.lookupActiveGoal(ctx); lookupErr == nil && goal != nil {
			goal.SetActivePlan(planRef.ID)
			if updateErr := store.Update(ctx, goal); updateErr != nil {
				logger.Warn("failed to set active plan on goal",
					"goal_id", goal.ID, "plan_id", planRef.ID, "error", updateErr)
			}
		}
	}

	result, err := l.Execute(ctx, planRef)
	if err != nil {
		// Build a synthetic failure result so Reflect can track the counter.
		result = &bot.BotExecutionResult{
			BotID:   l.employeeID,
			Success: false,
			Error:   err.Error(),
		}
	}

	health, reflectErr := l.Reflect(ctx, planRef, result)
	if reflectErr != nil {
		logger.Warn("approve-and-execute reflect failed (non-fatal)", "error", reflectErr)
	}

	// After Reflect completes (success OR failure), clear the active plan and
	// append to history.
	if store != nil {
		if goal, lookupErr := l.lookupActiveGoal(ctx); lookupErr == nil && goal != nil {
			goal.SetActivePlan("")
			goal.AppendHistory(planRef.ID)
			if updateErr := store.Update(ctx, goal); updateErr != nil {
				logger.Warn("failed to clear active plan on goal",
					"goal_id", goal.ID, "error", updateErr)
			}
		}
	}

	return result, health, nil
}

// ---------------------------------------------------------------------------
// Accessors
// ---------------------------------------------------------------------------

// ConsecutiveFailures returns the current consecutive-failure count. Safe for
// concurrent use.
func (l *GoalLoop) ConsecutiveFailures() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.consecutiveFailures
}

// ResetFailureCounter zeroes the consecutive-failure counter. Called when an
// operator manually resumes the employee.
func (l *GoalLoop) ResetFailureCounter() {
	l.mu.Lock()
	l.consecutiveFailures = 0
	l.mu.Unlock()
}

// LastAssessmentTime returns when Reflect last ran. Zero if never.
func (l *GoalLoop) LastAssessmentTime() time.Time {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.lastAssessmentTime
}

// EmployeeID returns the owning employee's identifier.
func (l *GoalLoop) EmployeeID() string {
	return l.employeeID
}

// ---------------------------------------------------------------------------
// Prompt builders
// ---------------------------------------------------------------------------

// buildAssessUserPrompt constructs the user message for the ASSESS LLM call.
func buildAssessUserPrompt(c *Constitution, trigger TriggerEvent) string {
	var sb strings.Builder
	sb.WriteString("# employee constitution\n\n")
	sb.WriteString("**purpose:** " + c.Purpose + "\n")
	sb.WriteString("**role:** " + c.Role + "\n\n")

	if len(c.Constraints.Never) > 0 {
		sb.WriteString("## prohibitions\n")
		for _, n := range c.Constraints.Never {
			sb.WriteString("- " + n + "\n")
		}
		sb.WriteString("\n")
	}

	sb.WriteString("# trigger event\n\n")
	sb.WriteString("**source:** " + trigger.Source + "\n")
	if trigger.Topic != "" {
		sb.WriteString("**topic:** " + trigger.Topic + "\n")
	}
	sb.WriteString("**fired_at:** " + trigger.FiredAt.UTC().Format(time.RFC3339) + "\n")
	if len(trigger.Payload) > 0 {
		sb.WriteString("**payload:**\n")
		sb.WriteString(truncate(string(trigger.Payload), 4000))
		sb.WriteString("\n")
	}

	sb.WriteString("\n# task\n\n")
	sb.WriteString("Given the trigger and your constitution, what should you do? ")
	sb.WriteString("Return candidates as JSON. If no action is warranted, return an empty candidates array.\n")
	return sb.String()
}

// buildReflectUserPrompt constructs the user message for the REFLECT LLM call.
func buildReflectUserPrompt(c *Constitution, result *bot.BotExecutionResult) string {
	var sb strings.Builder
	sb.WriteString("# execution outcome\n\n")
	if result == nil {
		sb.WriteString("(no result — execution returned nil)\n")
	} else {
		sb.WriteString(fmt.Sprintf("**success:** %v\n", result.Success))
		sb.WriteString(fmt.Sprintf("**tokens used:** %d\n", result.TokensUsed))
		sb.WriteString(fmt.Sprintf("**duration:** %s\n", result.Duration))
		if result.Error != "" {
			sb.WriteString("**error:** " + result.Error + "\n")
		}
		if result.Output != "" {
			sb.WriteString("\n## output\n\n")
			sb.WriteString(truncate(result.Output, 4000))
			sb.WriteString("\n")
		}
	}

	if c != nil {
		sb.WriteString("\n# constitution reminder\n\n")
		sb.WriteString("**purpose:** " + c.Purpose + "\n")
		if len(c.Constraints.Never) > 0 {
			sb.WriteString("## prohibitions\n")
			for _, n := range c.Constraints.Never {
				sb.WriteString("- " + n + "\n")
			}
		}
	}

	sb.WriteString("\n# task\n\n")
	sb.WriteString("Assess the goal health based on this execution outcome. ")
	sb.WriteString("Return JSON with health (healthy/at_risk/broken) and reasoning.\n")
	return sb.String()
}

// ---------------------------------------------------------------------------
// Response parsers
// ---------------------------------------------------------------------------

// assessLLMResponse is the JSON schema expected from the ASSESS LLM call.
type assessLLMResponse struct {
	Candidates []CandidatePlan `json:"candidates"`
}

// parseAssessResponse parses the LLM's JSON response from ASSESS. Returns
// (nil, error) on unparseable output — the caller handles the tier-1 fallback.
func parseAssessResponse(content string) ([]CandidatePlan, error) {
	content = strings.TrimSpace(content)
	// Strip markdown code fences if present.
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed assessLLMResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return nil, fmt.Errorf("parse assess response: %w", err)
	}
	// Defensive copy of candidates (avoid aliasing caller-visible slice).
	if len(parsed.Candidates) == 0 {
		return []CandidatePlan{}, nil
	}
	out := make([]CandidatePlan, len(parsed.Candidates))
	copy(out, parsed.Candidates)
	return out, nil
}

// reflectLLMResponse is the JSON schema expected from the REFLECT LLM call.
type reflectLLMResponse struct {
	Health    string `json:"health"`
	Reasoning string `json:"reasoning"`
}

// parseReflectResponse parses the LLM's JSON response from REFLECT into a
// GoalHealth value.
func parseReflectResponse(content string) (GoalHealth, error) {
	content = strings.TrimSpace(content)
	content = strings.TrimPrefix(content, "```json")
	content = strings.TrimPrefix(content, "```")
	content = strings.TrimSuffix(content, "```")
	content = strings.TrimSpace(content)

	var parsed reflectLLMResponse
	if err := json.Unmarshal([]byte(content), &parsed); err != nil {
		return GoalUnknown, fmt.Errorf("parse reflect response: %w", err)
	}
	health, err := ParseGoalHealth(parsed.Health)
	if err != nil {
		return GoalUnknown, fmt.Errorf("invalid health %q: %w", parsed.Health, err)
	}
	return health, nil
}

// ---------------------------------------------------------------------------
// Future-work notes
// ---------------------------------------------------------------------------

// Approval timeout (spec line 592): PlanCreator.CreatePlan accepts a context,
// so a context-based timeout can be applied at the call site. However, the
// approval_timeout semantics (auto-reject after N days with no human signoff)
// require a background sweeper job that checks plan age against the configured
// timeout. That sweeper is out of scope for Phase 3 — it belongs in the Plan
// signoff workflow itself (internal/plan). When implemented, the sweeper
// should:
//   1. Query plans in PendingApproval older than approval_timeout.
//   2. Call PlanManager.CancelPlan(ctx, planID, "approval timeout").
//   3. Mark the associated goal as at_risk via GoalStore.Update.
//   4. Write an audit finding at SeverityWarning with checkpoint=pre_exec.
