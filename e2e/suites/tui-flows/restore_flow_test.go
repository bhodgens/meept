//go:build e2e

package tuiflows

// T7 restore e2e (plan 20261119-usability-remediation, leaf
// 05-navigation/03-restore-queue-visibility):
//
//	tui-restore-01  queue_status reports a parked session's non-terminal
//	              tasks (task_states {task_id,state,name}) even with no
//	              active loop, and the listing is session-scoped (B5).
//	tui-restore-02  the default restore policy relaunches the TUI onto
//	              the most-recent session (session.get_most_recent) and
//	              an explicit --session target wins over the resume.
//	tui-restore-03  a resumed session carrying a stale executing task
//	              surfaces an interrupted-awareness system message in
//	              the chat transcript (pending work is never silently
//	              lost; F-ORCH-7 / 03-context/01 T4 resume contract).

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// createParkedTask seeds one non-terminal task on a session through
// the real RPC surface (task.create -> task.link -> task.update to
// planning) so queue_status reads its own store, not a fixture.
func createParkedTask(t *testing.T, rpc *harness.RPCClient, name, sessionID string) string {
	t.Helper()
	created := rpc.CallResult("task.create", map[string]any{
		"name":        name,
		"description": "e2e tui-restore-01 parked fixture",
	})
	taskID, _ := created["id"].(string)
	if taskID == "" {
		t.Fatalf("task.create returned no id: %v", created)
	}
	rpc.CallResult("task.link", map[string]any{
		"task_id":    taskID,
		"session_id": sessionID,
	})
	upd := rpc.CallResult("task.update", map[string]any{
		"id":    taskID,
		"state": "planning",
	})
	if st, _ := upd["state"].(string); st != "planning" {
		t.Fatalf("task.update to planning failed: %v", upd)
	}
	return taskID
}

// TestQueueStatusSurfacesParkedTask covers tui-restore-01: the RPC the
// TUI/GUI queue views poll answers task_states for a session whose work
// is parked (no active agent loop) and never leaks another session's
// tasks.
func TestQueueStatusSurfacesParkedTask(t *testing.T) {
	stack := harness.Start(t)
	rpc := harness.DialRPC(t, stack.SocketPath)

	sessA := stack.CreateSession(t, "parked-session", stack.MeeptHome)
	sessB := stack.CreateSession(t, "other-session", stack.MeeptHome)
	parkedID := createParkedTask(t, rpc, "quartzpark parked work", sessA)
	createParkedTask(t, rpc, "quartzpark foreign work", sessB)

	status := rpc.CallResult("chat.queue_status", map[string]any{
		"conversation_id": sessA,
	})
	raw, present := status["task_states"]
	if !present {
		t.Fatalf("queue_status omits task_states on a live daemon (B5 zeros-only payload): %v", status)
	}
	list, ok := raw.([]any)
	if !ok || len(list) == 0 {
		t.Fatalf("task_states empty/foreign shape for the parked session: %v", status)
	}
	byID := map[string]map[string]any{}
	for _, r := range list {
		row, rmOK := r.(map[string]any)
		if !rmOK {
			t.Fatalf("task_states row is %T, want object", r)
		}
		id, _ := row["task_id"].(string)
		byID[id] = row
	}
	row, inList := byID[parkedID]
	if !inList {
		t.Fatalf("parked task %s missing from task_states %v", parkedID, list)
	}
	if row["state"] != "planning" {
		t.Errorf("parked task state = %v, want planning", row["state"])
	}
	if name, _ := row["name"].(string); !strings.Contains(name, "parked work") {
		t.Errorf("parked task name = %v, want the fixture name", row["name"])
	}

	// Session scoping: the OTHER session's parked task must never appear.
	for id := range byID {
		own := rpc.CallResult("chat.queue_status", map[string]any{
			"conversation_id": sessB,
		})
		ownList, _ := own["task_states"].([]any)
		ownHas := false
		for _, r := range ownList {
			if m, ok := r.(map[string]any); ok {
				if m["task_id"] == id {
					ownHas = true
				}
			}
		}
		if id != parkedID && !ownHas {
			t.Errorf("task_states for %s crossed sessions: %q appears for %s but not its owner (scoping broken): %v", sessA, id, sessB, list)
		}
	}
}

// writeClientJSON5 drops a client.json5 into the TUI's user config
// search path ($HOME/.meept/client.json5; the harness pins HOME to the
// sandbox). session.auto_resume defaults true, so an empty section is
// the RESTORE-ON policy; the test writes explicit values.
func writeClientJSON5(t *testing.T, stack *harness.Stack, autoResume bool) {
	t.Helper()
	body := "{\n  \"session\": { \"auto_resume\": "
	if autoResume {
		body += "true"
	} else {
		body += "false"
	}
	body += " }\n}\n"
	path := filepath.Join(stack.Home, ".meept", "client.json5")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write client.json5: %v", err)
	}
}

// TestRestoreRelaunchesMostRecentSession covers tui-restore-02: with
// auto_resume=true and no explicit target, the TUI boots onto the most
// recently active session instead of a fresh one; an explicit
// --session target overrides the resume.
func TestRestoreRelaunchesMostRecentSession(t *testing.T) {
	stack := harness.Start(t)
	rpc := harness.DialRPC(t, stack.SocketPath)

	first := stack.CreateSession(t, "restore-first", stack.MeeptHome)
	// created_at/last_activity carry RFC3339 second granularity; the
	// most-recent query must see a strict ordering, so separate the
	// two creates by more than a second.
	time.Sleep(1100 * time.Millisecond)
	last := stack.CreateSession(t, "restore-last", stack.MeeptHome)

	// Downstream seam: the daemon's own most-recent answer is the
	// newer session - the exact RPC the TUI restore consults.
	recent := rpc.CallResult("session.get_most_recent", nil)
	if id, _ := recent["id"].(string); id != last {
		t.Fatalf("session.get_most_recent = %q, want the newest session %q (%v)", id, last, recent)
	}

	// auto_resume=true: boot with NO target; the app must land on
	// session.get_most_recent's answer, not create a new session.
	writeClientJSON5(t, stack, true)
	hp := startTUIAgainst(t, stack)
	settleAsync()
	if got := hp.app.ActiveSessionID(); got != last {
		t.Fatalf("auto_resume=true booted onto %q, want the most-recent session %q", got, last)
	}
	hp.finish()

	// Explicit target wins over the resume (same policy surface, other
	// arm): boot pinned to the OLDER session.
	hp2 := startTUIAgainst(t, stack, WithTargetSession(first))
	settleAsync()
	if got := hp2.app.ActiveSessionID(); got != first {
		t.Fatalf("WithTargetSession(%q) loaded %q instead", first, got)
	}
	hp2.finish()
}

// TestRestoreMarksInterruptedWorkOnResume covers tui-restore-03: a
// resumed session carrying a stale non-terminal task surfaces an
// interrupted-awareness system message in the chat transcript on boot
// - the pending turn is never silently lost (F-ORCH-7).
func TestRestoreMarksInterruptedWorkOnResume(t *testing.T) {
	stack := harness.Start(t)
	rpc := harness.DialRPC(t, stack.SocketPath)

	sess := stack.CreateSession(t, "interrupted-session", stack.MeeptHome)
	// A stale non-terminal task: created (pending) then pushed to
	// executing - what the store holds when a daemon dies mid-work.
	taskID := createParkedTask(t, rpc, "quartzstale unfinished turn", sess)
	upd := rpc.CallResult("task.update", map[string]any{
		"id":    taskID,
		"state": "executing",
	})
	if st, _ := upd["state"].(string); st != "executing" {
		t.Fatalf("task.update to executing failed: %v", upd)
	}
	harness.WaitFor(t, 10*time.Second, "stale task visible in task.list_extended", func() bool {
		list := rpc.CallResult("task.list_extended", nil)
		tasks, _ := list["tasks"].([]any)
		for _, raw := range tasks {
			if row, ok := raw.(map[string]any); ok && row["id"] == taskID {
				return row["state"] == "executing"
			}
		}
		return false
	})

	// Resume the session with the interrupted-awareness seam exercised
	// through the real event loop: the daemon is live, so the restore
	// fetch fires on SessionLoadedMsg and the one-shot sink renders the
	// marker.
	writeClientJSON5(t, stack, true)
	hp := startTUIAgainst(t, stack, WithTargetSession(sess))
	settleAsync()
	if got := hp.app.ActiveSessionID(); got != sess {
		t.Fatalf("target session not loaded: got %q want %q", got, sess)
	}
	app := hp.finish()

	needle := "interrupted work on this session: task \"quartzstale unfinished turn\" (" + taskID + ") was still executing"
	view := strings.Join(app.ChatTranscriptExcerpt(), "\n")
	if !strings.Contains(view, needle) {
		t.Errorf("resumed session with a stale executing task shows no interrupted-awareness marker containing %q; transcript tail:\n%s",
			needle, tailForError(view, 4000))
	}
}

// tailForError bounds a transcript dump inside a failure message.
func tailForError(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return "..." + s[len(s)-n:]
}
