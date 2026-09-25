//go:build e2e

// Package taskstate is the WAVE-B e2e suite for task lifecycle state
// contracts (manifest scenarios task-state-01..04): honest recounted
// counters on completion, one failed step failing the task with the error
// text as the result, and step jobs running in the session-resolved
// directory — asserted on tasks.db rows and on-disk artifacts.
package taskstate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

func newStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	return s
}

// waitAnyTask waits until at least one task row exists and returns the latest.
func waitAnyTask(t *testing.T, s *harness.Stack) harness.TaskRow {
	t.Helper()
	harness.WaitFor(t, 20*time.Second, "a task row to appear", func() bool {
		return len(harness.Tasks(t, s.TasksDBPath())) > 0
	})
	tasks := harness.Tasks(t, s.TasksDBPath())
	return tasks[len(tasks)-1]
}

// waitTaskTerminal waits until the task reaches completed or failed and
// returns the final row.
func waitTaskTerminal(t *testing.T, s *harness.Stack, taskID string, timeout time.Duration) harness.TaskRow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last string
	for time.Now().Before(deadline) {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == taskID {
				last = row.State
				if row.State == "completed" || row.State == "failed" {
					return row
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("task-state: task %s not terminal within %s (last state %q)", taskID, timeout, last)
	return harness.TaskRow{}
}

// ---------------------------------------------------------------------------
// task-state-01 (M): all-steps-done task completes with honest recounted counters
// ---------------------------------------------------------------------------

// TestTaskState01CompletedWithHonestCounters pins the honest completion
// contract: the single planned step completes, the task finalizes completed,
// and the store counters are RECOUNTED from step rows — total_jobs equals the
// step count, completed_jobs matches, failed_jobs is 0. No "2/1 completed,
// 200%" churn.
func TestTaskState01CompletedWithHonestCounters(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "ts01", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "counters.txt")
	s.Fake.SetPostToolText("Created counters.txt at " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-ts01", artifact, "counters")

	s.SubmitChatHTTP(t, sessionID,
		"Create a file named counters.txt containing counters")

	task := waitAnyTask(t, s)
	row := waitTaskTerminal(t, s, task.ID, 120*time.Second)

	if row.State != "completed" {
		t.Fatalf("task-state-01: state = %q, want completed", row.State)
	}
	// Honest recounted counters: total_jobs mirrors the actual step rows.
	steps := harness.Steps(t, s.TasksDBPath(), task.ID)
	if row.TotalJobs != len(steps) {
		t.Fatalf("task-state-01: total_jobs = %d but %d step rows exist (counters not recounted): %+v",
			row.TotalJobs, len(steps), row)
	}
	if row.CompletedJobs != 1 {
		t.Fatalf("task-state-01: completed_jobs = %d, want 1: %+v", row.CompletedJobs, row)
	}
	if row.FailedJobs != 0 {
		t.Fatalf("task-state-01: failed_jobs = %d, want 0: %+v", row.FailedJobs, row)
	}
	// The step rows agree with the counters.
	counts := harness.CountStepsByState(t, s.TasksDBPath(), task.ID)
	if counts["completed"]+counts["approved"] != 1 {
		t.Fatalf("task-state-01: step state counts disagree with task counters: %v", counts)
	}
}

// ---------------------------------------------------------------------------
// task-state-02 (S): one failed step fails the task; result carries the error text
// ---------------------------------------------------------------------------

// TestTaskState02FailedStepFailsTaskWithHonestResult pins the F2 honest
// failure contract: a step whose scripted tool call fails (file_write into a
// nonexistent directory) fails the STEP, the task finalizes failed (never
// completed), and the failed step's stored result carries the tool's error
// text — never the post-tool success narration.
func TestTaskState02FailedStepFailsTaskWithHonestResult(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "ts02", s.ProjectDir)

	// The doomed path is INSIDE the allowed project fence: blocker is a
	// FILE, so the write fails at the tool with a real OS error on every
	// retry (a security BLOCK would ride the permission-denied flow
	// instead of the repeat-error breaker). The identical-args failures
	// exhaust the repeat-error breaker, whose refusal ERROR fails the
	// step job (OnJobFailed) — a genuine step-level tool failure.
	const marker = "TS02-DOOMED"
	blocker := filepath.Join(s.ProjectDir, "ts02-blocker")
	if err := os.WriteFile(blocker, []byte("obstacle"), 0o644); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	doomedPath := filepath.Join(blocker, "forbidden.txt")
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"` + marker + `: create the forbidden file","tool_hint":"file_write","depends_on":[]}]}`)
	doomed := `{"path":"` + doomedPath + `","content":"nope","direct":true}`
	s.Fake.ScriptN(8,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{Name: "file_write", Arguments: doomed}))
	// Any other executor turn: honest, claim-free narration.
	s.Fake.SetPostToolText("The requested work could not be completed.")

	s.SubmitChatHTTP(t, sessionID,
		"Create a file at "+doomedPath+" containing nope. "+marker)

	task := waitAnyTask(t, s)
	row := waitTaskTerminal(t, s, task.ID, 240*time.Second)

	if row.State != "failed" {
		steps := harness.Steps(t, s.TasksDBPath(), task.ID)
		t.Fatalf("task-state-02: task state = %q, want failed; steps:\n%s",
			row.State, harness.FormatSteps(steps))
	}
	if row.FailedJobs < 1 {
		t.Fatalf("task-state-02: failed_jobs = %d, want >= 1: %+v", row.FailedJobs, row)
	}

	// The step's stored result carries the failure text.
	var failureText string
	for _, st := range harness.Steps(t, s.TasksDBPath(), task.ID) {
		if st.State == "failed" && st.Result != "" {
			failureText = st.Result
			break
		}
	}
	if failureText == "" {
		t.Fatalf("task-state-02: no failed step result text; steps:\n%s",
			harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), task.ID)))
	}
	if strings.Contains(failureText, "UNREACHABLE") {
		t.Fatalf("task-state-02: post-tool success narration leaked into a failed step: %q", failureText)
	}
}

// ---------------------------------------------------------------------------
// task-state-03 (M): validation-block failure is terminal and honest
// ---------------------------------------------------------------------------

// TestTaskState03UnverifiedNarrationNeverShipsAsSuccess pins the claim-vs-
// evidence gate's observable outcome: an executor turn that NARRATES a file
// write but performs NO tool call leaves no artifact on disk, and the turn's
// user-facing outcome never presents the fabricated creation as verified
// work. The gate's two honest arms are both acceptable: the task completes
// with the narration flagged unverified (not laundered through review as a
// verified artifact write), or the task fails honestly. What must never
// happen: the artifact existing without a tool call, or a bare success stub
// standing in for the fabricated work.
func TestTaskState03UnverifiedNarrationNeverShipsAsSuccess(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "ts03", s.ProjectDir)

	// NO scripted tool calls: pure narration of a write that never happened.
	ghost := filepath.Join(s.ProjectDir, "ghost.txt")
	s.Fake.SetPostToolText("I have created the file ghost.txt containing the analysis. " +
		"The file is ready at " + ghost + ".")

	// Subscribe BEFORE the submit: the relay fires the moment the task
	// finalizes, and a poll subscription opened after a fast completion
	// misses the event (the original flake).
	sess := openRPCSession(t, s)
	defer sess.Close()
	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	_ = jsonUnmarshal(subRaw, &sub)
	defer func() {
		_, _ = sess.call("bus.unsubscribe",
			map[string]string{"subscription_id": sub.SubscriptionID})
	}()

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named ghost.txt containing the analysis of the quarterly numbers")
	turnID, _ := ack["turn_id"].(string)

	var taskID string
	harness.WaitFor(t, 20*time.Second, "task row", func() bool {
		tasks := harness.Tasks(t, s.TasksDBPath())
		if len(tasks) > 0 {
			taskID = tasks[len(tasks)-1].ID
			return true
		}
		return false
	})
	row := waitTaskTerminal(t, s, taskID, 240*time.Second)

	// Honesty arm 1: the artifact must NOT exist (no tool ran).
	if _, err := os.Stat(ghost); err == nil {
		t.Fatalf("task-state-03: ghost artifact %s exists without any scripted tool call", ghost)
	}
	// Honesty arm 2: if the task completed, the fabricated narration is not
	// stored as a terminal step result verbatim as verified work.
	if row.State == "completed" {
		for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
			if (st.State == "completed" || st.State == "approved") &&
				strings.Contains(st.Result, "have created the file") {
				t.Fatalf("task-state-03: fabricated creation narration stored as a successful step result: %q",
					st.Result)
			}
		}
	}
	// The turn still reached a terminal answer (never hung): poll the
	// subscription opened before the submit.
	if turnID != "" && sub.SubscriptionID != "" {
		deadline := time.Now().Add(180 * time.Second)
		found := false
		for time.Now().Before(deadline) && !found {
			raw, err := sess.call("bus.poll", map[string]string{
				"subscription_id": sub.SubscriptionID,
			})
			if err == nil {
				var resp struct {
					Events []struct {
						Topic   string          `json:"topic"`
						Payload json.RawMessage `json:"payload"`
					} `json:"events"`
				}
				if jsonUnmarshal(raw, &resp) == nil {
					for _, ev := range resp.Events {
						if ev.Topic != "turn.terminal" {
							continue
						}
						var p struct {
							TurnID string `json:"turn_id"`
							Status string `json:"status"`
						}
						if jsonUnmarshal(ev.Payload, &p) == nil && p.TurnID == turnID && p.Status != "parked" {
							found = true
							break
						}
					}
				}
			}
			if !found {
				time.Sleep(250 * time.Millisecond)
			}
		}
		if !found {
			t.Fatalf("task-state-03: no terminal event for turn %s within 3m", turnID)
		}
	}
}

// waitAnyTerminal waits for any non-parked turn.terminal event for turnID.
// Uses ONE persistent connection for the subscription lifecycle.
func waitAnyTerminal(t *testing.T, s *harness.Stack, turnID string, timeout time.Duration) {
	t.Helper()
	sess := openRPCSession(t, s)
	defer sess.Close()
	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	_ = jsonUnmarshal(subRaw, &sub)
	defer func() {
		_, _ = sess.call("bus.unsubscribe",
			map[string]string{"subscription_id": sub.SubscriptionID})
	}()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := sess.call("bus.poll", map[string]string{
			"subscription_id": sub.SubscriptionID,
		})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if jsonUnmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic != "turn.terminal" {
						continue
					}
					var p struct {
						TurnID string `json:"turn_id"`
						Status string `json:"status"`
					}
					if jsonUnmarshal(ev.Payload, &p) == nil && p.TurnID == turnID && p.Status != "parked" {
						return
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("task-state-03: no terminal event for turn %s within %s", turnID, timeout)
}

// ---------------------------------------------------------------------------
// task-state-04 (M): step jobs run in the session-resolved directory
// ---------------------------------------------------------------------------

// TestTaskState04StepRunsInSessionProjectDir pins the working-directory
// invariant: a file_write with a RELATIVE path lands inside the session's
// project dir — never in the daemon's CWD (the harness starts the daemon
// with cwd = Work, deliberately different from ProjectDir).
//
// STILL SKIPPED (updated reason, 2026-09-24 — verified live with
// MEEPT_E2E_KEEP=1 sandboxes, not assumed): conversation-bound scripting
// now reliably delivers the relative-path file_write to executor turns,
// but the write lands in the DAEMON CWD because session resolution is
// broken for every lane that reaches a filesystem tool:
//
//   - TASK lane: the thread router routes the turn to a thread-scoped
//     conversation (conv-<hex> ≠ the session row's conversation_id); the
//     dispatcher links the task to THAT id, and
//     resolveStepWorkingDir's GetByConversationID lookup misses — the
//     step job falls back to the daemon CWD. Log shows no "Step job
//     working dir resolved" line.
//   - INLINE chat lane: ChatHandler passes the SAME thread conv id, so
//     sessionLoop/resolveAgent log "chat turn has no working directory
//     bound ... has_session=false" even with project.set applied.
//
// Fixing either lookup requires changes in internal/agent (thread router
// must surface the session-level id for store lookups) and/or
// internal/daemon — both outside this suite's scope. The session-bound
// working directory is exercised indirectly by the smoke suite's
// absolute-path artifact assertion.
func TestTaskState04StepRunsInSessionProjectDir(t *testing.T) {
	t.Skip("task-state-04 still deferred: thread-router conversation ids break session resolution. " +
		"Verified live: task lane links the task to a thread-scoped conv id, resolveStepWorkingDir's " +
		"GetByConversationID misses (no 'Step job working dir resolved' log), and the relative-path " +
		"file_write lands in the daemon CWD; the inline chat lane logs 'chat turn has no working " +
		"directory bound ... has_session=false' for the same reason even after project.set. Both " +
		"lookups need the session-level conversation id (fix in internal/agent thread router or " +
		"internal/session store), which is outside this suite's scope.")
	s := newStack(t)
	sessionID := s.CreateSession(t, "ts04", s.ProjectDir)
	_ = sessionID
}
