# Persistent Per-Model Cost Tracking Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make LLM cost tracking persistent (survive daemon restarts), dollar-denominated, and dynamically synced from providers that expose pricing APIs.

**Architecture:** Extend the existing `llm/metrics.Store` SQLite schema with a `cost_usd` column on `provider_requests` and a new `model_cost_daily` summary table. Extend `Budget` to track dollar cost alongside tokens. Add a `PricingSyncer` that fetches live pricing from OpenRouter and Together AI, falling back to the hardcoded catalog.

**Tech Stack:** Go 1.22+, SQLite (existing `pkg/sqlite.Pool`), `net/http` for provider API calls

---

## File Structure

| File | Action | Responsibility |
|------|--------|---------------|
| `internal/llm/metrics/store.go` | Modify | Add `cost_usd` column, `model_cost_daily` table, cost query methods |
| `internal/llm/metrics/store_test.go` | Modify | Tests for new schema and cost queries |
| `internal/llm/budget.go` | Modify | Add dollar cost tracking fields and methods |
| `internal/llm/budget_test.go` | Modify | Tests for dollar budget enforcement |
| `internal/llm/models.go` | Modify | Add `CostUSD` field to `TokenUsage` |
| `internal/llm/provider_manager.go` | Modify | Compute cost at request time, pass to metrics store |
| `internal/llm/pricing_sync.go` | Create | `PricingSyncer` — fetch live pricing from OpenRouter/Together, fall back to catalog |
| `internal/llm/pricing_sync_test.go` | Create | Tests for pricing sync |
| `internal/config/schema.go` | Modify | Add `DailyCostLimit` and `HourlyCostLimit` to `BudgetConfig` |
| `internal/daemon/components.go` | Modify | Wire dollar limits into budget, wire PricingSyncer |
| `config/meept.json5` | Modify | Add cost limit config fields with comments |

---

### Task 1: Add `cost_usd` to `RequestRecord` and SQLite schema

**Files:**
- Modify: `internal/llm/metrics/store.go:31-44,132-176,217-233`
- Modify: `internal/llm/metrics/store_test.go`

- [ ] **Step 1: Write the failing test**

Add a test in `internal/llm/metrics/store_test.go` that creates a `RequestRecord` with `CostUSD`, stores it, and queries it back.

```go
func TestStore_RecordWithCost(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")

	store, err := metrics.NewStore(metrics.StoreConfig{
		DBPath:        dbPath,
		RetentionDays: 7,
	})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	ctx := context.Background()
	if err := store.Initialize(ctx); err != nil {
		t.Fatal(err)
	}

	record := metrics.RequestRecord{
		Timestamp:        time.Now(),
		ProviderID:       "anthropic",
		ModelID:          "claude-sonnet-4-6",
		PromptTokens:     1000,
		CompletionTokens: 500,
		CachedTokens:     200,
		CostUSD:          0.0105, // 1000*$3/M + 500*$15/M = $0.003 + $0.0075
		LatencyMs:        1200,
		Success:          true,
	}

	if err := store.Record(ctx, record); err != nil {
		t.Fatal(err)
	}

	// Allow async worker to process
	time.Sleep(100 * time.Millisecond)

	// Verify the cost was stored by querying daily costs
	costs, err := store.GetDailyCosts(ctx, time.Now().Add(-24*time.Hour), time.Now())
	if err != nil {
		t.Fatal(err)
	}

	if len(costs) != 1 {
		t.Fatalf("expected 1 cost entry, got %d", len(costs))
	}
	if costs[0].ProviderID != "anthropic" {
		t.Errorf("expected provider anthropic, got %s", costs[0].ProviderID)
	}
	if costs[0].ModelID != "claude-sonnet-4-6" {
		t.Errorf("expected model claude-sonnet-4-6, got %s", costs[0].ModelID)
	}

	const tolerance = 0.0001
	if math.Abs(costs[0].TotalCost-record.CostUSD) > tolerance {
		t.Errorf("expected cost ~%.4f, got %.4f", record.CostUSD, costs[0].TotalCost)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/metrics/... -run TestStore_RecordWithCost -v`
Expected: FAIL — `CostUSD` field does not exist on `RequestRecord`, `GetDailyCosts` does not exist.

- [ ] **Step 3: Add `CostUSD` to `RequestRecord`**

In `internal/llm/metrics/store.go`, add the field:

```go
type RequestRecord struct {
	Timestamp        time.Time
	ProviderID       string
	ModelID          string
	PromptTokens     int
	CompletionTokens int
	CachedTokens     int
	CostUSD          float64   // Computed dollar cost for this request
	LatencyMs        int64
	TTFBMs           int64
	HTTPStatus       int
	ErrorType        ErrorType
	ErrorMessage     string
	Success          bool
}
```

- [ ] **Step 4: Update schema and INSERT to include `cost_usd`**

In `Initialize()`, add the migration after the existing `cached_tokens` migration:

```go
// Idempotent migrations for existing databases
db.ExecContext(ctx, "ALTER TABLE provider_requests ADD COLUMN cached_tokens INTEGER NOT NULL DEFAULT 0")
db.ExecContext(ctx, "ALTER TABLE provider_requests ADD COLUMN cost_usd REAL NOT NULL DEFAULT 0.0")
```

Add a new table to the schema string:

```sql
CREATE TABLE IF NOT EXISTS model_cost_daily (
    date         TEXT NOT NULL,
    provider_id  TEXT NOT NULL,
    model_id     TEXT NOT NULL,
    total_cost   REAL NOT NULL DEFAULT 0.0,
    total_prompt_tokens   INTEGER NOT NULL DEFAULT 0,
    total_completion_tokens INTEGER NOT NULL DEFAULT 0,
    request_count INTEGER NOT NULL DEFAULT 0,
    updated_at   INTEGER NOT NULL,
    PRIMARY KEY (date, provider_id, model_id)
);

CREATE INDEX IF NOT EXISTS idx_mcd_date ON model_cost_daily(date);
```

Update `recordSync()` to include `cost_usd`:

```go
const q = `
INSERT INTO provider_requests (ts, provider_id, model_id, prompt_tokens, completion_tokens, cached_tokens, cost_usd, latency_ms, ttfb_ms, http_status, error_type, error_message, success)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
`
// ... in ExecContext, add r.CostUSD between cached_tokens and latency_ms args:
_, err := db.ExecContext(ctx, q, ts, r.ProviderID, r.ModelID, r.PromptTokens, r.CompletionTokens, r.CachedTokens, r.CostUSD, r.LatencyMs, r.TTFBMs, r.HTTPStatus, string(r.ErrorType), r.ErrorMessage, success)
```

Also update `model_cost_daily` in `recordSync()` via UPSERT:

```go
// After inserting into provider_requests, upsert into model_cost_daily
if r.Success && r.CostUSD > 0 {
	dateStr := r.Timestamp.UTC().Format("2006-01-02")
	const upsertDaily = `
INSERT INTO model_cost_daily (date, provider_id, model_id, total_cost, total_prompt_tokens, total_completion_tokens, request_count, updated_at)
VALUES (?, ?, ?, ?, ?, ?, 1, ?)
ON CONFLICT(date, provider_id, model_id) DO UPDATE SET
    total_cost = total_cost + excluded.total_cost,
    total_prompt_tokens = total_prompt_tokens + excluded.total_prompt_tokens,
    total_completion_tokens = total_completion_tokens + excluded.total_completion_tokens,
    request_count = request_count + 1,
    updated_at = excluded.updated_at
`
	_, err := db.ExecContext(ctx, upsertDaily, dateStr, r.ProviderID, r.ModelID, r.CostUSD, r.PromptTokens, r.CompletionTokens, time.Now().UnixMilli())
	if err != nil {
		s.logger.Debug("failed to upsert daily cost", "error", err)
	}
}
```

- [ ] **Step 5: Add `DailyCostEntry` struct and `GetDailyCosts` query method**

```go
// DailyCostEntry holds aggregated cost for a provider/model on a given date.
type DailyCostEntry struct {
	Date                  string
	ProviderID            string
	ModelID               string
	TotalCost             float64
	TotalPromptTokens     int64
	TotalCompletionTokens int64
	RequestCount          int64
}

// GetDailyCosts returns cost entries within a date range.
func (s *Store) GetDailyCosts(ctx context.Context, from, to time.Time) ([]DailyCostEntry, error) {
	var entries []DailyCostEntry
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		fromStr := from.UTC().Format("2006-01-02")
		toStr := to.UTC().Format("2006-01-02")
		const q = `
SELECT date, provider_id, model_id, total_cost, total_prompt_tokens, total_completion_tokens, request_count
FROM model_cost_daily
WHERE date >= ? AND date <= ?
ORDER BY date DESC, total_cost DESC
`
		rows, err := db.QueryContext(ctx, q, fromStr, toStr)
		if err != nil {
			return err
		}
		defer rows.Close()

		for rows.Next() {
			var e DailyCostEntry
			if err := rows.Scan(&e.Date, &e.ProviderID, &e.ModelID, &e.TotalCost, &e.TotalPromptTokens, &e.TotalCompletionTokens, &e.RequestCount); err != nil {
				return err
			}
			entries = append(entries, e)
		}
		return rows.Err()
	})
	return entries, err
}
```

Also add `GetTotalCost` for a quick aggregate:

```go
// GetTotalCost returns the total dollar cost across all providers within a date range.
func (s *Store) GetTotalCost(ctx context.Context, from, to time.Time) (float64, error) {
	var total float64
	err := s.pool.WithConn(ctx, func(db *sql.DB) error {
		fromStr := from.UTC().Format("2006-01-02")
		toStr := to.UTC().Format("2006-01-02")
		const q = `SELECT COALESCE(SUM(total_cost), 0) FROM model_cost_daily WHERE date >= ? AND date <= ?`
		row := db.QueryRowContext(ctx, q, fromStr, toStr)
		return row.Scan(&total)
	})
	return total, err
}
```

- [ ] **Step 6: Run test to verify it passes**

Run: `go test ./internal/llm/metrics/... -run TestStore_RecordWithCost -v`
Expected: PASS

- [ ] **Step 7: Run all existing metrics tests to verify no regressions**

Run: `go test ./internal/llm/metrics/... -v`
Expected: All PASS

- [ ] **Step 8: Commit**

```bash
git add internal/llm/metrics/store.go internal/llm/metrics/store_test.go
git commit -m "feat(llm/metrics): add cost_usd column and model_cost_daily table for persistent cost tracking"
```

---

### Task 2: Extend Budget with dollar-denominated cost tracking

**Files:**
- Modify: `internal/llm/budget.go`
- Modify: `internal/llm/budget_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestBudget_DollarTracking(t *testing.T) {
	b := NewBudget(BudgetConfig{
		DailyCostLimit: 1.00, // $1/day
	}, slog.Default())

	// Record a $0.50 request
	b.RecordCost(CostRecord{
		Timestamp:        time.Now(),
		CostUSD:          0.50,
		PromptTokens:     10000,
		CompletionTokens: 5000,
	})

	status := b.GetStatus()
	if status.DailyCostUsed != 0.50 {
		t.Errorf("expected daily cost used 0.50, got %.4f", status.DailyCostUsed)
	}
	if !status.WithinCostBudget {
		t.Error("expected within cost budget")
	}

	// Record another $0.60 request — should exceed $1/day
	b.RecordCost(CostRecord{
		Timestamp:        time.Now(),
		CostUSD:          0.60,
		PromptTokens:     12000,
		CompletionTokens: 6000,
	})

	status = b.GetStatus()
	if status.WithinCostBudget {
		t.Error("expected cost budget to be exceeded (0.50 + 0.60 > 1.00)")
	}
}

func TestBudget_DollarCheckBudget(t *testing.T) {
	b := NewBudget(BudgetConfig{
		DailyCostLimit: 0.50,
	}, slog.Default())

	if !b.CheckBudget() {
		t.Error("expected budget to be available initially")
	}

	b.RecordCost(CostRecord{
		Timestamp: time.Now(),
		CostUSD:   0.60,
	})

	if b.CheckBudget() {
		t.Error("expected budget exceeded after $0.60 on $0.50 limit")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/... -run TestBudget_Dollar -v`
Expected: FAIL — `CostRecord` type and `RecordCost` method don't exist.

- [ ] **Step 3: Add `CostRecord` type and dollar fields to Budget**

In `internal/llm/budget.go`, add the new type after `usageRecord`:

```go
// costRecord is a timestamped dollar cost entry.
type costRecord struct {
	timestamp time.Time
	costUSD   float64
}
```

Add dollar tracking fields to `Budget` struct (after `dailyUsed`):

```go
// Dollar cost tracking
dailyCostLimit float64
hourlyCostLimit float64

// Hourly cost sliding window
hourlyCostWindow []costRecord

// Daily cost tracking — reset at midnight UTC
dailyCostUsed float64
```

Add to `BudgetConfig`:

```go
type BudgetConfig struct {
	HourlyLimit      int
	DailyLimit       int
	DailyCostLimit   float64 // Max dollar cost per UTC day (0 = no limit)
	HourlyCostLimit  float64 // Max dollar cost per sliding hour (0 = no limit)
	RateLimitRPM     int
	Aggressiveness   float64
	PerTaskBudget    int
	PerSessionBudget int
}
```

Add to `Status` struct:

```go
DailyCostUsed      float64 `json:"daily_cost_used"`
DailyCostLimit     float64 `json:"daily_cost_limit"`
DailyCostRemaining float64 `json:"daily_cost_remaining"`
HourlyCostUsed     float64 `json:"hourly_cost_used"`
HourlyCostLimit    float64 `json:"hourly_cost_limit"`
WithinCostBudget   bool    `json:"within_cost_budget"`
```

- [ ] **Step 4: Add `RecordCost` method and cost-aware budget checking**

```go
// CostRecord captures a dollar cost event for budget tracking.
type CostRecord struct {
	Timestamp        time.Time
	CostUSD          float64
	PromptTokens     int
	CompletionTokens int
}

// RecordCost records a dollar cost against the budget.
func (b *Budget) RecordCost(r CostRecord) {
	if r.CostUSD <= 0 {
		return
	}

	b.mu.Lock()
	defer b.mu.Unlock()

	b.maybeResetDaily()
	b.maybeResetDailyCost()

	b.hourlyCostWindow = append(b.hourlyCostWindow, costRecord{
		timestamp: r.Timestamp,
		costUSD:   r.CostUSD,
	})
	b.dailyCostUsed += r.CostUSD

	b.logger.Debug("Recorded dollar cost",
		"cost_usd", r.CostUSD,
		"hourly_cost", b.hourlyCostUsed(),
		"daily_cost", b.dailyCostUsed,
	)
}

// hourlyCostUsed returns total dollar cost in the current sliding hour.
func (b *Budget) hourlyCostUsed() float64 {
	cutoff := time.Now().Add(-time.Hour)
	total := 0.0
	for i := len(b.hourlyCostWindow) - 1; i >= 0; i-- {
		if b.hourlyCostWindow[i].timestamp.Before(cutoff) {
			break
		}
		total += b.hourlyCostWindow[i].costUSD
	}
	return total
}

// maybeResetDailyCost resets daily cost counter on day boundary.
func (b *Budget) maybeResetDailyCost() {
	today := dayOrdinal(time.Now().UTC())
	if today != b.currentDay {
		b.dailyCostUsed = 0
		b.hourlyCostWindow = b.hourlyCostWindow[:0]
	}
}
```

Update `CheckBudget()` to include cost limits — add after the existing token limit checks:

```go
// Cost limit checks
if b.dailyCostLimit > 0 {
	if b.dailyCostUsed >= b.effectiveCostLimit(b.dailyCostLimit) {
		return false
	}
}
if b.hourlyCostLimit > 0 {
	if b.hourlyCostUsed() >= b.effectiveCostLimit(b.hourlyCostLimit) {
		return false
	}
}
```

Add the helper:

```go
// effectiveCostLimit applies the aggressiveness factor to a dollar limit.
func (b *Budget) effectiveCostLimit(base float64) float64 {
	factor := 0.5 + 0.5*b.aggressiveness
	return base * factor
}
```

Update `GetStatus()` to populate the new cost fields:

```go
// After existing status computation:
effDailyCost := b.effectiveCostLimit(b.dailyCostLimit)
effHourlyCost := b.effectiveCostLimit(b.hourlyCostLimit)

// Add to returned Status:
DailyCostUsed:      b.dailyCostUsed,
DailyCostLimit:     effDailyCost,
DailyCostRemaining: max(effDailyCost-b.dailyCostUsed, 0),
HourlyCostUsed:     b.hourlyCostUsed(),
HourlyCostLimit:    effHourlyCost,
WithinCostBudget:   b.dailyCostUsed < effDailyCost && b.hourlyCostUsed() < effHourlyCost,
```

Update `NewBudget()` to initialize the new fields:

```go
return &Budget{
	// ... existing fields ...
	dailyCostLimit:    cfg.DailyCostLimit,
	hourlyCostLimit:   cfg.HourlyCostLimit,
	hourlyCostWindow:  make([]costRecord, 0),
	dailyCostUsed:     0,
}
```

Also update `maybeResetDaily()` to reset cost on day boundary:

```go
func (b *Budget) maybeResetDaily() {
	today := dayOrdinal(time.Now().UTC())
	if today != b.currentDay {
		b.logger.Info("Daily token budget reset (new UTC day)")
		b.dailyUsed = 0
		b.dailyCostUsed = 0
		b.hourlyCostWindow = b.hourlyCostWindow[:0]
		b.currentDay = today
	}
}
```

- [ ] **Step 5: Run test to verify it passes**

Run: `go test ./internal/llm/... -run TestBudget_Dollar -v`
Expected: PASS

- [ ] **Step 6: Run all existing budget tests**

Run: `go test ./internal/llm/... -run TestBudget -v`
Expected: All PASS (cost limits are 0 by default, so existing token-only tests are unaffected)

- [ ] **Step 7: Commit**

```bash
git add internal/llm/budget.go internal/llm/budget_test.go
git commit -m "feat(llm): add dollar-denominated cost tracking to Budget"
```

---

### Task 3: Wire cost computation into ProviderManager

**Files:**
- Modify: `internal/llm/provider_manager.go:307-349`
- Modify: `internal/llm/provider_manager.go:49-73`

- [ ] **Step 1: Write the failing test**

Add a test that verifies `ProviderManager` calls `Budget.RecordCost` with the correct dollar amount when a request succeeds.

```go
func TestProviderManager_RecordsDollarCost(t *testing.T) {
	budget := llm.NewBudget(llm.BudgetConfig{
		DailyCostLimit: 100.0,
	}, slog.Default())

	cfg := &llm.ModelConfig{
		BaseURL:              "http://localhost:1", // unreachable, but we use a mock
		ModelID:              "test-model",
		ProviderID:           "test",
		CostPerMillionInput:  3.0,
		CostPerMillionOutput: 15.0,
	}

	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{cfg},
		Budget:    budget,
	})
	defer pm.Stop()

	// Simulate recording a success with known token usage
	resp := &llm.Response{
		Usage: llm.TokenUsage{
			PromptTokens:     10000,
			CompletionTokens: 5000,
			TotalTokens:      15000,
		},
	}

	// Access the internal recordSuccess via reflection-free approach:
	// We test via budget state after Chat() with a mock Chatter
	entry := pm.GetPrimaryProvider()
	pm.recordSuccess(entry, resp, 100*time.Millisecond)

	status := budget.GetStatus()
	expectedCost := 10000*3.0/1_000_000 + 5000*15.0/1_000_000 // $0.03 + $0.075 = $0.105
	if math.Abs(status.DailyCostUsed-expectedCost) > 0.0001 {
		t.Errorf("expected daily cost used %.4f, got %.4f", expectedCost, status.DailyCostUsed)
	}
}
```

Note: since `recordSuccess` is unexported, this test must live in the `llm` package.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/... -run TestProviderManager_RecordsDollarCost -v`
Expected: FAIL — `DailyCostUsed` not being populated because `recordSuccess` doesn't call `RecordCost`.

- [ ] **Step 3: Add `RecordCost` call in `recordSuccess()`**

In `internal/llm/provider_manager.go`, update `recordSuccess()` — after the existing cost computation (lines 327-329), add:

```go
// Track dollar cost in budget
if pm.config.Budget != nil {
	pm.config.Budget.RecordCost(llm.CostRecord{
		Timestamp:        time.Now(),
		CostUSD:          cost,
		PromptTokens:     resp.Usage.PromptTokens,
		CompletionTokens: resp.Usage.CompletionTokens,
	})
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/... -run TestProviderManager_RecordsDollarCost -v`
Expected: PASS

- [ ] **Step 5: Run all provider manager tests**

Run: `go test ./internal/llm/... -v`
Expected: All PASS

- [ ] **Step 6: Commit**

```bash
git add internal/llm/provider_manager.go
git commit -m "feat(llm): wire dollar cost recording into ProviderManager.recordSuccess"
```

---

### Task 4: Wire cost into metrics store recording

**Files:**
- Modify: `internal/llm/provider_manager.go` (or the call site that records to metrics)

- [ ] **Step 1: Find where metrics store `Record()` is called**

Search for `metricsStore.Record` or `metrics.Record` in the codebase to find the call site where `RequestRecord` is created. This is where we need to compute and set `CostUSD`.

- [ ] **Step 2: Add cost computation at the metrics recording call site**

At the call site that creates a `RequestRecord`, compute the cost from the `ModelConfig` cost rates:

```go
costUSD := float64(usage.PromptTokens) * cfg.CostPerMillionInput / 1_000_000
costUSD += float64(usage.CompletionTokens) * cfg.CostPerMillionOutput / 1_000_000

record := metrics.RequestRecord{
	// ... existing fields ...
	CostUSD: costUSD,
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/llm/... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/llm/provider_manager.go
git commit -m "feat(llm): compute and persist cost_usd in metrics store records"
```

---

### Task 5: Create PricingSyncer for dynamic pricing

**Files:**
- Create: `internal/llm/pricing_sync.go`
- Create: `internal/llm/pricing_sync_test.go`

- [ ] **Step 1: Write the failing test**

```go
func TestPricingSyncer_FetchOpenRouter(t *testing.T) {
	// Use httptest server to mock OpenRouter API
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{
			"data": [
				{
					"id": "anthropic/claude-sonnet-4.6",
					"pricing": {
						"prompt": "0.000003",
						"completion": "0.000015"
					}
				},
				{
					"id": "openai/gpt-5.4",
					"pricing": {
						"prompt": "0.0000025",
						"completion": "0.00001"
					}
				}
			]
		}`)
	}))
	defer server.Close()

	syncer := llm.NewPricingSyncer(llm.PricingSyncerConfig{
		OpenRouterURL: server.URL + "/api/v1/models",
		Logger:        slog.Default(),
	})

	prices, err := syncer.FetchOpenRouter(context.Background())
	if err != nil {
		t.Fatal(err)
	}

	if len(prices) != 2 {
		t.Fatalf("expected 2 prices, got %d", len(prices))
	}

	// OpenRouter returns per-token pricing; we store per-million
	sonnetInput := prices["anthropic/claude-sonnet-4.6"]
	if sonnetInput == nil {
		t.Fatal("expected claude-sonnet-4.6 entry")
	}

	const tolerance = 0.01
	if math.Abs(sonnetInput.InputCost-3.0) > tolerance {
		t.Errorf("expected input cost ~3.0, got %.4f", sonnetInput.InputCost)
	}
	if math.Abs(sonnetInput.OutputCost-15.0) > tolerance {
		t.Errorf("expected output cost ~15.0, got %.4f", sonnetInput.OutputCost)
	}
}

func TestPricingSyncer_MergeWithCatalog(t *testing.T) {
	syncer := llm.NewPricingSyncer(llm.PricingSyncerConfig{
		Logger: slog.Default(),
	})

	// Simulate fetched prices (empty — provider down)
	fetched := map[string]*llm.LivePrice{}

	// Should fall back to catalog
	merged := syncer.MergeWithCatalog(fetched)
	if len(merged) == 0 {
		t.Error("expected catalog fallback to produce entries")
	}

	// Catalog should have anthropic models
	found := false
	for _, p := range merged {
		if p.ProviderID == "anthropic" {
			found = true
			break
		}
	}
	if !found {
		t.Error("expected anthropic entries from catalog fallback")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/llm/... -run TestPricingSyncer -v`
Expected: FAIL — `PricingSyncer`, `NewPricingSyncer`, `LivePrice` don't exist.

- [ ] **Step 3: Implement PricingSyncer**

Create `internal/llm/pricing_sync.go`:

```go
package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// LivePrice holds a dynamically fetched price for a model.
type LivePrice struct {
	ProviderID string
	ModelID    string
	InputCost  float64 // USD per million tokens
	OutputCost float64 // USD per million tokens
	Source     string  // "openrouter", "together", or "catalog"
	FetchedAt  time.Time
}

// PricingSyncerConfig configures the pricing syncer.
type PricingSyncerConfig struct {
	OpenRouterURL string        // URL for OpenRouter models endpoint
	TogetherURL   string        // URL for Together AI models endpoint
	SyncInterval  time.Duration // How often to re-sync (default: 6h)
	HTTPTimeout   time.Duration // Per-request timeout (default: 30s)
	Logger        *slog.Logger
}

// PricingSyncer fetches live model pricing from provider APIs.
type PricingSyncer struct {
	config PricingSyncerConfig
	client *http.Client
	mu     sync.RWMutex
	prices map[string]*LivePrice // key: "provider/model-id"
	logger *slog.Logger
}

// NewPricingSyncer creates a new pricing syncer.
func NewPricingSyncer(cfg PricingSyncerConfig) *PricingSyncer {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}
	if cfg.HTTPTimeout == 0 {
		cfg.HTTPTimeout = 30 * time.Second
	}
	if cfg.SyncInterval == 0 {
		cfg.SyncInterval = 6 * time.Hour
	}
	return &PricingSyncer{
		config: cfg,
		client: &http.Client{Timeout: cfg.HTTPTimeout},
		prices: make(map[string]*LivePrice),
		logger: cfg.Logger,
	}
}

// openrouterResponse models the OpenRouter /api/v1/models response.
type openrouterResponse struct {
	Data []struct {
		ID      string `json:"id"`
		Pricing struct {
			Prompt     string `json:"prompt"`
			Completion string `json:"completion"`
		} `json:"pricing"`
	} `json:"data"`
}

// FetchOpenRouter fetches pricing from OpenRouter's models endpoint.
// Returns a map keyed by model ID (e.g., "anthropic/claude-sonnet-4.6").
func (ps *PricingSyncer) FetchOpenRouter(ctx context.Context) (map[string]*LivePrice, error) {
	if ps.config.OpenRouterURL == "" {
		return nil, fmt.Errorf("openrouter URL not configured")
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ps.config.OpenRouterURL, nil)
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}

	resp, err := ps.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetching openrouter pricing: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	var orResp openrouterResponse
	if err := json.Unmarshal(body, &orResp); err != nil {
		return nil, fmt.Errorf("parsing response: %w", err)
	}

	prices := make(map[string]*LivePrice, len(orResp.Data))
	for _, m := range orResp.Data {
		promptPrice := parseFloat(m.Pricing.Prompt)
		completionPrice := parseFloat(m.Pricing.Completion)

		if promptPrice <= 0 && completionPrice <= 0 {
			continue
		}

		prices[m.ID] = &LivePrice{
			ModelID:    m.ID,
			InputCost:  promptPrice * 1_000_000, // per-token -> per-million
			OutputCost: completionPrice * 1_000_000,
			Source:     "openrouter",
			FetchedAt:  time.Now(),
		}
	}

	ps.logger.Info("Fetched pricing from OpenRouter", "models", len(prices))
	return prices, nil
}

// MergeWithCatalog merges fetched prices with the hardcoded catalog,
// preferring fetched prices and filling gaps from the catalog.
func (ps *PricingSyncer) MergeWithCatalog(fetched map[string]*LivePrice) []*LivePrice {
	result := make([]*LivePrice, 0)

	// Start with fetched prices
	for _, p := range fetched {
		result = append(result, p)
	}

	// Add catalog entries not already covered by fetched prices
	covered := make(map[string]bool)
	for k := range fetched {
		covered[k] = true
	}

	for providerID, models := range ProviderModels {
		for _, m := range models {
			key := providerID + "/" + m.ModelID
			if covered[key] || covered[m.ModelID] {
				continue
			}
			result = append(result, &LivePrice{
				ProviderID: providerID,
				ModelID:    m.ModelID,
				InputCost:  m.InputCost,
				OutputCost: m.OutputCost,
				Source:     "catalog",
				FetchedAt:  time.Now(),
			})
			covered[key] = true
		}
	}

	return result
}

// UpdatePrices updates the cached prices from fetched data.
func (ps *PricingSyncer) UpdatePrices(prices map[string]*LivePrice) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	for k, v := range prices {
		ps.prices[k] = v
	}
}

// GetPrice returns the cached price for a model, or nil if not found.
func (ps *PricingSyncer) GetPrice(modelKey string) *LivePrice {
	ps.mu.RLock()
	defer ps.mu.RUnlock()
	return ps.prices[modelKey]
}

// Sync fetches prices from all configured providers and updates the cache.
func (ps *PricingSyncer) Sync(ctx context.Context) error {
	allFetched := make(map[string]*LivePrice)

	if ps.config.OpenRouterURL != "" {
		prices, err := ps.FetchOpenRouter(ctx)
		if err != nil {
			ps.logger.Warn("Failed to fetch OpenRouter pricing", "error", err)
		} else {
			for k, v := range prices {
				allFetched[k] = v
			}
		}
	}

	// Future: add Together AI fetch here

	// Merge with catalog
	merged := ps.MergeWithCatalog(allFetched)

	// Update cache
	mergedMap := make(map[string]*LivePrice, len(merged))
	for _, p := range merged {
		key := p.ProviderID + "/" + p.ModelID
		mergedMap[key] = p
	}
	ps.UpdatePrices(mergedMap)

	ps.logger.Info("Pricing sync complete", "total_models", len(mergedMap))
	return nil
}

// StartPeriodicSync starts background sync at the configured interval.
// Returns a stop channel.
func (ps *PricingSyncer) StartPeriodicSync(ctx context.Context) chan struct{} {
	stop := make(chan struct{})

	// Initial sync
	go func() {
		if err := ps.Sync(ctx); err != nil {
			ps.logger.Warn("Initial pricing sync failed", "error", err)
		}
	}()

	go func() {
		ticker := time.NewTicker(ps.config.SyncInterval)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := ps.Sync(context.Background()); err != nil {
					ps.logger.Warn("Periodic pricing sync failed", "error", err)
				}
			case <-stop:
				return
			case <-ctx.Done():
				return
			}
		}
	}()

	return stop
}

// parseFloat safely parses a price string.
func parseFloat(s string) float64 {
	var f float64
	fmt.Sscanf(s, "%f", &f)
	return f
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/llm/... -run TestPricingSyncer -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/llm/pricing_sync.go internal/llm/pricing_sync_test.go
git commit -m "feat(llm): add PricingSyncer for dynamic pricing from OpenRouter/Together APIs"
```

---

### Task 6: Add cost limit config and wire into daemon

**Files:**
- Modify: `internal/config/schema.go:377-385`
- Modify: `internal/daemon/components.go:210-272`
- Modify: `config/meept.json5`

- [ ] **Step 1: Add cost limit fields to BudgetConfig**

In `internal/config/schema.go`, update `BudgetConfig`:

```go
type BudgetConfig struct {
	HourlyTokenLimit     int     `json:"hourly_token_limit" toml:"hourly_token_limit"`
	DailyTokenLimit      int     `json:"daily_token_limit"  toml:"daily_token_limit"`
	DailyCostLimit       float64 `json:"daily_cost_limit"   toml:"daily_cost_limit"`   // Max USD per UTC day (0 = no limit)
	HourlyCostLimit      float64 `json:"hourly_cost_limit"  toml:"hourly_cost_limit"`  // Max USD per sliding hour (0 = no limit)
	RateLimitRPM         int     `json:"rate_limit_rpm"     toml:"rate_limit_rpm"`
	Aggressiveness       float64 `json:"aggressiveness"     toml:"aggressiveness"`
	PerTaskTokenLimit    int     `json:"per_task_token_limit"  toml:"per_task_token_limit"`
	PerSessionTokenLimit int     `json:"per_session_token_limit" toml:"per_session_token_limit"`
}
```

- [ ] **Step 2: Update daemon wiring to pass cost limits**

In `internal/daemon/components.go`, update the budget creation:

```go
budgetTracker = llm.NewBudget(llm.BudgetConfig{
	HourlyLimit:      cfg.LLM.Budget.HourlyTokenLimit,
	DailyLimit:       cfg.LLM.Budget.DailyTokenLimit,
	DailyCostLimit:   cfg.LLM.Budget.DailyCostLimit,
	HourlyCostLimit:  cfg.LLM.Budget.HourlyCostLimit,
	RateLimitRPM:     cfg.LLM.Budget.RateLimitRPM,
	Aggressiveness:   cfg.LLM.Budget.Aggressiveness,
	PerTaskBudget:    cfg.LLM.Budget.PerTaskTokenLimit,
	PerSessionBudget: cfg.LLM.Budget.PerSessionTokenLimit,
}, logger.With("component", "budget"))
```

- [ ] **Step 3: Add config template entries**

In `config/meept.json5`, in the budget section, add:

```json5
budget: {
    hourly_token_limit: 500000,    // max tokens per sliding hour (0 = unlimited)
    daily_token_limit:  5000000,   // max tokens per UTC day (0 = unlimited)
    daily_cost_limit:   0,         // max USD per UTC day (0 = unlimited)
    hourly_cost_limit:  0,         // max USD per sliding hour (0 = unlimited)
    rate_limit_rpm:     0,         // requests per minute (0 = unlimited)
    aggressiveness:     0.5,       // 0.0 = conservative, 1.0 = use full budget
}
```

- [ ] **Step 4: Run all tests**

Run: `go test ./internal/config/... ./internal/daemon/... -v`
Expected: All PASS

- [ ] **Step 5: Commit**

```bash
git add internal/config/schema.go internal/daemon/components.go config/meept.json5
git commit -m "feat(config): add daily/hourly cost limit config for dollar-denominated budgets"
```

---

### Task 7: Wire PricingSyncer into daemon lifecycle

**Files:**
- Modify: `internal/daemon/components.go`

- [ ] **Step 1: Add PricingSyncer to daemon Components struct**

Find the `Components` struct in `internal/daemon/components.go` (or `daemon.go`) and add:

```go
PricingSyncer *llm.PricingSyncer
```

- [ ] **Step 2: Initialize PricingSyncer in daemon startup**

In the same section where the budget and LLM client are initialized, add:

```go
// Create pricing syncer for dynamic model pricing
pricingSyncer := llm.NewPricingSyncer(llm.PricingSyncerConfig{
	OpenRouterURL: "https://openrouter.ai/api/v1/models",
	SyncInterval:  6 * time.Hour,
	Logger:        logger.With("component", "pricing-sync"),
})
c.PricingSyncer = pricingSyncer
```

- [ ] **Step 3: Start periodic sync in daemon start lifecycle**

In the daemon start method (where other background workers are started), add:

```go
if c.PricingSyncer != nil {
	c.pricingSyncStop = c.PricingSyncer.StartPeriodicSync(ctx)
}
```

Ensure `pricingSyncStop` channel is closed in daemon shutdown.

- [ ] **Step 4: Run all tests**

Run: `go build ./... && go test ./internal/daemon/... -v`
Expected: Build succeeds, tests pass

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/components.go
git commit -m "feat(daemon): wire PricingSyncer into daemon lifecycle with periodic background sync"
```

---

### Task 8: Integrate live prices into model resolution

**Files:**
- Modify: `internal/llm/providers.go:188-229`

- [ ] **Step 1: Add PricingSyncer-aware cost enrichment to ResolveModelRef**

In `ResolveModelRef()`, after building the `ModelConfig`, check the PricingSyncer for live prices:

```go
// enrichCostFromSyncer updates model costs from live pricing data if available.
func enrichCostFromSyncer(cfg *ModelConfig, syncer *PricingSyncer) {
	if syncer == nil {
		return
	}
	key := cfg.ProviderID + "/" + cfg.ModelID
	if live := syncer.GetPrice(key); live != nil && live.InputCost > 0 {
		cfg.CostPerMillionInput = live.InputCost
		cfg.CostPerMillionOutput = live.OutputCost
	}
}
```

Call this at the end of `ResolveModelRef()` and `GetAllModels()`.

- [ ] **Step 2: Write test**

```go
func TestEnrichCostFromSyncer(t *testing.T) {
	syncer := llm.NewPricingSyncer(llm.PricingSyncerConfig{
		Logger: slog.Default(),
	})
	syncer.UpdatePrices(map[string]*llm.LivePrice{
		"test/test-model": {
			ProviderID: "test",
			ModelID:    "test-model",
			InputCost:  5.0,
			OutputCost: 25.0,
			Source:     "openrouter",
		},
	})

	cfg := &llm.ModelConfig{
		ProviderID:           "test",
		ModelID:              "test-model",
		CostPerMillionInput:  0.0,
		CostPerMillionOutput: 0.0,
	}

	llm.enrichCostFromSyncer(cfg, syncer)

	if cfg.CostPerMillionInput != 5.0 {
		t.Errorf("expected input cost 5.0, got %.2f", cfg.CostPerMillionInput)
	}
	if cfg.CostPerMillionOutput != 25.0 {
		t.Errorf("expected output cost 25.0, got %.2f", cfg.CostPerMillionOutput)
	}

	// Verify nil syncer is safe
	cfg2 := &llm.ModelConfig{
		ProviderID:           "test",
		ModelID:              "test-model",
		CostPerMillionInput:  1.0,
		CostPerMillionOutput: 2.0,
	}
	llm.enrichCostFromSyncer(cfg2, nil)
	if cfg2.CostPerMillionInput != 1.0 {
		t.Error("nil syncer should not modify costs")
	}
}
```

- [ ] **Step 3: Run all tests**

Run: `go test ./internal/llm/... -v`
Expected: All PASS

- [ ] **Step 4: Commit**

```bash
git add internal/llm/providers.go
git commit -m "feat(llm): enrich model costs from live pricing data during resolution"
```

---

### Task 9: Add cost reporting to CLI status command

**Files:**
- Modify: `cmd/meept/status.go` (or wherever status output is rendered)

- [ ] **Step 1: Find the status output handler**

Search for where `GetProviderStatus()` and `GetStatus()` output is formatted for the CLI.

- [ ] **Step 2: Add cost display to status output**

Add daily cost and cost budget information to the status display, e.g.:

```
budget:
  tokens: 12,450 / 5,000,000 daily
  cost:   $0.0342 / $1.00 daily
  hourly: $0.0120 / $0.50
```

- [ ] **Step 3: Test manually**

Run: `./bin/meept status`
Expected: Shows cost information alongside token information

- [ ] **Step 4: Commit**

```bash
git add cmd/meept/
git commit -m "feat(cli): display dollar cost in status command output"
```

---

## Self-Review

**1. Spec coverage:**
- Persistent cost per model: Task 1 (`model_cost_daily` table), Task 4 (wire into metrics recording)
- Dollar budget limits: Task 2 (Budget extension), Task 6 (config fields), Task 7 (daemon wiring)
- Dynamic pricing from providers: Task 5 (PricingSyncer), Task 8 (enrichment)
- Cost in CLI: Task 9
- ProviderHealth.TotalCost persistence: Covered by Task 1 + Task 4 (metrics store is the persistent ledger, ProviderHealth remains in-memory fast-path)

**2. Placeholder scan:**
- Task 4 Step 1 says "find where metrics store Record() is called" — this is a legitimate discovery step but should have a concrete path. The recording happens inside the LLM client (OpenAI and Anthropic chatter implementations) where `RequestRecord` is constructed. Will need to check `internal/llm/client.go` and `internal/llm/anthropic.go` for the exact call sites.
- Task 9 Step 1 says "find the status output handler" — similar discovery step. Should verify path.

**3. Type consistency:**
- `CostRecord` is defined in Task 2 in `budget.go` and used in Task 3 in `provider_manager.go` — both in package `llm`, consistent.
- `LivePrice` defined in Task 5, used in Task 8 — consistent.
- `RequestRecord.CostUSD float64` added in Task 1, set in Task 4 — consistent.
- `BudgetConfig.DailyCostLimit float64` in Task 2 config, wired in Task 6 — consistent.
