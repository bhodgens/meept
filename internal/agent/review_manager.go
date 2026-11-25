package agent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/modelrating"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// CompletionStatus represents the result of a completion validation.
type CompletionStatus string

const (
	CompletionValid   CompletionStatus = "valid"
	CompletionInvalid CompletionStatus = "invalid"
	CompletionPartial CompletionStatus = "partial"
)

// ValidationResult holds the result of ValidateCompletion().
type ValidationResult struct {
	Status   CompletionStatus `json:"status"`
	Feedback string           `json:"feedback"`
	Missing  []string         `json:"missing,omitempty"`  // Items not completed
	Verified []string         `json:"verified,omitempty"` // Items verified complete
}

// ReviewManager orchestrates the review process for task steps.
type ReviewManager struct {
	mu               sync.RWMutex
	registry         *AgentRegistry
	stepStore        *task.StepStore
	taskStore        *task.Store
	policy           *ReviewPolicy
	validationPolicy *ValidationPolicy
	bus              *bus.MessageBus
	logger           *slog.Logger

	// sessionStore resolves the ORIGIN of a review step -- the session whose
	// working directory the reviewer lane must run in (issue #137, the
	// code-shaped half). Nil = no session context: the reviewer loop keeps
	// whatever dir it has and the denial log still names the path.
	sessionStore SessionStoreReader

	// metricsStore is the app-level metrics store the blame wiring (C3,
	// leaf 02-trace-stamping) resolves reviewed_model_id through:
	// ResolveReviewedModel(traceID, stepAgentID) reads the implementer's
	// llm_calls rows off the step-scoped trace. Late-wired via
	// SetMetricsStore (the same pattern as SetSessionStore) because the
	// store handle is built before the orchestrator wiring block that
	// constructs this manager. Nil = the payload falls back to the agent's
	// configured model ref alone (the pre-leaf posture).
	metricsStore *metrics.Store
}

// ReviewManagerConfig holds configuration for creating a ReviewManager.
type ReviewManagerConfig struct {
	Registry         *AgentRegistry
	StepStore        *task.StepStore
	TaskStore        *task.Store
	Policy           *ReviewPolicy
	ValidationPolicy *ValidationPolicy
	Bus              *bus.MessageBus
	Logger           *slog.Logger

	// SessionStore is the session origin resolver for review steps
	// (SessionStoreReader: lookup by primary id OR conversation id). It is
	// what lets the reviewer lane resolve the SAME working dir the step job
	// ran in. Nil = the lane keeps its legacy "no dir bound" behavior.
	SessionStore SessionStoreReader
}

// NewReviewManager creates a new review manager.
func NewReviewManager(cfg ReviewManagerConfig) *ReviewManager {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.Policy == nil {
		cfg.Policy = DefaultReviewPolicy()
	}
	if cfg.ValidationPolicy == nil {
		cfg.ValidationPolicy = DefaultValidationPolicy()
	}

	// Wire the registry into the policy so SelectReviewer can do dynamic
	// reviewer-role lookup by reviews_domain.
	cfg.Policy.Registry = cfg.Registry

	return &ReviewManager{
		registry:         cfg.Registry,
		stepStore:        cfg.StepStore,
		taskStore:        cfg.TaskStore,
		policy:           cfg.Policy,
		validationPolicy: cfg.ValidationPolicy,
		bus:              cfg.Bus,
		logger:           cfg.Logger,
		sessionStore:     cfg.SessionStore,
	}
}

// SetSessionStore wires (or replaces) the session origin resolver used to
// resolve a review step's working directory. Nil-safe per the repo setter
// convention: nil keeps the current resolver.
func (rm *ReviewManager) SetSessionStore(store SessionStoreReader) {
	if rm == nil || store == nil {
		return
	}
	rm.mu.Lock()
	rm.sessionStore = store
	rm.mu.Unlock()
}

// SetMetricsStore wires (or replaces) the metrics store used by the C3
// blame wiring (leaf 02-trace-stamping): reviewed_model_id resolves through
// Store.ResolveReviewedModel off the step-scoped trace. Late-armed by the
// daemon (components.go) because the store handle exists before this
// manager but the orchestrator block composes both. Nil-safe per the repo
// setter convention: nil keeps the current store (fallback-only posture).
func (rm *ReviewManager) SetMetricsStore(store *metrics.Store) {
	if rm == nil || store == nil {
		return
	}
	rm.mu.Lock()
	rm.metricsStore = store
	rm.mu.Unlock()
}

// reviewerLoop returns the reviewer agent loop for a review step, scoped to
// the step's task and carrying the resolved working dir. Step-scoping (rather
// than the registry.Get() "_default" bucket) is what keeps two concurrent
// tasks on one daemon from sharing -- and clobbering -- one reviewer dir; and
// the explicit SetWorkingDir is what gives a freshly created task-scoped loop
// its dir at all (createLoop leaves it "" for the old two-phase init, which
// this lane never did).
func (rm *ReviewManager) reviewerLoop(reviewerID string, step *task.TaskStep, workingDir string) (*AgentLoop, error) {
	if rm.registry == nil {
		return nil, fmt.Errorf("review manager has no agent registry")
	}
	loop, err := rm.registry.GetForTask(reviewerID, step.TaskID)
	if err != nil {
		return nil, err
	}
	if workingDir != "" {
		loop.SetWorkingDir(workingDir)
	}
	return loop, nil
}

// resolveStepWorkingDir returns the working directory a review step must run
// in, together with the session field that supplied it:
//
//	WorktreePath > ProjectPath > DetectionContext.CWD
//
// (session.ResolveWorkingDir -- the repo-wide precedence). The step's own
// SessionID hint is tried by primary id and by conversation id (the two
// session keys, cf. handler.go's resolveStepWorkingDir link chain), and an
// unset hint falls back to the FIRST linked session of the step's task, so a
// legacy step with no stamp still resolves the task's session.
//
// Returns ("", WorkingDirFromNone) when nothing resolves. The daemon's own
// process CWD is never consulted (AGENTS.md: os.Getwd is not a sanctioned
// fallback) and no global active-project lookup happens here -- an unbound
// step gets an unbound reviewer, loudly logged by the caller.
func (rm *ReviewManager) resolveStepWorkingDir(step *task.TaskStep) (string, session.WorkingDirSource) {
	if rm == nil || step == nil || rm.sessionStore == nil {
		return "", session.WorkingDirFromNone
	}

	// Snapshot the store under the lock (the setter may swap it); every
	// lookup below runs lock-free, per the mutex-scope rule.
	rm.mu.RLock()
	store := rm.sessionStore
	rm.mu.RUnlock()
	if store == nil {
		return "", session.WorkingDirFromNone
	}

	if step.SessionID != "" {
		sess := store.Get(step.SessionID)
		if sess == nil {
			// Session ids and conversation ids are distinct identifiers
			// (AGENTS.md); a conversation-scoped stamp resolves on the
			// second lookup.
			sess = store.GetByConversationID(step.SessionID)
		}
		if dir, src := session.ResolveWorkingDir(sess); dir != "" {
			return dir, src
		}
	}

	// The step record may carry a stamp the in-memory copy lacks (the
	// tactical scheduler passes its own copy, persisted via SetSessionID):
	// reload the row and retry the stamp when there is one.
	if rm.stepStore != nil && step.ID != "" {
		if fresh, err := rm.stepStore.GetByID(step.ID); err == nil && fresh != nil &&
			fresh.SessionID != "" && fresh.SessionID != step.SessionID {
			sess := store.Get(fresh.SessionID)
			if sess == nil {
				sess = store.GetByConversationID(fresh.SessionID)
			}
			if dir, src := session.ResolveWorkingDir(sess); dir != "" {
				return dir, src
			}
		}
	}

	// No (or unresolvable) step stamp: fall back to the task's linked
	// sessions, in order, first resolvable wins.
	if rm.taskStore != nil && step.TaskID != "" {
		if t, err := rm.taskStore.GetByID(step.TaskID); err == nil && t != nil {
			for _, sessID := range t.LinkedSessions {
				sess := store.Get(sessID)
				if sess == nil {
					sess = store.GetByConversationID(sessID)
				}
				if dir, src := session.ResolveWorkingDir(sess); dir != "" {
					return dir, src
				}
			}
		}
	}

	return "", session.WorkingDirFromNone
}

// ReviewStep initiates review of a completed step.
// ReviewStep runs the full review pipeline for one step. Every TERMINAL
// verdict (approved/rejected/needs-info, from ANY return path) publishes
// step.review_completed via the deferred emit below — bughunt 2026-11-24
// H1/H2: the evidence gate and 8 other early returns previously returned
// before the single tail publish, so the model-rating panel never saw
// gate rejections and pass rates were biased toward LLM-judged steps.
// The two infrastructure-error returns (reviewer unresolvable, RunOnce
// failure) carry (nil, err) and deliberately publish nothing: they are
// not verdicts, and the step's failed state is recorded by the caller.
func (rm *ReviewManager) ReviewStep(ctx context.Context, step *task.TaskStep, spec *TaskSpec) (result *ReviewResult, err error) {
	startTime := time.Now()

	// One deferred emit covers every terminal verdict return path below.
	// (nil, err) infrastructure failures are not verdicts and stay silent.
	defer func() {
		if err == nil && result != nil {
			rm.publishReviewEvent(step.ID, step.TaskID, result, step.RevisionCount)
		}
	}()

	rm.logger.Info("Starting review",
		"step_id", step.ID,
		"task_id", step.TaskID,
		"tool_hint", step.ToolHint,
	)

	// Set step to reviewing state
	if err := rm.stepStore.SetState(step.ID, task.StepReviewing); err != nil {
		rm.logger.Error("Failed to set step to reviewing", "error", err)
	}

	// Snapshot policy under lock to avoid racing with SetPolicy (S1-17).
	rm.mu.RLock()
	policy := rm.policy
	rm.mu.RUnlock()

	// FAIL: Tool execution failed -- nothing meaningful to review. The step
	// must not pass review: an approved error is how "Task completed" stubs
	// lied to users (2026-09-04 finding F2). This gate runs before every
	// policy path (needs-review, auto-approve, heuristic) so an error step
	// can never be laundered into an approval.
	if rm.stepHasError(step) {
		rm.logger.Info("Review rejected: step execution produced an error",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		if err := rm.stepStore.SetState(step.ID, task.StepFailed); err != nil {
			rm.logger.Error("Failed to set error step to failed", "error", err)
		}
		return &ReviewResult{
			Status:     ReviewRejected,
			Feedback:   "Rejected: step execution error -- " + truncateString(firstLine(step.Result), 200),
			Confidence: 1.0,
		}, nil
	}

	// 06-evidence-review (W4, F-NEW-2/4/13): a file-shaped step whose tool
	// ledger shows no successful file-tool execution can never pass review
	// as completed. The frozen p6-positive-control shape: the committer ran
	// 3 steps with zero file tools, all auto-approved, hello.txt unchanged.
	// This gate runs before EVERY policy path (skip-review, auto-approve,
	// trivial-task heuristic) and judges only tool-issued evidence
	// (step.Evidence, fed from tool.execution.complete via tactical.go) --
	// never the model's Claims or narration. Real file evidence falls
	// through untouched (no over-gating); non-file steps are unaffected.
	if verdict := rm.fileEvidenceGate(step, policy); verdict != nil {
		return verdict, nil
	}

	// F9 (2026-09-17 bughunt): the claim-without-evidence guards must run
	// BEFORE the policy.NeedsReview shortcut below. DefaultReviewPolicy
	// skips review for conversational hints ("chat", "report", …), so a
	// conversational step narrating a fabricated tool execution
	// ("extraction ran clean") with zero tool evidence was auto-approved
	// here and heuristicReviewPasses -- where the guards live -- never
	// ran. The guard is cheap and policy-independent: a step CLAIMING a
	// specific tool execution it cannot evidence needs a reviewer even
	// when its hint is on the skip list. Plain conversational answers
	// (no tool-run claims) still fall through to the shortcut and pass.
	if !rm.heuristicGuardsPass(step) {
		// Route to the full reviewer instead of waving the step through.
		// SelectReviewer/buildReviewPrompt need no policy short-circuit;
		// fall through to the normal review flow below (the trivial-task
		// heuristic re-runs the same guards and lands on the same
		// escalate-to-reviewer path, so behavior is consistent).
		rm.logger.Warn("Claim-without-evidence guard fired under a skip-review policy hint; forcing full review",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
	}

	// Check if review is needed based on policy. A file-shaped step with
	// no successful file-tool evidence can never ride this shortcut
	// (06-evidence-review): the gate above either rejected it or forced
	// the full reviewer lane.
	fileGateForced := stepIsFileShaped(step) && !hasSuccessfulFileEvidence(step.Evidence)
	if !policy.NeedsReview(step) && rm.heuristicGuardsPass(step) && !fileGateForced {
		rm.logger.Debug("Step does not require review", "step_id", step.ID)
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   "Auto-approved (no review required)",
			Confidence: 1.0,
		}, nil
	}

	// Check auto-approve patterns (never for a forced file-evidence step)
	if policy.ShouldAutoApprove(step) && !fileGateForced {
		rm.logger.Debug("Step auto-approved", "step_id", step.ID)
		if err := rm.stepStore.SetState(step.ID, task.StepApproved); err != nil {
			rm.logger.Error("Failed to set step to approved", "error", err)
		}
		return &ReviewResult{
			Status:     ReviewApproved,
			Feedback:   "Auto-approved (low-risk change)",
			Confidence: 1.0,
		}, nil
	}

	// Check if human intervention is needed
	if policy.RequiresHumanIntervention(step) {
		rm.logger.Warn("Step requires human intervention",
			"step_id", step.ID,
			"revision_count", step.RevisionCount,
		)
		// User-language failure copy (06-planning/03 T3, F13c): name the
		// attempt count and the next action. Internal vocabulary ("max
		// ralph loop", "revision cycles", "human intervention") never
		// reaches the user.
		feedback := fmt.Sprintf(
			"Could not verify the work after %d attempts - please review the task and either add the missing evidence or adjust the step.",
			policy.MaxRevisionCycles)
		if spec != nil {
			var fb strings.Builder
			for _, c := range spec.Criteria {
				if c.StepSequence == step.Sequence {
					fmt.Fprintf(&fb, " Original acceptance criteria: %s", c.AcceptanceCriteria)
					break
				}
			}
			feedback += fb.String()
		}
		return &ReviewResult{
			Status:     ReviewNeedsInfo,
			Feedback:   feedback,
			Confidence: 1.0,
		}, nil
	}

	// FAIL backstop: Tool execution failed -- nothing meaningful to review.
	// Unreachable when the pre-policy gate above fires, but kept as a
	// hard guarantee: no code path in ReviewStep may return ReviewApproved
	// for an error step (2026-09-04 finding F2).
	if rm.stepHasError(step) {
		rm.logger.Info("Review rejected: step execution produced an error",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		if err := rm.stepStore.SetState(step.ID, task.StepFailed); err != nil {
			rm.logger.Error("Failed to set error step to failed", "error", err)
		}
		return &ReviewResult{
			Status:     ReviewRejected,
			Feedback:   "Rejected: step execution error -- " + truncateString(firstLine(step.Result), 200),
			Confidence: 1.0,
		}, nil
	}

	// SKIP: Trivial task -- fewer than 3 steps in the task means low
	// complexity where LLM review cost outweighs benefit. Use a
	// lightweight heuristic check instead.
	if rm.isTrivialTask(step) && !fileGateForced {
		rm.logger.Info("Skipping full review: trivial task (<3 steps), using heuristic",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		// Leaf 02 (20261121-interactive-usability) honesty gate, order
		// pinned by the contract "errored steps never pass review": the
		// error check runs FIRST, before the machine-shape check and the
		// heuristic, so an errored step can never ride the trivial bypass
		// to an approval. Live defect (probe B, task-...0007): the guard
		// nudged an unbacked-claim turn, the model replied with literal
		// tool-call JSON, and THIS heuristic approved it as completed.
		if rm.stepHasError(step) {
			rm.logger.Info("Trivial-task heuristic: step execution produced an error; rejecting",
				"step_id", step.ID,
				"tool_hint", step.ToolHint,
			)
			if err := rm.stepStore.SetState(step.ID, task.StepFailed); err != nil {
				rm.logger.Error("Failed to set error step to failed", "error", err)
			}
			return &ReviewResult{
				Status:     ReviewRejected,
				Feedback:   "Rejected: step execution error -- " + truncateString(firstLine(step.Result), 200),
				Confidence: 1.0,
			}, nil
		}
		// Machine-shaped-result gate: a result that is raw tool-call/tool-
		// result JSON (the model narrated its tool calls as text) is not an
		// answer a human can act on. Route it to the full reviewer instead
		// of auto-approving (the detector reuses the reply guard's shapes;
		// see stepResultIsMachineShaped).
		if stepResultIsMachineShaped(step.Result) {
			rm.logger.Warn("Trivial-task heuristic: result is machine-shaped tool JSON; escalating to full review",
				"step_id", step.ID,
				"tool_hint", step.ToolHint,
			)
			// Fall through to the full reviewer below -- do NOT reject:
			// a reviewer may still salvage legitimate work narrated in an
			// ugly shape. A truly short/vacuous dump stays a rejection.
			if len(strings.TrimSpace(step.Result)) < 40 {
				return &ReviewResult{
					Status:     ReviewRejected,
					Feedback:   "Heuristic check failed: result is raw tool-call JSON, not a user-facing answer",
					Confidence: 0.7,
				}, nil
			}
		} else if rm.heuristicReviewPasses(step) {
			if err := rm.stepStore.SetState(step.ID, task.StepApproved); err != nil {
				rm.logger.Error("Failed to set step to approved", "error", err)
			}
			// F1 (2026-09-12 bughunt): SetState writes the DB only; the
			// in-memory step still carries its pre-review state, and the
			// full-row Update below writes `state = step.State`
			// (internal/task/step.go) -- reverting the approval to a
			// non-terminal state so AreAllCompleted never fires. Assign
			// the state before the Update (the 2026-09-06 fix shape the
			// rejection path already uses).
			step.State = task.StepApproved
			// Mark the step validated only when there is tool-issued
			// evidence for the approval to stand on (F11): a heuristic
			// pass over a step with no evidence verifies nothing, so the
			// record stays Validated=false rather than claiming a
			// verification that never happened. An existing unverified
			// marker is never cleared here.
			if !step.Validated && hasMeaningfulEvidence(step.Evidence) {
				step.Validated = true
			}
			if err := rm.stepStore.Update(step); err != nil {
				rm.logger.Warn("Failed to persist heuristic approval state",
					"step_id", step.ID, "error", err)
			}
			return &ReviewResult{
				Status:     ReviewApproved,
				Feedback:   "Auto-approved (heuristic check: trivial task, non-empty result)",
				Confidence: 1.0,
			}, nil
		}
		// Guard failure vs. meaningless content (sweep e2e, 2026-09-15/16):
		// the guard refusals (artifact/tool-execution claims with no
		// captured evidence) describe SUSPICION, not emptiness. A coder
		// that wrote the file and documented "go test -- exit code 0" was
		// rejected outright here because shell-run verification produces
		// no file-tool evidence events. Suspicion must go to the full
		// reviewer, not reject the step. Only a truly vacuous result
		// (whitespace/short) stays a heuristic rejection.
		if len(strings.TrimSpace(step.Result)) < 40 {
			return &ReviewResult{
				Status:     ReviewRejected,
				Feedback:   "Heuristic check failed: non-empty result but no meaningful content",
				Confidence: 0.7,
			}, nil
		}
		rm.logger.Info("Heuristic guard flagged the result; escalating to full review",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		// Fall through to the full reviewer below -- do NOT reject.
	}

	// Select reviewer agent
	reviewerID := policy.SelectReviewer(step)
	rm.logger.Debug("Selected reviewer",
		"step_id", step.ID,
		"reviewer", reviewerID,
	)

	// Build review prompt
	prompt := rm.buildReviewPrompt(step, spec)

	// Resolve the working dir the reviewer lane runs in (issue #137): the
	// step's session origin, WorktreePath > ProjectPath > DetectionContext.CWD.
	// Before this, registry.Get() handed back the "_default" loop whose dir is
	// "" -- the reviewer's file_read fell through to whatever the daemon CWD
	// was and a fence denial named no path, which is exactly the
	// unattributable WARN captured on the scratch rig.
	workingDir, wdSource := rm.resolveStepWorkingDir(step)
	if workingDir == "" {
		rm.logger.Warn("review lane has no working directory; the reviewer will require explicit paths",
			"step_id", step.ID,
			"task_id", step.TaskID,
			"session_id", step.SessionID,
			"source", string(wdSource),
		)
	}

	// Get reviewer agent loop, SCOPED TO THE STEP: the reviewer must not share
	// the registry's "_default" loop with every other task on this daemon (a
	// full suite runs many tasks through one process, and whichever dir was
	// stamped last won).
	reviewerLoop, err := rm.reviewerLoop(reviewerID, step, workingDir)
	if err != nil {
		// Fail CLOSED: an unresolvable reviewer must never auto-approve.
		// The old auto-approve fallback defeated the F9 guard whenever
		// the reviewer agent spec was missing (e.g. an empty registry on
		// a fresh checkout) -- a guard-flagged step sailed through as
		// approved. Surface the error like the RunOnce failure below.
		rm.logger.Error("Failed to get reviewer agent", "reviewer", reviewerID, "error", err)
		return nil, fmt.Errorf("reviewer %s not available: %w", reviewerID, err)
	}
	rm.logger.Info("Review lane working dir",
		"step_id", step.ID,
		"task_id", step.TaskID,
		"reviewer", reviewerID,
		"working_dir", workingDir,
		"source", string(wdSource),
	)

	// Run reviewer agent
	output, err := reviewerLoop.RunOnce(ctx, prompt, step.ID)
	if err != nil {
		rm.logger.Error("Reviewer agent failed", "error", err)
		return nil, fmt.Errorf("reviewer agent failed: %w", err)
	}

	// Parse review result (named result: the deferred emit publishes it).
	result = rm.parseReviewResult(output)
	// 06-evidence-review: the forced reviewer may reject or ask for info,
	// but it can never APPROVE a file-shaped step whose ledger still
	// shows no successful file-tool execution -- the reviewer's narration
	// is not tool-issued evidence. Convert such an approval into the
	// deterministic actionable rejection.
	if result.Status == ReviewApproved && fileGateForced {
		feedback := fmt.Sprintf(
			"Rejected: no file operation was executed for this step -- the tool ledger shows zero successful file-tool executions, "+
				"so the requested file change did not happen. Task: %s. "+
				"Execute the file edit or write (for example file_edit/file_write on the named file), "+
				"then verify the file actually changed before reporting completion.",
			strings.TrimSpace(step.Description))
		result = &ReviewResult{Status: ReviewRejected, Feedback: feedback, Confidence: 1.0}
	}
	result.ReviewerID = reviewerID
	result.Duration = time.Since(startTime)

	rm.logger.Info("Review completed",
		"step_id", step.ID,
		"status", result.Status,
		"confidence", result.Confidence,
		"duration", result.Duration,
	)

	// Publish review event happens via the deferred emit at function entry
	// (bughunt 2026-11-24 H1/H2): it covered ONLY this tail path before,
	// leaving the gate and 8 early-return paths unpublished.

	return result, nil
}

// buildReviewPrompt creates a review prompt for a step.
func (rm *ReviewManager) buildReviewPrompt(step *task.TaskStep, spec *TaskSpec) string {
	var sb strings.Builder

	sb.WriteString("REVIEW TASK STEP\n\n")
	fmt.Fprintf(&sb, "Step ID: %s\n", step.ID)
	fmt.Fprintf(&sb, "Description: %s\n", step.Description)
	fmt.Fprintf(&sb, "Tool Hint: %s\n", step.ToolHint)
	fmt.Fprintf(&sb, "Agent: %s\n", step.AgentID)
	fmt.Fprintf(&sb, "Result:\n%s\n\n", step.Result)

	sb.WriteString("Your task is to review this work for:\n")
	sb.WriteString("- Correctness: Does the work accomplish what was described?\n")
	sb.WriteString("- Quality: Is the work well-executed and follows best practices?\n")
	sb.WriteString("- Completeness: Is anything missing that should be included?\n")
	sb.WriteString("- Safety: Are there any security issues or potential problems?\n\n")

	sb.WriteString("Respond with a structured review in this JSON format:\n")
	sb.WriteString(`{"status": "approved" | "rejected" | "needs_info", `)
	sb.WriteString(`"feedback": "explanation", `)
	sb.WriteString(`"issues": ["issue1", "issue2"], `)
	sb.WriteString(`"confidence": 0.0-1.0}\n\n`)

	// M5: ask (optionally) for the rubric verdicts the emission path already
	// understands; defect grades only, never a "pass" verdict row.
	sb.WriteString("OPTIONAL: include a \"verdicts\" array grading specific defects. Each verdict: {\"rubric_id\": \"catalog id or free string\", \"scope\": \"model|workflow\", \"category\": one defect grade of wrong_language, wrong_api_or_dependency, logic_error, security_defect, incomplete_implementation, unrequested_change, build_or_test_failure, style_bloat, convention_violation (never \"pass\"), \"severity\": \"cosmetic|functional|blocking|critical\", \"detail\": \"one sentence\", \"confidence\": 0.0-1.0}. Omit verdicts entirely when the work is clean.\n\n")

	sb.WriteString("If approving, keep feedback brief. If rejecting, provide specific actionable feedback.\n")

	// Include acceptance criteria from spec if available
	if spec != nil {
		for _, c := range spec.Criteria {
			if c.StepSequence == step.Sequence {
				sb.WriteString("\nACCEPTANCE CRITERIA (you MUST check these):\n")
				sb.WriteString(c.AcceptanceCriteria)
				sb.WriteString("\n\nEvaluate the work specifically against these criteria.\n")
				break
			}
		}
	}

	return sb.String()
}

// parseReviewResult extracts the review decision from LLM output.
func (rm *ReviewManager) parseReviewResult(output string) *ReviewResult {
	result := &ReviewResult{
		Status:     ReviewApproved, // Default to approve
		Feedback:   "No explicit feedback provided",
		Confidence: 0.5,
	}

	// Try to extract JSON from the output using multiple strategies
	jsonStr := rm.extractReviewJSON(output)
	jsonParsed := false

	if jsonStr != "" {
		var parsed struct {
			Status     string   `json:"status"`
			Feedback   string   `json:"feedback"`
			Issues     []string `json:"issues"`
			Confidence float64  `json:"confidence"`
			// M5: the reviewer's optional rubric grades. Same element shape
			// as the frozen C4 wire (verdictWireShape); absent/null keeps
			// the legacy verdict-free posture.
			Verdicts []verdictWireShape `json:"verdicts"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &parsed); err == nil {
			jsonParsed = true
			switch strings.ToLower(parsed.Status) {
			case "approved", "approve", "pass", "lgtm":
				result.Status = ReviewApproved
			case "rejected", "reject", "fail":
				result.Status = ReviewRejected
			case "needs_info", "needsinfo", "needs-info", "info":
				result.Status = ReviewNeedsInfo
			}

			if parsed.Feedback != "" {
				result.Feedback = parsed.Feedback
			}
			if len(parsed.Issues) > 0 {
				result.Issues = parsed.Issues
			}
			if parsed.Confidence > 0 {
				result.Confidence = parsed.Confidence
			}
			result.Verdicts = parseReviewerVerdicts(rm.logger, parsed.Verdicts)
		}
	}

	// Fallback: analyze text for decision ONLY if JSON parsing failed
	// Use phrase matching to avoid false positives from negations
	if !jsonParsed {
		result.Status, result.Confidence = rm.analyzeReviewText(output)
	}

	// Extract feedback from non-JSON parts if needed
	if result.Feedback == "No explicit feedback provided" && output != "" {
		feedback := output
		if jsonStr != "" {
			feedback = strings.ReplaceAll(output, jsonStr, "")
		}
		// Remove markdown code fences
		feedback = regexp.MustCompile("```[\\s\\S]*?```").ReplaceAllString(feedback, "")
		feedback = strings.TrimSpace(feedback)
		if len(feedback) > 500 {
			feedback = feedback[:500] + "..."
		}
		if feedback != "" {
			result.Feedback = feedback
		}
	}

	return result
}

// extractReviewJSON attempts to extract valid JSON containing a status field from output.
func (rm *ReviewManager) extractReviewJSON(output string) string {
	// Strategy 1: Check if the entire output is valid JSON
	trimmed := strings.TrimSpace(output)
	if strings.HasPrefix(trimmed, "{") && strings.HasSuffix(trimmed, "}") {
		if json.Valid([]byte(trimmed)) && strings.Contains(trimmed, `"status"`) {
			return trimmed
		}
	}

	// Strategy 2: Extract from markdown code fence
	codeBlockPattern := regexp.MustCompile("```(?:json)?\\s*\\n?([\\s\\S]*?)\\n?```")
	if matches := codeBlockPattern.FindStringSubmatch(output); len(matches) > 1 {
		candidate := strings.TrimSpace(matches[1])
		if json.Valid([]byte(candidate)) && strings.Contains(candidate, `"status"`) {
			return candidate
		}
	}

	// Strategy 3: Find JSON object by balanced braces
	start := strings.Index(output, "{")
	if start >= 0 {
		depth := 0
	braceSearch:
		for i := start; i < len(output); i++ {
			switch output[i] {
			case '{':
				depth++
			case '}':
				depth--
				if depth == 0 {
					candidate := output[start : i+1]
					if json.Valid([]byte(candidate)) && strings.Contains(candidate, `"status"`) {
						return candidate
					}
					break braceSearch
				}
			}
		}
	}

	return ""
}

// analyzeReviewText performs text analysis to determine review status.
// It uses phrase matching to avoid false positives from negations.
func (rm *ReviewManager) analyzeReviewText(output string) (result ReviewStatus, f float64) {
	lower := strings.ToLower(output)

	// Check for explicit rejection phrases (high confidence)
	rejectionPhrases := []string{
		"i reject",
		"this is rejected",
		"status: rejected",
		"my verdict is reject",
		"decision: reject",
		"must be rejected",
		"should be rejected",
		"needs revision",
		"requires revision",
		"cannot approve",
		"cannot be approved",
		"do not approve",
		"fails review",
		"review: fail",
	}
	for _, phrase := range rejectionPhrases {
		if strings.Contains(lower, phrase) {
			return ReviewRejected, 0.8
		}
	}

	// Check for explicit approval phrases (high confidence)
	approvalPhrases := []string{
		"i approve",
		"this is approved",
		"status: approved",
		"my verdict is approve",
		"decision: approve",
		"looks good",
		"lgtm",
		"passes review",
		"review: pass",
	}
	for _, phrase := range approvalPhrases {
		if strings.Contains(lower, phrase) {
			return ReviewApproved, 0.8
		}
	}

	// Check for needs_info phrases
	needsInfoPhrases := []string{
		"need more info",
		"needs more information",
		"unclear",
		"please clarify",
		"cannot determine",
	}
	for _, phrase := range needsInfoPhrases {
		if strings.Contains(lower, phrase) {
			return ReviewNeedsInfo, 0.7
		}
	}

	// Default to approved with low confidence if no clear signal
	return ReviewApproved, 0.3
}

// HandleReviewResult processes a review result and updates step state.
// Returns any newly-created revision step(s) so that callers with scheduling
// responsibilities (e.g. TacticalScheduler) can enqueue them.
func (rm *ReviewManager) HandleReviewResult(ctx context.Context, stepID string, result *ReviewResult, spec *TaskSpec) ([]*task.TaskStep, error) {
	step, err := rm.stepStore.GetByID(stepID)
	if err != nil {
		return nil, fmt.Errorf("failed to get step: %w", err)
	}
	if step == nil {
		return nil, fmt.Errorf("step not found: %s", stepID)
	}

	var revisions []*task.TaskStep
	// outErr accumulates errors from non-fatal SetResult calls (S1-10).
	var outErr error

	// Persist the verdict beside the step (tiered-iteration leaf 03): the
	// ReviewResult was previously ephemeral (bus event only), so the
	// critic's evidence assembler had nothing to read back per session.
	// Every terminal verdict -- including the auto-approval paths -- is
	// written; the reason is bounded at write time (reviewReasonMaxChars)
	// by SetReviewVerdict. A write failure is non-fatal (logged, surfaced
	// through outErr alongside the other non-fatal SetResult failures) so
	// review gating behavior is unchanged.
	if err := rm.stepStore.SetReviewVerdict(stepID, string(result.Status), result.Feedback); err != nil {
		rm.logger.Error("Failed to persist review verdict",
			"step_id", stepID,
			"status", string(result.Status),
			"error", err,
		)
		outErr = errors.Join(outErr, fmt.Errorf("failed to persist review verdict: %w", err))
	}

	switch result.Status {
	case ReviewApproved:
		// Mark as approved (terminal state)
		if err := rm.stepStore.SetState(step.ID, task.StepApproved); err != nil {
			return nil, fmt.Errorf("failed to set approved state: %w", err)
		}
		// F1 (2026-09-12 bughunt): SetState writes the DB only; the step
		// was loaded while still 'reviewing' (SetState at ReviewStep), so
		// the full-row Update below would serialize that stale state back
		// over the approval -- leaving the step non-terminal and the task
		// unfinalizable. Assign the state FIRST (the 2026-09-06 fix shape
		// the rejection path below already uses).
		step.State = task.StepApproved
		// Reviewer approval IS the platform's judgment that the step is
		// done (e2e run 13, 2026-09-11) -- but only stamp Validated when
		// there is tool-issued evidence for it (F11). An approval with no
		// evidence verifies nothing, so the record keeps Validated=false;
		// a standing unverified marker (ValidationError) is never cleared
		// by an approval.
		if !step.Validated && hasMeaningfulEvidence(step.Evidence) {
			step.Validated = true
		}
		if err := rm.stepStore.Update(step); err != nil {
			rm.logger.Warn("failed to persist approval state", "step_id", step.ID, "error", err)
		}
		rm.logger.Info("Step approved", "step_id", step.ID, "feedback", result.Feedback)

		// Promote dependent steps
		promoted, err := rm.stepStore.PromoteReadySteps(step.TaskID)
		if err != nil {
			rm.logger.Error("Failed to promote ready steps", "error", err)
		} else if len(promoted) > 0 {
			rm.logger.Info("Promoted dependent steps after approval",
				"count", len(promoted),
				"task_id", step.TaskID,
			)
		}

	case ReviewRejected:
		// Execution-error gate: the step is already failed (terminal).
		// A revision depending on a failed step can never be promoted
		// (IsSuccessfullyTerminal blocks on failed deps), so revising is
		// pointless -- keep the failure and record the feedback only.
		if step.State == task.StepFailed {
			rm.logger.Warn("Step rejected after execution error; not creating revision",
				"step_id", step.ID,
			)
			if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
				rm.logger.Error("Failed to set failure feedback", "error", err)
				outErr = fmt.Errorf("failed to set failure feedback: %w", err)
			}
			break
		}

		// Reject + bump revision count in ONE full-row write. The previous
		// two-step sequence (SetState(rejected) then Update(step) with the
		// stale in-memory state) wrote "reviewing" back over "rejected" and
		// left the revision step depending on a permanently non-terminal
		// original -- revisions never scheduled (2026-09-06 live-run finding).
		step.IncrementRevision()
		step.State = task.StepRejected
		if err := rm.stepStore.Update(step); err != nil {
			// AGENT-23 FIX: Return the error so callers that manage the
			// scheduling lifecycle (e.g. TacticalScheduler) can observe
			// revision-count failures and surface them to the operator.
			return nil, fmt.Errorf("failed to persist rejected state: %w", err)
		}
		if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			rm.logger.Error("Failed to set rejection feedback", "error", err)
			outErr = fmt.Errorf("failed to set rejection feedback: %w", err)
		}
		rm.logger.Info("Step rejected", "step_id", step.ID, "issues", result.Issues)

		// Create revision step with feedback context
		revisionContext := BuildRevisionContext(result, spec)
		revision := task.CreateRevisionWithContext(step, result.Feedback, revisionContext)
		if err := rm.stepStore.Create(revision); err != nil {
			rm.logger.Error("Failed to create revision step", "error", err)
		} else {
			rm.logger.Info("Created revision step",
				"revision_id", revision.ID,
				"original_id", step.ID,
				"revision_count", step.RevisionCount,
			)
			revisions = append(revisions, revision)

			// Update task TotalJobs to include the new revision step.
			// Atomic increment (A-08 pattern): the previous Get→mutate→Update
			// sequence lost increments when a concurrent step completion
			// wrote back a stale counter snapshot.
			if rm.taskStore != nil {
				if err := rm.taskStore.IncrementTotalJobs(step.TaskID); err != nil {
					rm.logger.Error("Failed to update task TotalJobs for revision", "error", err)
				}
			}
		}

	case ReviewNeedsInfo:
		// Keep in reviewing state, update result with feedback
		if err := rm.stepStore.SetResult(step.ID, result.Feedback); err != nil {
			rm.logger.Error("Failed to set needs_info feedback", "error", err)
			outErr = fmt.Errorf("failed to set needs_info feedback: %w", err)
		}
		rm.logger.Info("Step needs more info", "step_id", step.ID)
	}

	return revisions, outErr
}

// verdictWireShape is the FROZEN wire element for one verdict on the
// review payloads (model-rating-metrics-plan C4). modelrating.Verdict has
// no JSON tags of its own (the C1 catalog struct is store-facing), so the
// emitter maps onto these snake_case keys explicitly — marshaling the Go
// field names instead is a wire defect the gate pins.
type verdictWireShape struct {
	RubricID   string  `json:"rubric_id"`
	Scope      string  `json:"scope"`
	Category   string  `json:"category"`
	Severity   string  `json:"severity"`
	Detail     string  `json:"detail"`
	Confidence float64 `json:"confidence"`
}

// parseReviewerVerdicts (M5) maps the reviewer JSON's optional verdicts
// entries onto modelrating.Verdict, dropping rows that violate the frozen
// C1 vocabulary (scope/category/severity combos) via Verdict.Validate. The
// collector re-validates at persist and would reject an invalid row anyway;
// dropping at parse keeps the VALID siblings alive instead of poisoning the
// batch. An unknown rubric_id is KEPT — the emitter is lenient by contract
// (see verdictsToWire), only the vocab is enforced. A nil/empty input yields
// nil, so the legacy verdict-free reviewer shape parses byte-identically.
func parseReviewerVerdicts(logger *slog.Logger, raw []verdictWireShape) []modelrating.Verdict {
	if len(raw) == 0 {
		return nil
	}
	var out []modelrating.Verdict
	for _, v := range raw {
		verdict := modelrating.Verdict{
			RubricID:   modelrating.RubricID(v.RubricID),
			Scope:      modelrating.RubricScope(v.Scope),
			Category:   modelrating.Category(v.Category),
			Severity:   modelrating.Severity(v.Severity),
			Detail:     v.Detail,
			Confidence: v.Confidence,
		}
		if err := verdict.Validate(); err != nil {
			logger.Debug("Dropping invalid reviewer verdict", "error", err, "rubric_id", v.RubricID)
			continue
		}
		out = append(out, verdict)
	}
	return out
}

// passVerdictFor builds the issue-#168-AC1 pass row for an APPROVED review:
// exactly one verdict-free row (category "pass", empty severity, no rubric)
// so per-model pass/fail rates use a real denominator. An approved step that
// already carries defect verdicts (approved-with-findings) gets NO pass row —
// the pass row exists so a clean approval is countable, not to offset defects.
func passVerdictFor(result *ReviewResult) []modelrating.Verdict {
	if result.Status != ReviewApproved || len(result.Verdicts) > 0 {
		return nil
	}
	return []modelrating.Verdict{{
		RubricID:   "",
		Scope:      modelrating.ScopeModel,
		Category:   modelrating.CategoryPass,
		Severity:   modelrating.SeverityNone,
		Detail:     "approved: no defects found",
		Confidence: result.Confidence,
	}}
}

// verdictsToWire maps the ReviewResult's verdicts onto the frozen C4 wire
// shape. Lenient by contract: an unknown rubric id keeps its row (RubricID
// carries the id as-is — catalog membership is NOT enforced on emission;
// the empty-detail fallback keeps the row rather than dropping it).
func verdictsToWire(verdicts []modelrating.Verdict) []verdictWireShape {
	if len(verdicts) == 0 {
		return nil
	}
	out := make([]verdictWireShape, 0, len(verdicts))
	for _, v := range verdicts {
		detail := v.Detail
		if detail == "" {
			detail = "no detail provided"
		}
		out = append(out, verdictWireShape{
			RubricID:   string(v.RubricID),
			Scope:      string(v.Scope),
			Category:   string(v.Category),
			Severity:   string(v.Severity),
			Detail:     detail,
			Confidence: v.Confidence,
		})
	}
	return out
}

// agentModelRefForSpec resolves an agent's model id through the SAME
// precedence chain modelRefForAgent uses at loop creation (registry.go):
// spec.Model > spec.ModelAlias > same-id alias > "". It returns the model
// REFERENCE (alias or direct ref), "" when nothing binds — the model id is
// never guessed. Nil-safe: no registry/spec means no identity.
func (rm *ReviewManager) agentModelRefForSpec(agentID string) string {
	if rm == nil || rm.registry == nil || agentID == "" {
		return ""
	}
	spec, ok := rm.registry.GetSpec(agentID)
	if !ok {
		return ""
	}
	return modelRefForAgent(spec, rm.registry.resolver)
}

// reviewEmitterIdentities gathers the frozen C4 identity fields for a
// review emission. Everything degrades to "" rather than being guessed:
//
//	reviewed_model_id    the step actor's model ref ("" unresolvable)
//	judge_agent_id       result.ReviewerID (the grading agent)
//	judge_model_id       the judge's model ref via modelRefForAgent
//	orchestrator_*       the task's AssignedAgent + its model ref
func (rm *ReviewManager) reviewEmitterIdentities(stepID, taskID, judgeAgentID string) (reviewedModelID, judgeModelID, orchestratorAgentID, orchestratorModelID string) {
	// Reviewed actor: the step's AgentID (the implementer being graded).
	if rm.stepStore != nil && stepID != "" {
		if step, err := rm.stepStore.GetByID(stepID); err == nil && step != nil {
			reviewedModelID = rm.agentModelRefForSpec(step.AgentID)
		}
	}
	judgeModelID = rm.agentModelRefForSpec(judgeAgentID)
	if rm.taskStore != nil && taskID != "" {
		if t, err := rm.taskStore.GetByID(taskID); err == nil && t != nil {
			orchestratorAgentID = t.AssignedAgent
			orchestratorModelID = rm.agentModelRefForSpec(t.AssignedAgent)
		}
	}
	return reviewedModelID, judgeModelID, orchestratorAgentID, orchestratorModelID
}

// enrichedReviewPayload builds the C4 payload map shared by BOTH review
// topics (step.review_completed and task.review_completed must carry
// identical payloads). isFinal is terminal-finality of THIS pass:
// needs_info is never final; approved/rejected are.
//
// C3 blame wiring (leaf 02-trace-stamping): trace_id is the step-scoped
// stamp "stp-"+stepID — the id the step job's ctx carried (components.go
// stepJobTraceContext), so the implementer's llm_calls rows are joined by
// it. reviewed_model_id resolves via Store.ResolveReviewedModel(traceID,
// stepAgentID) — the FINAL SUCCESSFUL row, never an error row; the
// modelRefForAgent value is a FALLBACK for when the join returns "" (no
// stamped rows: legacy steps, metrics store unavailable/nil). The trace id
// itself is always stamped on the payload verbatim, join hit or not.
func (rm *ReviewManager) enrichedReviewPayload(stepID, taskID string, result *ReviewResult, revisionCount int) map[string]any {
	isFinal := result.Status != ReviewNeedsInfo
	reviewedModelID, judgeModelID, orchestratorAgentID, orchestratorModelID := rm.reviewEmitterIdentities(stepID, taskID, result.ReviewerID)
	// C3: the step-scoped trace id mirrors the stamp the implementer's
	// calls ran under. An empty step id keeps "" (never a bare "stp-"
	// sentinel — the tracectx guard's shape).
	stepTraceID := ""
	if stepID != "" {
		stepTraceID = "stp-" + stepID
		// Blame rule: the implementer's FINAL SUCCESSFUL row for
		// (stepTraceID, step.AgentID). Join miss ("" result, store error,
		// or no store) keeps the configured model ref from
		// reviewEmitterIdentities — a fallback, never a replacement.
		if rm.metricsStore != nil {
			var stepAgentID string
			if rm.stepStore != nil {
				if step, err := rm.stepStore.GetByID(stepID); err == nil && step != nil {
					stepAgentID = step.AgentID
				}
			}
			if stepAgentID != "" {
				if resolved, err := rm.metricsStore.ResolveReviewedModel(stepTraceID, stepAgentID); err == nil && resolved != "" {
					reviewedModelID = resolved
				} else if err != nil {
					rm.logger.Debug("blame join failed; keeping configured model ref",
						"step_id", stepID,
						"trace_id", stepTraceID,
						"agent_id", stepAgentID,
						"error", err,
					)
				}
			}
		}
	}
	return map[string]any{
		KeyStepID:               stepID,
		KeyTaskID:               taskID,
		"status":                string(result.Status),
		"feedback":              result.Feedback,
		"confidence":            result.Confidence,
		"reviewer":              result.ReviewerID,
		"revision_count":        revisionCount,
		"verdicts":              verdictsToWire(append(result.Verdicts, passVerdictFor(result)...)),
		"reviewed_model_id":     reviewedModelID,
		"judge_agent_id":        result.ReviewerID,
		"judge_model_id":        judgeModelID,
		"orchestrator_agent_id": orchestratorAgentID,
		"orchestrator_model_id": orchestratorModelID,
		"trace_id":              stepTraceID,
		"cycle":                 revisionCount,
		"is_final":              isFinal,
	}
}

// publishReviewEvent publishes a review completion event.
func (rm *ReviewManager) publishReviewEvent(stepID, taskID string, result *ReviewResult, revisionCount int) {
	if rm.bus == nil {
		return
	}

	// ONE construction site: both topics carry the identical enriched
	// payload (C4). The task.* twin exists for subscribers (TUI,
	// ChatHandler) that listen on task.* but not step.*.
	payload := rm.enrichedReviewPayload(stepID, taskID, result, revisionCount)

	msg, err := models.NewBusMessage(models.MessageTypeEvent, "review-manager", payload)
	if err != nil {
		rm.logger.Error("Failed to create review message", "error", err)
		return
	}

	rm.bus.Publish("step.review_completed", msg)

	// Also publish under task.* prefix for backward compatibility with
	// subscribers (TUI, ChatHandler) that subscribe to task.* but not step.*.
	taskMsg, taskErr := models.NewBusMessage(models.MessageTypeEvent, "review-manager", payload)
	if taskErr != nil {
		rm.logger.Error("Failed to create task.review_completed message", "error", taskErr)
		return
	}
	rm.bus.Publish("task.review_completed", taskMsg)
}

// SetPolicy updates the review policy.
func (rm *ReviewManager) SetPolicy(policy *ReviewPolicy) {
	if policy == nil {
		return
	}
	rm.mu.Lock()
	rm.policy = policy
	rm.mu.Unlock()
	rm.logger.Info("Review policy updated")
}

// ValidateCompletion checks that all assigned work for a step is actually done.
// It examines the step's result, evidence, and claims to determine if the work
// described in the step was fully completed.
func (rm *ReviewManager) ValidateCompletion(ctx context.Context, step *task.TaskStep, taskDesc string) (*ValidationResult, error) {
	// Snapshot validation policy under lock to avoid racing with
	// SetValidationPolicy (S1-17).
	rm.mu.RLock()
	validationPolicy := rm.validationPolicy
	rm.mu.RUnlock()

	// Check if validation is needed
	if !validationPolicy.NeedsValidation(step) {
		return &ValidationResult{
			Status:   CompletionValid,
			Feedback: "Validation not required for this step type",
		}, nil
	}

	rm.logger.Info("Validating step completion",
		"step_id", step.ID,
		"task_id", step.TaskID,
		"tool_hint", step.ToolHint,
	)

	// Check 1: Step has a non-empty result
	if strings.TrimSpace(step.Result) == "" {
		return &ValidationResult{
			Status:   CompletionInvalid,
			Feedback: "Step completed with empty result",
			Missing:  []string{"result content"},
		}, nil
	}

	// Check 2: Evidence was provided (if applicable)
	if step.ToolHint == string(IntentCode) || step.ToolHint == KeywordRefactor || step.ToolHint == KeywordFix {
		if len(step.Evidence) == 0 && len(step.Claims) == 0 {
			return &ValidationResult{
				Status:   CompletionPartial,
				Feedback: "Code change completed without evidence or claims - cannot verify",
				Missing:  []string{"evidence", "claims"},
			}, nil
		}
	}

	// Check 3: Verify claims match the step description
	verified, missing := rm.checkClaimsAgainstDescription(step)
	if len(missing) > 0 && len(step.Evidence) == 0 {
		return &ValidationResult{
			Status:   CompletionPartial,
			Feedback: fmt.Sprintf("Step partially completed: %d items verified, %d items missing", len(verified), len(missing)),
			Missing:  missing,
			Verified: verified,
		}, nil
	}

	// Check 4: Cross-reference with original task intent if available
	if taskDesc != "" && len(step.Evidence) > 0 {
		// When evidence is present, only flag if relevance is extremely low
		taskKeywords := extractKeywords(taskDesc)
		resultLower := strings.ToLower(step.Result)
		matchedKeywords := 0
		for _, kw := range taskKeywords {
			if strings.Contains(resultLower, strings.ToLower(kw)) {
				matchedKeywords++
			}
		}

		// If less than 15% of task keywords appear in the result, flag as partial
		if len(taskKeywords) > 0 && float64(matchedKeywords)/float64(len(taskKeywords)) < 0.15 {
			return &ValidationResult{
				Status:   CompletionPartial,
				Feedback: "Step result has low relevance to original task description",
				Missing:  []string{"task-relevant content"},
				Verified: verified,
			}, nil
		}
	} else if taskDesc != "" {
		// No evidence - use stricter relevance check (30% threshold)
		taskKeywords := extractKeywords(taskDesc)
		resultLower := strings.ToLower(step.Result)
		matchedKeywords := 0
		for _, kw := range taskKeywords {
			if strings.Contains(resultLower, strings.ToLower(kw)) {
				matchedKeywords++
			}
		}

		if len(taskKeywords) > 0 && float64(matchedKeywords)/float64(len(taskKeywords)) < 0.3 {
			return &ValidationResult{
				Status:   CompletionPartial,
				Feedback: "Step result has low relevance to original task description",
				Missing:  []string{"task-relevant content"},
				Verified: verified,
			}, nil
		}
	}

	rm.logger.Info("Step validation passed",
		"step_id", step.ID,
		"verified_count", len(verified),
	)

	return &ValidationResult{
		Status:   CompletionValid,
		Feedback: "All assigned work verified complete",
		Verified: verified,
	}, nil
}

// checkClaimsAgainstDescription compares step claims against the step description
// to verify the stated work was completed.
func (rm *ReviewManager) checkClaimsAgainstDescription(step *task.TaskStep) (verified, missing []string) {
	verified = nil
	missing = nil

	descLower := strings.ToLower(step.Description)

	// Check if claims are present
	if len(step.Claims) > 0 {
		verified = append(verified, step.Claims...)
	} else {
		// No explicit claims - check if the result mentions completing the description
		resultLower := strings.ToLower(step.Result)
		descKeywords := extractKeywords(step.Description)

		for _, kw := range descKeywords {
			if strings.Contains(resultLower, strings.ToLower(kw)) {
				verified = append(verified, kw)
			} else {
				missing = append(missing, kw)
			}
		}
	}

	// If step had errors, it's not complete
	if strings.Contains(descLower, "fix") || strings.Contains(descLower, "debug") {
		// Check result for error indicators
		resultLower := strings.ToLower(step.Result)
		errorIndicators := []string{string(MessageTypeError), "failed", "could not", "unable to", "not found"}
		hasErrors := false
		for _, indicator := range errorIndicators {
			if strings.Contains(resultLower, indicator) && !strings.Contains(resultLower, "fixed") && !strings.Contains(resultLower, "resolved") {
				hasErrors = true
				break
			}
		}
		if hasErrors && len(step.Claims) == 0 {
			missing = append(missing, "error resolution confirmation")
		}
	}

	return verified, missing
}

// extractKeywords extracts meaningful keywords from a description.
func extractKeywords(desc string) []string {
	// Remove common stop words and extract meaningful keywords
	stopWords := map[string]bool{
		"the": true, "a": true, "an": true, "is": true, "are": true,
		"was": true, "were": true, "be": true, "been": true, "being": true,
		"have": true, "has": true, "had": true, "do": true, "does": true,
		"did": true, "will": true, "would": true, "could": true, "should": true,
		"may": true, "might": true, "can": true, "shall": true, "to": true,
		"of": true, "in": true, "for": true, "on": true, "with": true,
		"at": true, "by": true, "from": true, "as": true, "into": true,
		"through": true, "during": true, "before": true, "after": true,
		"and": true, "but": true, "or": true, "nor": true, "not": true,
		"so": true, "yet": true, "both": true, "either": true, "neither": true,
		"this": true, "that": true, "these": true, "those": true,
		"it": true, "its": true, "which": true, "who": true, "whom": true,
		"what": true, "where": true, "when": true, "how": true, "why": true,
	}

	words := strings.Fields(strings.ToLower(desc))
	var keywords []string
	for _, word := range words {
		// Remove punctuation
		word = strings.Trim(word, ".,;:!?'\"()[]{}")
		if len(word) > 2 && !stopWords[word] {
			keywords = append(keywords, word)
		}
	}

	return keywords
}

// SetValidationPolicy updates the validation policy.
func (rm *ReviewManager) SetValidationPolicy(policy *ValidationPolicy) {
	if policy == nil {
		return
	}
	rm.mu.Lock()
	rm.validationPolicy = policy
	rm.mu.Unlock()
	rm.logger.Info("Validation policy updated")
}

// GetValidationPolicy returns the current validation policy.
func (rm *ReviewManager) GetValidationPolicy() *ValidationPolicy {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.validationPolicy
}

// GetPolicy returns the current review policy.
func (rm *ReviewManager) GetPolicy() *ReviewPolicy {
	rm.mu.RLock()
	defer rm.mu.RUnlock()
	return rm.policy
}

// stepHasError returns true if the step result appears to contain an error
// or failure message, making a review redundant.
//
// The indicators are checked against the STRUCTURED envelope, not free
// prose. The step-job result (components.go) is a JSON envelope whose
// "response" field is the model's own narration: a successful agent that
// merely MENTIONS a past failure ("an initial draft failed to link, so I
// switched to a library package") must not be judged failed by scanning
// its narrative. The sweep found exactly that false positive (e2e
// 2026-09-15: a coder step with passing vet/build/test was rejected
// because its narration contained "failed to link"). Parse the envelope;
// if it parses, judge success by the envelope's "success" flag and keep
// the prose scan only for the structured "error" field. Non-JSON results
// (legacy free-text steps) still get the prose scan.
func (rm *ReviewManager) stepHasError(step *task.TaskStep) bool {
	result := strings.TrimSpace(step.Result)
	if result == "" {
		return false // Empty result: will be caught by later heuristic, not "error"
	}

	// Structured envelope path (preferred): authoritative success flag.
	// Success is *bool: an ABSENT field is not a failure (JSON zero-value
	// would make every legacy envelope that omits the key an error).
	// Bughunt 2026-11-24 M9: the field was decoded but never read, so an
	// explicit {"success":false} passed review as long as status/error
	// were clean.
	var envelope struct {
		Success *bool  `json:"success"`
		Status  string `json:"status"`
		Error   string `json:"error"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err == nil {
		if envelope.Error != "" {
			return true
		}
		if envelope.Status == "failed" {
			return true
		}
		// Envelope parsed: the processor's success flag is authoritative.
		if envelope.Success != nil && !*envelope.Success {
			return true
		}
		// Narrative text inside "response"/"evidence" is not re-scanned --
		// agents legitimately describe past failures there.
		return false
	}

	// Legacy free-text path.
	lower := strings.ToLower(result)
	errorIndicators := []string{
		"message:error",
		"message: error",
		"error executing",
		"error running",
		"error:",
		"failed to",
		"could not",
		"unable to",
		"rpc error",
		"permission denied",
		"access denied",
		"not found",
		"timeout",
		"connection refused",
	}
	for _, indicator := range errorIndicators {
		if strings.Contains(lower, indicator) {
			return true
		}
	}
	return false
}

// isTrivialTask returns true if the task has fewer than 3 steps.
// Low step-count tasks are trivial enough that an LLM review's token cost
// outweighs any benefit from a thorough inspection.
func (rm *ReviewManager) isTrivialTask(step *task.TaskStep) bool {
	if rm.stepStore == nil {
		return false // Cannot determine task size, default to full review
	}
	steps, err := rm.stepStore.ListByTaskID(step.TaskID)
	if err != nil {
		rm.logger.Debug("Failed to list task steps for review gating",
			"task_id", step.TaskID, "error", err)
		return false
	}
	return len(steps) < 3
}

// heuristicGuardsPass runs ONLY the claim-without-evidence guards from
// heuristicReviewPasses -- no error/emptiness checks, no full heuristic
// verdict. F9 (2026-09-17 bughunt): ReviewStep consults this BEFORE the
// policy.NeedsReview shortcut, so a step whose hint is on the policy
// skip list ("chat", "report", …) cannot be auto-approved when it
// narrates a specific tool execution with no tool evidence behind it.
// Returns true when the guards are silent (step may proceed).
func (rm *ReviewManager) heuristicGuardsPass(step *task.TaskStep) bool {
	result := strings.TrimSpace(step.Result)
	if result == "" {
		return true
	}
	// Scan scope mirrors heuristicReviewPasses: structured step-job
	// envelopes are scanned on the model's RESPONSE narration only.
	scanText := result
	var envelope struct {
		Response string `json:"response"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err == nil && envelope.Response != "" {
		scanText = envelope.Response
	}
	// Artifact guard: a non-conversational step claiming artifacts with
	// no meaningful tool evidence (e2e run 7, 2026-09-10).
	if step.ToolHint != "" && !reviewHintIsConversational(step.ToolHint) && !hasMeaningfulEvidence(step.Evidence) {
		if claimsArtifacts(scanText) {
			rm.logger.Warn("Review policy gate: artifact claims with no tool evidence; review required",
				"step_id", step.ID,
				"tool_hint", step.ToolHint,
			)
			return false
		}
	}
	// Tool-execution-claim guard (agent sweep e2e, 2026-09-15): any step
	// asserting a specific tool execution with no meaningful tool
	// evidence needs a reviewer, conversational hint or not.
	if !hasMeaningfulEvidence(step.Evidence) && claimsToolExecution(scanText) {
		rm.logger.Warn("Review policy gate: tool-execution claims with no tool evidence; review required",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		return false
	}
	return true
}

// heuristicReviewPasses performs a lightweight, non-LLM check to determine
// whether a low-risk step should pass review. It returns true when the
// step has a non-empty, meaningful result -- the default state for benign
// operations that did not fail. Error-shaped results never pass: a long
// error string is still an error.
func (rm *ReviewManager) heuristicReviewPasses(step *task.TaskStep) bool {
	if rm.stepHasError(step) {
		return false
	}
	result := strings.TrimSpace(step.Result)
	if result == "" {
		return false
	}
	// Machine-shaped-result guard (leaf 02, 20261121-interactive-usability,
	// probe B task-...0007): a result that is raw tool-call/tool-result JSON
	// is not a user-facing answer -- the model narrated (or echoed) its tool
	// calls as text. Auto-approving that dump is how the probe reported
	// "completed" with zero artifacts. Refuse the heuristic pass; the
	// caller routes the step to the full reviewer.
	if stepResultIsMachineShaped(result) {
		rm.logger.Warn("Heuristic review: machine-shaped tool-JSON result; refusing auto-approve",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		return false
	}
	// If result is purely whitespace or very short (no meaningful content)
	if len(result) < 3 {
		return false
	}
	// Scan scope (sweep e2e, 2026-09-15): for structured step-job envelopes,
	// scan only the model's RESPONSE narration -- not the whole envelope.
	// The envelope's evidence[] strings quote the tool-issued items and the
	// model's report copy, so markers like "file_exists" appear there on
	// every successful file-tool turn and the artifact/claim guards would
	// reject genuinely successful work. Legacy free-text results keep the
	// whole-string scan.
	scanText := result
	var envelope struct {
		Response string `json:"response"`
		Success  bool   `json:"success"`
	}
	if err := json.Unmarshal([]byte(result), &envelope); err == nil && envelope.Response != "" {
		scanText = envelope.Response
	}
	// Reasoning-only guard (2026-09-10, outcome-loop L3 session): the
	// reasoning-watchdog writes "[reasoning-only turn]" assistant markers
	// and its terminate path returns the canned "I stopped after extended
	// thinking" text as the step result. Neither is evidence of work --
	// both are the loop GIVING UP. A canned no-tool termination must not
	// ride the trivial-task heuristic to auto-approval.
	if !reviewHintIsConversational(step.ToolHint) &&
		strings.Contains(scanText, "stopped after extended thinking") &&
		!hasMeaningfulEvidence(step.Evidence) {
		rm.logger.Warn("Heuristic review: reasoning-only termination with no tool evidence; refusing auto-approve",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		return false
	}
	// Claim-without-action guard (e2e run 7, 2026-09-10): the coder returned
	// a structured report asserting file_exists evidence WITHOUT calling any
	// tool (iterations=1, zero tool executions) and this heuristic
	// auto-approved it -- a hallucinated completion. An execution step whose
	// report claims artifacts but which shows no tool activity is not
	// verifiable; route it to the full reviewer instead of waving it
	// through. Memory/analysis hints legitimately produce reports without
	// tools, so scope the suspicion to artifact claims only.
	//
	// F9 (2026-09-12 bughunt): the predicate MUST be the structural one.
	// The daemon's step-job envelope encodes `evidence` as a []string of
	// prose, so decoding into []models.Evidence leaves a ONE-element slice
	// holding a zero-value Evidence -- len(step.Evidence) == 0 is therefore
	// never true for a job-driven step and this refusal was unreachable.
	if step.ToolHint != "" && !reviewHintIsConversational(step.ToolHint) && !hasMeaningfulEvidence(step.Evidence) {
		if claimsArtifacts(scanText) {
			rm.logger.Warn("Heuristic review: artifact claims with no tool evidence; refusing auto-approve",
				"step_id", step.ID,
				"tool_hint", step.ToolHint,
			)
			return false
		}
	}
	// Tool-execution-claim guard (agent sweep e2e, 2026-09-15): chat/analyst
	// steps reported "Extraction ran clean on the first pass" -- claiming a
	// json_extract tool execution -- with zero extraction-model calls having
	// been routed (metrics.db: 0 llm_calls to local-extract). These hints
	// are conversational, so the artifact guard above does not apply; but a
	// CONVERSATIONAL agent claiming it RAN a named tool is the same
	// hallucination shape. Any result asserting a specific tool execution
	// with no meaningful tool evidence routes to full review.
	if !hasMeaningfulEvidence(step.Evidence) && claimsToolExecution(scanText) {
		rm.logger.Warn("Heuristic review: tool-execution claims with no tool evidence; refusing auto-approve",
			"step_id", step.ID,
			"tool_hint", step.ToolHint,
		)
		return false
	}
	// Plan-vacuity guard (leaf 03, 20260918-tool-boundary-hardening; e2e
	// 2026-09-18): a planner-hint step whose result is pure first-person
	// future-intent narration ("I will...", "Let me...") with zero
	// task-management tool evidence and no structured plan artifact is
	// VACUOUS -- approving it lets a doomed loop continue. The gate is
	// against SILENT auto-approval: the step routes to the full reviewer,
	// which may still approve with justification.
	if planLooksVacuous(step) {
		rm.logger.Warn("Heuristic review: vacuous plan result (narration only); refusing auto-approve",
			"step_id", step.ID, "tool_hint", step.ToolHint)
		return false
	}
	return true
}

// reviewHintIsConversational reports whether a tool hint denotes work whose
// product IS the report (no artifact claims expected).
func reviewHintIsConversational(hint string) bool {
	switch strings.ToLower(strings.TrimSpace(hint)) {
	case "chat", "report", "recall", "research", "analyze", "analyst", "plan":
		return true
	}
	return false
}

// claimsToolExecution reports whether a result asserts that a specific
// NAMED tool was executed ("extraction ran clean", "called json_extract",
// "ran the web_fetch tool"). Sweep e2e 2026-09-15: conversational agents
// fabricated tool-execution reports with zero tool activity behind them.
// Generic phrases like "used my tools" don't match; the claim must name a
// tool-ish noun or a tool-run verb.
func claimsToolExecution(result string) bool {
	lower := strings.ToLower(result)
	for _, marker := range []string{
		"extraction ran", "extraction ran clean",
		"called the json_extract", "called json_extract",
		"ran the json_extract", "json_extract tool",
		"called the web_fetch", "ran web_fetch", "web_fetch tool",
		"called the websearch", "websearch tool",
		"shell_exec tool", "transcript_fetch tool",
		"via json_extract", "via web_fetch", "via websearch",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// claimsArtifacts reports whether a result string asserts file/artifact
// creation or existence -- the shape the run-7 hallucinated report used.
func claimsArtifacts(result string) bool {
	lower := strings.ToLower(result)
	for _, marker := range []string{
		"file_exists", "created file", "wrote file", "file written",
		"created hello", "\"created ", "created the file", "wrote the file",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

// hasMeaningfulEvidence reports whether the step's Evidence slice carries
// at least one entry a tool actually produced (non-zero Type, Subject, or
// Value). A zero-value Evidence is a decode artifact: the step-job result
// envelope encodes `evidence` as a []string of prose, and unmarshaling a
// string into models.Evidence leaves a zero struct IN the slice (Go keeps
// the element with an UnmarshalTypeError but continues decoding). The
// failed-smoke step (task-20260911211112.532862000-0002) carried exactly
// one such zero entry, defeating the len(evidence)==0 check. Structurally
// empty evidence is not evidence.
func hasMeaningfulEvidence(evs []models.Evidence) bool {
	for _, e := range evs {
		if e.Type != "" || e.Subject != "" || e.Value != "" {
			return true
		}
	}
	return false
}
