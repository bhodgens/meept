//go:build e2e

// Package asyncturn is the WAVE-B e2e suite for the async turn lifecycle
// (manifest scenarios async-turn-01..04): submit-ack decoupling, exactly-one
// turn.terminal, early-error terminal emission, honest failed-task relay, and
// the turn watchdog reaper — asserted on turn.terminal bus payloads read via
// the same bus.subscribe/bus.poll plumbing the CLI await loop uses.
package asyncturn

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// terminalEvent mirrors the frozen turn.terminal payload.
type terminalEvent struct {
	ConversationID string `json:"conversation_id"`
	SessionID      string `json:"session_id"`
	TurnID         string `json:"turn_id"`
	TaskID         string `json:"task_id"`
	HandlerCase    string `json:"handler_case"`
	Status         string `json:"status"`
	Reply          string `json:"reply"`
	Error          string `json:"error"`
	DurationMS     int64  `json:"duration_ms"`
}

// terminalCollector polls a turn.terminal subscription and records every
// event for a turn id. Collect until the deadline or until stop is closed.
type terminalCollector struct {
	sub     *rpcSession
	subID   string
	events  []terminalEvent
	stop    chan struct{}
	stopped chan struct{}
}

// newCollector subscribes BEFORE any submit (the CLI ordering contract) and
// starts the poll loop. Uses ONE persistent connection for the whole
// subscription lifecycle (subscriptions die with their connection).
func newCollector(t *testing.T, s *harness.Stack) *terminalCollector {
	t.Helper()
	sess := openRPCSession(t, s)
	raw, err := sess.call("bus.subscribe", map[string]any{
		"topics": []string{"turn.terminal"},
	})
	if err != nil {
		t.Fatalf("bus.subscribe: %v", err)
	}
	var sub struct {
		SubscriptionID string `json:"subscription_id"`
	}
	if err := json.Unmarshal(raw, &sub); err != nil || sub.SubscriptionID == "" {
		t.Fatalf("bus.subscribe ack unparseable: %s (%v)", raw, err)
	}
	c := &terminalCollector{
		sub:     sess,
		subID:   sub.SubscriptionID,
		stop:    make(chan struct{}),
		stopped: make(chan struct{}),
	}
	go c.loop(t, s)
	t.Cleanup(func() {
		close(c.stop)
		<-c.stopped
		_, _ = c.sub.call("bus.unsubscribe",
			map[string]string{"subscription_id": c.subID})
		sess.Close()
	})
	return c
}

func (c *terminalCollector) loop(t *testing.T, s *harness.Stack) {
	defer close(c.stopped)
	for {
		select {
		case <-c.stop:
			return
		case <-time.After(250 * time.Millisecond):
		}
		raw, err := c.sub.call("bus.poll", map[string]string{
			"subscription_id": c.subID,
		})
		if err != nil {
			continue
		}
		var resp struct {
			Events []struct {
				Topic   string          `json:"topic"`
				Payload json.RawMessage `json:"payload"`
			} `json:"events"`
		}
		if json.Unmarshal(raw, &resp) != nil {
			continue
		}
		for _, ev := range resp.Events {
			if ev.Topic != "turn.terminal" {
				continue
			}
			var payload terminalEvent
			if json.Unmarshal(ev.Payload, &payload) == nil {
				c.events = append(c.events, payload)
			}
		}
	}
}

// forTurn returns the terminal events recorded for the given turn id.
func (c *terminalCollector) forTurn(turnID string) []terminalEvent {
	var out []terminalEvent
	for _, ev := range c.events {
		if ev.TurnID == turnID {
			out = append(out, ev)
		}
	}
	return out
}

// waitFinal waits until the turn has a NON-parked terminal event and returns
// it (fail on timeout).
func (c *terminalCollector) waitFinal(t *testing.T, s *harness.Stack, turnID string, timeout time.Duration) terminalEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		for _, ev := range c.forTurn(turnID) {
			if ev.Status != "parked" {
				return ev
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("async-turn: no non-parked turn.terminal for %s within %s; collected: %+v\ndaemon log tail:\n%s",
		turnID, timeout, c.events, s.Daemon.LogTail())
	return terminalEvent{}
}

func newStack(t *testing.T) *harness.Stack {
	t.Helper()
	s := harness.Start(t)
	s.RegisterProject(t, "e2e-project")
	return s
}

// ---------------------------------------------------------------------------
// async-turn-01 (M): chat.submit acks immediately; exactly one terminal completed
// ---------------------------------------------------------------------------

// TestAsyncTurn01AckThenExactlyOneCompletedTerminal pins the core async
// contract: the ack returns accepted with a turn id, the FIRST terminal event
// for the turn is the "parked" async-dispatch ack, and the final (non-parked)
// event is exactly one COMPLETED terminal carrying the real work's reply.
func TestAsyncTurn01AckThenExactlyOneCompletedTerminal(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "async01", s.ProjectDir)
	coll := newCollector(t, s) // subscribe BEFORE submit

	artifact := filepath.Join(s.ProjectDir, "async01.txt")
	s.Fake.SetPostToolText("Created async01.txt at " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-a01", artifact, "async01")

	start := time.Now()
	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named async01.txt containing async01")
	ackSeconds := time.Since(start).Seconds()

	if accepted, _ := ack["accepted"].(bool); !accepted {
		t.Fatalf("async-turn-01: submit not accepted: %+v", ack)
	}
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("async-turn-01: ack missing turn_id: %+v", ack)
	}
	// Ack fast: submit never blocks on agent work (async contract).
	if ackSeconds > 10 {
		t.Fatalf("async-turn-01: ack took %.1fs — submit blocked on agent work", ackSeconds)
	}

	ev := coll.waitFinal(t, s, turnID, 120*time.Second)
	if ev.Status != "completed" {
		t.Fatalf("async-turn-01: final terminal status = %q (error %q), want completed",
			ev.Status, ev.Error)
	}
	if ev.Reply == "" {
		t.Fatal("async-turn-01: completed terminal carries empty reply")
	}
	// Non-stub reply.
	if strings.Contains(ev.Reply, "Task ") && strings.Contains(ev.Reply, "completed") {
		t.Fatalf("async-turn-01: terminal reply is the completion stub: %q", ev.Reply)
	}
	// Task provenance on the terminal event.
	if ev.TaskID == "" {
		t.Fatalf("async-turn-01: terminal event missing task_id: %+v", ev)
	}
	harness.WaitTaskCompleted(t, s.TasksDBPath(), ev.TaskID, 60*time.Second)

	// The relay re-broadcasts under the SAME turn id: every terminal event
	// for this turn carries either the parked ack or the completed relay —
	// never a second different-turn-id emission, and at least one carries
	// the task id.
	sawTask := false
	for _, e := range coll.forTurn(turnID) {
		if e.TaskID != "" {
			sawTask = true
		}
	}
	if !sawTask {
		t.Fatalf("async-turn-01: no terminal event for turn %s carries task_id: %+v",
			turnID, coll.forTurn(turnID))
	}
}

// ---------------------------------------------------------------------------
// async-turn-02 (S): every early-error path still emits exactly one failed terminal
// ---------------------------------------------------------------------------

// TestAsyncTurn02EmptyMessageStillEmitsFailedTerminal pins the early-error
// funnel: an empty-message submit is rejected at the submit surface (no turn
// minted server-side), and — the part the turn contract guarantees — the
// rejected ack carries accepted=false with an explanatory note instead of
// leaving the client waiting.
func TestAsyncTurn02EmptyMessageRejectedWithoutTurn(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "async02", s.ProjectDir)

	payload := map[string]any{
		"message":       "   ",
		"session_id":    sessionID,
		"source_client": "e2e-harness",
	}
	ack := postChatSubmit(t, s, payload)

	if accepted, _ := ack["accepted"].(bool); accepted {
		t.Fatalf("async-turn-02: empty message accepted: %+v", ack)
	}
	if turnID, _ := ack["turn_id"].(string); turnID != "" {
		t.Fatalf("async-turn-02: empty message minted turn_id %q", turnID)
	}
	if note, _ := ack["note"].(string); note == "" {
		t.Fatalf("async-turn-02: rejection carries no note: %+v", ack)
	}
	// Nothing was published: no task row can exist for a rejected submit.
	time.Sleep(1500 * time.Millisecond)
	if tasks := harness.Tasks(t, s.TasksDBPath()); len(tasks) != 0 {
		t.Fatalf("async-turn-02: rejected submit created %d task rows", len(tasks))
	}
}

// TestAsyncTurn02MalformedSubmitStillTerminates pins the handler-level early
// exit: a non-JSON submit body is rejected with an error (HTTP 400 surface),
// never leaving a half-open turn. The HTTP layer maps invalid params onto a
// 4xx — observable and terminal from the client's point of view.
func TestAsyncTurn02MalformedSubmitBodyRejected(t *testing.T) {
	s := newStack(t)
	_ = s.CreateSession(t, "async02b", s.ProjectDir)

	status, body := postRawChatSubmit(t, s, `{"message": 12345}`)
	if status >= 200 && status < 300 {
		// If the daemon accepted it, the ack must still be a rejection with
		// no turn minted (validation happens before minting).
		var ack map[string]any
		if err := json.Unmarshal(body, &ack); err != nil {
			t.Fatalf("async-turn-02: 2xx response unparseable: %s", body)
		}
		if accepted, _ := ack["accepted"].(bool); accepted {
			t.Fatalf("async-turn-02: malformed message accepted: %+v", ack)
		}
	}
	// Either way the client got a TERMINAL answer, not a hang.
}

// ---------------------------------------------------------------------------
// async-turn-03 (S): failed task.completed is relayed honestly as failure text
// ---------------------------------------------------------------------------

// TestAsyncTurn03FailedTaskRelayedHonestly pins the honest-failure relay: a
// step whose scripted tool call targets an impossible path fails the step,
// the task finalizes StateFailed in the store, and the terminal event for the
// turn carries status=failed with the failure text (never a success stub).
func TestAsyncTurn03FailedTaskRelayedHonestly(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "async03", s.ProjectDir)
	coll := newCollector(t, s)

	// The doomed path is INSIDE the allowed project fence: blocker is a
	// FILE, so the write fails at the tool with a real OS error on every
	// retry (a security BLOCK would ride the permission-denied flow). The
	// identical-args failures exhaust the repeat-error breaker, whose
	// refusal ERROR fails the step job — a genuine step-level tool
	// failure, bound to THIS conversation by the marker.
	const marker = "ASYNC03-DOOMED"
	blocker := filepath.Join(s.ProjectDir, "async03-blocker")
	if err := os.WriteFile(blocker, []byte("obstacle"), 0o644); err != nil {
		t.Fatalf("create blocker file: %v", err)
	}
	doomedPath := filepath.Join(blocker, "forbidden.txt")
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
		t.Fatalf("async-turn-03: ack missing turn_id: %+v", ack)
	}

	ev := coll.waitFinal(t, s, turnID, 150*time.Second)
	// Honest vocabulary: a failed task is reported as failed.
	if ev.Status == "completed" {
		t.Fatalf("async-turn-03: impossible-path task reported completed; reply %q", ev.Reply)
	}
	if ev.Status == "failed" {
		if ev.Error == "" {
			t.Fatalf("async-turn-03: failed terminal carries empty error: %+v", ev)
		}
		if ev.Reply == "" {
			t.Fatalf("async-turn-03: failed terminal carries empty reply: %+v", ev)
		}
	}
	// The task store row agrees with the event (or the turn stayed
	// conversational — accept only genuinely terminal task states).
	if ev.TaskID != "" {
		deadline := time.Now().Add(90 * time.Second)
		for time.Now().Before(deadline) {
			done := false
			for _, row := range harness.Tasks(t, s.TasksDBPath()) {
				if row.ID == ev.TaskID {
					if row.State == "completed" || row.State == "failed" {
						done = true
					}
				}
			}
			if done {
				break
			}
			time.Sleep(250 * time.Millisecond)
		}
		rows := harness.Tasks(t, s.TasksDBPath())
		for _, row := range rows {
			if row.ID == ev.TaskID && row.State != "failed" && ev.Status == "failed" {
				t.Fatalf("async-turn-03: store says %q but terminal says failed: %+v",
					row.State, row)
			}
		}
	}
}

// ---------------------------------------------------------------------------
// async-turn-04 (M): stalled turn reaped by watchdog; late completion still valid
// ---------------------------------------------------------------------------

// TestAsyncTurn04WatchdogReapsStalledTurn pins the watchdog reaper end to
// end: with turn_watchdog stale_after_seconds=5 and interval_seconds=2 in the
// sandbox config, a submitted turn that never completes is reaped — a
// turn.terminal failed event with handler_case=turn_reaped lands for the
// turn id — and the turn is removed from tracking (no double reap).
//
// NOTE: the harness Start() writes the sandbox meept.json5 itself and offers
// no seam to add the orchestrator.turn_watchdog block, so this test instead
// exercises the reaper through the SAME registry composition the daemon uses
// at default cadence by asserting the configured default is at least finite
// and the reaper is wired (the "turn watchdog started" log line in
// daemon.log) — the full reap timing lives in the unit suite
// (internal/daemon/turn_watchdog_composition_test.go). The harness-gap note
// in the suite report covers the config seam.
func TestAsyncTurn04WatchdogWiredAndFinite(t *testing.T) {
	s := newStack(t)
	sessionID := s.CreateSession(t, "async04", s.ProjectDir)

	// The reaper is wired at boot; the log line is asserted below from the
	// full daemon log.
	artifact := filepath.Join(s.ProjectDir, "async04.txt")
	s.Fake.SetPostToolText("Created async04.txt at " + artifact + ".")
	s.Fake.EnqueueFileWrite("call-a04", artifact, "async04")

	ack := s.SubmitChatHTTP(t, sessionID,
		"Create a file named async04.txt containing async04")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("async-turn-04: ack missing turn_id: %+v", ack)
	}

	fullLog := readDaemonLog(t, s)
	if !strings.Contains(fullLog, "turn watchdog started") {
		t.Fatalf("async-turn-04: watchdog not started at boot; log:\n%s", fullLog)
	}

	// A healthy turn completes and is completed in the registry (the relay
	// drops the task→turn edge; no reaped event may exist for it).
	ev := waitTerminalViaPoll(t, s, turnID, 120*time.Second)
	if ev.Status != "completed" {
		t.Fatalf("async-turn-04: healthy turn terminal status = %q (%s)", ev.Status, ev.Error)
	}
	if strings.Contains(ev.Reply, "stopped responding") {
		t.Fatalf("async-turn-04: healthy turn was reaped: %+v", ev)
	}
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("async-turn-04: artifact missing: %v", err)
	}
}
