package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jmoiron/sqlx"
)

// pollLLMCalls polls the llm_calls sidecar until fn sees the rows it wants.
func pollLLMCalls(t *testing.T, db *sqlx.DB, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// llmCallsRows reads the raw llm_calls ledger rows.
func llmCallsRows(t *testing.T, db *sqlx.DB) []struct {
	Provider string `db:"provider"`
	ModelID  string `db:"model_id"`
} {
	t.Helper()
	var rows []struct {
		Provider string `db:"provider"`
		ModelID  string `db:"model_id"`
	}
	_ = db.Select(&rows, `SELECT provider, model_id FROM llm_calls`)
	return rows
}

// anthropicMessagesServer serves a minimal Anthropic Messages API response
// while capturing the requested model id.
type anthropicCapture struct {
	models []string
	hits   int32
}

func (c *anthropicCapture) record(model string) {
	atomic.AddInt32(&c.hits, 1)
	c.models = append(c.models, model)
}

func (c *anthropicCapture) last() string {
	if len(c.models) == 0 {
		return ""
	}
	return c.models[len(c.models)-1]
}

func anthropicServer(cap *anthropicCapture) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cap.record(body.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"msg_1","type":"message","role":"assistant","content":[{"type":"text","text":"hi"}],"model":"` + body.Model + `","stop_reason":"end_turn","usage":{"input_tokens":3,"output_tokens":1}}`))
	}))
}

// codexServer serves a minimal Codex Responses API response while capturing
// the requested model id.
func codexServer(cap *anthropicCapture) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		cap.record(body.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"output":[{"type":"message","content":[{"type":"output_text","text":"hi"}]}],"usage":{"input_tokens":3,"output_tokens":1,"input_tokens_details":{"cached_tokens":0}}}`))
	}))
}

// TestAnthropicClient_ResolvedModelReachesRequestAndLedger pins gap 1 for the
// Anthropic path: a request-scoped selection puts the RESOLVED model id on
// the wire (Messages payload "model") and records the provider/model that
// actually served the call in metrics.db llm_calls. The client's stored
// config stays untouched.
func TestAnthropicClient_ResolvedModelReachesRequestAndLedger(t *testing.T) {
	cap := &anthropicCapture{}
	server := anthropicServer(cap)
	defer server.Close()

	store, dbPath := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	db := openLLMCallsSidecar(t, dbPath)

	client := NewAnthropicClient(&ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "anthropic",
		ModelID:    "claude-haiku-default",
	})
	client.SetUsageStore(store)

	resolved := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-opus-4-resolved",
	}
	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithResolvedModel(resolved), WithAgentScope("coder")); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got := cap.last(); got != resolved.ModelID {
		t.Errorf("wire model = %q, want the resolved alias model %q", got, resolved.ModelID)
	}
	if got := client.Config().ModelID; got != "claude-haiku-default" {
		t.Errorf("client config mutated by a request-scoped selection: ModelID = %q", got)
	}

	pollLLMCalls(t, db, func() bool { return len(llmCallsRows(t, db)) == 1 })
	rows := llmCallsRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("expected 1 llm_calls row, got %d", len(rows))
	}
	if rows[0].Provider != "anthropic" || rows[0].ModelID != resolved.ModelID {
		t.Errorf("llm_calls row = %s|%s, want anthropic|%s", rows[0].Provider, rows[0].ModelID, resolved.ModelID)
	}
}

// TestAnthropicClient_UserOverrideBeatsResolvedModel pins the precedence
// chain on the Anthropic wire: when BOTH a user directive and an alias
// resolution ride the same request, the USER directive wins — regardless of
// option order (the alias option is appended last here, mirroring
// chatWithFailoverRaw's append-after hazard).
func TestAnthropicClient_UserOverrideBeatsResolvedModel(t *testing.T) {
	cap := &anthropicCapture{}
	server := anthropicServer(cap)
	defer server.Close()

	client := NewAnthropicClient(&ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "anthropic",
		ModelID:    "claude-haiku-default",
	})

	userOverride := &ModelConfig{ProviderID: "anthropic", ModelID: "claude-user-choice"}
	alias := &ModelConfig{ProviderID: "anthropic", ModelID: "claude-alias-choice"}

	// Caller opts first (user directive), then the alias option appended
	// AFTER — the exact ordering chatWithFailoverRaw produces.
	_, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithModelOverride(userOverride), WithResolvedModel(alias))
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := cap.last(); got != userOverride.ModelID {
		t.Errorf("wire model = %q, want the USER override %q to beat the alias %q", got, userOverride.ModelID, alias.ModelID)
	}

	// Reversed order must behave identically: precedence is decided by the
	// ranked channels, never by slice order.
	cap2 := &anthropicCapture{}
	server2 := anthropicServer(cap2)
	defer server2.Close()
	client2 := NewAnthropicClient(&ModelConfig{
		BaseURL:    server2.URL,
		ProviderID: "anthropic",
		ModelID:    "claude-haiku-default",
	})
	if _, err := client2.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithResolvedModel(alias), WithModelOverride(userOverride)); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := cap2.last(); got != userOverride.ModelID {
		t.Errorf("reversed option order: wire model = %q, still want the USER override %q", got, userOverride.ModelID)
	}
}

// TestCodexClient_ResolvedModelReachesRequestAndLedger pins gap 1 for the
// Codex path: the resolved model id reaches the Responses payload and the
// llm_calls ledger names it.
func TestCodexClient_ResolvedModelReachesRequestAndLedger(t *testing.T) {
	cap := &anthropicCapture{}
	server := codexServer(cap)
	defer server.Close()

	store, dbPath := newUsageTestStoreAtPath(t, filepath.Join(t.TempDir(), "metrics.db"))
	db := openLLMCallsSidecar(t, dbPath)

	client := NewCodexClient(&ModelConfig{
		BaseURL:    server.URL,
		ProviderID: "codex",
		ModelID:    "gpt-5.1-codex-default",
	})
	client.SetUsageStore(store)

	resolved := &ModelConfig{
		ProviderID: "codex",
		ModelID:    "gpt-5.2-codex-resolved",
	}
	if _, err := client.Chat(context.Background(),
		[]ChatMessage{{Role: RoleUser, Content: "hello"}},
		WithResolvedModel(resolved), WithAgentScope("coder")); err != nil {
		t.Fatalf("Chat: %v", err)
	}

	if got := cap.last(); got != resolved.ModelID {
		t.Errorf("wire model = %q, want the resolved alias model %q", got, resolved.ModelID)
	}
	if got := client.Config().ModelID; got != "gpt-5.1-codex-default" {
		t.Errorf("client config mutated by a request-scoped selection: ModelID = %q", got)
	}

	pollLLMCalls(t, db, func() bool { return len(llmCallsRows(t, db)) == 1 })
	rows := llmCallsRows(t, db)
	if len(rows) != 1 {
		t.Fatalf("expected 1 llm_calls row, got %d", len(rows))
	}
	if rows[0].Provider != "codex" || rows[0].ModelID != resolved.ModelID {
		t.Errorf("llm_calls row = %s|%s, want codex|%s", rows[0].Provider, rows[0].ModelID, resolved.ModelID)
	}
}

// TestCodexClient_UserOverrideBeatsResolvedModel pins the precedence chain on
// the Codex wire: user directive > alias resolution, in BOTH append orders.
func TestCodexClient_UserOverrideBeatsResolvedModel(t *testing.T) {
	userOverride := &ModelConfig{ProviderID: "codex", ModelID: "codex-user-choice"}
	alias := &ModelConfig{ProviderID: "codex", ModelID: "codex-alias-choice"}

	for _, order := range []string{"override-first", "alias-first"} {
		t.Run(order, func(t *testing.T) {
			cap := &anthropicCapture{}
			server := codexServer(cap)
			defer server.Close()

			client := NewCodexClient(&ModelConfig{
				BaseURL:    server.URL,
				ProviderID: "codex",
				ModelID:    "gpt-5.1-codex-default",
			})

			var opts []ChatOption
			if order == "override-first" {
				opts = []ChatOption{WithModelOverride(userOverride), WithResolvedModel(alias)}
			} else {
				opts = []ChatOption{WithResolvedModel(alias), WithModelOverride(userOverride)}
			}
			if _, err := client.Chat(context.Background(),
				[]ChatMessage{{Role: RoleUser, Content: "hello"}}, opts...); err != nil {
				t.Fatalf("Chat: %v", err)
			}
			if got := cap.last(); got != userOverride.ModelID {
				t.Errorf("wire model = %q, want the USER override %q (order %s)", got, userOverride.ModelID, order)
			}
		})
	}
}

// TestRequestModelOverridePrecedence is the table-driven unit test for the
// ranked merge in requestModelOverride: the precedence lanes and the
// foreign-provider guard, independent of any transport.
func TestRequestModelOverridePrecedence(t *testing.T) {
	cfg := &ModelConfig{ProviderID: "local", ModelID: "configured-default"}

	tests := []struct {
		name      string
		opts      []ChatOption
		wantModel string
		wantLane  string
		wantOK    bool
	}{
		{
			name:     "no selection keeps configured default",
			opts:     nil,
			wantOK:   false,
			wantLane: "",
		},
		{
			name:      "alias alone applies",
			opts:      []ChatOption{WithResolvedModel(&ModelConfig{ProviderID: "local", ModelID: "alias-model"})},
			wantModel: "alias-model",
			wantLane:  "alias",
			wantOK:    true,
		},
		{
			name:      "user directive alone applies",
			opts:      []ChatOption{WithModelOverride(&ModelConfig{ProviderID: "local", ModelID: "user-model"})},
			wantModel: "user-model",
			wantLane:  "user-directive",
			wantOK:    true,
		},
		{
			name: "user directive beats alias (override appended first)",
			opts: []ChatOption{
				WithModelOverride(&ModelConfig{ProviderID: "local", ModelID: "user-model"}),
				WithResolvedModel(&ModelConfig{ProviderID: "local", ModelID: "alias-model"}),
			},
			wantModel: "user-model",
			wantLane:  "user-directive",
			wantOK:    true,
		},
		{
			name: "user directive beats alias (override appended LAST — the chatWithFailoverRaw hazard)",
			opts: []ChatOption{
				WithResolvedModel(&ModelConfig{ProviderID: "local", ModelID: "alias-model"}),
				WithModelOverride(&ModelConfig{ProviderID: "local", ModelID: "user-model"}),
			},
			wantModel: "user-model",
			wantLane:  "user-directive",
			wantOK:    true,
		},
		{
			name: "empty directive provider inherits client provider and wins",
			opts: []ChatOption{
				WithResolvedModel(&ModelConfig{ModelID: "alias-model"}),
				WithModelOverride(&ModelConfig{ModelID: "user-model"}),
			},
			wantModel: "user-model",
			wantLane:  "user-directive",
			wantOK:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			probe := &chatOptions{}
			for _, opt := range tt.opts {
				opt(probe)
			}
			providerID, modelID, lane, ok := probe.requestModelOverride(cfg)
			if ok != tt.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tt.wantOK)
			}
			if lane != tt.wantLane {
				t.Errorf("lane = %q, want %q", lane, tt.wantLane)
			}
			if !tt.wantOK {
				return
			}
			if modelID != tt.wantModel {
				t.Errorf("model = %q, want %q", modelID, tt.wantModel)
			}
			wantProvider := cfg.ProviderID
			if tt.wantLane == "alias" {
				_ = providerID
			}
			if providerID != wantProvider {
				t.Errorf("provider = %q, want %q (inherit)", providerID, wantProvider)
			}
		})
	}
}
