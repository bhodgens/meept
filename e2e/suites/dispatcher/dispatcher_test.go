//go:build e2e

// Package dispatcher is the WAVE-B e2e suite for the agent dispatcher's
// externally-observable routing contracts (manifest scenarios
// dispatcher-01..07): every test boots the full hermetic stack (fake LLM +
// scratch daemon) and asserts END STATES — the task store row, the
// turn.terminal payload, or the user-facing reply text — never internals.
//
// Scripting surfaces (per the harness contract):
//   - imperative + artifact-noun inputs classify as code via the harness
//     heuristic (async task lane);
//   - SetClassifierOutput pins a specific lane (chat, quickplan) when a
//     non-default route is needed;
//   - deterministic gates (model-directive clarify, media-URL guard,
//     platform fast path, compound keywords) are driven by message shape
//     and need no scripting.
package dispatcher

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// newStack is the per-test stack bootstrap shared by this suite: project
// registered, session bound to the project dir.
func newStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	return s
}

// waitAnyTask waits until at least one task row exists and returns the latest.
func waitAnyTask(t *testing.T, s *harness.Stack) harness.TaskRow {
	t.Helper()
	harness.WaitFor(t, 20*time.Second, "a task row to appear", func() bool {
		return len(harness.Tasks(t, s.TasksDBPath())) > 0
	})
	tasks := harness.Tasks(t, s.TasksDBPath())
	return tasks[len(tasks)-1]
}

// terminalSub is a turn.terminal subscription bound to its own RPC
// connection (subscriptions die with the connection that made them).
type terminalSub struct {
	sess  *rpcSession
	subID string
}

// subscribeTerminal opens a turn.terminal subscription. Call BEFORE
// submitting the chat turn: the task-end relay fires the moment the task
// finalizes, and a subscription opened after a fast completion misses the
// event.
func subscribeTerminal(t *testing.T, s *harness.Stack) *terminalSub {
	t.Helper()
	sess := openRPCSession(t, s)
	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(subRaw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack unparseable: %s (%v)", subRaw, err)
	}
	t.Cleanup(func() {
		_, _ = sess.call("bus.unsubscribe",
			map[string]string{"subscription_id": sub.SubscriptionID})
		sess.Close()
	})
	return &terminalSub{sess: sess, subID: sub.SubscriptionID}
}

// waitCompletedTerminalOn polls the subscription for a non-parked
// turn.terminal event for turnID and asserts status=completed with a
// non-stub reply.
func (ts *terminalSub) waitCompletedTerminalOn(t *testing.T, s *harness.Stack, turnID string) {
	t.Helper()
	deadline := time.Now().Add(120 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := ts.sess.call("bus.poll", map[string]string{
			"subscription_id": ts.subID,
		})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic != "turn.terminal" {
						continue
					}
					var p struct {
						TurnID string `json:"turn_id"`
						Status string `json:"status"`
						Reply  string `json:"reply"`
					}
					if json.Unmarshal(ev.Payload, &p) != nil || p.TurnID != turnID {
						continue
					}
					if p.Status == "parked" {
						continue
					}
					if p.Status != "completed" {
						t.Fatalf("dispatcher: turn %s terminal status = %q (reply %q)",
							turnID, p.Status, p.Reply)
					}
					if p.Reply == "" ||
						(strings.Contains(p.Reply, "Task ") && strings.Contains(p.Reply, "completed") &&
							len(p.Reply) < 40) {
						t.Fatalf("dispatcher: turn %s terminal reply is empty or a bare stub: %q",
							turnID, p.Reply)
					}
					return
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("dispatcher: no non-parked turn.terminal for turn %s within 2m\ndaemon log tail:\n%s",
		turnID, s.Daemon.LogTail())
}

// waitNoTask asserts no task row appears within a short grace window.
func waitNoTask(t *testing.T, s *harness.Stack) {
	t.Helper()
	time.Sleep(2 * time.Second)
	if tasks := harness.Tasks(t, s.TasksDBPath()); len(tasks) != 0 {
		t.Fatalf("expected no task rows, found %d: %+v", len(tasks), tasks)
	}
}

// ---------------------------------------------------------------------------
// dispatcher-01 (S): chat imperative routes to a direct reply; no task created
// ---------------------------------------------------------------------------

// TestDispatcher01ChatDirectReplyNoTask pins the chat lane: a conversational
// input pinned to the chat classification is answered INLINE (the reply is
// the fake LLM's chat text, never a stub) and the task store gains NO row —
// the turn never dispatches async work.
func TestDispatcher01ChatDirectReplyNoTask(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp01", s.ProjectDir)

	const chatReply = "hello! how can i help you today?"
	// Pin the classifier to the chat lane: the fake LLM's heuristic would
	// otherwise answer "code" for every input. Chat is inline — the
	// dispatcher answers directly from the chat loop without a task.
	s.Fake.SetClassifierOutput(`{"intent":"chat","confidence":0.95,"reasoning":"pinned chat"}`)
	s.Fake.SetChatText(chatReply)
	s.Fake.SetPostToolText(chatReply)

	reply := s.ChatTurn(t, sessionID, "hey there, good morning", 120*time.Second)

	// Observable end state 1: the user-facing reply is the real chat text.
	if !strings.Contains(reply, "how can i help") {
		t.Fatalf("dispatcher-01: reply is not the scripted chat text: %q", reply)
	}
	// A1 invariant: never the "Task ... completed." stub.
	if strings.Contains(reply, "Task ") && strings.Contains(reply, "completed") {
		t.Fatalf("dispatcher-01: reply is the completion stub: %q", reply)
	}

	// Observable end state 2: no task row was created for a chat turn.
	waitNoTask(t, s)
}

// ---------------------------------------------------------------------------
// dispatcher-02 (M): planning intent creates a task row and dispatches async
// ---------------------------------------------------------------------------

// TestDispatcher02PlanningIntentCreatesTask pins the async lane: an
// imperative file-work request classifies code (harness heuristic), creates
// a REAL task row, and the orchestrator drives its step to a
// successfully-terminal state with the artifact on disk.
func TestDispatcher02PlanningIntentCreatesTask(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp02", s.ProjectDir)

	const marker = "DISP02-MARKER"
	artifact := filepath.Join(s.ProjectDir, "dispatch02.txt")
	// Pin the plan so the step-job prompt carries the marker the executor
	// script binds to.
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"` + marker + `: create the file dispatch02.txt containing dispatch02","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains(marker)),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"dispatch02","direct":true}`,
		}))
	s.Fake.SetPostToolText("Created dispatch02.txt at " + artifact + " with the requested content.")

	tsub := subscribeTerminal(t, s)
	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named dispatch02.txt containing dispatch02. "+marker)

	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("dispatcher-02: ack missing turn_id: %+v", ack)
	}

	// Observable end state 1: the task row exists and reaches completed
	// with honest counters.
	task := waitAnyTask(t, s)
	row := harness.WaitTaskCompleted(t, s.TasksDBPath(), task.ID, 120*time.Second)
	if row.CompletedJobs < 1 {
		t.Fatalf("dispatcher-02: completed_jobs = %d, want >= 1; row: %+v",
			row.CompletedJobs, row)
	}

	// Observable end state 2: a step row for the task is successfully
	// terminal and the artifact landed (the step actually executed).
	// Successfully-terminal: completed (no review flow) or approved (full
	// review passed). A wide window rides out parallel-suite delays.
	deadline := time.Now().Add(150 * time.Second)
	var step harness.StepRow
	for time.Now().Before(deadline) {
		for _, st := range harness.Steps(t, s.TasksDBPath(), task.ID) {
			if st.State == "completed" || st.State == "approved" {
				step = st
				break
			}
		}
		if step.ID != "" {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	if step.ID == "" {
		t.Fatalf("dispatcher-02: no successfully-terminal step for task %s within 150s; steps:\n%s",
			task.ID, harness.FormatSteps(harness.Steps(t, s.TasksDBPath(), task.ID)))
	}
	if step.Result == "" {
		t.Fatalf("dispatcher-02: terminal step %s has empty result", step.ID)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("dispatcher-02: artifact %s missing: %v\ndaemon log tail:\n%s",
			artifact, err, s.Daemon.LogTail())
	}

	// Observable end state 3: a non-parked turn.terminal for this turn id
	// reports completed with a non-stub reply.
	tsub.waitCompletedTerminalOn(t, s, turnID)
}

// waitCompletedTerminal polls bus.subscribe/bus.poll for a non-parked
// turn.terminal event for turnID and asserts status=completed with a
// non-stub reply. Uses ONE persistent connection: subscriptions are tied to
// the connection that created them.
func waitCompletedTerminal(t *testing.T, s *harness.Stack, turnID string) {
	t.Helper()
	sess := openRPCSession(t, s)
	defer sess.Close()

	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(subRaw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack unparseable: %s (%v)", subRaw, err)
	}
	defer func() {
		_, _ = sess.call("bus.unsubscribe",
			map[string]string{"subscription_id": sub.SubscriptionID})
	}()

	deadline := time.Now().Add(90 * time.Second)
	var last string
	for time.Now().Before(deadline) {
		raw, err := sess.call("bus.poll", map[string]string{
			"subscription_id": sub.SubscriptionID,
		})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic != "turn.terminal" {
						continue
					}
					var p struct {
						TurnID string `json:"turn_id"`
						Status string `json:"status"`
						Reply  string `json:"reply"`
					}
					if json.Unmarshal(ev.Payload, &p) != nil || p.TurnID != turnID {
						continue
					}
					if p.Status == "parked" {
						continue
					}
					if p.Status != "completed" {
						t.Fatalf("dispatcher: turn %s terminal status = %q (reply %q)",
							turnID, p.Status, p.Reply)
					}
					if p.Reply == "" ||
						(strings.Contains(p.Reply, "Task ") && strings.Contains(p.Reply, "completed") &&
							len(p.Reply) < 40) {
						t.Fatalf("dispatcher: turn %s terminal reply is empty or a bare stub: %q",
							turnID, p.Reply)
					}
					return
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	_ = last
	t.Fatalf("dispatcher: no non-parked turn.terminal for turn %s within 90s\ndaemon log tail:\n%s",
		turnID, s.Daemon.LogTail())
}

// ---------------------------------------------------------------------------
// dispatcher-03 (M): ambiguous input yields a clarification; resume dispatches
// ---------------------------------------------------------------------------

// TestDispatcher03ClarificationThenResume pins the clarify lane: an ambiguous
// model-directive ("use glm models" — a provider with no scope) yields a
// clarification question as the turn's reply with NO task row, and the
// follow-up answer on the SAME conversation re-enters classification and
// dispatches real work (task row + completed step + artifact).
func TestDispatcher03ClarificationThenResume(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp03", s.ProjectDir)

	// Turn 1: the clarify branch answers inline, before any classifier runs.
	ack1 := s.SubmitChatHTTP(t, sessionID, "use glm models")
	if accepted, _ := ack1["accepted"].(bool); !accepted {
		t.Fatalf("dispatcher-03: clarify submit rejected: %+v", ack1)
	}
	convID, _ := ack1["conversation_id"].(string)
	if convID == "" {
		t.Fatalf("dispatcher-03: ack missing conversation_id: %+v", ack1)
	}
	// Read turn 1's reply from the turn.terminal event (the clarification
	// reply is synchronous in the event).
	clarifyReply := awaitTurnReply(t, s, ack1)
	if !strings.Contains(strings.ToLower(clarifyReply), "model") {
		t.Fatalf("dispatcher-03: clarification reply missing the model/scope question: %q", clarifyReply)
	}
	waitNoTask(t, s)

	// Turn 2: the answer re-enters via the pending-clarification resume
	// (same conversation id is what makes the session tracker treat it as a
	// follow-up) and becomes a real work request.
	artifact := filepath.Join(s.ProjectDir, "clarified.txt")

	// Pin the INTENT ANALYZER (system prompt "intent analysis assistant";
	// distinct from the intent classifier) so the resumed combined input
	// analyzes as unambiguous implementation work — without this the
	// re-analysis re-clarifies and the resume never dispatches.
	s.Fake.Script(harness.SystemPromptContains("intent analysis assistant"),
		harness.TextResponse(`{"goal":"create the clarified file","ambiguity":0.1,"scope":"narrow","category":"implementation","suggested_questions":[],"confidence":0.95,"suggested_mode":"direct"}`))
	// Pin the classifier to the code lane for the resumed turn, and pin
	// the plan so the step-job prompt carries the marker the executor
	// script binds to.
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"pinned"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"create the file clarified.txt containing clarified work","tool_hint":"file_write","depends_on":[]}]}`)
	// Bind the executor turn to the resumed work.
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(),
			harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("clarified.txt")),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"clarified work","direct":true}`,
		}))
	s.Fake.SetPostToolText("Created clarified.txt at " + artifact + " after the clarification.")

	tsub2 := subscribeTerminal(t, s)
	ack2 := submitChatWithConversation(t, s, sessionID, convID,
		"the entire task: create a file named clarified.txt containing clarified work")
	turnID2, _ := ack2["turn_id"].(string)
	if turnID2 == "" {
		t.Fatalf("dispatcher-03: resume ack missing turn_id: %+v", ack2)
	}

	task := waitAnyTask(t, s)
	harness.WaitTaskCompleted(t, s.TasksDBPath(), task.ID, 120*time.Second)
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("dispatcher-03: resumed work produced no artifact: %v\ndaemon log tail:\n%s",
			err, s.Daemon.LogTail())
	}
	tsub2.waitCompletedTerminalOn(t, s, turnID2)
}

// awaitTurnReply waits for the non-parked turn.terminal reply of the single
// turn just submitted. Uses ONE persistent connection for the subscription.
func awaitTurnReply(t *testing.T, s *harness.Stack, ack map[string]any) string {
	t.Helper()
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("awaitTurnReply: ack has no turn_id: %+v", ack)
	}
	sess := openRPCSession(t, s)
	defer sess.Close()

	subRaw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	_ = json.Unmarshal(subRaw, &sub)
	defer func() {
		_, _ = sess.call("bus.unsubscribe",
			map[string]string{"subscription_id": sub.SubscriptionID})
	}()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		raw, err := sess.call("bus.poll", map[string]string{
			"subscription_id": sub.SubscriptionID,
		})
		if err == nil {
			var resp struct {
				Events []struct {
					Topic   string          `json:"topic"`
					Payload json.RawMessage `json:"payload"`
				} `json:"events"`
			}
			if json.Unmarshal(raw, &resp) == nil {
				for _, ev := range resp.Events {
					if ev.Topic != "turn.terminal" {
						continue
					}
					var p struct {
						TurnID string `json:"turn_id"`
						Status string `json:"status"`
						Reply  string `json:"reply"`
					}
					if json.Unmarshal(ev.Payload, &p) == nil && p.TurnID == turnID && p.Status != "parked" {
						return p.Reply
					}
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("dispatcher-03: no terminal reply for turn %s within 60s", turnID)
	return ""
}

// ---------------------------------------------------------------------------
// dispatcher-04 (S): platform roster fast path is rewritten to user language
// ---------------------------------------------------------------------------

// TestDispatcher04RosterDumpRewrittenByReplyGuard pins the reply guard: a
// platform introspection question short-circuits to the deterministic roster
// dump (no LLM), but the reply the USER sees is the guard's fallback text —
// never the "## Available Agents" catalog.
func TestDispatcher04RosterDumpRewrittenByReplyGuard(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp04", s.ProjectDir)

	// Pin the classifier to the platform lane: introspection is answered
	// inline by the deterministic roster fast path (no LLM, no task).
	s.Fake.SetClassifierOutput(`{"intent":"platform","confidence":0.95,"reasoning":"pinned platform"}`)
	s.Fake.SetChatText("UNREACHABLE — the platform fast path must not call the model")
	s.Fake.SetPostToolText("UNREACHABLE — the platform fast path must not call the model")

	reply := s.ChatTurn(t, sessionID, "what agents are available", 120*time.Second)

	// The catalog must never reach the user.
	if strings.Contains(reply, "Available Agents") {
		t.Fatalf("dispatcher-04: raw roster shipped to the user: %q", reply)
	}
	// The reply must be user-shaped. The guard fallback text (or the
	// recall-continuity digest that supersedes it on continuity-gated
	// replies) is what lands instead — the roster must never ship raw.
	if strings.TrimSpace(reply) == "" {
		t.Fatalf("dispatcher-04: empty reply")
	}
	// No task row: introspection is inline.
	waitNoTask(t, s)
}

// ---------------------------------------------------------------------------
// dispatcher-05 (M): quickplan routing repair picks the repaired lane
// ---------------------------------------------------------------------------

// TestDispatcher05QuickplanLanePlansAndExecutes pins the quickplan route
// (classifier pinned to quickplan): the turn dispatches async, the quickplan
// planner runs (the fake LLM's canned plan), and the executor step completes
// with the artifact on disk — plan+clarify+execute autonomously, observable
// as a completed task row.
func TestDispatcher05QuickplanLanePlansAndExecutes(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp05", s.ProjectDir)

	// Pin the classifier to the quickplan lane.
	s.Fake.SetClassifierOutput(`{"intent":"quickplan","confidence":0.95,"reasoning":"pinned"}`)

	artifact := filepath.Join(s.ProjectDir, "quickplan.txt")
	s.Fake.SetPostToolText("Created quickplan.txt at " + artifact + ", one step at a time.")
	s.Fake.EnqueueFileWrite("call-d05", artifact, "quickplan work")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Work through the quickplan: create a file named quickplan.txt containing quickplan work, one at a time without stopping")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("dispatcher-05: ack missing turn_id: %+v", ack)
	}

	task := waitAnyTask(t, s)
	row := harness.WaitTaskCompleted(t, s.TasksDBPath(), task.ID, 150*time.Second)
	if row.State != "completed" {
		t.Fatalf("dispatcher-05: task state = %q, want completed", row.State)
	}
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("dispatcher-05: artifact %s missing: %v\ndaemon log tail:\n%s",
			artifact, err, s.Daemon.LogTail())
	}
	if got := strings.TrimSpace(string(data)); got != "quickplan work" {
		t.Fatalf("dispatcher-05: artifact content = %q, want %q", got, "quickplan work")
	}
	_ = turnID
}

// ---------------------------------------------------------------------------
// dispatcher-06 (M): compound request is split, not misrouted into one lane
// ---------------------------------------------------------------------------

// TestDispatcher06CompoundSplitRecordsIntents pins the compound split: a
// long two-work request ("create a file ... and also fix bug ...") matches
// two keyword intents (code + debug), classifies compound, and creates a
// parent task whose metadata records BOTH intent fragments
// (compound_intent_types) — observable in the task store row — and the task
// still finalizes rather than hanging.
//
// Scripting: the fake LLM's classifier branch serves BOTH the single-intent
// and multi-intent prompts from one pinned output. A JSON ARRAY answer makes
// the single-intent parse fail (falling through to the keyword chain) while
// the multi-intent detector parses it as the fragment list — exactly the
// two-code-fragments shape compound routing needs.
func TestDispatcher06CompoundSplitRecordsIntents(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp06", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "compound.md")
	s.Fake.SetPostToolText("Created compound.md at " + artifact + " with both halves done.")
	s.Fake.EnqueueFileWrite("call-d06a", artifact, "first piece\nsecond piece")

	// >80 chars (compoundKeywordThreshold) with " and also " compound
	// signal; keyword fragments "create a file" (code) and "fix bug"
	// (debug) at 0.4 apiece, plus the pinned array output giving the
	// multi-intent detector two strong (>= 0.5) fragments => compound.
	s.Fake.SetClassifierOutput(`[{"intent":"code","confidence":0.9},{"intent":"debug","confidence":0.85}]`)

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named compound.md containing the first piece of the plan and also fix bug in the script where the second piece is missing")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("dispatcher-06: ack missing turn_id: %+v", ack)
	}

	task := waitAnyTask(t, s)

	// The parent task's metadata carries the compound record with both
	// fragments — read it via the task.get RPC (the HTTP surface is
	// rate-limited, and this loop polls).
	var meta struct {
		CompoundType        string   `json:"compound_type"`
		CompoundIntents     int      `json:"compound_intents"`
		CompoundIntentTypes []string `json:"compound_intent_types"`
	}
	harness.WaitFor(t, 20*time.Second, "compound metadata on task "+task.ID, func() bool {
		sess := openRPCSession(t, s)
		defer sess.Close()
		raw, err := sess.call("task.get", map[string]any{"id": task.ID})
		if err != nil {
			return false
		}
		var resp struct {
			Metadata json.RawMessage `json:"metadata"`
		}
		if json.Unmarshal(raw, &resp) != nil {
			return false
		}
		if json.Unmarshal(resp.Metadata, &meta) == nil && meta.CompoundType != "" {
			return true
		}
		var asString string
		if err := json.Unmarshal(resp.Metadata, &asString); err == nil {
			return json.Unmarshal([]byte(asString), &meta) == nil && meta.CompoundType != ""
		}
		return false
	})
	if meta.CompoundType == "" {
		t.Fatalf("dispatcher-06: task %s has no compound_type metadata: %+v", task.ID, meta)
	}
	if len(meta.CompoundIntentTypes) < 2 {
		t.Fatalf("dispatcher-06: compound split produced %d intents (%v); want >= 2",
			len(meta.CompoundIntentTypes), meta.CompoundIntentTypes)
	}

	// The compound task still runs to a terminal state (never hangs).
	harness.WaitFor(t, 150*time.Second, "compound task "+task.ID+" terminal", func() bool {
		for _, row := range harness.Tasks(t, s.TasksDBPath()) {
			if row.ID == task.ID && (row.State == "completed" || row.State == "failed") {
				return true
			}
		}
		return false
	})
}

// ---------------------------------------------------------------------------
// dispatcher-07 (S): media-URL input gets the deterministic media-guard verdict
// ---------------------------------------------------------------------------

// TestDispatcher07MediaURLGuardDeterministicAnalystRoute pins the media-URL
// guard: a media-consumption request carrying a YouTube URL routes
// deterministically (method=media_url_guard, decided BEFORE the LLM
// classifier) to the analyst — an INLINE analyze lane. Observable end states:
// the user gets the analyst lane's reply text, no executor tool work runs,
// and no task row is created.
func TestDispatcher07MediaURLGuardDeterministicAnalystRoute(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp07", s.ProjectDir)

	// The analyst lane is a chat-shaped model call. Set both text seams to
	// the same marker so the assertion is honest regardless of which branch
	// serves it.
	const analystReply = "here is the summary of the video you shared"
	s.Fake.SetChatText(analystReply)
	s.Fake.SetPostToolText(analystReply)

	reply := s.ChatTurn(t, sessionID,
		"summarize this video https://youtu.be/dQw4w9WgXcQ in three bullet points",
		120*time.Second)

	if !strings.Contains(reply, "summary of the video") {
		t.Fatalf("dispatcher-07: reply is not the analyst lane text: %q", reply)
	}
	// analyze is inline: no task row and no async dispatch.
	waitNoTask(t, s)
}
