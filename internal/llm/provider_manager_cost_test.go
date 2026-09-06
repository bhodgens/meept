package llm

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"
)

// countingChatter is a minimal Chatter stub whose Chat returns a fixed
// response and records cost into the budget handle exactly like the real
// client layer does (client.go/codex.go/anthropic.go each RecordCost their
// own request via the budget threaded in at construction).
type countingChatter struct {
	mu       sync.Mutex
	calls    int
	budget   *Budget
	cost     float64
	response *Response
}

func (c *countingChatter) Chat(_ context.Context, _ []ChatMessage, _ ...ChatOption) (*Response, error) {
	c.mu.Lock()
	c.calls++
	c.mu.Unlock()
	// Client-layer cost recording (the seam ProviderManager must not duplicate).
	if c.budget != nil && c.cost > 0 {
		c.budget.RecordCost(CostRecord{
			Timestamp: time.Now(),
			CostUSD:   c.cost,
		})
	}
	return c.response, nil
}

func (c *countingChatter) ChatWithProgress(ctx context.Context, msgs []ChatMessage, p ProgressCallback, opts ...ChatOption) (*Response, error) {
	return c.Chat(ctx, msgs, opts...)
}

func (c *countingChatter) Config() *ModelConfig {
	return &ModelConfig{ProviderID: "stub", ModelID: "stub-model"}
}

// TestProviderManager_NoDoubleCostRecording pins the cost-ownership split:
// the UNDERLYING client records cost into the shared Budget (client.go,
// codex.go, anthropic.go all do via their own budget handles, threaded
// through createChatterFor's WithBudget), so ProviderManager.recordSuccess
// must NOT RecordCost the same request again. Pre-fix, every PM-routed
// request was counted twice against budget enforcement.
func TestProviderManager_NoDoubleCostRecording(t *testing.T) {
	budget := NewBudget(BudgetConfig{
		DailyCostLimit: 100.0,
	}, nil)

	expected := 10000*3.0/1_000_000 + 5000*15.0/1_000_000 // $0.105
	chatter := &countingChatter{
		budget: budget,
		cost:   expected, // the client layer records exactly once per request
		response: &Response{
			Usage: TokenUsage{PromptTokens: 10000, CompletionTokens: 5000, TotalTokens: 15000},
		},
	}

	entry := &ProviderEntry{
		Config: &ModelConfig{
			ProviderID:           "stub",
			ModelID:              "stub-model",
			CostPerMillionInput:  3.0,
			CostPerMillionOutput: 15.0,
		},
		Chatter: chatter,
		Health: &ProviderHealth{
			ProviderID: "stub",
			Status:     ProviderStatusHealthy,
		},
	}

	pm := &ProviderManager{
		config: ProviderManagerConfig{
			Budget: budget,
			Logger: slog.Default(),
		},
		providers: []*ProviderEntry{entry},
	}

	// One request flows through the manager: the stub's Chat (client layer)
	// records cost, then recordSuccess runs. Budget must show exactly ONE
	// record; the provider health ledger tracks its own copy for telemetry.
	chatter.Chat(context.Background(), nil)
	pm.recordSuccess(entry, chatter.response, 100*time.Millisecond)

	status := budget.GetStatus()
	if status.DailyCostUsed > expected+0.0001 {
		t.Fatalf("PM double-recorded cost: DailyCostUsed=%.6f, want <= %.6f (client layer owns cost)",
			status.DailyCostUsed, expected)
	}
	if status.DailyCostUsed < expected-0.0001 {
		t.Fatalf("no cost recorded at all: DailyCostUsed=%.6f, want %.6f", status.DailyCostUsed, expected)
	}
	if entry.Health.TotalCost < expected-0.0001 {
		t.Errorf("entry TotalCost = %.6f, want ~%.6f (provider telemetry keeps its own ledger)",
			entry.Health.TotalCost, expected)
	}
}
