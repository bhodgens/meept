package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
)

// Gate tests for interactive-usability leaf 04 (planner interview
// questions surfaced as session messages + answer seam + watchdog
// exemption). In-package per the leaf contract.

// newInterviewTestPlanner builds a StrategicPlanner over a real temp
// task store with the interview LLM path unused (awaitInterviewAnswers
// and the answer seam never call the planner model).
func newInterviewTestPlanner(t *testing.T) (*StrategicPlanner, *task.Store) {
	t.Helper()
	store, err := newTestTaskStore(t.TempDir())
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	sp := NewStrategicPlanner(StrategicPlannerConfig{
		TaskStore: store,
		StepStore: store.StepStore(),
		Logger:    slogDiscardLogger(),
	})
	return sp, store
}

// TestInterviewQuestionsPersistedAsMessage pins master C-3 shape: at the
// interview pause point the questions are persisted as an assistant
// session message whose metadata carries planner_interview = task_id.
func TestInterviewQuestionsPersistedAsMessage(t *testing.T) {
	sp, store := newInterviewTestPlanner(t)

	var saved []session.Message
	sp.SetInterviewMessageSink(func(sessionID string, msg session.Message) error {
		if sessionID != "conv-interview-1" {
			t.Errorf("sink sessionID = %q, want conv-interview-1", sessionID)
		}
		saved = append(saved, msg)
		return nil
	})

	tsk := newTestTask("interview task", "build a multi-phase report pipeline")
	tsk.SetState(task.StatePlanning)
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := PlanRequest{TaskID: tsk.ID, SessionID: "conv-interview-1"}
	pctx := &plan.PlanningContext{
		InterviewQuestions: []string{"which storage engine?", "what retention window?"},
	}
	if err := sp.awaitInterviewAnswers(context.Background(), tsk, req, pctx); err != nil {
		t.Fatalf("awaitInterviewAnswers: %v", err)
	}

	if len(saved) != 1 {
		t.Fatalf("sink received %d messages, want 1", len(saved))
	}
	msg := saved[0]
	if msg.Role != "assistant" {
		t.Errorf("role = %q, want assistant", msg.Role)
	}
	for _, q := range pctx.InterviewQuestions {
		if !strings.Contains(msg.Content, q) {
			t.Errorf("message content %q missing question %q", msg.Content, q)
		}
	}
	if got := msg.Metadata[PlannerInterviewMetaKey]; got != tsk.ID {
		t.Errorf("metadata[%s] = %q, want task id %q", PlannerInterviewMetaKey, got, tsk.ID)
	}
}

// TestInterviewAnswerResumesPlanner pins the answer path: the next user
// message in the same conversation is delivered to the waiting planner,
// which resumes decomposition past the interview; a second message in
// the same conversation behaves normally (duplicate-answer protection).
func TestInterviewAnswerResumesPlanner(t *testing.T) {
	sp, store := newInterviewTestPlanner(t)

	tsk := newTestTask("resume task", "design and implement the archive service")
	tsk.SetState(task.StatePlanning)
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}

	req := PlanRequest{TaskID: tsk.ID, SessionID: "conv-resume-1"}
	pctx := &plan.PlanningContext{InterviewQuestions: []string{"which backend?"}}
	if err := sp.awaitInterviewAnswers(context.Background(), tsk, req, pctx); err != nil {
		t.Fatalf("awaitInterviewAnswers: %v", err)
	}

	ack, answered := sp.DeliverInterviewAnswer("conv-resume-1", "use sqlite; keep it single-node")
	if !answered {
		t.Fatal("first answer not accepted; interview waiter missing")
	}
	if ack == "" {
		t.Error("ack text empty")
	}
	if _, again := sp.DeliverInterviewAnswer("conv-resume-1", "second message"); again {
		t.Error("second answer accepted after the interview was resolved; duplicate-answer protection broken")
	}

	// The planner must resume past the interview: no registry is wired,
	// so decomposition degrades to fallback steps and the task leaves
	// StatePlanning (executing). Wait bounded — the resume runs async.
	deadline := time.Now().Add(5 * time.Second)
	for {
		got, err := store.GetByID(tsk.ID)
		if err != nil {
			t.Fatalf("get task: %v", err)
		}
		if got.State != task.StatePlanning {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("task still %s after answer delivery; planner did not resume", got.State)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// TestWatchdogExemptsWaitingInterview pins the watchdog contract: a turn
// whose task sits in a waiting-interview park state is exempt from
// staleness reaping, and the questions-sent touch is live.
func TestWatchdogExemptsWaitingInterview(t *testing.T) {
	if !exemptFromReap("awaiting_interview") {
		t.Error("awaiting_interview not exempt from reap")
	}
	if !exemptFromReap(task.StatePlanning) {
		t.Error("planning state not exempt from reap")
	}
	if exemptFromReap(task.StateExecuting) {
		t.Error("executing state must still be reapable")
	}
}
