package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
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

const interviewAmbiguityThreshold = 0.6

// interviewPromptTemplate is the system prompt for generating interview questions
// based on true intent analysis. The LLM generates 2-4 targeted questions about
// scope, constraints, requirements, and ambiguities.
const interviewPromptTemplate = `You are a project planning interviewer. Based on the user's request and intent analysis below, generate 2-4 targeted interview questions to resolve ambiguities before task decomposition.

Your questions should cover:
1. Specific scope boundaries (what is in vs. out of scope)
2. Constraints and preferences (technology, performance, timeline)
3. Priority or ordering of requirements
4. Specific ambiguities identified in the analysis

Rules:
- Generate ONLY valid JSON, no markdown, no explanation
- Keep questions concise and actionable
- Each question should have a clear, specific focus
- Maximum 4 questions, minimum 2

Output format:
{"questions": ["question 1", "question 2", ...]}

Request: %s

Intent analysis:
- Goal: %s
- Ambiguity: %.1f
- Scope: %s
- Category: %s
- Confidence: %.1f
- Identified ambiguities: %s`

const plannerPromptTemplate = `You are a task planner. Decompose the following request into discrete, executable steps.
Each step should be a single unit of work that can be assigned to a specialist agent.

Available tool hints (use these to indicate what kind of agent should handle each step):
- "code" or "refactor" → coding specialist
- "debug" or "fix" → debugging specialist
- "analyze" or "research" → analysis specialist
- "git" or "commit" → git operations specialist
- "plan" → further planning/decomposition
- "chat" → general conversation

Output ONLY valid JSON in this exact format (no markdown, no explanation):
{
  "steps": [
    {"description": "step description", "tool_hint": "code", "depends_on": []},
    {"description": "step description", "tool_hint": "code", "depends_on": [0]},
    {"description": "step description", "tool_hint": "git", "depends_on": [0, 1]}
  ]
}

The "depends_on" field uses 0-based step indices. Steps with empty depends_on can run in parallel.
Keep the plan to %d steps maximum. Be specific and actionable.

%s

Request to decompose:
%s`

// StrategicPlanner decomposes tasks into steps using an LLM planner agent.
type StrategicPlanner struct {
	registry    *AgentRegistry
	taskStore   *task.Store
	stepStore   *task.StepStore
	bus         *bus.MessageBus
	logger      *slog.Logger
	pairManager *PairManager

	maxPlanSteps   int
	plannerTimeout time.Duration
}

// StrategicPlannerConfig holds configuration for the strategic planner.
type StrategicPlannerConfig struct {
	Registry       *AgentRegistry
	TaskStore      *task.Store
	StepStore      *task.StepStore
	Bus            *bus.MessageBus
	Logger         *slog.Logger
	PairManager    *PairManager
	MaxPlanSteps   int
	PlannerTimeout time.Duration
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

	return &StrategicPlanner{
		registry:       cfg.Registry,
		taskStore:      cfg.TaskStore,
		stepStore:      cfg.StepStore,
		bus:            cfg.Bus,
		logger:         cfg.Logger,
		pairManager:    cfg.PairManager,
		maxPlanSteps:   cfg.MaxPlanSteps,
		plannerTimeout: cfg.PlannerTimeout,
	}
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
		return nil, nil
	}
	if req.TrueAnalysis.Ambiguity < interviewAmbiguityThreshold && req.TrueAnalysis.Scope != "broad" {
		sp.logger.Debug("Skipping interview: low ambiguity",
			"task_id", req.TaskID,
			"ambiguity", req.TrueAnalysis.Ambiguity,
			"scope", req.TrueAnalysis.Scope,
		)
		return nil, nil
	}

	// Need to generate interview questions. Get the planner agent.
	if sp.registry == nil {
		sp.logger.Warn("Agent registry not available for interview, skipping",
			"task_id", req.TaskID,
		)
		return nil, nil
	}
	plannerLoop, err := sp.registry.Get(config.AgentIDPlanner)
	if err != nil {
		sp.logger.Warn("Planner agent not available for interview, skipping",
			"task_id", req.TaskID,
			"error", err,
		)
		return nil, nil
	}

	// Build the list of identified ambiguities.
	ambiguityList := "none"
	if len(req.TrueAnalysis.SuggestedQuestions) > 0 {
		ambiguityList = strings.Join(req.TrueAnalysis.SuggestedQuestions, "; ")
	}

	prompt := fmt.Sprintf(interviewPromptTemplate,
		req.Input,
		req.TrueAnalysis.Goal,
		req.TrueAnalysis.Ambiguity,
		req.TrueAnalysis.Scope,
		req.TrueAnalysis.Category,
		req.TrueAnalysis.Confidence,
		ambiguityList,
	)

	interviewCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()

	conversationID := fmt.Sprintf("interview-%s-%d", req.TaskID, time.Now().UnixNano())
	output, err := plannerLoop.RunOnce(interviewCtx, prompt, conversationID)
	if err != nil {
		sp.logger.Warn("Interview question generation failed, skipping",
			"task_id", req.TaskID,
			"error", err,
		)
		return nil, nil
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

	return pctx, nil
}

// parseInterviewQuestions extracts question strings from the LLM interview output.
func (sp *StrategicPlanner) parseInterviewQuestions(output string) []string {
	jsonStr := extractJSON(output)
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

	// Check if this task should use pair sessions instead of normal steps
	if sp.shouldUsePairSession(req) {
		sp.logger.Info("Using pair session for task",
			"task_id", req.TaskID,
			"intent", req.Intent,
		)
		pairSteps, pairErr := sp.createPairSessionPlan(ctx, req, parentMemoryRefs)
		if pairErr != nil {
			sp.logger.Error("Failed to create pair session plan, falling back",
				"task_id", req.TaskID,
				"error", pairErr,
			)
			// Fall through to normal planning
		} else {
			// Persist steps
			for _, step := range pairSteps {
				if err := sp.stepStore.Create(step); err != nil {
					sp.logger.Error("Failed to persist step", "step_id", step.ID, "error", err)
					return fmt.Errorf("failed to persist steps: %w", err)
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
			})

			sp.publishEvent("orchestrator.schedule", map[string]any{
				KeyTaskID: req.TaskID,
			})

			return nil
		}
	}

	var steps []*task.TaskStep

	// Interview phase: if the request has TrueAnalysis with high ambiguity,
	// conduct an interview before decomposition.
	if sp.registry != nil {
		pctx, interviewErr := sp.ConductInterview(ctx, req)
		if interviewErr != nil {
			sp.logger.Warn("Interview failed, proceeding with planning",
				"task_id", req.TaskID,
				"error", interviewErr,
			)
		} else if pctx != nil && !pctx.InterviewCompleted {
			// Interview incomplete: publish event with questions and return early.
			// The task stays in planning state until answers are provided.
			sp.publishEvent("task.interview", map[string]any{
				KeyTaskID:     req.TaskID,
				"session_id":  req.SessionID,
				"questions":   pctx.InterviewQuestions,
				"ambiguities": pctx.Ambiguities,
			})

			// Store planning context on the task metadata so it persists.
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
		} else if pctx != nil {
			// Interview completed: inject the planning context into the request
			// so that generatePlan uses verified context.
			req.PlanningCtx = pctx
			sp.logger.Info("Interview completed, proceeding with verified context",
				"task_id", req.TaskID,
			)
		}
	}

	// Fast-path: simple requests don't need LLM decomposition
	if !sp.shouldDecompose(req) {
		sp.logger.Info("Fast-path: skipping decomposition for simple request",
			"task_id", req.TaskID,
			"intent", req.Intent,
		)
		steps = sp.createFallbackSteps(req, parentMemoryRefs)
	} else {
		// Use planner agent to generate plan
		var err error
		steps, err = sp.generatePlan(ctx, req)
		if err != nil {
			sp.logger.Warn("Plan generation failed, creating single-step fallback",
				"task_id", req.TaskID,
				"error", err,
			)
			steps = sp.createFallbackSteps(req, parentMemoryRefs)
		}
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

	// Approval gate: if the task went through an interview and user has not
	// yet approved, pause here and present the plan for sign-off.
	if req.PlanningCtx != nil && req.PlanningCtx.InterviewCompleted && !req.PlanningCtx.UserApproved {
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
	})

	// Publish orchestrator.schedule to trigger tactical scheduling
	sp.publishEvent("orchestrator.schedule", map[string]any{
		KeyTaskID: req.TaskID,
	})

	return nil
}

// generatePlan uses the planner agent to decompose the request.
func (sp *StrategicPlanner) generatePlan(ctx context.Context, req PlanRequest) ([]*task.TaskStep, error) {
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
	prompt := fmt.Sprintf(plannerPromptTemplate, sp.maxPlanSteps, contextSection, req.Input)

	// Run with timeout
	planCtx, cancel := context.WithTimeout(ctx, sp.plannerTimeout)
	defer cancel()

	conversationID := fmt.Sprintf("plan-%s-%d", req.TaskID, time.Now().UnixNano())
	output, err := plannerLoop.RunOnce(planCtx, prompt, conversationID)
	if err != nil {
		return nil, fmt.Errorf("planner failed: %w", err)
	}

	// Parse JSON output
	return sp.parsePlanOutput(req.TaskID, output)
}

// parsePlanOutput extracts steps from the planner LLM output.
func (sp *StrategicPlanner) parsePlanOutput(taskID, output string) ([]*task.TaskStep, error) {
	// Try to find JSON in the output (LLM might wrap it in markdown)
	jsonStr := extractJSON(output)
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

// createFallbackSteps creates a single step when planning fails.
func (sp *StrategicPlanner) createFallbackSteps(req PlanRequest, parentRefs []string) []*task.TaskStep {
	step := task.NewTaskStep(req.TaskID, req.Input, 0)
	step.ToolHint = req.Intent
	// Copy parent refs
	for _, ref := range parentRefs {
		step.AddMemoryRef(ref)
	}
	return []*task.TaskStep{step}
}

// shouldDecompose returns true if the request warrants LLM-based task decomposition.
// Simple intents and short requests are handled as single-step tasks to avoid
// over-decomposition and unnecessary LLM calls.
func (sp *StrategicPlanner) shouldDecompose(req PlanRequest) bool {
	switch req.Intent {
	case string(IntentChat), string(IntentReport), string(IntentRecall), string(IntentPlatform), string(IntentSearch), string(IntentAnalyze):
		return false
	case string(IntentCompound):
		// Compound intents always need decomposition into sub-tasks
		return true
	}

	// Short requests (<100 chars) without complex action verbs are likely simple
	if len(req.Input) < 100 {
		lower := strings.ToLower(req.Input)

		// Check for complexity indicators that warrant decomposition
		complexityIndicators := []string{
			"and then", "after that", "followed by", "multiple",
			"several", "all of", "each of", "for every",
			"step by step", "steps", "phases",
		}
		for _, indicator := range complexityIndicators {
			if strings.Contains(lower, indicator) {
				return true
			}
		}

		// Simple requests don't need decomposition
		return false
	}

	// If the request was ambiguous enough to need interviewing, probably decompose
	if req.TrueAnalysis != nil && req.TrueAnalysis.Ambiguity >= 0.5 {
		return true
	}

	// If interview context says this is broad scope, decompose
	if req.PlanningCtx != nil && req.PlanningCtx.InterviewCompleted {
		return true
	}

	// Longer requests may benefit from decomposition
	return true
}

// shouldUsePairSession returns true when a task should use the pair session
// model instead of independent step scheduling.
//
// Criteria:
//   - Intent is "code" or "debug" AND the input is complex (>200 chars or
//     contains complexity indicators)
//   - Intent is "compound" (multi-intent tasks always benefit from pairing)
//   - The task name/description contains security-sensitive keywords
func (sp *StrategicPlanner) shouldUsePairSession(req PlanRequest) bool {
	if sp.pairManager == nil {
		return false
	}

	// Compound tasks always use pair sessions
	if req.Intent == string(IntentCompound) {
		return true
	}

	// Code and debug intents with complex descriptions
	switch req.Intent {
	case string(IntentCode), string(IntentDebug):
		if len(req.Input) > 200 {
			return true
		}
		lower := strings.ToLower(req.Input)
		securityIndicators := []string{
			"security", "authentication", "authorization",
			"encryption", "credential", "password", "token",
			"vulnerable", "vulnerability", "cve",
		}
		for _, indicator := range securityIndicators {
			if strings.Contains(lower, indicator) {
				return true
			}
		}
	}

	return false
}

// createPairSessionPlan creates a pair session for the task instead of
// independent steps. It creates two placeholder steps (actor + reviewer)
// and publishes a pair session creation event.
func (sp *StrategicPlanner) createPairSessionPlan(ctx context.Context, req PlanRequest, parentMemoryRefs []string) ([]*task.TaskStep, error) {
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
	switch intent {
	case string(IntentCode), string(IntentCompound):
		return config.AgentIDCoder
	case string(IntentDebug):
		return config.AgentIDDebugger
	default:
		return config.AgentIDCoder
	}
}

// selectReviewerAgent chooses the reviewer agent for a pair session based on intent.
func (sp *StrategicPlanner) selectReviewerAgent(intent string) string {
	switch intent {
	case string(IntentCode), string(IntentCompound):
		return config.AgentIDPlanner
	case string(IntentDebug):
		return config.AgentIDAnalyst
	default:
		return config.AgentIDPlanner
	}
}

// extractCriteria extracts simple criteria from a task description.
// Splits on sentence boundaries and filters trivially short items.
func (sp *StrategicPlanner) extractCriteria(input string) []string {
	// Split on common sentence delimiters
	replacements := []string{". ", "|", "\n"}
	working := input
	for _, r := range replacements {
		working = strings.ReplaceAll(working, r, "\n")
	}

	lines := strings.Split(working, "\n")
	var criteria []string
	for _, line := range lines {
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

	// Build remaining work description
	var completedDescs []string
	for _, s := range completedSteps {
		if s.State.IsSuccessfullyTerminal() {
			completedDescs = append(completedDescs, s.Description)
		}
	}

	replanDesc := fmt.Sprintf(
		"RE-PLAN: Task '%s' failed with error: %s.\nCompleted steps (do not redo): %s\nRemaining work: %s",
		t.Description,
		failureReason,
		fmt.Sprintf("%v", completedDescs),
		t.Description,
	)

	req := PlanRequest{
		TaskID: taskID,
		Input:  replanDesc,
		Intent: string(IntentPlan),
	}

	return sp.Plan(ctx, req)
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
	for k, v := range kv {
		meta[k] = v
	}
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

// extractJSON finds and extracts a JSON object from text that might contain
// markdown code fences or other wrapping.
func extractJSON(s string) string {
	// Try direct parse first
	s = strings.TrimSpace(s)
	if json.Valid([]byte(s)) {
		return s
	}

	// Try to extract from markdown code fence
	if idx := strings.Index(s, "```json"); idx >= 0 {
		start := idx + 7
		end := strings.Index(s[start:], "```")
		if end > 0 {
			candidate := strings.TrimSpace(s[start : start+end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}

	// Try to extract from generic code fence
	if idx := strings.Index(s, "```"); idx >= 0 {
		start := idx + 3
		// Skip language identifier on the same line
		if nl := strings.Index(s[start:], "\n"); nl >= 0 {
			start += nl + 1
		}
		end := strings.Index(s[start:], "```")
		if end > 0 {
			candidate := strings.TrimSpace(s[start : start+end])
			if json.Valid([]byte(candidate)) {
				return candidate
			}
		}
	}

	// Try to find a JSON object by braces
	braceStart := strings.Index(s, "{")
	braceEnd := strings.LastIndex(s, "}")
	if braceStart >= 0 && braceEnd > braceStart {
		candidate := s[braceStart : braceEnd+1]
		if json.Valid([]byte(candidate)) {
			return candidate
		}
	}

	return ""
}
