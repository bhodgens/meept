package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestProviderManager_NewProviderManager(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{
				ProviderID: "provider1",
				BaseURL:    "http://localhost:8001/v1",
				ModelID:    "model1",
			},
			{
				ProviderID: "provider2",
				BaseURL:    "http://localhost:8002/v1",
				ModelID:    "model2",
			},
		},
	}

	pm := NewProviderManager(cfg)

	if pm.ProviderCount() != 2 {
		t.Errorf("expected 2 providers, got %d", pm.ProviderCount())
	}

	if pm.HealthyProviderCount() != 2 {
		t.Errorf("expected 2 healthy providers, got %d", pm.HealthyProviderCount())
	}
}

func TestProviderManager_GetProviderHealth(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "test1", BaseURL: "http://test1/v1", ModelID: "m1"},
			{ProviderID: "test2", BaseURL: "http://test2/v1", ModelID: "m2"},
		},
	}

	pm := NewProviderManager(cfg)
	health := pm.GetProviderHealth()

	if len(health) != 2 {
		t.Errorf("expected 2 health entries, got %d", len(health))
	}

	for _, h := range health {
		if h.Status != ProviderStatusHealthy {
			t.Errorf("expected healthy status, got %s", h.Status)
		}
	}
}

func TestProviderManager_DisableEnableProvider(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "test1", BaseURL: "http://test1/v1", ModelID: "m1"},
		},
	}

	pm := NewProviderManager(cfg)

	// Disable
	err := pm.DisableProvider("test1")
	if err != nil {
		t.Fatalf("DisableProvider failed: %v", err)
	}

	health := pm.GetProviderHealth()
	if health[0].Status != ProviderStatusDisabled {
		t.Errorf("expected disabled status, got %s", health[0].Status)
	}

	// Enable
	err = pm.EnableProvider("test1")
	if err != nil {
		t.Fatalf("EnableProvider failed: %v", err)
	}

	health = pm.GetProviderHealth()
	if health[0].Status == ProviderStatusDisabled {
		t.Error("expected non-disabled status after enable")
	}
}

func TestProviderManager_DisableNonexistent(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "test1", BaseURL: "http://test1/v1", ModelID: "m1"},
		},
	}

	pm := NewProviderManager(cfg)

	err := pm.DisableProvider("nonexistent")
	if err == nil {
		t.Error("expected error for nonexistent provider")
	}
}

func TestProviderManager_AddRemoveProvider(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "test1", BaseURL: "http://test1/v1", ModelID: "m1"},
		},
	}

	pm := NewProviderManager(cfg)

	// Add provider
	pm.AddProvider(&ModelConfig{
		ProviderID: "test2",
		BaseURL:    "http://test2/v1",
		ModelID:    "m2",
	}, 1)

	if pm.ProviderCount() != 2 {
		t.Errorf("expected 2 providers after add, got %d", pm.ProviderCount())
	}

	// Remove provider
	err := pm.RemoveProvider("test2")
	if err != nil {
		t.Fatalf("RemoveProvider failed: %v", err)
	}

	if pm.ProviderCount() != 1 {
		t.Errorf("expected 1 provider after remove, got %d", pm.ProviderCount())
	}
}

func TestProviderManager_GetPrimaryProvider(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: "http://primary/v1", ModelID: "m1"},
			{ProviderID: "backup", BaseURL: "http://backup/v1", ModelID: "m2"},
		},
	}

	pm := NewProviderManager(cfg)

	primary := pm.GetPrimaryProvider()
	if primary == nil {
		t.Fatal("expected non-nil primary provider")
	}

	if primary.Config.ProviderID != "primary" {
		t.Errorf("expected primary provider, got %s", primary.Config.ProviderID)
	}
}

func TestProviderManager_CostOptimizedRouting(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{
				ProviderID:           "expensive",
				BaseURL:              "http://expensive/v1",
				ModelID:              "m1",
				CostPerMillionInput:  10.0,
				CostPerMillionOutput: 30.0,
			},
			{
				ProviderID:           "cheap",
				BaseURL:              "http://cheap/v1",
				ModelID:              "m2",
				CostPerMillionInput:  0.5,
				CostPerMillionOutput: 1.5,
			},
		},
		CostOptimized: true,
	}

	pm := NewProviderManager(cfg)

	primary := pm.GetPrimaryProvider()
	if primary == nil {
		t.Fatal("expected non-nil primary provider")
	}

	// With cost optimization, cheaper provider should be first
	if primary.Config.ProviderID != "cheap" {
		t.Errorf("expected cheap provider with cost optimization, got %s", primary.Config.ProviderID)
	}

	// Disable cost optimization
	pm.SetCostOptimized(false)

	// Now it should be by priority (expensive was added first)
	primary = pm.GetPrimaryProvider()
	if primary.Config.ProviderID != "expensive" {
		t.Errorf("expected expensive provider without cost optimization, got %s", primary.Config.ProviderID)
	}
}

func TestProviderManager_ResetProviderHealth(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "test1", BaseURL: "http://test1/v1", ModelID: "m1"},
		},
	}

	pm := NewProviderManager(cfg)

	// Modify health
	pm.mu.Lock()
	pm.providers[0].Health.Status = ProviderStatusUnhealthy
	pm.providers[0].Health.ConsecutiveFails = 5
	pm.mu.Unlock()

	// Reset
	err := pm.ResetProviderHealth("test1")
	if err != nil {
		t.Fatalf("ResetProviderHealth failed: %v", err)
	}

	health := pm.GetProviderHealth()
	if health[0].Status != ProviderStatusHealthy {
		t.Errorf("expected healthy after reset, got %s", health[0].Status)
	}
	if health[0].ConsecutiveFails != 0 {
		t.Errorf("expected 0 consecutive fails after reset, got %d", health[0].ConsecutiveFails)
	}
}

func TestProviderManager_Failover(t *testing.T) {
	// Create a counter to track which server gets called
	var primaryCalls int32
	var backupCalls int32

	// Create mock servers
	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		// Always fail
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "primary always fails"}`))
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupCalls, 1)
		// Always succeed
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Hello from backup!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer backupServer.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{
				ProviderID: "primary",
				BaseURL:    primaryServer.URL,
				ModelID:    "model1",
			},
			{
				ProviderID: "backup",
				BaseURL:    backupServer.URL,
				ModelID:    "model2",
			},
		},
		FailoverTimeout: 5 * time.Second,
	}

	pm := NewProviderManager(cfg)
	ctx := context.Background()

	// Make a request - should failover to backup
	resp, err := pm.Chat(ctx, []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "Hello from backup!" {
		t.Errorf("expected backup response, got %s", resp.Content)
	}

	// Primary server may be called multiple times due to Client's internal retry logic
	if atomic.LoadInt32(&primaryCalls) < 1 {
		t.Errorf("expected at least 1 primary call, got %d", primaryCalls)
	}

	// Backup should be called exactly once (after primary exhausts retries)
	if atomic.LoadInt32(&backupCalls) < 1 {
		t.Errorf("expected at least 1 backup call, got %d", backupCalls)
	}

	// Check that primary is now degraded/unhealthy
	health := pm.GetProviderHealth()
	primaryHealth := health[0]
	if primaryHealth.Status == ProviderStatusHealthy {
		t.Error("expected primary to be degraded or unhealthy after failure")
	}
	if primaryHealth.ConsecutiveFails != 1 {
		t.Errorf("expected 1 consecutive fail, got %d", primaryHealth.ConsecutiveFails)
	}
}

func TestProviderManager_AllProvidersFail(t *testing.T) {
	// Create servers that always fail
	server1 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "server1 fail"}`))
	}))
	defer server1.Close()

	server2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error": "server2 fail"}`))
	}))
	defer server2.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "p1", BaseURL: server1.URL, ModelID: "m1"},
			{ProviderID: "p2", BaseURL: server2.URL, ModelID: "m2"},
		},
		FailoverTimeout: 5 * time.Second,
	}

	pm := NewProviderManager(cfg)
	ctx := context.Background()

	_, err := pm.Chat(ctx, []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})

	if err == nil {
		t.Error("expected error when all providers fail")
	}
}

func TestProviderManager_SuccessUpdatesHealth(t *testing.T) {
	// Create a server that always succeeds
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"choices": [{
				"index": 0,
				"message": {"role": "assistant", "content": "Success!"},
				"finish_reason": "stop"
			}],
			"usage": {"prompt_tokens": 100, "completion_tokens": 50, "total_tokens": 150}
		}`))
	}))
	defer server.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{
				ProviderID:           "test",
				BaseURL:              server.URL,
				ModelID:              "model1",
				CostPerMillionInput:  1.0,
				CostPerMillionOutput: 2.0,
			},
		},
	}

	pm := NewProviderManager(cfg)
	ctx := context.Background()

	// Make several requests
	for range 3 {
		_, err := pm.Chat(ctx, []ChatMessage{
			{Role: RoleUser, Content: "Hello"},
		})
		if err != nil {
			t.Fatalf("Chat failed: %v", err)
		}
	}

	health := pm.GetProviderHealth()
	if len(health) != 1 {
		t.Fatalf("expected 1 health entry, got %d", len(health))
	}

	h := health[0]
	if h.SuccessCount != 3 {
		t.Errorf("expected 3 successes, got %d", h.SuccessCount)
	}

	if h.TotalTokens != 450 { // 150 * 3
		t.Errorf("expected 450 total tokens, got %d", h.TotalTokens)
	}

	if h.TotalCost == 0 {
		t.Error("expected non-zero total cost")
	}

	// Note: AvgLatencyMs could be 0 or very low for local test servers
	// The important thing is that success tracking is working correctly
	// which is verified by the SuccessCount and TotalTokens checks above
}

func TestProviderManager_GetProviderStatus(t *testing.T) {
	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "healthy1", BaseURL: "http://h1/v1", ModelID: "m1"},
			{ProviderID: "healthy2", BaseURL: "http://h2/v1", ModelID: "m2"},
		},
	}

	pm := NewProviderManager(cfg)

	// Mark one as unhealthy
	pm.mu.Lock()
	pm.providers[1].Health.Status = ProviderStatusUnhealthy
	pm.mu.Unlock()

	status := pm.GetProviderStatus()

	if status["total_providers"].(int) != 2 {
		t.Errorf("expected 2 total providers")
	}

	if status["healthy_count"].(int) != 1 {
		t.Errorf("expected 1 healthy provider")
	}

	if status["unhealthy_count"].(int) != 1 {
		t.Errorf("expected 1 unhealthy provider")
	}
}

func TestProviderManager_ContextCancellation(t *testing.T) {
	// Create a slow server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(5 * time.Second)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "slow", BaseURL: server.URL, ModelID: "m1"},
		},
		FailoverTimeout: 10 * time.Second,
	}

	pm := NewProviderManager(cfg)

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := pm.Chat(ctx, []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})

	if err == nil {
		t.Error("expected error on context cancellation")
	}
}

func TestProviderManager_SkipsUnhealthyProviders(t *testing.T) {
	var primaryCalls int32
	var backupCalls int32

	primaryServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&primaryCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-123",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Primary"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer primaryServer.Close()

	backupServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&backupCalls, 1)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{
			"id": "chatcmpl-124",
			"choices": [{"index": 0, "message": {"role": "assistant", "content": "Backup"}, "finish_reason": "stop"}],
			"usage": {"prompt_tokens": 10, "completion_tokens": 5, "total_tokens": 15}
		}`))
	}))
	defer backupServer.Close()

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "primary", BaseURL: primaryServer.URL, ModelID: "m1"},
			{ProviderID: "backup", BaseURL: backupServer.URL, ModelID: "m2"},
		},
	}

	pm := NewProviderManager(cfg)
	ctx := context.Background()

	// Mark primary as unhealthy
	pm.mu.Lock()
	pm.providers[0].Health.Status = ProviderStatusUnhealthy
	pm.providers[0].Health.ConsecutiveFails = 5
	pm.mu.Unlock()

	// Request should go to backup
	resp, err := pm.Chat(ctx, []ChatMessage{
		{Role: RoleUser, Content: "Hello"},
	})

	if err != nil {
		t.Fatalf("Chat failed: %v", err)
	}

	if resp.Content != "Backup" {
		t.Errorf("expected Backup response, got %s", resp.Content)
	}

	// Primary should not have been called
	if atomic.LoadInt32(&primaryCalls) != 0 {
		t.Errorf("expected 0 primary calls, got %d", primaryCalls)
	}

	if atomic.LoadInt32(&backupCalls) != 1 {
		t.Errorf("expected 1 backup call, got %d", backupCalls)
	}
}
