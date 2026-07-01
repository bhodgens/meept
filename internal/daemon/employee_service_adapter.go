package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/comm/http"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/plan"
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
func (a employeeServiceAdapter) ApprovePlan(ctx context.Context, goalID, planID, reason string) error {
	return a.m.ApprovePlan(ctx, goalID, planID, reason)
}

// RejectPlan wraps Manager.RejectPlan.
func (a employeeServiceAdapter) RejectPlan(ctx context.Context, goalID, planID, reason string) error {
	return a.m.RejectPlan(ctx, goalID, planID, reason)
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
// Token counts from RunOnce are not directly available (RunOnce returns
// only the response string); we return 0 for tokensUsed. The per-turn
// cost is tracked separately by the LLM client's token cache. This keeps
// the GoalLoop executor path functional without duplicating the metrics
// infrastructure.
//
// See docs/superpowers/specs/2026-06-23-ai-employee-design.md spec line
// 304: "The LLM call inside ASSESS uses the existing AgentLoop.RunOnce() —
// no new inference path."
type agentLoopBotExecutorAdapter struct {
	agentLoop *agent.AgentLoop
	botMgr    *bot.Manager
	logger    *slog.Logger
}

// ExecuteBot runs a single turn through the agent loop. The systemPrompt
// is currently logged but not passed to RunOnce (AgentLoop constructs its
// own system prompts internally). The userMessage becomes the user turn.
// Returns (output, tokensUsed, err).
func (a *agentLoopBotExecutorAdapter) ExecuteBot(ctx context.Context, systemPrompt, userMessage string) (string, int, error) {
	if a.agentLoop == nil {
		return "", 0, fmt.Errorf("agent loop not configured")
	}
	// Use a conversation ID scoped to the bot executor so sessions don't
	// collide with user-driven conversations. The agent loop may override
	// this internally.
	conversationID := fmt.Sprintf("bot-exec-%d", time.Now().UnixNano())
	response, err := a.agentLoop.RunOnce(ctx, userMessage, conversationID)
	if err != nil {
		a.logger.Warn("agent loop execute failed",
			"error", err,
			"conversation_id", conversationID)
		return "", 0, err
	}
	return response, 0, nil
}

// Compile-time guard: agentLoopBotExecutorAdapter must satisfy
// bot.BotExecutor.
var _ bot.BotExecutor = (*agentLoopBotExecutorAdapter)(nil)

// planCreatorAdapter wraps *plan.PlanManager to satisfy
// employee.PlanCreator. The GoalLoop calls CreatePlan to route tier-2
// candidates through the existing Plan signoff workflow. The adapter
// translates between the two CreatePlan signatures (employee.PlanCreator
// has fewer params — projectPath defaults to "").
type planCreatorAdapter struct {
	planMgr *plan.PlanManager
}

// CreatePlan delegates to PlanManager.CreatePlan, translating the
// employee.PlanCreator signature. projectPath defaults to "" (the plan
// manager resolves it from the project ID). Returns a PlanRef with the
// plan's ID, state, and empty prompt (the GoalLoop fills in the prompt).
func (a *planCreatorAdapter) CreatePlan(ctx context.Context, title, description, projectID, sessionID string) (employee.PlanRef, error) {
	if a.planMgr == nil {
		return employee.PlanRef{}, fmt.Errorf("plan manager not configured")
	}
	p, err := a.planMgr.CreatePlan(ctx, title, description, projectID, "", sessionID)
	if err != nil {
		return employee.PlanRef{}, err
	}
	return employee.PlanRef{
		ID:    p.ID,
		State: string(p.State),
	}, nil
}

// Compile-time guard: planCreatorAdapter must satisfy employee.PlanCreator.
var _ employee.PlanCreator = (*planCreatorAdapter)(nil)

// storeBackedGoalLookup wraps *employee.GoalStore to satisfy
// employee.GoalLookup. The GoalLoop uses this to find the active goal
// for an employee during Reflect (spec line 296: "Updates Goal.Health").
type storeBackedGoalLookup struct {
	store *employee.GoalStore
}

// ActiveGoal returns the first active goal for the employee from the
// store. Returns (nil, nil) when no active goal exists.
func (l *storeBackedGoalLookup) ActiveGoal(ctx context.Context, employeeID string) (*employee.Goal, error) {
	if l.store == nil {
		return nil, nil
	}
	goals, err := l.store.ListActive(ctx, employeeID)
	if err != nil {
		return nil, err
	}
	if len(goals) == 0 {
		return nil, nil
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
func (a pushNotifierAdapter) Publish(event interface{}) {
	ne, ok := event.(*http.NotificationEvent)
	if !ok {
		// Best-effort: drop unknown types silently rather than panicking.
		return
	}
	a.emitter.Publish(ne)
}

// PublishNotification forwards the arguments to EventEmitter.PublishNotification,
// translating the interface{} notifType to NotificationType.
func (a pushNotifierAdapter) PublishNotification(sessionID, agentID string, notifType interface{}, title, message string) {
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
