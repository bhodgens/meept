package daemon

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
)

// invocationProbe executes a real executor path without a model or artifact evidence.
type invocationProbe struct{}

func (invocationProbe) Name() string                          { return "platform_status" }
func (invocationProbe) Description() string                   { return "offline invocation probe" }
func (invocationProbe) IsReadOnly(map[string]any) bool        { return true }
func (invocationProbe) IsConcurrencySafe(map[string]any) bool { return true }
func (invocationProbe) Parameters() llm.FunctionParameters {
	return llm.FunctionParameters{Type: "object"}
}
func (invocationProbe) Execute(context.Context, map[string]any) (any, error) {
	return "probe-complete", nil
}

func TestToolEvidenceCollectorRealExecutorCompletion(t *testing.T) {
	registry := agent.NewPlaceholderToolRegistry()
	registry.Register(invocationProbe{})
	b := bus.New(nil, nil)
	sub := b.Subscribe("invocation-probe", "tool.execution.complete")
	defer b.Unsubscribe(sub)
	executor := agent.NewExecutor(registry, nil, agent.WithExecutorBus(b), agent.WithExecutorAgentID("researcher"))
	executor.SetConversationID("probe-conversation")
	result := executor.Execute(context.Background(), llm.ToolCall{
		ID: "probe-call", Type: "function",
		Function: llm.ToolCallFunction{Name: "platform_status", Arguments: "{}"},
	})
	if !result.Success {
		t.Fatalf("real tool execution failed: %s", result.Error)
	}
	select {
	case message := <-sub.Channel:
		collector := newToolEvidenceCollector()
		collector.absorb(message.Payload)
		calls := collector.drainInvocations("probe-conversation")
		if len(calls) != 1 || calls[0].ToolCallID != "probe-call" || !calls[0].Success || calls[0].Cached {
			t.Fatalf("real completion not recorded: %+v", calls)
		}
	case <-time.After(time.Second):
		t.Fatal("executor completion event missing")
	}
}

func TestToolEvidenceCollectorRecordsInvocationsWithoutEvidence(t *testing.T) {
	c := newToolEvidenceCollector()
	payload := json.RawMessage(`{"conversation_id":"step-task-1-step-1","agent_id":"researcher","tool_call_id":"call-1","tool_name":"json_extract","success":true,"cached":false}`)
	c.absorb(payload)
	c.absorb(payload) // A duplicate delivery must not become a second call.
	calls := c.drainInvocations("step-task-1-step-1")
	if len(calls) != 1 {
		t.Fatalf("got %d calls, want one recorded json_extract invocation", len(calls))
	}
	if calls[0].ToolCallID != "call-1" || calls[0].ToolName != "json_extract" || !calls[0].Success {
		t.Fatalf("incorrect invocation: %+v", calls[0])
	}
	if got := c.drain("step-task-1-step-1"); len(got) != 0 {
		t.Fatalf("invocation must not invent artifact evidence: %+v", got)
	}
	if got := c.drainInvocations("step-task-1-step-1"); len(got) != 0 {
		t.Fatalf("invocation drain did not clear: %+v", got)
	}
}

func TestToolEvidenceCollectorInvocationIsolationAndFailures(t *testing.T) {
	c := newToolEvidenceCollector()
	for _, payload := range []string{
		`{"conversation_id":"a","agent_id":"researcher","tool_call_id":"call-1","tool_name":"json_extract","success":false,"cached":false}`,
		`{"conversation_id":"b","agent_id":"researcher","tool_call_id":"call-2","tool_name":"json_extract","success":true,"cached":true}`,
	} {
		c.absorb(json.RawMessage(payload))
	}
	a, b := c.drainInvocations("a"), c.drainInvocations("b")
	if len(a) != 1 || len(b) != 1 || a[0].Success || !b[0].Cached {
		t.Fatalf("failure/cache state or isolation lost: a=%+v b=%+v", a, b)
	}
}
