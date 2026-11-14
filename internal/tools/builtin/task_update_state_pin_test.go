package builtin

// M-1 (bughunt 2026-11-14 round 2) pin: task_update's "state" argument is a
// validated write. The old raw SetState(task.TaskState(stateStr)) let an
// agent-executed tool write ANY string as state with no validity or
// transition check. A bogus string must error; a legal transition must
// succeed; an illegal edge must error naming the allowed transitions.

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTaskUpdatePinStore(t *testing.T) *task.Store {
	t.Helper()
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })
	return store
}

// TestTaskUpdate_BogusStateRejected pins M-1a: an unknown state string is a
// tool error and never lands in the store.
func TestTaskUpdate_BogusStateRejected(t *testing.T) {
	store := newTaskUpdatePinStore(t)
	tool := NewTaskUpdateTool(store)

	created := task.NewTask("m1 pin", "bogus state")
	require.NoError(t, store.Create(created))

	for _, bogus := range []string{"doneness", "PENDING", "", "in_progress"} {
		_, err := tool.Execute(context.Background(), map[string]any{
			"id":    created.ID,
			"state": bogus,
		})
		// Empty string means "no state update requested" — only non-empty
		// bogus values must error. The "" case exercises the skip path.
		if bogus == "" {
			continue
		}
		require.Error(t, err, "state %q must be rejected", bogus)
		assert.Contains(t, err.Error(), "invalid task state")
	}

	got, err := store.GetByID(created.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatePending, got.State, "store state must be untouched after bogus-state rejections")
}

// TestTaskUpdate_LegalTransitionSucceeds pins M-1b: a legal pending ->
// executing edge through the tool still applies and persists.
func TestTaskUpdate_LegalTransitionSucceeds(t *testing.T) {
	store := newTaskUpdatePinStore(t)
	tool := NewTaskUpdateTool(store)

	created := task.NewTask("m1 legal", "legal transition")
	require.NoError(t, store.Create(created))

	res, err := tool.Execute(context.Background(), map[string]any{
		"id":    created.ID,
		"state": "executing",
	})
	require.NoError(t, err)
	require.NotNil(t, res)

	got, err := store.GetByID(created.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StateExecuting, got.State)
}

// TestTaskUpdate_IllegalTransitionRejected pins M-1c: a VALID enum value on
// an ILLEGAL edge errors and names the allowed transitions.
func TestTaskUpdate_IllegalTransitionRejected(t *testing.T) {
	store := newTaskUpdatePinStore(t)
	tool := NewTaskUpdateTool(store)

	created := task.NewTask("m1 illegal", "illegal edge")
	require.NoError(t, store.Create(created))

	// pending -> completed is an illegal edge per taskTransitions.
	_, err := tool.Execute(context.Background(), map[string]any{
		"id":    created.ID,
		"state": "completed",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid task state transition")
	assert.True(t, strings.Contains(err.Error(), "allowed transitions"), "error must name the allowed transitions, got: %v", err)

	got, err := store.GetByID(created.ID)
	require.NoError(t, err)
	assert.Equal(t, task.StatePending, got.State, "illegal edge must leave the persisted state untouched")
}
