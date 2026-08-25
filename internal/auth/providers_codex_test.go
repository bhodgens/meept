package auth

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

func TestResolveProviderConfigOpenAICodex(t *testing.T) {
	cfg, err := ResolveProviderConfig("openai-codex")
	if err != nil {
		t.Fatalf("ResolveProviderConfig: %v", err)
	}

	if cfg.ClientIDDefault != "app_EMoamEEZ73f0CkXaXp7hrann" {
		t.Errorf("ClientIDDefault = %q, want app_EMoamEEZ73f0CkXaXp7hrann", cfg.ClientIDDefault)
	}
	if cfg.DeviceUserCodeEP != "https://auth.openai.com/api/accounts/deviceauth/usercode" {
		t.Errorf("DeviceUserCodeEP = %q", cfg.DeviceUserCodeEP)
	}
	if cfg.DevicePollEP != "https://auth.openai.com/api/accounts/deviceauth/token" {
		t.Errorf("DevicePollEP = %q", cfg.DevicePollEP)
	}
	if cfg.TokenEP != "https://auth.openai.com/oauth/token" {
		t.Errorf("TokenEP = %q", cfg.TokenEP)
	}
	if cfg.Flow != FlowDeviceCodex {
		t.Errorf("Flow = %q, want %q", cfg.Flow, FlowDeviceCodex)
	}
	if cfg.BaseURL != "https://chatgpt.com/backend-api/codex" {
		t.Errorf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Transport != llm.TransportCodexResponses {
		t.Errorf("Transport = %q, want %q", cfg.Transport, llm.TransportCodexResponses)
	}
	if cfg.ProviderID != "openai-codex" {
		t.Errorf("ProviderID = %q", cfg.ProviderID)
	}
}
