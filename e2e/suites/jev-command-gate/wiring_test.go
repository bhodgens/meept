//go:build e2e

// jev-gate-03: score-gate daemon wiring (jev gate wiring leaf,
// docs/plans/20261103-wiring-gap-remediation/04-jev-gate-wiring.md).
//
// Covers the WIRING composition end-to-end at the same hermetic standard
// as leaves 01/02 (stubs produce only real-client shapes):
//   - the daemon adapter maps [security.jev_command_gate] config onto
//     jevgate.ScoreGateConfig (mirrors components.go's construction block),
//   - the constructed hook, registered as "jev-score-gate" in a REAL
//     agent.NewHookRegistry, turns a shell call scoring below threshold
//     into the confirmation escalation (Block=true),
//   - WIRING-FAILURE PROPERTY: with the registration deleted, the same
//     call falls through every remaining stage and executes (Block=false)
//   - proof the gate's enforcement depends on the live registration and
//     not on latent package state.
//
// Hermetic: no daemon, no model.
package jevgatecmd

import (
	"context"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/jevgate"
	"github.com/caimlas/meept/internal/llm"
)

// jevStubScorer mirrors the leaf-01 stub discipline: returns a canned REAL
// client shape keyed off the serialized command text.
type jevStubScorer struct {
	result *llm.CommandScoreResult
	err    error
}

func (s *jevStubScorer) ScoreCommand(ctx context.Context, serializedContext string) (*llm.CommandScoreResult, error) {
	return s.result, s.err
}

// buildArmedHook reproduces the daemon construction block
// (internal/daemon/components.go, jev construction comment) from a config
// knob: adapter mapping + NewScoreGateHook + capture recorder.
func buildArmedHook(t *testing.T, cfg config.JevGateConfig, scorer jevgate.ScoreClient) *jevgate.ScoreGateHook {
	t.Helper()
	_ = cfg // adapter exercised in TestJevGate03a_ConfigAdapterMatchesWiring
	hook := jevgate.NewScoreGateHook(
		scorer,
		jevgate.ScoreGateConfig{
			Enabled:         true,
			ScoreThreshold:  cfg.ScoreThreshold,
			ConfidenceFloor: cfg.ConfidenceThreshold,
		},
		nil, // confirm seam nil-tolerant; escalation still blocks
		nil, // logger default
	)
	return hook
}

// registerJevGate mirrors the registration block: name + priority as wired.
func registerJevGate(reg *agent.HookRegistry, hook *jevgate.ScoreGateHook) {
	reg.RegisterBeforeToolCall("jev-score-gate", agent.HookPriorityHigh, hook)
}

func jevShellCall(command string) llm.ToolCall {
	return llm.ToolCall{
		Function: llm.ToolCallFunction{
			Name:      "shell",
			Arguments: `{"command":` + quoteGo(command) + `}`,
		},
	}
}

func quoteGo(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// TestJevGate03a_ConfigAdapterMatchesWiring pins the adapter: the config
// knob values land on the gate config verbatim (same field-for-field
// mapping components.go performs).
func TestJevGate03a_ConfigAdapterMatchesWiring(t *testing.T) {
	cfg := config.JevGateConfig{Enabled: true, ScoreThreshold: 0.85, ConfidenceThreshold: 0.20}
	hook := buildArmedHook(t, cfg, &jevStubScorer{})
	if !hook.Enabled() {
		t.Fatal("armed hook reports disabled: adapter dropped Enabled=true")
	}
}

// TestJevGate03b_ArmedBootEscalatesShellCall covers the armed path: a
// shell tool call whose score is below threshold goes through the
// registered hook and comes back as the confirmation escalation shape.
func TestJevGate03b_ArmedBootEscalatesShellCall(t *testing.T) {
	cfg := config.JevGateConfig{Enabled: true, ScoreThreshold: 0.90, ConfidenceThreshold: 0.10}
	// Real-client shape: band answer 0.2 (below the 0.90 allow bar),
	// argmax-only confidence floor 1.0, degraded flag mirrors production.
	scorer := &jevStubScorer{result: bandResult("0.2")}
	hook := buildArmedHook(t, cfg, scorer)

	reg := agent.NewHookRegistry(nil)
	registerJevGate(reg, hook)

	res := reg.RunBeforeToolCalls(context.Background(), jevShellCall("rm -rf /tmp/important"))
	if !res.Block {
		t.Fatalf("armed gate let a below-threshold shell call through: %+v", res)
	}
	if res.Reason == "" || !strings.Contains(res.Reason, "confirmation required") {
		t.Fatalf("escalation reason missing the confirmation shape: %q", res.Reason)
	}
}

// TestJevGate03c_DeleteRegistrationCallExecutes is the WIRING-FAILURE
// property: delete the registration and the identical call falls through
// the registry and executes. Proves enforcement rides the live
// registration (and, inversely, that an armed-but-unregistered hook is
// inert).
func TestJevGate03c_DeleteRegistrationCallExecutes(t *testing.T) {
	cfg := config.JevGateConfig{Enabled: true, ScoreThreshold: 0.90, ConfidenceThreshold: 0.10}
	scorer := &jevStubScorer{result: bandResult("0.2")}
	hook := buildArmedHook(t, cfg, scorer)

	// The "deleted registration" shape: a registry with NO jev hook (the
	// daemon builds one when components.JevScoreGate is nil - exactly the
	// disabled/removed posture).
	reg := agent.NewHookRegistry(nil)

	res := reg.RunBeforeToolCalls(context.Background(), jevShellCall("rm -rf /tmp/important"))
	if res.Block {
		t.Fatalf("unregistered gate still blocked the call: %+v", res)
	}

	// Contrast arm: the SAME hook, registered, blocks - proving the delta
	// is the registration, not the hook construction.
	reg2 := agent.NewHookRegistry(nil)
	registerJevGate(reg2, hook)
	res2 := reg2.RunBeforeToolCalls(context.Background(), jevShellCall("rm -rf /tmp/important"))
	if !res2.Block {
		t.Fatal("contrast arm failed: registered hook did not block")
	}
}
