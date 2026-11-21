// Pins for usability-remediation fixes A/B/C:
//
//	A — Manager.Trigger on an un-constitutioned plain bot fails loudly with
//	    ErrEmployeeNoConstitution instead of the legacy synthesized
//	    "triggered" receipt (no execution happened; no fake success metric).
//	B — synthesizeConservativeConstitution copies the bot definition's
//	    DailyBudgetCents into the synthesized constitution, and the LIVE
//	    budget path (PreExecChecker + store-backed BudgetChecker) sees it.
//	C — GoalLoop.Execute wraps executor.ExecuteBot in the bounded retry
//	    (DefaultRetryPolicy semantics from the retired bot runner).
package employee

import (
	"context"
	"database/sql"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/metrics"
)

// ---------------------------------------------------------------------------
// (A) constitution-required bot triggers
// ---------------------------------------------------------------------------

// newPlainBotTestManager builds a Manager over a shared temp SQLite DB with
// ONE enabled bot definition and NO constitution + NO GoalLoop registered —
// exactly the shape boot wiring skips (internal/daemon/components.go
// HasConstitution continue). This is the plain-bot row a bus trigger hits.
func newPlainBotTestManager(t *testing.T, botID string) *Manager {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "plainbot.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })

	m := NewManagerWithStores(bot.NewManager(botStore, nil), botStore, nil, nil, nil, slog.New(slog.DiscardHandler))
	if err := botStore.Create(context.Background(), bot.BotDefinition{
		ID:      botID,
		Name:    botID,
		Prompt:  "plain bot without a constitution",
		Enabled: true,
	}); err != nil {
		t.Fatalf("seed plain bot: %v", err)
	}
	return m
}

// TestTrigger_PlainBot_NoConstitution_ErrorsLoudly pins remediation (A): a
// trigger on an enabled, un-constitutioned plain bot must NOT fabricate a
// TriggerResult{Status:"triggered"} — it must fail with
// ErrEmployeeNoConstitution and emit NO success metric.
func TestTrigger_PlainBot_NoConstitution_ErrorsLoudly(t *testing.T) {
	const botID = "plain-bot-a"
	m := newPlainBotTestManager(t, botID)

	// Real metrics store over a temp DB: Manager.emitMetric writes through
	// it, and Close() performs the final batch flush, so the emitted
	// outcomes are queryable afterwards.
	metricsPath := filepath.Join(t.TempDir(), "metrics.db")
	mstore, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  metricsPath,
		BatchSize:     1,
		FlushInterval: time.Minute,
		RetentionDays: 1,
	})
	if err != nil {
		t.Fatalf("metrics store: %v", err)
	}
	m.SetMetricsStore(mstore)

	result, err := m.Trigger(context.Background(), botID, map[string]any{"source": "bus_event"})
	if err == nil {
		_ = mstore.Close()
		t.Fatalf("Trigger must fail on a plain bot (no constitution); got result %+v", result)
	}
	if !errors.Is(err, ErrEmployeeNoConstitution) {
		_ = mstore.Close()
		t.Fatalf("Trigger error = %v, want errors.Is ErrEmployeeNoConstitution", err)
	}
	if result != nil {
		_ = mstore.Close()
		t.Errorf("Trigger must return a nil TriggerResult on the loud error, got %+v", result)
	}
	// The error text names the repair paths.
	msg := err.Error()
	if !strings.Contains(msg, botID) || !strings.Contains(msg, "no constitution") ||
		!strings.Contains(msg, "agents migrate") {
		t.Errorf("error %q should name the employee, the missing constitution, and agents migrate", msg)
	}

	// No fake success: the recorded invocations outcomes must contain no
	// "success" row (the legacy fallback emitted outcome=success for a
	// no-op) and must carry the loud "error" outcome.
	if cerr := mstore.Close(); cerr != nil {
		t.Fatalf("close metrics store: %v", cerr)
	}
	db, err := sql.Open("sqlite", "file:"+metricsPath+"?mode=ro")
	if err != nil {
		t.Fatalf("reopen metrics: %v", err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT tags FROM metrics_live WHERE metric_name = 'employee.invocations'`)
	if err != nil {
		t.Fatalf("query metrics: %v", err)
	}
	defer rows.Close()
	var outcomes []string
	for rows.Next() {
		var tags string
		if err := rows.Scan(&tags); err != nil {
			t.Fatalf("scan tags: %v", err)
		}
		outcomes = append(outcomes, tags)
	}
	if len(outcomes) == 0 {
		t.Fatal("expected the failed trigger to record an employee.invocations metric")
	}
	var sawError, sawSuccess bool
	for _, tags := range outcomes {
		if strings.Contains(tags, `"outcome":"success"`) {
			sawSuccess = true
		}
		if strings.Contains(tags, `"outcome":"error"`) {
			sawError = true
		}
	}
	if sawSuccess {
		t.Error("legacy synthesized fallback emitted a fake success metric")
	}
	if !sawError {
		t.Errorf("invocations metric outcomes = %v, want an error outcome (failure visible to telemetry)", outcomes)
	}
}

// TestTrigger_MissingEmployee_StillNotFound pins that the loud
// no-constitution error did not swallow the not-found contract.
func TestTrigger_MissingEmployee_StillNotFound(t *testing.T) {
	m := newPlainBotTestManager(t, "plain-bot-exists")
	_, err := m.Trigger(context.Background(), "no-such-bot", nil)
	if !errors.Is(err, ErrEmployeeNotFound) {
		t.Fatalf("Trigger on missing id = %v, want ErrEmployeeNotFound", err)
	}
}

// ---------------------------------------------------------------------------
// (B) constraints propagation into the synthesized constitution
// ---------------------------------------------------------------------------

// TestSynthesizeConservativeConstitution_PropagatesBudget pins remediation
// (B): a def with DailyBudgetCents=X synthesizes a constitution whose
// Constraints carry X, MaxInvocationsPerDay propagates, and zero-value
// fields keep the DefaultConservativeConstitution defaults (no clobber).
func TestSynthesizeConservativeConstitution_PropagatesBudget(t *testing.T) {
	def := bot.BotDefinition{
		ID:          "budget-bot",
		Name:        "Budget Bot",
		Description: "Sends digests. Never spends money.",
		Prompt:      "You are a digest bot.",
		Constraints: bot.BotConstraints{
			DailyBudgetCents:     4200,
			MaxInvocationsPerDay: 17,
		},
	}

	c := synthesizeConservativeConstitution(def)

	if c.Constraints.DailyBudgetCents != 4200 {
		t.Errorf("DailyBudgetCents = %d, want 4200 copied from the bot definition", c.Constraints.DailyBudgetCents)
	}
	if c.Constraints.MaxInvocationsPerDay != 17 {
		t.Errorf("MaxInvocationsPerDay = %d, want 17 copied from the bot definition", c.Constraints.MaxInvocationsPerDay)
	}

	// Defaults are not clobbered for fields BotDefinition lacks: the
	// conservative never-rule, tier, risk ceiling, and escalation target
	// survive untouched.
	base := DefaultConservativeConstitution("x")
	if len(c.Constraints.Never) != len(base.Constraints.Never) || c.Constraints.Never[0] != base.Constraints.Never[0] {
		t.Errorf("Never defaults clobbered: %v vs %v", c.Constraints.Never, base.Constraints.Never)
	}
	if c.AutonomyTier != Tier1Reactive || c.Constraints.RiskCeiling != RiskCeilingLow {
		t.Errorf("conservative defaults changed: tier=%v ceiling=%v", c.AutonomyTier, c.Constraints.RiskCeiling)
	}
	if c.Charter != def.Prompt {
		t.Errorf("Charter = %q, want the bot prompt verbatim", c.Charter)
	}

	// A def with NO declared constraints keeps the zero ("no limit")
	// defaults byte-identically.
	bare := synthesizeConservativeConstitution(bot.BotDefinition{ID: "bare", Prompt: "p"})
	if bare.Constraints.DailyBudgetCents != 0 || bare.Constraints.MaxInvocationsPerDay != 0 || bare.Constraints.MaxTokensPerTurn != 0 {
		t.Errorf("bare def must keep zero/unlimited defaults, got %+v", bare.Constraints)
	}

	// The synthesized constitution must still validate — the propagated
	// numbers cannot make ApplyMigration's persist path fail.
	if err := c.Validate(def.ID); err != nil {
		t.Fatalf("synthesized constitution failed validation: %v", err)
	}
}

// TestSynthesizeConservativeConstitution_BudgetPathSeesIt pins the second
// half of (B): the LIVE budget enforcement (PreExecChecker step 5, reading
// constitution.Constraints) actually fires against the propagated
// DailyBudgetCents value — end to end through the production
// botStoreBudgetChecker over a real bot store, the same one the daemon
// wires (internal/daemon/components.go).
func TestSynthesizeConservativeConstitution_BudgetPathSeesIt(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "budget_pin.db")
	botStore, err := bot.NewStore(dbPath)
	if err != nil {
		t.Fatalf("bot store: %v", err)
	}
	t.Cleanup(func() { botStore.Close() })

	const botID = "budget-live-bot"
	ctx := context.Background()
	if err := botStore.Create(ctx, bot.BotDefinition{ID: botID, Name: botID, Prompt: "p", Enabled: true}); err != nil {
		t.Fatalf("seed bot: %v", err)
	}
	// Seed today's spend to exactly the propagated budget.
	if err := botStore.UpdateState(ctx, bot.BotState{
		DefinitionID:   botID,
		TodayDate:      time.Now().Format("2006-01-02"),
		TodayCostCents: 500,
	}); err != nil {
		t.Fatalf("seed state: %v", err)
	}

	def := bot.BotDefinition{
		ID:          botID,
		Description: "Digests.",
		Prompt:      "You are a digest bot.",
		Constraints: bot.BotConstraints{DailyBudgetCents: 500},
	}
	c := synthesizeConservativeConstitution(def)

	checker := NewBotStoreBudgetChecker(botStore, slog.New(slog.DiscardHandler))
	p := NewPreExecChecker(botID, &c, WithBudgetChecker(checker))

	dec := p.Check("send_digest", "message_send", map[string]string{})
	if dec.Allowed {
		t.Fatal("live PreExecChecker must deny once the propagated daily budget (500c) is spent — the value never reached the enforcement path")
	}
	if !strings.Contains(dec.Reason, "daily budget") {
		t.Fatalf("refusal reason = %q, want the daily-budget enforcement to be the cause", dec.Reason)
	}

	// Under budget: allowed (the pin proves the gate keys on the
	// propagated value, not on any constant).
	if err := botStore.UpdateState(ctx, bot.BotState{
		DefinitionID:   botID,
		TodayDate:      time.Now().Format("2006-01-02"),
		TodayCostCents: 499,
	}); err != nil {
		t.Fatalf("reseed state: %v", err)
	}
	if dec2 := p.Check("send_digest", "message_send", map[string]string{}); !dec2.Allowed {
		t.Fatalf("spend 499c under a 500c budget must be allowed, got %q", dec2.Reason)
	}
}

// ---------------------------------------------------------------------------
// (C) bounded per-round retry on GoalLoop.Execute
// ---------------------------------------------------------------------------

// flakyExecutor fails the first n calls, then succeeds.
type flakyExecutor struct {
	failuresLeft atomic.Int32
	calls        atomic.Int32
	err          error
}

func (f *flakyExecutor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	f.calls.Add(1)
	if f.failuresLeft.Add(-1) >= 0 {
		return "", 0, f.err
	}
	return "recovered", 7, nil
}

// alwaysFailExecutor fails every call.
type alwaysFailExecutor struct {
	calls atomic.Int32
	err   error
}

func (a *alwaysFailExecutor) ExecuteBot(_ context.Context, _, _ string) (string, int, error) {
	a.calls.Add(1)
	return "", 0, a.err
}

// TestGoalLoop_Execute_TransientFailureRetriesThenSucceeds pins remediation
// (C): with the DEFAULT policy (2 retries), an executor that fails once
// then succeeds must yield a successful round after 2 attempts — the
// transient failure never reaches the failure counter (Reflect-side
// feeding is pinned by the success result: a retry-recovered round is
// byte-identical to a first-try success for everything downstream).
func TestGoalLoop_Execute_TransientFailureRetriesThenSucceeds(t *testing.T) {
	exec := &flakyExecutor{err: errors.New("transient network blip")}
	exec.failuresLeft.Store(1) // fail attempt 1 only

	loop := NewGoalLoop("emp-retry", testTier1Constitution(), nil, nil).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 2, RetryBackoff: time.Millisecond})

	result, err := loop.Execute(context.Background(), PlanRef{ID: "p-retry"})
	if err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if !result.Success {
		t.Fatalf("retry-recovered round must report success, got %+v", result)
	}
	if result.Output != "recovered" {
		t.Errorf("output = %q, want the successful attempt's output", result.Output)
	}
	if got := exec.calls.Load(); got != 2 {
		t.Errorf("executor calls = %d, want 2 (1 failure + 1 retry success)", got)
	}
}

// TestGoalLoop_Execute_PersistentFailureBurnsRetriesOnce pins the failure
// half: a persistent executor error burns exactly DefaultMaxRetries+1
// attempts, records ONE failed result (one failure for Reflect to feed),
// and carries the executor error text.
func TestGoalLoop_Execute_PersistentFailureBurnsRetriesOnce(t *testing.T) {
	exec := &alwaysFailExecutor{err: errors.New("boom persistent")}

	// Default policy comes from NewGoalLoop: 2 retries => 3 attempts max.
	loop := NewGoalLoop("emp-retry-persist", testTier1Constitution(), nil, nil).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 2, RetryBackoff: time.Millisecond})

	result, err := loop.Execute(context.Background(), PlanRef{ID: "p-retry-persist"})
	if err != nil {
		t.Fatalf("Execute must not return an error (the failure is inside the result): %v", err)
	}
	if result.Success {
		t.Fatal("persistent failure must report a failed result")
	}
	if result.Error == "" || !strings.Contains(result.Error, "boom persistent") {
		t.Errorf("result.Error = %q, want the executor error text", result.Error)
	}
	if got := exec.calls.Load(); got != 3 {
		t.Errorf("executor calls = %d, want 3 (1 initial + 2 retries burned once)", got)
	}
	// Exactly one failure feeds ConsecutiveFailures (via the single
	// failed result; a per-attempt feed would have made it 3).
	if loop.ConsecutiveFailures() != 0 {
		t.Errorf("Execute itself must not touch the counter (Reflect feeds it); got %d", loop.ConsecutiveFailures())
	}
	// The result is the sole failure receipt — reflect it once.
	if _, hErr := loop.Reflect(context.Background(), PlanRef{ID: "p-retry-persist"}, result); hErr != nil {
		t.Fatalf("Reflect: %v", hErr)
	}
	if cf := loop.ConsecutiveFailures(); cf != 1 {
		t.Errorf("consecutiveFailures after one retried-to-exhaustion round = %d, want 1 (retries burn once, not per-attempt)", cf)
	}
}

// TestGoalLoop_Execute_CtxCancelIsNonRetriable pins the cancellation gate:
// once ctx.Err() is set, no further attempt is made even though retries
// remain.
func TestGoalLoop_Execute_CtxCancelIsNonRetriable(t *testing.T) {
	exec := &alwaysFailExecutor{err: context.Canceled}

	loop := NewGoalLoop("emp-retry-cancel", testTier1Constitution(), nil, nil).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{MaxRetries: 5, RetryBackoff: time.Millisecond})

	ctx, cancel := context.WithCancel(context.Background())
	// The executor fails with a ctx-shaped error; cancel before Execute so
	// ctx.Err() != nil short-circuits any retry.
	cancel()

	result, err := loop.Execute(ctx, PlanRef{ID: "p-cancel"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Success {
		t.Fatal("cancelled round must fail")
	}
	if got := exec.calls.Load(); got != 1 {
		t.Errorf("executor calls = %d, want 1 (cancellation is non-retriable)", got)
	}
}

// TestGoalLoop_Execute_ZeroPolicyDisablesRetry pins the opt-out lever:
// WithRetryPolicy(RetryPolicy{}) restores the single-attempt behavior.
func TestGoalLoop_Execute_ZeroPolicyDisablesRetry(t *testing.T) {
	exec := &alwaysFailExecutor{err: errors.New("single shot")}

	loop := NewGoalLoop("emp-retry-off", testTier1Constitution(), nil, nil).
		WithExecutor(exec).
		WithRetryPolicy(RetryPolicy{})

	result, err := loop.Execute(context.Background(), PlanRef{ID: "p-off"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Success {
		t.Fatal("failure must report a failed result")
	}
	if got := exec.calls.Load(); got != 1 {
		t.Errorf("executor calls = %d, want 1 (retry disabled by zero policy)", got)
	}
}

// TestNewGoalLoop_DefaultRetryPolicy pins that the production default
// carries the retired runner's semantics without any opt-in.
func TestNewGoalLoop_DefaultRetryPolicy(t *testing.T) {
	loop := NewGoalLoop("emp-default-retry", testTier1Constitution(), nil, nil)
	loop.mu.Lock()
	p := loop.retryPolicy
	loop.mu.Unlock()
	if p.MaxRetries != DefaultRetryPolicy.MaxRetries || p.RetryBackoff != DefaultRetryPolicy.RetryBackoff {
		t.Errorf("default retry policy = %+v, want %+v (old bot runner semantics)", p, DefaultRetryPolicy)
	}
	if DefaultRetryPolicy.MaxRetries != 2 || DefaultRetryPolicy.RetryBackoff != time.Second {
		t.Errorf("DefaultRetryPolicy drifted from the runner values {2, 1s}: %+v", DefaultRetryPolicy)
	}
}
