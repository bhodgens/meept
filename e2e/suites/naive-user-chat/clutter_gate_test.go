//go:build e2e

// 04-clutter TEST-GATE T8 (F11a empty-reply + F11b trivial lane): naive-user
// e2e extensions. Manifest scenarios naive-user-chat-09..10.
//
//	naive-user-chat-09  tool-only turn completes with a NON-EMPTY reply
//	                    naming the written path (F-ORCH-15)
//	naive-user-chat-10  trivial question takes the direct chat lane: no
//	                    task row, one brief reply (F-ORCH-5)
//
// Driven against the fake-LLM harness. Both are RED today for a missing-
// implementation reason and flip GREEN after 04-clutter/01 (09) and
// 04-clutter/02 (10).
package naiveuserchat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// clutterTerminal is the turn.terminal payload subset these gates read.
type clutterTerminal struct {
	TurnID      string `json:"turn_id"`
	TaskID      string `json:"task_id"`
	HandlerCase string `json:"handler_case"`
	Status      string `json:"status"`
	Reply       string `json:"reply"`
	Error       string `json:"error"`
}

// waitClutterTerminal subscribes BEFORE submit and polls for the turn's
// non-parked terminal on ONE persistent connection.
func waitClutterTerminal(t *testing.T, s *harness.Stack, turnID string, timeout time.Duration) clutterTerminal {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	sub := rpc.CallResult("bus.subscribe", map[string]any{"topics": []string{"turn.terminal"}})
	subID, _ := sub["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %v", sub)
	}
	t.Cleanup(func() {
		rpc.CallResult("bus.unsubscribe", map[string]string{"subscription_id": subID})
	})
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		res := rpc.CallResult("bus.poll", map[string]string{"subscription_id": subID})
		if events, ok := res["events"].([]any); ok {
			for _, evAny := range events {
				ev, ok := evAny.(map[string]any)
				if !ok || ev["topic"] != "turn.terminal" {
					continue
				}
				raw, _ := json.Marshal(ev["payload"])
				var term clutterTerminal
				if json.Unmarshal(raw, &term) != nil || term.TurnID != turnID || term.Status == "parked" {
					continue
				}
				return term
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no non-parked turn.terminal for %s within %s", turnID, timeout)
	return clutterTerminal{}
}

// ---------------------------------------------------------------------------
// naive-user-chat-09 (M): tool-only turn never ships an empty reply
// ---------------------------------------------------------------------------

// TestNaiveUserChat09ToolOnlyReplyNonEmpty drives a tooluse turn whose
// executor calls file_write and then emits NO final text (blank stop) - the
// frozen p1 shape (F-ORCH-15: "has_report=false", reply=""). The completed
// terminal must carry a non-empty reply naming the written path, synthesized
// from the tool ledger. RED today: reply="" ships verbatim.
func TestNaiveUserChat09ToolOnlyReplyNonEmpty(t *testing.T) {
	s := harness.Start(t, harness.WithExtraEnv(map[string]string{
		"MEEPT_DISABLE_DIGEST_CONTEXT": "1",
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-nuc09", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "hello.txt")
	// Pin the tooluse lane (the live p1 classification: route_to_agent).
	classifierPin(s, "tooluse")
	s.Fake.EnqueueFileWrite("call-nuc09", artifact, "hello world")
	// The p1 empty-reply mechanism: the model's post-tool completion is a
	// REPORT-ONLY JSON block with no user-facing prose. RouteToAgent's
	// StripReport removes the report and ships the remaining "" - the
	// frozen "has_report=false / reply=''" shape. (A literally blank
	// completion would trip the client's ErrEmptyResponse hardening, a
	// different defense; the report-only form is non-empty to the client
	// but strips to an empty user reply.)
	s.Fake.SetPostToolText("```json\n{\"status\":\"completed\",\"accomplished\":[\"Created hello.txt with hello world\"],\"not_done\":[],\"issues\":[],\"observations\":[]}\n```")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named hello.txt in the project directory with the text 'hello world'. Tell me the full path of the file you created.")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("naive-user-chat-09: ack missing turn_id: %+v", ack)
	}

	term := waitClutterTerminal(t, s, turnID, 180*time.Second)
	if term.Status != "completed" {
		t.Fatalf("naive-user-chat-09: status = %q (error %q), want completed (the work ran)", term.Status, term.Error)
	}
	// The artifact must exist: synthesis has real ledger evidence.
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("naive-user-chat-09: file_write did not run (rig precondition): %v", err)
	}
	// RED today: the completed terminal ships reply="" (frozen p1 shape).
	if strings.TrimSpace(term.Reply) == "" {
		t.Fatalf("naive-user-chat-09: F-ORCH-15 completed turn shipped an empty reply; "+
			"the tool ledger recorded a write of %s - synthesize from it", artifact)
	}
	if !strings.Contains(term.Reply, "hello.txt") {
		t.Errorf("naive-user-chat-09: synthesized reply does not name hello.txt: %q", term.Reply)
	}
}

// ---------------------------------------------------------------------------
// naive-user-chat-10 (M): trivial question takes the direct chat lane
// ---------------------------------------------------------------------------

// TestNaiveUserChat10TrivialFastLane drives the frozen 2+2 fixture. With the
// trivial fast lane (orchestrator.trivial_fast_lane, default on) the turn
// must answer directly: NO task row, one brief reply, no task machinery.
// RED today: the fixture over-classifies to quickplan/code and creates a
// task (F-ORCH-5).
func TestNaiveUserChat10TrivialFastLane(t *testing.T) {
	s := harness.Start(t, harness.WithExtraEnv(map[string]string{
		"MEEPT_DISABLE_DIGEST_CONTEXT": "1",
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "e2e-nuc10", s.ProjectDir)

	// The live over-classification: the trivial question scored quickplan.
	// The fast lane must pre-empt it BEFORE the classifier chain.
	classifierPin(s, "quickplan")
	s.Fake.SetChatText("4")

	ack := s.SubmitChatHTTP(t, sessionID, "What is 2+2? Answer briefly.")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("naive-user-chat-10: ack missing turn_id: %+v", ack)
	}

	term := waitClutterTerminal(t, s, turnID, 120*time.Second)
	if term.Status != "completed" {
		t.Fatalf("naive-user-chat-10: status = %q (error %q), want completed", term.Status, term.Error)
	}
	// RED today: a task row is created for the trivial question.
	if term.TaskID != "" {
		t.Errorf("naive-user-chat-10: F-ORCH-5 trivial question dispatched task %s - the fast lane creates no task", term.TaskID)
	}
	if tasks := harness.Tasks(t, s.TasksDBPath()); len(tasks) != 0 {
		t.Errorf("naive-user-chat-10: trivial question left %d task rows, want 0", len(tasks))
	}
	// One brief reply, not a job-log dump.
	if strings.TrimSpace(term.Reply) == "" {
		t.Fatalf("naive-user-chat-10: trivial question shipped an empty reply")
	}
	if strings.Contains(term.Reply, "## task completed") || strings.Contains(term.Reply, "job-") {
		t.Errorf("naive-user-chat-10: trivial reply is a task/job-log dump, want one brief answer: %q", term.Reply)
	}
	if len(term.Reply) > 200 {
		t.Errorf("naive-user-chat-10: trivial reply is not brief (%d chars): %q", len(term.Reply), term.Reply)
	}
}
