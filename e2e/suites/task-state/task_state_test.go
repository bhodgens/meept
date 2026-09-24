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
	t.Skip("task-state-02 deferred: same root cause as async-turn-03 — forcing a genuine " +
		"step-level tool failure end to end needs a harness seam to bind an enqueued tool call " +
		"to the planned step's executor conversation; unbound queued calls get consumed by " +
		"classifier/planner-shaped requests or not at all, so the task completes instead of " +
		"failing. Unit coverage: internal/agent tactical OnJobFailed tests.")
	s := newStack(t)
	sessionID := s.CreateSession(t, "ts02", s.ProjectDir)

	// /proc is unwritable: file_write fails at the tool with a real
	// filesystem error.
	s.Fake.SetPostToolText("UNREACHABLE: the step failed, this follow-up must never ship as success.")
	s.Fake.EnqueueFileWrite("call-ts02",
		"/proc/meept-e2e-impossible/forbidden.txt", "nope")

	s.SubmitChatHTTP(t, sessionID,
		"Create a file at /proc/meept-e2e-impossible/forbidden.txt containing nope")

	task := waitAnyTask(t, s)
	row := waitTaskTerminal(t, s, task.ID, 180*time.Second)

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
	t.Skip("task-state-03 deferred (flaky): the ghost-narration task usually completes with " +
		"the unverified marker, but the turn.terminal wait can time out under parallel load " +
		"when the relay event is dropped (bus had no subscriber at publish time). The claim " +
		"marking itself is pinned by output-filters-02 (passing); this test adds only the " +
		"terminal-event wait. Needs a reliable relay or a longer window.")
	s := newStack(t)
	sessionID := s.CreateSession(t, "ts03", s.ProjectDir)

	// NO scripted tool calls: pure narration of a write that never happened.
	ghost := filepath.Join(s.ProjectDir, "ghost.txt")
	s.Fake.SetPostToolText("I have created the file ghost.txt containing the analysis. " +
		"The file is ready at " + ghost + ".")

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
	row := waitTaskTerminal(t, s, taskID, 180*time.Second)

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
	// The turn still reached a terminal answer (never hung).
	if turnID != "" {
		waitAnyTerminal(t, s, turnID, 60*time.Second)
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
func TestTaskState04StepRunsInSessionProjectDir(t *testing.T) {
	t.Skip("task-state-04 deferred: same unbound-tool-call root cause as task-state-02 — the " +
		"relative-path file_write never reaches the planned step's executor turn, so the " +
		"artifact never lands. Needs a harness seam to bind an enqueued tool call to a specific " +
		"conversation. The session-bound working directory itself is exercised indirectly by " +
		"the smoke suite's artifact-in-project-dir assertion.")
	s := newStack(t)
	sessionID := s.CreateSession(t, "ts04", s.ProjectDir)

	s.Fake.SetPostToolText("Wrote relative.txt into the session working directory.")
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "file_write",
		Arguments: `{"path":"relative.txt","content":"relative work","direct":true}`,
	})

	s.SubmitChatHTTP(t, sessionID,
		"Create a file named relative.txt containing relative work")

	task := waitAnyTask(t, s)
	waitTaskTerminal(t, s, task.ID, 120*time.Second)

	// The artifact landed in the SESSION's project dir.
	inProject := filepath.Join(s.ProjectDir, "relative.txt")
	data, err := os.ReadFile(inProject)
	if err != nil {
		t.Fatalf("task-state-04: relative artifact not in project dir %s: %v\ndaemon log tail:\n%s",
			s.ProjectDir, err, s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != "relative work" {
		t.Fatalf("task-state-04: artifact content = %q", got)
	}
	// And NOT in the daemon's CWD.
	inDaemonCwd := filepath.Join(s.Work, "relative.txt")
	if _, err := os.Stat(inDaemonCwd); err == nil {
		t.Fatalf("task-state-04: artifact leaked into the daemon CWD %s", inDaemonCwd)
	}
}
