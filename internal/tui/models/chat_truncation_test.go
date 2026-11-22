package models

// Leaf 08 (20261121-interactive-usability) TUI truncation gate: background
// task cards render ResultSummary/result/error UNBOUNDED (the daemon caps
// summaries at ~12k via buildResultSummary). The old 45-50 char
// TruncateString cuts made completed-task answers unreadable - the whole
// point of a background task is that its answer lands in the card when the
// turn is elsewhere.

import (
	"strings"
	"testing"
)

func TestTaskCard_LongResultSummaryRendersUnbounded(t *testing.T) {
	m := newTestChatModel()
	long := strings.Repeat("answer-word ", 60) // ~720 chars, far past 50

	m.Update(ChatTaskResultMsg{
		TaskID:         "task-trunc-1",
		TaskName:       "long running job",
		State:          StateCompleted,
		CompletedSteps: 1,
		TotalSteps:     1,
		ResultSummary:  long,
	})

	found := false
	for _, msg := range m.messages {
		if strings.Contains(msg.Content, long) {
			found = true
		}
	}
	if !found {
		t.Fatalf("task.completed card truncated the %d-char ResultSummary - want the full text rendered (daemon caps at ~12k, client renders verbatim)", len(long))
	}
}

func TestTaskCard_LongJSONResultRendersUnbounded(t *testing.T) {
	m := newTestChatModel()
	long := strings.Repeat("result-body ", 60)

	reply := `{"task_completed": true, "task_id": "task-trunc-2", ` +
		`"name": "json relay job", "completed": 1, "total": 1, "result": "` +
		strings.ReplaceAll(long, `"`, `\"`) + `"}`
	m.Update(ChatResponseMsg{Reply: reply})

	found := false
	for _, msg := range m.messages {
		if strings.Contains(msg.Content, long) {
			found = true
		}
	}
	if !found {
		t.Fatalf("task.completed relay card truncated the %d-char result - want the full text rendered", len(long))
	}
}

func TestTaskCard_LongErrorRendersUnbounded(t *testing.T) {
	m := newTestChatModel()
	longErr := strings.Repeat("e", 300)

	reply := `{"task_failed": true, "task_id": "task-trunc-3", ` +
		`"name": "failing job", "completed": 0, "failed": 1, "total": 1, "error": "` +
		longErr + `"}`
	m.Update(ChatResponseMsg{Reply: reply})

	found := false
	for _, msg := range m.messages {
		if strings.Contains(msg.Content, longErr) {
			found = true
		}
	}
	if !found {
		t.Fatalf("task.failed card truncated the %d-char error text - want the full error rendered", len(longErr))
	}
}

func TestTaskCard_DetachedAckRendersUnbounded(t *testing.T) {
	m := newTestChatModel()
	longMsg := strings.Repeat("m", 200)

	reply := `{"async": true, "task_id": "task-trunc-4", "message": "` + longMsg + `"}`
	m.Update(ChatResponseMsg{Reply: reply})

	found := false
	for _, msg := range m.messages {
		if strings.Contains(msg.Content, longMsg) {
			found = true
		}
	}
	if !found {
		t.Fatalf("task detached card truncated the %d-char ack message - want the full text rendered", len(longMsg))
	}
}

func TestTaskCard_FailedStateUsesErrorRole(t *testing.T) {
	// Leaf 08 error styling: a failed task card renders error-styled
	// (RoleError), visually distinct from ordinary system notices.
	m := newTestChatModel()
	m.Update(ChatTaskResultMsg{
		TaskID:        "task-trunc-5",
		TaskName:      "failed job",
		State:         StateFailed,
		ResultSummary: "boom",
	})

	if len(m.messages) == 0 {
		t.Fatal("failed task card rendered nothing")
	}
	last := m.messages[len(m.messages)-1]
	if last.Role != RoleError {
		t.Fatalf("failed task card role = %q, want %q (error styling, leaf 08)", last.Role, RoleError)
	}
}
