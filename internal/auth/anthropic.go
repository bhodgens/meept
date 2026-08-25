package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Frozen Anthropic OAuth constants (mirroring hermes agent/anthropic_adapter.go).
const (
	// anthropicPlatformTokenEP is the primary token endpoint; the console
	// endpoint below is the fallback since the platform migration.
	anthropicPlatformTokenEP = "https://platform.claude.com/v1/oauth/token"
	anthropicConsoleTokenEP  = "https://console.anthropic.com/v1/oauth/token"
	// anthropicRedirectURI is the registered OAuth callback.
	anthropicRedirectURI = "https://console.anthropic.com/oauth/code/callback"
	// anthropicAuthorizeEP is the authorization endpoint base.
	anthropicAuthorizeEP = "https://claude.ai/oauth/authorize"
	// anthropicTokenUA is the User-Agent for token endpoint requests.
	// Anthropic 429-rate-limits token requests whose UA starts with
	// claude-code/; the axios UA is verified working.
	anthropicTokenUA = "axios/1.7.9"
)

// anthropicTokenEndpoints are the token endpoints tried in order. A variable
// (not a const) so tests can point it at httptest servers.
var anthropicTokenEndpoints = []string{anthropicPlatformTokenEP, anthropicConsoleTokenEP}

// AnthropicTokenEndpoints returns the token endpoints tried in order.
func AnthropicTokenEndpoints() []string {
	return append([]string(nil), anthropicTokenEndpoints...)
}

// BuildAnthropicAuthorizeURL builds the Claude OAuth authorization URL with
// PKCE S256 challenge and state.
func BuildAnthropicAuthorizeURL(clientID, challenge, state string) string {
	q := url.Values{}
	q.Set("code", "true")
	q.Set("client_id", clientID)
	q.Set("response_type", "code")
	q.Set("redirect_uri", anthropicRedirectURI)
	q.Set("scope", "org:create_api_key user:profile user:inference")
	q.Set("code_challenge", challenge)
	q.Set("code_challenge_method", "S256")
	q.Set("state", state)
	return anthropicAuthorizeEP + "?" + q.Encode()
}

// anthropicTokenResponse is the token exchange/refresh response shape.
type anthropicTokenResponse struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
}

// postAnthropicToken posts the encoded body to one token endpoint and parses
// the response. A non-200 status or a missing access_token is an error.
func postAnthropicToken(ctx context.Context, endpoint, contentType, body string) (*TokenResult, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", contentType)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", anthropicTokenUA)

	resp, err := deviceFlowHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d", resp.StatusCode)
	}

	var tr anthropicTokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}
	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	return &TokenResult{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
	}, nil
}

// anthropicPostAll tries all token endpoints in order with the given body,
// returning the first success or a combined error mentioning the last error.
func anthropicPostAll(ctx context.Context, contentType, body string) (*TokenResult, error) {
	var lastErr error
	for _, ep := range anthropicTokenEndpoints {
		result, err := postAnthropicToken(ctx, ep, contentType, body)
		if err == nil {
			return result, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("all token endpoints failed, last error: %w", lastErr)
}

// ExchangeAnthropicCode exchanges an authorization code for tokens using the
// PKCE verifier, trying both token endpoints in order.
func ExchangeAnthropicCode(ctx context.Context, clientID, code, verifier string) (*TokenResult, error) {
	form := url.Values{}
	form.Set("grant_type", "authorization_code")
	form.Set("code", code)
	form.Set("redirect_uri", anthropicRedirectURI)
	form.Set("client_id", clientID)
	form.Set("code_verifier", verifier)
	return anthropicPostAll(ctx, "application/x-www-form-urlencoded", form.Encode())
}

// RefreshAnthropicToken refreshes an Anthropic token. When jsonBody is false
// the request is form-encoded; otherwise a JSON body is sent.
func RefreshAnthropicToken(ctx context.Context, clientID, refreshToken string, jsonBody bool) (*TokenResult, error) {
	if jsonBody {
		payload, err := json.Marshal(map[string]string{
			"grant_type":    "refresh_token",
			"refresh_token": refreshToken,
			"client_id":     clientID,
		})
		if err != nil {
			return nil, fmt.Errorf("marshal refresh request: %w", err)
		}
		return anthropicPostAll(ctx, "application/json", string(payload))
	}
	form := url.Values{}
	form.Set("grant_type", "refresh_token")
	form.Set("refresh_token", refreshToken)
	form.Set("client_id", clientID)
	return anthropicPostAll(ctx, "application/x-www-form-urlencoded", form.Encode())
}
