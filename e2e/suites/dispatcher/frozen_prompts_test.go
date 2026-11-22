//go:build e2e

// Package dispatcher frozen-prompt extension: 02-routing TEST-GATE T7
// (W8; audit-routing R1/R2; master matrix F6, scenarios dispatcher-08/09).
//
// The p1/p6 usability probes are FROZEN verbatim in
// internal/agent/testdata/usability_prompts.json. These scenarios drive
// the exact probe wording through the full hermetic stack with the
// classifier scripted to the biased git verdict the live 350M/8B
// routers emitted, and assert the END STATE: the turn routes to a
// file-capable executor (never the contextless committer) and the
// artifact lands in the session's project dir.
//
// RED today: the shape-gated veto lets the git costume through on the
// edit prompts (dispatcher-08), and the p6 control's explicit-tool
// wording is the positive control that must keep routing correctly.

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

// frozenPrompts loads the verbatim probe fixture from the agent package
// testdata (single source of truth; never reword).
func frozenPrompts(t *testing.T) (p1 []string, p6 []string) {
	t.Helper()
	// Suite cwd is e2e/suites/dispatcher; repo root is three levels up.
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "internal", "agent", "testdata", "usability_prompts.json"))
	if err != nil {
		t.Fatalf("read frozen usability prompts: %v", err)
	}
	var f struct {
		P1Naive   []string `json:"p1_naive"`
		P6Control []string `json:"p6_control"`
	}
	if err := json.Unmarshal(data, &f); err != nil {
		t.Fatalf("parse frozen prompts: %v", err)
	}
	if len(f.P1Naive) != 4 || len(f.P6Control) != 3 {
		t.Fatalf("fixture drift: p1=%d p6=%d, want 4/3", len(f.P1Naive), len(f.P6Control))
	}
	return f.P1Naive, f.P6Control
}

// terminalOutcome captures one turn.terminal payload for a turn id.
type terminalOutcome struct {
	TurnID     string `json:"turn_id"`
	Status     string `json:"status"`
	AgentID    string `json:"agent_id"`
	IntentType string `json:"intent_type"`
	Reply      string `json:"reply"`
}

// waitTerminalFor polls a terminal subscription for the given turn id
// and returns the first non-parked terminal payload.
func waitTerminalFor(t *testing.T, ts *terminalSub, turnID string, timeout time.Duration) terminalOutcome {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		raw, err := ts.sess.call("bus.poll", map[string]string{"subscription_id": ts.subID})
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
					var out terminalOutcome
					if json.Unmarshal(ev.Payload, &out) != nil || out.TurnID != turnID {
						continue
					}
					if out.Status == "parked" {
						continue
					}
					return out
				}
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no non-parked turn.terminal for %s within %s", turnID, timeout)
	return terminalOutcome{}
}

// requireExecutorRoute asserts the routing end state: never the git
// lane / committer for file-edit work.
func requireExecutorRoute(t *testing.T, out terminalOutcome, prompt string) {
	t.Helper()
	if out.IntentType == "git" || out.AgentID == "committer" {
		t.Errorf("frozen prompt routed to git/committer (intent=%q agent=%q): the git-verb agreement veto must discard the biased verdict regardless of input shape (W8)\nprompt: %s",
			out.IntentType, out.AgentID, prompt)
	}
}

// TestDispatcher08FrozenNaiveEditRoutesExecutor drives the p1 T2 edit
// prompt VERBATIM with the classifier scripted to the live git costume.
// The turn must route to a file-capable executor and the edit must land
// in the project dir - never the contextless committer.
func TestDispatcher08FrozenNaiveEditRoutesExecutor(t *testing.T) {
	p1, _ := frozenPrompts(t)
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp08-frozen-edit", s.ProjectDir)

	// T1: create the file (the frozen naive wording; heuristic code lane).
	artifact := filepath.Join(s.ProjectDir, "hello.txt")
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("hello.txt")),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"hello world","direct":true}`,
		}))
	s.Fake.SetPostToolText("Created hello.txt at " + artifact)
	s.ChatTurn(t, sessionID, p1[0], 120*time.Second)
	if _, err := os.Stat(artifact); err != nil {
		t.Fatalf("T1 artifact missing: %v", err)
	}

	// T2: the FROZEN edit prompt with the biased git verdict scripted.
	s.Fake.ResetScripts()
	s.Fake.Script(harness.IsClassifierRequest(),
		harness.ClassifierJSONResponse(`{"intent":"git","confidence":0.91,"agent_type":"committer"}`))
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("hello meept")),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"hello meept","direct":true}`,
		}))
	s.Fake.SetPostToolText("Updated hello.txt to say hello meept.")

	tsub := subscribeTerminal(t, s)
	ack := s.SubmitChatHTTP(t, sessionID, p1[1])
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("dispatcher-08: ack missing turn_id: %+v", ack)
	}
	out := waitTerminalFor(t, tsub, turnID, 120*time.Second)
	requireExecutorRoute(t, out, p1[1])

	// The edit actually landed (end state, not narration).
	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("artifact unreadable after edit turn: %v", err)
	}
	if !strings.Contains(string(data), "hello meept") {
		t.Errorf("hello.txt still says %q after the frozen edit turn: the edit never executed (git lane swallowed it)", string(data))
	}
}

// TestDispatcher09FrozenControlEditRoutesExecutor is the p6 positive
// control: explicit tool-named edit wording must route to the executor
// and land the edit. If this passes while dispatcher-08 fails, the
// defect is wording sensitivity, not a broken lane (the probe's own
// hypothesis, pinned as a regression).
func TestDispatcher09FrozenControlEditRoutesExecutor(t *testing.T) {
	_, p6 := frozenPrompts(t)
	s := newStack(t)
	sessionID := s.CreateSession(t, "disp09-frozen-control", s.ProjectDir)

	artifact := filepath.Join(s.ProjectDir, "hello.txt")
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("hello.txt")),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"hello world","direct":true}`,
		}))
	s.Fake.SetPostToolText("Created hello.txt at " + artifact)
	s.ChatTurn(t, sessionID, p6[0], 120*time.Second)

	s.Fake.ResetScripts()
	s.Fake.ScriptN(1,
		harness.And(harness.IsExecutorRequest(), harness.Not(harness.IsPlannerRequest()),
			harness.MessageContains("hello meept")),
		harness.ToolCallResponse(harness.ToolCall{
			Name:      "file_write",
			Arguments: `{"path":"` + artifact + `","content":"hello meept","direct":true}`,
		}))
	s.Fake.SetPostToolText("Updated hello.txt to say hello meept.")

	tsub := subscribeTerminal(t, s)
	ack := s.SubmitChatHTTP(t, sessionID, p6[1])
	turnID, _ := ack["turn_id"].(string)
	if turnID == "" {
		t.Fatalf("dispatcher-09: ack missing turn_id: %+v", ack)
	}
	out := waitTerminalFor(t, tsub, turnID, 120*time.Second)
	requireExecutorRoute(t, out, p6[1])

	data, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatalf("artifact unreadable after control edit turn: %v", err)
	}
	if !strings.Contains(string(data), "hello meept") {
		t.Errorf("control edit never landed: hello.txt = %q", string(data))
	}
}
