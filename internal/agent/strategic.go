package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"regexp"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/id"
	"github.com/caimlas/meept/pkg/models"
)

// Sentinel errors for ConductInterview failure paths. Callers use errors.Is
// to distinguish "no interview needed" from infrastructure failures.
var (
	ErrInterviewNotNeeded      = errors.New("interview not needed: ambiguity below threshold or no analysis")
	ErrInterviewNoRegistry     = errors.New("interview skipped: agent registry not available")
	ErrInterviewPlannerMissing = errors.New("interview skipped: planner agent not available")
	ErrInterviewGenerationFail = errors.New("interview skipped: LLM question generation failed")
)

// PlanRequest is the input to the strategic planner.
type PlanRequest struct {
	TaskID    string `json:"task_id"`
	SessionID string `json:"session_id"`
	Input     string `json:"input"`
	Intent    string `json:"intent"`

	// Compound support (Phase 3)
	IsCompound   bool   `json:"is_compound,omitempty"`
	CompoundType string `json:"compound_type,omitempty"`

	// PlanningContext carries verified context from a Prometheus-style interview.
	PlanningCtx *plan.PlanningContext `json:"planning_ctx,omitempty"`

	// TrueAnalysis carries IntentGate-style pre-classification analysis.
	TrueAnalysis *TrueIntentAnalysis `json:"true_analysis,omitempty"`

	// Mode is the complexity-routing mode from Thread D (direct/plan/spec_plan/
	// spec_pair). The strategic planner uses this to short-circuit planning for
	// "direct" mode or select the spec-plan template for "spec_plan".
	Mode string `json:"mode,omitempty"`
}

// plannerStep is the JSON structure expected from the planner LLM output.
type plannerStep struct {
	Description string `json:"description"`
	ToolHint    string `json:"tool_hint,omitempty"`
	DependsOn   []int  `json:"depends_on,omitempty"` // 0-indexed sequence references
}

// plannerOutput is the structured JSON output from the planner agent.
type plannerOutput struct {
	Steps []plannerStep `json:"steps"`
}

// PlanPhaseSpec is a planner-declared phase. Distinct from plan.PlanPhase
// (the persisted record) — this is the LLM's output shape before
// validation/persistence.
type PlanPhaseSpec struct {
	Name        string        `json:"name"`
	Description string        `json:"description"`
	Steps       []plannerStep `json:"steps"`
	Produces    []Artifact    `json:"produces"`
	Consumes    []Artifact    `json:"consumes"`
	DependsOn   []int         `json:"depends_on,omitempty"`
}

// plannerPhaseOutput is the JSON envelope returned by the multi-phase planner
// LLM call. parsePhaseOutput unmarshals into this, then runs validation and
// repair passes.
type plannerPhaseOutput struct {
	Phases []PlanPhaseSpec `json:"phases"`
}

const interviewAmbiguityThreshold = 0.6

// StrategicPlanner decomposes tasks into steps using an LLM planner agent.
type StrategicPlanner struct {
	registry    *AgentRegistry
	taskStore   *task.Store
	stepStore   *task.StepStore
	bus         *bus.MessageBus
	logger      *slog.Logger
	pairManager *PairManager
	routing     *RoutingTable

	maxPlanSteps          int
	plannerTimeout        time.Duration
	approvalStepThreshold int
	simpleInputMaxChars   int
	pairInputMinChars     int
	interviewAmbiguity    float64
	metricsStore          *metrics.Store
	templateLoader        *plannerTemplateLoader
	maxPhases             int
	maxStepsPerPhase      int
	planPhaseSink         func(taskID string, phases []PlanPhaseSpec)
}

// StrategicPlannerConfig holds configuration for the strategic planner.
type StrategicPlannerConfig struct {
	Registry       *AgentRegistry
	TaskStore      *task.Store
	StepStore      *task.StepStore
	Bus            *bus.MessageBus
	Logger         *slog.Logger
	PairManager    *PairManager
	Routing        *RoutingTable
	MaxPlanSteps   int
	PlannerTimeout time.Duration
	// ApprovalStepThreshold is the minimum number of planned steps that
	// triggers the approval gate (even without an interview). Defaults to 5.
	ApprovalStepThreshold int
	// SimpleInputMaxChars is the threshold below which a request is
	// considered "simple" and may skip LLM decomposition. Defaults to 100.
	SimpleInputMaxChars int
	// PairInputMinChars is the threshold above which code/debug requests
	// are routed to pair sessions. Defaults to 200.
	PairInputMinChars int
	// InterviewAmbiguity overrides the hardcoded threshold (default 0.6).
	// 0 means use the default.
	InterviewAmbiguity float64
	// MetricsStore, when non-nil, receives planner outcome metrics.
	MetricsStore *metrics.Store
	// TemplateLoader, if non-nil, supplies markdown-overridable planner
	// prompts. If nil, the planner constructs a default loader.
	TemplateLoader *plannerTemplateLoader
	// MaxPhases caps the number of phases the multi-phase planner may
	// produce. Defaults to 12 when <= 0.
	MaxPhases int
	// MaxStepsPerPhase caps the number of steps per phase in multi-phase
	// planning. Defaults to 8 when <= 0.
	MaxStepsPerPhase int
	// PlanPhaseSink, when non-nil, receives the planner's phase declarations
	// for persistence (e.g., writing to plan.Store). Called after a
	// successful planMultiPhase decomposition.
	PlanPhaseSink func(taskID string, phases []PlanPhaseSpec)
}

// NewStrategicPlanner creates a new strategic planner.
func NewStrategicPlanner(cfg StrategicPlannerConfig) *StrategicPlanner {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.MaxPlanSteps <= 0 {
		cfg.MaxPlanSteps = 10
	}
	if cfg.PlannerTimeout <= 0 {
		cfg.PlannerTimeout = 120 * time.Second
	}
	if cfg.ApprovalStepThreshold <= 0 {
		cfg.ApprovalStepThreshold = 5
	}
	if cfg.SimpleInputMaxChars <= 0 {
		cfg.SimpleInputMaxChars = 100
	}
	if cfg.PairInputMinChars <= 0 {
		cfg.PairInputMinChars = 200
	}
	if cfg.MaxPhases <= 0 {
		cfg.MaxPhases = 12
	}
	if cfg.MaxStepsPerPhase <= 0 {
		cfg.MaxStepsPerPhase = 8
	}
	if cfg.Routing == nil {
		cfg.Routing = NewDefaultRoutingTable()
	}

	// InterviewAmbiguity: 0 falls back to the legacy const (backward compat
	// for callers that don't pass the value via config).
	interviewAmb := cfg.InterviewAmbiguity
	if interviewAmb == 0 {
		interviewAmb = interviewAmbiguityThreshold
	}

	sp := &StrategicPlanner{
		registry:              cfg.Registry,
		taskStore:             cfg.TaskStore,
		stepStore:             cfg.StepStore,
		bus:                   cfg.Bus,
		logger:                cfg.Logger,
		pairManager:           cfg.PairManager,
		routing:               cfg.Routing,
		maxPlanSteps:          cfg.MaxPlanSteps,
		plannerTimeout:        cfg.PlannerTimeout,
		approvalStepThreshold: cfg.ApprovalStepThreshold,
		simpleInputMaxChars:   cfg.SimpleInputMaxChars,
		pairInputMinChars:     cfg.PairInputMinChars,
		interviewAmbiguity:    interviewAmb,
		metricsStore:          cfg.MetricsStore,
		templateLoader:        cfg.TemplateLoader,
		maxPhases:             cfg.MaxPhases,
		maxStepsPerPhase:      cfg.MaxStepsPerPhase,
		planPhaseSink:         cfg.PlanPhaseSink,
	}
	if sp.templateLoader == nil {
		sp.templateLoader = newPlannerTemplateLoader()
		sp.templateLoader.fallbacks["planner/decompose.md"] = defaultDecomposeFallback()
		sp.templateLoader.fallbacks["planner/interview.md"] = defaultInterviewFallback()
		sp.templateLoader.fallbacks["planner/decompose_spec.md"] = defaultDecomposeSpecFallback()
	}
	return sp
}

// ConductInterview determines whether an interview is needed for the given plan
// request. If the TrueAnalysis indicates high ambiguity or broad scope, it uses
// the LLM to generate targeted interview questions. If interview answers are
// already present, it marks the interview as completed and returns.
//
// Returns a PlanningContext that may or may not have InterviewCompleted set to true.
// If the interview is incomplete, the caller should present the questions to the
// user and re-invoke ConductInterview once answers are collected.
func (sp *StrategicPlanner) ConductInterview(ctx context.Context, req PlanRequest) (*plan.PlanningContext, error) {
	// If we already have interview answers, mark as completed and return.
	if req.PlanningCtx != nil && len(req.PlanningCtx.InterviewAnswers) > 0 {
		req.PlanningCtx.InterviewCompleted = true
		sp.logger.Info("Interview completed with answers",
			"task_id", req.TaskID,
			"answer_count", len(req.PlanningCtx.InterviewAnswers),
		)
		return req.PlanningCtx, nil
	}

	// Skip interview if no true analysis or ambiguity is below threshold.
	if req.TrueAnalysis == nil {
		return nil, ErrInterviewNotNeeded
	}
	ambiguityThreshold := sp.interviewAmbiguity
	if ambiguityThreshold == 0 {
		ambiguityThreshold = interviewAmbiguityThreshold
	}
	if req.TrueAnalysis.Ambiguity < ambiguityThreshold && req.TrueAnalysis.Scope != "broad" {
		sp.logger.Debug("Skipping interview: low ambiguity",
			"task_id", req.TaskID,
			"ambiguity", req.TrueAnalysis.Ambiguity,
			"scope", req.TrueAnalysis.Scope,
		)
		return nil, ErrInterviewNotNeeded
	}

	// Need to generate interview questions. Get the planner agent.
	if sp.registry == nil {
		sp.logger.Warn("Agent registry not available for interview, skipping",
			"task_id", req.TaskID,
		)
		return nil, ErrInterviewNoRegistry
	}
	plannerLoop, err := sp.registry.Get(config.AgentIDPlanner)
	if err != nil {
		sp.logger.Warn("Planner agent not available for interview, skipping",
			"task_id", req.TaskID,
			"error", err,
		)
		return nil, ErrInterviewPlannerMissing
	}

	// Build the list of identified ambiguities.
	ambiguityList := "none"
	if len(req.TrueAnalysis.SuggestedQuestions) > 0 {
		ambiguityList = strings.Join(req.TrueAnalysis.SuggestedQuestions, "; ")
	}

	prompt, renderErr := sp.templateLoader.render("planner/interview.md", map[string]any{
		"Request":     req.Input,
		"Goal":        req.TrueAnalysis.Goal,
		"Ambiguity":   req.TrueAnalysis.Ambiguity,
		"Scope":       req.TrueAnalysis.Scope,
		"Category":    req.TrueAnalysis.Category,
		"Confidence":  req.TrueAnalysis.Confidence,
		"Ambiguities": ambiguityList,
	})
	if renderErr != nil {
		return nil, fmt.Errorf("render interview template: %w", renderErr)
	}

	interviewCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()

	conversationID := fmt.Sprintf("interview-%s-%s", req.TaskID, id.Generate(""))
	output, err := plannerLoop.RunOnce(interviewCtx, prompt, conversationID)
	if err != nil {
		sp.logger.Warn("Interview question generation failed, skipping",
			"task_id", req.TaskID,
			"error", err,
		)
		return nil, ErrInterviewGenerationFail
	}

	questions := sp.parseInterviewQuestions(output)
	if len(questions) == 0 {
		// Fallback to suggested questions from TrueAnalysis if LLM failed to produce JSON.
		if len(req.TrueAnalysis.SuggestedQuestions) > 0 {
			questions = req.TrueAnalysis.SuggestedQuestions
		} else {
			questions = []string{"Could you provide more details about what you'd like to accomplish?"}
		}
	}

	// Cap to 4 questions.
	if len(questions) > 4 {
		questions = questions[:4]
	}

	pctx := &plan.PlanningContext{
		InterviewQuestions: questions,
		InterviewCompleted: false,
		TrueGoal:           req.TrueAnalysis.Goal,
		Ambiguities:        req.TrueAnalysis.SuggestedQuestions,
	}

	sp.logger.Info("Interview questions generated",
		"task_id", req.TaskID,
		"question_count", len(questions),
		"ambiguity", req.TrueAnalysis.Ambiguity,
	)

	sp.recordMetric("strategic_planner.interview_triggered", 1, nil)

	return pctx, nil
}

// parseInterviewQuestions extracts question strings from the LLM interview output.
func (sp *StrategicPlanner) parseInterviewQuestions(output string) []string {
	jsonStr := ExtractJSON(output)
	if jsonStr == "" {
		return nil
	}

	var result struct {
		Questions []string `json:"questions"`
	}
	if err := json.Unmarshal([]byte(jsonStr), &result); err != nil {
		sp.logger.Warn("Failed to parse interview questions JSON", "error", err)
		return nil
	}
	return result.Questions
}

// Plan decomposes a task into executable steps.
func (sp *StrategicPlanner) Plan(ctx context.Context, req PlanRequest) error {
	sp.logger.Info("Starting strategic planning",
		"task_id", req.TaskID,
		"session_id", req.SessionID,
		"intent", req.Intent,
		"mode", req.Mode,
	)

	// Set task state to planning
	t, err := sp.taskStore.GetByID(req.TaskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", req.TaskID)
	}
	t.SetState(task.StatePlanning)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task state to planning", "error", err)
	}

	// Copy parent MemoryRefs for context inheritance
	parentMemoryRefs := t.MemoryRefs

	mode := req.Mode
	if mode == "" {
		mode = sp.inferLegacyMode(req)
	}

	var steps []*task.TaskStep

	switch mode {
	case "spec_pair":
		// spec_pair has its own persistence path: the actor step is promoted
		// immediately (reviewer depends on it), no approval gate is applied,
		// and the task.planned event carries pair_session: true. For these
		// reasons it short-circuits the shared tail below.
		if _, pairErr := sp.handlePairSession(ctx, t, req, parentMemoryRefs); pairErr != nil {
			sp.logger.Error("Pair-session handling failed, falling back to direct", "error", pairErr)
			steps = sp.createFallbackSteps(req, parentMemoryRefs)
			// Fall through to shared tail with fallback steps.
		} else {
			// Pair-session path already persisted steps, updated task, and
			// published task.planned + orchestrator.schedule. Return now.
			return nil
		}
	case "direct":
		steps = sp.createFallbackSteps(req, parentMemoryRefs)
	case "plan":
		if sp.shouldInterview(req, mode) && sp.registry != nil {
			pctx, interviewErr := sp.ConductInterview(ctx, req)
			if interviewErr == nil && pctx != nil && !pctx.InterviewCompleted {
				return sp.awaitInterviewAnswers(ctx, t, req, pctx)
			}
			if pctx != nil && pctx.InterviewCompleted {
				req.PlanningCtx = pctx
			}
		}
		steps, err = sp.planSinglePhase(ctx, req)
		if err != nil {
			sp.logger.Warn("Single-phase plan failed, using fallback steps",
				"task_id", req.TaskID, "error", err)
			sp.recordMetric("strategic_planner.fallback", 1, map[string]string{"intent": req.Intent, "reason": "plan_failed"})
			steps = sp.createFallbackSteps(req, parentMemoryRefs)
		}
	case "spec_plan":
		// spec_plan always interviews when a registry is available.
		if sp.registry != nil {
			pctx, interviewErr := sp.ConductInterview(ctx, req)
			if interviewErr == nil && pctx != nil && !pctx.InterviewCompleted {
				return sp.awaitInterviewAnswers(ctx, t, req, pctx)
			}
			if pctx != nil && pctx.InterviewCompleted {
				req.PlanningCtx = pctx
			}
		}
		steps, err = sp.planMultiPhase(ctx, req)
		if err != nil {
			sp.logger.Warn("Multi-phase plan failed, falling back to single-phase",
				"task_id", req.TaskID, "error", err)
			steps, err = sp.planSinglePhase(ctx, req)
			if err != nil {
				sp.logger.Warn("Single-phase fallback failed, using generic fallback",
					"task_id", req.TaskID, "error", err)
				steps = sp.createFallbackSteps(req, parentMemoryRefs)
			}
		}
	default:
		// Unknown mode: degrade to direct-style fallback.
		steps = sp.createFallbackSteps(req, parentMemoryRefs)
	}

	// Inject parent MemoryRefs to first step (if any exist and steps were created)
	if len(steps) > 0 && len(parentMemoryRefs) > 0 {
		for _, ref := range parentMemoryRefs {
			steps[0].AddMemoryRef(ref)
		}
		sp.logger.Info("Copied parent MemoryRefs to first step",
			"task_id", req.TaskID,
			"refs", len(parentMemoryRefs),
		)
	}

	// Approval gate: tasks that went through an interview (but are not yet
	// approved) OR plans that exceed the complexity threshold must be
	// presented to the user for sign-off before execution.
	if sp.requiresApproval(req, steps) {
		return sp.awaitUserApproval(ctx, t, steps, req)
	}

	// Persist steps
	for _, step := range steps {
		if err := sp.stepStore.Create(step); err != nil {
			sp.logger.Error("Failed to persist step", "step_id", step.ID, "error", err)
			return fmt.Errorf("failed to persist steps: %w", err)
		}
	}

	// Generate specification from planned steps and store in task metadata
	spec := GenerateSpecFromSteps(steps)
	StoreSpecInTask(t, spec)
	sp.logger.Info("Generated task spec",
		"task_id", req.TaskID,
		"criteria_count", len(spec.Criteria),
	)

	// Update task job count
	t.TotalJobs = len(steps)
	t.SetState(task.StateExecuting)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task after planning", "error", err)
	}

	// Promote root steps (no dependencies) to ready
	promoted, err := sp.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		sp.logger.Error("Failed to promote ready steps", "error", err)
	} else {
		sp.logger.Info("Promoted root steps to ready",
			"task_id", req.TaskID,
			"promoted", len(promoted),
			"total_steps", len(steps),
		)
	}

	// Publish task.planned event for TUI
	sp.publishEvent("task.planned", map[string]any{
		KeyTaskID:     req.TaskID,
		"session_id":  req.SessionID,
		"total_steps": len(steps),
		"ready_steps": len(promoted),
		"mode":        mode,
	})

	// Publish orchestrator.schedule to trigger tactical scheduling
	sp.publishEvent("orchestrator.schedule", map[string]any{
		KeyTaskID: req.TaskID,
	})

	return nil
}

// handlePairSession wraps the spec_pair flow: create the pair steps,
// persist them, update task state, and publish events. It returns nil steps
// with nil error to signal "already handled" (the caller returns immediately).
// On error the caller falls back to the shared tail using fallback steps.
func (sp *StrategicPlanner) handlePairSession(ctx context.Context, t *task.Task, req PlanRequest, parentMemoryRefs []string) ([]*task.TaskStep, error) {
	sp.logger.Info("Using pair session for task",
		"task_id", req.TaskID,
		"intent", req.Intent,
	)
	pairSteps, pairErr := sp.planPairSession(ctx, req, parentMemoryRefs)
	if pairErr != nil {
		return nil, pairErr
	}

	// Persist steps
	for _, step := range pairSteps {
		if err := sp.stepStore.Create(step); err != nil {
			sp.logger.Error("Failed to persist step", "step_id", step.ID, "error", err)
			return nil, fmt.Errorf("failed to persist steps: %w", err)
		}
	}

	t.TotalJobs = len(pairSteps)
	t.SetState(task.StateExecuting)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task after pair planning", "error", err)
	}

	// Promote actor step to ready (reviewer depends on it)
	promoted, err := sp.stepStore.PromoteReadySteps(req.TaskID)
	if err != nil {
		sp.logger.Error("Failed to promote pair steps", "error", err)
	}

	sp.publishEvent("task.planned", map[string]any{
		KeyTaskID:      req.TaskID,
		"session_id":   req.SessionID,
		"total_steps":  len(pairSteps),
		"ready_steps":  len(promoted),
		"pair_session": true,
		"mode":         "spec_pair",
	})

	sp.publishEvent("orchestrator.schedule", map[string]any{
		KeyTaskID: req.TaskID,
	})

	return pairSteps, nil
}

// inferLegacyMode reconstructs a mode for empty-Mode requests, preserving
// the pre-Thread-D heuristics during rollout. Once all callers populate
// Mode, this becomes dead code.
func (sp *StrategicPlanner) inferLegacyMode(req PlanRequest) string {
	// Compound intents always used pair sessions pre-Thread-D. Both the
	// IsCompound flag (set by handler.publishPlanRequest) and the raw
	// intent string (set by legacy/test callers) trigger spec_pair, matching
	// the old shouldUsePairSession behavior.
	if req.IsCompound || req.Intent == string(IntentCompound) {
		return "spec_pair"
	}
	it := IntentType(req.Intent)
	switch it {
	case IntentChat, IntentRecall, IntentStatus, IntentReport, IntentPlatform, IntentSearch:
		return "direct"
	case IntentPlan, IntentArchitect:
		return "spec_plan"
	default:
		if len(req.Input) < sp.simpleInputMaxChars {
			return "direct"
		}
		return "plan"
	}
}

// shouldInterview decides whether to conduct a design interview before
// decomposition, based on the planning mode. spec_plan always interviews;
// plan interviews only when TrueAnalysis indicates ambiguity above the
// configured threshold; direct/spec_pair never do.
func (sp *StrategicPlanner) shouldInterview(req PlanRequest, mode string) bool {
	switch mode {
	case "spec_plan":
		return true
	case "plan":
		return req.TrueAnalysis != nil &&
			req.TrueAnalysis.Ambiguity >= sp.interviewAmbiguity
	default:
		return false
	}
}

// awaitInterviewAnswers publishes the interview-questions event, stores the
// planning context on the task metadata, and returns nil so Plan returns
// without proceeding to decomposition. The task remains in StatePlanning
// until the caller (TUI/HTTP) re-invokes Plan with answers.
func (sp *StrategicPlanner) awaitInterviewAnswers(ctx context.Context, t *task.Task, req PlanRequest, pctx *plan.PlanningContext) error {
	sp.publishEvent("task.interview", map[string]any{
		KeyTaskID:     req.TaskID,
		"session_id":  req.SessionID,
		"questions":   pctx.InterviewQuestions,
		"ambiguities": pctx.Ambiguities,
	})

	if pctxJSON, err := json.Marshal(pctx); err == nil {
		t.Metadata = json.RawMessage(pctxJSON)
		if err := sp.taskStore.Update(t); err != nil {
			sp.logger.Warn("Failed to update task with planning context", "error", err)
		}
	}

	sp.logger.Info("Interview questions sent, awaiting user answers",
		"task_id", req.TaskID,
		"question_count", len(pctx.InterviewQuestions),
	)
	return nil
}

// planMultiPhase is the multi-phase decomposition entry point. It renders
// the decompose_spec template, calls the planner LLM, parses the output with
// a 4-layer malformed-output defense (ExtractJSON → unmarshal → repair →
// cap), retries once on parse failure, and flattens phases into TaskSteps
// with inter-phase dependencies. On success, the planPhaseSink callback (if
// wired) receives the phase declarations for persistence.
func (sp *StrategicPlanner) planMultiPhase(ctx context.Context, req PlanRequest) ([]*task.TaskStep, error) {
	plannerLoop, err := sp.registry.Get(config.AgentIDPlanner)
	if err != nil {
		return nil, fmt.Errorf("planner agent not available: %w", err)
	}

	// Build context section (same logic as planSinglePhase).
	contextSection := sp.buildContextSection(req)

	prompt, err := sp.templateLoader.render("planner/decompose_spec.md", map[string]any{
		"MaxStepsPerPhase": sp.maxStepsPerPhase,
		"MaxPhases":        sp.maxPhases,
		"ContextSection":   contextSection,
		"Input":            req.Input,
	})
	if err != nil {
		if errors.Is(err, ErrTemplateNotFound) {
			// Template not found on disk and no fallback registered.
			sp.logger.Warn("decompose_spec template not available, falling back to planSinglePhase",
				"task_id", req.TaskID, "error", err,
			)
			return sp.planSinglePhase(ctx, req)
		}
		// Template was found but is malformed (parse error) or has a data
		// mismatch (execute error). This is a configuration error that should
		// not be silently masked by falling back to single-phase.
		sp.logger.Error("decompose_spec template is malformed, aborting multi-phase plan",
			"task_id", req.TaskID, "error", err,
		)
		return nil, fmt.Errorf("render decompose_spec template: %w", err)
	}

	// Append dynamic agent availability hints (same as planSinglePhase).
	if sp.registry != nil {
		if hintSection := BuildPlannerPromptHint(sp.registry); hintSection != "" {
			prompt += "\n\n## Dynamic Agent Availability\n" + hintSection
		}
	}

	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()

	conversationID := fmt.Sprintf("plan-%s-%s", req.TaskID, id.Generate(""))
	output, err := plannerLoop.RunOnce(planCtx, prompt, conversationID)
	if err != nil {
		return nil, fmt.Errorf("planner failed: %w", err)
	}

	parsed, err := parsePhaseOutput(output, sp.maxPhases)
	if err != nil {
		// Layer C: one retry with feedback.
		sp.logger.Warn("phase parse failed, retrying with feedback",
			"task_id", req.TaskID, "error", err)
		retryPrompt := prompt + "\n\nPrevious attempt failed:\n" + err.Error()
		output2, retryErr := plannerLoop.RunOnce(planCtx, retryPrompt, conversationID+"-retry")
		if retryErr != nil {
			return nil, fmt.Errorf("planner retry failed: %w (original: %v)", retryErr, err)
		}
		parsed, err = parsePhaseOutput(output2, sp.maxPhases)
		if err != nil {
			return nil, fmt.Errorf("planner produced malformed phases after retry: %w", err)
		}
	}

	sp.recordMetric("strategic_planner.plan_generated", 1, map[string]string{
		"intent":  req.Intent,
		"outcome": "success",
		"mode":    "spec_plan",
	})

	// Flatten phases into TaskSteps. Each step gets Phase = phase.Name.
	// Inter-phase dependencies: first step of phase N+1 depends on last
	// step of phase N (unless the step already has explicit deps).
	var steps []*task.TaskStep
	var prevPhaseLastStepID string
	for phaseIdx, phase := range parsed.Phases {
		var stepIDsInPhase []string
		for stepIdx, ps := range phase.Steps {
			// Cap per-phase steps.
			if sp.maxStepsPerPhase > 0 && len(stepIDsInPhase) >= sp.maxStepsPerPhase {
				break
			}
			seq := phaseIdx*1000 + stepIdx // stable sequence across phases
			step := task.NewTaskStep(req.TaskID, ps.Description, seq)
			step.ToolHint = ps.ToolHint
			step.Phase = phase.Name
			// Within-phase dependencies (0-indexed → step IDs).
			for _, depIdx := range ps.DependsOn {
				if depIdx >= 0 && depIdx < len(stepIDsInPhase) {
					step.DependsOn = append(step.DependsOn, stepIDsInPhase[depIdx])
				}
			}
			// Inter-phase dependency: first step of phase N+1 depends on
			// last step of phase N (unless this is phase 0 or already has deps).
			if stepIdx == 0 && prevPhaseLastStepID != "" && len(step.DependsOn) == 0 {
				step.DependsOn = append(step.DependsOn, prevPhaseLastStepID)
			}
			steps = append(steps, step)
			stepIDsInPhase = append(stepIDsInPhase, step.ID)
		}
		if len(stepIDsInPhase) > 0 {
			prevPhaseLastStepID = stepIDsInPhase[len(stepIDsInPhase)-1]
		}
	}

	if len(steps) == 0 {
		return nil, fmt.Errorf("planner produced no executable steps")
	}

	sp.recordMetric("strategic_planner.plan_steps", float64(len(steps)), map[string]string{
		"intent": req.Intent, "mode": "spec_plan",
	})

	// Persist phase metadata via sink (if wired).
	if sp.planPhaseSink != nil {
		sp.planPhaseSink(req.TaskID, parsed.Phases)
	}

	sp.logger.Info("Multi-phase plan generated",
		"task_id", req.TaskID,
		"phases", len(parsed.Phases),
		"steps", len(steps),
	)

	return steps, nil
}

// SetPlanPhaseSink registers a callback invoked after multi-phase plan
// generation. The callback receives the planner's phase declarations for
// persistence (e.g., writing to plan.Store). Nil-guarded per CLAUDE.md.
func (sp *StrategicPlanner) SetPlanPhaseSink(fn func(taskID string, phases []PlanPhaseSpec)) {
	if fn == nil {
		return
	}
	sp.planPhaseSink = fn
}

// buildContextSection produces the verified-context block used in planner
// prompts. It incorporates both TrueAnalysis and PlanningContext (interview)
// data.
func (sp *StrategicPlanner) buildContextSection(req PlanRequest) string {
	var contextSection string
	if req.TrueAnalysis != nil {
		var sb strings.Builder
		sb.WriteString("## Verified Context\n")
		if req.TrueAnalysis.Goal != "" {
			sb.WriteString(fmt.Sprintf("True goal: %s\n", req.TrueAnalysis.Goal))
		}
		if req.TrueAnalysis.Scope != "" {
			sb.WriteString(fmt.Sprintf("Scope: %s\n", req.TrueAnalysis.Scope))
		}
		if req.TrueAnalysis.Category != "" {
			sb.WriteString(fmt.Sprintf("Category: %s\n", req.TrueAnalysis.Category))
		}
		contextSection = sb.String()
	}
	if req.PlanningCtx != nil && req.PlanningCtx.InterviewCompleted {
		var sb strings.Builder
		if contextSection != "" {
			sb.WriteString(contextSection)
		} else {
			sb.WriteString("## Verified Context\n")
		}
		if req.PlanningCtx.TrueGoal != "" {
			sb.WriteString(fmt.Sprintf("True goal: %s\n", req.PlanningCtx.TrueGoal))
		}
		if len(req.PlanningCtx.Requirements) > 0 {
			sb.WriteString("Requirements:\n")
			for _, r := range req.PlanningCtx.Requirements {
				sb.WriteString(fmt.Sprintf("- %s\n", r))
			}
		}
		if len(req.PlanningCtx.Constraints) > 0 {
			sb.WriteString("Constraints:\n")
			for k, v := range req.PlanningCtx.Constraints {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
			}
		}
		contextSection = sb.String()
	}
	return contextSection
}

// planSinglePhase uses the planner agent to decompose the request.
// Renamed from generatePlan in Thread D Task 5 to fit the mode-switch nomenclature
// (direct/plan/spec_plan/spec_pair → createFallbackSteps/planSinglePhase/planMultiPhase/planPairSession).
func (sp *StrategicPlanner) planSinglePhase(ctx context.Context, req PlanRequest) ([]*task.TaskStep, error) {
	plannerLoop, err := sp.registry.Get(config.AgentIDPlanner)
	if err != nil {
		return nil, fmt.Errorf("planner agent not available: %w", err)
	}

	// Build planning context section if available
	var contextSection string
	if req.TrueAnalysis != nil {
		var sb strings.Builder
		sb.WriteString("## Verified Context\n")
		if req.TrueAnalysis.Goal != "" {
			sb.WriteString(fmt.Sprintf("True goal: %s\n", req.TrueAnalysis.Goal))
		}
		if req.TrueAnalysis.Scope != "" {
			sb.WriteString(fmt.Sprintf("Scope: %s\n", req.TrueAnalysis.Scope))
		}
		if req.TrueAnalysis.Category != "" {
			sb.WriteString(fmt.Sprintf("Category: %s\n", req.TrueAnalysis.Category))
		}
		contextSection = sb.String()
	}
	if req.PlanningCtx != nil && req.PlanningCtx.InterviewCompleted {
		var sb strings.Builder
		if contextSection != "" {
			sb.WriteString(contextSection)
		} else {
			sb.WriteString("## Verified Context\n")
		}
		if req.PlanningCtx.TrueGoal != "" {
			sb.WriteString(fmt.Sprintf("True goal: %s\n", req.PlanningCtx.TrueGoal))
		}
		if len(req.PlanningCtx.Requirements) > 0 {
			sb.WriteString("Requirements:\n")
			for _, r := range req.PlanningCtx.Requirements {
				sb.WriteString(fmt.Sprintf("- %s\n", r))
			}
		}
		if len(req.PlanningCtx.Constraints) > 0 {
			sb.WriteString("Constraints:\n")
			for k, v := range req.PlanningCtx.Constraints {
				sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
			}
		}
		contextSection = sb.String()
	}

	// Build prompt
	prompt, renderErr := sp.templateLoader.render("planner/decompose.md", map[string]any{
		"MaxSteps":       sp.maxPlanSteps,
		"ContextSection": contextSection,
		"Input":          req.Input,
	})
	if renderErr != nil {
		return nil, fmt.Errorf("render decompose template: %w", renderErr)
	}

	// When a registry is available, append dynamic agent availability hints
	// so the planner LLM knows which specialist agents are actually enabled.
	if sp.registry != nil {
		if hintSection := BuildPlannerPromptHint(sp.registry); hintSection != "" {
			prompt += "\n\n## Dynamic Agent Availability\n" + hintSection
		}
	}

	// Run with timeout
	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()

	conversationID := fmt.Sprintf("plan-%s-%s", req.TaskID, id.Generate(""))
	output, err := plannerLoop.RunOnce(planCtx, prompt, conversationID)
	if err != nil {
		sp.recordMetric("strategic_planner.plan_generated", 1, map[string]string{"intent": req.Intent, "outcome": "failure"})
		return nil, fmt.Errorf("planner failed: %w", err)
	}

	sp.recordMetric("strategic_planner.plan_generated", 1, map[string]string{"intent": req.Intent, "outcome": "success"})

	// Parse JSON output
	steps, parseErr := sp.parsePlanOutput(req.TaskID, output)
	if parseErr != nil {
		return nil, parseErr
	}

	sp.recordMetric("strategic_planner.plan_steps", float64(len(steps)), map[string]string{"intent": req.Intent})
	return steps, nil
}

// parsePlanOutput extracts steps from the planner LLM output.
func (sp *StrategicPlanner) parsePlanOutput(taskID, output string) ([]*task.TaskStep, error) {
	// Try to find JSON in the output (LLM might wrap it in markdown)
	jsonStr := ExtractJSON(output)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in planner output")
	}

	var plan plannerOutput
	if err := json.Unmarshal([]byte(jsonStr), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse plan JSON: %w", err)
	}

	if len(plan.Steps) == 0 {
		return nil, fmt.Errorf("planner returned empty plan")
	}

	// Cap to max steps
	if len(plan.Steps) > sp.maxPlanSteps {
		plan.Steps = plan.Steps[:sp.maxPlanSteps]
	}

	// Convert to TaskStep objects
	steps := make([]*task.TaskStep, len(plan.Steps))
	stepIDs := make([]string, len(plan.Steps))

	// First pass: create steps and collect IDs
	for i, ps := range plan.Steps {
		step := task.NewTaskStep(taskID, ps.Description, i)
		step.ToolHint = ps.ToolHint
		steps[i] = step
		stepIDs[i] = step.ID
	}

	// Second pass: resolve dependency indices to step IDs
	for i, ps := range plan.Steps {
		if len(ps.DependsOn) > 0 {
			deps := make([]string, 0, len(ps.DependsOn))
			for _, depIdx := range ps.DependsOn {
				if depIdx >= 0 && depIdx < len(stepIDs) && depIdx != i {
					deps = append(deps, stepIDs[depIdx])
				} else {
					sp.logger.Debug("filtering invalid dependency index",
						"task_id", taskID,
						"step_index", i,
						"invalid_dep_index", depIdx,
						"valid_range", fmt.Sprintf("[0,%d)", len(stepIDs)),
					)
				}
			}
			steps[i].DependsOn = deps
		}
	}

	sp.logger.Info("Parsed plan output",
		"task_id", taskID,
		"steps", len(steps),
	)

	return steps, nil
}

// createFallbackSteps creates a single step when planning fails. When a
// verified PlanningContext is present, the interview's distilled requirements
// and constraints are prepended to the step description so the executing agent
// has the verified context that was collected during the interview phase.
func (sp *StrategicPlanner) createFallbackSteps(req PlanRequest, parentRefs []string) []*task.TaskStep {
	description := req.Input

	// If a verified PlanningContext with substantive content exists, prepend
	// a "## Verified Context" section so the executing agent doesn't lose the
	// interview results.
	if req.PlanningCtx != nil && req.PlanningCtx.InterviewCompleted {
		pctx := req.PlanningCtx
		hasContent := pctx.TrueGoal != "" || len(pctx.Requirements) > 0 || len(pctx.Constraints) > 0
		if hasContent {
			var sb strings.Builder
			sb.WriteString("## Verified Context\n")
			if pctx.TrueGoal != "" {
				sb.WriteString(fmt.Sprintf("True goal: %s\n", pctx.TrueGoal))
			}
			if len(pctx.Requirements) > 0 {
				sb.WriteString("Requirements:\n")
				for _, r := range pctx.Requirements {
					sb.WriteString(fmt.Sprintf("- %s\n", r))
				}
			}
			if len(pctx.Constraints) > 0 {
				sb.WriteString("Constraints:\n")
				for k, v := range pctx.Constraints {
					sb.WriteString(fmt.Sprintf("- %s: %s\n", k, v))
				}
			}
			sb.WriteString("\n")
			sb.WriteString(description)
			description = sb.String()
		}
	}

	step := task.NewTaskStep(req.TaskID, description, 0)
	step.ToolHint = req.Intent
	// Copy parent refs
	for _, ref := range parentRefs {
		step.AddMemoryRef(ref)
	}
	return []*task.TaskStep{step}
}

// planPairSession creates a pair session for the task instead of
// independent steps. It creates two placeholder steps (actor + reviewer)
// and publishes a pair session creation event.
// Renamed from createPairSessionPlan in Thread D Task 5.
func (sp *StrategicPlanner) planPairSession(ctx context.Context, req PlanRequest, parentMemoryRefs []string) ([]*task.TaskStep, error) {
	if sp.pairManager == nil {
		return nil, fmt.Errorf("pair manager not configured")
	}
	session := sp.pairManager.CreateSession(
		req.TaskID,
		req.Input,
		sp.selectActorAgent(req.Intent),
		sp.selectReviewerAgent(req.Intent),
		DefaultPairMaxRounds,
	)

	// Extract criteria from the input (simple heuristic: split on sentences)
	criteria := sp.extractCriteria(req.Input)
	session.SetCriteria(criteria)

	// Create actor step (first round)
	actorStep := task.NewTaskStep(req.TaskID, fmt.Sprintf("[pair:actor] %s", req.Input), 0)
	actorStep.ToolHint = req.Intent
	actorStep.AgentID = session.ActorAgentID
	for _, ref := range parentMemoryRefs {
		actorStep.AddMemoryRef(ref)
	}
	session.AddStepID(actorStep.ID)

	// Create reviewer step (depends on actor)
	reviewerStep := task.NewTaskStep(req.TaskID, fmt.Sprintf("[pair:reviewer] review %s", req.Input), 1)
	reviewerStep.ToolHint = string(IntentReview)
	reviewerStep.AgentID = session.ReviewerAgentID
	reviewerStep.DependsOn = []string{actorStep.ID}
	for _, ref := range parentMemoryRefs {
		reviewerStep.AddMemoryRef(ref)
	}
	session.AddStepID(reviewerStep.ID)

	sp.logger.Info("Created pair session plan",
		"task_id", req.TaskID,
		"session_id", session.ID,
		"actor", session.ActorAgentID,
		"reviewer", session.ReviewerAgentID,
		"criteria", len(criteria),
	)

	sp.recordMetric("strategic_planner.pair_session", 1, map[string]string{"intent": req.Intent})

	// Publish pair session created event
	sp.publishEvent("pair.session_created", map[string]any{
		KeyTaskID:    req.TaskID,
		"session_id": session.ID,
		"actor":      session.ActorAgentID,
		"reviewer":   session.ReviewerAgentID,
		"max_rounds": session.MaxRounds,
		"criteria":   criteria,
	})

	return []*task.TaskStep{actorStep, reviewerStep}, nil
}

// selectActorAgent chooses the actor agent for a pair session based on intent.
func (sp *StrategicPlanner) selectActorAgent(intent string) string {
	rt := sp.routing
	if rt == nil {
		rt = NewDefaultRoutingTable()
	}
	return rt.ActorFor(intent)
}

// selectReviewerAgent chooses the reviewer agent for a pair session based on intent.
func (sp *StrategicPlanner) selectReviewerAgent(intent string) string {
	rt := sp.routing
	if rt == nil {
		rt = NewDefaultRoutingTable()
	}
	return rt.ReviewerFor(intent)
}

// abbrevRe matches common abbreviations that end with "." but should not be
// treated as sentence boundaries when followed by another sentence. Also
// matches decimal-number prefixes (digit followed by ".").
var abbrevRe = regexp.MustCompile(`(?i)\b(?:e\.g|i\.e|etc|vs|mr|mrs|ms|dr|prof|sr|jr|st|approx|fig|cf|al)\.\s+\S`)

// decimalPointRe matches a digit-prefixed decimal point followed by a digit,
// e.g. "3.14" — should not be treated as a sentence boundary.
var decimalPointRe = regexp.MustCompile(`\d\.\d`)

// sentenceSplitRe matches sentence-ending punctuation followed by whitespace,
// pipe, or newline. This is the primary split pattern used after protecting
// abbreviations and decimal points.
var sentenceSplitRe = regexp.MustCompile(`[.!?]\s+|\||\n`)

// splitSentenceBoundaries splits input on sentence boundaries. It avoids
// splitting on common abbreviations such as "e.g.", "i.e.", "Mr.", "etc." and
// on decimal numbers like "3.14". Pipe and newline always split.
func splitSentenceBoundaries(input string) []string {
	// Protect abbreviations and decimals by replacing their internal ". " with
	// a placeholder before splitting. This is the simplest RE2-compatible way
	// to exclude these patterns without lookahead support.
	protected := input
	protected = abbrevRe.ReplaceAllStringFunc(protected, func(match string) string {
		// Replace the ". " between abbreviation and next word with ".\u00A0"
		// (non-breaking space) so the sentence-split regex won't match it.
		return strings.Replace(match, ". ", ".\u00A0", 1)
	})
	protected = decimalPointRe.ReplaceAllStringFunc(protected, func(match string) string {
		return strings.Replace(match, ".", "\u2009", 1) // thin space as decimal sep
	})

	pieces := sentenceSplitRe.Split(protected, -1)

	// Restore protected characters.
	for i, p := range pieces {
		pieces[i] = strings.ReplaceAll(p, "\u00A0", " ")
		pieces[i] = strings.ReplaceAll(pieces[i], "\u2009", ".")
	}
	return pieces
}

// extractCriteria extracts simple criteria from a task description.
// Splits on sentence boundaries (avoiding common abbreviations) and filters
// trivially short items.
func (sp *StrategicPlanner) extractCriteria(input string) []string {
	// Split on regex sentence boundaries instead of naive ". " which breaks on
	// abbreviations such as "e.g.", "i.e.", and decimals like "3.14".
	pieces := splitSentenceBoundaries(input)

	var criteria []string
	for _, line := range pieces {
		trimmed := strings.TrimSpace(line)
		// Skip headers, empty lines, and trivially short items
		if len(trimmed) < 10 || strings.HasPrefix(trimmed, "#") {
			continue
		}
		criteria = append(criteria, trimmed)
	}

	// If no criteria extracted, use the whole input as one criterion
	if len(criteria) == 0 {
		criteria = []string{input}
	}

	return criteria
}

// ReplanFailedTask re-plans a failed task into smaller steps for retry.
// This is called by the EscalationManager when a task fails and needs to be
// broken down into more manageable pieces.
func (sp *StrategicPlanner) ReplanFailedTask(ctx context.Context, taskID, failureReason string) error {
	sp.logger.Info("Re-planning failed task",
		"task_id", taskID,
		"failure_reason", failureReason,
	)

	if sp.taskStore == nil {
		return fmt.Errorf("task store not available for re-planning")
	}

	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}

	// Get any completed steps to avoid re-doing them
	var completedSteps []*task.TaskStep
	if sp.stepStore != nil {
		var err error
		completedSteps, err = sp.stepStore.ListByTaskID(taskID)
		if err != nil {
			sp.logger.Error("Failed to list steps for re-plan", "error", err)
		}
	}

	// Build remaining work description. Classify the existing steps into
	// completed (successfully terminal) vs. uncompleted so the planner knows
	// what's left to retry or finish.
	var completedDescs, remainingDescs []string
	for _, s := range completedSteps {
		if s.State.IsSuccessfullyTerminal() {
			completedDescs = append(completedDescs, s.Description)
		} else {
			remainingDescs = append(remainingDescs, s.Description)
		}
	}

	// Format completed steps as "do not redo" context.
	completedList := "none"
	if len(completedDescs) > 0 {
		var sb strings.Builder
		for _, d := range completedDescs {
			sb.WriteString(fmt.Sprintf("\n  - %s", d))
		}
		completedList = sb.String()
	}

	// Format remaining (uncompleted) work for the planner. Fall back to the
	// original task objective if no uncompleted steps were recorded.
	remainingList := "re-attempt the original task objective"
	if len(remainingDescs) > 0 {
		var sb strings.Builder
		for _, d := range remainingDescs {
			sb.WriteString(fmt.Sprintf("\n  - %s", d))
		}
		remainingList = sb.String()
	}

	replanDesc := fmt.Sprintf(
		"RE-PLAN: Task '%s' failed with error: %s.\nCompleted steps (do not redo): %s\nRemaining (uncompleted) steps to retry or finish: %s",
		t.Description,
		failureReason,
		completedList,
		remainingList,
	)

	req := PlanRequest{
		TaskID: taskID,
		Input:  replanDesc,
		Intent: string(IntentPlan),
	}

	return sp.Plan(ctx, req)
}

// requiresApproval returns true when the plan must be paused for user sign-off
// before execution. Triggers on either an un-approved completed interview OR a
// plan whose step count exceeds the configured complexity threshold.
func (sp *StrategicPlanner) requiresApproval(req PlanRequest, steps []*task.TaskStep) bool {
	// Interview-completed but not yet user-approved.
	if req.PlanningCtx != nil && req.PlanningCtx.InterviewCompleted && !req.PlanningCtx.UserApproved {
		return true
	}
	// Complexity-based gate: many-step plans need review even without interview.
	if sp.approvalStepThreshold > 0 && len(steps) >= sp.approvalStepThreshold {
		return true
	}
	return false
}

// awaitUserApproval stores the generated plan steps in the task metadata,
// sets the task to StateAwaitingApproval, and publishes a pending_approval
// event so the TUI/transport layer can present the plan to the user.
func (sp *StrategicPlanner) awaitUserApproval(ctx context.Context, t *task.Task, steps []*task.TaskStep, req PlanRequest) error {
	// Build plan summary for the event payload.
	summaries := make([]map[string]string, 0, len(steps))
	for i, step := range steps {
		summaries = append(summaries, map[string]string{
			"sequence":    fmt.Sprintf("%d", i),
			"description": step.Description,
			"tool_hint":   step.ToolHint,
		})
	}

	// Serialize the steps into task metadata under the "pending_steps" key.
	if err := sp.storePendingSteps(t, steps); err != nil {
		sp.logger.Error("Failed to store pending steps for approval", "error", err)
		return fmt.Errorf("failed to store pending steps: %w", err)
	}

	t.TotalJobs = len(steps)
	t.SetState(task.StateAwaitingApproval)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task to awaiting_approval", "error", err)
		return fmt.Errorf("failed to update task state: %w", err)
	}

	sp.logger.Info("Plan awaiting user approval",
		"task_id", req.TaskID,
		"steps", len(steps),
	)

	sp.recordMetric("strategic_planner.approval_gate", 1, nil)

	sp.publishEvent("task.pending_approval", map[string]any{
		KeyTaskID:    req.TaskID,
		"session_id": req.SessionID,
		"steps":      summaries,
		"total":      len(steps),
	})

	return nil
}

// ApprovePlan resumes a plan that was paused at the approval gate.
// It loads the persisted steps, sets UserApproved on the planning context,
// transitions to StateExecuting, and triggers scheduling.
func (sp *StrategicPlanner) ApprovePlan(ctx context.Context, taskID string) error {
	sp.logger.Info("Approving plan", "task_id", taskID)

	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.State != task.StateAwaitingApproval {
		return fmt.Errorf("task %s is in state %q, expected %q", taskID, t.State, task.StateAwaitingApproval)
	}

	// Extract pending steps from task metadata.
	steps, err := sp.extractPendingSteps(t)
	if err != nil {
		return fmt.Errorf("failed to extract pending steps for task %s: %w", taskID, err)
	}
	if len(steps) == 0 {
		return fmt.Errorf("no pending steps found for task %s", taskID)
	}

	// Mark the planning context as user-approved.
	if len(t.Metadata) > 0 {
		var meta map[string]json.RawMessage
		if json.Unmarshal(t.Metadata, &meta) == nil {
			if pctxRaw, ok := meta["planning_context"]; ok {
				var pctx plan.PlanningContext
				if json.Unmarshal(pctxRaw, &pctx) == nil {
					pctx.UserApproved = true
					if updated, err := json.Marshal(pctx); err == nil {
						meta["planning_context"] = updated
					}
				}
			}
			if merged, err := json.Marshal(meta); err == nil {
				t.Metadata = merged
			}
		}
	}

	// Remove the pending_steps key from metadata since steps will be persisted now.
	t.Metadata = removeMetadataKey(t.Metadata, "pending_steps")

	// Persist steps
	for _, step := range steps {
		if err := sp.stepStore.Create(step); err != nil {
			sp.logger.Error("Failed to persist step on approval", "step_id", step.ID, "error", err)
			return fmt.Errorf("failed to persist steps: %w", err)
		}
	}

	// Generate spec from planned steps.
	spec := GenerateSpecFromSteps(steps)
	StoreSpecInTask(t, spec)
	sp.logger.Info("Generated task spec on approval",
		"task_id", taskID,
		"criteria_count", len(spec.Criteria),
	)

	// Transition to executing.
	t.SetState(task.StateExecuting)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task after approval", "error", err)
		return fmt.Errorf("failed to update task: %w", err)
	}

	// Promote root steps (no dependencies) to ready.
	promoted, err := sp.stepStore.PromoteReadySteps(taskID)
	if err != nil {
		sp.logger.Error("Failed to promote ready steps on approval", "error", err)
	} else {
		sp.logger.Info("Promoted root steps after approval",
			"task_id", taskID,
			"promoted", len(promoted),
			"total_steps", len(steps),
		)
	}

	sp.publishEvent("task.approved", map[string]any{
		KeyTaskID:     taskID,
		"total_steps": len(steps),
		"ready_steps": len(promoted),
	})

	sp.publishEvent("orchestrator.schedule", map[string]any{
		KeyTaskID: taskID,
	})

	sp.logger.Info("Plan approved and scheduled", "task_id", taskID)
	return nil
}

// RejectPlan cancels a plan that was awaiting approval.
// It sets the task to StateRejected and publishes a rejection event.
func (sp *StrategicPlanner) RejectPlan(ctx context.Context, taskID string, reason string) error {
	sp.logger.Info("Rejecting plan", "task_id", taskID, "reason", reason)

	t, err := sp.taskStore.GetByID(taskID)
	if err != nil || t == nil {
		return fmt.Errorf("task not found: %s", taskID)
	}
	if t.State != task.StateAwaitingApproval {
		return fmt.Errorf("task %s is in state %q, expected %q", taskID, t.State, task.StateAwaitingApproval)
	}

	// Clean up pending steps from metadata.
	t.Metadata = removeMetadataKey(t.Metadata, "pending_steps")

	t.SetState(task.StateRejected)
	if err := sp.taskStore.Update(t); err != nil {
		sp.logger.Error("Failed to update task after rejection", "error", err)
		return fmt.Errorf("failed to update task: %w", err)
	}

	sp.publishEvent("task.rejected", map[string]any{
		KeyTaskID: taskID,
		"reason":  reason,
	})

	sp.logger.Info("Plan rejected", "task_id", taskID)
	return nil
}

// storePendingSteps serializes steps and stores them in the task metadata under the
// "pending_steps" key, merging with any existing metadata.
func (sp *StrategicPlanner) storePendingSteps(t *task.Task, steps []*task.TaskStep) error {
	pendingStepsData, err := json.Marshal(steps)
	if err != nil {
		return fmt.Errorf("failed to marshal pending steps: %w", err)
	}
	t.Metadata = mergeMetadata(t.Metadata, map[string]json.RawMessage{
		"pending_steps": pendingStepsData,
	})
	return nil
}

// extractPendingSteps deserializes steps from the task's "pending_steps" metadata key.
func (sp *StrategicPlanner) extractPendingSteps(t *task.Task) ([]*task.TaskStep, error) {
	if len(t.Metadata) == 0 {
		return nil, fmt.Errorf("task has no metadata")
	}

	var meta map[string]json.RawMessage
	if err := json.Unmarshal(t.Metadata, &meta); err != nil {
		return nil, fmt.Errorf("failed to parse task metadata: %w", err)
	}

	raw, ok := meta["pending_steps"]
	if !ok {
		return nil, fmt.Errorf("no pending_steps in metadata")
	}

	var steps []*task.TaskStep
	if err := json.Unmarshal(raw, &steps); err != nil {
		return nil, fmt.Errorf("failed to unmarshal pending steps: %w", err)
	}
	return steps, nil
}

// mergeMetadata merges a set of key-value pairs into existing JSON metadata.
// If existing is nil/empty, it creates a fresh object.
func mergeMetadata(existing json.RawMessage, kv map[string]json.RawMessage) json.RawMessage {
	var meta map[string]json.RawMessage
	if len(existing) > 0 {
		if json.Unmarshal(existing, &meta) != nil {
			meta = make(map[string]json.RawMessage)
		}
	} else {
		meta = make(map[string]json.RawMessage)
	}
	maps.Copy(meta, kv)
	merged, err := json.Marshal(meta)
	if err != nil {
		return existing
	}
	return merged
}

// removeMetadataKey removes a single key from the task's JSON metadata map.
func removeMetadataKey(existing json.RawMessage, key string) json.RawMessage {
	if len(existing) == 0 {
		return existing
	}
	var meta map[string]json.RawMessage
	if json.Unmarshal(existing, &meta) != nil {
		return existing
	}
	delete(meta, key)
	merged, err := json.Marshal(meta)
	if err != nil {
		return existing
	}
	return merged
}

func (sp *StrategicPlanner) publishEvent(topic string, data map[string]any) {
	if sp.bus == nil {
		return
	}

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "strategic-planner", data)
	if err != nil {
		sp.logger.Error("Failed to create bus message", "error", err)
		return
	}

	sp.bus.Publish(topic, msg)
}

// recordMetric emits a planner metric when a metrics store is configured.
// All calls are nil-guarded so the planner works without metrics.
func (sp *StrategicPlanner) recordMetric(name string, value float64, tags map[string]string) {
	if sp.metricsStore != nil {
		sp.metricsStore.Record(name, value, tags)
	}
}

// parsePhaseOutput extracts phases from planner LLM output and runs a
// validate-and-repair pass: drop empty phases, cap count, repair invalid
// enum kinds to "file", drop dangling depends_on indices, downgrade dangling
// consumes (references to nothing any phase produces) to optional.
//
// This is the core of the 4-layer malformed-output defense:
//   1. ExtractJSON — strips markdown fences / surrounding prose.
//   2. json.Unmarshal — structural validation.
//   3. Repair pass — normalizes or drops invalid entries.
//   4. Cap — enforces maxPhases ceiling.
//
// Returns an error if no phases survive the repair pass.
func parsePhaseOutput(raw string, maxPhases int) (*plannerPhaseOutput, error) {
	jsonStr := ExtractJSON(raw)
	if jsonStr == "" {
		return nil, fmt.Errorf("no JSON found in phase planner output")
	}
	var out plannerPhaseOutput
	if err := json.Unmarshal([]byte(jsonStr), &out); err != nil {
		return nil, fmt.Errorf("parse phase JSON: %w", err)
	}

	// Repair pass: drop empty phases, repair invalid kinds, remap depends_on.
	// We build an oldIndex→newIndex mapping so that depends_on indices stay
	// valid after phases are dropped.
	filtered := make([]PlanPhaseSpec, 0, len(out.Phases))
	oldToNew := make(map[int]int, len(out.Phases))
	for origIdx, p := range out.Phases {
		// Drop phases with no name AND no steps (LLM cruft).
		if p.Name == "" && len(p.Steps) == 0 {
			continue
		}
		oldToNew[origIdx] = len(filtered)
		// Repair invalid kinds on produces.
		for i := range p.Produces {
			if !p.Produces[i].IsValidKind() {
				p.Produces[i].Kind = "file"
			}
		}
		// Repair invalid kinds on consumes.
		for i := range p.Consumes {
			if !p.Consumes[i].IsValidKind() {
				p.Consumes[i].Kind = "file"
			}
		}
		// Remap depends_on indices: only keep deps that survived filtering,
		// and translate them to the new (filtered) index space.
		remappedDeps := make([]int, 0, len(p.DependsOn))
		for _, idx := range p.DependsOn {
			if newIdx, ok := oldToNew[idx]; ok {
				remappedDeps = append(remappedDeps, newIdx)
			}
		}
		p.DependsOn = remappedDeps
		filtered = append(filtered, p)
	}
	out.Phases = filtered

	// Cap phase count.
	if maxPhases > 0 && len(out.Phases) > maxPhases {
		out.Phases = out.Phases[:maxPhases]
		// Re-validate depends_on after truncation: drop indices that point
		// past the truncated list.
		for i := range out.Phases {
			validDeps := make([]int, 0, len(out.Phases[i].DependsOn))
			for _, idx := range out.Phases[i].DependsOn {
				if idx >= 0 && idx < len(out.Phases) {
					validDeps = append(validDeps, idx)
				}
			}
			out.Phases[i].DependsOn = validDeps
		}
	}

	// Repair dangling consumes: if a consume references a name that no phase
	// produces, downgrade it to optional so checkPhaseReady won't block.
	producedNames := make(map[string]struct{})
	for _, p := range out.Phases {
		for _, a := range p.Produces {
			producedNames[a.Name] = struct{}{}
		}
	}
	for i := range out.Phases {
		for j := range out.Phases[i].Consumes {
			if _, ok := producedNames[out.Phases[i].Consumes[j].Name]; !ok {
				out.Phases[i].Consumes[j].Required = false
			}
		}
	}

	if len(out.Phases) == 0 {
		return nil, fmt.Errorf("planner produced no valid phases")
	}
	return &out, nil
}

// checkPhaseReady returns an error if any required consume is missing from
// the store. Optional consumes are best-effort and never block.
func checkPhaseReady(phase *PlanPhaseSpec, store *artifactStore) error {
	for _, c := range phase.Consumes {
		if c.Required && !store.Has(c.Name) {
			return fmt.Errorf("phase %q requires %q but it wasn't produced",
				phase.Name, c.Name)
		}
	}
	return nil
}

