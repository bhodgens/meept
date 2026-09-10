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
		} else if len(tasks) > 0 {
			// GetTasksForSession is ordered by updated_at DESC, so
			// tasks[0] is the most recently updated task.
			lastTask := tasks[0]
			if lastTask != nil {
				digest.LastTaskName = truncateString(lastTask.Name, digestTaskNameCap)
				digest.LastTaskState = string(lastTask.State)
				digest.LastTaskAgent = lastTask.AssignedAgent
				d.populateDigestStepResult(digest, lastTask.ID, sessionID)
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
