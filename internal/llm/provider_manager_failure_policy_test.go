package llm

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestProviderManager_DefaultPolicyUntouched pins the production default:
// a manager whose chatters were never given a FailurePolicyConfig resolves
// the package defaults (30s throttle base, 1h polling floor, 24h horizon,
// 3 short retries). Guards the SetFailurePolicyConfig seam from silently
// changing what unconfigured managers do.
func TestProviderManager_DefaultPolicyUntouched(t *testing.T) {
	pm := NewProviderManager(ProviderManagerConfig{})
	pm.mu.RLock()
	injected := pm.failurePolicy != nil
	pm.mu.RUnlock()
	if injected {
		t.Fatal("a fresh ProviderManager must not carry an injected failure policy")
	}

	c := NewClient(&ModelConfig{}, WithLogger(discardLogger()))
	got := c.policyCfg()
	want := FailurePolicyConfig{
		Horizon:      24 * time.Hour,
		BaseThrottle: 30 * time.Second,
		PollFloor:    time.Hour,
	}
	if got != want {
		t.Fatalf("default policy = %+v, want %+v (30s/1h/24h)", got, want)
	}
	if c.shortRetryBudget() != defaultShortRetries {
		t.Fatalf("default short retries = %d, want %d", c.shortRetryBudget(), defaultShortRetries)
	}
}

// TestProviderManager_SetFailurePolicyConfig verifies the seam: the config
// is propagated to OpenAI-compatible Client chatters that exist now AND
// chatters added later (AddProvider), so a bare-429 retry loop waits the
// injected ~1ms step instead of the 30s production default. Nil-receiver
// and nil-config safety are covered at the end.
func TestProviderManager_SetFailurePolicyConfig(t *testing.T) {
	var hits atomic.Int32
	// Bare 429 forever: under a fast policy the client must exhaust its
	// short budget in milliseconds; under the production default the very
	// first in-loop wait would be 30s.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":{"message":"429 bare"}}`))
	}))
	defer srv.Close()

	pm := NewProviderManager(ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "throttled", BaseURL: srv.URL, ModelID: "m1"},
		},
		FailoverTimeout: 10 * time.Second,
	})
	pm.SetFailurePolicyConfig(fastFailurePolicyCfg)

	// The injected config must be visible on the existing chatter.
	pm.mu.RLock()
	chatter, ok := pm.providers[0].Chatter.(*Client)
	pm.mu.RUnlock()
	if !ok {
		t.Fatalf("chatter is %T, want *Client", pm.providers[0].Chatter)
	}
	if got := chatter.failurePolicy; got == nil || got.BaseThrottle != fastFailurePolicyCfg.BaseThrottle {
		t.Fatalf("existing chatter policy = %+v, want injected fast config", got)
	}

	start := time.Now()
	if _, err := pm.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hi"}}); err == nil {
		t.Fatal("expected error from sustained throttle")
	}
	elapsed := time.Since(start)
	if elapsed > 5*time.Second {
		t.Fatalf("throttle loop took %v with fast policy — seam not applied", elapsed)
	}

	// A chatter added AFTER the setter must inherit the config too.
	pm.AddProvider(&ModelConfig{ProviderID: "late", BaseURL: srv.URL, ModelID: "m2"}, 1)
	pm.mu.RLock()
	lateChatter, ok := pm.providers[1].Chatter.(*Client)
	pm.mu.RUnlock()
	if !ok {
		t.Fatalf("late chatter is %T, want *Client", pm.providers[1].Chatter)
	}
	if got := lateChatter.failurePolicy; got == nil || got.BaseThrottle != fastFailurePolicyCfg.BaseThrottle {
		t.Fatalf("late-added chatter policy = %+v, want injected fast config", got)
	}

	// Nil config restores the default (not a panic, not a stuck nil deref).
	pm.SetFailurePolicyConfig(nil)
	pm.mu.RLock()
	chatter, _ = pm.providers[0].Chatter.(*Client)
	pm.mu.RUnlock()
	if chatter.failurePolicy != nil {
		t.Fatalf("nil config should restore the nil-safe default, got %+v", chatter.failurePolicy)
	}

	var nilPM *ProviderManager
	nilPM.SetFailurePolicyConfig(fastFailurePolicyCfg) // must not panic
}
