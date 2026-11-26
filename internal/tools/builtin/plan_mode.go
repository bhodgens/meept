package builtin

import (
	"context"
	"sort"
	"strings"
	"sync"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/tools"
)

// PlanModePersistence is the per-session durability seam for the plan_mode
// bool. It mirrors the existing per-session flag accessors on the session
// store (SetForeground/SetNoFence/SetGoalID); the orchestrator wires a
// store-backed implementation. A nil persistence makes plan_mode in-memory
// only (default off, still per-session within the process).
type PlanModePersistence interface {
	GetPlanMode(sessionID string) (bool, error)
	SetPlanMode(sessionID string, on bool) error
}

// PlanPolicySection is the prompt section injected into the agent's system
// prompt while plan mode is active for the session. It tells the model to
// explore and design before executing, and to present plans via
// exit_plan_mode for an inline Approve / Keep planning decision.
const PlanPolicySection = `## Plan Mode Policy

You are collaborating in PLAN MODE. Your job is to explore and design before
executing: read the task, consider alternatives, and draft a concrete plan as
markdown that starts with a single top-level "# " heading. Do not begin
broad execution of the plan yourself. When you have a plan ready for the user
to approve, call exit_plan_mode with the full markdown plan. The user will
choose Approve or Keep planning; on Keep planning, revise your plan according
to the feedback and present it again. Plan mode is a collaboration state and
is distinct from goal mode: you are not creating an employee goal, only
agreeing on a plan with the user.`

// PlanModeController owns the per-session plan_mode toggle. It is a session
// collaboration state (DISTINCT from goal mode): entering plan_mode never
// touches a goal, and the round driver never runs in plan_mode.
type PlanModeController struct {
	mu       sync.Mutex
	sessions map[string]bool
	persist  PlanModePersistence
}

// NewPlanModeController creates a controller with every session defaulting to
// plan mode OFF. A nil persistence keeps the toggle in-memory per-session.
func NewPlanModeController(persist PlanModePersistence) *PlanModeController {
	return &PlanModeController{
		sessions: make(map[string]bool),
		persist:  persist,
	}
}

// SetPersistence wires (or replaces) the session-store-backed durability seam.
// Nil-safe: passing nil is a no-op.
func (c *PlanModeController) SetPersistence(p PlanModePersistence) {
	if p != nil {
		c.persist = p
	}
}

// PlanMode reports whether the session is currently in plan mode. Defaults to
// false for an unknown session. Reads honor a wired persistence seam: an
// in-memory miss falls back to the store and caches the result so a daemon
// restart does not silently drop the flag.
func (c *PlanModeController) PlanMode(sessionID string) bool {
	if sessionID == "" {
		return false
	}
	c.mu.Lock()
	on, ok := c.sessions[sessionID]
	c.mu.Unlock()
	if ok {
		return on
	}
	if c.persist != nil {
		if stored, err := c.persist.GetPlanMode(sessionID); err == nil {
			c.mu.Lock()
			c.sessions[sessionID] = stored
			c.mu.Unlock()
			return stored
		}
	}
	return false
}

// SetPlanMode toggles plan mode for the session and persists it when a seam
// is wired. The in-memory view is always updated; a store write error is
// returned (and logged by the caller) but never leaves the session stuck in a
// stale in-memory value.
func (c *PlanModeController) SetPlanMode(sessionID string, on bool) error {
	if sessionID == "" {
		return nil
	}
	c.mu.Lock()
	c.sessions[sessionID] = on
	c.mu.Unlock()
	if c.persist != nil {
		return c.persist.SetPlanMode(sessionID, on)
	}
	return nil
}

// Clear drops the in-memory entry (defaulting the session back to OFF) and, when
// a persistence seam is wired, persists false so the flag is not resurrected
// after a restart.
func (c *PlanModeController) Clear(sessionID string) error {
	if sessionID == "" {
		return nil
	}
	c.mu.Lock()
	delete(c.sessions, sessionID)
	c.mu.Unlock()
	if c.persist != nil {
		return c.persist.SetPlanMode(sessionID, false)
	}
	return nil
}

// ActiveSessions returns the snapshot of session IDs currently in plan mode,
// sorted for deterministic prompt output.
func (c *PlanModeController) ActiveSessions() []string {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []string
	for id, on := range c.sessions {
		if on {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// PlanPolicySection returns the plan-policy prompt text when at least one
// session is in plan mode, else "". The context injector appends it only when
// non-empty, so the section is present only while plan mode is active.
func (c *PlanModeController) PlanPolicySection() string {
	if len(c.ActiveSessions()) == 0 {
		return ""
	}
	return PlanPolicySection
}

// HandlePlanCommand is the /plan command handler. It mirrors the dsh /plan
// surface (:232-278): an empty or non-"off" argument toggles plan mode ON and
// returns any trailing message to be steered to the agent; "off"/"stop"/"exit"
// clears it. Plan mode is distinct from goal mode — this never touches a goal.
//
// Marshal is not required by ACP or the tool registry; the handler returns a
// typed reply for the orchestrator to render.
func (c *PlanModeController) HandlePlanCommand(sessionID, input string) (*PlanCommandReply, error) {
	text := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(input), "/plan"))
	lower := strings.ToLower(text)
	switch {
	case lower == "off" || lower == "stop" || lower == "exit" || lower == "end":
		if err := c.SetPlanMode(sessionID, false); err != nil {
			return nil, err
		}
		return &PlanCommandReply{On: false, Message: "plan mode off"}, nil
	case text == "":
		on := !c.PlanMode(sessionID)
		if err := c.SetPlanMode(sessionID, on); err != nil {
			return nil, err
		}
		if on {
			return &PlanCommandReply{On: true, Message: "plan mode on"}, nil
		}
		return &PlanCommandReply{On: false, Message: "plan mode off"}, nil
	default:
		if err := c.SetPlanMode(sessionID, true); err != nil {
			return nil, err
		}
		// Non-"off" message: activate plan mode and steer the text to the agent.
		return &PlanCommandReply{On: true, Steer: text, Message: "plan mode on"}, nil
	}
}

// PlanCommandReply is the typed result of a /plan command toggle.
type PlanCommandReply struct {
	// On reports whether plan mode ended on for the session.
	On bool
	// Steer carries the non-"off" message text to route to the agent while
	// plan mode is active (empty when none).
	Steer string
	// Message is the human-facing toggle status.
	Message string
}

// PlanGateManager is the subset of *plan.PlanManager the exit_plan_mode tool
// needs to land an approved plan on the existing durable gate. It is satisfied
// by *plan.PlanManager (CreatePlan / SubmitPlan / ApprovePlan).
type PlanGateManager interface {
	CreatePlan(ctx context.Context, title, description, projectID, projectPath, sessionID string) (*plan.Plan, error)
	SubmitPlan(ctx context.Context, planID string) error
	ApprovePlan(ctx context.Context, planID, sessionID, by string) error
}

// ExitPlanModeTool presents the model's markdown plan for an inline
// Approve / Keep planning decision and, on Approve, exits plan mode and lands
// the plan on the existing durable gate. It is only available (progressive
// disclosure) while plan mode is active for the bound session.
//
// Concurrency (A6-6): ONE instance is shared across every loop in the tool
// registry, and the agent loop injects the per-turn responder + session
// binding through the ResponseFuncSetter seam (speak.go wireResponseSetters).
// Two concurrent session loops mutating plain struct fields is a data race
// and leaks cross-session plan-mode state. The fields are therefore mutex-
// guarded, and Execute resolves the SESSION from the per-turn tool context
// (tools.SessionIDFromContext, injected by executeToolCallsUngated) — the
// authoritative per-call identity — falling back to the last-injected field
// only when the ctx carries none (legacy direct callers).
type ExitPlanModeTool struct {
	tools.ToolDefaults
	ctrl *PlanModeController

	mu        sync.RWMutex
	askUser   ResponseFunc
	sessionID string

	planGate PlanGateManager
	approver string
}

// NewExitPlanModeTool creates the tool bound to a plan-mode controller. The
// response func, plan gate, and session binding are wired by the orchestrator
// via the setters below.
func NewExitPlanModeTool(ctrl *PlanModeController) *ExitPlanModeTool {
	return &ExitPlanModeTool{
		ctrl:     ctrl,
		approver: "user",
	}
}

// SetResponseFunc wires the ask channel used to present Approve / Keep
// planning. Nil-safe (typed-nil guard). Race-safe (A6-6): the shared tool
// instance is injected per-turn by EVERY session loop, so the store runs
// under the mutex.
func (t *ExitPlanModeTool) SetResponseFunc(fn ResponseFunc) {
	if fn == nil {
		return
	}
	t.mu.Lock()
	t.askUser = fn
	t.mu.Unlock()
}

// SetPlanGate wires the existing durable plan gate used to land approved
// plans. Nil-safe.
func (t *ExitPlanModeTool) SetPlanGate(g PlanGateManager) {
	if g != nil {
		t.mu.Lock()
		t.planGate = g
		t.mu.Unlock()
	}
}

// SetSessionID binds the tool to a session (legacy fallback binding).
// exit_plan_mode is session-scoped, but the tool INSTANCE is shared across
// loops, so per-turn execution reads the session from the tool context
// (tools.SessionIDFromContext — injected per turn by the agent loop) and
// consults this field only when the ctx carries no session. Race-safe
// under the mutex (A6-6).
func (t *ExitPlanModeTool) SetSessionID(sessionID string) {
	t.mu.Lock()
	t.sessionID = sessionID
	t.mu.Unlock()
}

// SetApprover sets the human identifier recorded as the approver on the plan
// gate signoff. Defaults to "user".
func (t *ExitPlanModeTool) SetApprover(by string) {
	if by != "" {
		t.mu.Lock()
		t.approver = by
		t.mu.Unlock()
	}
}

// sessionFor resolves the executing session: the per-turn tool context is
// authoritative (one shared instance, many concurrent loops — A6-6); the
// last-injected field is the legacy fallback for direct callers whose ctx
// carries no session.
func (t *ExitPlanModeTool) sessionFor(ctx context.Context) string {
	if sid := tools.SessionIDFromContext(ctx); sid != "" {
		return sid
	}
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.sessionID
}

// Name implements tools.Tool.
func (t *ExitPlanModeTool) Name() string { return "exit_plan_mode" }

// Category implements tools.Tool.
func (t *ExitPlanModeTool) Category() string { return "agent" }

// Description implements tools.Tool.
func (t *ExitPlanModeTool) Description() string {
	return "Present your drafted markdown plan (starting with a '# ' heading) to the user for an inline Approve or Keep planning decision while plan mode is active. On Approve, plan mode is left and the plan is submitted through the durable plan-approval gate; on Keep planning, the user's feedback is returned to you to revise the plan and present it again."
}

// Parameters implements tools.Tool.
func (t *ExitPlanModeTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"plan": {
				Type:        schemaTypeString,
				Description: "The full markdown plan, which must start with a single top-level '# ' heading.",
			},
		},
		Required: []string{"plan"},
	}
}

// Execute implements tools.Tool. It validates the plan heading, presents the
// Approve / Keep planning question via the ask channel, and on Approve lands
// the plan on the existing durable gate (CreatePlan -> SubmitPlan ->
// ApprovePlan) before leaving plan mode.
func (t *ExitPlanModeTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	// A6-6: session from the per-turn context (authoritative for loop
	// turns), field fallback for legacy direct callers.
	sessionID := t.sessionFor(ctx)
	if sessionID == "" || t.ctrl == nil || !t.ctrl.PlanMode(sessionID) {
		return tools.NewErrorResult("exit_plan_mode is only available while plan mode is active for this session"), nil
	}

	// The responder/gate/approver are snapshotted under the mutex — a
	// concurrent session loop's per-turn injection can never race this
	// read (A6-6). A ctx-carried responder (tools.TurnResponderFromContext,
	// stamped by loops that support it) outranks the last-injected field.
	var askUser ResponseFunc
	if r := tools.TurnResponderFromContext(ctx); r != nil {
		askUser = ResponseFunc(r)
	}
	t.mu.RLock()
	if askUser == nil {
		askUser = t.askUser
	}
	planGate := t.planGate
	approver := t.approver
	t.mu.RUnlock()

	rawPlan, _ := args["plan"].(string)
	planText := strings.TrimSpace(rawPlan)
	if planText == "" {
		return tools.NewErrorResult("plan is required and must be a non-empty string"), nil
	}
	if !planHasTopHeading(planText) {
		return tools.NewErrorResult("plan must be markdown that starts with a '# ' heading"), nil
	}

	if askUser == nil {
		return tools.NewErrorResult("presentation channel is not available: no user response callback configured"), nil
	}

	const present = "A plan has been prepared and presented for review. Approve it to leave plan mode and land it on the plan gate, or keep planning."
	answer, err := askUser(ctx, present, []string{"Approve", "Keep planning"})
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, err
	}

	decision := classifyDecision(answer)
	if decision == keepPlanning {
		// Plan mode stays active; the user's feedback returns to the model.
		return tools.NewSuccessResult(map[string]any{
			"decision": "keep_planning",
			"feedback": answer,
		}), nil
	}

	// Approve: land the plan on the existing durable gate, then leave plan mode.
	if planGate == nil {
		return tools.NewErrorResult("plan approval gate is not available"), nil
	}
	title := planTitle(planText)
	created, err := planGate.CreatePlan(ctx, title, planText, "", "", sessionID)
	if err != nil {
		return tools.NewErrorResult("create plan: " + err.Error()), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	}
	if err := planGate.SubmitPlan(ctx, created.ID); err != nil {
		return tools.NewErrorResult("submit plan: " + err.Error()), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	}
	if err := planGate.ApprovePlan(ctx, created.ID, sessionID, approver); err != nil {
		return tools.NewErrorResult("approve plan: " + err.Error()), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	}
	_ = t.ctrl.SetPlanMode(sessionID, false)

	return tools.NewSuccessResult(map[string]any{
		"decision": "approved",
		"plan_id":  created.ID,
		"state":    string(plan.StateApproved),
		"title":    title,
	}), nil
}

type decision int

const (
	approvePlan decision = iota
	keepPlanning
)

// classifyDecision maps the user's free-text reply to an Approve or Keep
// planning decision. "approve" matches the Approve option; everything else
// (including "keep planning") is treated as keep-planning feedback so the
// model revises and presents again.
//
// A6-11: the original shape substring-matched "approve", so "do not
// approve", "don't approve", and "disapprove" all returned approvePlan —
// the negated reply silently approved the plan. The match is now
// anchored: an explicit negation denylist is checked FIRST and forces
// keep-planning, then a word-boundary positive set must match (the
// standalone word "approve"/"approved"/"approve it"/"lgtm"/"yes, approve"
// shapes), so a plan approves only when the reply is a clear affirmative.
// Everything unmatched stays keep-planning (fail-closed default: an
// ambiguous reply returns feedback to the model, never lands a plan).
// The responder currently fails closed upstream (speak.go delivers an
// error, so no production reply reaches this classifier yet) — the fix is
// still required for the moment an interactive sink lands.
func classifyDecision(answer string) decision {
	lower := strings.ToLower(strings.TrimSpace(answer))
	if lower == "" {
		return keepPlanning
	}
	// Negations first: any of these forces keep-planning even when the
	// affirmative word also appears ("do not approve" contains
	// "approve").
	for _, neg := range decisionNegations {
		if strings.Contains(lower, neg) {
			return keepPlanning
		}
	}
	for _, pos := range decisionAffirmatives {
		if matchWordBoundary(lower, pos) {
			return approvePlan
		}
	}
	return keepPlanning
}

// decisionNegations are the explicit refusal shapes (checked before the
// affirmatives so a negated sentence never approves).
var decisionNegations = []string{
	"do not approve",
	"don't approve",
	"dont approve",
	"don t approve",
	"not approve",
	"never approve",
	"cannot approve",
	"can't approve",
	"cant approve",
	"won't approve",
	"wont approve",
	"disapprove",
	"reject",
	"keep planning",
	"not yet",
	"not now",
	"hold off",
	"hold on",
}

// decisionAffirmatives are the word-boundary positive shapes that mean
// "approve the plan".
var decisionAffirmatives = []string{
	"approve",
	"approved",
	"approving",
	"lgtm",
	"ok to proceed",
	"go ahead",
	"proceed",
}

// matchWordBoundary reports whether word occurs in s delimited by
// non-alphanumeric boundaries ("" counts at the string edges). This keeps
// "disapprove"/"unapproved" from matching "approve" while allowing
// "approve it" / "i approve" / "approved!" (negations already ran).
func matchWordBoundary(s, word string) bool {
	for i := 0; i+len(word) <= len(s); {
		idx := strings.Index(s[i:], word)
		if idx < 0 {
			return false
		}
		start := i + idx
		end := start + len(word)
		startOK := start == 0 || !isAlnum(s[start-1])
		endOK := end == len(s) || !isAlnum(s[end])
		if startOK && endOK {
			return true
		}
		i = end
	}
	return false
}

func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// planTitle derives a plan title from the required leading '# ' heading, e.g.
// "# probe plan" -> "probe plan".
func planTitle(md string) string {
	for line := range strings.Lines(md) {
		trimmed := strings.TrimSpace(line)
		if heading, ok := strings.CutPrefix(trimmed, "# "); ok {
			return strings.TrimSpace(heading)
		}
	}
	return "untitled plan"
}

// planHasTopHeading reports whether the markdown starts with a single
// top-level "# " heading (the required heading shape; "## ..." subheadings are
// rejected).
func planHasTopHeading(md string) bool {
	for line := range strings.Lines(md) {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		return strings.HasPrefix(trimmed, "# ")
	}
	return false
}

// RegisterPlanModeTools registers the exit_plan_mode tool with the registry.
// The controller is the per-session source of truth for progressive
// disclosure; the tool's response func, plan gate, and session binding are
// wired by the orchestrator on the returned tool.
func RegisterPlanModeTools(registry *tools.Registry, ctrl *PlanModeController) {
	registry.Register(NewExitPlanModeTool(ctrl))
}
