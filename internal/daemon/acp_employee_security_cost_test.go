package daemon

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/acp"
	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/employee"
)

// catalogModeResolver emulates (*acp.Manager).EffectivePermissionMode:
// an explicit call option (the agent-definition override) wins, else the
// operator-owned catalog entry, else the global. The tests below use a
// catalog entry of "permissive" while the global is "human" — the exact
// H5 bypass shape.
func catalogModeResolver(catalogMode string) permissionModeResolver {
	return func(_ string, callMode string) string {
		if callMode != "" {
			return callMode
		}
		return catalogMode
	}
}

// TestEmployeeBotExecutor_H5CatalogPermissiveDefeatsHumanGlobal pins the
// H5 bypass: with global=human and an empty agent override, an
// operator-owned catalog entry of "permissive" is the mode the session
// will ACTUALLY run under. The tier-3 D3 guard must refuse when it sees
// that effective mode; a tier-2 employee with the same shape is allowed
// with a warning (permissive is acceptable below tier-3). RED before the
// fix: effectiveACPPermissionMode read only spec+global, so both cases
// saw "human" and neither the refusal nor the warning fired.
func TestEmployeeBotExecutor_H5CatalogPermissiveDefeatsHumanGlobal(t *testing.T) {
	t.Parallel()
	src := stubSpecSource{specs: map[string]*agent.AgentSpec{"bob": acpSpec("")}}
	resolver := catalogModeResolver("permissive")

	t.Run("tier3 refuses catalog permissive under human global", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier3Autonomous), nil, src,
			"human", dummyResolver(), nil, logger, resolver)
		if !refuse || got != nil {
			t.Fatalf("tier-3 with catalog permissive must refuse: got=%v refuse=%v", got, refuse)
		}
		msg := buf.String()
		for _, want := range []string{"level=ERROR", "tier_3_autonomous", "permissive"} {
			if !strings.Contains(msg, want) {
				t.Errorf("error log missing %q, got: %s", want, msg)
			}
		}
	})

	t.Run("tier2 same shape warns and wires", func(t *testing.T) {
		var buf bytes.Buffer
		logger := bufLogger(t, &buf)
		got, refuse := employeeBotExecutor(acpEmployee(employee.Tier2Propose), nil, src,
			"human", dummyResolver(), nil, logger, resolver)
		if refuse || got == nil {
			t.Fatalf("tier-2 with catalog permissive must warn-and-wire: got=%v refuse=%v", got, refuse)
		}
		msg := buf.String()
		if !strings.Contains(msg, "level=WARN") || !strings.Contains(msg, "permissive") {
			t.Errorf("expected WARN naming permissive, got: %s", msg)
		}
	})
}

// ctxProbeLoop implements acpTurnRunner and records whether the ctx it
// received was stamped no-permissive; it can also report a cost through
// the ctx sink to exercise M2 end-to-end.
type ctxProbeLoop struct {
	mu               sync.Mutex
	reply            string
	costUSD          float64
	noPermissiveSeen bool
}

func (l *ctxProbeLoop) SetTurnSystemPrefix(string) {}

func (l *ctxProbeLoop) RunOnce(ctx context.Context, _, _ string) (string, error) {
	l.mu.Lock()
	l.noPermissiveSeen = acp.NoPermissiveFromContext(ctx)
	cost := l.costUSD
	l.mu.Unlock()
	// Simulate a harness that reports cost on its prompt response: acp.Session
	// forwards it through the ctx sink the adapter planted (M2, D4 decision C).
	acp.ReportTurnCost(ctx, cost)
	return l.reply, nil
}

func (l *ctxProbeLoop) wasNoPermissive() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.noPermissiveSeen
}

// TestACPEmployeeExecutorAdapter_Tier3StampsNoPermissive pins the runtime
// fail-closed backstop: a tier-3 adapter stamps WithNoPermissive on its
// turn ctx so the acp.Manager refuses a permissive session from ANY source
// (including the catalog entry the wiring guard cannot see). A tier-2
// adapter leaves the ctx unmarked.
func TestACPEmployeeExecutorAdapter_Tier3StampsNoPermissive(t *testing.T) {
	t.Parallel()
	logger := slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil))

	t.Run("tier3 stamps marker", func(t *testing.T) {
		loop := &ctxProbeLoop{reply: "ok"}
		a := &acpEmployeeExecutorAdapter{
			employeeID: "alice", agentID: "bob", logger: logger, noPermissive: true,
			resolveLoop: func(string) (acpTurnRunner, error) { return loop, nil },
		}
		if _, _, err := a.ExecuteBot(context.Background(), "", "work"); err != nil {
			t.Fatalf("ExecuteBot: %v", err)
		}
		if !loop.wasNoPermissive() {
			t.Error("tier-3 turn ctx must carry the no-permissive marker")
		}
	})

	t.Run("tier2 leaves marker unset", func(t *testing.T) {
		loop := &ctxProbeLoop{reply: "ok"}
		a := &acpEmployeeExecutorAdapter{
			employeeID: "alice", agentID: "bob", logger: logger,
			resolveLoop: func(string) (acpTurnRunner, error) { return loop, nil },
		}
		if _, _, err := a.ExecuteBot(context.Background(), "", "work"); err != nil {
			t.Fatalf("ExecuteBot: %v", err)
		}
		if loop.wasNoPermissive() {
			t.Error("tier-2 turn ctx must NOT carry the no-permissive marker")
		}
	})
}

// TestACPEmployeeExecutorAdapter_CostedResponseYieldsCents pins M2: a
// costed ACP response (cost forwarded through the ctx sink by the harness)
// yields a non-zero ExecutionCostCents so the daily-budget lane binds.
// RED before the fix: the ACP path bypassed reasoningCycle and never
// recorded cost, so ExecutionCostCents() was structurally always 0.
func TestACPEmployeeExecutorAdapter_CostedResponseYieldsCents(t *testing.T) {
	t.Parallel()
	loop := &ctxProbeLoop{reply: "done", costUSD: 0.0123}
	a := &acpEmployeeExecutorAdapter{
		employeeID: "alice", agentID: "bob",
		logger:      slog.New(slog.NewTextHandler(&bytes.Buffer{}, nil)),
		resolveLoop: func(string) (acpTurnRunner, error) { return loop, nil },
	}
	if _, _, err := a.ExecuteBot(context.Background(), "", "work"); err != nil {
		t.Fatalf("ExecuteBot: %v", err)
	}
	if got := a.ExecutionCostCents(); got != 1 {
		t.Errorf("ExecutionCostCents() = %d, want 1 (round(0.0123*100)); ACP cost was discarded", got)
	}
}
