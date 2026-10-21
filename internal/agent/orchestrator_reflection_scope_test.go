package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/lint"
	"github.com/caimlas/meept/pkg/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrchestrator_ReflectionThreadsConversationScope pins the M6 fix
// (bughunt 2026-10-19): handleToolExecutionComplete threads the event's
// conversation_id into the reflection fix-request LLM call via
// WithScopeOptions, so the daemon-singleton ReflectionEngine's calls carry
// the serving session's task scope (affinity header + budget scope) instead
// of running unscoped. The scope is observed on the wire through the real
// llm.Client's ${session_id} ExtraHeaders sentinel (same channel
// scope_threading_test.go uses).
func TestOrchestrator_ReflectionThreadsConversationScope(t *testing.T) {
	runCase := func(t *testing.T, conversationID string) []string {
		t.Helper()
		_, client, log := scopeHeaderServer(t)

		// A real edited file the linter can read; the registered linter
		// always reports one error so RunReflection reaches requestFix.
		dir := t.TempDir()
		src := filepath.Join(dir, "main.go")
		require.NoError(t, os.WriteFile(src, []byte("package main\n"), 0o644))

		linter := lint.NewRegistry()
		linter.Register("go", func(_ context.Context, _, _, _ string) ([]lint.LinterResult, error) {
			return []lint.LinterResult{{
				File:     src,
				Line:     0,
				Message:  "pinned lint error",
				Severity: "error",
				Rule:     "test/pinned",
			}}, nil
		})

		engine := NewReflectionEngineWithConfig(slog.Default(), linter, nil, client, ReflectionConfig{
			AutoLint: true,
			AutoTest: false,
			WorkDir:  dir,
		})

		o := NewOrchestrator(OrchestratorDeps{Logger: slog.Default()})
		o.SetReflectionEngine(engine)

		payload := map[string]any{
			"tool_call_id": "call-1",
			"tool_name":    "file_edit",
			"success":      true,
			"edited_files": []string{src},
		}
		if conversationID != "" {
			payload[KeyConversationID] = conversationID
		}
		msg, err := models.NewBusMessage(models.MessageTypeEvent, "executor", payload)
		require.NoError(t, err)

		o.handleToolExecutionComplete(context.Background(), msg)

		// The handler runs reflection on a wg goroutine: poll for the
		// fix-request header with a deadline.
		deadline := time.Now().Add(5 * time.Second)
		for len(log.all()) == 0 && time.Now().Before(deadline) {
			time.Sleep(10 * time.Millisecond)
		}
		return log.all()
	}

	t.Run("with conversation id", func(t *testing.T) {
		headers := runCase(t, "conv-scope-123")
		require.NotEmpty(t, headers, "reflection fix request must hit the LLM endpoint")
		assert.Equal(t, "conv-scope-123", headers[0],
			"reflection fix call must carry the event's conversation id as task scope")
	})

	t.Run("without conversation id", func(t *testing.T) {
		headers := runCase(t, "")
		require.NotEmpty(t, headers)
		assert.Equal(t, "", headers[0],
			"an event without conversation_id must not invent a scope")
	})
}

// TestOrchestrator_ReflectionScopePayloadShape pins the wire key the
// handler decodes: the executor publishes conversation_id on
// tool.execution.complete (executor.go publishToolComplete), and the
// handler's decode struct must use the same JSON tag.
func TestOrchestrator_ReflectionScopePayloadShape(t *testing.T) {
	raw := `{"tool_call_id":"c1","tool_name":"file_edit","success":true,"conversation_id":"conv-9","edited_files":["a.go"]}`
	var event struct {
		ToolCallID     string   `json:"tool_call_id"`
		ToolName       string   `json:"tool_name"`
		Success        bool     `json:"success"`
		ConversationID string   `json:"conversation_id,omitempty"`
		EditedFiles    []string `json:"edited_files,omitempty"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &event))
	assert.Equal(t, "conv-9", event.ConversationID)
	assert.Equal(t, KeyConversationID, "conversation_id",
		"KeyConversationID must stay the wire key the handler decodes")
}
