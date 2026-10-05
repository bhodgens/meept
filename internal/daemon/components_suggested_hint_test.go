package daemon

import (
	"context"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/queue"
	"github.com/caimlas/meept/pkg/security"
)

// reportHintChatter answers every turn with the given final-report JSON —
// the scripted stand-in for a finished step turn whose report carries the
// model's advisory suggested_next_hint (agent-routing tree leaf 03).
type reportHintChatter struct {
	response string
}

func (c *reportHintChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: c.response}, nil
}

func (c *reportHintChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, cb llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *reportHintChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "report-hint"}
}

func newReportHintLoop(t *testing.T, response string) *agent.AgentLoop {
	t.Helper()
	return agent.NewAgentLoop("sess-hint-env-test", t.TempDir(),
		agent.WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		agent.WithLLMChatter(&reportHintChatter{response: response}),
		agent.WithSecurityChecker(security.NewPermissionChecker(security.Config{})),
	)
}

// hintReport wraps a completed report JSON in the fenced code block the
// loop's ExtractReport parser expects.
func hintReport(hintJSON string) string {
	return "```json\n" + hintJSON + "\n```\nDone."
}

// TestAgentJobProcessor_EnvelopeCarriesSuggestedNextHint pins the
// successor-hints envelope contract (agent-routing tree leaf 03): the
// step-job result envelope produced by Process (the buildStepResult region)
// projects the finished turn's report-level suggested_next_hint onto the
// envelope, truncated defensively to 64 chars (model output), and omits the
// key entirely when the report carries none — an absent hint must not
// serialize as an empty string, or every completion would look hinted.
func TestAgentJobProcessor_EnvelopeCarriesSuggestedNextHint(t *testing.T) {
	t.Run("report hint lands on envelope", func(t *testing.T) {
		p, _, _ := newTestAgentJobProcessor(t)
		p.agentLoop = newReportHintLoop(t, hintReport(
			`{"status":"completed","accomplished":["did the thing"],"suggested_next_hint":"debugger"}`))

		job := &queue.Job{
			ID:      "job-hint-env-1",
			TaskID:  "task-hint-env-1",
			AgentID: "coder",
			Type:    queue.JobTypeProjectTask,
			Payload: stepJobPayload(t, "step-hint-env-1", "task-hint-env-1", "do the step work"),
		}
		result, err := p.Process(context.Background(), job)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}

		env, ok := result.(map[string]any)
		if !ok {
			t.Fatalf("Process result type %T, want map[string]any", result)
		}
		if got, ok := env["suggested_next_hint"].(string); !ok || got != "debugger" {
			t.Errorf("envelope suggested_next_hint = %v (present=%v), want \"debugger\"", env["suggested_next_hint"], ok)
		}
	})

	t.Run("long hint truncates to 64 chars", func(t *testing.T) {
		p, _, _ := newTestAgentJobProcessor(t)
		p.agentLoop = newReportHintLoop(t, hintReport(
			`{"status":"completed","accomplished":["x"],"suggested_next_hint":"`+strings.Repeat("a", 100)+`"}`))

		job := &queue.Job{
			ID:      "job-hint-env-2",
			TaskID:  "task-hint-env-2",
			AgentID: "coder",
			Type:    queue.JobTypeProjectTask,
			Payload: stepJobPayload(t, "step-hint-env-2", "task-hint-env-2", "do the step work"),
		}
		result, err := p.Process(context.Background(), job)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}

		env := result.(map[string]any)
		got, _ := env["suggested_next_hint"].(string)
		if len(got) != 64 {
			t.Errorf("envelope suggested_next_hint length = %d, want 64 (defensive truncation)", len(got))
		}
		if got != strings.Repeat("a", 64) {
			t.Errorf("truncated hint = %q, want the first 64 chars", got)
		}
	})

	t.Run("absent hint omits the envelope key", func(t *testing.T) {
		p, _, _ := newTestAgentJobProcessor(t)
		p.agentLoop = newReportHintLoop(t, hintReport(
			`{"status":"completed","accomplished":["plain work"]}`))

		job := &queue.Job{
			ID:      "job-hint-env-3",
			TaskID:  "task-hint-env-3",
			AgentID: "coder",
			Type:    queue.JobTypeProjectTask,
			Payload: stepJobPayload(t, "step-hint-env-3", "task-hint-env-3", "do the step work"),
		}
		result, err := p.Process(context.Background(), job)
		if err != nil {
			t.Fatalf("Process: %v", err)
		}

		env := result.(map[string]any)
		if _, present := env["suggested_next_hint"]; present {
			t.Errorf("envelope carries suggested_next_hint = %v, want the key absent (omitempty semantics)", env["suggested_next_hint"])
		}

		// And the serialized envelope must not carry an empty key either:
		// the tactical adoption triggers on non-empty after decode.
		wire, err := json.Marshal(env)
		if err != nil {
			t.Fatalf("marshal envelope: %v", err)
		}
		if strings.Contains(string(wire), "suggested_next_hint") {
			t.Errorf("serialized envelope contains suggested_next_hint: %s", wire)
		}
	})
}
