package rpc

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/caimlas/meept/internal/plan"
)

// CompileProblemView is the wire shape of one compile problem (JSON view of
// plan.CompileProblem; the rpc package does not import agent, and the
// compiler's problems are simple enough to mirror by value).
type CompileProblemView struct {
	Line    int    `json:"line"`
	Message string `json:"message"`
}

// DraftSource is the narrow seam the seal handler needs from the agent
// package's draft store (rpc CANNOT import agent — cycle). The daemon wires
// closures over StrategicPlanner into this interface.
type DraftSource interface {
	// DraftFor returns the current draft markdown and its sealed hash
	// ("" while still a draft). ok=false when the task has no draft.
	DraftFor(taskID string) (markdown string, sealedHash string, ok bool)
	// SealDraft stamps the sealed document's hash on the task's draft.
	SealDraft(taskID string, hash string) error
}

// PlanSealHandler exposes the brainstorm draft lifecycle to CLI/TUI/HTTP:
// plan.seal seals + compiles + persists + executes a task's draft;
// plan.draft gets or saves the draft markdown. Direct RegisterHandler, NOT a
// bus proxy (same shape as TaskApprovalHandler): sealing must run the exact
// persist/execute sequence in-process.
//
// All injected funcs are nil-guarded; a nil func yields a clear per-call
// error rather than a panic. No typed-nil interface hazards: DraftSource is
// checked as a complete interface value.
type PlanSealHandler struct {
	// DraftSource provides draft access (typically closures over
	// StrategicPlanner). Nil ⇒ seal/draft calls error.
	DraftSource DraftSource
	// Compile compiles sealed markdown into phase specs. Returns problems
	// (NOT an error) when the document has compile problems. Typically
	// plan.CompileSealed adapted in daemon wiring.
	Compile func(markdown string, maxPhases int) (phases any, hash string, warnings []string, problems []CompileProblemView, err error)
	// Persist stores the compiled phases via the EXISTING plan persistence
	// path. tree is non-nil in tree mode (the human/leaf-agent artifact);
	// flat phases are persisted in both modes — the orchestrator still
	// executes phases.
	Persist func(taskID string, phases any, tree *plan.EmittedTree) error
	// Execute transitions the task to executing via the existing schedule
	// path (orchestrator.schedule event).
	Execute func(taskID string) error
	// ShouldEmitTree overrides the flat-vs-tree gate. Nil ⇒ the real
	// plan.ShouldEmitTree runs against the compiled phase specs. Tests
	// inject here to pin gate behavior deterministically.
	ShouldEmitTree func(phases any) bool
	// SaveDraft stores draft markdown (plan.draft save). Nil ⇒ error.
	SaveDraft func(taskID, markdown string) error
	// GetDraft fetches draft markdown + version (plan.draft get).
	GetDraft func(taskID string) (markdown string, version int, ok bool)
	// MaxPhases is the compile phase cap. Wired from the strategic planner's
	// existing MaxPlanSteps config sibling (default 10) — reused, not a new
	// knob.
	MaxPhases int
}

// NewPlanSealHandler creates a handler with the core seal-path injection.
func NewPlanSealHandler(src DraftSource, compile func(string, int) (any, string, []string, []CompileProblemView, error), persist func(string, any, *plan.EmittedTree) error, execute func(string) error, maxPhases int) *PlanSealHandler {
	return &PlanSealHandler{
		DraftSource: src,
		Compile:     compile,
		Persist:     persist,
		Execute:     execute,
		MaxPhases:   maxPhases,
	}
}

// RegisterPlanSealMethods registers the draft/seal RPC methods. Distinct
// object from plan.approve (plan-lifecycle on the plan store): these operate
// on the TASK draft.
func (h *PlanSealHandler) RegisterPlanSealMethods(server *Server) {
	server.RegisterHandler("plan.seal", h.handleSeal)
	server.RegisterHandler("plan.draft", h.handleDraft)
}

// HandleSealJSON invokes plan.seal exactly as the RPC dispatcher would
// (raw params in, raw JSON result out).
func (h *PlanSealHandler) HandleSealJSON(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	res, err := h.handleSeal(ctx, params)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("marshal seal result: %w", err)
	}
	return out, nil
}

// HandleDraftJSON invokes plan.draft exactly as the RPC dispatcher would.
func (h *PlanSealHandler) HandleDraftJSON(ctx context.Context, params json.RawMessage) (json.RawMessage, error) {
	res, err := h.handleDraft(ctx, params)
	if err != nil {
		return nil, err
	}
	out, err := json.Marshal(res)
	if err != nil {
		return nil, fmt.Errorf("marshal draft result: %w", err)
	}
	return out, nil
}

func (h *PlanSealHandler) handleSeal(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		TaskID string `json:"task_id"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}
	if h.DraftSource == nil {
		return nil, fmt.Errorf("plan seal service not available")
	}

	markdown, _, ok := h.DraftSource.DraftFor(req.TaskID)
	if !ok {
		return nil, fmt.Errorf("no draft for task %s", req.TaskID)
	}
	if h.Compile == nil || h.Persist == nil || h.Execute == nil {
		return nil, fmt.Errorf("plan seal service not available")
	}

	phases, hash, warnings, problems, err := h.Compile(markdown, h.maxPhases())
	if err != nil {
		return nil, fmt.Errorf("plan compile failed: %w", err)
	}
	if len(problems) > 0 {
		// Compile problems are a RESULT, not an error: the CLI prints them
		// and the draft stays a draft for the next brainstorm round.
		return map[string]any{
			"task_id":  req.TaskID,
			"status":   "problems",
			"problems": problems,
			"warnings": warnings,
		}, nil
	}
	// A compile that returns neither problems nor phases violates the
	// compiler contract — refuse rather than seal an empty plan.
	if specCount(phases) == 0 {
		return nil, fmt.Errorf("plan compile produced no phases and no problems")
	}

	// Flat-vs-tree gate. Tree mode additionally writes the hierarchical
	// tree (master + leaves) as the human/leaf-agent artifact; the flat
	// phases persist in BOTH modes — the orchestrator executes phases, and
	// leaf dispatch is a follow-up tree's integration point.
	var tree *plan.EmittedTree
	mode := "flat"
	if h.shouldEmit(phases) {
		emitted, emitErr := plan.EmitTree(toCompiledPlan(phases, hash), plan.TreeEmitOptions{})
		if emitErr != nil {
			return nil, fmt.Errorf("tree emission failed: %w", emitErr)
		}
		tree = emitted
		mode = "tree"
	}

	if err := h.Persist(req.TaskID, phases, tree); err != nil {
		return nil, fmt.Errorf("failed to persist plan: %w", err)
	}

	// Seal (stamp the hash) only after persistence succeeded, then execute.
	if err := h.DraftSource.SealDraft(req.TaskID, hash); err != nil {
		return nil, fmt.Errorf("failed to seal draft: %w", err)
	}
	if err := h.Execute(req.TaskID); err != nil {
		return nil, fmt.Errorf("failed to execute sealed plan: %w", err)
	}

	return map[string]any{
		"task_id":  req.TaskID,
		"status":   "sealed",
		"hash":     hash,
		"mode":     mode,
		"warnings": warnings,
	}, nil
}

func (h *PlanSealHandler) handleDraft(ctx context.Context, params json.RawMessage) (any, error) {
	var req struct {
		TaskID   string  `json:"task_id"`
		Markdown *string `json:"markdown"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid params: %w", err)
	}
	if req.TaskID == "" {
		return nil, fmt.Errorf("task_id is required")
	}

	// markdown present ⇒ save; absent ⇒ get.
	if req.Markdown != nil {
		if h.SaveDraft == nil {
			return nil, fmt.Errorf("plan draft service not available")
		}
		if err := h.SaveDraft(req.TaskID, *req.Markdown); err != nil {
			return nil, err
		}
		version := 0
		if h.GetDraft != nil {
			if _, v, ok := h.GetDraft(req.TaskID); ok {
				version = v
			}
		}
		return map[string]any{
			"task_id": req.TaskID,
			"status":  "saved",
			"version": version,
		}, nil
	}

	if h.GetDraft == nil {
		return nil, fmt.Errorf("plan draft service not available")
	}
	markdown, version, ok := h.GetDraft(req.TaskID)
	if !ok {
		return nil, fmt.Errorf("no draft for task %s", req.TaskID)
	}
	return map[string]any{
		"task_id":  req.TaskID,
		"status":   "draft",
		"markdown": markdown,
		"version":  version,
	}, nil
}

// maxPhases resolves the cap: injected value when positive, else the
// strategic planner's default (MaxPlanSteps default 10).
func (h *PlanSealHandler) maxPhases() int {
	if h.MaxPhases > 0 {
		return h.MaxPhases
	}
	return 10
}

// specCount counts phases in the compiled shape; 0 for foreign shapes.
func specCount(phases any) int {
	specs, ok := phases.([]plan.PhaseSpec)
	if !ok {
		return 0
	}
	return len(specs)
}

// shouldEmit consults the injected gate or the real plan.ShouldEmitTree.
func (h *PlanSealHandler) shouldEmit(phases any) bool {
	if h.ShouldEmitTree != nil {
		return h.ShouldEmitTree(phases)
	}
	cp := toCompiledPlan(phases, "")
	if cp == nil {
		return false
	}
	return plan.ShouldEmitTree(cp, plan.TreeEmitOptions{})
}

// toCompiledPlan re-wraps []plan.PhaseSpec into a CompiledPlan for the pure
// emitter helpers. Returns nil for foreign shapes.
func toCompiledPlan(phases any, hash string) *plan.CompiledPlan {
	specs, ok := phases.([]plan.PhaseSpec)
	if !ok {
		return nil
	}
	return &plan.CompiledPlan{Phases: specs, Hash: hash}
}
