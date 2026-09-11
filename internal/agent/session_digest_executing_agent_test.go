package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// ---------------------------------------------------------------------------
// e2e run 5 (2026-09-10) T3 pins: the executing agent's prompt carries no
// session context, so "did the change get made?" misrouted to git/committer
// and the committer answered "I don't have any evidence that a change was
// made" even though T1 had completed and stored its result in the session.
// Fix: buildContextMessage injects a compact digest block of the session's
// most recent PRIOR task, excluding the current turn's own just-created
// placeholder task.
// ---------------------------------------------------------------------------

// buildContextMessage must inject the digest of the session's most recent
// PRIOR task (T1: completed, holds the artifact result) — not the current
// turn's own placeholder task (T3: pending, no steps), which
// ClassifyAndRoute created seconds before RouteToAgent ran.
func TestBuildContextMessage_InjectsPriorTaskDigest(t *testing.T) {
	d, reg := newDigestTestDispatcher(t)

	now := time.Now().UTC()
	// T1: the prior completed task whose step holds the artifact answer.
	seedDigestTask(t, d, "task-t1", "create hello.txt", "sess-t3",
		task.StateCompleted, now.Add(-5*time.Minute), "coder")
	seedDigestStep(t, reg, "task-t1", 2, task.StepApproved,
		"/var/scratch/project/hello.txt contains hello")

	// T3: the current turn's placeholder — newest, must be excluded.
	seedDigestTask(t, d, "task-t3", "did the change get made", "sess-t3",
		task.StatePending, now, "committer")

	result := &DispatchResult{
		AgentID:       "committer",
		Intent:        &Intent{Type: string(IntentGit), Summary: "did the change get made"},
		OriginalInput: "did the change get made? where is the file?",
		Task: func() *task.Task {
			tk := task.NewTask("did the change get made", "current turn")
			tk.ID = "task-t3"
			return tk
		}(),
	}

	msg := d.buildContextMessage(result, "sess-t3")
	if !strings.Contains(msg, "hello.txt contains hello") {
		t.Fatalf("context message missing prior task result; got:\n%s", msg)
	}
	if !strings.Contains(msg, "## Session context") {
		t.Fatalf("context message missing session-context block header; got:\n%s", msg)
	}
	if strings.Contains(msg, "task-t1") == false && strings.Contains(msg, "create hello.txt") == false {
		t.Fatalf("context message does not name the prior task; got:\n%s", msg)
	}
	if strings.Contains(msg, `Prior task: "did the change get made"`) {
		t.Fatalf("digest leaked the current turn's own placeholder task; got:\n%s", msg)
	}
}

// A session with NO prior task (first turn, or only the current placeholder)
// must produce no session-context block — the contextless prompt shape is
// byte-preserved.
func TestBuildContextMessage_NoBlockWithoutPriorTask(t *testing.T) {
	d, _ := newDigestTestDispatcher(t)

	// Only the current turn's own task exists.
	seedDigestTask(t, d, "task-first", "make a thing", "sess-fresh",
		task.StatePending, time.Now().UTC(), "coder")

	result := &DispatchResult{
		AgentID:       "coder",
		Intent:        &Intent{Type: string(IntentCode), Summary: "make a thing"},
		OriginalInput: "make a thing",
		Task: func() *task.Task {
			tk := task.NewTask("make a thing", "current turn")
			tk.ID = "task-first"
			return tk
		}(),
	}

	msg := d.buildContextMessage(result, "sess-fresh")
	if strings.Contains(msg, "## Session context") {
		t.Fatalf("session-context block injected for a first-turn session; got:\n%s", msg)
	}
	if !strings.Contains(msg, "make a thing") {
		t.Fatalf("original input lost; got:\n%s", msg)
	}
}

// The exclusion is what makes the digest correct: with the current turn's
// task excluded, the digest surfaces the prior completed task; without it
// (legacy path), it would surface the pending placeholder.
func TestBuildSessionContextDigestExcluding_SkipsCurrentTurnTask(t *testing.T) {
	d, reg := newDigestTestDispatcher(t)

	now := time.Now().UTC()
	seedDigestTask(t, d, "task-old", "write report", "sess-x",
		task.StateCompleted, now.Add(-time.Hour), "coder")
	seedDigestStep(t, reg, "task-old", 0, task.StepApproved, "report.md written")
	seedDigestTask(t, d, "task-new", "status question", "sess-x",
		task.StatePending, now, "chat")

	got := d.buildSessionContextDigestExcluding("sess-x", "task-new")
	if got.IsEmpty() {
		t.Fatal("digest empty after excluding the current turn's task")
	}
	if got.LastTaskName != "write report" {
		t.Fatalf("LastTaskName = %q, want the prior task %q", got.LastTaskName, "write report")
	}
	if got.LastTaskState != string(task.StateCompleted) {
		t.Fatalf("LastTaskState = %q, want completed", got.LastTaskState)
	}

	// Control: excluding nothing surfaces the newest task (the placeholder) —
	// pinning WHY the exclusion parameter exists.
	legacy := d.buildSessionContextDigestExcluding("sess-x", "")
	if legacy.LastTaskName != "status question" {
		t.Fatalf("control: LastTaskName = %q, want the newest task %q",
			legacy.LastTaskName, "status question")
	}
}

// Rendering contract for the executing-agent block: bounded, factual,
// human-language header (not a machine catalog — the reply guard must
// never trip on it).
func TestBuildSessionContextBlock_Render(t *testing.T) {
	digest := &SessionContextDigest{
		LastTaskName:      "create hello.txt",
		LastTaskState:     string(task.StateCompleted),
		LastTaskAgent:     "coder",
		LastResultSummary: "/tmp/p/hello.txt contains hello",
		WorkingDirectory:  "/tmp/p",
	}
	block := BuildSessionContextBlock(digest)
	for _, want := range []string{
		"## Session context",
		`Prior task: "create hello.txt" — status: completed (agent: coder)`,
		"Prior result: /tmp/p/hello.txt contains hello",
		"Working directory: /tmp/p",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing %q; got:\n%s", want, block)
		}
	}

	// Context-usage rule (e2e A5, gh #37): the block must instruct the
	// model to ANSWER FROM the context for questions about prior work —
	// with specifics — instead of treating it as passive background.
	for _, want := range []string{
		"asks about, refers to, or follows up on this work",
		"name the specific files, paths, and results",
	} {
		if !strings.Contains(block, want) {
			t.Fatalf("block missing usage rule %q; got:\n%s", want, block)
		}
	}

	if BuildSessionContextBlock(nil) != "" {
		t.Fatal("nil digest must render as empty string")
	}
	if BuildSessionContextBlock(&SessionContextDigest{}) != "" {
		t.Fatal("empty digest must render as empty string")
	}
}

// Run-7 T3 pin: the quickplan route must carry the digest block into the
// plan request's session context, so the PLANNER sees T1's completed result
// and stops writing "Ask user for the file path" steps for status
// questions about prior work.
func TestBuildPlanSessionContext_ComposesDigestIntoExecutionContext(t *testing.T) {
	d, reg := newDigestTestDispatcher(t)

	now := time.Now().UTC()
	// Open task OLDER than T1: it exercises the execution-context block
	// (open titles) while T1 stays the most recent non-excluded task the
	// digest half must surface.
	seedDigestTask(t, d, "task-open", "unrelated open work", "sess-plan",
		task.StateExecuting, now.Add(-10*time.Minute), "coder")
	seedDigestTask(t, d, "task-t1", "create hello.txt", "sess-plan",
		task.StateCompleted, now.Add(-5*time.Minute), "coder")
	seedDigestStep(t, reg, "task-t1", 2, task.StepApproved,
		"/var/p/hello.txt contains hello")

	// Current turn's placeholder must be excluded from the digest half.
	got := d.BuildPlanSessionContext(context.Background(), "sess-plan", "task-cur")
	if !strings.Contains(got, "hello.txt contains hello") {
		t.Fatalf("plan session context missing prior task result; got:\n%s", got)
	}
	if !strings.Contains(got, "## Session execution context") {
		t.Fatalf("plan session context lost the execution-context block; got:\n%s", got)
	}
	if !strings.Contains(got, "## Session context") {
		t.Fatalf("plan session context lost the digest block header; got:\n%s", got)
	}

	// Context-less session: digest empty, exec-context empty → empty overall
	// (no empty header block for the planner).
	fresh := d.BuildPlanSessionContext(context.Background(), "sess-none", "")
	if fresh != "" {
		t.Fatalf("context-less session must produce empty plan session context; got:\n%s", fresh)
	}
}
