package tui

// 05-navigation TEST-GATE T6 (leaf 00T-gate.md, matrix F12c; leaf
// 03-restore-queue-visibility T1; F-ORCH-7).
//
// Pins the TUI half of the aligned restore policy:
//   - auto_resume=true: relaunch restores the most-recent session
//     (session.get_most_recent), never a silent fresh session.
//   - auto_resume=false: explicit empty start - the most-recent RPC is
//     never consulted and a new session is created.
//   - Pending-turn awareness is rebuilt from non-terminal task state:
//     a stale executing/planning task on the restored session surfaces
//     an "interrupted" marker instead of vanishing (03-context/01 T4
//     resume contract consumer).
//
// RED today: the first two arms PASS (the TUI already honors
// auto_resume - the divergence is the GUI half, pinned Dart-side); the
// interrupted-awareness arm fails: no client surface marks stale
// non-terminal tasks on restore.
//
// NOTE (deviation): the leaf names internal/tui/models/restore_test.go,
// but its verify command targets ./internal/tui/ and the pin needs the
// App-level loadSession seam + the headless harness, both package tui.
// The gate reuses the call-recording stub from command_handler_plan_test.go.

import (
	"testing"

	"github.com/caimlas/meept/internal/sharedclient"
	"github.com/caimlas/meept/internal/tui/models"
	"github.com/caimlas/meept/internal/tui/types"
)

// gateRestoreStub seeds the RPC answers a restore needs.
func gateRestoreStub(t *testing.T) *gateCallStub {
	t.Helper()
	stub := newGateCallStub(t)
	stub.setResult("session.get_most_recent", map[string]any{
		"id":              "sess-last",
		"name":            "last real session",
		"conversation_id": "conv-last",
		"project_path":    "/test/project",
	})
	stub.setResult("session.list", map[string]any{"sessions": []any{
		map[string]any{"id": "sess-last", "name": "last real session", "project_path": "/test/project"},
	}})
	stub.setResult("session.create", map[string]any{
		"id":              "sess-fresh",
		"name":            "default",
		"conversation_id": "conv-fresh",
	})
	stub.setResult("task.list_extended", map[string]any{"tasks": []any{
		map[string]any{
			"id":    "task-stale",
			"name":  "quartz stale work",
			"state": "executing",
		},
	}})
	return stub
}

// gateRestoreApp builds a bare App wired to the stub (no event loop;
// loadSessionImpl is driven directly - deterministic, no sleeps).
func gateRestoreApp(t *testing.T, stub *gateCallStub) *App {
	t.Helper()
	app := createTestApp()
	client := NewRPCClient(stub.socketPath)
	if err := client.Connect(); err != nil {
		t.Fatalf("connect: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	app.rpc = client
	app.sessionMgr = sharedclient.NewSessionManager(client, app.clientConfig.Session.DefaultName)
	return app
}

// TestRestorePolicyAligned pins both auto_resume arms plus the
// interrupted-awareness rebuild on the restored session.
func TestRestorePolicyAligned(t *testing.T) {
	t.Run("auto_resume_true_restores_most_recent", func(t *testing.T) {
		stub := gateRestoreStub(t)
		app := gateRestoreApp(t, stub)
		app.clientConfig.Session.AutoResume = true

		msg, ok := app.loadSessionImpl().(SessionLoadedMsg)
		if !ok {
			t.Fatal("loadSessionImpl returned no SessionLoadedMsg")
		}
		if msg.Err != nil {
			t.Fatalf("restore error: %v", msg.Err)
		}
		if msg.Session == nil || msg.Session.ID != "sess-last" {
			t.Fatalf("auto_resume=true did not restore the most-recent session: %+v", msg)
		}
		if msg.IsNew {
			t.Error("restored session reported as new (must be a resume)")
		}
	})

	t.Run("auto_resume_false_explicit_empty", func(t *testing.T) {
		stub := gateRestoreStub(t)
		app := gateRestoreApp(t, stub)
		app.clientConfig.Session.AutoResume = false

		msg, ok := app.loadSessionImpl().(SessionLoadedMsg)
		if !ok {
			t.Fatal("loadSessionImpl returned no SessionLoadedMsg")
		}
		if gateStubCalled(stub, "session.get_most_recent") {
			t.Error("auto_resume=false still consulted session.get_most_recent (policy must be explicit empty)")
		}
		if msg.Session != nil && msg.Session.ID == "sess-last" {
			t.Error("auto_resume=false resumed the last session anyway")
		}
		if !msg.IsNew && msg.Err == nil {
			t.Errorf("auto_resume=false neither created a fresh session nor errored honestly: %+v", msg)
		}
	})

	t.Run("stale_nonterminal_task_interrupted_awareness", func(t *testing.T) {
		stub := gateRestoreStub(t)
		app := gateRestoreApp(t, stub)
		app.clientConfig.Session.AutoResume = true

		msg, ok := app.loadSessionImpl().(SessionLoadedMsg)
		if !ok || msg.Session == nil {
			t.Fatal("restore precondition broken")
		}
		// Bind the chat view to the restored session (the App's
		// SessionLoadedMsg handler does this in production).
		app.chat.SetSession(msg.Session)

		// The restored session carries a stale non-terminal task (the
		// daemon restarted mid-work). Feed the tasks fetch result the
		// same way the event loop does.
		app.tasks.Update(models.TasksUpdateMsg{Tasks: []types.TaskExtended{{
			Task: types.Task{ID: "task-stale", Name: "quartz stale work", State: "executing"},
		}}})

		// Pending awareness must be rebuilt: the user sees an
		// interrupted marker for the stale work, never silent loss.
		found := false
		for _, m := range app.chat.GetMessages() {
			if m.Role == "system" && containsFold(m.Content, "interrupted") {
				found = true
			}
		}
		if !found {
			t.Error("restored session with a stale non-terminal task shows no 'interrupted' awareness marker (pending state silently lost; F-ORCH-7 / 03-context T4 resume contract)")
		}
	})
}

// containsFold is a tiny ASCII case-insensitive contains (the gate's
// fixture text is ASCII-only).
func containsFold(s, sub string) bool {
	if len(sub) == 0 {
		return true
	}
	for i := 0; i+len(sub) <= len(s); i++ {
		match := true
		for j := 0; j < len(sub); j++ {
			a, b := s[i+j], sub[j]
			if a >= 'A' && a <= 'Z' {
				a += 'a' - 'A'
			}
			if b >= 'B' && b <= 'Z' {
				b += 'a' - 'A'
			}
			if a != b {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
