package builtin

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/scheduler"
	"github.com/caimlas/meept/internal/task"
	"github.com/caimlas/meept/internal/tools"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeSearchProvider is a scripted SearchProvider for web_search sweep rows:
// valid-args execution must not hit the network.
type fakeSearchProvider struct{}

func (fakeSearchProvider) Search(ctx context.Context, query string, limit int) (SearchResults, error) {
	return SearchResults{Query: query, Results: []SearchResult{{Title: "t", URL: "https://example.com", Snippet: "s"}}}, nil
}

// argSweepCase describes one {tool, required arg, minimal valid args} row.
// The harness drives each case through the REAL registry Execute path using
// the tool's own declared schema, so the sweep stays honest about what the
// gate enforces.
type argSweepCase struct {
	toolName string
	// requiredArg is the required argument the negative case omits.
	requiredArg string
	// validArgs satisfies every required arg and lets Execute succeed.
	validArgs map[string]any
}

func argSweepCases(seedTaskID string, target string) []argSweepCase {
	return []argSweepCase{
		{
			toolName:    "task_create",
			requiredArg: "name",
			validArgs:   map[string]any{"name": "sweep task"},
		},
		{
			toolName:    "task_get",
			requiredArg: "task_id",
			validArgs:   map[string]any{"task_id": seedTaskID},
		},
		{
			toolName:    "task_update",
			requiredArg: "id",
			validArgs:   map[string]any{"id": seedTaskID, "state": "executing"},
		},
		{
			toolName:    "file_write",
			requiredArg: "path",
			validArgs: map[string]any{
				"path":    target,
				"content": "written by sweep",
				"direct":  true, // land on disk; no pending-changes staging in tests
			},
		},
		{
			toolName:    "file_edit",
			requiredArg: "path",
			validArgs: map[string]any{
				"path": target,
				"edits": []any{
					map[string]any{"op": "insert_before", "anchor": "BOF", "content": "FIRST"},
				},
			},
		},
		{
			toolName:    "web_search",
			requiredArg: "query",
			validArgs:   map[string]any{"query": "meept agent daemon", "limit": float64(1)},
		},
		{
			toolName:    "json_extract",
			requiredArg: "schema",
			validArgs: map[string]any{
				"schema": map[string]any{
					"type":       "object",
					"properties": map[string]any{"title": map[string]any{"type": "string"}},
					"required":   []any{"title"},
				},
				"text": "A paper about Test Topic from 2024.",
			},
		},
		{
			toolName:    "cron_create",
			requiredArg: "name",
			validArgs: map[string]any{
				"name":     "sweep job",
				"interval": "minutely",
				"job_type": "reminder",
				"message":  "beep",
			},
		},
	}
}

// TestArgBoundarySweep pins the registry-level boundary for the curated
// builtin set: every tool executes with valid args and rejects empty
// required args with an invalid_args error envelope BEFORE its Execute runs.
func TestArgBoundarySweep(t *testing.T) {
	reg, taskStore, target := newArgSweepRegistry(t)

	// Seed a real task so task_get/task_update valid-args rows exercise the
	// genuine success path (a not-found id is a legitimate error envelope).
	seed := task.NewTask("sweep seed", "")
	require.NoError(t, taskStore.Create(seed))

	for _, tc := range argSweepCases(seed.ID, target) {
		tc := tc
		t.Run(tc.toolName+"/valid_args_execute", func(t *testing.T) {
			res, err := reg.Execute(context.Background(), tc.toolName, tc.validArgs)
			require.Nil(t, err)
			require.NotNil(t, res)
			assert.Truef(t, res.Success,
				"%s with valid args should succeed, got error envelope: %s", tc.toolName, res.Error)
			assert.Equalf(t, "", res.ErrCode,
				"%s valid-args result must not carry invalid_args", tc.toolName)
		})

		t.Run(tc.toolName+"/missing_required_rejected", func(t *testing.T) {
			args := map[string]any{}
			for k, v := range tc.validArgs {
				if k != tc.requiredArg {
					args[k] = v
				}
			}
			res, err := reg.Execute(context.Background(), tc.toolName, args)
			require.Nil(t, err, "boundary failures go in the envelope, not the error return")
			require.NotNil(t, res)
			assert.Falsef(t, res.Success, "%s with missing required arg must not succeed", tc.toolName)
			assert.Equalf(t, "invalid_args", res.ErrCode, "%s envelope must carry ErrCode invalid_args, got %q (%s)", tc.toolName, res.ErrCode, res.Error)
			assert.Containsf(t, res.Error, tc.requiredArg+" is missing",
				"%s error must name the missing argument", tc.toolName)
		})
	}
}

// TestArgBoundarySweep_SchemaRequiredCovered asserts every curated tool's
// schema actually DECLARES the boundary the sweep enforces. If this fails,
// the tool's Required list drifted from its hand-rolled checks and the
// registry gate would no longer protect the documented contract.
func TestArgBoundarySweep_SchemaRequiredCovered(t *testing.T) {
	reg, _, target := newArgSweepRegistry(t)

	for _, tc := range argSweepCases("unused", target) {
		tc := tc
		t.Run(tc.toolName, func(t *testing.T) {
			tool := reg.Get(tc.toolName)
			require.NotNil(t, tool)
			params := tool.Parameters()
			assert.Containsf(t, params.Required, tc.requiredArg,
				"%s schema must declare %q in Required", tc.toolName, tc.requiredArg)
			prop, ok := params.Properties[tc.requiredArg]
			require.Truef(t, ok, "%s schema must declare property %q", tc.toolName, tc.requiredArg)
			assert.NotEmpty(t, prop.Type, "%s.%s must declare a type", tc.toolName, tc.requiredArg)
		})
	}
}

// newArgSweepRegistry builds a registry with the sweep tools registered
// against real dependencies (SQLite task store, temp file, scripted search
// provider, scripted extraction chatter, real scheduler on a temp data dir).
// Returns the task store (for seeding) and the sweep target file path.
func newArgSweepRegistry(t *testing.T) (*tools.Registry, *task.Store, string) {
	t.Helper()

	taskStore, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), nil)
	require.NoError(t, err)
	t.Cleanup(func() { taskStore.Close() })

	dir := t.TempDir()
	target := filepath.Join(dir, "target.txt")
	require.NoError(t, os.WriteFile(target, []byte("alpha\nbeta\n"), 0o644))

	sched, err := scheduler.NewScheduler(config.SchedulerConfig{}, nil, scheduler.WithDataDir(t.TempDir()))
	require.NoError(t, err)

	reg := tools.NewRegistry(nil)
	reg.Register(NewTaskCreateTool(taskStore))
	reg.Register(NewTaskGetTool(taskStore))
	reg.Register(NewTaskUpdateTool(taskStore))
	reg.Register(NewWriteFileTool(nil))
	reg.Register(NewFileEditTool(nil, nil))

	search := NewWebSearchTool(time.Second)
	search.SetSearchProvider(fakeSearchProvider{})
	reg.Register(search)

	reg.Register(NewJSONExtractTool(&fakeChatter{content: `{"title":"Test Topic"}`}, time.Second))
	reg.Register(NewCronCreateTool(sched))

	return reg, taskStore, target
}
