package agent

import (
	"context"
	"fmt"
	"strings"
	"time"
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

// RecallAnswer is the option-3 continuity answer for a recall-intent
// follow-up. handled=false means the caller should fall through to the
// normal LLM path (no prior task, or nothing to say without an LLM).
func (d *Dispatcher) RecallAnswer(ctx context.Context, result *DispatchResult, conversationID string, wait bool) (string, bool) {
	if result == nil || result.Intent == nil || result.Intent.Type != string(IntentRecall) {
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
	default:
		sb.WriteString("The work above was still in progress; its stored result may not reflect the final state.\n")
	}
	return strings.TrimSpace(sb.String()), true
}
