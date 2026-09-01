package agent

import (
	"context"
	"fmt"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAutoTriggerDisabled(t *testing.T) {
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "a.go")

	hook := NewVerificationAutoTrigger(tr, VerificationConfig{
		Enabled:     false,
		AutoTrigger: true,
	})
	mod := hook.PrepareNextTurn(context.Background(), TurnState{})
	assert.False(t, mod.Modified)
	assert.Empty(t, mod.ExtraMessages)
}

func TestAutoTriggerBelowThreshold(t *testing.T) {
	tr := NewVerificationTracker(3)
	tr.RecordToolCall("file_write", "a.go")
	tr.RecordToolCall("file_edit", "b.go")

	hook := NewVerificationAutoTrigger(tr, VerificationConfig{
		Enabled:     true,
		AutoTrigger: true,
	})
	mod := hook.PrepareNextTurn(context.Background(), TurnState{})
	assert.False(t, mod.Modified)
	assert.Empty(t, mod.ExtraMessages)
}

func TestAutoTriggerAtThreshold(t *testing.T) {
	tr := NewVerificationTracker(3)
	tr.RecordToolCall("file_write", "main.go")
	tr.RecordToolCall("file_edit", "util.go")
	tr.RecordToolCall("file_delete", "old.go")

	hook := NewVerificationAutoTrigger(tr, VerificationConfig{
		Enabled:     true,
		AutoTrigger: true,
	})
	mod := hook.PrepareNextTurn(context.Background(), TurnState{})

	require.True(t, mod.Modified)
	require.Len(t, mod.ExtraMessages, 1)
	assert.Equal(t, llm.RoleUser, mod.ExtraMessages[0].Role)
	assert.Contains(t, mod.ExtraMessages[0].Content, "verify your work")
	assert.Contains(t, mod.ExtraMessages[0].Content, "main.go")
	assert.Contains(t, mod.ExtraMessages[0].Content, "util.go")
	assert.Contains(t, mod.ExtraMessages[0].Content, "old.go")
	assert.Equal(t, "verification auto-trigger (nudge)", mod.Reason)

	// Tracker should be reset after trigger.
	assert.False(t, tr.ShouldTrigger())
}

func TestAutoTriggerAutoTriggerFalse(t *testing.T) {
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "a.go")

	hook := NewVerificationAutoTrigger(tr, VerificationConfig{
		Enabled:     true,
		AutoTrigger: false,
	})
	mod := hook.PrepareNextTurn(context.Background(), TurnState{})
	assert.False(t, mod.Modified)
	assert.Empty(t, mod.ExtraMessages)
}

func TestAutoTriggerNoFilesTracked(t *testing.T) {
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("shell_execute", "")

	hook := NewVerificationAutoTrigger(tr, VerificationConfig{
		Enabled:     true,
		AutoTrigger: true,
	})
	mod := hook.PrepareNextTurn(context.Background(), TurnState{})

	require.True(t, mod.Modified)
	require.Len(t, mod.ExtraMessages, 1)
	assert.Contains(t, mod.ExtraMessages[0].Content, "(none tracked)")
}

// driveToExhaustion runs the hook through maxLoops fix iterations plus the
// final exhausted call, returning the exhaustion-turn modification and the
// hook for post-state assertions. Each turn re-arms a fresh tracker so the
// verifier spawns on every PrepareNextTurn.
func driveToExhaustion(t *testing.T, hook *VerificationAutoTrigger) TurnModification {
	t.Helper()
	maxLoops := hook.config.MaxFixLoops
	if maxLoops < 1 {
		maxLoops = 3
	}
	for i := 0; i < maxLoops; i++ {
		tr := NewVerificationTracker(1)
		tr.RecordToolCall("file_write", "f.go")
		hook.tracker = tr
		mod := hook.PrepareNextTurn(context.Background(), TurnState{})
		require.True(t, mod.Modified)
		require.Contains(t, mod.Reason, "fix loop", "turn %d should be a fix loop", i+1)
	}
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "f.go")
	hook.tracker = tr
	mod := hook.PrepareNextTurn(context.Background(), TurnState{})
	require.True(t, mod.Modified)
	return mod
}

const legacyEscalationPrefix = "Adversarial verification failed after %d fix attempts. Manual review needed.\n\nLast verifier output:\n%s"

func TestHandleFail_ExhaustedWithEscalationModel(t *testing.T) {
	verifierOutput := "VERDICT: FAIL\nCheck: go test\nError: 3 tests failing"
	spawner := &mockSpawner{output: verifierOutput}
	hook := NewVerificationAutoTrigger(NewVerificationTracker(1), VerificationConfig{
		Enabled: true, AutoTrigger: true, MaxFixLoops: 3,
	})
	hook.SetSpawner(spawner)
	hook.SetAgentSpec(&AgentSpec{EscalationModel: "strong/model"})
	hook.SetResolver(&stubResolver{resolved: "openai/gpt-5"})

	mod := driveToExhaustion(t, hook)

	assert.Equal(t, "verification model escalation", mod.Reason)
	require.Len(t, mod.ExtraMessages, 1)
	assert.Equal(t, llm.RoleUser, mod.ExtraMessages[0].Role)
	// Content must carry both the escalation-model instruction (with the
	// resolved ref) and the last verifier output.
	assert.Contains(t, mod.ExtraMessages[0].Content, "openai/gpt-5")
	assert.Contains(t, mod.ExtraMessages[0].Content, "escalation model")
	assert.Contains(t, mod.ExtraMessages[0].Content, verifierOutput)
	// Leaf 04: the decision is CONSUMED at the exhaustion-turn return —
	// ApplyEscalation cleared it, tagged ModelOverride, and armed the
	// persistent override (R1).
	assert.Nil(t, hook.pendingEscalation, "pending decision must be consumed on the exhaustion turn")
	assert.Equal(t, "openai/gpt-5", mod.ModelOverride, "modification carries the escalated ref")
	// Q2: fixCount reset so the escalated model gets its own budget.
	assert.Equal(t, 0, hook.fixCount)
}

func TestHandleFail_ExhaustedNoEscalationModel_LegacyByteIdentical(t *testing.T) {
	verifierOutput := "VERDICT: FAIL\nError: build broke"
	spawner := &mockSpawner{output: verifierOutput}
	hook := NewVerificationAutoTrigger(NewVerificationTracker(1), VerificationConfig{
		Enabled: true, AutoTrigger: true, MaxFixLoops: 3,
	})
	hook.SetSpawner(spawner)
	// spec and resolver deliberately never set: legacy path.

	mod := driveToExhaustion(t, hook)

	wantContent := fmt.Sprintf(legacyEscalationPrefix, 3, verifierOutput)
	assert.Equal(t, wantContent, mod.ExtraMessages[0].Content, "legacy exhaustion message must be byte-identical")
	assert.Equal(t, "verification escalation", mod.Reason)
	assert.Nil(t, hook.pendingEscalation, "legacy path must not mark an escalation")
	assert.Equal(t, 0, hook.fixCount)
}

func TestHandleFail_ExhaustedResolutionFailed_LegacyOutput(t *testing.T) {
	verifierOutput := "VERDICT: FAIL\nError: build broke"
	spawner := &mockSpawner{output: verifierOutput}
	hook := NewVerificationAutoTrigger(NewVerificationTracker(1), VerificationConfig{
		Enabled: true, AutoTrigger: true, MaxFixLoops: 3,
	})
	hook.SetSpawner(spawner)
	// escalation_model configured but resolver missing → resolution_failed
	// must degrade to the legacy user-escalation output.
	hook.SetAgentSpec(&AgentSpec{EscalationModel: "strong/model"})
	hook.SetResolver(nil)

	mod := driveToExhaustion(t, hook)

	wantContent := fmt.Sprintf(legacyEscalationPrefix, 3, verifierOutput)
	assert.Equal(t, wantContent, mod.ExtraMessages[0].Content, "resolution_failed must fall back to legacy output verbatim")
	assert.Equal(t, "verification escalation", mod.Reason)
	assert.Nil(t, hook.pendingEscalation)
	assert.Equal(t, 0, hook.fixCount)
}

func TestVerificationSetters_NilSafe(t *testing.T) {
	hook := NewVerificationAutoTrigger(NewVerificationTracker(1), VerificationConfig{})

	assert.NotPanics(t, func() { hook.SetAgentSpec(nil) }, "SetAgentSpec(nil) must be a no-op")
	assert.NotPanics(t, func() { hook.SetResolver(nil) }, "SetResolver(nil) must be a no-op")
	assert.Nil(t, hook.spec)
	assert.Nil(t, hook.resolver)

	// Typed-nil guard: a non-nil interface carrying a nil pointer must not
	// replace a previously-set resolver (mirrors SetSpawner semantics).
	var typedNil *stubResolver
	assert.NotPanics(t, func() { hook.SetResolver(typedNil) })
	assert.Nil(t, hook.resolver)

	// Non-nil values are stored.
	hook.SetAgentSpec(&AgentSpec{EscalationModel: "m"})
	assert.NotNil(t, hook.spec)
	hook.SetResolver(&stubResolver{resolved: "r"})
	assert.NotNil(t, hook.resolver)
}

// ---------------------------------------------------------------------------
// Leaf 04: ApplyEscalation — model switch with persistent override (R1),
// fresh-turn restore, and bus observability.
// ---------------------------------------------------------------------------

// escalationSpawner records every SpawnVerifier modelRef (leaf contract: the
// escalated window must cover the verifier spawn too) and returns a FAIL
// verdict so repeated PrepareNextTurn calls drive the fix loop.
type escalationSpawner struct {
	calls int
	refs  []string
	fail  bool
}

func (c *escalationSpawner) SpawnVerifier(_ context.Context, _ string, modelRef string) (string, error) {
	c.calls++
	c.refs = append(c.refs, modelRef)
	if c.fail {
		return "VERDICT: FAIL\nCheck: build\nError: stale", nil
	}
	return "### Check: build\n**Result: PASS**\n\nVERDICT: PASS", nil
}

// newEscalationHarness wires a hook with stub seams and returns recorders:
// applied refs, cleared count, and published [topic, to_model] events.
func newEscalationHarness(maxLoops int) (*VerificationAutoTrigger, *escalationSpawner, *[]string, *int, *[][]string) {
	spawner := &escalationSpawner{fail: true}
	applied := &[]string{}
	cleared := new(int)
	events := &[][]string{}
	hook := NewVerificationAutoTrigger(NewVerificationTracker(1), VerificationConfig{
		Enabled:     true,
		AutoTrigger: true,
		MaxFixLoops: maxLoops,
	})
	hook.SetSpawner(spawner)
	hook.SetAgentSpec(&AgentSpec{EscalationModel: "strong/model"})
	hook.SetResolver(&stubResolver{resolved: "zai/glm-4.7"})
	hook.SetOverrideApplier(func(ref string) { *applied = append(*applied, ref) })
	hook.SetClearOverride(func() { *cleared++ })
	hook.SetEventPublisher(func(topic string, payload map[string]any) {
		*events = append(*events, []string{topic, payload["to_model"].(string)})
	})
	hook.SetAgentIDSource(func() string { return "agent-under-test" })
	return hook, spawner, applied, cleared, events
}

// rearmTracker fires the hook on the next PrepareNextTurn call.
func rearmTracker(t *testing.T, hook *VerificationAutoTrigger) {
	t.Helper()
	tr := NewVerificationTracker(1)
	tr.RecordToolCall("file_write", "f.go")
	hook.tracker = tr
}

func TestEscalationAppliesPersistentOverrideOnExhaustionTurn(t *testing.T) {
	hook, _, applied, _, events := newEscalationHarness(2)
	driveToExhaustion(t, hook)

	assert.Equal(t, []string{"zai/glm-4.7"}, *applied, "override must be armed the moment escalation is decided")
	require.Len(t, *events, 1)
	assert.Equal(t, []string{TopicAgentModelEscalated, "zai/glm-4.7"}, (*events)[0])
}

// The bus payload must carry the EXACT key set from master Contract 4 —
// {agent_id, from_model, to_model, reason, fix_loops} — with from_model set
// to the base ref the loop was running before the switch.
func TestEscalationBusPayloadKeysExact(t *testing.T) {
	spawner := &escalationSpawner{fail: true}
	events := &[]map[string]any{}
	hook := NewVerificationAutoTrigger(NewVerificationTracker(1), VerificationConfig{
		Enabled:     true,
		AutoTrigger: true,
		MaxFixLoops: 0, // unset → handleFail applies the default of 3; fix_loops must report 3
	})
	hook.SetSpawner(spawner)
	hook.SetAgentSpec(&AgentSpec{EscalationModel: "strong/model"})
	hook.SetResolver(&stubResolver{resolved: "zai/glm-4.7"})
	hook.SetEventPublisher(func(topic string, payload map[string]any) {
		*events = append(*events, payload)
	})

	// Drive to exhaustion on an observable base ref so from_model is
	// captured (mirrors driveToExhaustion but passes ModelRef): 3 fix
	// loops, then the 4th call exceeds MaxFixLoops=3.
	for i := 0; i < 4; i++ {
		hook.tracker = NewVerificationTracker(1)
		hook.tracker.RecordToolCall("file_write", "f.go")
		hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "base/ref"})
	}

	require.Len(t, *events, 1)
	payload := (*events)[0]
	wantKeys := []string{"agent_id", "from_model", "to_model", "reason", "fix_loops"}
	require.Len(t, payload, len(wantKeys), "payload must have exactly the contract keys")
	for _, k := range wantKeys {
		if _, ok := payload[k]; !ok {
			t.Errorf("payload missing key %q (have %v)", k, payload)
		}
	}
	assert.Equal(t, "base/ref", payload["from_model"], "from_model is the pre-escalation base ref")
	assert.Equal(t, "zai/glm-4.7", payload["to_model"])
	assert.Equal(t, "fix_loops_exhausted", payload["reason"])
	assert.Equal(t, 3, payload["fix_loops"], "unset MaxFixLoops must report the effective default 3")
}

func TestEscalationBusPayloadNoSecondEventOnEarlyPath(t *testing.T) {
	// The exhaustion turn consumes the decision via handleFail's
	// ApplyEscalation call; the hook's early PrepareNextTurn path must not
	// double-publish for the same decision.
	hook, _, _, _, events := newEscalationHarness(1)
	driveToExhaustion(t, hook)
	require.Len(t, *events, 1)

	// Nothing pending → early path no-op → no additional events, even when
	// the tracker re-fires for an unrelated new fix loop.
	rearmTracker(t, hook)
	hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "base/ref"})
	assert.Len(t, *events, 1, "no pending decision must mean no extra bus event")
}

func TestEscalationConsumedOnceAndIdempotent(t *testing.T) {
	hook, _, applied, _, _ := newEscalationHarness(1)
	driveToExhaustion(t, hook)

	assert.Nil(t, hook.pendingEscalation, "pending decision must be consumed")
	assert.Len(t, *applied, 1)

	// Second call with nothing pending is an idempotent no-op.
	mod := TurnModification{}
	assert.False(t, ApplyEscalation(hook, &mod))
	assert.False(t, mod.Modified)
	assert.Empty(t, mod.ModelOverride)
	assert.Len(t, *applied, 1, "override must NOT be re-armed")
}

func TestEscalatedWindowCoversVerifierSpawn(t *testing.T) {
	hook, spawner, applied, _, _ := newEscalationHarness(1)
	driveToExhaustion(t, hook)
	require.NotEmpty(t, *applied, "precondition: escalation armed")

	// The next verifier spawn happens INSIDE the escalated window.
	rearmTracker(t, hook)
	hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "base/ref"})
	assert.Equal(t, "zai/glm-4.7", spawner.refs[len(spawner.refs)-1],
		"spawn inside the escalated window must use the escalated ref")
}

func TestEscalationPendingBeatsTrackerGate(t *testing.T) {
	hook, spawner, applied, _, _ := newEscalationHarness(1)
	driveToExhaustion(t, hook)
	spawnsAfterDrive := spawner.calls

	// Simulate a harness that re-marks pending but never consumed it: the
	// early path must apply even with an unreachable tracker threshold.
	hook.pendingEscalation = &EscalationDecision{Escalate: true, ModelRef: "zai/glm-4.7", Reason: "fix_loops_exhausted"}
	hook.tracker = NewVerificationTracker(10)
	mod := hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "base/ref"})

	assert.True(t, mod.Modified, "pending escalation must apply even when the tracker gate would not fire")
	assert.Equal(t, "zai/glm-4.7", mod.ModelOverride)
	assert.Nil(t, hook.pendingEscalation)
	assert.Len(t, *applied, 2)
	assert.Equal(t, spawnsAfterDrive, spawner.calls, "no fresh spawn on the early path")
}

func TestFreshTurnClearsPersistentOverride(t *testing.T) {
	hook, spawner, applied, cleared, _ := newEscalationHarness(1)
	driveToExhaustion(t, hook)
	require.NotEmpty(t, *applied)

	// Fresh turn: verifier PASSES — the fix loop is done.
	spawner.fail = false
	rearmTracker(t, hook)
	mod := hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "base/ref"})
	assert.False(t, mod.Modified, "PASS must not modify the turn")

	hook.ClearModelOverride(context.Background())
	assert.Empty(t, hook.escalatedModelRef, "escalated window must be closed")
	assert.Equal(t, 1, *cleared, "loop override must be cleared on fresh turn")

	// Post-clear, a new spawn uses the BASE ref again (no sticky escalation).
	spawner.fail = true
	spawner.refs = nil
	rearmTracker(t, hook)
	hook.PrepareNextTurn(context.Background(), TurnState{ModelRef: "base/ref"})
	assert.Equal(t, "base/ref", spawner.refs[len(spawner.refs)-1],
		"post-escalation fresh turn resolves to the BASE ref")
}

func TestClearEscalationIdempotent(t *testing.T) {
	hook, _, _, cleared, _ := newEscalationHarness(1)
	assert.NotPanics(t, func() { hook.ClearModelOverride(context.Background()) }, "clear with nothing armed is a no-op")
	assert.Equal(t, 0, *cleared)

	hook.escalatedModelRef = "zai/glm-4.7"
	hook.ClearModelOverride(context.Background())
	assert.Equal(t, 1, *cleared)
	hook.ClearModelOverride(context.Background())
	assert.Equal(t, 1, *cleared, "second clear must not re-clear")
}

func TestSetPersistentOverrideNotDemotedByOneShotRouting(t *testing.T) {
	loop := NewAgentLoop("s", "/tmp", WithHookRegistry(NewHookRegistry(nil)))

	loop.SetPersistentModelOverride("zai/glm-4.7")
	require.True(t, loop.IsModelOverridePersistent())

	// applyTurnModification replays the same ref through the one-shot seam.
	loop.applyTurnModification(TurnModification{Modified: true, ModelOverride: "zai/glm-4.7"})

	assert.True(t, loop.IsModelOverridePersistent(), "persistent override must survive one-shot replay of the same ref")
	assert.Equal(t, "zai/glm-4.7", loop.GetModelOverride())
}

func TestSetModelOverrideStillDemotesOnDifferentRef(t *testing.T) {
	loop := NewAgentLoop("s", "/tmp", WithHookRegistry(NewHookRegistry(nil)))
	loop.SetPersistentModelOverride("zai/glm-4.7")
	loop.SetModelOverride("other/model")
	assert.False(t, loop.IsModelOverridePersistent(), "a DIFFERENT ref still demotes to one-shot")
	assert.Equal(t, "other/model", loop.GetModelOverride())
}

func TestHookRegistrySweepsFreshTurnOverrides(t *testing.T) {
	hook, _, _, cleared, _ := newEscalationHarness(1)
	hook.escalatedModelRef = "zai/glm-4.7"

	reg := NewHookRegistry(nil)
	reg.RegisterPrepareNextTurn("verification_auto_trigger", HookPriorityNormal, hook)
	reg.ClearFreshTurnOverrides(context.Background())
	assert.Equal(t, 1, *cleared, "sweep must clear the armed escalation")

	reg.ClearFreshTurnOverrides(context.Background())
	assert.Equal(t, 1, *cleared, "second sweep is a no-op (hook holds nothing)")
}
