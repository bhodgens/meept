//go:build e2e

// Recall-continuity branch-selection regressions (commits d045a450,
// bb6830a7, 14dd4145, ee0274be, 9c6f46f4). A follow-up QUESTION about prior
// work ("did the change get made?", "what files did you make?") must be
// answered from the stored task result via Dispatcher.RecallAnswer — whose
// digest lookup keys on the SESSION-level conversation id (run-38 root
// cause) — and must never create a new task (shouldCreateTask gate, run
// 44). Imperative messages referencing prior work must NOT take the recall
// shortcut and must execute normally.
//
// Digest context is DISABLED (MEEPT_DISABLE_DIGEST_CONTEXT=1): the recall
// turn's prompt then carries NO artifact info, so an answer naming
// hello.txt can only come from the stored-task continuity path. A sentinel
// chat text makes any LLM-fallback answer detectable.
package continuityregressions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// chatFallbackSentinel is the scripted reply for generic (non-recall-answer)
// LLM turns: if a follow-up question reaches the LLM instead of the stored
// result, the reply betrays it.
const chatFallbackSentinel = "FALLBACK-SENTINEL-Q77"

// startRecallSandbox boots the sandbox with digest context off and seeds
// T1: a completed work turn that created hello.txt (the prior work every
// follow-up question refers to). Returns (sandbox, sessionID, helloPath).
func startRecallSandboxWithPriorWork(t *testing.T) (*sandbox, string, string) {
	t.Helper()
	s := newSandbox(t, withExtraEnv("MEEPT_DISABLE_DIGEST_CONTEXT=1"))
	s.Fake.SetChatText(chatFallbackSentinel)
	s.Fake.SetPostToolText("done")

	sessionID := s.createSession("e2e-recall", s.ProjectDir)

	// T1: create hello.txt (the completed prior task).
	helloPath := filepath.Join(s.ProjectDir, "hello.txt")
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"continuity-regressions T1 pin"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"create the file hello.txt containing the word hello","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.SetPostToolText("Created hello.txt at " + helloPath + " containing the word hello.")
	s.Fake.EnqueueFileWrite("call-cr-t1", helloPath, "hello")

	replyT1 := s.chatTurn(sessionID, "create a file named hello.txt in the current directory containing the word hello", 180*time.Second)
	// The substantive marker ONLY (L17): the forbidden "Task <id>
	// completed." stub must NOT satisfy this gate — accepting it made the
	// acceptance weaker than the sibling suite's own A1 contract, which
	// rejects the stub. The fake's post-tool text names hello.txt, so an
	// honest acknowledgment carries it.
	if !strings.Contains(strings.ToLower(replyT1), "hello.txt") {
		t.Fatalf("T1 did not acknowledge the work substantively (no hello.txt marker): %q", replyT1)
	}
	if data, err := os.ReadFile(helloPath); err != nil || strings.TrimSpace(string(data)) != "hello" {
		t.Fatalf("T1 artifact missing or wrong: %v (%q)", err, string(data))
	}

	// Wait for the T1 task row to be terminal so the digest can answer
	// from the stored result.
	var taskID string
	harnessWaitFor(t, 20*time.Second, "T1 task row", func() bool {
		for _, row := range s.taskRows() {
			taskID = row.ID
			return true
		}
		return false
	})
	s.waitTaskTerminal(taskID, 120*time.Second)
	return s, sessionID, helloPath
}

// harnessWaitFor is a local poll-until helper (avoids importing the harness
// WaitFor under a different receiver shape).
func harnessWaitFor(t *testing.T, timeout time.Duration, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("timed out after %s waiting for %s", timeout, what)
}

// TestRecallFollowUpAnsweredFromStoredResult pins the recall branch (run 38
// session-conversation-id root cause + runs 39-42 label-independence): the
// follow-up question is answered from the stored task result — the reply
// names the prior task's artifact and carries the digest's answer shape,
// never the LLM sentinel. The session-level conversation id resolution is
// proven BY the answer arriving at all: the digest lookup keys on the
// session conversation id, and a wrong-key lookup falls through to the
// sentinel-armed LLM. Manifest scenario continuity-regressions-03.
func TestRecallFollowUpAnsweredFromStoredResult(t *testing.T) {
	s, sessionID, _ := startRecallSandboxWithPriorWork(t)

	// T3: the exact run-38/42 follow-up question, classifier labeled
	// recall (the label the naive classifier gives it most runs — but the
	// branch gates on message shape, not the label).
	s.Fake.SetClassifierOutput(`{"intent":"recall","confidence":0.95,"reasoning":"continuity-regressions T3 pin"}`)
	s.Fake.SetPostToolText(chatFallbackSentinel)

	replyT3 := s.chatTurn(sessionID, "did the change get made? where is the file?", 180*time.Second)

	if strings.Contains(replyT3, chatFallbackSentinel) {
		t.Fatalf("T3 fell through to the LLM (no continuity answer): %q", replyT3)
	}
	// RecallAnswer's shape: `Prior task: "<name>" — status: <state>` plus
	// the stored best step result (which names hello.txt).
	if !strings.Contains(replyT3, "Prior task:") {
		t.Fatalf("T3 reply is not the stored-result continuity answer: %q", replyT3)
	}
	if !strings.Contains(strings.ToLower(replyT3), "hello.txt") {
		t.Fatalf("T3 continuity answer does not name the artifact: %q", replyT3)
	}
	if !strings.Contains(strings.ToLower(replyT3), "completed") {
		t.Fatalf("T3 continuity answer does not carry the task state: %q", replyT3)
	}
}

// TestRecallFollowUpLabeledWorkCreatesNoTask pins the shouldCreateTask gate
// (run 44): the same follow-up question classified as WORK must not create
// a task — before the gate, the task row was created and the async-dispatch
// branch intercepted before the recall branch could answer. The turn must
// complete without a new task row and without executing new work.
// Manifest scenario continuity-regressions-04.
func TestRecallFollowUpLabeledWorkCreatesNoTask(t *testing.T) {
	s, sessionID, _ := startRecallSandboxWithPriorWork(t)

	before := len(s.taskRows())

	// The SAME question, now labeled work (the run-44 mislabel).
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"continuity-regressions run-44 pin"}`)
	s.Fake.SetPostToolText(chatFallbackSentinel)

	reply := s.chatTurn(sessionID, "did the change get made? where is the file?", 180*time.Second)

	after := len(s.taskRows())
	if after != before {
		t.Fatalf("follow-up question created %d new task row(s) (run-44 regression); before=%d after=%d\nreply: %s",
			after-before, before, after, reply)
	}
	// No new work executed: hello.txt is untouched.
	helloPath := filepath.Join(s.ProjectDir, "hello.txt")
	data, err := os.ReadFile(helloPath)
	if err != nil || strings.TrimSpace(string(data)) != "hello" {
		t.Fatalf("prior artifact was modified by the follow-up question: %v (%q)", err, string(data))
	}
}

// TestImperativeFollowUpExecutesNotRecalls pins the gate's other half
// (runs 42/44): an IMPERATIVE message referencing prior work ("the file")
// must NOT take the recall shortcut — it must execute (the file really
// changes) and must not return the canned Prior-task answer.
// Manifest scenario continuity-regressions-05.
func TestImperativeFollowUpExecutesNotRecalls(t *testing.T) {
	s, sessionID, helloPath := startRecallSandboxWithPriorWork(t)

	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"continuity-regressions imperative pin"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"update the file hello.txt to say goodbye","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.SetPostToolText("Updated hello.txt to say goodbye.")
	s.Fake.EnqueueFileWrite("call-cr-t4", helloPath, "goodbye")

	reply := s.chatTurn(sessionID, "now update the file to say goodbye", 180*time.Second)

	if strings.Contains(reply, "Prior task:") {
		t.Fatalf("imperative follow-up took the recall shortcut: %q", reply)
	}
	data, err := os.ReadFile(helloPath)
	if err != nil || strings.TrimSpace(string(data)) != "goodbye" {
		t.Fatalf("imperative follow-up did not execute the update: %v (%q)", err, string(data))
	}
}
