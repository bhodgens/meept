// Package builtin — goal_tools.go implements the model-facing goal tools
// (dsh-convergence leaf docs/plans/20261102-dsh-convergence/02-goals-plans/
// 02-model-goal-tools.md, contract C4b): goal_get / goal_create / goal_update
// over the employee.Manager goal store, surfaced to the model as snake_case
// tools with an authority gate.
//
// Authority model (dsh): create/edit/pause/resume are only legal under a
// direct human request; complete/blocked may be driven by an autonomous
// continuation round, and a self-block is gated at the consecutive-round
// threshold (default 3). When the round context is unknown the tools fail
// safe and require a direct human turn.
//
// Import cycle: internal/tools/builtin cannot import internal/employee
// (employee → bot → … → tools/builtin), so the goal store is abstracted
// behind the local goalManager interface and the builtin-local goalRecord /
// GoalCreateSpec views. The daemon wiring leaf supplies an adapter onto
// *employee.Manager via SetGoalProvider (goal_register.go) — the same
// inversion-of-control seam agent_message.go uses for its store.
package builtin

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// goalPhase mirrors the string forms of employee.GoalState used by the
// tools. Kept local (not imported) for the builtin ← employee import cycle.
type goalPhase string

const (
	goalPhaseActive  goalPhase = "active"
	goalPhasePaused  goalPhase = "paused"
	goalPhaseRetired goalPhase = "retired"
	goalPhaseBlocked goalPhase = "blocked"
)

// defaultBlockedAfterConsecutiveRounds mirrors
// employee.DefaultBlockedAfterConsecutiveRounds (the consecutive-round
// threshold at which an autonomous self-block is permitted). It must track
// the employee default; a self-block before this many rounds is rejected.
const defaultBlockedAfterConsecutiveRounds = 3

// GoalCreateSpec is the builtin-local creation request the wiring adapter
// maps onto employee.GoalSpec. Objective is the mandate/objective prose;
// MaxRounds bounds the autonomous rounds (0 = default cap).
type GoalCreateSpec struct {
	Owner     string
	Objective string
	MaxRounds int
}

// GoalRecord is the builtin-local view of a persisted goal shared by the
// tools and the wiring adapter. Phase uses the employee phase string;
// Objective is the mandate. Armed is the process-local activation flag
// (employee.(*Goal).IsArmed).
type GoalRecord struct {
	ID            string
	Revision      int64
	Objective     string
	Phase         string
	RoundsStarted int
	MaxRounds     int
	BlockedReason *string
	Armed         bool
}

// goalManager abstracts the employee.Manager goal methods the three tools
// need. The daemon wiring leaf adapts *employee.Manager (via SetGoalProvider)
// onto this interface through the goal-registrar seam.
type goalManager interface {
	// CreateGoal validates and persists a new goal, returning the durable
	// record. The adapter must reflect MaxRounds (spec.MaxRounds) in the
	// persisted record.
	CreateGoal(ctx context.Context, spec GoalCreateSpec) (*GoalRecord, error)
	// GetGoal returns the goal by ID with its current revision loaded.
	GetGoal(ctx context.Context, id string) (*GoalRecord, error)
	// ListActive returns the owner's active (non-retired) goals, oldest
	// first; used by goal_get to resolve the current goal.
	ListActive(ctx context.Context, owner string) ([]*GoalRecord, error)
	// UpdateGoal persists the mutated record. The adapter (and its backing
	// employee store) advance the CAS revision on write.
	UpdateGoal(ctx context.Context, g *GoalRecord) error
}

// goalOwner resolves the owning agent/employee id of the calling turn. It is
// the same injection pattern agent_message.go uses for its sender func; the
// daemon wires the current-agent resolver in via SetGoalProvider.
type goalOwner func(ctx context.Context) string

// Tool-facing result shape (contract C4b). goal_get returns
// {"goal": <value>|null}; goal_create / goal_update echo {"goal": <value>}.
type goalValue struct {
	ID            string  `json:"id"`
	Revision      int64   `json:"revision"`
	Objective     string  `json:"objective"`
	Phase         string  `json:"phase"`
	RoundsStarted int     `json:"roundsStarted"`
	MaxRounds     int     `json:"maxRounds"`
	BlockedReason *string `json:"blockedReason,omitempty"`
	Activation    bool    `json:"activation"`
}

// goalValueFromRecord maps the backing GoalRecord view onto the tool-facing
// compact value.
func goalValueFromRecord(g *GoalRecord) goalValue {
	var br *string
	if g != nil && g.BlockedReason != nil && *g.BlockedReason != "" {
		reason := *g.BlockedReason
		br = &reason
	}
	if g == nil {
		return goalValue{}
	}
	return goalValue{
		ID:            g.ID,
		Revision:      g.Revision,
		Objective:     g.Objective,
		Phase:         g.Phase,
		RoundsStarted: g.RoundsStarted,
		MaxRounds:     g.MaxRounds,
		BlockedReason: br,
		Activation:    g.Armed,
	}
}

// ---------------------------------------------------------------------------
// Authority gate
// ---------------------------------------------------------------------------

// Sentinel errors exported for the e2e gate (contract C4b). Execute returns
// them wrapped in a tools.ErrorResult so the model sees the message verbatim
// and errors.Is still matches.
var (
	// ErrGoalToolDirectHumanRequired is returned by create/edit/pause/resume
	// when the turn is not backed by a direct human request.
	ErrGoalToolDirectHumanRequired = errors.New("goal tool requires direct human authority; this turn is an automatic continuation round")

	// ErrGoalToolBlockThreshold is returned when the model self-blocks a goal
	// before it reaches the consecutive-round threshold (GOAL_TOOL_BLOCK_THRESHOLD).
	ErrGoalToolBlockThreshold = errors.New("GOAL_TOOL_BLOCK_THRESHOLD: goal cannot self-block before the consecutive-round threshold")

	// ErrGoalToolBlockedReasonRequired is returned when action=blocked is
	// invoked without a non-empty blocked_reason.
	ErrGoalToolBlockedReasonRequired = errors.New("goal_update blocked requires a non-empty blocked_reason")

	// ErrGoalToolStaleRevision is returned when goal_update's revision does
	// not match the goal's current revision (optimistic-concurrency CAS).
	ErrGoalToolStaleRevision = errors.New("GOAL_TOOL_STALE_REVISION: the goal has moved past the supplied revision; re-read with goal_get and retry")

	// ErrGoalToolNotFound is returned when goal_update targets an unknown goal.
	ErrGoalToolNotFound = errors.New("goal not found")

	// ErrGoalToolOwnerUnresolved is returned when the current owner cannot be
	// resolved for the calling turn.
	ErrGoalToolOwnerUnresolved = errors.New("goal owner not resolved for this turn")
)

// goalAutonomousRound is the process-local authority knob leaf 03 (the round
// driver) sets on a continuation turn so complete/blocked are permitted.
// Default false = fail-safe: an unknown/absent flag requires a direct human
// turn. It is the exported seam this leaf and the driver meet on without
// coordination (process-global; the drive runs one continuation at a time).
var goalAutonomousRound atomic.Bool

// SetGoalAutonomousRound records whether the current round is an autonomous
// continuation round (no human in the loop). The round driver calls this at
// the top/bottom of each continuation turn. When false or never set, the
// goal tools treat the turn as requiring direct human authority (fail-safe).
func SetGoalAutonomousRound(v bool) { goalAutonomousRound.Store(v) }

// goalAutonomousRoundActive reports whether the current round is autonomous.
func goalAutonomousRoundActive() bool { return goalAutonomousRound.Load() }

// requireDirectHuman enforces the create/edit/pause/resume gate: those
// actions need a direct human turn, so they are refused during an autonomous
// continuation round. This is deliberately NOT a no-op default — an unset
// flag (false) means direct human, which is the safe default.
func requireDirectHuman() error {
	if goalAutonomousRoundActive() {
		return ErrGoalToolDirectHumanRequired
	}
	return nil
}

// ---------------------------------------------------------------------------
// goal_get
// ---------------------------------------------------------------------------

// GoalGetTool returns the owner's current active goal as a compact value.
type GoalGetTool struct {
	tools.ToolDefaults
	mgr   goalManager
	owner goalOwner
}

// NewGoalGetTool constructs the goal_get tool bound to the goal store and
// owner resolver.
func NewGoalGetTool(mgr goalManager, owner goalOwner) *GoalGetTool {
	return &GoalGetTool{mgr: mgr, owner: owner}
}

func (t *GoalGetTool) Name() string     { return "goal_get" }
func (t *GoalGetTool) Category() string { return "goal" }

func (t *GoalGetTool) Description() string {
	return "Return the current goal as {" +
		"id, revision, objective, phase, roundsStarted, maxRounds, blockedReason, activation}. " +
		"Returns {\"goal\": null} when no goal is active. Read-only."
}

func (t *GoalGetTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type:       schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{},
		Required:   []string{},
	}
}

// IsReadOnly reports that goal_get never mutates state and may be cached.
func (t *GoalGetTool) IsReadOnly(map[string]any) bool { return true }

// IsConcurrencySafe reports that goal_get may run alongside other tool calls.
func (t *GoalGetTool) IsConcurrencySafe(map[string]any) bool { return true }

// Execute resolves the current active goal for the owner.
func (t *GoalGetTool) Execute(ctx context.Context, _ map[string]any) (any, error) {
	if t.mgr == nil {
		return tools.NewErrorResult("goal store not available"), nil
	}
	owner, err := resolveGoalOwner(t.owner, ctx)
	if err != nil {
		return tools.NewErrorResultErr(err), nil
	}
	goals, err := t.mgr.ListActive(ctx, owner)
	if err != nil {
		return tools.NewErrorResultErr(err), nil
	}
	if len(goals) == 0 {
		return map[string]any{"goal": nil}, nil
	}
	return map[string]any{"goal": goalValueFromRecord(goals[0])}, nil
}

// resolveGoalOwner resolves the owning agent id for the calling turn.
func resolveGoalOwner(owner goalOwner, ctx context.Context) (string, error) {
	if owner == nil {
		return "", fmt.Errorf("%w: no owner resolver is bound", ErrGoalToolOwnerUnresolved)
	}
	id := owner(ctx)
	if id == "" {
		return "", fmt.Errorf("%w: resolver returned an empty owner", ErrGoalToolOwnerUnresolved)
	}
	return id, nil
}

// ---------------------------------------------------------------------------
// goal_create
// ---------------------------------------------------------------------------

// GoalCreateTool validates and persists a new goal via Manager.CreateGoal.
// Direct-human authority is required.
type GoalCreateTool struct {
	tools.ToolDefaults
	mgr   goalManager
	owner goalOwner
}

// NewGoalCreateTool constructs the goal_create tool.
func NewGoalCreateTool(mgr goalManager, owner goalOwner) *GoalCreateTool {
	return &GoalCreateTool{mgr: mgr, owner: owner}
}

func (t *GoalCreateTool) Name() string     { return "goal_create" }
func (t *GoalCreateTool) Category() string { return "goal" }

func (t *GoalCreateTool) Description() string {
	return "Create a new goal for the current employee. Requires direct human " +
		"authority: a goal is only created when a human explicitly requested it."
}

func (t *GoalCreateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"objective": {
				Type:        schemaTypeString,
				Description: "The goal's objective in plain prose (required).",
			},
			"max_rounds": {
				Type:        schemaTypeNumber,
				Description: "Optional cap on autonomous rounds (positive integer; default is the employee default).",
			},
		},
		Required: []string{"objective"},
	}
}

// Execute validates the objective, requires direct-human authority, then
// persists via the goal store and echoes the created value.
func (t *GoalCreateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.mgr == nil {
		return tools.NewErrorResult("goal store not available"), nil
	}
	if err := requireDirectHuman(); err != nil {
		return tools.NewErrorResultErr(fmt.Errorf(
			"goal_create denied: %w (creating a goal outside a direct human request requires permission)", err)), nil
	}
	objective := strings.TrimSpace(asString(args["objective"]))
	if objective == "" {
		return tools.NewErrorResult("goal_create: objective is required"), nil
	}
	maxRounds, _, err := parsePositiveIntField(args, "max_rounds")
	if err != nil {
		return tools.NewErrorResult("goal_create: " + err.Error()), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	}
	owner, err := resolveGoalOwner(t.owner, ctx)
	if err != nil {
		return tools.NewErrorResultErr(err), nil
	}
	g, err := t.mgr.CreateGoal(ctx, GoalCreateSpec{
		Owner:     owner,
		Objective: objective,
		MaxRounds: maxRounds,
	})
	if err != nil {
		return tools.NewErrorResultErr(err), nil
	}
	return map[string]any{"goal": goalValueFromRecord(g)}, nil
}

// ---------------------------------------------------------------------------
// goal_update
// ---------------------------------------------------------------------------

// GoalUpdateTool mutates an existing goal. Actions edit/pause/resume require
// a direct human turn; complete/blocked may be driven by an autonomous round,
// with self-block gated at the consecutive-round threshold.
type GoalUpdateTool struct {
	tools.ToolDefaults
	mgr   goalManager
	owner goalOwner
}

// NewGoalUpdateTool constructs the goal_update tool.
func NewGoalUpdateTool(mgr goalManager, owner goalOwner) *GoalUpdateTool {
	return &GoalUpdateTool{mgr: mgr, owner: owner}
}

func (t *GoalUpdateTool) Name() string     { return "goal_update" }
func (t *GoalUpdateTool) Category() string { return "goal" }

func (t *GoalUpdateTool) Description() string {
	return "Update an existing goal. action is one of edit|pause|resume|complete|blocked. " +
		"edit/pause/resume require direct human authority; complete/blocked may run in an " +
		"autonomous round (blocked requires a non-empty blocked_reason and must reach the " +
		"consecutive-round threshold when self-reported). revision must match the current " +
		"goal revision (optimistic concurrency) — re-read with goal_get if it is stale."
}

func (t *GoalUpdateTool) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{
		Type: schemaTypeObject,
		Properties: map[string]llm.ParameterProperty{
			"goal_id": {
				Type:        schemaTypeString,
				Description: "The ID of the goal to update (required).",
			},
			"revision": {
				Type:        schemaTypeNumber,
				Description: "The goal's current revision from goal_get, used as an optimistic-concurrency check (required).",
			},
			"action": {
				Type:        schemaTypeString,
				Description: "edit|pause|resume|complete|blocked (required). edit updates objective/max_rounds; complete retires the goal as done; blocked marks it blocked.",
				Enum:        []string{"edit", "pause", "resume", "complete", "blocked"},
			},
			"objective": {
				Type:        schemaTypeString,
				Description: "New objective prose (action=edit only).",
			},
			"max_rounds": {
				Type:        schemaTypeNumber,
				Description: "New autonomous-round cap, positive integer (action=edit only).",
			},
			"blocked_reason": {
				Type:        schemaTypeString,
				Description: "Human-readable reason for blocking (action=blocked only, required).",
			},
		},
		Required: []string{"goal_id", "revision", "action"},
	}
}

// Execute applies the requested action under the authority gate and persists.
func (t *GoalUpdateTool) Execute(ctx context.Context, args map[string]any) (any, error) {
	if t.mgr == nil {
		return tools.NewErrorResult("goal store not available"), nil
	}
	goalID := strings.TrimSpace(asString(args["goal_id"]))
	if goalID == "" {
		return tools.NewErrorResult("goal_update: goal_id is required"), nil
	}
	rev, ok := args["revision"]
	if !ok {
		return tools.NewErrorResult("goal_update: revision is required"), nil
	}
	revision, err := asInt64(rev)
	if err != nil {
		return tools.NewErrorResult("goal_update: revision must be a number"), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
	}
	action := strings.TrimSpace(asString(args["action"]))
	if action == "" {
		return tools.NewErrorResult("goal_update: action is required"), nil
	}

	// Load current for CAS + mutation baseline.
	cur, err := t.mgr.GetGoal(ctx, goalID)
	if err != nil {
		return tools.NewErrorResultErr(err), nil
	}
	if cur == nil {
		return tools.NewErrorResultErr(ErrGoalToolNotFound), nil
	}
	if cur.Revision != revision {
		return tools.NewErrorResultErr(fmt.Errorf("%w: supplied revision %d, current %d",
			ErrGoalToolStaleRevision, revision, cur.Revision)), nil
	}

	next := *cur // copy — no shared mutation with the store's copy
	switch action {
	case "edit":
		if err := requireDirectHuman(); err != nil {
			return tools.NewErrorResultErr(fmt.Errorf("goal_update edit denied: %w", err)), nil
		}
		changed := false
		if objective, present := args["objective"]; present {
			trimmed := strings.TrimSpace(asString(objective))
			if trimmed == "" {
				return tools.NewErrorResult("goal_update edit: objective must not be empty"), nil
			}
			next.Objective = trimmed
			changed = true
		}
		if mr, present, err := parsePositiveIntField(args, "max_rounds"); err != nil {
			return tools.NewErrorResult("goal_update edit: " + err.Error()), nil //nolint:nilerr // tool contract: failures surface via the result envelope, not the Go error
		} else if present {
			next.MaxRounds = mr
			changed = true
		}
		if !changed {
			return tools.NewErrorResult("goal_update edit: provide objective and/or max_rounds to change"), nil
		}

	case "pause":
		if err := requireDirectHuman(); err != nil {
			return tools.NewErrorResultErr(fmt.Errorf("goal_update pause denied: %w", err)), nil
		}
		next.Phase = string(goalPhasePaused)
		next.Armed = false

	case "resume":
		if err := requireDirectHuman(); err != nil {
			return tools.NewErrorResultErr(fmt.Errorf("goal_update resume denied: %w", err)), nil
		}
		next.Phase = string(goalPhaseActive)
		next.Armed = true

	case "complete":
		// Legal under direct human or autonomous authority.
		next.Phase = string(goalPhaseRetired)
		next.Armed = false

	case "blocked":
		reason := strings.TrimSpace(asString(args["blocked_reason"]))
		if reason == "" {
			return tools.NewErrorResultErr(ErrGoalToolBlockedReasonRequired), nil
		}
		// A self-report during an autonomous round must reach the
		// consecutive-round threshold; a direct human may block at any round.
		if goalAutonomousRoundActive() && next.RoundsStarted < defaultBlockedAfterConsecutiveRounds {
			return tools.NewErrorResultErr(fmt.Errorf("%w: roundsStarted %d < %d",
				ErrGoalToolBlockThreshold, next.RoundsStarted, defaultBlockedAfterConsecutiveRounds)), nil
		}
		reasonCopy := reason
		next.BlockedReason = &reasonCopy
		next.Phase = string(goalPhaseBlocked)
		next.Armed = false

	default:
		return tools.NewErrorResult(fmt.Sprintf("goal_update: unknown action %q (want edit|pause|resume|complete|blocked)", action)), nil
	}

	if err := t.mgr.UpdateGoal(ctx, &next); err != nil {
		return tools.NewErrorResultErr(err), nil
	}
	// Re-fetch for the authoritative post-write revision.
	updated, err := t.mgr.GetGoal(ctx, goalID)
	if err != nil || updated == nil {
		return map[string]any{"goal": goalValueFromRecord(&next)}, nil //nolint:nilerr // re-fetch is best-effort; the just-written record is authoritative enough
	}
	return map[string]any{"goal": goalValueFromRecord(updated)}, nil
}

// ---------------------------------------------------------------------------
// small helpers
// ---------------------------------------------------------------------------

func asString(v any) string {
	if v == nil {
		return ""
	}
	if s, ok := v.(string); ok {
		return s
	}
	return fmt.Sprintf("%v", v)
}

// parsePositiveIntField reads a required-positive integer field from the
// JSON-decoded args (numbers arrive as float64). present=false when the key
// is absent or null; an invalid value returns a descriptive error.
func parsePositiveIntField(args map[string]any, key string) (int, bool, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return 0, false, nil
	}
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	case int:
		f = float64(n)
	case int64:
		f = float64(n)
	default:
		return 0, true, fmt.Errorf("%s must be a positive integer", key)
	}
	if f <= 0 || f != float64(int64(f)) {
		return 0, true, fmt.Errorf("%s must be a positive integer", key)
	}
	return int(f), true, nil
}

func asInt64(v any) (int64, error) {
	switch n := v.(type) {
	case float64:
		if n != float64(int64(n)) {
			return 0, fmt.Errorf("not an integer")
		}
		return int64(n), nil
	case float32:
		return int64(n), nil
	case int:
		return int64(n), nil
	case int64:
		return n, nil
	case string:
		// Tolerate a numeric string (some model tool-call encoders emit it).
		f, err := parsePositiveFloatField(n)
		if err == nil && f == float64(int64(f)) {
			return int64(f), nil
		}
		return 0, fmt.Errorf("not an integer")
	default:
		return 0, fmt.Errorf("not a number")
	}
}

// parsePositiveFloatField parses a non-empty integer-valued string with no
// sign/leading-whitespace guard requirement beyond the int64 range.
func parsePositiveFloatField(s string) (float64, error) {
	var (
		f   float64
		err error
	)
	if strings.ContainsAny(s, " 	\n") {
		return 0, fmt.Errorf("not a number")
	}
	_, err = fmt.Sscanf(s, "%f", &f)
	if err != nil {
		return 0, err
	}
	return f, nil
}
