//go:build e2e

// Gate test for the interview option gates (leaf 06; contract C8 and the
// C2 setter SetInterviewOptionGates in
// docs/plans/20261101-fresh-context-critic/master.md).
//
// PARKED as .staged. Leaf 06 re-activates ONLY this file. It pins:
//
//	F8 TestInterviewFlagOffKeepsStringQuestions (C8: flag off keeps
//	   today's {"questions": ["..."]} string-shape parse byte-identical;
//	   option-shaped output does NOT leak into the string path)
//
// Self-contained on purpose: this file compiles alone when the sibling
// .staged files are still parked.
package plancritic

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/task"
)

// interviewChatter returns the canned interview-question payload and
// records calls.
type interviewChatter struct {
	reply   string
	mu      sync.Mutex
	calls   int
	prompts []string
}

func (c *interviewChatter) Chat(_ context.Context, messages []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	var sb strings.Builder
	for _, m := range messages {
		sb.WriteString(string(m.Role))
		sb.WriteString(": ")
		sb.WriteString(m.Content)
		sb.WriteString("\n")
	}
	c.mu.Lock()
	c.prompts = append(c.prompts, sb.String())
	c.calls++
	c.mu.Unlock()
	return &llm.Response{Content: c.reply, FinishReason: "stop", Usage: llm.TokenUsage{TotalTokens: 5}}, nil
}

func (c *interviewChatter) ChatWithProgress(ctx context.Context, messages []llm.ChatMessage, progress llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, messages, opts...)
}

func (c *interviewChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "interview-e2e-model"}
}

// interviewPlanner builds a planner for ConductInterview-style parsing.
func interviewPlanner(t *testing.T, chatter llm.Chatter) *agent.StrategicPlanner {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	msgBus := bus.New(nil, logger)
	t.Cleanup(func() { msgBus.Close() })
	store, err := task.NewStore(filepath.Join(t.TempDir(), "tasks.db"), logger)
	if err != nil {
		t.Fatalf("task store: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	reg := agent.NewAgentRegistry(agent.RegistryConfig{
		Logger:         logger,
		PlannerChatter: chatter,
	})
	if err := reg.RegisterSpec(&agent.AgentSpec{
		ID:      config.AgentIDPlanner,
		Name:    "Planner",
		Role:    agent.RoleExecutor,
		Enabled: true,
	}); err != nil {
		t.Fatalf("register planner spec: %v", err)
	}
	return agent.NewStrategicPlanner(agent.StrategicPlannerConfig{
		Registry:       reg,
		TaskStore:      store,
		StepStore:      store.StepStore(),
		Bus:            msgBus,
		MaxPlanSteps:   5,
		PlannerTimeout: 10 * time.Second,
		Logger:         logger,
	})
}

// F8 (C8): flag off keeps today's string-question parse. The planner
// must expose a parse surface whose flag-off behavior is the existing
// {"questions": [...]} shape; option-shaped JSON must not be silently
// degraded into string questions, and a `recommended` key is a parse
// error for that question when the flag is ON (the question is dropped,
// not defaulted).
func TestInterviewFlagOffKeepsStringQuestions(t *testing.T) {
	chatter := &interviewChatter{reply: `{"questions": ["What size avatar?"]}`}
	sp := interviewPlanner(t, chatter)
	sp.SetInterviewOptionGates(false) // C2 setter (RED today: undefined)

	questions := sp.ParseInterviewQuestions(`{"questions": ["What size avatar?"]}`) // parse seam (RED today: undefined; leaf 06 exports it)
	if !reflect.DeepEqual(questions, []string{"What size avatar?"}) {
		t.Fatalf("F8 (C8): flag-off string questions = %#v, want [\"What size avatar?\"] (today's shape must stay byte-identical)", questions)
	}

	// Flag off + option-shaped output: the string path must keep its
	// behavior (empty string list is today's result for a shape it
	// cannot parse; the option payload must NOT appear as strings).
	optionShape := sp.ParseInterviewQuestions(`{"questions": [{"question": "Which store?", "options": [{"mechanism": "s3", "trade_off": "cost", "failure_condition": "outage"}]}]}`)
	if len(optionShape) != 0 {
		t.Fatalf("F8 (C8): flag off must keep the string path; option-shaped JSON leaked through as %#v", optionShape)
	}

	// Flag on: the C8 shape parses into option questions and a
	// `recommended` key is a parse error for that question (dropped).
	sp.SetInterviewOptionGates(true)
	opts := sp.ParseInterviewOptionQuestions(`{"questions": [{"question": "Which store?", "options": [{"mechanism": "s3", "trade_off": "cost", "failure_condition": "outage"}]}, {"question": "Which CDN?", "recommended": "cloudfront", "options": [{"mechanism": "cdn", "trade_off": "latency", "failure_condition": "region loss"}]}]}`) // RED today: undefined
	if len(opts) != 1 {
		t.Fatalf("F8 (C8): option questions = %d, want 1 (the recommended-keyed question must be dropped, not defaulted)", len(opts))
	}
	if len(opts) > 0 && opts[0].Question != "Which store?" {
		t.Fatalf("F8 (C8): kept question = %q, want %q", opts[0].Question, "Which store?")
	}
}
