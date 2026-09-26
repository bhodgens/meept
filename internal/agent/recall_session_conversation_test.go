package agent

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// Run-38 A5 root-cause pin: RecallAnswer resolves the digest through the
// SESSION-level conversation id (the key session_tasks links use). The
// production call site (RouteToAgent) must pass sessionConversationID —
// the thread-router-resolved id finds no task links and the branch
// silently falls through, losing continuity (runs 31-38).

func TestRecallAnswerAnswersFromSessionLinkedTask(t *testing.T) {
	d, _ := newDigestTestDispatcher(t)

	sessionID := "sess-conv-123"
	seedDigestTask(t, d, "task-done", "create hello.txt", sessionID,
		task.StateCompleted, time.Now(), "coder")

	res := &DispatchResult{
		AgentID: "chat",
		Intent:  &Intent{Type: string(IntentRecall), Summary: "did the change get made?"},
	}
	answer, handled := d.RecallAnswer(context.Background(), res, sessionID, false)
	if !handled || answer == "" {
		t.Fatalf("RecallAnswer not handled (handled=%v) — session-linked task not found", handled)
	}
	if !strings.Contains(answer, "create hello.txt") {
		t.Fatalf("answer does not name the task: %q", answer)
	}
	if !strings.Contains(answer, "completed") {
		t.Fatalf("answer does not carry the task state: %q", answer)
	}
}
