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
	// The decision is stored for leaf 04's consumption seam.
	require.NotNil(t, hook.pendingEscalation)
	assert.True(t, hook.pendingEscalation.Escalate)
	assert.Equal(t, "openai/gpt-5", hook.pendingEscalation.ModelRef)
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
