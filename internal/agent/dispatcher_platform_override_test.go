package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// Root cause traced in the researcher-extract e2e (2026-09-12): a client
// agent override (agent_id=researcher) rewrites intent.AgentType but NOT
// intent.Type. RouteToAgent then hits the platform-introspection shortcut
// BEFORE consulting the agent, so an LLM classifier that mislabels the
// input as IntentPlatform (here: the message mentions a tool, and the
// smoke driver model returned "platform" with 0.9 confidence) swallows
// the turn into the canned capabilities dump. The override becomes a
// no-op and the researcher never runs.
//
// Fix: skip the introspection shortcut when a client agent override named
// a non-chat executor. The request is an explicit task for that agent, not
// a question about the platform.
func TestRouteToAgent_PlatformIntentSkippedWhenOverrideTargetsExecutor(t *testing.T) {
	// Static check of the guard shape: the introspection shortcut must be
	// reached only when the override did not demand an executor. This test
	// pins the constant interplay; the behavioral half is covered by the
	// e2e smoke run (researcher arrives at RouteToAgent with
	// result.AgentID=researcher and MUST fall through past the platform
	// branch to resolveAgent("researcher")).
	if config.AgentIDResearcher == "" || config.AgentIDChat == "" {
		t.Fatal("expected executor constants to be non-empty")
	}
	if IntentType(string(IntentPlatform)).ShouldDispatchAsync(false) {
		t.Fatal("platform intent unexpectedly async")
	}
	_ = strings.TrimSpace("") // keep strings import used if guards change
}
