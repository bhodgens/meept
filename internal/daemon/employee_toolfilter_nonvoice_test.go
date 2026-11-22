package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
)

// Employee tool-visibility pins (bughunt: non-voice employee turns).
//
// Bug 1: the goal-loop (non-voice) executor lane ran every employee turn
// directly on the shared daemon AgentLoop, so the model always saw the
// full template tool list regardless of the constitution. The fix routes
// each employee-bound turn through a PER-TURN CLONE whose registry is
// filtered by employeeVisibleTools, mirroring the voice lane. The shared
// template loop's registry must never be mutated.
//
// The ACP lane (acpEmployeeExecutorAdapter) is deliberately NOT
// double-filtered: the loop it resolves is a registry agent's loop whose
// registry is already spec-scoped (AgentRegistry.filterTools), and the
// acp harness branch never serves meept tool definitions to a model.

// --- employeeTurnClone: the per-turn clone helper ---

func TestEmployeeToolfilter_TurnCloneFiltersNotTemplate(t *testing.T) {
	parent := nameRegistry{tools: []tools.Tool{
		nameTool{name: "inbox"},
		nameTool{name: "shell"},
	}}
	template := agent.NewAgentLoop("template-etf", t.TempDir(), agent.WithToolRegistry(parent))
	c := &employee.Constitution{Constraints: employee.ConstitutionalConstraints{
		ToolsAllowed: []string{"inbox"},
	}}

	clone := employeeTurnClone(template, "conv-etf-1", "secretary", c)
	if clone == nil {
		t.Fatal("employeeTurnClone returned nil for a live template")
	}

	// The clone sees only the allowlisted tool.
	if got := defNames(clone.ToolRegistry()); len(got) != 1 || got[0] != "inbox" {
		t.Fatalf("clone tools = %v, want [inbox]", got)
	}
	if clone.ToolRegistry().Get("shell") != nil {
		t.Fatal("shell is reachable on the clone registry")
	}
	// The shared template registry is untouched.
	if got := defNames(template.ToolRegistry()); len(got) != 2 {
		t.Fatalf("template tools = %v, want both", got)
	}
	if template.ToolRegistry().Get("shell") == nil {
		t.Fatal("clone filtered the SHARED template registry")
	}
	// The agent id is stamped so the call-time employee gate routes.
	if clone.GetAgentID() != "secretary" {
		t.Fatalf("clone agent id = %q, want secretary", clone.GetAgentID())
	}
}

func TestEmployeeToolfilter_TurnCloneNilTemplate(t *testing.T) {
	if got := employeeTurnClone(nil, "conv", "emp", &employee.Constitution{}); got != nil {
		t.Fatal("nil template must yield a nil clone, never a panic")
	}
}

// --- employeeBotExecutor: the local lane gets the bound wrapper ---

func TestEmployeeToolfilter_LocalEmployeeGetsBoundExecutor(t *testing.T) {
	var buf bytes.Buffer
	logger := bufLogger(t, &buf)
	parent := nameRegistry{tools: []tools.Tool{
		nameTool{name: "inbox"},
		nameTool{name: "shell"},
	}}
	template := agent.NewAgentLoop("shared-etf", t.TempDir(), agent.WithToolRegistry(parent))
	inner := &agentLoopBotExecutorAdapter{agentLoop: template, logger: logger}

	emp := employee.Employee{
		ID: "secretary",
		Constitution: employee.Constitution{
			Purpose: "answer calls",
			Constraints: employee.ConstitutionalConstraints{
				ToolsAllowed: []string{"inbox"},
			},
		}}

	got, refuse := employeeBotExecutor(emp, inner, stubSpecSource{specs: map[string]*agent.AgentSpec{}}, "", dummyResolver(), nil, logger)
	if refuse {
		t.Fatal("a healthy local employee must never be refused")
	}
	bound, ok := got.(*employeeBoundAgentLoopExecutor)
	if !ok {
		t.Fatalf("executor = %T, want *employeeBoundAgentLoopExecutor", got)
	}
	if bound.employeeID != "secretary" {
		t.Fatalf("bound employee id = %q, want secretary", bound.employeeID)
	}
	if bound.inner != inner {
		t.Fatal("wrapper must delegate to the default executor, not replace it")
	}
	if buf.Len() != 0 {
		t.Errorf("wrapping must log nothing, got: %s", buf.String())
	}
}

func TestEmployeeToolfilter_ConstitutionlessEmployeeKeepsDefaultExecutor(t *testing.T) {
	var buf bytes.Buffer
	logger := bufLogger(t, &buf)
	template := agent.NewAgentLoop("shared-etf2", t.TempDir())
	inner := &agentLoopBotExecutorAdapter{agentLoop: template, logger: logger}

	emp := employee.Employee{
		ID: "plain"}
	// No constitution: the visibility rule would deny everything the
	// employee should inherit; the default executor passes through
	// byte-identical.
	got, refuse := employeeBotExecutor(emp, inner, stubSpecSource{specs: map[string]*agent.AgentSpec{}}, "", dummyResolver(), nil, logger)
	if refuse {
		t.Fatal("must not refuse")
	}
	var want bot.BotExecutor = inner
	if got != want {
		t.Fatalf("executor = %T, want the inner adapter unchanged", got)
	}
}

func TestEmployeeToolfilter_NonAgentLoopExecutorUntouched(t *testing.T) {
	// A non-*agentLoopBotExecutorAdapter default (test doubles, future
	// executors) passes through un-wrapped: the fix binds the known
	// shared-loop lane, never blindly wraps everything.
	logger := slog.New(slog.DiscardHandler)
	sentinel := &sentinelExecutor{}
	emp := employee.Employee{
		ID: "e",
		Constitution: employee.Constitution{
			Purpose:     "p",
			Constraints: employee.ConstitutionalConstraints{ToolsAllowed: []string{"inbox"}},
		}}
	got, refuse := employeeBotExecutor(emp, sentinel, stubSpecSource{specs: map[string]*agent.AgentSpec{}}, "", dummyResolver(), nil, logger)
	if refuse || got != bot.BotExecutor(sentinel) {
		t.Fatalf("got=%v refuse=%v, want the sentinel unchanged", got, refuse)
	}
}

// --- ExecuteBot on the bound wrapper: full turn, shared registry intact ---

func TestEmployeeToolfilter_BoundTurnRunsAndLeavesSharedIntact(t *testing.T) {
	parent := nameRegistry{tools: []tools.Tool{
		nameTool{name: "inbox"},
		nameTool{name: "shell"},
	}}
	chatter := &personaChatter{reply: "goal round done"}
	logger := slog.New(slog.DiscardHandler)
	template := agent.NewAgentLoop("shared-etf3", t.TempDir(),
		agent.WithLLMChatter(chatter),
		agent.WithToolRegistry(parent))
	inner := &agentLoopBotExecutorAdapter{agentLoop: template, logger: logger}

	c := employee.Constitution{
		Purpose:     "answer calls",
		Constraints: employee.ConstitutionalConstraints{ToolsAllowed: []string{"inbox"}},
	}
	// The turn ran on a per-turn clone that saw only the allowlisted
	// tool. The cloneHook seam (test-only) snapshots the clone before
	// RunOnce; the production executor writes no shared state.
	var cloneTools []string
	bound := &employeeBoundAgentLoopExecutor{
		inner:        inner,
		employeeID:   "secretary",
		constitution: c,
		cloneHook:    func(cl *agent.AgentLoop) { cloneTools = defNames(cl.ToolRegistry()) },
	}
	out, tokens, err := bound.ExecuteBot(context.Background(), "sys", "do the thing")
	if err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
	if out != "goal round done" || tokens != 0 {
		t.Fatalf("got (%q, %d), want (\"goal round done\", 0)", out, tokens)
	}
	// The turn ran through the chatter; the shared template loop's
	// registry was never touched.
	if got := defNames(template.ToolRegistry()); len(got) != 2 || got[1] != "shell" {
		t.Fatalf("shared template tools after turn = %v, want both", got)
	}
	if template.GetAgentID() == "secretary" {
		t.Fatalf("shared template agent id mutated to the employee id")
	}
	if len(cloneTools) != 1 || cloneTools[0] != "inbox" {
		t.Fatalf("turn-clone tools = %v, want [inbox]", cloneTools)
	}
}

func TestEmployeeToolfilter_BoundTurnNilLoopFailsLoud(t *testing.T) {
	bound := &employeeBoundAgentLoopExecutor{
		inner:        &agentLoopBotExecutorAdapter{logger: slog.New(slog.DiscardHandler)},
		employeeID:   "x",
		constitution: employee.Constitution{Purpose: "p"},
	}
	if _, _, err := bound.ExecuteBot(context.Background(), "", "x"); err == nil {
		t.Fatal("nil shared loop must fail loudly, not panic")
	}
}

// --- ExecutionCostCents pins (goal-loop budget lane) ---

// costedPersonaChatter is personaChatter carrying a per-turn dollar cost
// on the response, so a goal-loop round records spend.
type costedPersonaChatter struct {
	costUSD float64
}

func (c *costedPersonaChatter) Chat(_ context.Context, _ []llm.ChatMessage, _ ...llm.ChatOption) (*llm.Response, error) {
	return &llm.Response{Content: "done", CostUSD: c.costUSD}, nil
}

func (c *costedPersonaChatter) ChatWithProgress(ctx context.Context, msgs []llm.ChatMessage, p llm.ProgressCallback, opts ...llm.ChatOption) (*llm.Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *costedPersonaChatter) Config() *llm.ModelConfig {
	return &llm.ModelConfig{ModelID: "costed-persona-stub"}
}

// TestExecutionCostCents_RoundsDollarsToCents pins the budget lane's cent
// conversion: a turn whose provider cost is 0.0123 dollars reports 1 cent
// (round), so DailyBudgetCents enforcement sees real spend.
// RED before the fix: ExecutionCostCents did not exist on the adapters
// (compile error); a truncating stub would report 0.
func TestExecutionCostCents_RoundsDollarsToCents(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	template := agent.NewAgentLoop("shared-cost-1", t.TempDir(),
		agent.WithLLMChatter(&costedPersonaChatter{costUSD: 0.0123}))
	inner := &agentLoopBotExecutorAdapter{agentLoop: template, logger: logger}
	bound := &employeeBoundAgentLoopExecutor{
		inner:        inner,
		employeeID:   "costed-emp",
		constitution: employee.Constitution{Purpose: "answer"},
	}
	if _, _, err := bound.ExecuteBot(context.Background(), "sys", "do the thing"); err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
	if got := bound.ExecutionCostCents(); got != 1 {
		t.Errorf("bound ExecutionCostCents() = %d, want 1 (round(0.0123*100))", got)
	}
	if got := inner.ExecutionCostCents(); got != 0 {
		t.Errorf("shared inner ExecutionCostCents() = %d, want 0 (the turn ran on the clone, not the shared loop)", got)
	}
}

// TestExecutionCostCents_ZeroCostReportsZero pins the unconfigured-price
// posture: a loop whose provider reports no cost (CostUSD 0, the default
// when prices are unset) reports 0 cents — never a fabricated spend.
// RED before the fix: ExecutionCostCents did not exist (compile error).
func TestExecutionCostCents_ZeroCostReportsZero(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	template := agent.NewAgentLoop("shared-cost-0", t.TempDir(),
		agent.WithLLMChatter(&personaChatter{reply: "done"}))
	inner := &agentLoopBotExecutorAdapter{agentLoop: template, logger: logger}
	bound := &employeeBoundAgentLoopExecutor{
		inner:        inner,
		employeeID:   "free-emp",
		constitution: employee.Constitution{Purpose: "answer"},
	}
	if _, _, err := bound.ExecuteBot(context.Background(), "sys", "do the thing"); err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
	if got := bound.ExecutionCostCents(); got != 0 {
		t.Errorf("bound ExecutionCostCents() = %d, want 0 (unseen cost)", got)
	}
	if got := inner.ExecutionCostCents(); got != 0 {
		t.Errorf("inner ExecutionCostCents() = %d, want 0 (unseen cost)", got)
	}
}

// --- the ACP lane is not double-filtered: routing unchanged ---

func TestEmployeeToolfilter_ACPPathStaysOnACPSAdapter(t *testing.T) {
	src := stubSpecSource{specs: map[string]*agent.AgentSpec{"bob": acpSpec("decision")}}
	emp := acpEmployee(employee.Tier2Propose)
	// A constitution with an allowlist must NOT rewrap the acp executor:
	// the acp branch never serves meept tool definitions to a model, and
	// the registry loop it resolves is already spec-scoped (filterTools).
	emp.Constitution.Constraints.ToolsAllowed = []string{"inbox"}
	got, refuse := employeeBotExecutor(emp, &sentinelExecutor{}, src, "", dummyResolver(), nil, slog.New(slog.DiscardHandler))
	if refuse {
		t.Fatal("must not refuse")
	}
	if _, ok := got.(*acpEmployeeExecutorAdapter); !ok {
		t.Fatalf("executor = %T, want the acp adapter unchanged", got)
	}
}
