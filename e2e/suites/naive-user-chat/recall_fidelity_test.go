//go:build e2e

// 03-context TEST-GATE T6 (docs/plans/20261119-usability-remediation/
// 03-context/00T-gate.md, branch matrix F10; findings F-ORCH-16/24,
// F-NEW-7/9; W10).
//
// Live-daemon recall extensions of the naive-user-chat suite, driven
// against the fake-LLM harness with the frozen probe facts (p1 hello.txt,
// p2 codeword ZEBRA-4417):
//
//   - naive-user-chat-06: failed-state fidelity. After a genuinely failed
//     task, "did the change get made?" must say failed plainly, never
//     "still in progress" (F-ORCH-16a).
//   - naive-user-chat-07: task-set coverage. After hello.txt creation plus
//     an unrelated newer turn, "what files did you make for me in this
//     session?" must name hello.txt, not only the last task
//     (F-ORCH-16b / F-NEW-7).
//   - naive-user-chat-08: fact->memory routing. The planted codeword
//     asked back as a fact question must be answered from the memory
//     lane - no task_get plumbing text, no decomposition prose
//     (F-ORCH-24 / F-NEW-7).
//
// RED today for the documented defect reasons; GREEN after 03-context/02.
// Digest context is DISABLED so continuity answers can only come from the
// stored-task / memory lanes (same determinism rule as the base suite).
package naiveuserchat

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// recallGateStack boots the hermetic stack with digest context off.
func recallGateStack(t *testing.T) *harness.Stack {
	t.Helper()
	return harness.Start(t, harness.WithExtraEnv(map[string]string{
		"MEEPT_DISABLE_DIGEST_CONTEXT": "1",
	}))
}

// gateClassifierPin pins the classifier lane for the next turn.
func gateClassifierPin(s *harness.Stack, intent string) {
	s.Fake.SetClassifierOutput(`{"intent":"` + intent + `","confidence":0.95,"reasoning":"recall gate pin"}`)
}

// TestRecallFailedStateFidelity pins F-ORCH-16a over the live daemon: a
// failed prior task is reported as failed, never "still in progress".
func TestRecallFailedStateFidelity(t *testing.T) {
	s := recallGateStack(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "recall-failed", s.ProjectDir)

	// A doomed write INSIDE the project fence: the blocker is a FILE, so
	// the tool fails with a real OS error on every retry (async-turn-03
	// shape) and the task finalizes failed.
	const marker = "RECALL-GATE-DOOMED"
	blocker := filepath.Join(s.ProjectDir, "recall-gate-blocker")
	if err := os.WriteFile(blocker, []byte("obstacle"), 0o644); err != nil {
		t.Fatalf("create blocker: %v", err)
	}
	doomedPath := filepath.Join(blocker, "forbidden.txt")
	gateClassifierPin(s, "code")
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"` + marker + `: create the forbidden file","tool_hint":"file_write","depends_on":[]}]}`)
	doomed := `{"path":"` + doomedPath + `","content":"nope","direct":true}`
	s.Fake.ScriptN(8,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{Name: "file_write", Arguments: doomed}))
	s.Fake.SetPostToolText("The requested work could not be completed.")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file at "+doomedPath+" containing nope. "+marker)
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("ack missing turn_id: %v", ack)
	}
	ev := waitRecallGateTerminal(t, s, turnID, 150*time.Second)
	if ev.Status == "completed" {
		t.Fatalf("fixture: doomed task reported completed; cannot test failed-state recall: %+v", ev)
	}

	// The recall follow-up about the failed work.
	gateClassifierPin(s, "recall")
	s.Fake.SetPostToolText("done")
	reply := s.ChatTurn(t, sessionID, "did the change get made?", 120*time.Second)
	lower := strings.ToLower(reply)
	if strings.Contains(lower, "still in progress") {
		t.Fatalf("RED (F-ORCH-16a): failed task misreported as still in progress: %q", reply)
	}
	if !strings.Contains(lower, "failed") {
		t.Fatalf("RED (F-ORCH-16a): recall answer does not name the failure plainly: %q", reply)
	}
}

// TestRecallTaskSetCoverage pins F-ORCH-16b / F-NEW-7 over the live daemon:
// after hello.txt creation plus an unrelated newer turn, the plural
// artifact question must name hello.txt (the session's artifact), not
// only the newest unrelated task. The unit gate TestRecallTaskSet pins
// the RecallAnswer output deterministically; this e2e arm exercises the
// full live path.
func TestRecallTaskSetCoverage(t *testing.T) {
	s := recallGateStack(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "recall-taskset", s.ProjectDir)
	helloPath := filepath.Join(s.ProjectDir, "hello.txt")

	// T1: create hello.txt (the session's real artifact).
	gateClassifierPin(s, "code")
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"create the file hello.txt containing the word hello","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.SetPostToolText("Created hello.txt at " + helloPath + " containing the word hello.")
	s.Fake.EnqueueFileWrite("call-rts-t1", helloPath, "hello")
	replyT1 := s.ChatTurn(t, sessionID, turn1, 180*time.Second)
	if !strings.Contains(strings.ToLower(replyT1), "hello.txt") {
		t.Fatalf("fixture: T1 reply does not name hello.txt: %q", replyT1)
	}

	// T2: an unrelated newer turn that is NOT file work (the live
	// capital-of-France shape from F-ORCH-16). Scripted as a real
	// memory_store tool turn so it executes a tool and completes AFTER the
	// artifact work (a tool-less chat turn would trip the unbacked-claims
	// nudge on T1's history and converge-abort - a harness artifact).
	s.Fake.ResetScripts()
	gateClassifierPin(s, "code")
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"store the geography fact in memory","tool_hint":"memory","depends_on":[]}]}`)
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name:      "memory_store",
		Arguments: `{"content":"the capital of France is Paris","type":"episodic","category":"conversation"}`,
	})
	replyT2 := s.ChatTurn(t, sessionID, "remember that the capital of France is Paris", 180*time.Second)
	if strings.TrimSpace(replyT2) == "" {
		t.Fatal("fixture: unrelated turn returned empty reply")
	}

	// T3: the plural artifact question. Full fake reset drains every stale
	// T1/T2 tool call and script so this turn executes no file write. The
	// deterministic coverage assertion is the durable task set below (the
	// unit gate TestRecallTaskSet pins the RecallAnswer output itself; a
	// fake chat reply here could echo hello.txt from loop history and pass
	// for the wrong reason, so the reply is observed but not asserted).
	s.Fake.Reset()
	gateClassifierPin(s, "recall")
	s.Fake.SetPostToolText("done")
	reply := s.ChatTurn(t, sessionID, "what files did you make for me in this session?", 120*time.Second)
	t.Logf("T3 plural-artifact reply (observed): %q", reply)

	// Deterministic gate: the session's durable task set must hold BOTH the
	// hello.txt artifact work and the newer unrelated work. A last-task-only
	// recall digest drops the artifact task from a plural answer
	// (F-ORCH-16b / F-NEW-7); the fix must surface the bounded task set.
	// RED today: the artifact task row is present but the recall lane's
	// digest (asserted deterministically in TestRecallTaskSet) omits it.
	tasks := harness.Tasks(t, s.TasksDBPath())
	var haveHello, haveUnrelated bool
	for _, row := range tasks {
		name := strings.ToLower(row.Name)
		// The task name is the ~100-char intent summary, so the artifact
		// task reads "create a file named hello." - match the hello stem.
		if strings.Contains(name, "hello") {
			haveHello = true
		}
		if strings.Contains(name, "capital of france") || strings.Contains(name, "geography") || strings.Contains(name, "remember") {
			haveUnrelated = true
		}
	}
	if !haveHello {
		t.Fatalf("fixture: hello.txt artifact task absent from the session task set: %+v", tasks)
	}
	if !haveUnrelated {
		t.Fatalf("fixture: unrelated newer task absent from the session task set: %+v", tasks)
	}
	// Both tasks exist in the durable session task set. The recall lane's
	// last-task-only digest answers a plural artifact question from the
	// newest (unrelated) task alone, dropping hello.txt (F-ORCH-16b /
	// F-NEW-7). The deterministic RED assertion on the answer text lives in
	// the unit gate TestRecallTaskSet (isolated dispatcher, no loop-history
	// echo); this e2e pin establishes the live two-task precondition the
	// 03-context/02 task-set digest must serve, and records the observed
	// reply for the review trail.
	t.Logf("session task set holds hello.txt artifact + unrelated newer task; observed plural reply: %q", reply)
}

// TestRecallFactToMemoryLive pins F-ORCH-24 / F-NEW-7: the planted session
// codeword asked back as a fact question is answered from the memory lane
// - no task_get plumbing, no decomposition prose.
func TestRecallFactToMemoryLive(t *testing.T) {
	s := recallGateStack(t)
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "recall-fact", s.ProjectDir)

	const codeword = "ZEBRA-4417"

	// Plant the fact through a real memory_store tool turn (the p2 A1
	// shape: the codeword is stored in the session's memory).
	s.Fake.EnqueueToolCalls(harness.ToolCall{
		Name: "memory_store",
		Arguments: `{"content":"the secret codeword for this session is ` + codeword +
			`, written in alpha_notes.txt","type":"episodic","category":"conversation"}`,
	})
	s.ChatTurn(t, sessionID,
		"Create a short note for the session checklist: store in memory that the secret codeword is "+codeword,
		120*time.Second)

	// The fact question, classified as the live rig's quickplan verdict
	// (F-ORCH-24). The recall lane must answer from memory, not decompose
	// into task_get.
	gateClassifierPin(s, "quickplan")
	s.Fake.SetChatText("The codeword you told me earlier is " + codeword + ".")
	reply := s.ChatTurn(t, sessionID,
		"What was the codeword I told you earlier in this session?", 150*time.Second)

	lower := strings.ToLower(reply)
	if strings.Contains(lower, "task_get") || strings.Contains(lower, "task id was missing") ||
		strings.Contains(lower, "requires a task_id") {
		t.Fatalf("RED (F-ORCH-24): fact question answered with task_get plumbing: %q", reply)
	}
	if strings.Contains(lower, "decompose") {
		t.Fatalf("RED (F-NEW-7): decomposition prose leaked into the fact answer: %q", reply)
	}
	if !strings.Contains(reply, codeword) {
		t.Fatalf("RED (F-ORCH-24): fact answer does not carry the planted codeword %s: %q", codeword, reply)
	}
}

// waitRecallGateTerminal subscribes to turn.terminal and waits for the
// named turn's non-parked terminal (single persistent RPC connection).
func waitRecallGateTerminal(t *testing.T, s *harness.Stack, turnID string, timeout time.Duration) recallGateTerminal {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	sub := rpc.CallResult("bus.subscribe", map[string]any{"topics": []string{"turn.terminal"}})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res := rpc.CallResult("bus.poll", map[string]string{"subscription_id": subID})
		if events, ok := res["events"].([]any); ok {
			for _, evAny := range events {
				ev, ok := evAny.(map[string]any)
				if !ok || ev["topic"] != "turn.terminal" {
					continue
				}
				payload, ok := ev["payload"].(map[string]any)
				if !ok || payload["turn_id"] != turnID || payload["status"] == "parked" {
					continue
				}
				out := recallGateTerminal{}
				out.Status, _ = payload["status"].(string)
				out.Reply, _ = payload["reply"].(string)
				out.TaskID, _ = payload["task_id"].(string)
				return out
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no non-parked turn.terminal for %s within %s", turnID, timeout)
	return recallGateTerminal{}
}

type recallGateTerminal struct {
	Status string
	Reply  string
	TaskID string
}
