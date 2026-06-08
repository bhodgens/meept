package bot

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func testCostTracker(t *testing.T) (*CostTracker, *Manager) {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "bots.db"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	t.Cleanup(func() { store.Close() })
	mgr := NewManager(store, nil)
	ct := NewCostTracker(store, nil)
	return ct, mgr
}

func TestRecordExecution_IncrementCounters(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("cost-bot")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	if err := mgr.CreateBot(ctx, def); err != nil {
		t.Fatalf("CreateBot: %v", err)
	}

	result := &BotExecutionResult{
		BotID:      "cost-bot",
		TokensUsed: 150,
		Success:    true,
		Duration:   time.Second,
	}
	cost := ExecutionCost{TokensUsed: 150, CostCents: 3}

	if err := ct.RecordExecution(ctx, "cost-bot", result, cost); err != nil {
		t.Fatalf("RecordExecution: %v", err)
	}

	state, err := mgr.GetBotStatus(ctx, "cost-bot")
	if err != nil {
		t.Fatalf("GetBotStatus: %v", err)
	}

	if state.TotalRuns != 1 {
		t.Errorf("TotalRuns = %d, want 1", state.TotalRuns)
	}
	if state.TotalTokensUsed != 150 {
		t.Errorf("TotalTokensUsed = %d, want 150", state.TotalTokensUsed)
	}
	if state.TotalCostCents != 3 {
		t.Errorf("TotalCostCents = %d, want 3", state.TotalCostCents)
	}
	if state.TodayRuns != 1 {
		t.Errorf("TodayRuns = %d, want 1", state.TodayRuns)
	}
	if state.TodayCostCents != 3 {
		t.Errorf("TodayCostCents = %d, want 3", state.TodayCostCents)
	}
	if state.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures = %d, want 0", state.ConsecutiveFailures)
	}
	if state.LastError != "" {
		t.Errorf("LastError = %q, want empty", state.LastError)
	}
	if state.LastRunAt == nil {
		t.Fatal("LastRunAt is nil, want non-nil")
	}
}

func TestRecordExecution_Accumulates(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("accum-bot")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	exec1 := &BotExecutionResult{BotID: "accum-bot", TokensUsed: 100, Success: true, Duration: time.Second}
	exec2 := &BotExecutionResult{BotID: "accum-bot", TokensUsed: 200, Success: true, Duration: time.Second}

	ct.RecordExecution(ctx, "accum-bot", exec1, ExecutionCost{TokensUsed: 100, CostCents: 2})
	ct.RecordExecution(ctx, "accum-bot", exec2, ExecutionCost{TokensUsed: 200, CostCents: 4})

	state, _ := mgr.GetBotStatus(ctx, "accum-bot")

	if state.TotalRuns != 2 {
		t.Errorf("TotalRuns = %d, want 2", state.TotalRuns)
	}
	if state.TotalTokensUsed != 300 {
		t.Errorf("TotalTokensUsed = %d, want 300", state.TotalTokensUsed)
	}
	if state.TotalCostCents != 6 {
		t.Errorf("TotalCostCents = %d, want 6", state.TotalCostCents)
	}
	if state.TodayCostCents != 6 {
		t.Errorf("TodayCostCents = %d, want 6", state.TodayCostCents)
	}
}

func TestRecordExecution_FailureTracking(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("fail-bot")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	failResult := &BotExecutionResult{
		BotID:  "fail-bot",
		Success: false,
		Error:   "LLM timeout",
		Duration: time.Second,
	}
	ct.RecordExecution(ctx, "fail-bot", failResult, ExecutionCost{CostCents: 1})

	state, _ := mgr.GetBotStatus(ctx, "fail-bot")

	if state.ConsecutiveFailures != 1 {
		t.Errorf("ConsecutiveFailures = %d, want 1", state.ConsecutiveFailures)
	}
	if state.LastError != "LLM timeout" {
		t.Errorf("LastError = %q, want %q", state.LastError, "LLM timeout")
	}

	// Successful run resets consecutive failures.
	okResult := &BotExecutionResult{BotID: "fail-bot", Success: true, Duration: time.Second}
	ct.RecordExecution(ctx, "fail-bot", okResult, ExecutionCost{CostCents: 0})

	state, _ = mgr.GetBotStatus(ctx, "fail-bot")
	if state.ConsecutiveFailures != 0 {
		t.Errorf("ConsecutiveFailures after success = %d, want 0", state.ConsecutiveFailures)
	}
	if state.LastError != "" {
		t.Errorf("LastError after success = %q, want empty", state.LastError)
	}
}

func TestRecordExecution_DailyRollover(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("rollover-bot")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	yesterday := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	today := time.Now().Format("2006-01-02")

	// Set state with yesterday's date and high costs.
	state := BotState{
		DefinitionID:   "rollover-bot",
		Status:         BotStatusRunning,
		TodayRuns:      50,
		TodayCostCents: 100,
		TodayDate:      yesterday,
		TotalRuns:      50,
		TotalCostCents: 100,
	}
	mgr.store.UpdateState(ctx, state)

	result := &BotExecutionResult{BotID: "rollover-bot", TokensUsed: 10, Success: true, Duration: time.Second}
	ct.RecordExecution(ctx, "rollover-bot", result, ExecutionCost{TokensUsed: 10, CostCents: 1})

	updated, _ := mgr.GetBotStatus(ctx, "rollover-bot")

	if updated.TodayDate != today {
		t.Errorf("TodayDate = %q, want %q", updated.TodayDate, today)
	}
	if updated.TodayRuns != 1 {
		t.Errorf("TodayRuns after rollover = %d, want 1", updated.TodayRuns)
	}
	if updated.TodayCostCents != 1 {
		t.Errorf("TodayCostCents after rollover = %d, want 1", updated.TodayCostCents)
	}
	// Totals should keep accumulating.
	if updated.TotalRuns != 51 {
		t.Errorf("TotalRuns = %d, want 51", updated.TotalRuns)
	}
	if updated.TotalCostCents != 101 {
		t.Errorf("TotalCostCents = %d, want 101", updated.TotalCostCents)
	}
}

func TestIsBudgetExhausted_UnderBudget(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("budget-ok")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.Constraints = BotConstraints{DailyBudgetCents: 100}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	state := BotState{
		DefinitionID:   "budget-ok",
		Status:         BotStatusRunning,
		TodayCostCents: 50,
		TodayDate:      time.Now().Format("2006-01-02"),
	}
	mgr.store.UpdateState(ctx, state)

	exhausted, err := ct.IsBudgetExhausted(ctx, "budget-ok")
	if err != nil {
		t.Fatalf("IsBudgetExhausted: %v", err)
	}
	if exhausted {
		t.Error("expected budget NOT exhausted, got exhausted")
	}
}

func TestIsBudgetExhausted_AtBudget(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("budget-at")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.Constraints = BotConstraints{DailyBudgetCents: 100}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	state := BotState{
		DefinitionID:   "budget-at",
		Status:         BotStatusRunning,
		TodayCostCents: 100,
		TodayDate:      time.Now().Format("2006-01-02"),
	}
	mgr.store.UpdateState(ctx, state)

	exhausted, err := ct.IsBudgetExhausted(ctx, "budget-at")
	if err != nil {
		t.Fatalf("IsBudgetExhausted: %v", err)
	}
	if !exhausted {
		t.Error("expected budget exhausted, got not exhausted")
	}
}

func TestIsBudgetExhausted_NoBudgetSet(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("no-budget")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.Constraints = BotConstraints{} // no daily budget
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	state := BotState{
		DefinitionID:   "no-budget",
		Status:         BotStatusRunning,
		TodayCostCents: 99999,
		TodayDate:      time.Now().Format("2006-01-02"),
	}
	mgr.store.UpdateState(ctx, state)

	exhausted, err := ct.IsBudgetExhausted(ctx, "no-budget")
	if err != nil {
		t.Fatalf("IsBudgetExhausted: %v", err)
	}
	if exhausted {
		t.Error("expected budget NOT exhausted (no budget configured), got exhausted")
	}
}

func TestIsBudgetExhausted_StaleDate(t *testing.T) {
	ctx := context.Background()
	ct, mgr := testCostTracker(t)

	def := testBotDef("stale-date")
	def.Triggers = []BotTrigger{{Type: TriggerTypeWebhook, Enabled: true}}
	def.Constraints = BotConstraints{DailyBudgetCents: 10}
	def.CreatedAt = time.Now().UTC().Truncate(time.Second)
	def.UpdatedAt = time.Now().UTC().Truncate(time.Second)
	mgr.CreateBot(ctx, def)

	state := BotState{
		DefinitionID:   "stale-date",
		Status:         BotStatusRunning,
		TodayCostCents: 100,
		TodayDate:      time.Now().AddDate(0, 0, -1).Format("2006-01-02"),
	}
	mgr.store.UpdateState(ctx, state)

	exhausted, err := ct.IsBudgetExhausted(ctx, "stale-date")
	if err != nil {
		t.Fatalf("IsBudgetExhausted: %v", err)
	}
	if exhausted {
		t.Error("expected budget NOT exhausted (stale date), got exhausted")
	}
}

func TestIsBudgetExhausted_BotNotFound(t *testing.T) {
	ctx := context.Background()
	ct, _ := testCostTracker(t)

	_, err := ct.IsBudgetExhausted(ctx, "nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent bot, got nil")
	}
}
