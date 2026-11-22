package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"math"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/pkg/models"
)

// employeeServiceAdapter wraps *employee.Manager to satisfy the
// services.EmployeeManager interface. The adapter bridges the concrete
// *employee.Manager signatures (which return typed structs) to the
// any-typed interface methods. This is the composition point that
// avoids an import cycle: services never imports employee.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md (Phase 7,
// "Service layer").
type employeeServiceAdapter struct {
	m *employee.Manager
}

// Compile-time guard: employeeServiceAdapter must satisfy
// services.EmployeeManager. The blank assignment is intentionally
// omitted to avoid a daemon -> services cycle in the test; the daemon
// already imports services in daemon.go.

// ListEmployees wraps Manager.ListEmployees, converting []Employee to []any.
func (a employeeServiceAdapter) ListEmployees(ctx context.Context, statusFilter string) ([]any, error) {
	emps, err := a.m.ListEmployees(ctx, statusFilter)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(emps))
	for i, e := range emps {
		out[i] = e
	}
	return out, nil
}

// GetEmployee wraps Manager.GetEmployee, converting *Employee to any.
func (a employeeServiceAdapter) GetEmployee(ctx context.Context, id string) (any, error) {
	return a.m.GetEmployee(ctx, id)
}

// Hire wraps Manager.Hire, type-asserting req to employee.HireRequest.
func (a employeeServiceAdapter) Hire(ctx context.Context, req any) (any, error) {
	hireReq, ok := req.(employee.HireRequest)
	if !ok {
		return nil, fmt.Errorf("hire: expected employee.HireRequest, got %T", req)
	}
	return a.m.Hire(ctx, hireReq)
}

// UpdateEmployee wraps Manager.UpdateEmployee, type-asserting req to
// employee.UpdateRequest.
func (a employeeServiceAdapter) UpdateEmployee(ctx context.Context, req any) (any, error) {
	updateReq, ok := req.(employee.UpdateRequest)
	if !ok {
		return nil, fmt.Errorf("update: expected employee.UpdateRequest, got %T", req)
	}
	return a.m.UpdateEmployee(ctx, updateReq)
}

// Retire wraps Manager.Retire.
func (a employeeServiceAdapter) Retire(ctx context.Context, id string) error {
	return a.m.Retire(ctx, id)
}

// Pause wraps Manager.Pause.
func (a employeeServiceAdapter) Pause(ctx context.Context, id string) error {
	return a.m.Pause(ctx, id)
}

// Resume wraps Manager.Resume.
func (a employeeServiceAdapter) Resume(ctx context.Context, id string) error {
	return a.m.Resume(ctx, id)
}

// Trigger wraps Manager.Trigger, converting *TriggerResult to any.
func (a employeeServiceAdapter) Trigger(ctx context.Context, id string, payload map[string]any) (any, error) {
	return a.m.Trigger(ctx, id, payload)
}

// AmendConstitution wraps Manager.AmendConstitution, building an
// employee.AmendRequest from the individual arguments.
func (a employeeServiceAdapter) AmendConstitution(ctx context.Context, employeeID string, fields map[string]any, reason string) (string, error) {
	req := employee.AmendRequest{
		EmployeeID: employeeID,
		Fields:     fields,
		Reason:     reason,
		By:         "user", // The service layer is always called on behalf of the operator.
	}
	return a.m.AmendConstitution(ctx, req)
}

// ListGoals wraps Manager.ListGoals, converting []*Goal to []any.
func (a employeeServiceAdapter) ListGoals(ctx context.Context, employeeID string) ([]any, error) {
	goals, err := a.m.ListGoals(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(goals))
	for i, g := range goals {
		out[i] = g
	}
	return out, nil
}

// GetGoal wraps Manager.GetGoal, converting *Goal to any.
func (a employeeServiceAdapter) GetGoal(ctx context.Context, id string) (any, error) {
	return a.m.GetGoal(ctx, id)
}

// ApprovePlan wraps Manager.ApprovePlan.
func (a employeeServiceAdapter) ApprovePlan(ctx context.Context, goalID, planID, callerID, reason string) error {
	return a.m.ApprovePlan(ctx, goalID, planID, callerID, reason)
}

// RejectPlan wraps Manager.RejectPlan.
func (a employeeServiceAdapter) RejectPlan(ctx context.Context, goalID, planID, callerID, reason string) error {
	return a.m.RejectPlan(ctx, goalID, planID, callerID, reason)
}

// ListAuditFindings wraps Manager.ListAuditFindings, building an
// employee.AuditQuery from the individual arguments and converting
// []AuditFinding to []any.
func (a employeeServiceAdapter) ListAuditFindings(ctx context.Context, employeeID string, since time.Duration, severity string) ([]any, error) {
	q := employee.AuditQuery{
		EmployeeID: employeeID,
		Since:      since,
		Severity:   severity,
	}
	findings, err := a.m.ListAuditFindings(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(findings))
	for i, f := range findings {
		out[i] = f
	}
	return out, nil
}

// ResolveAuditFinding wraps Manager.ResolveAuditFinding.
func (a employeeServiceAdapter) ResolveAuditFinding(ctx context.Context, findingID, resolution, note string) error {
	return a.m.ResolveAuditFinding(ctx, findingID, resolution, note)
}

// Migrate wraps Manager.Migrate, converting []MigrationProposal to []any.
func (a employeeServiceAdapter) Migrate(ctx context.Context) ([]any, error) {
	proposals, err := a.m.Migrate(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]any, len(proposals))
	for i, p := range proposals {
		out[i] = p
	}
	return out, nil
}

// ApplyMigration wraps Manager.ApplyMigration, converting
// *MigrationApplyResult to any.
func (a employeeServiceAdapter) ApplyMigration(ctx context.Context, botID string) (any, error) {
	return a.m.ApplyMigration(ctx, botID)
}

// employeeBusPublisher adapts *bus.MessageBus to the employee.BusPublisher
// interface. This avoids the employee package importing internal/bus
// (cycle risk). The daemon injects this adapter via SetBusPublisher
// during NewComponents.
type employeeBusPublisher struct {
	bus    *bus.MessageBus
	logger *slog.Logger
}

// PublishEmployeePaused publishes an employee.paused bus event (spec line 383).
// The event payload includes the employee ID, reason, and source ("operator"
// or "auto_pause"). Best-effort: errors are logged, not returned.
func (p employeeBusPublisher) PublishEmployeePaused(employeeID, reason, source string) {
	if p.bus == nil {
		return
	}
	payload := employee.EmployeePausedEvent{
		EmployeeID: employeeID,
		Reason:     reason,
		Source:     source,
	}
	msg, err := models.NewBusMessage(
		models.MessageType("employee.paused"),
		"employee-manager",
		payload,
	)
	if err != nil {
		p.logger.Warn("employee.paused bus event: marshal failed",
			"employee_id", employeeID, "error", err)
		return
	}
	// Set the topic explicitly since NewBusMessage doesn't set it.
	msg.Topic = "employee.paused"
	p.bus.Publish("employee.paused", msg)
}

// PublishCriticalFinding publishes an employee.critical_finding bus event (E4).
// PostTurnAuditor emits this when it finds a critical finding. The Manager
// subscribes and calls Pause on receipt, decoupling the auditor from the
// lifecycle. Best-effort: errors are logged, not returned.
func (p employeeBusPublisher) PublishCriticalFinding(employeeID, findingID, violatedRule, evidence string) {
	if p.bus == nil {
		return
	}
	payload := employee.CriticalFindingEvent{
		EmployeeID:   employeeID,
		FindingID:    findingID,
		ViolatedRule: violatedRule,
		Evidence:     evidence,
	}
	msg, err := models.NewBusMessage(
		models.MessageType("employee.critical_finding"),
		"employee-auditor",
		payload,
	)
	if err != nil {
		p.logger.Warn("employee.critical_finding bus event: marshal failed",
			"employee_id", employeeID, "error", err)
		return
	}
	msg.Topic = "employee.critical_finding"
	p.bus.Publish("employee.critical_finding", msg)
}

// PublishConstitutionValidationError publishes an
// employee.constitution_validation_error bus event (H5). Emitted when a
// constitution fails validation at hire or load time. The event carries the
// employee ID, the validation error, and a summary of the invalid
// constitution for diagnostic purposes. Best-effort: errors are logged, not
// returned.
func (p employeeBusPublisher) PublishConstitutionValidationError(employeeID, validationError, constitutionSummary string) {
	if p.bus == nil {
		return
	}
	payload := employee.ConstitutionValidationErrorEvent{
		EmployeeID:          employeeID,
		ValidationError:     validationError,
		ConstitutionSummary: constitutionSummary,
	}
	msg, err := models.NewBusMessage(
		models.MessageType("employee.constitution_validation_error"),
		"employee-manager",
		payload,
	)
	if err != nil {
		p.logger.Warn("employee.constitution_validation_error bus event: marshal failed",
			"employee_id", employeeID, "error", err)
		return
	}
	msg.Topic = "employee.constitution_validation_error"
	p.bus.Publish("employee.constitution_validation_error", msg)
}

// bot.BotExecutor. The GoalLoop calls ExecuteBot(ctx, systemPrompt,
// userMessage) to run a single LLM turn. We delegate to AgentLoop.RunOnce
// which processes a single user message through the full reasoning loop.
//
// systemPrompt is armed as a one-shot turn prefix (routing-funnel C2)
// and cleared after that RunOnce. It is not written onto the loop's
// permanent system prompt. tokensUsed is LastTurnTokens() (C3): the
// provider TotalTokens from the turn that just finished, or 0 when the
// provider sent no usage. RunOnce's signature stays (string, error).
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md spec line
// 304: "The LLM call inside ASSESS uses the existing AgentLoop.RunOnce() —
// no new inference path."
type agentLoopBotExecutorAdapter struct {
	agentLoop *agent.AgentLoop
	botMgr    *bot.Manager
	logger    *slog.Logger
	// employeeID is set only when this adapter is bound to one employee.
	// The shared template adapter leaves it empty; employee turns run on
	// employeeBoundAgentLoopExecutor, which owns the standing session id.
	employeeID string
	// sessions is the existing session store. Nil is a no-op (gate tests).
	sessions session.Store
}

// ExecuteBot runs a single turn through the agent loop. systemPrompt is
// armed on this receiver for the next RunOnce only (C2), then cleared.
// The userMessage becomes the user turn. tokensUsed is the provider
// TotalTokens from that turn (C3), not a hardcoded 0.
// Returns (output, tokensUsed, err).
func (a *agentLoopBotExecutorAdapter) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	if a.agentLoop == nil {
		return "", 0, fmt.Errorf("agent loop not configured")
	}
	if a.employeeID == "" {
		return "", 0, fmt.Errorf("agent loop executor: standing session requires an employee id")
	}
	conversationID := employee.EmployeeChatSessionID(a.employeeID)
	if err := ensureEmployeeChatSession(a.sessions, a.employeeID); err != nil {
		warnEmployeeSession(a.logger, "standing employee session ensure failed",
			"employee_id", a.employeeID,
			"session_id", conversationID,
			"error", err)
	}
	a.agentLoop.SetTurnSystemPrefix(systemPrompt)
	response, err := a.agentLoop.RunOnce(ctx, userMessage, conversationID)
	tokens := a.agentLoop.LastTurnTokens()
	persistEmployeeExchange(a.sessions, conversationID, a.employeeID, userMessage, response, a.logger)
	if err != nil {
		warnEmployeeSession(a.logger, "agent loop execute failed",
			"error", err,
			"conversation_id", conversationID)
		return "", tokens, err
	}
	return response, tokens, nil
}

// Compile-time guard: agentLoopBotExecutorAdapter must satisfy
// bot.BotExecutor.
var _ bot.BotExecutor = (*agentLoopBotExecutorAdapter)(nil)

// ExecutionCostCents reports the just-finished turn's provider dollar cost
// (LastTurnCostUSD, routing-funnel C3) rounded to whole cents, so the
// goal-loop budget lane can forward it into RecordRoundSpend. Unseen or
// unconfigured cost reports 0; a negative value clamps to 0.
func (a *agentLoopBotExecutorAdapter) ExecutionCostCents() int {
	return dollarsToExecutionCents(a.agentLoop.LastTurnCostUSD())
}

// dollarsToExecutionCents converts a provider dollar cost into whole cents
// with round-half-away-from-zero semantics; negatives clamp to 0.
func dollarsToExecutionCents(costUSD float64) int {
	if costUSD <= 0 {
		return 0
	}
	return int(math.Round(costUSD * 100))
}

// employeeBoundAgentLoopExecutor wraps the shared agentLoopBotExecutorAdapter
// for ONE employee (goal-loop / non-voice lane, bug 1). The plain adapter
// runs every employee's ExecuteBot directly on the shared daemon loop, so
// the model saw the full template tool list regardless of the employee's
// constitution. This wrapper runs each turn on a throwaway per-turn clone
// (employeeTurnClone) whose registry carries the SAME visibility rule the
// voice lane applies (employeeVisibleTools), with the employee id stamped
// so the call-time PreExecChecker gate routes. The shared template loop is
// never mutated.
//
// The acp lane is deliberately NOT wrapped here: its loop is a registry
// agent's loop whose tool registry is already spec-scoped by
// AgentRegistry.filterTools at loop creation (constitution intersection
// rides the groups resolution), and the executor: acp branch never sends
// meept tool definitions to a model (the harness owns its own tools).
type employeeBoundAgentLoopExecutor struct {
	inner        *agentLoopBotExecutorAdapter
	employeeID   string
	constitution employee.Constitution
	// sessions is the existing session store, copied from the shared
	// adapter at wrap time. Nil is a no-op.
	sessions session.Store
	// cloneHook is a TEST-ONLY seam: when non-nil, ExecuteBot calls it
	// with the per-turn clone before running the turn, so a pin can
	// inspect the registry view the turn actually ran with. Production
	// wiring never sets it, so the production path writes no shared
	// state (a bare lastClone field was unguarded on a path that can
	// in principle run concurrent rounds).
	cloneHook func(*agent.AgentLoop)
	// lastCostUSD is the just-finished turn's provider dollar cost,
	// captured in ExecuteBot for ExecutionCostCents (goal-loop budget
	// lane). Rounds on one executor are sequential, so a plain field
	// needs no extra synchronization.
	lastCostUSD float64
}

// ExecuteBot runs one employee turn on a filtered per-turn clone.
// systemPrompt is armed on the clone (never the shared template's
// permanent prompt) for this RunOnce only. tokensUsed is the clone's
// LastTurnTokens. A clone that cannot be built (nil shared loop) fails
// loudly.
func (a *employeeBoundAgentLoopExecutor) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	if a.inner == nil || a.inner.agentLoop == nil {
		if a.inner != nil {
			return a.inner.ExecuteBot(ctx, systemPrompt, userMessage) // fails with "agent loop not configured"
		}
		return "", 0, fmt.Errorf("employee-bound executor %q: inner adapter not configured", a.employeeID)
	}
	c := a.constitution
	conversationID := employee.EmployeeChatSessionID(a.employeeID)
	sessions := a.sessions
	if sessions == nil && a.inner != nil {
		sessions = a.inner.sessions
	}
	if err := ensureEmployeeChatSession(sessions, a.employeeID); err != nil {
		warnEmployeeSession(a.inner.logger, "standing employee session ensure failed",
			"employee_id", a.employeeID,
			"session_id", conversationID,
			"error", err)
	}
	clone := employeeTurnClone(a.inner.agentLoop, conversationID, a.employeeID, &c)
	if clone == nil {
		return "", 0, fmt.Errorf("employee-bound executor %q: per-turn clone failed", a.employeeID)
	}
	if a.cloneHook != nil {
		a.cloneHook(clone)
	}
	// C2: prefix the clone's next RunOnce only. The template loop's
	// permanent system prompt is not written.
	clone.SetTurnSystemPrefix(systemPrompt)
	response, err := clone.RunOnce(ctx, userMessage, conversationID)
	tokens := clone.LastTurnTokens()
	// Goal-loop budget lane: capture the turn's provider dollar cost so
	// ExecutionCostCents reports it after the round. Unseen cost = 0.
	a.lastCostUSD = clone.LastTurnCostUSD()
	persistEmployeeExchange(sessions, conversationID, a.employeeID, userMessage, response, a.inner.logger)
	clone.Close()
	if err != nil {
		a.inner.logger.Warn("employee-bound agent loop execute failed",
			"employee_id", a.employeeID,
			"error", err,
			"conversation_id", conversationID)
		return "", tokens, err
	}
	return response, tokens, nil
}

// Compile-time guard: employeeBoundAgentLoopExecutor must satisfy
// bot.BotExecutor.
var _ bot.BotExecutor = (*employeeBoundAgentLoopExecutor)(nil)

// ExecutionCostCents reports the per-turn clone's provider dollar cost
// (LastTurnCostUSD) rounded to whole cents, mirroring the inner adapter —
// the goal-loop budget lane reads the executor that ran the round, and
// that is the clone. Unseen or unconfigured cost reports 0. The value is
// captured in ExecuteBot; goal-loop rounds on one executor are sequential
// (ExecuteBot returns before the round's RecordRoundSpend), so a plain
// field needs no extra synchronization.
func (a *employeeBoundAgentLoopExecutor) ExecutionCostCents() int {
	return dollarsToExecutionCents(a.lastCostUSD)
}

// planCreatorAdapter wraps *plan.PlanManager to satisfy
// employee.PlanCreator. The GoalLoop calls CreatePlan to route tier-2
// candidates through the existing Plan signoff workflow. The adapter
// translates between the two CreatePlan signatures (employee.PlanCreator
// has fewer params — projectPath defaults to "").
type planCreatorAdapter struct {
	planMgr *plan.PlanManager
	logger  *slog.Logger
}

// newPlanCreatorAdapter builds the adapter; nil logger falls back to
// slog.Default() so the submit-refusal WARN path is never a nil deref.
func newPlanCreatorAdapter(planMgr *plan.PlanManager, logger *slog.Logger) *planCreatorAdapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &planCreatorAdapter{planMgr: planMgr, logger: logger}
}

// CreatePlan delegates to PlanManager.CreatePlan, translating the
// employee.PlanCreator signature. projectPath defaults to "" (the plan
// manager resolves it from the project ID). Returns a PlanRef with the
// plan's ID, state, and empty prompt (the GoalLoop fills in the prompt).
// CreatePlan creates the plan through the PlanManager, persisting the goal
// linkage ATOMICALLY when a goal is known (CreatePlanWithGoal) instead of the
// old create-then-SetPlanGoal patch write: a failure between the two writes
// silently dropped the linkage (bughunt LOW). The optional
// employee.PlanGoalSetter contract remains satisfied for callers that still
// stamp a goal after creation.
func (a *planCreatorAdapter) CreatePlan(ctx context.Context, title, description, projectID, sessionID string) (employee.PlanRef, error) {
	return a.CreatePlanWithGoal(ctx, title, description, projectID, sessionID, "")
}

// CreatePlanWithGoal is CreatePlan with the owning goal id, persisted in the
// same INSERT as the plan row (employee.PlanCreatorWithGoal contract).
//
// Plan 20261113-escalation-resume leaf 06: the GoalLoop's contract says the
// plan enters pending_approval for signoff (goal_loop.go Plan: "The plan
// enters PendingApproval state"), but PlanManager.CreatePlanWithGoal stops
// at StateDraft — SubmitPlan's only production caller was the plan-mode
// tool. A draft plan can NEVER be approved (ApprovePlan CASes
// pending_approval->approved), which stranded every goal-loop plan and made
// the escalation-resume path dead in production. The adapter therefore
// submits right after creation.
//
// SubmitPlan auto-approve (internal/plan/manager.go): when
// plans.approval.require_approval is FALSE the submit immediately approves
// (empty session, by="system") and fires plan.approved — creation still
// succeeds and the resume pump handles the rest. When TRUE (production
// default, config/meept.json5) the plan parks at pending_approval and the
// plan.submitting event reaches the gate-park pump (gate_park_wiring.go):
// a goal-loop session id (gloop_*) with no armed gate is harmlessly
// checkpointed or skipped, and approve flows normally.
func (a *planCreatorAdapter) CreatePlanWithGoal(ctx context.Context, title, description, projectID, sessionID, goalID string) (employee.PlanRef, error) {
	if a.planMgr == nil {
		return employee.PlanRef{}, fmt.Errorf("plan manager not configured")
	}
	p, err := a.planMgr.CreatePlanWithGoal(ctx, title, description, projectID, "", sessionID, goalID)
	if err != nil {
		return employee.PlanRef{}, err
	}
	// Submit draft -> pending_approval so the signoff workflow can reach
	// it. A refused submit (state rules, auto-approve failure) is a WARN:
	// creation itself succeeded, and the caller must still get the plan
	// ref — failing here would strand the goal round with no artifact.
	if err := a.planMgr.SubmitPlan(ctx, p.ID); err != nil {
		a.logger.Warn("goal-loop plan submit refused; plan stays in draft",
			"plan_id", p.ID, "goal_id", p.GoalID, "error", err)
	} else {
		// Re-read so the returned ref carries the post-submit state
		// (pending_approval, or approved under auto-approve).
		if fresh, getErr := a.planMgr.GetPlan(ctx, p.ID); getErr == nil {
			p = fresh
		}
	}
	return employee.PlanRef{
		ID:     p.ID,
		State:  string(p.State),
		GoalID: p.GoalID,
	}, nil
}

// SetPlanGoal persists the goal linkage on the stored plan row
// (goal-completion leaf 02, C6): the GoalLoop stamps Plan.GoalID right after
// CreatePlan returns, so every goal-loop plan traces to its owning goal.
// Compile-time guard below keeps the adapter in sync with the optional
// employee.PlanGoalSetter interface.
func (a *planCreatorAdapter) SetPlanGoal(ctx context.Context, planID, goalID string) error {
	if a.planMgr == nil {
		return fmt.Errorf("plan manager not configured")
	}
	return a.planMgr.SetPlanGoal(ctx, planID, goalID)
}

// SetPlanPrompt persists the candidate goal prompt on the stored plan row
// (issue #148 resume-prompt-loss, employee.PlanPromptSetter contract): the
// GoalLoop stamps Plan.Prompt right after CreatePlan returns so a resumed
// ApproveAndExecute — which carries only PlanRef{ID} — rehydrates the exact
// execution prompt through planBriefLookup instead of falling back to a
// generic mandate.
func (a *planCreatorAdapter) SetPlanPrompt(ctx context.Context, planID, prompt string) error {
	if a.planMgr == nil {
		return fmt.Errorf("plan manager not configured")
	}
	return a.planMgr.SetPlanPrompt(ctx, planID, prompt)
}

// Compile-time guard: planCreatorAdapter must satisfy employee.PlanGoalSetter.
var _ employee.PlanGoalSetter = (*planCreatorAdapter)(nil)

// Compile-time guard: planCreatorAdapter must satisfy
// employee.PlanPromptSetter (issue #148).
var _ employee.PlanPromptSetter = (*planCreatorAdapter)(nil)

// Compile-time guard: planCreatorAdapter must satisfy employee.PlanCreator.
var _ employee.PlanCreator = (*planCreatorAdapter)(nil)

// storeBackedGoalLookup wraps *employee.GoalStore to satisfy
// employee.GoalLookup. The GoalLoop uses this to find the active goal
// for an employee during Reflect (spec line 296: "Updates Goal.Health").
type storeBackedGoalLookup struct {
	store *employee.GoalStore
}

// ActiveGoal returns the first active goal for the employee from the store.
// (nil, nil) is the documented "no active goal" answer — the GoalLoop treats
// it as nothing-to-reflect-on, not a failure.
func (l *storeBackedGoalLookup) ActiveGoal(ctx context.Context, employeeID string) (*employee.Goal, error) {
	if l.store == nil {
		return nil, nil //nolint:nilnil // absent store is the documented "no active goal" contract, not an error
	}
	goals, err := l.store.ListActive(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	if len(goals) == 0 {
		return nil, nil //nolint:nilnil // "no active goal exists" is an intentional empty-result contract, not an error
	}
	return goals[0], nil
}

// Compile-time guard: storeBackedGoalLookup must satisfy
// employee.GoalLookup.
var _ employee.GoalLookup = (*storeBackedGoalLookup)(nil)

// planDisposerAdapter wraps *plan.PlanManager to satisfy
// employee.PlanDisposer without creating an import cycle. The employee
// Manager calls ApprovePlan/RejectPlan on this adapter, which delegates
// to the existing plan.PlanManager signoff workflow.
type planDisposerAdapter struct {
	pm *plan.PlanManager
}

// ApprovePlan delegates to PlanManager.ApprovePlan (spec lines 294-306).
func (a *planDisposerAdapter) ApprovePlan(ctx context.Context, planID, sessionID, by string) error {
	return a.pm.ApprovePlan(ctx, planID, sessionID, by)
}

// RejectPlan delegates to PlanManager.RejectPlan (spec lines 294-306).
func (a *planDisposerAdapter) RejectPlan(ctx context.Context, planID, sessionID, by, reason string) error {
	return a.pm.RejectPlan(ctx, planID, sessionID, by, reason)
}

// Compile-time guard: planDisposerAdapter must satisfy
// employee.PlanDisposer.
var _ employee.PlanDisposer = (*planDisposerAdapter)(nil)

// planBriefLookup adapts PlanManager.GetPlan to employee.PlanBriefLookup so
// a resumed execution (PlanRef carries only the id) can show the employee
// the approved plan's title and description. A nil manager returns nil so
// the caller does not install a lookup — a nil *PlanManager must not be
// closed over by a non-nil func (typed-nil: the first GetPlan would panic).
func planBriefLookup(pm *plan.PlanManager) employee.PlanBriefLookup {
	if pm == nil {
		return nil
	}
	return func(ctx context.Context, planID string) (employee.PlanBrief, bool, error) {
		p, err := pm.GetPlan(ctx, planID)
		if err != nil {
			return employee.PlanBrief{}, false, err
		}
		if p == nil {
			return employee.PlanBrief{}, false, nil
		}
		return employee.PlanBrief{Title: p.Title, Description: p.Description, Prompt: p.Prompt}, true, nil
	}
}

// planLookupAdapter wraps *plan.PlanManager to satisfy employee.PlanLookup.
// Used by the approval-timeout sweeper to determine when a plan was created.
type planLookupAdapter struct {
	pm *plan.PlanManager
}

// PlanCreatedAt returns the creation timestamp of the plan with the given ID.
func (a *planLookupAdapter) PlanCreatedAt(ctx context.Context, planID string) (time.Time, error) {
	p, err := a.pm.GetPlan(ctx, planID)
	if err != nil {
		return time.Time{}, err
	}
	return p.CreatedAt, nil
}

// Compile-time guard: planLookupAdapter must satisfy employee.PlanLookup.
var _ employee.PlanLookup = (*planLookupAdapter)(nil)

// planStateLookupAdapter adapts *plan.PlanManager to
// employee.PlanStateLookup (a plan's current signoff state as a string).
// Same adapter shape as planLookupAdapter: the employee package cannot
// import internal/plan (cycle risk). Used by the approval-timeout sweeper
// to filter ActivePlanIDs entries to plans actually pending approval
// before auto-rejecting (an entry can be an already-executing plan).
type planStateLookupAdapter struct {
	pm *plan.PlanManager
}

// PlanState returns the current signoff state of the plan with the given
// ID (e.g. "pending_approval", "approved", "executing").
func (a *planStateLookupAdapter) PlanState(ctx context.Context, planID string) (string, error) {
	p, err := a.pm.GetPlan(ctx, planID)
	if err != nil {
		return "", err
	}
	return string(p.State), nil
}

// Compile-time guard: planStateLookupAdapter must satisfy
// employee.PlanStateLookup.
var _ employee.PlanStateLookup = (*planStateLookupAdapter)(nil)

// pushNotifierAdapter bridges *EventEmitter to the services.notifier
// interface. The services package defines notifier with interface{} params
// to avoid importing comm/http or daemon. EventEmitter.Publish takes a
// concrete *http.NotificationEvent and PublishNotification takes a concrete
// NotificationType, so *EventEmitter does not satisfy notifier directly.
// This adapter performs the type translation.
type pushNotifierAdapter struct {
	emitter *EventEmitter
}

// Publish forwards an arbitrary event to the emitter. The event must be
// *http.NotificationEvent or a type convertible to it; other types are
// dropped with a warning log.
func (a pushNotifierAdapter) Publish(event any) {
	ne, ok := event.(*http.NotificationEvent)
	if !ok {
		// Best-effort: drop unknown types silently rather than panicking.
		return
	}
	a.emitter.Publish(ne)
}

// PublishNotification forwards the arguments to EventEmitter.PublishNotification,
// translating the interface{} notifType to NotificationType.
func (a pushNotifierAdapter) PublishNotification(sessionID, agentID string, notifType any, title, message string) {
	var nt NotificationType
	switch v := notifType.(type) {
	case NotificationType:
		nt = v
	case string:
		nt = NotificationType(v)
	case nil:
		nt = NotificationTypeInfo
	default:
		nt = NotificationTypeInfo
	}
	a.emitter.PublishNotification(sessionID, agentID, nt, title, message)
}
