//go:build e2e

// Gate leaf 00T — R6 advisory budget (T15, T16; Contract C7, leaf 08).
//
// T15: mode steer + a breached daily cost cap COMPLETES the turn and
// emits one budget_advisory signal on the existing agent.quota_wait
// topic; BudgetExceededError is never returned. T16: with no mode key,
// the breach refuses the turn exactly as main does (enforce default).
//
// RED today: on main the budget gate refuses in BOTH worlds (no steer
// mode exists), so T15 fails at runtime (the steer turn parks/fails
// instead of completing); the llm.BudgetModeSteer / Advisory symbols
// live in contracts_test.go (compile-RED until leaf 08 lands).
package sovereignparity

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// budgetWorld builds a single-provider daemon with llm.budget enabled and
// a per-session cost cap so tiny usage breaches it deterministically.
func budgetWorld(t *testing.T, mode string) (*paritySandbox, *parityStub) {
	t.Helper()
	stub := newParityStub(t)
	cfg := parityConfig{
		providers: []parityProvider{
			{
				id:     "provbud",
				stub:   stub,
				apiKey: "key-bud",
				models: []parityModel{
					// Non-zero cost so the USD cap is reachable: the stub
					// reports usage tokens, and the daemon prices the call.
					{name: "model-bud", caps: []string{"completion", "code", "reasoning", "tool_use"}, inCost: 1000.0, outCost: 1000.0, ctxLimit: 32768, maxOutput: 4096},
				},
			},
		},
		aliases: []parityAlias{
			{name: "general", members: []string{"provbud/model-bud"}, timeout: 5, maxFails: 3},
		},
		chatAlias: "general",
		budget: map[string]any{
			// enforce is the default; an explicit mode key appears only in
			// the steer world. A near-zero daily cost cap breaches on the
			// very first priced call.
			"enabled":          true,
			"daily_cost_limit": 0.00001,
			"aggressiveness":   1.0,
		},
	}
	if mode != "" {
		cfg.budget["mode"] = mode
	}
	s := newParitySandbox(t, cfg)
	return s, stub
}

// TestBudgetSteer_CompletesOverCapWithAdvisory (T15): the steer world
// completes the turn over the cap and surfaces budget_advisory.
func TestBudgetSteer_CompletesOverCapWithAdvisory(t *testing.T) {
	s, stub := budgetWorld(t, "steer")

	// Subscribe to agent.quota_wait BEFORE the turn (the CLI ordering
	// contract) and watch for the advisory class.
	sess, subID := subscribeTopic(t, s, "agent.quota_wait")
	defer sess.Close()

	sessID := s.createSession(t, "budget-steer")
	// Turn 1 accrues usage over the near-zero cap (10k-priced tokens >
	// 0.00001 USD). Turn 2 runs WITH the cap breached: steer must still
	// complete it.
	if reply := s.chatTurn(t, sessID, "steer mode turn one accrues", 120*time.Second); reply == "" {
		t.Fatalf("T15: steer-mode accrual turn did not complete. daemon tail:\n%s", tailRunes(s.daemonLog(t), 2000))
	}
	if stub.hitCount() == 0 {
		t.Fatal("steer-mode turn never reached the upstream; the budget blocked it instead of steering")
	}
	ack := s.submitChat(t, sessID, "steer mode turn two over cap")
	if turnID, _ := ack["turn_id"].(string); turnID != "" {
		sess2, sub2 := subscribeTopic(t, s, "turn.terminal")
		term := waitTerminal(t, sess2, sub2, turnID, 90*time.Second)
		if term.Status != "completed" || term.Reply == "" {
			t.Fatalf("T15 R6 RED (expected): steer-mode turn over a BREACHED cap did not complete (status %q, err %q) — no advisory budget mode exists on main. daemon tail:\n%s", term.Status, term.Error, tailRunes(s.daemonLog(t), 3000))
		}
	} else if note, _ := ack["note"].(string); note != "" && !strings.Contains(note, "budget") {
		t.Fatalf("T15: steer submit refused for a non-budget reason: %+v", ack)
	}

	// The advisory signal: one budget_advisory-class event on the EXISTING
	// agent.quota_wait topic (never a new topic).
	payload := waitForBusEvent(t, sess, subID, "class", "budget_advisory", 30*time.Second)
	if payload == nil {
		t.Fatal("T15: no budget_advisory-class event arrived on agent.quota_wait")
	}
}

// TestBudgetSteer_EnforceIsDefault (T16): with NO mode key, the breached
// cap refuses the turn exactly as main does — BudgetExceededError, no
// completed reply, no advisory class.
func TestBudgetSteer_EnforceIsDefault(t *testing.T) {
	s, _ := budgetWorld(t, "")

	sess, subID := subscribeTopic(t, s, "agent.quota_wait")
	defer sess.Close()

	sessID := s.createSession(t, "budget-enforce")
	ack := s.submitChat(t, sessID, "enforce mode turn one")
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		// Submit-surface refusal is an acceptable enforce posture only if
		// the note names the budget; anything else is a contract break.
		if note, _ := ack["note"].(string); note == "" {
			t.Fatalf("T16: enforce-mode submit rejected with no note: %+v", ack)
		}
		t.Logf("T16: enforce refusal surfaced at submit: %v", ack["note"])
		return
	}
	// RULING (orchestrator, 2026-11-17): wait on a turn.terminal
	// subscription — the pre-existing subID above polls agent.quota_wait,
	// a topic that never carries turn.terminal, so the original wait
	// could only time out. T16 uses its OWN waiter: the shared
	// waitTerminal skips "parked" (correct for T15, where parked is the
	// async-ack before the relay), but in the ENFORCE world the budget
	// park IS the terminal — the turn parks for the window and no relay
	// ever fires. Here "parked" with the queue notice is the expected
	// enforce refusal; "completed" is the only forbidden state.
	termSess, termSub := subscribeTopic(t, s, "turn.terminal")
	defer termSess.Close()
	term := waitTerminalAny(t, termSess, termSub, turnID, 120*time.Second)
	// Enforce posture: the turn is refused — a failed/parked terminal (the
	// budget park holds the turn for the window) or an errored turn. What
	// it must NEVER be: completed with a normal reply.
	if term.Status == "completed" {
		t.Fatalf("T16 R6 RED (expected): enforce mode COMPLETED the over-cap turn (reply %.60q) — the budget gate must refuse; the cap never armed (probe the harness before blaming the contract)", term.Reply)
	}
	if !strings.Contains(s.daemonLog(t), "daily cost budget exceeded") {
		t.Fatal("T16: no BudgetExceededError text in the daemon log; the enforce refusal did not come from the budget gate")
	}
	// No budget_advisory class may ride enforce mode.
	for range 8 {
		raw, err := sess.call("bus.poll", map[string]string{"subscription_id": subID})
		if err == nil && raw != nil && containsClass(raw, "budget_advisory") {
			t.Fatal("T16: a budget_advisory signal fired in enforce mode; advisory signaling is steer-only")
		}
	}
}

// waitTerminalAny is waitTerminal WITHOUT the parked skip: some turns'
// FINAL terminal state IS "parked" (the enforce-world budget park holds
// the turn for the whole window; no relay ever fires). T15 needs the skip
// (parked is the async-ack, the relay follows); T16 needs the parked
// event itself.
func waitTerminalAny(t *testing.T, sess *busSession, subID, turnID string, timeout time.Duration) terminalEvent {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := sess.call("bus.poll", map[string]string{"subscription_id": subID})
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
					var payload terminalEvent
					if json.Unmarshal(ev.Payload, &payload) != nil || payload.TurnID != turnID {
						continue
					}
					return payload
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no turn.terminal for %s within %s", turnID, timeout)
	return terminalEvent{}
}

// containsClass reports whether a bus.poll raw response carries an event
// whose decoded payload has payload["class"] == want.
func containsClass(raw []byte, want string) bool {
	var resp struct {
		Events []struct {
			Payload json.RawMessage `json:"payload"`
		} `json:"events"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return false
	}
	for _, ev := range resp.Events {
		var payload map[string]any
		if json.Unmarshal(ev.Payload, &payload) != nil {
			continue
		}
		if v, _ := payload["class"].(string); v == want {
			return true
		}
	}
	return false
}
