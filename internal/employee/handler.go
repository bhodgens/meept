// Package employee provides the AI employee framework: constitution-bound
// persistent agents with goal loops, enforcement, and audit.
//
// This file implements the RPC handler layer (Phase 6 of the AI Employee
// Design spec). The RPCHandler exposes employee.* methods over JSON-RPC,
// replacing the legacy bot.* namespace (hard cutover per spec line 529).
package employee

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/bot"
)

// errNotConfigured is returned when the Manager is nil (employees not wired).
var errNotConfigured = errors.New("employees not configured")

// amend frozen-field set consulted by the handler when a request omits
// amendment metadata. The Manager performs authoritative enforcement; this is
// a defensive pre-check so callers get a fast error.

// RPCHandler provides JSON-RPC handlers for employee management under the
// agents.* namespace. It wraps an employee.Manager and exposes the methods
// listed in the AI Employee Design spec (lines 532-540).
//
// All handlers gracefully handle a nil Manager by returning errNotConfigured,
// so a daemon that has not wired the employee subsystem returns clear errors
// instead of panicking.
type RPCHandler struct {
	manager *Manager
	logger  *slog.Logger
}

// NewRPCHandler creates a new RPC handler for employee operations.
//
// manager may be nil during a partial rollout; handlers will return
// errNotConfigured in that case.
func NewRPCHandler(manager *Manager) *RPCHandler {
	return &RPCHandler{
		manager: manager,
		logger:  slog.Default(),
	}
}

// SetLogger replaces the default logger. Nil values are ignored to prevent
// typed-nil panics (per CLAUDE.md setter nil guard rule).
func (h *RPCHandler) SetLogger(l *slog.Logger) {
	if l == nil {
		return
	}
	h.logger = l
}

// Handlers returns the map of RPC method names to handler functions.
//
// The keys are full method names including the "agents." prefix, matching the
// pattern used by the bot RPCHandler. The daemon registers them verbatim via
// rpcServer.RegisterHandler.
func (h *RPCHandler) Handlers() map[string]func(context.Context, json.RawMessage) (any, error) {
	return map[string]func(context.Context, json.RawMessage) (any, error){
		// Lifecycle (spec line 533: direct ports of bot.* lifecycle)
		"agents.list":   h.handleList,
		"agents.get":    h.handleGet,
		"agents.create": h.handleCreate,
		"agents.update": h.handleUpdate,
		"agents.delete": h.handleDelete,

		// Runtime control (spec line 534)
		"agents.pause":   h.handlePause,
		"agents.resume":  h.handleResume,
		"agents.trigger": h.handleTrigger,

		// Constitution amendment (spec line 536)
		"agents.amend": h.handleAmend,

		// Goals (spec lines 537-538)
		"agents.goals.list":    h.handleGoalsList,
		"agents.goals.get":     h.handleGoalsGet,
		"agents.goals.approve": h.handleGoalsApprove,
		"agents.goals.reject":  h.handleGoalsReject,

		// Audit (spec line 539)
		"agents.audit.list":    h.handleAuditList,
		"agents.audit.resolve": h.handleAuditResolve,

		// Migration (spec line 540)
		"agents.migrate": h.handleMigrate,
	}
}

// ---------------------------------------------------------------------------
// Request/response types
//
// These are intentionally permissive: the manager performs authoritative
// validation. The handler only unmarshals enough to route the call.

type listRequest struct {
	// StatusFilter filters by employee status ("running", "paused", ...).
	// Empty = all.
	StatusFilter string `json:"status,omitempty"`
}

type idRequest struct {
	ID string `json:"id"`
}

type createRequest struct {
	// Employee is the full employee definition (BotDefinition + Constitution).
	// We accept the raw bot definition shape plus a constitution field; the
	// manager.Hire validates the constitution.
	ID           string                 `json:"id"`
	Name         string                 `json:"name"`
	Description  string                 `json:"description,omitempty"`
	Prompt       string                 `json:"prompt"`
	Model        string                 `json:"model,omitempty"`
	Triggers     []bot.BotTrigger       `json:"triggers,omitempty"`
	MemoryScope  bot.MemoryScope        `json:"memory_scope,omitempty"`
	Tools        []string               `json:"tools,omitempty"`
	Enabled      bool                   `json:"enabled"`
	Constitution map[string]any         `json:"constitution"`
	// RawDefinition preserves unknown fields for forward compatibility.
	RawDefinition map[string]json.RawMessage `json:"-"`
}

type updateRequest struct {
	ID           string         `json:"id"`
	Name         string         `json:"name,omitempty"`
	Description  string         `json:"description,omitempty"`
	Prompt       string         `json:"prompt,omitempty"`
	Model        string         `json:"model,omitempty"`
	Tools        []string       `json:"tools,omitempty"`
	Enabled      *bool          `json:"enabled,omitempty"`
	Constitution map[string]any `json:"constitution,omitempty"`
}

type triggerRequest struct {
	ID      string         `json:"id"`
	Payload map[string]any `json:"payload,omitempty"`
}

type amendRequest struct {
	ID     string            `json:"id"`
	Fields map[string]any    `json:"fields"`
	Reason string            `json:"reason,omitempty"`
}

type goalsListRequest struct {
	EmployeeID string `json:"employee_id,omitempty"`
}

type goalApproveRequest struct {
	GoalID string `json:"goal_id"`
	PlanID string `json:"plan_id"`
	Reason string `json:"reason,omitempty"`
}

type goalRejectRequest struct {
	GoalID string `json:"goal_id"`
	PlanID string `json:"plan_id"`
	Reason string `json:"reason"`
}

type auditListRequest struct {
	EmployeeID string `json:"employee_id"`
	Since      string `json:"since,omitempty"`  // duration string e.g. "24h", "7d"
	Severity   string `json:"severity,omitempty"` // info|warning|critical
}

type auditResolveRequest struct {
	FindingID  string `json:"finding_id"`
	Resolution string `json:"resolution"` // false_positive|acknowledged|constitution_amended
	Note       string `json:"note,omitempty"`
}

type migrateRequest struct {
	// Apply, if set, writes the proposed constitution for the given bot ID to
	// disk instead of returning a dry-run proposal. Empty = dry-run for all.
	Apply string `json:"apply,omitempty"`
}

// ---------------------------------------------------------------------------
// Lifecycle handlers
// ---------------------------------------------------------------------------

func (h *RPCHandler) handleList(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	// Request body is optional for list; ignore unmarshal errors for empty body.
	var req listRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}

	employees, err := h.manager.ListEmployees(ctx, req.StatusFilter)
	if err != nil {
		return nil, fmt.Errorf("list employees: %w", err)
	}
	return map[string]any{"agents": employees}, nil
}

func (h *RPCHandler) handleGet(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req idRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	return h.manager.GetEmployee(ctx, req.ID)
}

func (h *RPCHandler) handleCreate(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req createRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid employee definition: %w", err)
	}

	// The manager.Hire validates the constitution (spec: agents.create
	// validates the constitution, delegates to Manager.Hire).
	employee, err := h.manager.Hire(ctx, HireRequest{
		ID:           req.ID,
		Name:         req.Name,
		Description:  req.Description,
		Prompt:       req.Prompt,
		Model:        req.Model,
		Triggers:     req.Triggers,
		MemoryScope:  req.MemoryScope,
		Tools:        req.Tools,
		Enabled:      req.Enabled,
		Constitution: req.Constitution,
	})
	if err != nil {
		return nil, fmt.Errorf("hire: %w", err)
	}
	return map[string]any{
		"id":      employee.ID,
		"status":  "created",
		"version": employee.Constitution.Version,
	}, nil
}

func (h *RPCHandler) handleUpdate(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req updateRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid update request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	updated, err := h.manager.UpdateEmployee(ctx, UpdateRequest{
		ID:           req.ID,
		Name:         req.Name,
		Description:  req.Description,
		Prompt:       req.Prompt,
		Model:        req.Model,
		Tools:        req.Tools,
		Enabled:      req.Enabled,
		Constitution: req.Constitution,
	})
	if err != nil {
		return nil, fmt.Errorf("update: %w", err)
	}
	return map[string]any{"id": updated.ID, "status": "updated"}, nil
}

func (h *RPCHandler) handleDelete(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req idRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.manager.Retire(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("retire: %w", err)
	}
	return map[string]any{"id": req.ID, "status": "deleted"}, nil
}

// ---------------------------------------------------------------------------
// Runtime control handlers
// ---------------------------------------------------------------------------

func (h *RPCHandler) handlePause(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req idRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.manager.Pause(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("pause: %w", err)
	}
	return map[string]any{"id": req.ID, "status": "paused"}, nil
}

func (h *RPCHandler) handleResume(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req idRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if err := h.manager.Resume(ctx, req.ID); err != nil {
		return nil, fmt.Errorf("resume: %w", err)
	}
	return map[string]any{"id": req.ID, "status": "resumed"}, nil
}

func (h *RPCHandler) handleTrigger(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req triggerRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid trigger request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	result, err := h.manager.Trigger(ctx, req.ID, req.Payload)
	if err != nil {
		return nil, fmt.Errorf("trigger: %w", err)
	}
	return map[string]any{
		"id":          req.ID,
		"status":      "triggered",
		"invocation":  result,
	}, nil
}

// ---------------------------------------------------------------------------
// Constitution amendment handler (spec line 536)
//
// Routes through Plan signoff via Manager.AmendConstitution. The amendment
// does not apply immediately; it produces a Plan in PendingApproval routed to
// the employee's escalates_to approvers.
// ---------------------------------------------------------------------------

func (h *RPCHandler) handleAmend(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req amendRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid amend request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	if len(req.Fields) == 0 {
		return nil, fmt.Errorf("at least one field is required")
	}

	planID, err := h.manager.AmendConstitution(ctx, AmendRequest{
		EmployeeID: req.ID,
		Fields:     req.Fields,
		Reason:     req.Reason,
	})
	if err != nil {
		return nil, fmt.Errorf("amend constitution: %w", err)
	}
	return map[string]any{
		"id":           req.ID,
		"status":       "amendment_proposed",
		"plan_id":      planID,
		"approval_url": fmt.Sprintf("meept plans show %s", planID),
	}, nil
}

// ---------------------------------------------------------------------------
// Goals handlers
// ---------------------------------------------------------------------------

func (h *RPCHandler) handleGoalsList(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req goalsListRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}
	goals, err := h.manager.ListGoals(ctx, req.EmployeeID)
	if err != nil {
		return nil, fmt.Errorf("list goals: %w", err)
	}
	return map[string]any{"goals": goals}, nil
}

func (h *RPCHandler) handleGoalsGet(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid request: %w", err)
	}
	if req.ID == "" {
		return nil, fmt.Errorf("id is required")
	}
	return h.manager.GetGoal(ctx, req.ID)
}

func (h *RPCHandler) handleGoalsApprove(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req goalApproveRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid approve request: %w", err)
	}
	if req.GoalID == "" || req.PlanID == "" {
		return nil, fmt.Errorf("goal_id and plan_id are required")
	}
	if err := h.manager.ApprovePlan(ctx, req.GoalID, req.PlanID, req.Reason); err != nil {
		return nil, fmt.Errorf("approve plan: %w", err)
	}
	return map[string]any{
		"goal_id": req.GoalID,
		"plan_id": req.PlanID,
		"status":  "approved",
	}, nil
}

func (h *RPCHandler) handleGoalsReject(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req goalRejectRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid reject request: %w", err)
	}
	if req.GoalID == "" || req.PlanID == "" {
		return nil, fmt.Errorf("goal_id and plan_id are required")
	}
	if req.Reason == "" {
		return nil, fmt.Errorf("reason is required when rejecting a plan")
	}
	if err := h.manager.RejectPlan(ctx, req.GoalID, req.PlanID, req.Reason); err != nil {
		return nil, fmt.Errorf("reject plan: %w", err)
	}
	return map[string]any{
		"goal_id": req.GoalID,
		"plan_id": req.PlanID,
		"status":  "rejected",
	}, nil
}

// ---------------------------------------------------------------------------
// Audit handlers
// ---------------------------------------------------------------------------

func (h *RPCHandler) handleAuditList(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req auditListRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid audit request: %w", err)
	}
	if req.EmployeeID == "" {
		return nil, fmt.Errorf("employee_id is required")
	}

	var since time.Duration
	if req.Since != "" {
		d, err := time.ParseDuration(req.Since)
		if err != nil {
			return nil, fmt.Errorf("invalid since duration %q: %w", req.Since, err)
		}
		since = d
	}

	findings, err := h.manager.ListAuditFindings(ctx, AuditQuery{
		EmployeeID: req.EmployeeID,
		Since:      since,
		Severity:   req.Severity,
	})
	if err != nil {
		return nil, fmt.Errorf("list audit findings: %w", err)
	}
	return map[string]any{"findings": findings}, nil
}

func (h *RPCHandler) handleAuditResolve(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req auditResolveRequest
	if err := json.Unmarshal(raw, &req); err != nil {
		return nil, fmt.Errorf("invalid resolve request: %w", err)
	}
	if req.FindingID == "" {
		return nil, fmt.Errorf("finding_id is required")
	}
	switch req.Resolution {
	case "false_positive", "acknowledged", "constitution_amended":
		// ok
	default:
		return nil, fmt.Errorf("invalid resolution %q: must be false_positive, acknowledged, or constitution_amended", req.Resolution)
	}
	if err := h.manager.ResolveAuditFinding(ctx, req.FindingID, req.Resolution, req.Note); err != nil {
		return nil, fmt.Errorf("resolve finding: %w", err)
	}
	return map[string]any{
		"finding_id": req.FindingID,
		"status":     "resolved",
	}, nil
}

// ---------------------------------------------------------------------------
// Migration handler (spec line 540, CLI line 476-477)
//
// Scans ~/.meept/bots/*.json via Manager.Migrate. When req.Apply is set,
// writes the proposed constitution for that bot ID to disk; otherwise runs a
// dry-run scan across all legacy bots and returns proposed constitutions.
// ---------------------------------------------------------------------------

func (h *RPCHandler) handleMigrate(ctx context.Context, raw json.RawMessage) (any, error) {
	if h.manager == nil {
		return nil, errNotConfigured
	}
	var req migrateRequest
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &req)
	}

	if req.Apply != "" {
		// Apply proposed constitution for a specific bot ID.
		result, err := h.manager.ApplyMigration(ctx, req.Apply)
		if err != nil {
			return nil, fmt.Errorf("apply migration for %s: %w", req.Apply, err)
		}
		return map[string]any{
			"status":   "applied",
			"bot_id":   req.Apply,
			"applied":  result.Applied,
			"warnings": result.Warnings,
		}, nil
	}

	// Dry-run scan.
	proposals, err := h.manager.Migrate(ctx)
	if err != nil {
		return nil, fmt.Errorf("migration scan: %w", err)
	}
	return map[string]any{
		"status":    "dry_run",
		"proposals": proposals,
		"count":     len(proposals),
	}, nil
}
