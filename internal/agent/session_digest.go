package agent

import (
	"context"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/plan"
)

// SessionContextDigest is a compact per-session summary the dispatcher can
// hand to the intent analyzer so messages are judged with session state
// rather than in isolation.
type SessionContextDigest struct {
	// LastTaskName is the name of the session's most recently updated
	// task, capped at 200 characters.
	LastTaskName string
	// LastTaskState is the state of the most recent task (e.g. "completed").
	LastTaskState string
	// LastTaskAgent is the agent assigned to the most recent task.
	LastTaskAgent string
	// LastResultSummary is the first line (capped 400 chars) of the most
	// recent task's best terminal step result.
	LastResultSummary string
	// LastIntentType is the session's most recently recorded intent type.
	LastIntentType string
	// WorkingDirectory is the session's effective working directory,
	// resolved as WorktreePath > ProjectPath > DetectionContext.CWD
	// (mirroring resolveStepWorkingDir). Empty when the session is
	// unknown or carries no paths. Enrichment only: IsEmpty ignores it.
	WorkingDirectory string
}

// IsEmpty reports whether the digest carries no information. The caller
// treats an empty digest as "no session context" (pre-tree behavior).
func (s *SessionContextDigest) IsEmpty() bool {
	return s == nil ||
		(s.LastTaskName == "" &&
			s.LastTaskState == "" &&
			s.LastTaskAgent == "" &&
			s.LastResultSummary == "" &&
			s.LastIntentType == "")
}

// IsEmptyIgnoringClarify reports whether the digest carries no information
// EXCEPT a trailing clarify marker. buildClarificationResult records the
// clarify intent in the session tracker, so a session whose only history is
// a pending clarification produces a digest whose LastIntentType is "clarify"
// — making IsEmpty false even though no real context exists. The
// clarification-resume gate (ResumeAfterClarification A5) uses this variant
// so "still ambiguous after clarification" can re-fire a follow-up question
// for exactly the context-less sessions clarification exists for
// (bughunt 2026-09-10 M2). The ClassifyAndRoute gate keeps plain IsEmpty.
func (s *SessionContextDigest) IsEmptyIgnoringClarify() bool {
	return s == nil ||
		(s.LastTaskName == "" &&
			s.LastTaskState == "" &&
			s.LastTaskAgent == "" &&
			s.LastResultSummary == "" &&
			(s.LastIntentType == "" || s.LastIntentType == string(IntentClarify)))
}

// Caps for digest fields, per master contract SG1.
const (
	digestTaskNameCap = 200
	digestSummaryCap  = 400
)

// buildSessionContextDigest assembles a SessionContextDigest for a session
// from the dispatcher's stores. All store access is nil-guarded; any store
// error degrades that field to empty (Debug log) and never propagates. An
// empty sessionID yields the empty digest.
func (d *Dispatcher) buildSessionContextDigest(sessionID string) *SessionContextDigest {
	return d.buildSessionContextDigestExcluding(sessionID, "")
}

// buildSessionContextDigestExcluding is buildSessionContextDigest with one
// refinement: tasks whose ID matches excludeTaskID are skipped when picking
// the "most recent" task. The executing-agent context block (see
// BuildSessionContextBlock) is built AFTER ClassifyAndRoute has already
// created the CURRENT turn's own task — always the newest row for the
// session, and always state=pending with no steps. Without the exclusion
// the digest would describe the question instead of the prior work the
// question refers to (e2e run 5, 2026-09-10 T3: "did the change get made?"
// must surface T1's completed artifact, not T3's own fresh placeholder).
// Classification-side callers (AnalyzeTrueIntent) have no current task yet
// and pass "" — unchanged behavior.
func (d *Dispatcher) buildSessionContextDigestExcluding(sessionID, excludeTaskID string) *SessionContextDigest {
	digest := &SessionContextDigest{}
	if sessionID == "" {
		return digest
	}

	if d.taskStore != nil {
		tasks, err := d.taskStore.GetTasksForSession(sessionID)
		if err != nil {
			d.logger.Debug("Failed to get tasks for session digest",
				"session_id", sessionID,
				"error", err,
			)
		} else {
			// GetTasksForSession is ordered by updated_at DESC, so the
			// first non-excluded entry is the most recently updated task
			// that is not the current turn's own placeholder.
			for _, lastTask := range tasks {
				if lastTask == nil || lastTask.ID == excludeTaskID {
					continue
				}
				digest.LastTaskName = truncateString(lastTask.Name, digestTaskNameCap)
				digest.LastTaskState = string(lastTask.State)
				digest.LastTaskAgent = lastTask.AssignedAgent
				d.populateDigestStepResult(digest, lastTask.ID, sessionID)
				break
			}
		}
	}

	if d.sessionTracker != nil {
		if lastIntent := d.sessionTracker.GetLastIntent(sessionID); lastIntent != nil {
			digest.LastIntentType = lastIntent.Type
		}
	}

	// WorkingDirectory: resolve from the session store (leaf 03) with the
	// same precedence as resolveStepWorkingDir so the analyzer sees the
	// directory the step jobs will actually use. Enrichment only — any
	// failure or miss degrades to empty; IsEmpty ignores this field.
	if d.sessionStore != nil {
		if sess := d.sessionStore.GetByConversationID(sessionID); sess != nil {
			switch {
			case sess.WorktreePath != "":
				digest.WorkingDirectory = sess.WorktreePath
			case sess.ProjectPath != "":
				digest.WorkingDirectory = sess.ProjectPath
			case sess.DetectionContext != nil && sess.DetectionContext.CWD != "":
				digest.WorkingDirectory = sess.DetectionContext.CWD
			}
		}
	}

	return digest
}

// populateDigestStepResult fills the digest's LastResultSummary from the
// best terminal step result of the given task, mirroring the bestStepResult
// selection rule in handler.go. Store access is nil-guarded and any store
// error is logged at Debug without propagating.
func (d *Dispatcher) populateDigestStepResult(digest *SessionContextDigest, taskID, sessionID string) {
	if d.taskRegistry == nil {
		return
	}
	stepStore := d.taskRegistry.StepStore()
	if stepStore == nil {
		return
	}
	steps, err := stepStore.ListByTaskID(taskID)
	if err != nil {
		d.logger.Debug("Failed to list steps for session digest",
			"session_id", sessionID,
			"task_id", taskID,
			"error", err,
		)
		return
	}
	digest.LastResultSummary = truncateString(firstLine(bestStepResult(steps)), digestSummaryCap)
}

// ---------------------------------------------------------------------------
// Quickplan Session execution context (quickplan-mode leaf 02 / master
// Contract 6).
// ---------------------------------------------------------------------------

// maxQuickPlanContextTasks caps how many open tracked-task titles the
// Session execution context block lists.
const maxQuickPlanContextTasks = 5

// buildSessionExecutionContext renders the "Session execution context"
// block handed to the orchestrator for quick_plan dispatches. It carries
// the session state the per-message classifier cannot see:
//
//   - Active plan: ID + title + state (session's plan tracker)
//   - Open tracked tasks: count + first N titles
//   - Prior quickplan waves in this conversation (session tracker)
//
// All store access is nil-guarded; any error degrades that section to
// empty (Debug log) and never propagates. Returns "" when no section has
// content — the block is omitted entirely, per the one-way rule: session
// evidence upgrades to quickplan; absence never downgrades an explicit
// quickplan.
func (d *Dispatcher) buildSessionExecutionContext(ctx context.Context, sessionID string) string {
	if sessionID == "" {
		return ""
	}
	var sb strings.Builder

	// Active plan: most recent non-terminal plan linked to this session.
	if d.planManager != nil {
		plans, err := d.planManager.GetPlansForSession(ctx, sessionID)
		if err != nil {
			d.logger.Debug("quickplan session context: plan lookup failed",
				"session_id", sessionID,
				"error", err,
			)
		}
		var active *plan.Plan
		for _, p := range plans {
			if p == nil || p.State.IsTerminal() {
				continue
			}
			if active == nil || p.UpdatedAt.After(active.UpdatedAt) {
				active = p
			}
		}
		if active != nil {
			fmt.Fprintf(&sb, "- Active plan: %s %q (state: %s)\n",
				active.ID, active.Title, active.State)
		}
	}

	// Open tracked tasks: non-terminal tasks linked to this session.
	if d.taskStore != nil {
		tasks, err := d.taskStore.GetTasksForSession(sessionID)
		if err != nil {
			d.logger.Debug("quickplan session context: task lookup failed",
				"session_id", sessionID,
				"error", err,
			)
		}
		open := 0
		var titles []string
		for _, t := range tasks {
			if t == nil || t.State.IsTerminal() {
				continue
			}
			open++
			if len(titles) < maxQuickPlanContextTasks {
				titles = append(titles, t.Name)
			}
		}
		if open > 0 {
			fmt.Fprintf(&sb, "- Open tracked tasks: %d\n", open)
			for i, title := range titles {
				fmt.Fprintf(&sb, "  %d. %s\n", i+1, truncateString(title, 120))
			}
		}
	}

	// Prior quickplan waves in this conversation (session tracker
	// intents; the tracker caps history at 20 entries).
	if d.sessionTracker != nil {
		if state := d.sessionTracker.GetSession(sessionID); state != nil {
			waves := 0
			for _, it := range state.IntentHistory {
				if it != nil && it.Type == string(IntentQuickPlan) {
					waves++
				}
			}
			if waves > 0 {
				fmt.Fprintf(&sb, "- Prior quickplan runs in this conversation: %d\n", waves)
			}
		}
	}

	if sb.Len() == 0 {
		return ""
	}
	return "## Session execution context\n" + sb.String()
}

// BuildSessionExecutionContext is the exported wrapper used by the chat
// handler to attach the Session execution context block to quick_plan
// plan requests (quickplan-mode leaf 02).
func (d *Dispatcher) BuildSessionExecutionContext(ctx context.Context, sessionID string) string {
	return d.buildSessionExecutionContext(ctx, sessionID)
}

// BuildPlanSessionContext is BuildSessionExecutionContext composed with the
// session digest block (BuildSessionContextBlock) for plan requests. The
// execution-context block carries open task TITLES; the digest block adds
// the most recent PRIOR task's state and best terminal step result — the
// evidence a "did the change get made?" plan needs to answer from session
// history instead of planning an interrogation of the user (e2e run 7,
// 2026-09-11 T3). excludeTaskID is the current turn's own placeholder task,
// created by ClassifyAndRoute before this call; digestContextEnabled gates
// the digest half so MEEPT_DISABLE_DIGEST_CONTEXT=1 opts BOTH injections
// out together.
func (d *Dispatcher) BuildPlanSessionContext(ctx context.Context, sessionID, excludeTaskID string) string {
	execCtx := d.buildSessionExecutionContext(ctx, sessionID)
	if !d.digestContextEnabled() {
		return execCtx
	}
	digest := d.buildSessionContextDigestExcluding(sessionID, excludeTaskID)
	if digest.IsEmpty() {
		return execCtx
	}
	if execCtx == "" {
		return BuildSessionContextBlock(digest)
	}
	return execCtx + "\n" + BuildSessionContextBlock(digest)
}

// PlanDigestContext is the digest-only half of BuildPlanSessionContext: the
// session digest block WITHOUT the quickplan execution-context block. Used
// for plan requests in NON-quickplan modes (direct/plan/spec_plan) — e2e
// run 8 (2026-09-11) T3 classified git @0.9 and dispatched through
// createFallbackSteps, whose step prompt is req.Input verbatim; without
// this, a "did the change get made?" git dispatch executes with no session
// context at all. Returns "" for context-less sessions (no empty header
// block in the step prompt). Gated by the same MEEPT_DISABLE_DIGEST_CONTEXT
// flag as every other digest injection.
func (d *Dispatcher) PlanDigestContext(sessionID, excludeTaskID string) string {
	if !d.digestContextEnabled() {
		return ""
	}
	digest := d.buildSessionContextDigestExcluding(sessionID, excludeTaskID)
	if digest.IsEmpty() {
		return ""
	}
	return BuildSessionContextBlock(digest)
}

// SetPlanManager wires the plan manager after construction. The daemon
// creates the dispatcher before the plan system is initialized (same
// ordering constraint as Orchestrator.SetPlanManager); nil is a no-op and
// leaves the quickplan session-context block without its Active plan
// section.
func (d *Dispatcher) SetPlanManager(pm *plan.PlanManager) {
	if pm != nil {
		d.planManager = pm
	}
}
