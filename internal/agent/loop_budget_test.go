package agent

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
	"github.com/caimlas/meept/pkg/security"
)

// sizerTool is a mock tool that also implements tools.ResultSizer with the
// given declared floor.
type sizerTool struct {
	MockTool
	floor int
}

func (t *sizerTool) MaxResultTokens() int { return t.floor }

// budgetScriptChatter is a scripted llm.Chatter: first call emits one tool
// call, second call ends the turn.
type budgetScriptChatter struct {
	calls int
}

func (c *budgetScriptChatter) Chat(ctx context.Context, messages []llm.ChatMessage, opts ...llm.ChatOption) (*llm.Response, error) {
	c.calls++
	if c.calls == 1 {
		return &llm.Response{
			Content:      "fetching",
			FinishReason: "tool_calls",
			Usage:        llm.TokenUsage{TotalTokens: 10},
			ToolCalls: []llm.ToolCall{{
				ID:   "call-budget-1",
				Type: "function",
				Function: llm.ToolCallFunction{
					Name:      "file_read",
					Arguments: `{"path":"/tmp/x.txt"}`,
				},
			}},
		}, nil
	}
	return &llm.Response{Content: "done", FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 10}}, nil
}

func (c *budgetScriptChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *budgetScriptChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "budget-script-chatter"}
}

// newBudgetFloorLoop builds a loop wired for budget-floor tests. The scripted
// chatter emits a `file_read` tool call (a RiskSafe BuiltinRules action, so
// the default-permissive test security checker allows execution).
func newBudgetFloorLoop(t *testing.T, tool tools.Tool) (*AgentLoop, *Conversation) {
	t.Helper()
	registry := NewPlaceholderToolRegistry()
	if tool != nil {
		registry.Register(tool)
	}
	secChecker := security.NewPermissionChecker(security.Config{})
	loop := NewAgentLoop("sess-budget-floor", "/tmp",
		WithLLMChatter(&budgetScriptChatter{}),
		WithToolRegistry(registry),
		WithSecurityChecker(secChecker),
		WithMessageBus(bus.New(nil, slog.New(slog.DiscardHandler))),
		WithAgentConfig(AgentConfig{MaxIterations: 5}),
	)
	loop.executor = NewExecutor(registry, secChecker)
	conv := loop.conversations.Get("conv-budget-floor")
	conv.AddUserMessage("go")
	return loop, conv
}

// runDecayedTurn drives one reasoningCycle turn in a state where the dynamic
// tool budget decays to its 600-token floor: after the tool-carrying LLM
// response (usage 10) with convBudget 11, ratio = 1 - 10/11 so
// dynamicToolBudget = max(3000*1/11, 600) = 600.
func runDecayedTurn(t *testing.T, loop *AgentLoop, conv *Conversation) {
	t.Helper()
	loop.config.MaxConversationTokens = 11
	if _, err := loop.reasoningCycle(context.Background(), conv, "conv-budget-floor"); err != nil {
		t.Fatalf("reasoningCycle: %v", err)
	}
}

// toolResultContent returns the tool-role message content the loop produced
// for the single scripted tool call.
func toolResultContent(t *testing.T, conv *Conversation) string {
	t.Helper()
	for _, msg := range conv.GetMessages() {
		if msg.Role == llm.RoleTool && msg.ToolCallID == "call-budget-1" {
			return msg.Content
		}
	}
	t.Fatal("no tool result message found for call-budget-1")
	return ""
}

// TestLoopBudget_FloorHonoredForDeclaredTool: with the dynamic budget
// decayed to its 600-token floor, a ResultSizer tool declaring a 1400-token
// floor gets its result INTACT in the conversation. 3900 chars + JSON
// wrapper (~62) = ~3962 chars fits the lifted 1400*3=4200-char budget; at
// the unlifted 600-token floor (1800 chars) it would be clipped. (The spec's
// "4200-char content" ignores ToCompressedJSON's 200-char wrapper reserve;
// 3900 encodes the same intent with correct arithmetic.)
func TestLoopBudget_FloorHonoredForDeclaredTool(t *testing.T) {
	content := strings.Repeat("a", 3900)
	tool := &sizerTool{
		MockTool: *NewMockTool("file_read", "fetch", func(ctx context.Context, args map[string]any) (any, error) {
			return content, nil
		}),
		floor: 1400,
	}
	loop, conv := newBudgetFloorLoop(t, tool)
	runDecayedTurn(t, loop, conv)

	got := toolResultContent(t, conv)
	if !strings.Contains(got, content) {
		t.Errorf("3900-char content clipped despite declared 1400-token floor (result len=%d)", len(got))
	}
}

// TestLoopBudget_FloorCapsNotRaises: in the same decayed state a 5000-char
// content IS still clipped under the 1400-token floor — a declared floor is
// a floor, never a ceiling raise (5000+wrapper chars > 1400*3=4200).
func TestLoopBudget_FloorCapsNotRaises(t *testing.T) {
	content := strings.Repeat("b", 5000)
	tool := &sizerTool{
		MockTool: *NewMockTool("file_read", "fetch", func(ctx context.Context, args map[string]any) (any, error) {
			return content, nil
		}),
		floor: 1400,
	}
	loop, conv := newBudgetFloorLoop(t, tool)
	runDecayedTurn(t, loop, conv)

	got := toolResultContent(t, conv)
	if strings.Contains(got, content) {
		t.Errorf("5000-char content NOT clipped under the 1400-token floor (result len=%d)", len(got))
	}
	if !strings.Contains(got, "truncated") {
		t.Errorf("clipped result missing truncation marker: %.120s", got)
	}
}

// TestLoopBudget_UndeclaredToolClippedAtDynamicFloor: the SAME decayed
// position with a tool that does NOT implement ResultSizer is clipped at the
// 600-token dynamic floor (today's behavior preserved).
func TestLoopBudget_UndeclaredToolClippedAtDynamicFloor(t *testing.T) {
	content := strings.Repeat("c", 3900)
	tool := NewMockTool("file_read", "fetch", func(ctx context.Context, args map[string]any) (any, error) {
		return content, nil
	})
	loop, conv := newBudgetFloorLoop(t, tool)
	runDecayedTurn(t, loop, conv)

	got := toolResultContent(t, conv)
	if strings.Contains(got, content) {
		t.Errorf("undeclared tool's 3900-char content NOT clipped at decayed 600-token budget (len=%d)", len(got))
	}
}

// TestLoopBudget_NilRegistryAndUnknownTool: a nil registry (and likewise an
// unknown tool name) must not panic and must preserve today's clipping.
func TestLoopBudget_NilRegistryAndUnknownTool(t *testing.T) {
	content := strings.Repeat("d", 3900)
	tool := NewMockTool("other_tool", "other", func(ctx context.Context, args map[string]any) (any, error) {
		return "unrelated", nil
	})
	loop, conv := newBudgetFloorLoop(t, tool)
	loop.registry = nil // unknown tool name + nil registry
	runDecayedTurn(t, loop, conv)

	got := toolResultContent(t, conv)
	if strings.Contains(got, content) {
		t.Errorf("nil registry: 3900-char content NOT clipped (len=%d)", len(got))
	}
}

// TestLoopBudget_GenerousBudgetFloorNoop: under a generous (undecayed)
// budget the declared floor changes nothing and a 5000-char content stays
// whole (default convBudget -> dynamic budget ~3000 >= floor 1400).
func TestLoopBudget_GenerousBudgetFloorNoop(t *testing.T) {
	content := strings.Repeat("e", 5000)
	tool := &sizerTool{
		MockTool: *NewMockTool("file_read", "fetch", func(ctx context.Context, args map[string]any) (any, error) {
			return content, nil
		}),
		floor: 1400,
	}
	loop, conv := newBudgetFloorLoop(t, tool)
	if _, err := loop.reasoningCycle(context.Background(), conv, "conv-budget-floor"); err != nil {
		t.Fatalf("reasoningCycle: %v", err)
	}

	got := toolResultContent(t, conv)
	if !strings.Contains(got, content) {
		t.Errorf("5000-char content clipped under generous budget (len=%d)", len(got))
	}
}

// TestEffectiveResultBudget tables the pure budget helper the loop consults
// per result: dynamic budget, declared-floor lift, ceiling cap, and the
// nil-tool no-op.
func TestEffectiveResultBudget(t *testing.T) {
	sizer := &sizerTool{floor: 1400}
	plain := NewMockTool("plain", "plain", nil)
	bigFloor := &sizerTool{floor: 9000} // above the ceiling

	tests := []struct {
		name    string
		dynamic int
		tool    tools.Tool
		want    int
	}{
		{"undeclared tool keeps dynamic budget", 600, plain, 600},
		{"nil tool keeps dynamic budget", 600, nil, 600},
		{"floor below dynamic keeps dynamic", 2000, sizer, 2000},
		{"floor above dynamic lifts to floor", 600, sizer, 1400},
		{"floor capped at ceiling", 600, bigFloor, ToolResultMaxTokens},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := effectiveResultBudget(tc.dynamic, tc.tool); got != tc.want {
				t.Errorf("effectiveResultBudget(%d, %v) = %d, want %d", tc.dynamic, tc.tool, got, tc.want)
			}
		})
	}
}
