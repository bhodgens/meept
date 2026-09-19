package builtin

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestTaskCreate_InvalidArgsRejectedAtBoundary pins the 2026-09-18 e2e
// offender (45x task_create{} with per-call "name is required" errors): the
// registry gate must reject task_create with missing required args at the
// boundary, via an invalid_args error envelope, WITHOUT ever invoking the
// tool — zero tasks may reach the store.
func TestTaskCreate_InvalidArgsRejectedAtBoundary(t *testing.T) {
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	reg := tools.NewRegistry(nil)
	reg.Register(NewTaskCreateTool(store))

	// The exact e2e offender shape: empty args.
	res, execErr := reg.Execute(context.Background(), "task_create", map[string]any{})
	require.Nil(t, execErr, "boundary failures go in the envelope, not the error return")
	require.NotNil(t, res)
	assert.False(t, res.Success)
	assert.Equal(t, "invalid_args", res.ErrCode)
	assert.Contains(t, res.Error, "name is missing")

	// Defense-in-depth: the tool's own hand-rolled check still holds when
	// the tool is invoked directly with a whitespace name that slips past
	// any future gate change.
	tool := NewTaskCreateTool(store)
	_, directErr := tool.Execute(context.Background(), map[string]any{"name": "  "})
	require.Error(t, directErr)
	assert.Contains(t, directErr.Error(), "name is required")

	// Zero tasks created in the store.
	tasks, listErr := store.List(nil, 100)
	require.NoError(t, listErr)
	assert.Empty(t, tasks, "no task may be created when the boundary rejects the call")
}

// TestTaskCreate_ValidArgsThroughRegistry pins that a well-formed task_create
// call still flows through the real registry path and lands in the store.
func TestTaskCreate_ValidArgsThroughRegistry(t *testing.T) {
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { store.Close() })

	reg := tools.NewRegistry(nil)
	reg.Register(NewTaskCreateTool(store))

	res, execErr := reg.Execute(context.Background(), "task_create", map[string]any{
		"name":        "regression pin",
		"description": "created via the real registry path",
	})
	require.Nil(t, execErr)
	require.NotNil(t, res)
	assert.True(t, res.Success)
	assert.Equal(t, "", res.ErrCode)

	tasks, listErr := store.List(nil, 100)
	require.NoError(t, listErr)
	require.Len(t, tasks, 1)
	assert.Equal(t, "regression pin", tasks[0].Name)
}
