package agent

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// modelCapture records the "model" field of every chat request a test server
// receives.
type modelCapture struct {
	mu     sync.Mutex
	models []string
	hits   int32
}

func (c *modelCapture) record(model string) {
	atomic.AddInt32(&c.hits, 1)
	c.mu.Lock()
	c.models = append(c.models, model)
	c.mu.Unlock()
}

func (c *modelCapture) last() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.models) == 0 {
		return ""
	}
	return c.models[len(c.models)-1]
}

func (c *modelCapture) count() int { return int(atomic.LoadInt32(&c.hits)) }

func newModelCaptureServer(cap *modelCapture) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cap.record(body.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`))
	}))
}

// resolvedAliasResolver builds a resolver whose "classifier" alias resolves to
// local/<modelRef> at baseURL, mirroring the daemon's wiring (the alias is the
// only component that chooses a model).
func resolvedAliasResolver(t *testing.T, baseURL, modelRef, modelName string) *llm.Resolver {
	t.Helper()
	cfg := &llm.ProvidersConfig{
		ModelAliases: map[string]llm.ModelAliasEntry{
			testClassifierAlias: {Models: []string{"local/" + modelRef}},
		},
		Providers: map[string]llm.ProviderConfig{
			"local": {
				API:     "openai",
				Options: llm.ProviderOptionsConfig{BaseURL: baseURL},
				Models:  map[string]llm.ModelDef{modelRef: {Name: modelName}},
			},
		},
	}
	return llm.NewResolver(cfg, nil)
}

// TestAgentLoop_ResolvedAliasModelReachesRequest_ProviderManager is the bug-1
// regression test for the seam the daemon actually wires: the loop's chatter is
// a *llm.ProviderManager (no single config to switch). The alias resolves to
// the LOCAL provider/model, but the manager's default health/priority order
// would pick the (healthy, higher-priority) cloud provider. The assertion is
// on what the servers actually received: the local endpoint must get the
// resolved model and the cloud provider must get nothing. Pre-fix, the
// resolver's decision was recorded and then dropped — the cloud provider
// served the call.
func TestAgentLoop_ResolvedAliasModelReachesRequest_ProviderManager(t *testing.T) {
	agnesCap := &modelCapture{}
	agnes := newModelCaptureServer(agnesCap)
	defer agnes.Close()
	localCap := &modelCapture{}
	local := newModelCaptureServer(localCap)
	defer local.Close()

	const resolvedModel = "LFM2.5-8B-A1B-Q4_K_M.gguf"
	resolver := resolvedAliasResolver(t, local.URL, "lfm-8b-q4", resolvedModel)

	pm := llm.NewProviderManager(llm.ProviderManagerConfig{
		Providers: []*llm.ModelConfig{
			{ProviderID: "agnes", ModelID: "agnes-2.5-flash", BaseURL: agnes.URL},
			{ProviderID: "local", ModelID: "lfm-8b-mlx-4bit", BaseURL: local.URL},
		},
		Logger: slog.New(slog.DiscardHandler),
	})

	loop := NewAgentLoop("sess-resolved-model", t.TempDir(),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(pm),
	)

	resp, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil)
	if err != nil {
		t.Fatalf("chatWithFailoverRaw: %v", err)
	}
	if resp == nil {
		t.Fatal("nil response")
	}

	if agnesCap.count() != 0 {
		t.Errorf("agnes (manager default) received %d calls, want 0: the resolved alias model must reach the request", agnesCap.count())
	}
	if localCap.count() != 1 {
		t.Fatalf("local received %d calls, want 1", localCap.count())
	}
	if got := localCap.last(); got != resolvedModel {
		t.Errorf("local request model = %q, want the resolved alias model %q", got, resolvedModel)
	}
}

// TestAgentLoop_ResolvedAliasModelReachesRequest_ConcreteClient keeps the
// concrete-*llm.Client seam working: the loop retargets the client to the
// resolved alias model, so the request carries it. (Regression guard: the
// legacy SwitchModel path must not be dropped by the request-scoped fix.)
func TestAgentLoop_ResolvedAliasModelReachesRequest_ConcreteClient(t *testing.T) {
	cap := &modelCapture{}
	server := newModelCaptureServer(cap)
	defer server.Close()

	resolver := newFailoverResolver(t,
		&llm.ModelConfig{BaseURL: server.URL, ModelID: "m1", ProviderID: "p1"},
		&llm.ModelConfig{BaseURL: server.URL, ModelID: "m2", ProviderID: "p2"},
	)
	client := llm.NewClient(&llm.ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "p1",
		ModelID:    "configured-default",
	})
	loop := NewAgentLoop("sess-resolved-model-client", t.TempDir(),
		WithResolver(resolver),
		WithModelRef(testClassifierAlias),
		WithLLMChatter(client),
	)

	if _, err := loop.chatWithFailoverRaw(context.Background(),
		[]llm.ChatMessage{{Role: llm.RoleUser, Content: "hello"}}, nil); err != nil {
		t.Fatalf("chatWithFailoverRaw: %v", err)
	}
	if got := cap.last(); got != "m1" {
		t.Errorf("request model = %q, want the resolved alias model %q", got, "m1")
	}
}
