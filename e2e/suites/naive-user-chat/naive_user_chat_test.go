//go:build e2e

// Package naiveuserchat is the hermetic port of the live-model
// naive-user-chat scenario (scripts/e2e-naive-user-chat.sh): the same
// four-turn naive-user transcript —
//
//	T1 create a file named hello.txt …, then tell me the full path
//	T2 make it beep when it opens              (modify turn)
//	T3 did the change get made? where is the file?   (status turn)
//	T4 what files did you make for me?         (artifact turn)
//
// — driven against the fake-LLM harness, with the A0-A6 reply-shape and
// continuity assertions ported to Go test code:
//
//	A0  reply is non-empty
//	A1  reply is not the "Task ... completed." stub            (F1/C1)
//	A2  reply contains no "## Available Agents" roster dump    (F5/C5)
//	A3  reply is not a raw JSON object dump                    (F5/C5)
//	A4  hello.txt lands in the PROJECT dir (not the daemon cwd) and
//	    the T1 reply names hello.txt                           (F3/C3, leaf-03)
//	A5  the T3 status reply references hello.txt and is not a bare
//	    clarification request (session continuity)
//	A6  clean lifecycle: the scratch daemon terminates on test teardown
//	    (the harness registers the SIGTERM stop in t.Cleanup)
//
// Determinism notes (what the fake-LLM port simplifies vs the live tier):
//
//   - The intent classifier is PINNED per turn (T1 code, T3/T4 recall),
//     because a scripted fake would otherwise answer the classifier with
//     a canned lane while the real routing value under test is the
//     deterministic dispatcher arbitration AFTER the LLM verdict —
//     exactly the guards this suite exercises (platform_recall_arbitration,
//     platform_action_arbitration, chat_imperative_arbitration).
//   - Digest context is DISABLED (MEEPT_DISABLE_DIGEST_CONTEXT=1): the
//     T3 prompt then contains NO artifact info, so the A5 continuity
//     answer can only come from the stored-task RecallAnswer path —
//     deterministic, no prompt-content coupling.
//   - The T2 modify turn is scripted as a second file_write (append) to
//     hello.txt — a genuine tool execution — with chat text that keeps
//     the reply non-empty for its A0-A3 shape checks.
//
// Coverage map (manifest scenarios):
//
//	naive-user-chat-01  T1 create + reply shape + artifact        — TestNaiveUserChatScenario
//	naive-user-chat-02  T3 status turn continuity (A5)            — TestNaiveUserChatScenario
//	naive-user-chat-03  T4 artifact recall + reply shape          — TestNaiveUserChatScenario
//	naive-user-chat-04  A6 lifecycle teardown                     — TestNaiveUserChatLifecycle
package naiveuserchat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// The four transcript turns, verbatim from the live-model script.
const (
	turn1 = "create a file named hello.txt in the current directory containing the word hello, then tell me the full path"
	turn2 = "make it beep when it opens"
	turn3 = "did the change get made? where is the file?"
	turn4 = "what files did you make for me?"
)

// assertReplyShape ports assert_reply_shape: A0 (non-empty), A1 (not the
// "Task ... completed." stub), A2 (no agent-roster dump), A3 (not a raw
// JSON object dump). The live script's F6 quota-honesty sub-check is
// dropped: the fake LLM cannot produce provider quota failures here.
func assertReplyShape(t *testing.T, turn, reply string) {
	t.Helper()

	// A0: non-empty.
	if strings.TrimSpace(reply) == "" {
		t.Fatalf("A0/%s: reply is empty or whitespace-only", turn)
	}

	// A1: not the literal "Task ... completed." stub (F1/C1).
	trimmed := strings.TrimSpace(reply)
	if strings.HasPrefix(trimmed, "Task ") && strings.HasSuffix(trimmed, "completed.") {
		t.Fatalf("A1/%s: reply is the 'Task ... completed.' stub (F1/C1): %q", turn, reply)
	}

	// A2: no agent-roster dump (F5/C5).
	if strings.Contains(reply, "## Available Agents") {
		t.Fatalf("A2/%s: reply contains the agent-roster header (F5/C5): %q", turn, reply)
	}

	// A3: not a raw JSON object dump (F5/C5) — first/last non-space char
	// must not be the brace pair of a serialized envelope.
	compact := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '\t', '\n', '\r':
			return -1
		}
		return r
	}, reply)
	if len(compact) >= 2 && compact[0] == '{' && compact[len(compact)-1] == '}' {
		t.Fatalf("A3/%s: reply is a raw JSON object dump (F5/C5): %.200s", turn, reply)
	}
}

// classifierPin pins the intent-classifier lane for one turn. The fake LLM's
// classifier output is a sticky global, so the pin is re-armed before every
// turn (turn-split scripting, same pattern the tools-memory suite uses).
func classifierPin(s *harness.Stack, intent string) {
	s.Fake.SetClassifierOutput(`{"intent":"` + intent + `","confidence":0.95,"reasoning":"naive-user-chat e2e pin"}`)
}

// TestNaiveUserChatScenario drives the full four-turn transcript and ports
// the A0-A5 assertions (manifest scenarios naive-user-chat-01..03).
func TestNaiveUserChatScenario(t *testing.T) {
	// Digest context OFF: the T3 prompt must not already contain the
	// artifact info, or the A5 continuity assertion would pass for the
	// wrong reason (the answer could come from prompt injection instead
	// of the stored-task continuity path).
	s := harness.Start(t, harness.WithExtraEnv(map[string]string{
		"MEEPT_DISABLE_DIGEST_CONTEXT": "1",
	}))
	s.RegisterProject(t, "e2e-project")
	// Session bound to the project dir; the daemon cwd is deliberately
	// different (harness Start uses the work root), so A4 genuinely
	// exercises session working-dir resolution, never the daemon-cwd
	// fallback (leaf-03).
	sessionID := s.CreateSession(t, "e2e-naive", s.ProjectDir)

	helloPath := filepath.Join(s.ProjectDir, "hello.txt")

	// --- T1: create hello.txt, tell me the full path ------------------
	classifierPin(s, "code")
	// Pin the plan so the step description names the artifact: the
	// task-completion relay's step previews (formatTaskCompletedMessage)
	// quote step results, and the executor's post-tool text flows into
	// them — so the reply names hello.txt exactly as the A4 assertion
	// (live-script "T1 reply names hello.txt") requires.
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"create the file hello.txt containing the word hello","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.SetPostToolText("Created hello.txt at " + helloPath + " containing the word hello.")
	s.Fake.EnqueueFileWrite("call-nuc-t1", helloPath, "hello")

	replyT1 := s.ChatTurn(t, sessionID, turn1, 180*time.Second)
	assertReplyShape(t, "t1", replyT1)

	// A4: artifact landed in the PROJECT dir (not the daemon cwd) with
	// the scripted content.
	data, err := os.ReadFile(helloPath)
	if err != nil {
		t.Fatalf("A4: %s was not created (F3/C3): %v\nreply: %s\ndaemon log tail:\n%s",
			helloPath, err, replyT1, s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != "hello" {
		t.Fatalf("A4: hello.txt content = %q, want %q", got, "hello")
	}
	if _, err := os.Stat(filepath.Join(s.Work, "hello.txt")); err == nil {
		t.Fatalf("A4: hello.txt landed in the DAEMON cwd (%s) not the session project dir (F3/C3, leaf-03 regression)",
			s.Work)
	}
	// A4 (reply half): the T1 reply names the file.
	if !strings.Contains(strings.ToLower(replyT1), "hello.txt") {
		t.Fatalf("A4: T1 reply does not name hello.txt: %q", replyT1)
	}

	// --- T2: modify turn (make it beep when it opens) ------------------
	// Scripted as a second genuine tool execution (append a line to
	// hello.txt) — the artifact stays "hello\nbeep" and the post-tool
	// text keeps the reply shape-checkable.
	classifierPin(s, "code")
	s.Fake.SetPostToolText("Added the beep line to hello.txt at " + helloPath + ".")
	s.Fake.EnqueueFileWrite("call-nuc-t2", helloPath, "hello\nbeep")

	replyT2 := s.ChatTurn(t, sessionID, turn2, 180*time.Second)
	assertReplyShape(t, "t2", replyT2)

	// --- T3: status turn (did the change get made? where is the file?) -
	classifierPin(s, "recall")
	s.Fake.SetPostToolText("done")
	replyT3 := s.ChatTurn(t, sessionID, turn3, 180*time.Second)
	assertReplyShape(t, "t3", replyT3)

	// A5: continuity — the status reply references hello.txt (the exact
	// path spelling appears in the stored result) and is not a bare
	// clarification request.
	if strings.Contains(replyT3, "which file") && !strings.Contains(replyT3, "hello.txt") {
		t.Fatalf("A5: T3 is a bare clarification request — continuity gap: %q", replyT3)
	}
	if !strings.Contains(strings.ToLower(replyT3), "hello.txt") {
		t.Fatalf("A5: T3 reply does not reference hello.txt — continuity gap: %q", replyT3)
	}

	// --- T4: artifact turn (what files did you make for me?) -----------
	classifierPin(s, "recall")
	s.Fake.SetPostToolText("done")
	replyT4 := s.ChatTurn(t, sessionID, turn4, 180*time.Second)
	assertReplyShape(t, "t4", replyT4)

	// The task store recorded the T1 work with a successfully-terminal
	// step (the create turn actually executed).
	tasks := harness.Tasks(t, s.TasksDBPath())
	completed := 0
	for _, task := range tasks {
		for _, step := range harness.Steps(t, s.TasksDBPath(), task.ID) {
			if step.State == "completed" || step.State == "approved" {
				completed++
			}
		}
	}
	if completed == 0 {
		var dump string
		for _, task := range tasks {
			dump += harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), task.ID))
		}
		t.Fatalf("no successfully-terminal step across %d tasks; tasks: %+v\nsteps:\n%s", len(tasks), tasks, dump)
	}
}

// TestNaiveUserChatLifecycle ports A6: a dedicated stack is torn down via
// t.Cleanup (SIGTERM, then SIGKILL — Daemon.stop), and the test asserts the
// daemon actually terminated. Manifest scenario naive-user-chat-04.
func TestNaiveUserChatLifecycle(t *testing.T) {
	s := harness.Start(t, harness.WithExtraEnv(map[string]string{
		"MEEPT_DISABLE_DIGEST_CONTEXT": "1",
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-naive-lifecycle", s.ProjectDir)

	pid := s.Daemon.Pid()
	if pid == 0 {
		t.Fatal("A6: daemon pid not recorded after boot")
	}

	// One turn so the teardown tears down a daemon that did real work.
	classifierPin(s, "code")
	s.Fake.SetPostToolText("Created hello.txt at " + filepath.Join(s.ProjectDir, "hello.txt") + ".")
	s.Fake.EnqueueFileWrite("call-nuc-lc", filepath.Join(s.ProjectDir, "hello.txt"), "hello")
	s.ChatTurn(t, sessionID, turn1, 180*time.Second)

	// Trigger the harness teardown path now (idempotent: t.Cleanup's stop
	// is a sync.Once) and verify the daemon is gone.
	s.Daemon.Stop()

	harness.WaitFor(t, 15*time.Second, "daemon process termination (A6)", func() bool {
		return s.Daemon.Stopped()
	})
}
