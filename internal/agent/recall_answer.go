package agent

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// Option-3 continuity answers for recall-intent follow-ups (2026-09-25 A5,
// design in issue #58 comment): "did the change get made?" should be answered
// from the prior task's STORED RESULT, not re-asked through an LLM whose
// prompt raced the prior turn's completion.
//
// Three branches, in order:
//  1. prior task TERMINAL → answer directly from its stored result
//     (best step result, envelope-stripped prose; no LLM call).
//  2. prior task non-terminal and the caller allows waiting → bounded poll
//     for terminal state (recalls follow work the user just watched; a short
//     bounded wait converts "still running" into the real answer).
//  3. still non-terminal → explicit in-progress answer naming the task.
//
// Every branch names the task, so the A5 assertion (reply references the
// work) holds whenever there is anything to reference.

// recallWaitTimeout bounds branch 2's wait for a non-terminal prior task.
const recallWaitTimeout = 90 * time.Second

// recallPollInterval is the branch-2 poll cadence. 2s matches the sync-wait
// poll; faster polling buys nothing against a multi-second model call.
const recallPollInterval = 2 * time.Second

// RecallAnswer is the option-3 continuity answer for a follow-up question
// about prior work. The dispatcher gates the call on SHAPE
// (referencesPriorWork && isInterrogative, or the platform-labeled sibling),
// never on the intent label: the 8B classifier labels the same question
// recall/chat/work/platform across runs, so a label check here made the
// shape-gated branch a no-op for every non-recall label. handled=false means
// the caller should fall through to the normal LLM path (no prior task, or
// nothing to say without an LLM).
func (d *Dispatcher) RecallAnswer(ctx context.Context, result *DispatchResult, conversationID string, wait bool) (string, bool) {
	if result == nil || result.Intent == nil {
		return "", false
	}
	excludeID := ""
	if result.Task != nil {
		excludeID = result.Task.ID
	}
	digest := d.buildSessionContextDigestExcluding(conversationID, excludeID)
	if digest == nil || digest.IsEmpty() || digest.LastTaskID == "" {
		return "", false
	}

	// Branch 2: bounded wait for the in-flight prior task.
	if d.taskRegistry != nil && wait {
		deadline := time.Now().Add(recallWaitTimeout)
		for {
			t, err := d.taskRegistry.Get(context.Background(), digest.LastTaskID)
			if err == nil && t != nil && t.State.IsTerminal() {
				break
			}
			if time.Now().After(deadline) {
				break
			}
			select {
			case <-ctx.Done():
				return "", false
			case <-time.After(recallPollInterval):
			}
		}
		digest = d.buildSessionContextDigestExcluding(conversationID, excludeID)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Prior task: %q — status: %s", digest.LastTaskName, digest.LastTaskState)
	if digest.LastTaskAgent != "" {
		fmt.Fprintf(&sb, " (agent: %s)", digest.LastTaskAgent)
	}
	sb.WriteString("\n")
	if digest.LastResultSummary != "" {
		fmt.Fprintf(&sb, "Result: %s\n", digest.LastResultSummary)
	}
	if digest.WorkingDirectory != "" {
		fmt.Fprintf(&sb, "Working directory: %s\n", digest.WorkingDirectory)
	}
	switch strings.ToLower(digest.LastTaskState) {
	case "completed", "approved":
	case "failed", "cancelled", "rejected":
		// Terminal-failure fidelity (03-context/02 T1, F-ORCH-16a): a
		// failed/cancelled/rejected task is FINISHED work that did not
		// succeed. The old default branch told the user it was "still
		// in progress" -- a false promise of future delivery. Name the
		// terminal state plainly instead; the stored result above (when
		// present) carries the reason.
		switch strings.ToLower(digest.LastTaskState) {
		case "failed":
			sb.WriteString("The task failed; the work did not complete.\n")
		case "cancelled":
			sb.WriteString("The task was cancelled; the work did not complete.\n")
		case "rejected":
			sb.WriteString("The task was rejected; the work did not complete.\n")
		}
	default:
		sb.WriteString("The work above was still in progress; its stored result may not reflect the final state.\n")
	}
	// Session task-set coverage (03-context/02 T2, F-ORCH-16b/F-NEW-7):
	// a plural artifact question ("what files did you make?") is about
	// the SESSION's work, not only the newest task. List the remaining
	// recent tasks with their stored results, bounded.
	sb.WriteString(d.renderRecallTaskSet(conversationID, excludeID, digest.LastTaskID))
	return truncateString(strings.TrimSpace(sb.String()), recallAnswerCap), true
}

// Recall task-set bounds (03-context/02 T2): the answer must stay a
// compact digest, never an unbounded history dump (gate T4 caps the
// whole answer at 4000 chars). 10 tasks x (100-char name + 200-char
// result) + the header stays under the cap; recallAnswerCap is the
// final hard bound.
const (
	recallTaskSetCap       = 10
	recallTaskSetNameCap   = 100
	recallTaskSetResultCap = 200
	recallAnswerCap        = 3800
)

// renderRecallTaskSet lists the session's other recent tasks (newest
// first, bounded) with their best stored step results. The header task
// (lastTaskID) and the current turn's own task (excludeTaskID) are
// skipped -- the header already covers the former. Store access is
// nil-guarded; any error degrades to "" (the header-only answer).
func (d *Dispatcher) renderRecallTaskSet(sessionID, excludeTaskID, lastTaskID string) string {
	if d.taskStore == nil {
		return ""
	}
	tasks, err := d.taskStore.GetTasksForSession(sessionID)
	if err != nil {
		d.logger.Debug("Failed to get tasks for recall task set",
			"session_id", sessionID,
			"error", err,
		)
		return ""
	}
	var set []*task.Task
	for _, t := range tasks {
		if t == nil || t.ID == excludeTaskID || t.ID == lastTaskID {
			continue
		}
		set = append(set, t)
	}
	if len(set) == 0 {
		return ""
	}
	sort.Slice(set, func(i, j int) bool { return set[i].UpdatedAt.After(set[j].UpdatedAt) })
	if len(set) > recallTaskSetCap {
		set = set[:recallTaskSetCap]
	}
	var sb strings.Builder
	sb.WriteString("Other work in this session:\n")
	for _, t := range set {
		fmt.Fprintf(&sb, "- %q (status: %s)", truncateString(t.Name, recallTaskSetNameCap), t.State)
		if summary := d.recallTaskResultSummary(t.ID); summary != "" {
			fmt.Fprintf(&sb, ": %s", summary)
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// recallTaskResultSummary returns the bounded best-step-result summary
// for one task, mirroring populateDigestStepResult's selection rule.
func (d *Dispatcher) recallTaskResultSummary(taskID string) string {
	if d.taskRegistry == nil {
		return ""
	}
	stepStore := d.taskRegistry.StepStore()
	if stepStore == nil {
		return ""
	}
	steps, err := stepStore.ListByTaskID(taskID)
	if err != nil {
		d.logger.Debug("Failed to list steps for recall task set",
			"task_id", taskID,
			"error", err,
		)
		return ""
	}
	return truncateString(informativeResultLine(bestStepResult(steps)), recallTaskSetResultCap)
}
