package agent

import (
	"context"
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
