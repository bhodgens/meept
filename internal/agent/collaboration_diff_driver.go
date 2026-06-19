package agent

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

const (
	defaultReviewMaxRounds = 3
)

// DifferentialDriver implements the four-phase A/B + differentiation pipeline.
type DifferentialDriver struct {
	registry  *AgentRegistry
	workspace *WorkspaceManager
	pairMgr   *PairManager
	bus       *bus.MessageBus
	logger    *slog.Logger
}

// DifferentialDriverDeps holds dependencies.
type DifferentialDriverDeps struct {
	Registry    *AgentRegistry
	Workspace   *WorkspaceManager
	PairManager *PairManager
	Bus         *bus.MessageBus
	Logger      *slog.Logger
}

// NewDifferentialDriver creates a new differential driver.
func NewDifferentialDriver(deps DifferentialDriverDeps) *DifferentialDriver {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	return &DifferentialDriver{
		registry:  deps.Registry,
		workspace: deps.Workspace,
		pairMgr:   deps.PairManager,
		bus:       deps.Bus,
		logger:    deps.Logger,
	}
}

// Name returns the mode name.
func (d *DifferentialDriver) Name() string { return "differential" }

// CanInitiate returns true if agent-initiated differential mode is allowed.
func (d *DifferentialDriver) CanInitiate(agentID string, reason string) bool {
	return agentID == "coder" || agentID == "planner" || agentID == "analyst"
}

// Run executes the four-phase differential pipeline.
func (d *DifferentialDriver) Run(ctx context.Context, sess *CollaborationSession) (*CollaborationResult, error) {
	if len(sess.Participants) < 2 {
		return nil, NewCollaborationError(ErrCodeInvalidMode, sess.ID, "init", "differential requires at least 2 participants")
	}

	d.logger.Info("Starting differential session",
		"session_id", sess.ID,
		"task_id", sess.TaskID,
		"participants", sess.Participants,
	)

	d.publishEvent(sess.ID, TopicCollabSessionCreated, map[string]any{
		"session_id":   sess.ID,
		"mode":         "differential",
		"participants": sess.Participants,
		"task_id":      sess.TaskID,
	})

	startTime := time.Now()

	// Enforce time budget via derived context
	runCtx := ctx
	if sess.TimeBudget > 0 {
		var cancel context.CancelFunc
		runCtx, cancel = context.WithTimeout(ctx, sess.TimeBudget)
		defer cancel()
	}

	// Phase 1: Fork
	if err := d.phaseFork(runCtx, sess); err != nil {
		sess.MarkFailed()
		return nil, err // phaseFork returns CollaborationError with ErrCodeWorkspace
	}
	d.publishEvent(sess.ID, TopicCollabPhaseCompleted, map[string]any{
		"session_id": sess.ID, "phase": "fork", "workspace": sess.Workspace,
	})

	// Phase 2: Implement & Review
	branchAOK, branchBOK, err := d.phaseImplement(runCtx, sess)
	if err != nil {
		sess.MarkFailed()
		d.publishEvent(sess.ID, TopicCollabError, map[string]any{
			"session_id": sess.ID, "error": err.Error(), "phase": "implement",
		})
		return nil, err
	}
	d.publishEvent(sess.ID, TopicCollabPhaseCompleted, map[string]any{
		"session_id": sess.ID, "phase": "implement", "branch_a_ok": branchAOK, "branch_b_ok": branchBOK,
	})

	// Phase 3: Validate Checkpoint
	checkpointResult, err := d.phaseValidate(runCtx, sess, branchAOK, branchBOK)
	if err != nil {
		sess.MarkFailed()
		return nil, err
	}

	if !checkpointResult.AnyOK {
		sess.MarkFailed()
		d.publishEvent(sess.ID, TopicCollabDivergence, map[string]any{
			"session_id": sess.ID, "reason": "both branches failed",
		})
		return &CollaborationResult{
			SessionID: sess.ID,
			State:     SessionFailed,
			Workspace: sess.Workspace,
			TurnCount: sess.TurnCount(),
			Duration:  time.Since(startTime),
		}, nil
	}

	if checkpointResult.FallbackToA || checkpointResult.FallbackToB {
		d.publishEvent(sess.ID, TopicCollabDivergence, map[string]any{
			"session_id":    sess.ID,
			"fallback_to_a": checkpointResult.FallbackToA,
			"fallback_to_b": checkpointResult.FallbackToB,
		})
	}

	d.publishEvent(sess.ID, TopicCollabPhaseCompleted, map[string]any{
		"session_id": sess.ID, "phase": "validate",
		"any_ok": checkpointResult.AnyOK,
	})

	// Phase 4: Differentiate & Synthesize
	result, err := d.phaseDifferentiate(runCtx, sess, checkpointResult)
	if err != nil {
		sess.MarkFailed()
		d.publishEvent(sess.ID, TopicCollabError, map[string]any{
			"session_id": sess.ID, "error": err.Error(), "phase": "differentiate",
		})
		return nil, fmt.Errorf("phase 4 (differentiate) failed: %w", err)
	}

	sess.MarkConverged()
	result.Duration = time.Since(startTime)
	result.State = SessionConverged

	d.publishEvent(sess.ID, TopicCollabResult, map[string]any{
		"session_id":  sess.ID,
		"state":       string(SessionConverged),
		"turn_count":  result.TurnCount,
		"workspace":   sess.Workspace,
		"duration_ms": result.Duration.Milliseconds(),
	})

	return result, nil
}

// phaseFork creates the Diff workspace layout.
func (d *DifferentialDriver) phaseFork(ctx context.Context, sess *CollaborationSession) error {
	baseDir := getCollabWorkspaceBase()
	wsPath := filepath.Join(baseDir, "diff-"+sess.ID)

	dirs := []string{
		filepath.Join(wsPath, "branch-a"),
		filepath.Join(wsPath, "branch-b"),
		filepath.Join(wsPath, "combined"),
		filepath.Join(wsPath, "meta"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return NewCollaborationError(ErrCodeWorkspace, sess.ID, "fork", fmt.Sprintf("failed to create directory %s: %v", dir, err))
		}
	}

	planPath := filepath.Join(wsPath, "meta", "plan.md")
	content := fmt.Sprintf("# Task Plan\n\n**Session:** %s\n**Task:** %s\n\n", sess.ID, sess.TaskID)
	if err := os.WriteFile(planPath, []byte(content), 0644); err != nil {
		return NewCollaborationError(ErrCodeWorkspace, sess.ID, "fork", fmt.Sprintf("failed to write plan: %v", err))
	}

	sess.Workspace = wsPath
	sess.MarkActive()
	d.logger.Info("Phase 1: Fork complete", "workspace", wsPath)
	return nil
}

// phaseImplement runs independent PairManager sessions for each branch.
func (d *DifferentialDriver) phaseImplement(ctx context.Context, sess *CollaborationSession) (branchAOK, branchBOK bool, err error) {
	if d.pairMgr == nil {
		d.logger.Warn("PairManager not available, simulating branch implementation")
		return d.phaseImplementDirect(ctx, sess)
	}

	taskSpec := fmt.Sprintf("Implement: %s", sess.TaskID)

	sessionA := d.pairMgr.CreateSession(sess.ID+"-a", taskSpec, sess.Participants[0], "code-reviewer", defaultReviewMaxRounds)
	sessionB := d.pairMgr.CreateSession(sess.ID+"-b", taskSpec, sess.Participants[1], "code-reviewer", defaultReviewMaxRounds)

	_, errA := d.pairMgr.RunAllRounds(ctx, sessionA.ID)
	_, errB := d.pairMgr.RunAllRounds(ctx, sessionB.ID)

	branchAOK = errA == nil && sessionA.State == PairSessionConverged
	branchBOK = errB == nil && sessionB.State == PairSessionConverged

	d.logger.Info("Phase 2: Implement complete",
		"branch_a_ok", branchAOK,
		"branch_b_ok", branchBOK,
	)
	return branchAOK, branchBOK, nil
}

// phaseImplementDirect runs agents directly without PairManager.
func (d *DifferentialDriver) phaseImplementDirect(ctx context.Context, sess *CollaborationSession) (branchAOK, branchBOK bool, err error) {
	if d.registry == nil {
		return false, false, NewCollaborationError(ErrCodeAgentFailed, sess.ID, "implement_direct", "registry not available")
	}

	taskSpec := fmt.Sprintf("Implement the following task: %s", sess.TaskID)

	_, errA := d.registry.RunAgent(ctx, sess.Participants[0], taskSpec, fmt.Sprintf("diff-%s-branch-a", sess.ID))
	if errA != nil {
		d.logger.Warn("Branch A agent failed", "session_id", sess.ID, "error", errA)
	}
	branchAOK = errA == nil

	_, errB := d.registry.RunAgent(ctx, sess.Participants[1], taskSpec, fmt.Sprintf("diff-%s-branch-b", sess.ID))
	if errB != nil {
		d.logger.Warn("Branch B agent failed", "session_id", sess.ID, "error", errB)
	}
	branchBOK = errB == nil

	if !branchAOK && !branchBOK {
		return false, false, NewCollaborationError(ErrCodeAgentFailed, sess.ID, "implement_direct", fmt.Sprintf("both branches failed: a=%v, b=%v", errA, errB))
	}

	return branchAOK, branchBOK, nil
}

// ValidateCheckpointResult holds the result of phase 3.
type ValidateCheckpointResult struct {
	AnyOK            bool
	BranchAConverged bool
	BranchBConverged bool
	BranchAWorkspace string
	BranchBWorkspace string
	FallbackToA      bool
	FallbackToB      bool
}

// phaseValidate creates git checkpoints and handles fallbacks.
func (d *DifferentialDriver) phaseValidate(_ context.Context, sess *CollaborationSession, branchAOK, branchBOK bool) (*ValidateCheckpointResult, error) {
	result := &ValidateCheckpointResult{
		AnyOK:            branchAOK || branchBOK,
		BranchAConverged: branchAOK,
		BranchBConverged: branchBOK,
	}

	if sess.Workspace == "" {
		return result, nil
	}

	if branchAOK {
		result.BranchAWorkspace = filepath.Join(sess.Workspace, "branch-a")
	}
	if branchBOK {
		result.BranchBWorkspace = filepath.Join(sess.Workspace, "branch-b")
	}

	if branchAOK && !branchBOK {
		result.FallbackToA = true
		d.logger.Info("Phase 3: Fallback to branch A", "session_id", sess.ID)
	} else if !branchAOK && branchBOK {
		result.FallbackToB = true
		d.logger.Info("Phase 3: Fallback to branch B", "session_id", sess.ID)
	}

	return result, nil
}

// phaseDifferentiate runs the differentiator agent to synthesize combined output.
func (d *DifferentialDriver) phaseDifferentiate(ctx context.Context, sess *CollaborationSession, validateResult *ValidateCheckpointResult) (*CollaborationResult, error) {
	if d.registry == nil {
		return &CollaborationResult{
			SessionID: sess.ID,
			State:     SessionConverged,
			Workspace: sess.Workspace,
			TurnCount: sess.TurnCount(),
		}, nil
	}

	hasA := validateResult.BranchAConverged
	hasB := validateResult.BranchBConverged

	prompt := d.buildDifferentiatorPrompt(sess, hasA, hasB)

	differentiatorID := sess.Participants[0]
	if len(sess.Participants) > 2 {
		differentiatorID = sess.Participants[2]
	}

	diffOutput, err := d.registry.RunAgent(ctx, differentiatorID, prompt, fmt.Sprintf("diff-%s-differentiator", sess.ID))
	if err != nil {
		return nil, NewCollaborationError(ErrCodeAgentFailed, sess.ID, "differentiate", fmt.Sprintf("differentiator agent failed: %v", err))
	}

	if sess.Workspace != "" {
		combinedPath := filepath.Join(sess.Workspace, "combined", "result.md")
		if err := os.WriteFile(combinedPath, []byte(diffOutput), 0644); err != nil {
			d.logger.Warn("Failed to write combined result", "path", combinedPath, "error", err)
		}
	}

	d.logger.Info("Phase 4: Differentiate complete", "session_id", sess.ID)
	return &CollaborationResult{
		SessionID:   sess.ID,
		State:       SessionConverged,
		FinalOutput: diffOutput,
		Workspace:   sess.Workspace,
		TurnCount:   sess.TurnCount(),
	}, nil
}

func (d *DifferentialDriver) buildDifferentiatorPrompt(sess *CollaborationSession, hasA, hasB bool) string {
	prompt := "## Differential Analysis Task\n\n"
	prompt += fmt.Sprintf("**Session:** %s\n", sess.ID)
	prompt += fmt.Sprintf("**Original Task:** %s\n\n", sess.TaskID)

	prompt += "## Branch Status\n\n"
	if hasA {
		prompt += "- Branch A: **CONVERGED** (approved by reviewer)\n"
	} else {
		prompt += "- Branch A: **FAILED** (did not pass review)\n"
	}
	if hasB {
		prompt += "- Branch B: **CONVERGED** (approved by reviewer)\n"
	} else {
		prompt += "- Branch B: **FAILED** (did not pass review)\n"
	}

	prompt += "\n## Your Role\n"
	prompt += "You are the differentiator. Your job is to:\n"
	prompt += "1. Evaluate correctness, completeness, edge-case handling, and idiomatic quality.\n"
	prompt += "2. Compare both implementations.\n"
	prompt += "3. Synthesize the best parts into a final combined implementation.\n"
	prompt += "4. Write the final combined result.\n"
	prompt += "\n## Evaluation Criteria\n"
	prompt += "- Correctness: Does each implementation meet the spec?\n"
	prompt += "- Completeness: Any missing components?\n"
	prompt += "- Edge-case handling: Which handles errors/race conditions better?\n"
	prompt += "- Idiomatic quality: Which is cleaner, more maintainable?\n"
	prompt += "- Test coverage: Which has better coverage?\n"

	return prompt
}

// publishEvent publishes a collaboration event to the message bus.
func (d *DifferentialDriver) publishEvent(sessionID, topic string, data map[string]any) {
	if d.bus == nil {
		return
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "differential-driver", data)
	if err != nil {
		d.logger.Error("Failed to create differential bus message", "error", err)
		return
	}
	msg.Topic = topic
	d.bus.Publish(topic, msg)
}
