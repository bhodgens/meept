package llm

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	appmetrics "github.com/caimlas/meept/internal/metrics"
)

// refusedChatServer returns an OpenAI-compatible test server whose response
// carries finish_reason "content_filter" plus the given token usage — a
// provider-reported refusal.
func refusedChatServer(t *testing.T, prompt, completion int) *httptest.Server {
	t.Helper()
	body := map[string]any{
		"id": "chatcmpl-refused", "object": "chat.completion", "model": "test-model",
		"choices": []map[string]any{
			{"index": 0,
				"message":       map[string]any{"role": "assistant", "content": ""},
				"finish_reason": "content_filter"},
		},
		"usage": map[string]any{
			"prompt_tokens": prompt, "completion_tokens": completion,
			"total_tokens": prompt + completion,
		},
	}
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(body)
	}))
}

// scopes-3 finding 1: a refused Chat call must charge its provider-reported
// usage to the scoped budget — the pre-fix early-exit branch ledgered
// llm_calls but skipped budget.RecordUsageWithScope entirely, so a refused
// call consumed tokens yet never billed them.
func TestChatRefusalChargesBudget(t *testing.T) {
	srv := refusedChatServer(t, 100, 40)
	defer srv.Close()

	budget := NewBudget(BudgetConfig{PerSessionBudget: 1_000_000}, discardLogger())
	c := NewClient(&ModelConfig{
		BaseURL:              srv.URL,
		ProviderID:           "prov-a",
		ModelID:              "test-model",
		CostPerMillionInput:  3.0,
		CostPerMillionOutput: 15.0,
		MaxTokens:            64,
	}, WithBudget(budget))

	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithTaskScope("task-1", "sess-1"))
	if resp != nil {
		t.Fatalf("refusal must return a nil response, got %+v", resp)
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error must satisfy errors.As for *RefusalError; got %T: %v", err, err)
	}

	// Budget usage: one record, the provider-reported usage, scoped to the
	// turn's session. (CheckBudgetWithScope only populates Used on the
	// exceeded branches, so read the per-session ledger directly.)
	budget.mu.Lock()
	got := budget.sessions["sess-1"]
	budget.mu.Unlock()
	if got != 140 {
		t.Errorf("session usage = %d, want 140 (100 prompt + 40 completion billed to the scoped budget)", got)
	}
}

// scopes-3 finding 1 (cost half): the refusal's cost must be priced at the
// SERVING model's rates and recorded with the same scope.
func TestChatRefusalRecordsCostWithScope(t *testing.T) {
	srv := refusedChatServer(t, 100, 40)
	defer srv.Close()

	budget := NewBudget(BudgetConfig{PerSessionCostLimit: 1.0}, discardLogger())
	c := NewClient(&ModelConfig{
		BaseURL:              srv.URL,
		ProviderID:           "prov-a",
		ModelID:              "test-model",
		CostPerMillionInput:  3.0,
		CostPerMillionOutput: 15.0,
		MaxTokens:            64,
	}, WithBudget(budget))

	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithTaskScope("task-1", "sess-1"))
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error must satisfy errors.As for *RefusalError; got %T: %v", err, err)
	}

	// 100*3/1M + 40*15/1M = 0.0009 USD must appear in the session's cost.
	budget.mu.Lock()
	got, ok := budget.sessionCosts["sess-1"]
	budget.mu.Unlock()
	if !ok {
		t.Fatalf("no cost record for sess-1: RecordCostWithScope never fired on the refusal path")
	}
	wantCost := 100*3.0/1_000_000 + 40*15.0/1_000_000
	if got < wantCost*0.999 || got > wantCost*1.001 {
		t.Errorf("session cost = %v, want ~%v (priced at serving model rates)", got, wantCost)
	}
}

// scopes-3 finding 2: the openai-client refusal llm_calls row must stamp the
// provider the call actually served under, never pairing the client's
// configured default provider with the refusal's model. The cross-provider
// resolution itself is pinned at unit level (refusalProviderOr) because a
// request-scoped override naming a foreign provider is deliberately NOT
// applied inside one client (requestModelOverride falls through; the
// ProviderManager routes it) — so end-to-end we verify the effective
// provider/model pair reaches the row under a same-provider override.
func TestChatRefusalLedgerCarriesServingProvider(t *testing.T) {
	// Unit: the refusal's own stamp wins when present...
	if got := refusalProviderOr("prov-a", &RefusalError{ProviderID: "prov-b", ModelID: "m-b"}); got != "prov-b" {
		t.Errorf("refusalProviderOr = %q, want prov-b (refusal stamp wins)", got)
	}
	// ...and the serving provider fills an empty refusal stamp.
	if got := refusalProviderOr("prov-a", &RefusalError{ModelID: "m-b"}); got != "prov-a" {
		t.Errorf("refusalProviderOr = %q, want prov-a (serving provider fallback)", got)
	}

	// End-to-end: refusal under a request-scoped same-provider override.
	// The ledger row must name the pair that SERVED the call
	// (prov-a/wire-model), not the configured default model.
	srv := refusedChatServer(t, 7, 2)
	defer srv.Close()

	store := newUsageTestStore(t)
	c := NewClient(&ModelConfig{
		BaseURL:    srv.URL,
		ProviderID: "prov-a",
		ModelID:    "configured-default",
		MaxTokens:  64,
	})
	c.SetUsageStore(store)

	_, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithModelOverride(&ModelConfig{ProviderID: "prov-a", ModelID: "wire-model"}))
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error must satisfy errors.As for *RefusalError; got %T: %v", err, err)
	}
	if refusal.ProviderID != "prov-a" || refusal.ModelID != "wire-model" {
		t.Fatalf("refusal stamps %q/%q, want prov-a/wire-model", refusal.ProviderID, refusal.ModelID)
	}

	// recordUsageStore writes async; poll briefly.
	deadline := time.Now().Add(3 * time.Second)
	var rows []appmetrics.LLMUsageRow
	for time.Now().Before(deadline) {
		rows, err = store.QueryLLMCallUsage(time.Now().Add(-time.Hour), time.Now().Add(time.Hour), false)
		if err == nil && len(rows) == 1 {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected exactly 1 llm_calls row, got %d (err=%v)", len(rows), err)
	}
	if rows[0].Provider != "prov-a" {
		t.Errorf("llm_calls provider = %q, want prov-a (the serving provider)", rows[0].Provider)
	}
	if rows[0].TokensSent != 7 || rows[0].TokensRecv != 2 {
		t.Errorf("llm_calls usage = %d/%d, want 7/2", rows[0].TokensSent, rows[0].TokensRecv)
	}
	if rows[0].Errors != 1 {
		t.Errorf("llm_calls errors = %d, want 1 (refusal row is an error row)", rows[0].Errors)
	}
}

// scopes-3 finding 1, anthropic half: a refused Anthropic Chat call must
// charge its usage to the scoped budget too.
func TestAnthropicChatRefusalChargesBudget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "msg_refused",
			"type": "message",
			"role": "assistant",
			"model": "claude-selected",
			"content": [],
			"stop_reason": "refusal",
			"usage": {"input_tokens": 11, "output_tokens": 3}
		}`))
	}))
	defer srv.Close()

	budget := NewBudget(BudgetConfig{PerSessionBudget: 1_000_000}, discardLogger())
	c := NewAnthropicClient(&ModelConfig{
		BaseURL:              srv.URL,
		ProviderID:           ProviderIDAnthropic,
		ModelID:              "claude-selected",
		APIKey:               "test-key",
		MaxTokens:            64,
		CostPerMillionInput:  3.0,
		CostPerMillionOutput: 15.0,
	}, WithAnthropicBudget(budget), WithAnthropicLogger(discardLogger()))

	resp, err := c.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}},
		WithTaskScope("task-a", "sess-a"))
	if resp != nil {
		t.Fatalf("refusal must return a nil response, got %+v", resp)
	}
	var refusal *RefusalError
	if !errors.As(err, &refusal) {
		t.Fatalf("error must satisfy errors.As for *RefusalError; got %T: %v", err, err)
	}

	// Budget usage: the refused call's usage lands in the session ledger
	// (CheckBudgetWithScope only populates Used on the exceeded branches).
	budget.mu.Lock()
	got := budget.sessions["sess-a"]
	budget.mu.Unlock()
	if got != 14 {
		t.Errorf("session usage = %d, want 14 (11 input + 3 output billed to the scoped budget)", got)
	}
}

// refusalCostUSD: nil config prices to zero so no cost record fires without
// pricing data (mirrors the success path's cost gating).
func TestRefusalCostUSDNilConfig(t *testing.T) {
	if got := refusalCostUSD(TokenUsage{PromptTokens: 10, CompletionTokens: 5}, nil); got != 0 {
		t.Errorf("refusalCostUSD(nil cfg) = %v, want 0", got)
	}
}
