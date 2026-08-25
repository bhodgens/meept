package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestXaiOAuthProviderEntry(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveProviderConfig("xai-oauth")
	if err != nil {
		t.Fatalf("ResolveProviderConfig(xai-oauth): %v", err)
	}

	if cfg.ClientIDDefault != "b1a00492-073a-47ea-816f-4c329264a828" {
		t.Errorf("ClientIDDefault = %q, want b1a00492-073a-47ea-816f-4c329264a828", cfg.ClientIDDefault)
	}
	if cfg.DeviceEP != "https://auth.x.ai/oauth2/device/code" {
		t.Errorf("DeviceEP = %q, want https://auth.x.ai/oauth2/device/code", cfg.DeviceEP)
	}
	if cfg.DiscoveryURL != "https://auth.x.ai/.well-known/openid-configuration" {
		t.Errorf("DiscoveryURL = %q, want https://auth.x.ai/.well-known/openid-configuration", cfg.DiscoveryURL)
	}
	if cfg.BaseURL != "https://api.x.ai/v1" {
		t.Errorf("BaseURL = %q, want https://api.x.ai/v1", cfg.BaseURL)
	}

	wantScopes := "openid profile email offline_access grok-cli:access api:access"
	if got := strings.Join(cfg.Scopes, " "); got != wantScopes {
		t.Errorf("Scopes = %q, want %q", got, wantScopes)
	}

	if cfg.Flow != FlowDeviceRFC8628 {
		t.Errorf("Flow = %q, want %q", cfg.Flow, FlowDeviceRFC8628)
	}

	fc := cfg.DeviceFlowConfig()
	if !fc.FormEncoded {
		t.Errorf("DeviceFlowConfig().FormEncoded = false, want true")
	}
}

func TestDeviceFlowConfig_DefaultNotFormEncoded(t *testing.T) {
	t.Parallel()

	cfg, err := ResolveProviderConfig("github-models")
	if err != nil {
		t.Fatalf("ResolveProviderConfig(github-models): %v", err)
	}
	if cfg.DeviceFlowConfig().FormEncoded {
		t.Errorf("github-models DeviceFlowConfig().FormEncoded = true, want false")
	}
}

func TestResolveFlowConfig_Discovery(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"token_endpoint":"` + srvTokenPath(r) + `"}`))
	}))
	defer srv.Close()

	cfg := &OAuthProviderConfig{
		ProviderID:   "test-discovery",
		DeviceEP:     srv.URL + "/dev",
		TokenEP:      "",
		DiscoveryURL: srv.URL,
	}

	fc, err := cfg.ResolveFlowConfig(context.Background())
	if err != nil {
		t.Fatalf("ResolveFlowConfig: %v", err)
	}
	if fc.TokenEP != srv.URL+"/tok" {
		t.Errorf("TokenEP = %q, want %q", fc.TokenEP, srv.URL+"/tok")
	}
	if fc.DeviceEP != srv.URL+"/dev" {
		t.Errorf("DeviceEP = %q, want %q", fc.DeviceEP, srv.URL+"/dev")
	}
}

// srvTokenPath returns the full token endpoint URL for the test server,
// derived from the request so the endpoint matches the chosen listener host.
func srvTokenPath(r *http.Request) string {
	return "http://" + r.Host + "/tok"
}

func TestResolveFlowConfig_NoDiscovery(t *testing.T) {
	t.Parallel()

	cfg := &OAuthProviderConfig{
		ProviderID: "test-fixed",
		TokenEP:    "https://fixed/token",
	}

	fc, err := cfg.ResolveFlowConfig(context.Background())
	if err != nil {
		t.Fatalf("ResolveFlowConfig: %v", err)
	}
	if fc.TokenEP != "https://fixed/token" {
		t.Errorf("TokenEP = %q, want https://fixed/token", fc.TokenEP)
	}
}
