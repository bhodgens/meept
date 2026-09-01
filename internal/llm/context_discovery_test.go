package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// ---------------------------------------------------------------------------
// Task 1: fetchers + parsers (fixture servers per provider)
// ---------------------------------------------------------------------------

func TestFetchOllamaContexts(t *testing.T) {
	// Ollama flow: /api/tags lists models, /api/show returns per-model
	// context length. The fixture covers BOTH spec shapes: a top-level
	// context_length int and the nested model_info.* variant.
	mux := http.NewServeMux()
	mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"models":[{"name":"llama3.2"},{"name":"qwen2.5-coder"},{"name":""}]}`)
	})
	mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		// Decode the requested model so each fixture exercises one shape.
		var req struct {
			Model string `json:"model"`
		}
		_ = mustDecodeBody(r, &req)
		switch req.Model {
		case "llama3.2":
			fmt.Fprint(w, `{"context_length":131072}`)
		case "qwen2.5-coder":
			fmt.Fprint(w, `{"model_info":{"general.architecture":"qwen2","qwen2.context_length":32768}}`)
		default:
			fmt.Fprint(w, `{}`)
		}
	})
	server := httptest.NewServer(mux)
	defer server.Close()

	got, err := FetchOllamaContexts(context.Background(), server.Client(), slog.Default(), server.URL, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 discovered contexts, got %d: %v", len(got), got)
	}
	if v := got["ollama/llama3.2"]; v != 131072 {
		t.Errorf("ollama/llama3.2: expected 131072, got %d", v)
	}
	if v := got["ollama/qwen2.5-coder"]; v != 32768 {
		t.Errorf("ollama/qwen2.5-coder: expected 32768 (model_info variant), got %d", v)
	}
}

func TestFetchOllamaContexts_MalformedResponses(t *testing.T) {
	// Spec Task 1: malformed JSON -> empty map + logged warning, no error.
	t.Run("tags malformed", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"models": [not json}`)
		}))
		defer server.Close()
		got, err := FetchOllamaContexts(context.Background(), server.Client(), slog.Default(), server.URL, "")
		if err != nil {
			t.Fatalf("malformed tags JSON must not error, got %v", err)
		}
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %v", got)
		}
	})
	t.Run("show malformed skips model", func(t *testing.T) {
		mux := http.NewServeMux()
		mux.HandleFunc("/api/tags", func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"models":[{"name":"broken"},{"name":"good"}]}`)
		})
		mux.HandleFunc("/api/show", func(w http.ResponseWriter, r *http.Request) {
			var req struct {
				Model string `json:"model"`
			}
			_ = mustDecodeBody(r, &req)
			if req.Model == "broken" {
				fmt.Fprint(w, `{oops`)
				return
			}
			fmt.Fprint(w, `{"context_length":4096}`)
		})
		server := httptest.NewServer(mux)
		defer server.Close()
		got, err := FetchOllamaContexts(context.Background(), server.Client(), slog.Default(), server.URL, "")
		if err != nil {
			t.Fatalf("malformed show JSON must not error, got %v", err)
		}
		if len(got) != 1 || got["ollama/good"] != 4096 {
			t.Fatalf("expected only ollama/good=4096, got %v", got)
		}
	})
}

func TestFetchOpenRouterContexts(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"data":[
			{"id":"anthropic/claude-sonnet-4.6","context_length":200000},
			{"id":"vendor/no-ctx"},
			{"id":"vendor/zero","context_length":0}
		]}`)
	}))
	defer server.Close()

	got, err := FetchOpenRouterContexts(context.Background(), server.Client(), slog.Default(), server.URL, "sk-test")
	if err != nil {
		t.Fatal(err)
	}
	// Keys are registry ids: openrouter/<openrouter-id>.
	if len(got) != 1 {
		t.Fatalf("expected 1 usable entry, got %d: %v", len(got), got)
	}
	if v := got["openrouter/anthropic/claude-sonnet-4.6"]; v != 200000 {
		t.Errorf("expected 200000, got %d", v)
	}
}

func TestFetchOpenRouterContexts_Malformed(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[broken`)
	}))
	defer server.Close()
	got, err := FetchOpenRouterContexts(context.Background(), server.Client(), slog.Default(), server.URL, "")
	if err != nil {
		t.Fatalf("malformed JSON must not error, got %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("expected empty map, got %v", got)
	}
}

func TestFetchLocalModelsContexts(t *testing.T) {
	t.Run("props n_ctx applied per registered model", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.URL.Path != "/props" {
				http.NotFound(w, r)
				return
			}
			fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":8192}}`)
		}))
		defer server.Close()

		got := FetchLocalModelsContexts(context.Background(), server.Client(), slog.Default(), server.URL, []string{"org--repo-file", "second"})
		if len(got) != 2 {
			t.Fatalf("expected 2 entries, got %v", got)
		}
		if got["local-models/org--repo-file"] != 8192 || got["local-models/second"] != 8192 {
			t.Fatalf("expected n_ctx applied to every local-models key, got %v", got)
		}
	})
	t.Run("n_ctx zero yields empty map", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{"default_generation_settings":{"n_ctx":0}}`)
		}))
		defer server.Close()
		got := FetchLocalModelsContexts(context.Background(), server.Client(), slog.Default(), server.URL, []string{"m"})
		if len(got) != 0 {
			t.Fatalf("expected empty map for n_ctx=0, got %v", got)
		}
	})
	t.Run("malformed props yields empty map", func(t *testing.T) {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			fmt.Fprint(w, `{nope`)
		}))
		defer server.Close()
		got := FetchLocalModelsContexts(context.Background(), server.Client(), slog.Default(), server.URL, []string{"m"})
		if len(got) != 0 {
			t.Fatalf("expected empty map, got %v", got)
		}
	})
}

// ---------------------------------------------------------------------------
// Task 2: precedence merge — ALL six cells (master Contract 3)
// ---------------------------------------------------------------------------

// sixCellFixture builds a resolver whose provider has one model per
// precedence class, plus a catalog entry for the "catalog-only" class.
const ctxDiscTestProvider = "cdtest"

func sixCellFixture(t *testing.T) (*Resolver, *ContextDiscovery) {
	t.Helper()

	// Catalog entry backing the "catalog-only" cell. Injected into the
	// package catalog and removed when the test ends.
	ProviderModels[ctxDiscTestProvider] = []ModelCatalogEntry{
		{ModelID: "catalogonly", Name: "Catalog Only", ProviderID: ctxDiscTestProvider, ContextWindow: 4096},
	}
	t.Cleanup(func() { delete(ProviderModels, ctxDiscTestProvider) })

	cfg := &ProvidersConfig{
		Providers: map[string]ProviderConfig{
			ctxDiscTestProvider: {
				Options: ProviderOptionsConfig{BaseURL: "http://127.0.0.1:1"},
				Models: map[string]ModelDef{
					// explicit models.json5 context_limit — wins ALWAYS.
					"explicit": {ContextLimit: 111},
					// no explicit value, catalog has one — discovered wins
					// only under allow_context_override.
					"catalogonly": {},
					// no explicit value, no catalog value — discovered wins.
					"zero": {},
				},
			},
		},
	}
	resolver := NewResolver(cfg, nil)

	discovery := NewContextDiscovery(ContextDiscoveryConfig{Enabled: true}, nil)
	discovery.SetEndpoint(ctxDiscTestProvider, "http://endpoint.invalid", "")
	discovery.RegisterFetcher(ctxDiscTestProvider, func(ctx context.Context, baseURL, apiKey string) (map[string]int, error) {
		return map[string]int{
			ctxDiscTestProvider + "/explicit":    222,
			ctxDiscTestProvider + "/catalogonly": 222,
			ctxDiscTestProvider + "/zero":        222,
		}, nil
	})
	discovery.SetResolver(resolver)
	return resolver, discovery
}

func resolverModelByKey(t *testing.T, r *Resolver, key string) *ModelConfig {
	t.Helper()
	for _, m := range r.AllModels() {
		if m.ProviderID+"/"+m.ModelID == key || m.CatalogRef == key {
			return m
		}
	}
	t.Fatalf("resolver has no model %q", key)
	return nil
}

func TestContextDiscoverySync_SixPrecedenceCells(t *testing.T) {
	cases := []struct {
		name           string
		override       bool
		explicit       int
		catalogonly    int
		zero           int
		wantExplicit   int
		wantCatalogony int
		wantZero       int
	}{
		{
			name:           "override off",
			override:       false,
			wantExplicit:   111, // explicit config wins ALWAYS
			wantCatalogony: 0,   // non-zero catalog value: discovered skipped
			wantZero:       222, // absent value accepts discovery
		},
		{
			name:           "override on",
			override:       true,
			wantExplicit:   111, // explicit config STILL wins
			wantCatalogony: 222, // non-zero catalog yields under override
			wantZero:       222,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resolver, discovery := sixCellFixture(t)
			discovery.mu.Lock()
			discovery.cfg.AllowContextOverride = tc.override
			discovery.mu.Unlock()

			if err := discovery.Sync(context.Background()); err != nil {
				t.Fatal(err)
			}
			if got := resolverModelByKey(t, resolver, ctxDiscTestProvider+"/explicit").ContextLimit; got != tc.wantExplicit {
				t.Errorf("explicit: want %d, got %d", tc.wantExplicit, got)
			}
			if got := resolverModelByKey(t, resolver, ctxDiscTestProvider+"/catalogonly").ContextLimit; got != tc.wantCatalogony {
				t.Errorf("catalogonly: want %d, got %d", tc.wantCatalogony, got)
			}
			if got := resolverModelByKey(t, resolver, ctxDiscTestProvider+"/zero").ContextLimit; got != tc.wantZero {
				t.Errorf("zero: want %d, got %d", tc.wantZero, got)
			}

			// Display catalog refresh follows the same rule so the TUI
			// picker agrees with runtime.
			entry, ok := GetModel(ctxDiscTestProvider, "catalogonly")
			if !ok {
				t.Fatal("catalog entry vanished")
			}
			wantCatalogDisplay := 4096
			if tc.override {
				wantCatalogDisplay = 222
			}
			if entry.ContextWindow != wantCatalogDisplay {
				t.Errorf("display catalog: want %d, got %d", wantCatalogDisplay, entry.ContextWindow)
			}
		})
	}
}

func TestContextDiscoverySync_ErrorTolerance(t *testing.T) {
	resolver, discovery := sixCellFixture(t)
	discovery.RegisterFetcher("broken", func(ctx context.Context, baseURL, apiKey string) (map[string]int, error) {
		return nil, fmt.Errorf("boom")
	})
	discovery.SetEndpoint("broken", "http://endpoint.invalid", "")

	// One provider failing must not fail Sync nor block the healthy one.
	if err := discovery.Sync(context.Background()); err != nil {
		t.Fatalf("Sync must tolerate per-provider failure, got %v", err)
	}
	if got := resolverModelByKey(t, resolver, ctxDiscTestProvider+"/zero").ContextLimit; got != 222 {
		t.Fatalf("healthy provider not applied: %d", got)
	}
}

func TestContextDiscoverySync_DeltaLogging(t *testing.T) {
	_, discovery := sixCellFixture(t)

	var buf syncBuffer
	discovery.mu.Lock()
	discovery.logger = slog.New(slog.NewTextHandler(&buf, nil))
	discovery.mu.Unlock()

	if err := discovery.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	// Deltas logged with provider, model, from, to.
	for _, want := range []string{"context window updated", "provider=cdtest", "model=zero", "from=0", "to=222"} {
		if !strings.Contains(out, want) {
			t.Errorf("delta log missing %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "model=explicit") {
		t.Errorf("explicit model must not be logged as updated:\n%s", out)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func TestContextDiscoverySync_NoFetchersNoop(t *testing.T) {
	// A syncer with no fetchers (and, in production, disabled config)
	// performs no work and never errors.
	discovery := NewContextDiscovery(ContextDiscoveryConfig{}, nil)
	// Client aimed at an unroutable port: any accidental network call fails loudly.
	discovery.client = &http.Client{Timeout: 50 * time.Millisecond}
	if err := discovery.Sync(context.Background()); err != nil {
		t.Fatalf("no-op Sync must return nil, got %v", err)
	}
}

// ---------------------------------------------------------------------------
// Start: ticker loop; stop via ctx
// ---------------------------------------------------------------------------

func TestContextDiscoveryStart_TickerLoop(t *testing.T) {
	discovery := NewContextDiscovery(ContextDiscoveryConfig{Enabled: true, Interval: 20 * time.Millisecond}, nil)

	var mu sync.Mutex
	calls := 0
	discovery.RegisterFetcher("tick", func(ctx context.Context, baseURL, apiKey string) (map[string]int, error) {
		mu.Lock()
		calls++
		mu.Unlock()
		return map[string]int{}, nil
	})
	discovery.SetEndpoint("tick", "http://endpoint.invalid", "")

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	discovery.Start(ctx)

	deadline := time.Now().Add(2 * time.Second)
	for {
		mu.Lock()
		n := calls
		mu.Unlock()
		if n >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("ticker loop did not re-sync")
		}
		time.Sleep(5 * time.Millisecond)
	}

	cancel()
	mu.Lock()
	after := calls
	mu.Unlock()
	time.Sleep(80 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if calls != after {
		t.Fatalf("loop kept running after ctx cancel: %d -> %d", after, calls)
	}
}

// ---------------------------------------------------------------------------
// Write path: context_firewall.go:353 reads f.model.ContextLimit — the
// firewall's model pointer must observe SetContextLimits mutations.
// ---------------------------------------------------------------------------

func TestResolverSetContextLimits_FirewallConsumerSeesUpdate(t *testing.T) {
	resolver, discovery := sixCellFixture(t)

	model := resolverModelByKey(t, resolver, ctxDiscTestProvider+"/zero")
	firewall := NewContextFirewall(
		&stubChatter{resp: &Response{Content: "ok"}},
		model,
		ContextFirewallConfig{Enabled: true},
		nil, slog.Default(), nil,
	)

	if err := discovery.Sync(context.Background()); err != nil {
		t.Fatal(err)
	}
	if model.ContextLimit != 222 {
		t.Fatalf("resolver model not updated: %d", model.ContextLimit)
	}
	// Same pointer: the firewall (and every per-call modelConfigFrom copy
	// downstream of these pointers) observes the fresh value.
	if firewall.model.ContextLimit != 222 {
		t.Fatalf("firewall does not observe updated ContextLimit: %d", firewall.model.ContextLimit)
	}
}

// mustDecodeBody is a test helper for fixture handlers.
func mustDecodeBody(r *http.Request, v any) error {
	return json.NewDecoder(r.Body).Decode(v)
}
