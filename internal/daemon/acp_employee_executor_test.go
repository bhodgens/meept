package daemon

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/employee"
)

// --- stubs (mirror the existing daemon wiring-test style) ---

// stubACPLoop records RunOnce calls and returns a canned reply.
type stubACPLoop struct {
	mu    sync.Mutex
	calls []stubLoopCall
	reply string
	err   error
}

type stubLoopCall struct {
	userMessage    string
	conversationID string
}

func (s *stubACPLoop) RunOnce(_ context.Context, userMessage, conversationID string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, stubLoopCall{userMessage: userMessage, conversationID: conversationID})
	if s.err != nil {
		return "", s.err
	}
	return s.reply, nil
}

func (s *stubACPLoop) lastCall() (stubLoopCall, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.calls) == 0 {
		return stubLoopCall{}, false
	}
	return s.calls[len(s.calls)-1], true
}

// stubSpecSource is an acpSpecSource over a plain map.
type stubSpecSource struct {
	specs map[string]*agent.AgentSpec
}

func (s stubSpecSource) GetSpec(id string) (*agent.AgentSpec, bool) {
	spec, ok := s.specs[id]
	return spec, ok
}

// recording default executor: proves the non-acp path is untouched.
type sentinelExecutor struct{ called int }

func (s *sentinelExecutor) ExecuteBot(context.Context, string, string) (string, int, error) {
	s.called++
	return "local", 7, nil
}

func bufLogger(t *testing.T, buf *bytes.Buffer) *slog.Logger {
	t.Helper()
	return slog.New(slog.NewTextHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug}))
}

func acpSpec(mode string) *agent.AgentSpec {
	return &agent.AgentSpec{
		ID:       "bob",
		Executor: "acp",
		ACP:      &agent.ACPSpec{Agent: "hermes", PermissionMode: mode},
	}
}

func acpEmployee(tier employee.AutonomyTier) employee.Employee {
	emp := employee.Employee{
		BotDefinition: bot.BotDefinition{
			ID:            "alice",
			Prompt:        "work",
			Enabled:       true,
			ExecutorAgent: "bob",
		},
	}
	emp.Constitution = employee.Constitution{
		Purpose:      "test",
		Role:         "tester",
		Charter:      "test things",
		AutonomyTier: tier,
		EscalatesTo:  []string{"role:user"},
		Version:      1,
	}
	return emp
}

// --- adapter routing tests ---

func TestACPEmployeeExecutorAdapter_RoutesToReferencedLoop(t *testing.T) {
	t.Parallel()
	loop := &stubACPLoop{reply: "harness says done"}
	resolve := func(agentID string) (acpTurnRunner, error) {
		if agentID != "bob" {
			return nil, fmt.Errorf("unexpected agent %q", agentID)
		}
		return loop, nil
	}
	a := &acpEmployeeExecutorAdapter{
		employeeID:  "alice",
		agentID:     "bob",
		resolveLoop: resolve,
		logger:      slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
	}

	// systemPrompt must be ignored (D2: unused-by-design); the userMessage
	// rides RunOnce. A non-empty systemPrompt that leaks would show up in
	// no recorded field, so assert the call carries exactly the userMessage.
	out, tokens, err := a.ExecuteBot(context.Background(), "IGNORED SYSTEM PROMPT", "do the thing")
	if err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
	if out != "harness says done" {
		t.Errorf("output = %q, want harness reply", out)
	}
	if tokens != 0 {
		t.Errorf("tokens = %d, want 0 (no cost signal on the acp wire, D4-A)", tokens)
	}
	call, ok := loop.lastCall()
	if !ok {
		t.Fatal("referenced loop was never called")
	}
	if call.userMessage != "do the thing" {
		t.Errorf("userMessage = %q, want %q", call.userMessage, "do the thing")
	}
	if !strings.HasPrefix(call.conversationID, "bot-exec") {
		t.Errorf("conversation id %q should be bot-exec-scoped like the local adapter", call.conversationID)
	}
}

func TestACPEmployeeExecutorAdapter_ResolverErrors(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	t.Run("nil resolver", func(t *testing.T) {
		a := &acpEmployeeExecutorAdapter{employeeID: "alice", agentID: "bob", logger: logger}
		_, tokens, err := a.ExecuteBot(context.Background(), "", "x")
		if err == nil || tokens != 0 {
			t.Fatalf("expected error with 0 tokens, got (%v, %d)", err, tokens)
		}
	})

	t.Run("resolve failure passes through", func(t *testing.T) {
		want := errors.New("agent spec not found: bob")
		a := &acpEmployeeExecutorAdapter{
			employeeID: "alice", agentID: "bob", logger: logger,
			resolveLoop: func(string) (acpTurnRunner, error) { return nil, want },
		}
		_, _, err := a.ExecuteBot(context.Background(), "", "x")
		if !errors.Is(err, want) {
			t.Fatalf("err = %v, want wrapped %v", err, want)
		}
	})

	t.Run("loop error returns (0, err) and logs", func(t *testing.T) {
		loop := &stubACPLoop{err: errors.New("harness blew up")}
		a := &acpEmployeeExecutorAdapter{
			employeeID: "alice", agentID: "bob", logger: logger,
			resolveLoop: func(string) (acpTurnRunner, error) { return loop, nil },
		}
		out, tokens, err := a.ExecuteBot(context.Background(), "", "x")
		if err == nil || out != "" || tokens != 0 {
			t.Fatalf("got (%q, %d, %v), want (\"\", 0, err)", out, tokens, err)
		}
	})
}

func TestACPEmployeeLoopResolver_NilRegistry(t *testing.T) {
	t.Parallel()
	resolve := acpEmployeeLoopResolver(nil)
	loop, err := resolve("bob")
	if err == nil || loop != nil {
		t.Fatalf("nil registry must fail loudly, got (loop=%v, err=%v)", loop, err)
	}
}

// --- wiring-helper guard tests (D1/D3/D4 + byte-identical path) ---

func TestEmployeeBotExecutor_NonACPEmployeeUntouched(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	logger := bufLogger(t, &buf)
	sentinel := &sentinelExecutor{}
	resolveCalled := false
	resolve := func(string) (acpTurnRunner, error) { resolveCalled = true; return nil, nil }

	emp := employee.Employee{BotDefinition: bot.BotDefinition{ID: "plain", Prompt: "p"}}
	emp.Constitution = employee.Constitution{Purpose: "x", AutonomyTier: employee.Tier1Reactive}

	// A BROKEN registry would be irrelevant: the empty-ExecutorAgent path
	// must not read it at all (byte-identical path for non-acp employees).
	src := stubSpecSource{specs: map[string]*agent.AgentSpec{}}
	got, refuse := employeeBotExecutor(emp, sentinel, src, "permissive", resolve, logger)
	if refuse {
		t.Fatal("non-acp employee must never be refused")
	}
	if got == nil || got.(bot.BotExecutor) != sentinel {
		t.Error("non-acp employee must receive the default executor unchanged")
	}
	if resolveCalled {
		t.Error("resolver must not be consulted for a non-acp employee")
	}
	if buf.Len() != 0 {
		t.Errorf("non-acp path must log nothing, got: %s", buf.String())
	}
}

func TestEmployeeBotExecutor_D1Validation(t *testing.T) {
	t.Parallel()

	t.Run("unknown target refused with error", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		src := stubSpecSource{specs: map[string]*agent.AgentSpec{}}
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier2Propose), nil, src, "decision", dummyResolver(), logger)
		if !refuse || got != nil {
			t.Fatalf("unknown agent must refuse: got=%v refuse=%v", got, refuse)
		}
		if !strings.Contains(buf.String(), "level=ERROR") || !strings.Contains(buf.String(), "unknown agent") {
			t.Errorf("expected ERROR naming unknown agent, got: %s", buf.String())
		}
	})

	t.Run("non-acp target refused", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		src := stubSpecSource{specs: map[string]*agent.AgentSpec{
			"bob": {ID: "bob", Executor: "local"},
		}}
		_, refuse := employeeBotExecutor(acpEmployee(employee.Tier2Propose), nil, src, "decision", dummyResolver(), logger)
		if !refuse {
			t.Fatal("non-acp target must refuse")
		}
		msg := buf.String()
		if !strings.Contains(msg, "level=ERROR") || !strings.Contains(msg, "not acp-backed") {
			t.Errorf("expected 'not acp-backed' ERROR, got: %s", msg)
		}
	})

	t.Run("acp executor without acp block refused", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		src := stubSpecSource{specs: map[string]*agent.AgentSpec{
			"bob": {ID: "bob", Executor: "acp"},
		}}
		_, refuse := employeeBotExecutor(acpEmployee(employee.Tier2Propose), nil, src, "decision", dummyResolver(), logger)
		if !refuse {
			t.Fatal("executor: acp with nil acp block must refuse")
		}
	})

	t.Run("nil registry refused", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		_, refuse := employeeBotExecutor(acpEmployee(employee.Tier2Propose), nil, nil, "decision", dummyResolver(), logger)
		if !refuse {
			t.Fatal("executor_agent with no registry must refuse (cannot validate)")
		}
	})
}

func TestEmployeeBotExecutor_D3TierPermissionGuard(t *testing.T) {
	t.Parallel()
	src := stubSpecSource{specs: map[string]*agent.AgentSpec{"bob": acpSpec("")}}

	t.Run("tier3 + agent override permissive refused", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		s := stubSpecSource{specs: map[string]*agent.AgentSpec{"bob": acpSpec("permissive")}}
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier3Autonomous), nil, s, "human", dummyResolver(), logger)
		if !refuse || got != nil {
			t.Fatalf("tier-3 permissive must refuse: got=%v refuse=%v", got, refuse)
		}
		msg := buf.String()
		for _, want := range []string{"level=ERROR", "alice", "bob", "tier_3_autonomous", "permissive"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error log missing %q, got: %s", want, msg)
			}
		}
	})

	t.Run("tier3 + global permissive (agent empty) refused", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		_, refuse := employeeBotExecutor(acpEmployee(employee.Tier3Autonomous), nil, src, "permissive", dummyResolver(), logger)
		if !refuse {
			t.Fatal("effective mode falls back to global permissive — must refuse")
		}
	})

	t.Run("tier3 + human mode passes", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier3Autonomous), nil, src, "human", dummyResolver(), logger)
		if refuse || got == nil {
			t.Fatalf("tier-3 human must wire: got=%v refuse=%v", got, refuse)
		}
	})

	t.Run("tier2 + permissive warns and wires", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier2Propose), nil, src, "permissive", dummyResolver(), logger)
		if refuse || got == nil {
			t.Fatalf("tier-2 permissive must warn-and-wire: got=%v refuse=%v", got, refuse)
		}
		msg := buf.String()
		if !strings.Contains(msg, "level=WARN") || !strings.Contains(msg, "permissive") {
			t.Errorf("expected WARN naming permissive, got: %s", msg)
		}
	})

	t.Run("tier1 + permissive warns and wires", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier1Reactive), nil, src, "permissive", dummyResolver(), logger)
		if refuse || got == nil {
			t.Fatalf("tier-1 permissive must warn-and-wire: got=%v refuse=%v", got, refuse)
		}
		if !strings.Contains(buf.String(), "level=WARN") {
			t.Errorf("expected WARN, got: %s", buf.String())
		}
	})
}

func TestEmployeeBotExecutor_D4CostWarnings(t *testing.T) {
	t.Parallel()
	src := stubSpecSource{specs: map[string]*agent.AgentSpec{"bob": acpSpec("decision")}}

	t.Run("bot constraints daily budget warns", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		emp := acpEmployee(employee.Tier2Propose)
		emp.Constraints.DailyBudgetCents = 50
		got, refuse := employeeBotExecutor(emp, nil, src, "", dummyResolver(), logger)
		if refuse || got == nil {
			t.Fatalf("budget warning must not refuse: got=%v refuse=%v", got, refuse)
		}
		msg := buf.String()
		if !strings.Contains(msg, "level=WARN") || !strings.Contains(msg, "daily_budget_cents") {
			t.Errorf("expected daily_budget_cents WARN, got: %s", msg)
		}
	})

	t.Run("constitution daily budget warns", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		emp := acpEmployee(employee.Tier2Propose)
		emp.Constitution.Constraints.DailyBudgetCents = 25
		if _, refuse := employeeBotExecutor(emp, nil, src, "", dummyResolver(), logger); refuse {
			t.Fatal("must not refuse")
		} else if !strings.Contains(buf.String(), "daily_budget_cents") {
			t.Errorf("expected constitution budget WARN, got: %s", buf.String())
		}
	})

	t.Run("cost escalation trigger warns", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		emp := acpEmployee(employee.Tier2Propose)
		emp.Constitution.Constraints.EscalationTriggers = []employee.EscalationTrigger{
			{On: employee.EscalateOnCost, Match: "50", Reason: "budget"},
			{On: employee.EscalateOnTool, Match: "shell_execute", Reason: "signoff"},
		}
		if _, refuse := employeeBotExecutor(emp, nil, src, "", dummyResolver(), logger); refuse {
			t.Fatal("must not refuse")
		}
		msg := buf.String()
		if !strings.Contains(msg, "cost escalation trigger") || !strings.Contains(msg, "level=WARN") {
			t.Errorf("expected cost-trigger WARN, got: %s", msg)
		}
	})

	t.Run("healthy acp employee logs nothing", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier3Autonomous), nil, src, "decision", dummyResolver(), logger)
		if refuse || got == nil {
			t.Fatalf("tier-3 decision acp employee must wire: got=%v refuse=%v", got, refuse)
		}
		if buf.Len() != 0 {
			t.Errorf("clean acp wiring should log nothing, got: %s", buf.String())
		}
	})
}

func dummyResolver() func(string) (acpTurnRunner, error) {
	return func(string) (acpTurnRunner, error) { return &stubACPLoop{reply: "ok"}, nil }
}

// TestEmployeeBotExecutor_ACPEmployeeGetsAdapter checks the swap itself:
// a healthy reference produces the acp adapter (not the default executor),
// and the adapter routes turns to the resolver's loop.
func TestEmployeeBotExecutor_ACPEmployeeGetsAdapter(t *testing.T) {
	t.Parallel()
	src := stubSpecSource{specs: map[string]*agent.AgentSpec{"bob": acpSpec("human")}}
	loop := &stubACPLoop{reply: "routed"}
	resolve := func(agentID string) (acpTurnRunner, error) {
		if agentID != "bob" {
			return nil, fmt.Errorf("resolver got %q, want bob", agentID)
		}
		return loop, nil
	}
	defaultEx := &sentinelExecutor{}
	got, refuse := employeeBotExecutor(acpEmployee(employee.Tier2Propose), defaultEx, src, "", resolve,
		slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)))
	if refuse {
		t.Fatal("healthy reference must not refuse")
	}
	if _, ok := got.(*acpEmployeeExecutorAdapter); !ok {
		t.Fatalf("executor = %T, want *acpEmployeeExecutorAdapter", got)
	}
	out, tokens, err := got.ExecuteBot(context.Background(), "sys", "turn")
	if err != nil || out != "routed" || tokens != 0 {
		t.Fatalf("ExecuteBot = (%q, %d, %v)", out, tokens, err)
	}
	if defaultEx.called != 0 {
		t.Error("default executor must not be invoked for an acp employee")
	}
}
