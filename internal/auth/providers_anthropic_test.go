package auth

import "testing"

import "github.com/caimlas/meept/internal/llm"

func TestResolveProviderConfig_AnthropicSub(t *testing.T) {
	cfg, err := ResolveProviderConfig("anthropic-sub")
	if err != nil {
		t.Fatalf("ResolveProviderConfig: %v", err)
	}

	if cfg.ClientIDDefault != "9d1c250a-e61b-44d9-88ed-5944d1962f5e" {
		t.Errorf("ClientIDDefault = %q", cfg.ClientIDDefault)
	}
	if cfg.AuthorizeURL != "https://claude.ai/oauth/authorize" {
		t.Errorf("AuthorizeURL = %q", cfg.AuthorizeURL)
	}
	if cfg.TokenEP != "https://platform.claude.com/v1/oauth/token" {
		t.Errorf("TokenEP = %q", cfg.TokenEP)
	}
	wantScopes := []string{"org:create_api_key", "user:profile", "user:inference"}
	if len(cfg.Scopes) != len(wantScopes) {
		t.Fatalf("Scopes = %v, want %v", cfg.Scopes, wantScopes)
	}
	for i, s := range wantScopes {
		if cfg.Scopes[i] != s {
			t.Errorf("Scopes[%d] = %q, want %q", i, cfg.Scopes[i], s)
		}
	}
	if cfg.ProviderID != "anthropic-sub" {
		t.Errorf("ProviderID = %q", cfg.ProviderID)
	}
	if cfg.Flow != FlowPKCEPaste {
		t.Errorf("Flow = %q, want %q", cfg.Flow, FlowPKCEPaste)
	}
	if cfg.RefreshJSON {
		t.Errorf("RefreshJSON = true, want false")
	}
	if cfg.Transport != llm.TransportAnthropicMessages {
		t.Errorf("Transport = %q", cfg.Transport)
	}
	if cfg.BaseURL != "https://api.anthropic.com" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.VerifyHint != "authorize, then paste the code shown" {
		t.Errorf("VerifyHint = %q", cfg.VerifyHint)
	}
}
