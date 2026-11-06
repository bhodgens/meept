package daemon

// Serving-tier policy daemon-wiring tests (llm-serving-tiers leaf 01 T5
// arm-up, issue #82): applyTierPolicyToProviderManager is the ONE daemon
// call site for ProviderManager.SetTierPolicy. Flag off = no policy
// installed (byte-identical rotation); flag on = the mapped policy is
// live and the depth ceiling actually binds a real rotation.

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/llm"
)

// tierStubServer is a member stub serving blank completions: every
// OpenAI-compatible client classifies that as ErrEmptyResponse and the
// ProviderManager rotation moves to the next member (the exact lane the
// e2e tier-depth scenario pins; here it gives the rotation a chain that
// must stop at the floor).
func tierStubServer(t *testing.T) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/v1/chat/completions", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{"id":"stub","object":"chat.completion","created":0,"model":"stub-model","choices":[{"index":0,"message":{"role":"assistant","content":"   "},"finish_reason":"stop"}],"usage":{"prompt_tokens":1,"completion_tokens":1,"total_tokens":2}}`)
	})
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	return server.URL + "/v1"
}

func tierCfg() *config.Config {
	cfg := config.DefaultConfig()
	cfg.LLM.Cache.Enabled = false
	cfg.LLM.Cache.Semantic.Enabled = false
	return cfg
}

// TestTierPolicyWiringDisabled (flag off): the mapping returns nil and
// SetTierPolicy(nil) clears - the rotation is unbounded exactly as
// pre-leaf (the 4-member chain serves all four before failing).
func TestTierPolicyWiringDisabled(t *testing.T) {
	base := tierStubServer(t)
	configs := make([]*llm.ModelConfig, 0, 4)
	for i := 1; i <= 4; i++ {
		configs = append(configs, &llm.ModelConfig{
			ProviderID:   fmt.Sprintf("prov-%d", i),
			ModelID:      "stub-model",
			BaseURL:      base,
			APIKey:       fmt.Sprintf("k-%d", i),
			Timeout:      15 * time.Second,
			Capabilities: map[string]bool{"completion": true},
		})
	}
	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: configs,
	})
	defer pm.Stop()

	cfg := tierCfg() // LLM.Tiers zero-value: disabled
	if installed := applyTierPolicyToProviderManager(pm, cfg); installed != nil {
		t.Fatalf("disabled config must install no policy, got %+v", installed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := pm.Chat(ctx, []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("blank-completion chain must exhaust and fail")
	}
	if llm.IsTierFloorError(err) {
		t.Fatal("disabled policy must never floor-classify the rotation")
	}
}

// TestTierPolicyWiringEnabled (flag on): llm.tiers {enabled, max_depth:
// 2} maps field-for-field and the ceiling BINDS the real rotation - the
// 4-member chain stops at the floor (ErrTierFloor) instead of dispatching
// member 4. This is the daemon-level proof that the SetTierPolicy call
// site arms the feature.
func TestTierPolicyWiringEnabled(t *testing.T) {
	base := tierStubServer(t)
	configs := make([]*llm.ModelConfig, 0, 4)
	for i := 1; i <= 4; i++ {
		configs = append(configs, &llm.ModelConfig{
			ProviderID:   fmt.Sprintf("prov-%d", i),
			ModelID:      "stub-model",
			BaseURL:      base,
			APIKey:       fmt.Sprintf("k-%d", i),
			Timeout:      15 * time.Second,
			Capabilities: map[string]bool{"completion": true},
		})
	}
	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: configs,
	})
	defer pm.Stop()

	cfg := tierCfg()
	cfg.LLM.Tiers = config.TierPolicyConfig{
		Enabled:        true,
		MaxDepth:       2,
		TurnCeilings:   map[string]int{"small": 1},
		SignalDegraded: true,
	}
	installed := applyTierPolicyToProviderManager(pm, cfg)
	if installed == nil || !installed.Enabled || installed.MaxDepth != 2 ||
		installed.TurnCeilings["small"] != 1 || !installed.SignalDegraded {
		t.Fatalf("field mapping wrong: %+v", installed)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	_, err := pm.Chat(ctx, []llm.ChatMessage{{Role: llm.RoleUser, Content: "hi"}})
	if err == nil {
		t.Fatal("expected the rotation to stop at the tier floor")
	}
	if !llm.IsTierFloorError(err) {
		t.Fatalf("rotation not floor-classified with the policy armed: %v", err)
	}
}

// TestTierPolicyMappingNilCeilings: enabled policy with no turn
// ceilings maps with a nil map (the llm side treats absent as
// EffectiveDepth); disabled config never builds a policy even with
// knobs set (the Enabled gate binds).
func TestTierPolicyMappingNilCeilings(t *testing.T) {
	p := tierPolicyFromSchema(config.TierPolicyConfig{Enabled: true, MaxDepth: 3, SignalDegraded: true})
	if p == nil || p.MaxDepth != 3 || p.TurnCeilings != nil {
		t.Fatalf("mapping wrong: %+v", p)
	}
	if got := tierPolicyFromSchema(config.TierPolicyConfig{Enabled: false, MaxDepth: 5}); got != nil {
		t.Fatal("disabled config must map to nil policy")
	}
}
