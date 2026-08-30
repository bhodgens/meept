package shadow

// Leaf 06-shadow-capture Task 3: prove that CaptureToolInteraction with a
// tool-call response persists a record to the training store (not dropped),
// classified as task_type=tool_use. Uses the same t.TempDir +
// NewManager(ManagerConfig{...}) fixture shape as manager_test.go, in sync
// mode with heuristic scoring so the capture path makes no LLM calls and
// completes inline.

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// toolCaptureConfig returns an enabled, deterministic, LLM-free config:
// sync mode (ProcessRecord runs inline), full sampling, heuristic scoring.
func toolCaptureConfig(t *testing.T) *Config {
	t.Helper()
	cfg := DefaultConfig()
	cfg.Enabled = true
	cfg.DataDir = t.TempDir()
	cfg.Teacher.Model = "shadow-test-teacher"
	cfg.Shadowing.Mode = ModeSync
	cfg.Shadowing.SampleRate = 1.0
	cfg.Quality.Method = MethodHeuristic
	return cfg
}

func toolCaptureResponse() *llm.Response {
	return &llm.Response{
		Content:      "",
		FinishReason: "tool_calls",
		Usage:        llm.TokenUsage{PromptTokens: 10, CompletionTokens: 5, TotalTokens: 15},
		ToolCalls: []llm.ToolCall{{
			ID:   "call-1",
			Type: "function",
			Function: llm.ToolCallFunction{
				Name:      "read_file",
				Arguments: `{"path": "/tmp/test.txt"}`,
			},
		}},
	}
}

// TestCaptureToolInteraction_PersistsRecord proves a tool-call capture is
// STORED: GetStats must report 1 total record in the tool_use bucket, and
// ListRecords must return the row with the conversation's content.
func TestCaptureToolInteraction_PersistsRecord(t *testing.T) {
	mgr, err := NewManager(ManagerConfig{
		Config: toolCaptureConfig(t),
		Logger: slog.Default(),
	})
	require.NoError(t, err)
	defer mgr.Close()
	require.True(t, mgr.IsEnabled())

	ctx := context.Background()
	// >500 chars so classifyDomain/estimateComplexity clear the default
	// MinComplexity=Moderate gate in ShouldShadow (simple conversations
	// are intentionally not sampled).
	messages := []llm.ChatMessage{
		{Role: llm.RoleUser, Content: strings.Repeat(
			"Read the file /tmp/test.txt and report its full contents back to me. ", 10)},
	}
	mgr.CaptureToolInteraction(ctx, "conv-persist-1", messages, toolCaptureResponse(), "test-model", "")

	stats, err := mgr.GetStats(ctx)
	require.NoError(t, err)
	assert.Equal(t, 1, stats.TotalRecords,
		"tool interaction must be persisted, not dropped")
	assert.Equal(t, 1, stats.RecordsByTaskType[string(TaskTypeToolUse)],
		"persisted record must be classified task_type=tool_use")

	records, err := mgr.trainingStore.ListRecords(ctx, ListRecordsOptions{
		TaskType: TaskTypeToolUse,
	})
	require.NoError(t, err)
	require.Len(t, records, 1)
	rec := records[0]
	assert.Equal(t, "conv-persist-1", rec.ConversationID)
	assert.Equal(t, "test-model", rec.StudentModel)
	assert.Contains(t, rec.StudentContent, "read_file",
		"record content must encode the tool call")
	assert.Equal(t, "", rec.TeacherModel,
		"tool captures never consult the teacher")
}

// TestCaptureToolInteraction_DisabledDropsRecord is the negative control:
// when shadowing is disabled nothing is persisted.
func TestCaptureToolInteraction_DisabledDropsRecord(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Enabled = false
	cfg.DataDir = t.TempDir()

	mgr, err := NewManager(ManagerConfig{Config: cfg, Logger: slog.Default()})
	require.NoError(t, err)
	defer mgr.Close()
	require.False(t, mgr.IsEnabled())

	mgr.CaptureToolInteraction(context.Background(), "conv-persist-off",
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}},
		toolCaptureResponse(), "test-model", "")

	stats, err := mgr.GetStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, stats.TotalRecords, "disabled manager must persist nothing")
}

// TestCaptureToolInteraction_NoResponseDropped pins the nil-response guard:
// a nil response never produces a stored row.
func TestCaptureToolInteraction_NoResponseDropped(t *testing.T) {
	mgr, err := NewManager(ManagerConfig{
		Config: toolCaptureConfig(t),
		Logger: slog.Default(),
	})
	require.NoError(t, err)
	defer mgr.Close()

	mgr.CaptureToolInteraction(context.Background(), "conv-persist-nil",
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}},
		nil, "test-model", "")

	stats, err := mgr.GetStats(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, stats.TotalRecords, "nil response must not be stored")
}
