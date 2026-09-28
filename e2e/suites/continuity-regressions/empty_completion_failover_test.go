//go:build e2e

// Empty-completion alias-failover regressions (commit 317ba37b): a 200-OK
// completion whose content is blank or whitespace-only is a provider FLAKE.
// The internal/llm client must re-dispatch it IMMEDIATELY within the short
// retry budget against the SAME provider (no plan sleep — a blank body is
// not a rate-limit window), and on budget exhaustion surface the bare
// ErrEmptyResponse sentinel so the turn rotates to the fallback provider
// instead of completing with garbage.
package continuityregressions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// whitespaceBody is the agnes-2.5-flash flake shape: a 200-OK completion
// whose content is whitespace-only.
const whitespaceBody = "  \n\t\n"

// predExecutorTurn matches tool-bearing executor turns (the agent loop's
// tool-call round-trips; the same shape marker the fake's heuristics use).
func predExecutorTurn(body map[string]any) bool {
	raw, ok := body["tools"]
	if !ok || raw == nil {
		return false
	}
	tools, ok := raw.([]any)
	return ok && len(tools) > 0
}

// countExecutorRequests counts a fake's received requests that carry a
// tools array (the executor turns that hit the retry loop under test).
func countExecutorRequests(f *harness.FakeLLM) int {
	count := 0
	for _, body := range f.Requests() {
		if predExecutorTurn(body) {
			count++
		}
	}
	return count
}

// TestEmptyCompletionRetriedInBudgetSameProvider pins the in-budget retry:
// the executor's first TWO completions are whitespace-only flakes, the
// third (still inside the short retry budget) returns the real tool call.
// The turn must succeed and the wire must have hit the SAME provider at
// least three times — the flake was absorbed in-loop, not by rotation.
// Manifest scenario continuity-regressions-06.
func TestEmptyCompletionRetriedInBudgetSameProvider(t *testing.T) {
	s := newSandbox(t, withExtraEnv("MEEPT_DISABLE_DIGEST_CONTEXT=1"))
	sessionID := s.createSession("e2e-blank-retry", s.ProjectDir)

	helloPath := filepath.Join(s.ProjectDir, "blank-retry.txt")

	// First two executor turns: whitespace-only completions. After the
	// script budget is exhausted, the legacy heuristics serve the queued
	// tool call and then the post-tool text.
	s.Fake.ScriptN(2, predExecutorTurn, harness.TextResponse(whitespaceBody))
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"continuity-regressions blank-retry pin"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"create the file blank-retry.txt","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.SetPostToolText("Created blank-retry.txt at " + helloPath + ".")
	s.Fake.EnqueueFileWrite("call-cr-blank", helloPath, "blank-retry")

	reply := s.chatTurn(sessionID, "create a file named blank-retry.txt containing blank-retry", 180*time.Second)

	if _, err := os.Stat(helloPath); err != nil {
		t.Fatalf("artifact not created — the flake was not absorbed in-budget: %v\nreply: %s\ndaemon log tail:\n%s",
			err, reply, s.logTail())
	}
	execHits := countExecutorRequests(s.Fake)
	if execHits < 3 {
		t.Fatalf("executor hit the wire only %d time(s), want >= 3 (2 whitespace flakes + the real answer)", execHits)
	}
	if !strings.Contains(reply, "blank-retry") {
		t.Fatalf("reply does not reference the artifact: %q", reply)
	}
}

// TestEmptyCompletionAllBlankRotatesToFallbackProvider pins the exhaustion
// arm: the PRIMARY provider returns whitespace for EVERY executor turn
// (short budget exhausts → bare ErrEmptyResponse → alias failure), and the
// rotation must land on the FALLBACK provider, whose scripted answer serves
// the turn. The turn must complete with the fallback's reply — never with
// an empty/garbage success. Manifest scenario continuity-regressions-07.
func TestEmptyCompletionAllBlankRotatesToFallbackProvider(t *testing.T) {
	s := newSandbox(t,
		withSecondFake(),
		withExtraEnv("MEEPT_DISABLE_DIGEST_CONTEXT=1"),
		withModels(`{
    "classifier":  { "models": ["fallback/ok-model"], "timeout": 10, "max_fails": 2 },
    "summarizer":  { "models": ["fallback/ok-model"], "timeout": 10, "max_fails": 2 },
    "small":       { "models": ["fallback/ok-model"], "timeout": 10, "max_fails": 2 },
    "coder":       { "models": ["fake/blank-model", "fallback/ok-model"], "timeout": 60, "max_fails": 3 },
    "planner":     { "models": ["fallback/ok-model"], "timeout": 60, "max_fails": 3 },
    "analyst":     { "models": ["fallback/ok-model"], "timeout": 60, "max_fails": 3 }
  }`, `{
    "fake": {
      "api": "openai",
      "options": { "baseURL": "__FAKE1_V1__", "noAuth": true },
      "models": {
        "blank-model": {
          "name": "blank-model",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 65536,
          "max_output": 4096,
          "temperature": 0.7
        }
      }
    },
    "fallback": {
      "api": "openai",
      "options": { "baseURL": "__FAKE2_V1__", "noAuth": true },
      "models": {
        "ok-model": {
          "name": "ok-model",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 65536,
          "max_output": 4096,
          "temperature": 0.7
        }
      }
    }
  }`, "fake/blank-model"),
	)
	sessionID := s.createSession("e2e-blank-failover", s.ProjectDir)

	helloPath := filepath.Join(s.ProjectDir, "failover.txt")

	// PRIMARY (fake): every executor turn is a whitespace flake — the
	// client's short budget always exhausts and the sentinel surfaces.
	s.Fake.Script(predExecutorTurn, harness.TextResponse(whitespaceBody))

	// FALLBACK (fake2): serves the queued tool call, then the post-tool
	// text. Its reply text is the marker proving the turn completed via
	// rotation, not via the blank primary.
	s.Fake2.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"continuity-regressions failover pin"}`)
	s.Fake2.SetPlannerResponse(`{"steps":[{"description":"create the file failover.txt","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake2.SetPostToolText("Created failover.txt at " + helloPath + " via the fallback provider.")
	s.Fake2.EnqueueFileWrite("call-cr-failover", helloPath, "failover")

	reply := s.chatTurn(sessionID, "create a file named failover.txt containing failover", 240*time.Second)

	if _, err := os.Stat(helloPath); err != nil {
		t.Fatalf("artifact not created — rotation never landed on the fallback: %v\nreply: %s\ndaemon log tail:\n%s",
			err, reply, s.logTail())
	}
	if strings.TrimSpace(reply) == "" || strings.TrimSpace(reply) == strings.TrimSpace(whitespaceBody) {
		t.Fatalf("turn completed with an empty/whitespace success (317ba37b regression): %q", reply)
	}
	primaryHits := countExecutorRequests(s.Fake)
	fallbackHits := countExecutorRequests(s.Fake2)
	if primaryHits < 3 {
		t.Fatalf("primary provider served only %d executor turn(s), want >= 3 (the full short retry budget)", primaryHits)
	}
	if fallbackHits < 1 {
		t.Fatalf("fallback provider was never hit — no rotation happened (primary hits: %d)", primaryHits)
	}
}
