package agent

import (
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// Run 33 T4 regression: the digest must prefer the session's most recent
// COMPLETED task over a more recently updated failed task, so a follow-up
// ("what files did you make?") answers from delivered work.
func TestDigestPrefersCompletedOverFailed(t *testing.T) {
	d, _ := newDigestTestDispatcher(t)

	sessionID := "sess-digest-pick"
	// Completed work, older.
	seedDigestTask(t, d, "task-completed", "create hello.txt", sessionID,
		task.StateCompleted, time.Now().Add(-10*time.Minute), "coder")
	// Failed recall turn, more recently updated.
	seedDigestTask(t, d, "task-failed", "did the change get made?", sessionID,
		task.StateFailed, time.Now(), "chat")

	digest := d.buildSessionContextDigestExcluding(sessionID, "current-turn-id")
	if digest.LastTaskName == "" {
		t.Fatalf("digest has no task")
	}
	if digest.LastTaskName != "create hello.txt" {
		t.Fatalf("digest picked %q (state %s), want the completed 'create hello.txt'",
			digest.LastTaskName, digest.LastTaskState)
	}
	if digest.LastTaskState != string(task.StateCompleted) {
		t.Fatalf("digest state = %s, want completed", digest.LastTaskState)
	}
}

// Failures-only sessions keep the fallback: the most recently updated
// (failed) task is still the honest "most recent work" answer.
func TestDigestFallsBackWhenNothingCompleted(t *testing.T) {
	d, _ := newDigestTestDispatcher(t)

	sessionID := "sess-digest-fallback"
	seedDigestTask(t, d, "task-f1", "first turn", sessionID,
		task.StateFailed, time.Now().Add(-5*time.Minute), "chat")
	seedDigestTask(t, d, "task-f2", "second turn", sessionID,
		task.StateFailed, time.Now(), "chat")

	digest := d.buildSessionContextDigestExcluding(sessionID, "current-turn-id")
	if digest.LastTaskName != "second turn" {
		t.Fatalf("digest picked %q, want the most recent 'second turn'", digest.LastTaskName)
	}
}
