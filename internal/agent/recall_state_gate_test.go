package agent

// 03-context TEST-GATE T4 (docs/plans/20261119-usability-remediation/
// 03-context/00T-gate.md, master contract C1/C4; findings F-ORCH-16,
// F-NEW-7, W10).
//
// Recall fidelity, two pins:
//   - TestRecallFailedState: a TERMINAL failure (failed/cancelled/rejected)
//     must be named plainly ("failed: <reason>"), NEVER "still in
//     progress". Today recall_answer.go:86-90's state switch defaults
//     every non-completed state - including failed - to the in-progress
//     sentence (F-ORCH-16(a)).
//   - TestRecallTaskSet: a plural artifact question ("what files did you
//     make for me in this session?") must cover the SESSION TASK SET, not
//     just the single most-recent task. Today the digest is last-task-only
//     (buildSessionContextDigestExcluding), so the hello.txt artifact is
//     absent when an unrelated task is newer (F-ORCH-16(b), F-NEW-7).
//
// RED today for both gate arms. GREEN after 03-context/02. Control arms
// (completed unchanged, non-terminal keeps in-progress) pass today and
// must keep passing.
//
// NOTE (deviation): the leaf names recall_answer_test.go, which already
// exists with the option-3 pins. These gate tests live in a sibling file
// so no existing test is touched; the -run regex matches by test name.

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// recallGateResult is the follow-up dispatch shape the recall lane answers.
func recallGateResult(question string) *DispatchResult {
	return &DispatchResult{
		Intent:        &Intent{Type: string(IntentRecall), Confidence: 0.9},
		OriginalInput: question,
	}
}

// TestRecallFailedState pins terminal-failure fidelity: the answer names
// the failure plainly and never claims the work is still in progress.
func TestRecallFailedState(t *testing.T) {
	// Gate arms: each terminal-failure state must read as terminal.
	for _, tc := range []struct {
		name  string
		state task.TaskState
	}{
		{"failed", task.StateFailed},
		{"cancelled", task.StateCancelled},
		{"rejected", task.StateRejected},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d, reg := newDigestTestDispatcher(t)
			const sess = "sess-recall-failed"
			seedDigestTask(t, d, "task-terminal-"+tc.name, "change hello.txt", sess,
				tc.state, time.Now().UTC(), "coder")
			seedDigestStep(t, reg, "task-terminal-"+tc.name, 0, task.StepFailed,
				"permission denied writing hello.txt")

			answer, handled := d.RecallAnswer(context.Background(),
				recallGateResult("did the change get made?"), sess, false)
			if !handled || answer == "" {
				t.Fatalf("recall not handled for a session with a prior %s task", tc.state)
			}
			lower := strings.ToLower(answer)
			if strings.Contains(lower, "still in progress") {
				t.Fatalf("RED (F-ORCH-16a): terminal %s task misreported as in-progress: %q", tc.state, answer)
			}
			if !strings.Contains(lower, string(tc.state)) {
				t.Errorf("answer does not name the terminal state %q plainly: %q", tc.state, answer)
			}
		})
	}

	// Control (passes today): a completed task never reads as in-progress.
	t.Run("completed unchanged", func(t *testing.T) {
		d, reg := newDigestTestDispatcher(t)
		const sess = "sess-recall-completed"
		seedDigestTask(t, d, "task-done", "create hello.txt", sess,
			task.StateCompleted, time.Now().UTC(), "coder")
		seedDigestStep(t, reg, "task-done", 0, task.StepApproved,
			"hello.txt created at /w/project/hello.txt")
		answer, handled := d.RecallAnswer(context.Background(),
			recallGateResult("did the change get made?"), sess, false)
		if !handled {
			t.Fatal("completed prior task not handled")
		}
		if strings.Contains(strings.ToLower(answer), "still in progress") {
			t.Errorf("completed task misreported as in-progress: %q", answer)
		}
	})

	// Control (passes today): a genuinely non-terminal task keeps the
	// in-progress text (the fix must not silence real running work).
	t.Run("non-terminal keeps in-progress", func(t *testing.T) {
		d, _ := newDigestTestDispatcher(t)
		const sess = "sess-recall-running"
		seedDigestTask(t, d, "task-running", "build the report", sess,
			task.StateExecuting, time.Now().UTC(), "coder")
		answer, handled := d.RecallAnswer(context.Background(),
			recallGateResult("did the change get made?"), sess, false)
		if !handled {
			t.Fatal("executing prior task not handled")
		}
		if !strings.Contains(strings.ToLower(answer), "still in progress") {
			t.Errorf("non-terminal task lost its in-progress answer: %q", answer)
		}
	})
}

// TestRecallTaskSet pins session task-set coverage: a plural artifact
// question must surface the session's artifacts (hello.txt), not only the
// single most-recent task.
func TestRecallTaskSet(t *testing.T) {
	d, reg := newDigestTestDispatcher(t)
	const sess = "sess-recall-taskset"
	now := time.Now().UTC()

	// The session's real artifact work: hello.txt creation, completed but
	// OLDER than the unrelated task below.
	seedDigestTask(t, d, "task-hello", "create a file named hello.txt", sess,
		task.StateCompleted, now.Add(-2*time.Minute), "coder")
	seedDigestStep(t, reg, "task-hello", 0, task.StepApproved,
		"The file hello.txt has been created. Full path: /w/project/hello.txt")

	// An unrelated, NEWER completed task (the TUI's interleaved turn in
	// F-ORCH-16). The last-task-only digest picks THIS one and drops the
	// hello.txt artifact the plural question is actually about.
	seedDigestTask(t, d, "task-capital", "what is the capital of France", sess,
		task.StateCompleted, now, "chat")
	seedDigestStep(t, reg, "task-capital", 0, task.StepApproved,
		"The capital of France is Paris.")

	answer, handled := d.RecallAnswer(context.Background(),
		recallGateResult("what files did you make for me in this session?"), sess, false)
	if !handled || answer == "" {
		t.Fatal("recall not handled for a session with a prior task set")
	}
	if !strings.Contains(strings.ToLower(answer), "hello.txt") {
		t.Fatalf("RED (F-ORCH-16b/F-NEW-7): plural artifact answer omits the session's hello.txt artifact (last-task-only digest): %q", answer)
	}
	// Bounded coverage: the task-set answer must stay a compact digest,
	// never an unbounded history dump (02-recall-fidelity review rule).
	if len(answer) > 4000 {
		t.Errorf("task-set answer unbounded (%d chars); must stay a bounded digest", len(answer))
	}
}
