package agent

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// failoverTestServers spins up two httptest servers: a primary that returns
// an empty-content chat completion and a secondary that returns valid JSON
// content. Returns the servers plus the resolved ModelConfigs for each.
func failoverTestServers(t *testing.T, primaryBody, secondaryBody string) (primary, secondary *httptest.Server, primaryCfg, secondaryCfg *llm.ModelConfig) {
	t.Helper()

	primary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(primaryBody))
	}))
	secondary = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(secondaryBody))
	}))
	t.Cleanup(primary.Close)
	t.Cleanup(secondary.Close)

	primaryCfg = &llm.ModelConfig{BaseURL: primary.URL, ModelID: "primary", APIKey: "test"}
	secondaryCfg = &llm.ModelConfig{BaseURL: secondary.URL, ModelID: "secondary", APIKey: "test"}
	return
}

// newFailoverResolver builds a Resolver with a two-model "classifier" alias.
func newFailoverResolver(t *testing.T, first, second *llm.ModelConfig) *llm.Resolver {
	t.Helper()
	cfg := &llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			testClassifierAlias: {Models: []string{"p1/m1", "p2/m2"}},
		},
		Providers: map[string]llm.ProviderConfig{
			"p1": {API: "http", Options: llm.ProviderOptionsConfig{BaseURL: first.BaseURL}, Models: map[string]llm.ModelDef{
				"m1": {Name: "m1"},
			}},
			"p2": {API: "http", Options: llm.ProviderOptionsConfig{BaseURL: second.BaseURL}, Models: map[string]llm.ModelDef{
				"m2": {Name: "m2"},
			}},
		},
	}
	return llm.NewResolver(cfg, nil)
}

const testClassifierAlias = "classifier"

func emptyContentResponse() string {
	return `{"choices":[{"message":{"role":"assistant","content":""}}],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`
}

func validIntentResponse() string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": `{"intent":"code","confidence":0.9}`}},
		},
	})
	return string(b)
}

func validAnalysisResponse() string {
	b, _ := json.Marshal(map[string]any{
		"choices": []map[string]any{
			{"message": map[string]any{"role": "assistant", "content": `{"goal":"fix bug","ambiguity":0.2,"scope":"narrow","category":"fix","suggested_questions":[],"confidence":0.9}`}},
		},
	})
	return string(b)
}

func TestLLMClassifier_RotatesOnEmptyResponse(t *testing.T) {
	primarySrv, secondarySrv, primaryCfg, secondaryCfg := failoverTestServers(
		t, emptyContentResponse(), validIntentResponse())
	resolver := newFailoverResolver(t, primaryCfg, secondaryCfg)

	client := llm.NewClient(primaryCfg)
	c := NewLLMClassifier(LLMClassifierConfig{
		Client:      client,
		Model:       "primary",
		ModelConfig: primaryCfg,
		Resolver:    resolver,
		AliasName:   testClassifierAlias,
	}, nil)

	intent, err := c.Classify(context.Background(), "write some code", nil)
	if err != nil {
		t.Fatalf("expected success after rotation, got error: %v", err)
	}
	if intent == nil || intent.Type != "code" {
		t.Fatalf("expected intent 'code', got %+v", intent)
	}

	// Health should show success on candidate 2 (index advanced once).
	if !resolver.HasHealthyModels(testClassifierAlias) {
		t.Error("expected alias to be healthy after successful rotation")
	}
	_ = primarySrv  // used via configs; keep references alive
	_ = secondarySrv
}

func TestLLMClassifier_NoRotationWithoutResolver(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(emptyContentResponse()))
	}))
	defer srv.Close()

	cfg := &llm.ModelConfig{BaseURL: srv.URL, ModelID: "primary"}
	c := NewLLMClassifier(LLMClassifierConfig{
		Client:      llm.NewClient(cfg),
		Model:       "primary",
		ModelConfig: cfg,
		Resolver:    nil,
	}, nil)

	intent, err := c.Classify(context.Background(), "write some code", nil)
	if err == nil {
		t.Fatalf("expected error with nil resolver, got intent %+v", intent)
	}
	if got := atomic.LoadInt32(&calls); got != 1 {
		t.Errorf("expected exactly 1 request without resolver, got %d", got)
	}
}

func TestIntentAnalyzer_RotatesOnEmptyResponse(t *testing.T) {
	_, _, primaryCfg, secondaryCfg := failoverTestServers(
		t, emptyContentResponse(), validAnalysisResponse())
	resolver := newFailoverResolver(t, primaryCfg, secondaryCfg)

	ia := newIntentAnalyzerWithConfig(IntentAnalyzerConfig{
		ModelConfig: primaryCfg,
		Resolver:    resolver,
		AliasName:   testClassifierAlias,
	}, llm.NewClient(primaryCfg), nil)

	analysis, err := ia.AnalyzeTrueIntent(context.Background(), "fix this bug please")
	if err != nil {
		t.Fatalf("expected success after rotation, got error: %v", err)
	}
	if analysis == nil || analysis.Goal != "fix bug" {
		t.Fatalf("expected goal 'fix bug', got %+v", analysis)
	}
}
