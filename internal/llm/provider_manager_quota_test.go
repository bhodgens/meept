package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// quotaFailServer returns a 429 with a quota-shaped body (long reset
// horizon) — classified as QuotaResetError by the client (leaf 01).
func quotaFailServer(calls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"type":"usage_limit_reached","resets_at":4102444800}}`))
	}))
}

// okServer returns a minimal successful chat completion.
func okServer(calls *int32) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-1",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "ok"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 1, "completion_tokens": 1, "total_tokens": 2}
		}`))
	}))
}

// TestProviderManagerQuota_BlockPersistsAcrossChats verifies the audit gap:
// after a quota error the block is PERSISTED, so a second Chat call skips
// the quota-limited provider entirely (it is not re-hit).
func TestProviderManagerQuota_BlockPersistsAcrossChats(t *testing.T) {
	var quotaCalls, okCalls int32
	qs := quotaFailServer(&quotaCalls)
	defer qs.Close()
	os := okServer(&okCalls)
	defer os.Close()

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "quota-provider", BaseURL: qs.URL, ModelID: "m1"},
			{ProviderID: "ok-provider", BaseURL: os.URL, ModelID: "m2"},
		},
		// Keep the raw quota error path deterministic: no client-level
		// short retries are configured by default.
		Logger: nil,
	})
	pm.SetQuotaMaxWait(time.Hour)
	ctx := context.Background()

	// First call: quota provider 429s (after its internal retries), manager
	// blocks its credential and fails over to the ok provider.
	resp, err := pm.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "hello"}})
	if err != nil {
		t.Fatalf("first Chat: %v", err)
	}
	if resp.Content != "ok" {
		t.Fatalf("expected ok-provider response, got %q", resp.Content)
	}

	callsAfterFirst := atomic.LoadInt32(&quotaCalls)
	if callsAfterFirst < 1 {
		t.Fatalf("expected quota provider to be hit at least once, got %d", callsAfterFirst)
	}

	// Second call: the quota credential must be BLOCKED — the quota server
	// call count must not increase.
	if _, err := pm.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "again"}}); err != nil {
		t.Fatalf("second Chat: %v", err)
	}
	if got := atomic.LoadInt32(&quotaCalls); got != callsAfterFirst {
		t.Errorf("blocked provider was re-hit: quota calls %d -> %d", callsAfterFirst, got)
	}
	if got := atomic.LoadInt32(&okCalls); got < 2 {
		t.Errorf("expected ok provider to serve both calls, got %d", okCalls)
	}

	// The block must be visible in the manager state.
	pm.mu.RLock()
	defer pm.mu.RUnlock()
	key := QuotaCredentialKey("quota-provider", pm.providers[0].Config)
	until, ok := pm.quotaBlocks[key]
	if !ok {
		t.Fatal("expected a persisted quota block for quota-provider")
	}
	if until.Before(time.Now().Add(30 * time.Minute)) {
		t.Errorf("expected block horizon near now+1h, got %v", until)
	}
}

// TestProviderManagerQuota_ProbeReblocksAndClears verifies runHealthCheck
// quota integration: an expired block is probed; a quota-failing probe
// re-blocks (without marking Unhealthy worse), a successful probe clears.
func TestProviderManagerQuota_ProbeReblocksAndClears(t *testing.T) {
	t.Run("expired block probed and re-blocked on quota", func(t *testing.T) {
		var probeCalls int32
		qs := quotaFailServer(&probeCalls)
		defer qs.Close()

		pm := NewProviderManager(ProviderManagerConfig{
			Providers: []*ModelConfig{
				{ProviderID: "solo", BaseURL: qs.URL, ModelID: "m1"},
			},
		})
		pm.SetQuotaMaxWait(time.Hour)

		entry := pm.providers[0]
		entry.Health.Status = ProviderStatusUnhealthy
		pm.mu.Lock()
		pm.quotaBlocks = map[string]time.Time{
			QuotaCredentialKey("solo", entry.Config): time.Now().Add(-time.Minute), // EXPIRED
		}
		pm.mu.Unlock()

		pm.runHealthCheck(context.Background())

		if got := atomic.LoadInt32(&probeCalls); got != 1 {
			t.Fatalf("expected exactly 1 probe call, got %d", got)
		}
		pm.mu.RLock()
		until := pm.quotaBlocks[QuotaCredentialKey("solo", entry.Config)]
		pm.mu.RUnlock()
		if until.Before(time.Now().Add(30 * time.Minute)) {
			t.Errorf("expected credential re-blocked ~1h out, got %v", until)
		}
		// Quota must NOT further degrade health beyond Unhealthy (no
		// recordFailure semantics): still exactly Unhealthy.
		pm.mu.RLock()
		status := entry.Health.Status
		pm.mu.RUnlock()
		if status != ProviderStatusUnhealthy {
			t.Errorf("expected status to remain unhealthy, got %s", status)
		}

		// A still-blocked credential is not probed again.
		before := atomic.LoadInt32(&probeCalls)
		pm.runHealthCheck(context.Background())
		if got := atomic.LoadInt32(&probeCalls); got != before {
			t.Errorf("still-blocked credential was probed: %d -> %d", before, got)
		}
	})

	t.Run("successful probe clears expired block", func(t *testing.T) {
		var probeCalls int32
		os := okServer(&probeCalls)
		defer os.Close()

		pm := NewProviderManager(ProviderManagerConfig{
			Providers: []*ModelConfig{
				{ProviderID: "solo", BaseURL: os.URL, ModelID: "m1"},
			},
		})

		entry := pm.providers[0]
		entry.Health.Status = ProviderStatusUnhealthy
		pm.mu.Lock()
		pm.quotaBlocks = map[string]time.Time{
			QuotaCredentialKey("solo", entry.Config): time.Now().Add(-time.Minute), // EXPIRED
		}
		pm.mu.Unlock()

		pm.runHealthCheck(context.Background())

		if got := atomic.LoadInt32(&probeCalls); got != 1 {
			t.Fatalf("expected exactly 1 probe call, got %d", got)
		}
		pm.mu.RLock()
		_, blocked := pm.quotaBlocks[QuotaCredentialKey("solo", entry.Config)]
		pm.mu.RUnlock()
		if blocked {
			t.Error("expected successful probe to clear the expired block")
		}
		pm.mu.RLock()
		status := entry.Health.Status
		pm.mu.RUnlock()
		if status != ProviderStatusDegraded {
			t.Errorf("expected recovered status degraded, got %s", status)
		}
	})

	t.Run("unhealthy provider without block is still probed", func(t *testing.T) {
		var probeCalls int32
		os := okServer(&probeCalls)
		defer os.Close()

		pm := NewProviderManager(ProviderManagerConfig{
			Providers: []*ModelConfig{
				{ProviderID: "solo", BaseURL: os.URL, ModelID: "m1"},
			},
		})
		pm.providers[0].Health.Status = ProviderStatusUnhealthy

		pm.runHealthCheck(context.Background())
		if got := atomic.LoadInt32(&probeCalls); got != 1 {
			t.Errorf("expected 1 probe call, got %d", got)
		}
	})
}

// TestProviderManagerQuota_SuccessClearsBlock verifies that a successful
// Chat clears a persisted block even before its horizon elapses (the
// served request proves the window lifted).
func TestProviderManagerQuota_SuccessClearsBlock(t *testing.T) {
	var okCalls int32
	os := okServer(&okCalls)
	defer os.Close()

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "ok-provider", BaseURL: os.URL, ModelID: "m1"},
		},
	})
	pm.SetQuotaMaxWait(time.Hour)

	entry := pm.providers[0]
	pm.mu.Lock()
	pm.quotaBlocks = map[string]time.Time{
		QuotaCredentialKey("ok-provider", entry.Config): time.Now().Add(time.Hour), // STILL BLOCKED
	}
	pm.mu.Unlock()

	// The blocked credential would be skipped by getOrderedProviders...
	pm.mu.RLock()
	ordered := pm.getOrderedProviders()
	pm.mu.RUnlock()
	if len(ordered) != 0 {
		t.Fatalf("expected blocked provider to be filtered from ordering, got %d", len(ordered))
	}

	// ...but a direct success through Chat must clear the block. Simulate
	// expiry first (the Chat path would otherwise never reach the provider).
	pm.mu.Lock()
	pm.quotaBlocks[QuotaCredentialKey("ok-provider", entry.Config)] = time.Now().Add(-time.Second)
	pm.mu.Unlock()

	if _, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	pm.mu.RLock()
	_, stillBlocked := pm.quotaBlocks[QuotaCredentialKey("ok-provider", entry.Config)]
	pm.mu.RUnlock()
	if stillBlocked {
		t.Error("expected success to clear the credential block")
	}
}

// TestProviderManager_SetQuotaMaxWait verifies the setter: custom cap, nil
// receiver no-op, and zero/mean revert to DefaultQuotaMaxWait.
func TestProviderManager_SetQuotaMaxWait(t *testing.T) {
	pm := NewProviderManager(ProviderManagerConfig{})
	pm.mu.RLock()
	if got := pm.effectiveQuotaMaxWait(); got != DefaultQuotaMaxWait {
		t.Errorf("default = %v, want %v", got, DefaultQuotaMaxWait)
	}
	pm.mu.RUnlock()

	pm.SetQuotaMaxWait(2 * time.Hour)
	pm.mu.RLock()
	if got := pm.effectiveQuotaMaxWait(); got != 2*time.Hour {
		t.Errorf("after set = %v, want 2h", got)
	}
	pm.mu.RUnlock()

	// Zero restores the default.
	pm.SetQuotaMaxWait(0)
	pm.mu.RLock()
	if got := pm.effectiveQuotaMaxWait(); got != DefaultQuotaMaxWait {
		t.Errorf("after zero = %v, want default %v", got, DefaultQuotaMaxWait)
	}
	pm.mu.RUnlock()

	var nilPM *ProviderManager
	nilPM.SetQuotaMaxWait(time.Hour) // must not panic
}

// TestProviderManagerQuota_AllProvidersQuotaBlocked verifies that when every
// credential is blocked the manager surfaces an error instead of hammering
// a 429ing provider.
func TestProviderManagerQuota_AllProvidersQuotaBlocked(t *testing.T) {
	var calls int32
	qs := quotaFailServer(&calls)
	defer qs.Close()

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "quota-provider", BaseURL: qs.URL, ModelID: "m1"},
		},
	})
	pm.SetQuotaMaxWait(time.Hour)

	ctx := context.Background()
	if _, err := pm.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "x"}}); err == nil {
		t.Fatal("expected error when the only provider is quota-limited")
	}

	callsAfterFirst := atomic.LoadInt32(&calls)
	if _, err := pm.Chat(ctx, []ChatMessage{{Role: RoleUser, Content: "y"}}); err == nil {
		t.Fatal("expected error on second call with the only provider blocked")
	}
	if got := atomic.LoadInt32(&calls); got != callsAfterFirst {
		t.Errorf("blocked provider re-hit: %d -> %d", callsAfterFirst, got)
	}
}
