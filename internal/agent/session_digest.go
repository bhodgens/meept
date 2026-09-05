package agent

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
