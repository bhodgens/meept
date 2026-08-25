package auth

import (
	"fmt"
	"os"

	"github.com/caimlas/meept/internal/llm"
)

// FlowKind identifies which OAuth flow a provider uses.
// The empty value means the standard RFC 8628 device code flow
// (FlowDeviceRFC8628), so existing registry entries need no change.
type FlowKind string

const (
	// FlowDeviceRFC8628 is the standard RFC 8628 device code flow.
	FlowDeviceRFC8628 FlowKind = "device_rfc8628"
	// FlowDeviceCodex is the Codex-style device flow with separate
	// user-code and poll endpoints.
	FlowDeviceCodex FlowKind = "device_codex"
	// FlowPKCEPaste is the PKCE flow where the user pastes the callback
	// URL manually.
	FlowPKCEPaste FlowKind = "pkce_paste"
)

// OAuthProviderConfig holds the OAuth configuration for a provider.
// Client IDs and secrets are embedded as defaults with user overrides via
// environment variables.
type OAuthProviderConfig struct {
	ClientIDDefault     string // embedded default (not a secret)
	ClientIDEnvVar      string // env var for override
	ClientSecretDefault string // embedded default (empty for public clients)
	ClientSecretEnvVar  string // env var for override
	DeviceEP            string // device authorization endpoint
	TokenEP             string // token endpoint
	Scopes              []string
	ProviderID          string
	Transport           llm.ProviderTransport
	BaseURL             string
	ExtraHeaders        map[string]string // e.g., X-GitHub-Api-Version

	Flow             FlowKind // empty == FlowDeviceRFC8628
	DeviceUserCodeEP string   // codex: usercode endpoint
	DevicePollEP     string   // codex: poll endpoint
	AuthorizeURL     string   // claude: authorize endpoint
	VerifyHint       string   // claude: user-facing hint
	DiscoveryURL     string   // xai: .well-known/openid-configuration
	RefreshJSON      bool     // claude: refresh as JSON body
}

// OAuthProviders is the registry of supported OAuth device code providers.
// Client IDs marked with "<...>" are placeholders that must be replaced
// once the OAuth apps are registered.
var OAuthProviders = map[string]OAuthProviderConfig{
	"github-models": {
		ClientIDDefault: "placeholder-github-oauth-client-id",
		ClientIDEnvVar:  "MEEPT_GITHUB_CLIENT_ID",
		DeviceEP:        "https://github.com/login/device/code",
		TokenEP:         "https://github.com/login/oauth/access_token",
		Scopes:          []string{"models:read"},
		ProviderID:      "github-models",
		Transport:       llm.TransportOpenAIChat,
		BaseURL:         "https://models.github.ai/inference",
		ExtraHeaders: map[string]string{
			"Accept":               "application/vnd.github+json",
			"X-GitHub-Api-Version": "2026-03-10",
		},
	},
	"google-oauth": {
		ClientIDDefault:     "placeholder-google-oauth-client-id",
		ClientIDEnvVar:      "MEEPT_GOOGLE_CLIENT_ID",
		ClientSecretDefault: "placeholder-google-oauth-client-secret",
		ClientSecretEnvVar:  "MEEPT_GOOGLE_CLIENT_SECRET",
		DeviceEP:            "https://oauth2.googleapis.com/device/code",
		TokenEP:             "https://oauth2.googleapis.com/token",
		Scopes: []string{
			"https://www.googleapis.com/auth/generative-language.retriever",
		},
		ProviderID: "google-oauth",
		Transport:  llm.TransportOpenAIChat,
		BaseURL:    "https://generativelanguage.googleapis.com/v1beta/openai",
	},
	"google-calendar": {
		ClientIDDefault:     "placeholder-google-oauth-client-id",
		ClientIDEnvVar:      "MEEPT_GOOGLE_CLIENT_ID",
		ClientSecretDefault: "placeholder-google-oauth-client-secret",
		ClientSecretEnvVar:  "MEEPT_GOOGLE_CLIENT_SECRET",
		DeviceEP:            "https://oauth2.googleapis.com/device/code",
		TokenEP:             "https://oauth2.googleapis.com/token",
		Scopes: []string{
			"https://www.googleapis.com/auth/calendar.readonly",
			"https://www.googleapis.com/auth/calendar.events",
		},
		ProviderID: "google-calendar",
	},
}

// ResolveProviderConfig returns the effective OAuth configuration for a
// provider, applying environment variable overrides to the embedded defaults.
func ResolveProviderConfig(providerID string) (*OAuthProviderConfig, error) {
	cfg, ok := OAuthProviders[providerID]
	if !ok {
		return nil, fmt.Errorf("unknown oauth provider: %s", providerID)
	}

	resolved := cfg // copy

	// Apply environment variable overrides.
	if cfg.ClientIDEnvVar != "" {
		if envVal := os.Getenv(cfg.ClientIDEnvVar); envVal != "" {
			resolved.ClientIDDefault = envVal
		}
	}
	if cfg.ClientSecretEnvVar != "" {
		if envVal := os.Getenv(cfg.ClientSecretEnvVar); envVal != "" {
			resolved.ClientSecretDefault = envVal
		}
	}

	return &resolved, nil
}

// DeviceFlowConfig returns a DeviceFlowConfig from the resolved provider config.
func (c *OAuthProviderConfig) DeviceFlowConfig() DeviceFlowConfig {
	return DeviceFlowConfig{
		ClientID:     c.ClientIDDefault,
		ClientSecret: c.ClientSecretDefault,
		DeviceEP:     c.DeviceEP,
		TokenEP:      c.TokenEP,
		Scopes:       c.Scopes,
	}
}

// RegisteredProviders returns the list of registered OAuth provider IDs.
func RegisteredProviders() []string {
	ids := make([]string, 0, len(OAuthProviders))
	for id := range OAuthProviders {
		ids = append(ids, id)
	}
	return ids
}
