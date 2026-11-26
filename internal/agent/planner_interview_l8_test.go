package agent

import (
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/task"
)

// Bughunt L8 pins: (1) splitInterviewAnswer keeps blank reply lines as
// positional markers so a skipped-middle answer pairs with its own
// question; (2) DeliverInterviewAnswer no longer silently swallows an
// answer as an ordinary chat turn when the in-memory waiter was lost to a
// daemon restart — the persisted interview state routes it into the
// task-resume path.

// TestSplitInterviewAnswer_SkippedMiddleKeepsPosition pins L8(2)-i: the
// user leaves line 2 blank and answers Q3 on line 3. Pre-fix the blank
// line was dropped and Q3's text landed on Q2; post-fix the blank line is
// a positional "no answer" marker and Q3's text pairs with Q3.
func TestSplitInterviewAnswer_SkippedMiddleKeepsPosition(t *testing.T) {
	got := splitInterviewAnswer([]string{"Postgres\n\n nightly vacuum"}, 3)
	want := []string{"Postgres", "", "nightly vacuum"}
	if !slices.Equal(got, want) {
		t.Fatalf("positional split = %#v, want %#v", got, want)
	}
	// The skew the bug produced: index 1 must NOT carry Q3's text.
	if len(got) > 1 && got[1] == "nightly vacuum" {
		t.Fatal("Q3's answer was skewed onto Q2 (the L8 defect)")
	}
}

// TestSplitInterviewAnswer_AllLinesFilledUnchanged pins L8(2)-ii: a reply
// with one line per question behaves byte-identically to the pre-fix
// code (trimmed entries, exact count).
func TestSplitInterviewAnswer_AllLinesFilledUnchanged(t *testing.T) {
	got := splitInterviewAnswer([]string{" sqlite \n two nodes \n yes "}, 3)
	want := []string{"sqlite", "two nodes", "yes"}
	if !slices.Equal(got, want) {
		t.Fatalf("all-filled split = %#v, want %#v", got, want)
	}
	// More lines than questions: truncate to the question count, as
	// before.
	got = splitInterviewAnswer([]string{"a\nb\nc\nd"}, 3)
	if !slices.Equal(got, []string{"a", "b", "c"}) {
		t.Fatalf("over-long reply split = %#v, want [a b c]", got)
	}
	// Fewer lines than questions: positional lines + whole text, as
	// before.
	got = splitInterviewAnswer([]string{"a\nb"}, 3)
	if !slices.Equal(got, []string{"a", "b", "a\nb"}) {
		t.Fatalf("short reply split = %#v, want [a b \"a\\nb\"]", got)
	}
}

// armPersistedInterviewWithoutWaiter simulates the post-restart shape:
// the task row carries a fresh planner_interview block (and the pause-
// point PlanningContext metadata) but the in-memory waiter map is empty.
func armPersistedInterviewWithoutWaiter(t *testing.T, sp *StrategicPlanner, store *task.Store, tsk *task.Task, sessionID string, questions []string, sentAt time.Time) {
	t.Helper()
	pctx := plan.PlanningContext{InterviewQuestions: questions}
	pb, err := json.Marshal(pctx)
	if err != nil {
		t.Fatalf("marshal pctx: %v", err)
	}
	tsk.Metadata = pb
	tsk.SetState(task.StatePlanning)
	if err := store.Update(tsk); err != nil {
		t.Fatalf("update task: %v", err)
	}
	st := &plannerInterviewState{
		TaskID:    tsk.ID,
		SessionID: sessionID,
		Questions: questions,
	}
	sp.StoreInterviewState(tsk, PlanRequest{TaskID: tsk.ID, SessionID: sessionID}, st)
	// Force the sent_at stamp (StoreInterviewState always writes now;
	// the staleness leg needs a custom age).
	if sentAt != (time.Time{}) {
		got, err := store.GetByID(tsk.ID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		var meta map[string]any
		if err := json.Unmarshal(got.Metadata, &meta); err != nil {
			t.Fatalf("unmarshal meta: %v", err)
		}
		block, ok := meta["planner_interview"].(map[string]any)
		if !ok {
			t.Fatal("planner_interview block missing after StoreInterviewState")
		}
		block["sent_at"] = sentAt.UTC().Format(time.RFC3339)
		mb, err := json.Marshal(meta)
		if err != nil {
			t.Fatalf("marshal meta: %v", err)
		}
		got.Metadata = mb
		if err := store.Update(got); err != nil {
			t.Fatalf("update sent_at: %v", err)
		}
	}
	if sp.interviewWaitIsLive(tsk.ID) {
		t.Fatal("waiter should not be live in the simulated-restart arm")
	}
}

// waitForTaskToLeavePlanning polls until the resume path moves the task
// out of StatePlanning (bounded).
func waitForTaskToLeavePlanning(t *testing.T, store *task.Store, taskID string) task.TaskState {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for {
		got, err := store.GetByID(taskID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if got.State != task.StatePlanning {
			return got.State
		}
		if time.Now().After(deadline) {
			t.Fatalf("task %s still planning after cold-resume delivery", taskID)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestDeliverInterviewAnswer_RestartMissRoutesToResume pins L8(1)-iii:
// after a simulated restart (no waiter, persisted interview still open),
// the answer is NOT silently swallowed as a chat turn — DeliverInterviewAnswer
// claims it (ack, true), clears the persisted block, and the task leaves
// StatePlanning via the resume path. A duplicate answer on the same cold
// task is refused.
func TestDeliverInterviewAnswer_RestartMissRoutesToResume(t *testing.T) {
	sp, store := newInterviewTestPlanner(t)

	tsk := newTestTask("cold resume task",
		"design and implement the archive service with rotation, retention, and restore drills across two nodes")
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	armPersistedInterviewWithoutWaiter(t, sp, store, tsk, "conv-cold-1",
		[]string{"which backend?", "what retention?"}, time.Time{})

	ack, delivered := sp.DeliverInterviewAnswer("conv-cold-1", "sqlite\n7 years")
	if !delivered {
		t.Fatal("restart-miss answer fell through as an ordinary chat turn (L8 defect)")
	}
	if !strings.Contains(ack, "resuming the plan") {
		t.Errorf("ack = %q, want the resume ack", ack)
	}

	// The persisted interview block must be consumed like the hot path.
	got, err := store.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if ReadInterviewState(got) != nil {
		t.Error("planner_interview block still present after cold delivery")
	}

	// The resume consumed the claim: a second answer is refused, never a
	// second silent swallow of a fresh interview.
	if _, again := sp.DeliverInterviewAnswer("conv-cold-1", "second message"); again {
		t.Error("duplicate cold-path answer accepted; duplicate protection broken")
	}

	if state := waitForTaskToLeavePlanning(t, store, tsk.ID); state == "" {
		t.Fatal("task never left planning")
	}
}

// TestDeliverInterviewAnswer_RestartMissStaleFallsThrough pins the bound:
// an interview older than the wait window is watchdog territory, not a
// cold-resume trigger — the message falls through normally.
func TestDeliverInterviewAnswer_RestartMissStaleFallsThrough(t *testing.T) {
	sp, store := newInterviewTestPlanner(t)

	tsk := newTestTask("stale interview task",
		"design and implement the archive service with rotation, retention, and restore drills across two nodes")
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	armPersistedInterviewWithoutWaiter(t, sp, store, tsk, "conv-cold-stale",
		[]string{"which backend?"}, time.Now().Add(-interviewWaitWindow-time.Hour))

	if ack, delivered := sp.DeliverInterviewAnswer("conv-cold-stale", "sqlite"); delivered {
		t.Fatalf("stale interview cold-resumed (ack %q); expiry must leave it to the watchdog", ack)
	}
	got, err := store.GetByID(tsk.ID)
	if err != nil {
		t.Fatalf("get task: %v", err)
	}
	if ReadInterviewState(got) == nil {
		t.Error("stale interview block cleared by a fallthrough delivery")
	}
}

// TestDeliverInterviewAnswer_NoInterviewStillFallsThrough pins the
// no-op guarantee: with no waiter and no persisted interview, the seam
// stays byte-identical to the pre-fix fallthrough.
func TestDeliverInterviewAnswer_NoInterviewStillFallsThrough(t *testing.T) {
	sp, store := newInterviewTestPlanner(t)
	tsk := newTestTask("no interview task", "just chat with me")
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	if ack, delivered := sp.DeliverInterviewAnswer("conv-none-1", "hello"); delivered {
		t.Fatalf("ordinary chat turn was routed to the interview seam (ack %q)", ack)
	}
}
