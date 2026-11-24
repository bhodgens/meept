//go:build e2e

// 04-clutter TEST-GATE T8 (F11a): server reply-hygiene e2e extensions.
//
// Manifest scenarios output-filters-06..08. Driven against the fake-LLM
// harness; assertions read the turn.terminal relay via bus.poll, the
// chat.response review feed via bus.poll, the raw step rows via tasks.db,
// and the daemon log. Every scenario is RED today for a missing-
// implementation reason (the gated defect) and flips GREEN after
// 04-clutter/01.
//
//	output-filters-06  relay envelope strip + display-only raw preservation
//	output-filters-07  review feedback never reaches chat.response
//	output-filters-08  unmapped side-job completion logs no ERROR
package outputfilters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// p6Envelope is the VERBATIM executor step-result envelope from the frozen
// rig probe p6-positive-control.json events[52].payload.result.response
// (F-ORCH-21). The relay must strip it to prose; the store must keep it raw.
const p6Envelope = "{\n  \"status\": \"completed\",\n  \"accomplished\": [\n    \"Created file /private/tmp/usability-rig/project/hello.txt with content 'hello world' using file_write with direct:true\",\n    \"File exists and contains 11 bytes\"\n  ],\n  \"not_done\": [],\n  \"issues\": [],\n  \"observations\": [\n    \"The file was successfully written to the project directory as required\"\n  ],\n  \"suggested_next_agent\": \"\",\n  \"user_decision_needed\": false\n}"

// rawEnvelopeKeys must never render in a user-facing relay reply.
var rawEnvelopeKeys = []string{
	`"status":`, `"accomplished":`, `"not_done":`, `"issues":`,
	`"observations":`, `"suggested_next_agent":`, `"user_decision_needed":`,
}

// relayTerminal is the frozen turn.terminal payload subset these gates read.
type relayTerminal struct {
	TurnID      string `json:"turn_id"`
	TaskID      string `json:"task_id"`
	HandlerCase string `json:"handler_case"`
	Status      string `json:"status"`
	Reply       string `json:"reply"`
	Error       string `json:"error"`
}

// busCollector holds ONE persistent RPC connection for subscribe+poll (bus
// subscriptions are tied to the connection that created them).
type busCollector struct {
	rpc   *harness.RPCClient
	subID string
}

func newBusCollector(t *testing.T, s *harness.Stack, topics ...string) *busCollector {
	t.Helper()
	rpc := harness.DialRPC(t, s.SocketPath)
	res := rpc.CallResult("bus.subscribe", map[string]any{"topics": topics})
	subID, _ := res["subscription_id"].(string)
	if subID == "" {
		t.Fatalf("bus.subscribe returned no subscription_id: %+v", res)
	}
	t.Cleanup(func() {
		rpc.CallResult("bus.unsubscribe", map[string]string{"subscription_id": subID})
	})
	return &busCollector{rpc: rpc, subID: subID}
}

// poll drains one bus.poll batch into (topic, rawPayload) pairs.
func (c *busCollector) poll(t *testing.T) []struct {
	Topic   string
	Payload json.RawMessage
} {
	t.Helper()
	res := c.rpc.CallResult("bus.poll", map[string]string{"subscription_id": c.subID})
	var out []struct {
		Topic   string
		Payload json.RawMessage
	}
	events, _ := res["events"].([]any)
	for _, e := range events {
		em, _ := e.(map[string]any)
		topic, _ := em["topic"].(string)
		raw, _ := json.Marshal(em["payload"])
		out = append(out, struct {
			Topic   string
			Payload json.RawMessage
		}{topic, raw})
	}
	return out
}

// waitTerminal polls until a non-parked turn.terminal for turnID arrives.
func (c *busCollector) waitTerminal(t *testing.T, turnID string, timeout time.Duration) relayTerminal {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ev := range c.poll(t) {
			if ev.Topic != "turn.terminal" {
				continue
			}
			var term relayTerminal
			if json.Unmarshal(ev.Payload, &term) != nil || term.TurnID != turnID {
				continue
			}
			if term.Status != "parked" {
				return term
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("no non-parked turn.terminal for %s within %s", turnID, timeout)
	return relayTerminal{}
}

// ---------------------------------------------------------------------------
// output-filters-06 (M): relay envelope strip + display-only raw preservation
// ---------------------------------------------------------------------------

// TestOutputFilters06RelayEnvelopeStripped drives a task turn whose executor
// post-tool text is the frozen p6 envelope. The turn.terminal relay reply
// must carry recovered prose (no raw JSON keys); the stored step result must
// keep the raw envelope (strip is display-only). RED today: the relay ships
// the envelope verbatim (F-ORCH-21).
func TestOutputFilters06RelayEnvelopeStripped(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "of06", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "hello.txt")
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"create hello.txt with hello world","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.EnqueueFileWrite("call-of06", artifact, "hello world")
	// The executor's final text IS the machine envelope (the live p6 shape).
	s.Fake.SetPostToolText(p6Envelope)

	collector := newBusCollector(t, s, "turn.terminal")
	ack := s.SubmitChatHTTP(t, sessionID, "Create a file named hello.txt with hello world, then tell me the full path")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("output-filters-06: ack missing turn_id: %+v", ack)
	}

	term := collector.waitTerminal(t, turnID, 180*time.Second)
	if term.Status != "completed" {
		t.Fatalf("output-filters-06: terminal status = %q (error %q), want completed", term.Status, term.Error)
	}
	if term.HandlerCase != "task_completed_relay" {
		t.Fatalf("output-filters-06: handler_case = %q, want task_completed_relay", term.HandlerCase)
	}

	// RED today: the raw envelope keys render in the relay reply.
	for _, key := range rawEnvelopeKeys {
		if strings.Contains(term.Reply, key) {
			t.Fatalf("output-filters-06: F-ORCH-21 relay reply carries raw envelope key %s:\n%s", key, term.Reply)
		}
	}
	if !strings.Contains(term.Reply, "hello.txt") {
		t.Errorf("output-filters-06: relay reply lost the artifact prose: %q", term.Reply)
	}

	// Display-only strip: the STORE keeps the raw envelope. The step
	// result row is the daemon job envelope re-serialized, so the p6
	// envelope survives JSON-escaped inside it (the string field carries
	// \"accomplished\", never raw quotes) - the escaped form is the
	// correct preservation check (an unescaped search can never match a
	// JSON string field by construction). GREEN must survive the fix (the
	// strip must not rewrite the stored result).
	var taskID string
	for _, row := range harness.Tasks(t, s.TasksDBPath()) {
		taskID = row.ID
	}
	if taskID == "" {
		t.Fatal("output-filters-06: no task row recorded")
	}
	rawPreserved := false
	for _, st := range harness.Steps(t, s.TasksDBPath(), taskID) {
		if strings.Contains(st.Result, `\"accomplished\"`) || strings.Contains(st.Result, `"accomplished"`) {
			rawPreserved = true
		}
	}
	if !rawPreserved {
		t.Errorf("output-filters-06: raw envelope not preserved in the step store - strip must be display-only")
	}
}

// ---------------------------------------------------------------------------
// output-filters-07 (M): review feedback never reaches chat.response
// ---------------------------------------------------------------------------

// TestOutputFilters07ReviewFeedbackNotChat drives a task turn with a scripted
// reviewer verdict. The task.review_completed event must keep flowing (task
// detail feed) but NO chat.response frame may carry a "## review:" reply.
// RED today: handleReviewCompleted pushes "## review:" to chat.response
// (F-ORCH-25, frozen p2 events[111]).
func TestOutputFilters07ReviewFeedbackNotChat(t *testing.T) {
	// This gate grades the REVIEW lane (scripted reviewer verdict ->
	// task.review_completed feed). The harness default now boots the
	// pre-usability-wave posture with agent.review.enabled=false
	// (46e0eead), which skips the review flow entirely, so the suite
	// declares the posture it tests - the step-pipeline precedent.
	s := harness.Start(t, harness.WithConfigOverlay(map[string]any{
		"agent.review.enabled": true,
	}))
	s.RegisterProject(t, "e2e-project")
	sessionID := s.CreateSession(t, "of07", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "of07.txt")
	const marker = "OF07-REVIEW"
	// 3-step plan: isTrivialTask auto-approves < 3 steps without a full
	// reviewer, so the real work step plus two filler steps force the
	// scripted reviewer verdict to run (the step-pipeline-02 precedent).
	s.Fake.SetPlannerResponse(`{"steps":[
		{"description":"` + marker + `: create of07.txt","tool_hint":"file_write","depends_on":[]},
		{"description":"list the files in the project directory to verify the workspace","tool_hint":"file_write","depends_on":[]},
		{"description":"summarize the project directory contents in one sentence","tool_hint":"file_write","depends_on":[]}
	]}`)
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest()), harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{Name: "file_write", Arguments: `{"path":"` + artifact + `","content":"of07","direct":true}`}))
	s.Fake.SetPostToolText("The requested step work is complete.")
	// Scripted reviewer approval WITH feedback (the live p2 shape: an
	// approved verdict carrying reviewer prose pushed into chat).
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.MessageContains("REVIEW TASK STEP")),
		harness.TextResponse(`{"status":"approved","feedback":"`+marker+`: the work successfully confirmed creation of of07.txt.","confidence":0.9}`))

	collector := newBusCollector(t, s, "chat.response", "task.review_completed", "turn.terminal")
	ack := s.SubmitChatHTTP(t, sessionID, "Do the "+marker+" work: create of07.txt and finish")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("output-filters-07: ack missing turn_id: %+v", ack)
	}

	reviewSeen := false
	deadline := time.Now().Add(180 * time.Second)
	terminalDone := false
	for time.Now().Before(deadline) && !terminalDone {
		for _, ev := range collector.poll(t) {
			switch ev.Topic {
			case "chat.response":
				var resp struct {
					Reply string `json:"reply"`
				}
				if json.Unmarshal(ev.Payload, &resp) == nil && strings.Contains(resp.Reply, "## review:") {
					t.Fatalf("output-filters-07: F-ORCH-25 review feedback pushed to chat.response: %q", resp.Reply)
				}
			case "task.review_completed":
				reviewSeen = true
			case "turn.terminal":
				var term relayTerminal
				if json.Unmarshal(ev.Payload, &term) == nil && term.TurnID == turnID && term.Status != "parked" {
					terminalDone = true
				}
			}
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !terminalDone {
		t.Fatal("output-filters-07: turn never reached a terminal")
	}
	// The task-detail review feed must survive the chat reroute.
	if !reviewSeen {
		t.Errorf("output-filters-07: task.review_completed never flowed - the task-detail feed must survive")
	}
}

// ---------------------------------------------------------------------------
// output-filters-08 (S): unmapped side-job completion logs no ERROR
// ---------------------------------------------------------------------------

// TestOutputFilters08SideJobCompletionQuiet publishes a queue.job.completed
// for a job with no step mapping (the memory-capture/ambient side-job shape,
// empty step_id). The orchestrator must log it Debug-quiet, never ERROR.
// RED today: "failed to find step for job ...: step not found" at ERROR
// (F-ORCH-6).
func TestOutputFilters08SideJobCompletionQuiet(t *testing.T) {
	s := newStack(t)

	const sideJobID = "job-memory-side-e2e-0001"
	rpc := harness.DialRPC(t, s.SocketPath)
	res := rpc.CallResult("bus.publish", map[string]any{
		"topic": "queue.job.completed",
		"payload": map[string]any{
			"job_id": sideJobID,
			"result": map[string]any{"ok": true},
		},
	})
	if delivered, _ := res["delivered"].(float64); delivered < 1 {
		t.Fatalf("output-filters-08: bus.publish delivered=%v - orchestrator subscription not live: %+v", delivered, res)
	}

	// Give the orchestrator's bus goroutine time to handle the event.
	time.Sleep(1 * time.Second)

	data, err := os.ReadFile(s.Daemon.LogPath())
	if err != nil {
		t.Fatalf("output-filters-08: read daemon log: %v", err)
	}
	log := string(data)
	for _, line := range strings.Split(log, "\n") {
		if strings.Contains(line, sideJobID) && strings.Contains(line, "level=ERROR") {
			t.Fatalf("output-filters-08: F-ORCH-6 side-job completion logged at ERROR: %s", line)
		}
	}
}
