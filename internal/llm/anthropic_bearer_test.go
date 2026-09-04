package llm

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// bearerStubResolver returns a fixed token or error for bearer-mode tests.
type bearerStubResolver struct {
	token string
	err   error
}

func (s *bearerStubResolver) ResolveToken(_ context.Context, _ string) (string, error) {
	return s.token, s.err
}

// anthropicBearerServer serves a minimal valid Messages response while
// recording request headers for assertion.
type anthropicBearerServer struct {
	headers http.Header
	srv     *httptest.Server
}

func newAnthropicBearerServer(t *testing.T) *anthropicBearerServer {
	t.Helper()
	bs := &anthropicBearerServer{}
	bs.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bs.headers = r.Header.Clone()
		resp := map[string]any{
			"id":          "x",
			"type":        "message",
			"role":        "assistant",
			"model":       "m",
			"stop_reason": "end_turn",
			"content": []map[string]any{
				{"type": "text", "text": "hi"},
			},
			"usage": map[string]any{"input_tokens": 1, "output_tokens": 1},
		}
		b, err := json.Marshal(resp)
		if err != nil {
			t.Errorf("marshal fixture: %v", err)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(b); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(bs.srv.Close)
	return bs
}

func TestAnthropicBearer_NonStream(t *testing.T) {
	bs := newAnthropicBearerServer(t)
	cfg := &ModelConfig{
		ProviderID:    "anthropic",
		ModelID:       "claude-sonnet-4-5",
		BaseURL:       bs.srv.URL,
		APIKey:        "sk-should-not-be-used",
		OAuthProvider: "anthropic-sub",
	}
	client := NewAnthropicClient(cfg,
		WithAnthropicLogger(slog.Default()),
		WithAnthropicTokenResolver(&bearerStubResolver{token: "tok-1"}, "anthropic-sub"),
	)

	resp, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "hi" {
		t.Errorf("content %q", resp.Content)
	}

	if got := bs.headers.Get("Authorization"); got != "Bearer tok-1" {
		t.Errorf("authorization %q", got)
	}
	if got := bs.headers.Get("Anthropic-Beta"); got != "oauth-2025-04-20" {
		t.Errorf("anthropic-beta %q", got)
	}
	if got := bs.headers.Get("Anthropic-Version"); got == "" {
		t.Error("anthropic-version missing in bearer mode")
	}
	if got := bs.headers.Get("X-Api-Key"); got != "" {
		t.Errorf("x-api-key must be absent in bearer mode, got %q", got)
	}
}

func TestAnthropicBearer_ResolverError(t *testing.T) {
	bs := newAnthropicBearerServer(t)
	cfg := &ModelConfig{
		ProviderID:    "anthropic",
		ModelID:       "claude-sonnet-4-5",
		BaseURL:       bs.srv.URL,
		APIKey:        "sk-key",
		OAuthProvider: "anthropic-sub",
	}
	client := NewAnthropicClient(cfg,
		WithAnthropicTokenResolver(&bearerStubResolver{err: context.DeadlineExceeded}, "anthropic-sub"),
	)

	_, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}})
	if err == nil {
		t.Fatal("expected resolver error")
	}
	if !strings.Contains(err.Error(), "failed to resolve OAuth token") {
		t.Errorf("error %v", err)
	}
	var cerr *ClientError
	if !asClientError(err, &cerr) {
		t.Errorf("expected *ClientError, got %T", err)
	}
}

// asClientError wraps errors.As without importing errors twice in style.
func asClientError(err error, target **ClientError) bool {
	for err != nil {
		ce := &ClientError{}
		if errors.As(err, &ce) {
			*target = ce
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}

func TestAnthropicAPIKey_Regression(t *testing.T) {
	bs := newAnthropicBearerServer(t)
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
		BaseURL:    bs.srv.URL,
		APIKey:     "sk-classic",
	}
	client := NewAnthropicClient(cfg, WithAnthropicLogger(slog.Default()))

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	if got := bs.headers.Get("X-Api-Key"); got != "sk-classic" {
		t.Errorf("x-api-key %q", got)
	}
	if got := bs.headers.Get("Authorization"); got != "" {
		t.Errorf("authorization must be absent in api-key mode, got %q", got)
	}
	if got := bs.headers.Get("Anthropic-Beta"); got != "" {
		t.Errorf("anthropic-beta must be absent in api-key mode, got %q", got)
	}
}

func TestAnthropicBearer_OptionNilGuard(t *testing.T) {
	bs := newAnthropicBearerServer(t)
	cfg := &ModelConfig{
		ProviderID: "anthropic",
		ModelID:    "claude-sonnet-4-5",
		BaseURL:    bs.srv.URL,
		APIKey:     "sk-fallback",
	}
	client := NewAnthropicClient(cfg,
		WithAnthropicTokenResolver(nil, "ignored"),
	)

	if _, err := client.Chat(context.Background(), []ChatMessage{{Role: RoleUser, Content: "hello"}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	// Nil resolver ignored -> falls back to API key auth.
	if got := bs.headers.Get("X-Api-Key"); got != "sk-fallback" {
		t.Errorf("x-api-key %q", got)
	}
	if got := bs.headers.Get("Authorization"); got != "" {
		t.Errorf("authorization %q", got)
	}
}

func TestCreateChatterFor_AnthropicSub(t *testing.T) {
	chatter := createChatterFor(&ModelConfig{
		ProviderID:    "anthropic",
		BaseURL:       "https://api.anthropic.com",
		OAuthProvider: "anthropic-sub",
	}, nil, slog.Default(), &bearerStubResolver{token: "t"})
	if _, ok := chatter.(*AnthropicClient); !ok {
		t.Errorf("want *AnthropicClient, got %T", chatter)
	}
}

func TestGetProviderByID_AnthropicSub(t *testing.T) {
	p, ok := GetProviderByID("anthropic-sub")
	if !ok {
		t.Fatal("anthropic-sub not found in CanonicalProviders")
	}
	if p.AuthType != AuthOAuthDevice {
		t.Errorf("auth type %q", p.AuthType)
	}
	if p.Transport != TransportAnthropicMessages {
		t.Errorf("transport %q", p.Transport)
	}
	if p.BaseURL != "https://api.anthropic.com" {
		t.Errorf("base url %q", p.BaseURL)
	}
}
