package agent

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/session"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/pkg/models"
)

// Bughunt H3 pins: an interview waiter is parked under the canonical
// conversation id (PlanRequest.SessionID from publishPlanRequest(result,
// conversationID)), but the answer turn's req.SessionID is the primary
// session-... id on CLI/TUI sessions. The handler must canonicalize BOTH
// shapes to the same conv-... id before calling DeliverInterviewAnswer,
// whose w.sessionID == sessionID compare is verbatim.

// interviewScopeStore is a SessionStoreReader that models the CLI/TUI
// identity split: the primary id (session-...) and the canonical conversation
// id (conv-...) are DIFFERENT strings pointing at the same session.
type interviewScopeStore struct {
	primaryID      string
	conversationID string
}

func (s *interviewScopeStore) Get(id string) *session.Session {
	if id == s.primaryID {
		return &session.Session{ID: s.primaryID, ConversationID: s.conversationID}
	}
	return nil
}

func (s *interviewScopeStore) GetByConversationID(conversationID string) *session.Session {
	if conversationID == s.conversationID {
		return &session.Session{ID: s.primaryID, ConversationID: s.conversationID}
	}
	return nil
}

// TestResolveInterviewAnswerScope_BothShapes pins the canonicalizer directly:
// both the primary session id and the conv id resolve to the SAME canonical
// conversation id, matching the key the waiter is parked under.
func TestResolveInterviewAnswerScope_BothShapes(t *testing.T) {
	const primaryID = "session-cli-primary"
	const convID = "conv-cli-canonical"
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())
	h.SetSessionStore(&interviewScopeStore{primaryID: primaryID, conversationID: convID})

	for _, id := range []string{primaryID, convID} {
		got := h.resolveInterviewAnswerScope(id, convID)
		if got != convID {
			t.Errorf("resolveInterviewAnswerScope(%q, %q) = %q, want canonical %q", id, convID, got, convID)
		}
	}
}

// TestResolveInterviewAnswerScope_NoStorePassThrough pins the legacy
// no-session-store behavior: the raw session id (or, when empty, the
// conversation id) passes through unchanged.
func TestResolveInterviewAnswerScope_NoStorePassThrough(t *testing.T) {
	h := NewChatHandler(nil, nil, nil, slogDiscardLogger())
	if got := h.resolveInterviewAnswerScope("session-raw", "conv-fallback"); got != "session-raw" {
		t.Errorf("no-store scope = %q, want raw session id", got)
	}
	if got := h.resolveInterviewAnswerScope("", "conv-fallback"); got != "conv-fallback" {
		t.Errorf("no-store empty-session scope = %q, want conversation id", got)
	}
}

// newInterviewScopeHandler builds a ChatHandler wired to a real bus, a
// session store with distinct primary/conversation ids, and a StrategicPlanner
// whose answer seam is live. The bus lets us observe the chat.response the
// answer path emits (its presence proves the waiter was consumed and the
// handler took the interview_answer branch).
func newInterviewScopeHandler(t *testing.T, primaryID, convID string) (*ChatHandler, *StrategicPlanner, *bus.MessageBus, *task.Store) {
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
	msgBus := bus.New(nil, slogDiscardLogger())
	h := NewChatHandler(nil, nil, msgBus, slogDiscardLogger())
	h.SetSessionStore(&interviewScopeStore{primaryID: primaryID, conversationID: convID})
	h.SetPlannerInterview(sp)
	return h, sp, msgBus, store
}

// parkInterview parks a waiter for taskID under parkKey (the canonical
// conversation id at park time, matching the production publishPlanRequest
// path) without invoking a resume that needs a model.
func parkInterview(t *testing.T, sp *StrategicPlanner, store *task.Store, taskID, parkKey string) {
	t.Helper()
	tsk := newTestTask("interview scope task", "design and implement the archive service")
	tsk.ID = taskID
	tsk.SetState(task.StatePlanning)
	if err := store.Create(tsk); err != nil {
		t.Fatalf("create task: %v", err)
	}
	req := PlanRequest{TaskID: taskID, SessionID: parkKey}
	pctx := &plan.PlanningContext{InterviewQuestions: []string{"which backend?"}}
	if err := sp.awaitInterviewAnswers(t.Context(), tsk, req, pctx); err != nil {
		t.Fatalf("awaitInterviewAnswers: %v", err)
	}
}

// deliverAnswerThroughHandler drives one answer turn through the REAL
// handleRequest seam: it returns the chat.response ack, or fails if the
// handler fell through to normal dispatch (which, with no loop/dispatcher
// wired, produces an error response instead of the resume ack).
func deliverAnswerThroughHandler(t *testing.T, h *ChatHandler, msgBus *bus.MessageBus, conversationID, sessionID, answer string) ChatResponse {
	t.Helper()
	sub := msgBus.SubscribeWithBuffer("interview-answer-test", "chat.response", 16)
	defer msgBus.Unsubscribe(sub)

	req := ChatRequest{
		Message:        answer,
		ConversationID: conversationID,
		SessionID:      sessionID,
		TurnID:         "turn-interview-answer",
	}
	payload, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	h.handleRequest(t.Context(), &models.BusMessage{ID: "msg-interview-answer", Payload: payload})

	deadline := time.Now().Add(2 * time.Second)
	for {
		select {
		case msg := <-sub.Channel:
			var resp ChatResponse
			if err := json.Unmarshal(msg.Payload, &resp); err != nil {
				t.Fatalf("unmarshal response: %v", err)
			}
			if strings.Contains(resp.Reply, "resuming the plan") {
				return resp
			}
			t.Fatalf("answer did not resume the interview; got reply %q", resp.Reply)
		case <-time.After(deadline.Sub(time.Now())):
			t.Fatal("no chat.response from the answer turn")
		}
	}
}

// TestInterviewAnswerResumes_CLI_ParkConvLookupPrimary pins the exact H3
// scenario: park under the canonical conv id, then answer from a CLI/TUI
// turn carrying the primary session id. Before the fix, DeliverInterviewAnswer
// compared conv-... against session-... and never consumed the waiter.
func TestInterviewAnswerResumes_CLI_ParkConvLookupPrimary(t *testing.T) {
	const primaryID = "session-cli-primary"
	const convID = "conv-cli-canonical"
	const taskID = "task-interview-cli"
	h, sp, msgBus, store := newInterviewScopeHandler(t, primaryID, convID)
	parkInterview(t, sp, store, taskID, convID)

	deliverAnswerThroughHandler(t, h, msgBus, convID, primaryID, "use sqlite; keep it single-node")

	if sp.interviewWaitIsLive(taskID) {
		t.Error("interview waiter still live after a primary-id answer; scope was not canonicalized")
	}
}

// TestInterviewAnswerResumes_Reverse_ParkConvLookupConv pins the mirror
// answer shape: the GUI sends the canonical conv id AS its session id, so the
// answer turn's req.SessionID is already conv-.... It must still resume the
// conv-parked waiter byte-identically. Together with the CLI test above, both
// answer-id shapes (primary and canonical) converge on the park key.
//
// Note: the park key is ALWAYS the canonical conv id in production
// (publishPlanRequest(result, conversationID) -> PlanRequest.SessionID), so a
// "park under primary-id" variant is not a production shape. The canonicalizer
// unit pin (TestResolveInterviewAnswerScope_BothShapes) covers both id shapes
// feeding the answer boundary directly.
func TestInterviewAnswerResumes_Reverse_ParkConvLookupConv(t *testing.T) {
	const primaryID = "session-cli-primary"
	const convID = "conv-cli-canonical"
	const taskID = "task-interview-gui-shape"
	h, sp, msgBus, store := newInterviewScopeHandler(t, primaryID, convID)
	parkInterview(t, sp, store, taskID, convID)

	// GUI shape: the conv id is submitted in BOTH request identity fields.
	deliverAnswerThroughHandler(t, h, msgBus, convID, convID, "use sqlite; keep it single-node")

	if sp.interviewWaitIsLive(taskID) {
		t.Error("interview waiter still live after a conv-id answer; GUI shape regressed")
	}
}
