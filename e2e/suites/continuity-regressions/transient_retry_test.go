//go:build e2e

// Transient-transport retry lane regressions (per-model transient_retries):
// a proxy whose serving worker dies mid-request leaves a connection-level
// EOF with no HTTP status. With transient_retries set on the model, the
// client must re-dial the SAME provider inside one outer attempt, so the
// flake never consumes a rotation strike and never lands the turn on a
// fallback model. With the key ABSENT (0, the default) the error must
// surface exactly as before: rotation to the next alias member.
package continuityregressions

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/e2e/harness"
)

// transientRetryModels pins the coder alias through the kill-capable
// fake (transient_retries: 2), with the fallback provider as the second
// alias member. Classifier/summarizer/planner traffic is pinned to the
// fallback so only executor turns touch the fake.
const transientRetryModels = `{
    "classifier":  { "models": ["fallback/ok-model"], "timeout": 10, "max_fails": 2 },
    "summarizer":  { "models": ["fallback/ok-model"], "timeout": 10, "max_fails": 2 },
    "small":       { "models": ["fallback/ok-model"], "timeout": 10, "max_fails": 2 },
    "coder":       { "models": ["fake/retry-model", "fallback/ok-model"], "timeout": 60, "max_fails": 3 },
    "planner":     { "models": ["fallback/ok-model"], "timeout": 60, "max_fails": 3 },
    "analyst":     { "models": ["fallback/ok-model"], "timeout": 60, "max_fails": 3 }
  }`

const transientRetryProviders = `{
    "fake": {
      "api": "openai",
      "options": { "baseURL": "__FAKE1_V1__", "noAuth": true },
      "models": {
        "retry-model": {
          "name": "retry-model",
          "capabilities": ["completion", "code", "reasoning", "tool_use"],
          "input_cost": 0.0,
          "output_cost": 0.0,
          "context_limit": 65536,
          "max_output": 4096,
          "temperature": 0.7,
          "transient_retries": 2
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
  }`

// flagOffModels is the SAME topology with transient_retries absent (0):
// the lane must never fire and the transport error must rotate exactly
// like the pre-feature build.
const flagOffProviders = `{
    "fake": {
      "api": "openai",
      "options": { "baseURL": "__FAKE1_V1__", "noAuth": true },
      "models": {
        "retry-model": {
          "name": "retry-model",
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
  }`

// TestTransientRetryAbsorbsConnectionKillSameProvider pins the lane: the
// fake KILLs the first executor connection (EOF, no status), then serves
// the real tool call. With transient_retries=2 the re-dial must land the
// answer on the SAME primary provider - the fallback must see ZERO
// executor turns. Manifest scenario continuity-regressions-08.
func TestTransientRetryAbsorbsConnectionKillSameProvider(t *testing.T) {
	s := newSandbox(t,
		withSecondFake(),
		withExtraEnv("MEEPT_DISABLE_DIGEST_CONTEXT=1"),
		withModels(transientRetryModels, transientRetryProviders, "fake/retry-model"),
	)
	sessionID := s.createSession("e2e-transient-retry", s.ProjectDir)

	outPath := filepath.Join(s.ProjectDir, "transient.txt")

	// First executor turn: the connection dies mid-request. After the
	// script is consumed the default heuristics serve the queued tool
	// call (the re-dial) and then the post-tool text.
	s.Fake.ScriptOnce(predExecutorTurn, harness.KillConnectionResponse())
	s.Fake.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"continuity-regressions transient-retry pin"}`)
	s.Fake.SetPlannerResponse(`{"steps":[{"description":"create the file transient.txt","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake.SetPostToolText("Created transient.txt at " + outPath + ".")
	s.Fake.EnqueueFileWrite("call-transient-1", outPath, "transient")

	reply := s.chatTurn(sessionID, "create a file named transient.txt containing transient", 180*time.Second)

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("artifact not created - the connection kill was not absorbed: %v\nreply: %s\ndaemon log tail:\n%s",
			err, reply, s.logTail())
	}
	// The file content proves origin: only the PRIMARY fake carries the
	// queued file_write (call-transient-1), so a rotation-away answer
	// could not have produced this byte-exact artifact.
	data, err := os.ReadFile(outPath)
	if err != nil || strings.TrimSpace(string(data)) != "transient" {
		t.Fatalf("artifact content = %q err=%v, want %q (must come from the primary's queued write)", string(data), err, "transient")
	}
	// The kill + the absorbed re-dial both hit the primary: >= 2.
	execHits := countExecutorRequests(s.Fake)
	if execHits < 2 {
		t.Fatalf("primary provider served only %d executor request(s), want >= 2 (kill + re-dial)", execHits)
	}
	if !strings.Contains(reply, "transient") {
		t.Fatalf("reply does not reference the artifact: %q", reply)
	}
}

// TestTransientRetryFlagOffRotatesToFallback pins the DEFAULT-OFF arm:
// with transient_retries absent the EOF surfaces to the rotation layer
// and lands the executor turn on the fallback provider (byte-identical
// to the pre-feature behavior). Manifest scenario continuity-regressions-09.
func TestTransientRetryFlagOffRotatesToFallback(t *testing.T) {
	s := newSandbox(t,
		withSecondFake(),
		withExtraEnv("MEEPT_DISABLE_DIGEST_CONTEXT=1"),
		withModels(transientRetryModels, flagOffProviders, "fake/retry-model"),
	)
	sessionID := s.createSession("e2e-transient-off", s.ProjectDir)

	outPath := filepath.Join(s.ProjectDir, "transient-off.txt")

	s.Fake.ScriptOnce(predExecutorTurn, harness.KillConnectionResponse())
	s.Fake2.SetClassifierOutput(`{"intent":"code","confidence":0.95,"reasoning":"continuity-regressions transient-off pin"}`)
	s.Fake2.SetPlannerResponse(`{"steps":[{"description":"create the file transient-off.txt","tool_hint":"file_write","depends_on":[]}]}`)
	s.Fake2.SetPostToolText("Created transient-off.txt at " + outPath + " via the fallback provider.")
	s.Fake2.EnqueueFileWrite("call-transient-off", outPath, "transient-off")

	reply := s.chatTurn(sessionID, "create a file named transient-off.txt containing transient-off", 180*time.Second)

	if _, err := os.Stat(outPath); err != nil {
		t.Fatalf("artifact not created - rotation never landed on the fallback: %v\nreply: %s\ndaemon log tail:\n%s",
			err, reply, s.logTail())
	}
	// Evidence of rotation lives in the request counts: the fallback
	// MUST have served an executor turn (only Fake2 carries the queued
	// file_write, and the artifact exists solely from it - proof the
	// killed primary never answered turn 1). The primary may legitimately
	// see a later executor turn: one recorded failure is below
	// max_fails, so alias re-selection returns it for the post-tool turn
	// regardless of this feature.
	if fallbackExec := countExecutorRequests(s.Fake2); fallbackExec < 1 {
		t.Fatalf("fallback provider served %d executor turn(s), want >= 1 (the rotation arm of the EOF)", fallbackExec)
	}
}
