package llm

import (
	"testing"

	appmetrics "github.com/caimlas/meept/internal/metrics"
)

// TestProviderManager_SetLLMCallObserverFanout pins the llm-hop-tracing
// fan-out (bughunt 2026-11-06 llm-daemon F-1): the observer must reach
// EVERY chatter class the manager owns - the OpenAI-compatible Client,
// the AnthropicClient (base-URL or provider id), and the CodexClient -
// plus providers added later via AddProvider. Before the seam, only the
// daemon's primary client was attached and Anthropic/Codex hops emitted
// no llm.call spans.
func TestProviderManager_SetLLMCallObserverFanout(t *testing.T) {
	seen := map[string]int{}
	obs := func(rec appmetrics.LLMCallRecord) { seen[rec.Provider]++ }

	cfg := ProviderManagerConfig{
		Providers: []*ModelConfig{
			{ProviderID: "openai", BaseURL: "http://localhost:8001/v1", ModelID: "m1"},
			{ProviderID: "anthropic", BaseURL: "https://api.anthropic.com", ModelID: "m2"},
			{ProviderID: "openai-codex", BaseURL: "https://chatgpt.com/backend-api/codex", ModelID: "m3"},
		},
	}
	pm := NewProviderManager(cfg)

	pm.SetLLMCallObserver(obs)
	pm.SetLLMCallObserver(nil) // nil ignored (attach-only contract)

	check := func(who string, c Chatter) {
		t.Helper()
		var fn func(appmetrics.LLMCallRecord)
		switch x := c.(type) {
		case *Client:
			fn = x.llmCallObserverFn()
		case *AnthropicClient:
			fn = x.llmCallObserverFn()
		case *CodexClient:
			fn = x.llmCallObserverFn()
		default:
			t.Fatalf("%s: unexpected chatter type %T", who, c)
		}
		if fn == nil {
			t.Errorf("%s: chatter has no observer attached", who)
			return
		}
		fn(appmetrics.LLMCallRecord{Provider: who})
		if seen[who] != 1 {
			t.Errorf("%s: observer not fired (seen=%v)", who, seen)
		}
	}

	for _, e := range pm.providers {
		switch e.Config.ProviderID {
		case "anthropic":
			if _, ok := e.Chatter.(*AnthropicClient); !ok {
				t.Fatalf("anthropic provider built %T", e.Chatter)
			}
		case "openai-codex":
			if _, ok := e.Chatter.(*CodexClient); !ok {
				t.Fatalf("codex provider built %T", e.Chatter)
			}
		}
		check(e.Config.ProviderID, e.Chatter)
	}

	// Late addition inherits the observer too.
	pm.AddProvider(&ModelConfig{ProviderID: "anthropic2", BaseURL: "https://api.anthropic.com", ModelID: "m4"}, 9)
	last := pm.providers[len(pm.providers)-1]
	if c, ok := last.Chatter.(*AnthropicClient); ok {
		check("anthropic2-late", c)
	} else {
		t.Fatalf("late provider chatter type %T", last.Chatter)
	}
}
