package auth

import (
	"os"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestOAuthProviders_ContainsGitHubModels(t *testing.T) {
	cfg, ok := OAuthProviders["github-models"]
	if !ok {
		t.Fatal("missing github-models provider")
	}
	if cfg.ProviderID != "github-models" {
		t.Errorf("ProviderID = %q, want %q", cfg.ProviderID, "github-models")
	}
	if cfg.Transport != llm.TransportOpenAIChat {
		t.Errorf("Transport = %q, want %q", cfg.Transport, llm.TransportOpenAIChat)
	}
	if cfg.BaseURL != "https://models.github.ai/inference" {
		t.Errorf("BaseURL = %q, want %q", cfg.BaseURL, "https://models.github.ai/inference")
	}
	if cfg.ClientSecretDefault != "" {
		t.Error("GitHub should be a public client (no client secret)")
	}
	if cfg.ClientIDEnvVar != "MEEPT_GITHUB_CLIENT_ID" {
		t.Errorf("ClientIDEnvVar = %q, want %q", cfg.ClientIDEnvVar, "MEEPT_GITHUB_CLIENT_ID")
	}
	if len(cfg.Scopes) != 1 || cfg.Scopes[0] != "models:read" {
		t.Errorf("Scopes = %v, want [models:read]", cfg.Scopes)
	}
	if cfg.ExtraHeaders == nil || cfg.ExtraHeaders["Accept"] != "application/vnd.github+json" {
		t.Error("missing GitHub API headers")
	}
}

func TestOAuthProviders_ContainsGoogleOAuth(t *testing.T) {
	cfg, ok := OAuthProviders["google-oauth"]
	if !ok {
		t.Fatal("missing google-oauth provider")
	}
	if cfg.ProviderID != "google-oauth" {
		t.Errorf("ProviderID = %q, want %q", cfg.ProviderID, "google-oauth")
	}
	if cfg.Transport != llm.TransportOpenAIChat {
		t.Errorf("Transport = %q, want %q", cfg.Transport, llm.TransportOpenAIChat)
	}
	if cfg.BaseURL != "https://generativelanguage.googleapis.com/v1beta/openai" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.ClientSecretDefault == "" {
		t.Error("Google requires a client secret")
	}
	if len(cfg.Scopes) != 1 {
		t.Errorf("Scopes = %v, want 1 scope", cfg.Scopes)
	}
}

func TestResolveProviderConfig_Unknown(t *testing.T) {
	_, err := ResolveProviderConfig("nonexistent-provider")
	if err == nil {
		t.Fatal("expected error for unknown provider")
	}
}

func TestResolveProviderConfig_NoEnvOverride(t *testing.T) {
	cfg, err := ResolveProviderConfig("github-models")
	if err != nil {
		t.Fatalf("ResolveProviderConfig: %v", err)
	}
	// Should return the default client ID.
	if cfg.ClientIDDefault != OAuthProviders["github-models"].ClientIDDefault {
		t.Errorf("ClientIDDefault not preserved without env override")
	}
}

func TestResolveProviderConfig_EnvOverride(t *testing.T) {
	origVal := os.Getenv("MEEPT_GITHUB_CLIENT_ID")
	defer os.Setenv("MEEPT_GITHUB_CLIENT_ID", origVal)

	os.Setenv("MEEPT_GITHUB_CLIENT_ID", "env-client-id-123")

	cfg, err := ResolveProviderConfig("github-models")
	if err != nil {
		t.Fatalf("ResolveProviderConfig: %v", err)
	}
	if cfg.ClientIDDefault != "env-client-id-123" {
		t.Errorf("ClientIDDefault = %q, want %q", cfg.ClientIDDefault, "env-client-id-123")
	}
}

func TestResolveProviderConfig_ClientSecretEnvOverride(t *testing.T) {
	origVal := os.Getenv("MEEPT_GOOGLE_CLIENT_SECRET")
	defer os.Setenv("MEEPT_GOOGLE_CLIENT_SECRET", origVal)

	os.Setenv("MEEPT_GOOGLE_CLIENT_SECRET", "env-secret-456")

	cfg, err := ResolveProviderConfig("google-oauth")
	if err != nil {
		t.Fatalf("ResolveProviderConfig: %v", err)
	}
	if cfg.ClientSecretDefault != "env-secret-456" {
		t.Errorf("ClientSecretDefault = %q, want %q", cfg.ClientSecretDefault, "env-secret-456")
	}
}

func TestOAuthProviderConfig_DeviceFlowConfig(t *testing.T) {
	cfg := OAuthProviderConfig{
		ClientIDDefault:     "my-client-id",
		ClientSecretDefault: "my-client-secret",
		DeviceEP:            "https://example.com/device",
		TokenEP:             "https://example.com/token",
		Scopes:              []string{"scope1", "scope2"},
	}

	flowCfg := cfg.DeviceFlowConfig()
	if flowCfg.ClientID != "my-client-id" {
		t.Errorf("ClientID = %q, want %q", flowCfg.ClientID, "my-client-id")
	}
	if flowCfg.ClientSecret != "my-client-secret" {
		t.Errorf("ClientSecret = %q, want %q", flowCfg.ClientSecret, "my-client-secret")
	}
	if flowCfg.DeviceEP != "https://example.com/device" {
		t.Errorf("DeviceEP = %q", flowCfg.DeviceEP)
	}
	if flowCfg.TokenEP != "https://example.com/token" {
		t.Errorf("TokenEP = %q", flowCfg.TokenEP)
	}
	if len(flowCfg.Scopes) != 2 {
		t.Errorf("Scopes = %v, want 2", flowCfg.Scopes)
	}
}

func TestRegisteredProviders(t *testing.T) {
	providers := RegisteredProviders()
	if len(providers) != 3 {
		t.Errorf("expected 3 providers, got %d", len(providers))
	}

	seen := map[string]bool{}
	for _, p := range providers {
		seen[p] = true
	}
	if !seen["github-models"] {
		t.Error("missing github-models")
	}
	if !seen["google-oauth"] {
		t.Error("missing google-oauth")
	}
	if !seen["google-calendar"] {
		t.Error("missing google-calendar")
	}
}
