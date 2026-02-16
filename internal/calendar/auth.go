// Package calendar provides Google Calendar integration for meept.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	googleAuthURL  = "https://accounts.google.com/o/oauth2/v2/auth"
	googleTokenURL = "https://oauth2.googleapis.com/token"
)

// OAuth2Config holds OAuth2 configuration.
type OAuth2Config struct {
	ClientID     string
	ClientSecret string
	RedirectURI  string
	Scopes       []string
}

// DefaultOAuth2Config returns a config with default calendar scopes.
func DefaultOAuth2Config(clientID, clientSecret, redirectURI string) OAuth2Config {
	return OAuth2Config{
		ClientID:     clientID,
		ClientSecret: clientSecret,
		RedirectURI:  redirectURI,
		Scopes: []string{
			"https://www.googleapis.com/auth/calendar.readonly",
			"https://www.googleapis.com/auth/calendar.events",
		},
	}
}

// Token represents an OAuth2 token.
type Token struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry,omitempty"`
}

// Valid returns true if the token is valid and not expired.
func (t *Token) Valid() bool {
	if t.AccessToken == "" {
		return false
	}
	if t.Expiry.IsZero() {
		return true
	}
	return time.Now().Before(t.Expiry.Add(-time.Minute)) // 1 minute buffer
}

// OAuth2Authenticator handles the OAuth2 flow for Google Calendar.
type OAuth2Authenticator struct {
	config     OAuth2Config
	httpClient *http.Client
	tokenPath  string
}

// NewOAuth2Authenticator creates a new OAuth2 authenticator.
func NewOAuth2Authenticator(cfg OAuth2Config, tokenPath string) *OAuth2Authenticator {
	if tokenPath == "" {
		homeDir, _ := os.UserHomeDir()
		tokenPath = filepath.Join(homeDir, ".meept", "calendar_token.json")
	}
	return &OAuth2Authenticator{
		config:     cfg,
		httpClient: &http.Client{Timeout: 30 * time.Second},
		tokenPath:  tokenPath,
	}
}

// AuthURL returns the URL to redirect the user for authorization.
func (a *OAuth2Authenticator) AuthURL(state string) string {
	params := url.Values{}
	params.Set("client_id", a.config.ClientID)
	params.Set("redirect_uri", a.config.RedirectURI)
	params.Set("response_type", "code")
	params.Set("scope", strings.Join(a.config.Scopes, " "))
	params.Set("access_type", "offline")
	params.Set("prompt", "consent")
	if state != "" {
		params.Set("state", state)
	}
	return googleAuthURL + "?" + params.Encode()
}

// Exchange exchanges an authorization code for a token.
func (a *OAuth2Authenticator) Exchange(ctx context.Context, code string) (*Token, error) {
	params := url.Values{}
	params.Set("client_id", a.config.ClientID)
	params.Set("client_secret", a.config.ClientSecret)
	params.Set("code", code)
	params.Set("grant_type", "authorization_code")
	params.Set("redirect_uri", a.config.RedirectURI)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL,
		strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token exchange failed: %s", string(body))
	}

	var tokenResp struct {
		AccessToken  string `json:"access_token"`
		TokenType    string `json:"token_type"`
		ExpiresIn    int    `json:"expires_in"`
		RefreshToken string `json:"refresh_token"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	token := &Token{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		RefreshToken: tokenResp.RefreshToken,
	}
	if tokenResp.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return token, nil
}

// Refresh refreshes an expired token.
func (a *OAuth2Authenticator) Refresh(ctx context.Context, refreshToken string) (*Token, error) {
	params := url.Values{}
	params.Set("client_id", a.config.ClientID)
	params.Set("client_secret", a.config.ClientSecret)
	params.Set("refresh_token", refreshToken)
	params.Set("grant_type", "refresh_token")

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, googleTokenURL,
		strings.NewReader(params.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := a.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("token refresh failed: %s", string(body))
	}

	var tokenResp struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&tokenResp); err != nil {
		return nil, fmt.Errorf("failed to parse token response: %w", err)
	}

	token := &Token{
		AccessToken:  tokenResp.AccessToken,
		TokenType:    tokenResp.TokenType,
		RefreshToken: refreshToken, // Keep the original refresh token
	}
	if tokenResp.ExpiresIn > 0 {
		token.Expiry = time.Now().Add(time.Duration(tokenResp.ExpiresIn) * time.Second)
	}

	return token, nil
}

// SaveToken saves a token to the token file.
func (a *OAuth2Authenticator) SaveToken(token *Token) error {
	// Ensure directory exists
	dir := filepath.Dir(a.tokenPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("failed to create token directory: %w", err)
	}

	data, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal token: %w", err)
	}

	if err := os.WriteFile(a.tokenPath, data, 0600); err != nil {
		return fmt.Errorf("failed to write token file: %w", err)
	}

	return nil
}

// LoadToken loads a token from the token file.
func (a *OAuth2Authenticator) LoadToken() (*Token, error) {
	data, err := os.ReadFile(a.tokenPath)
	if err != nil {
		return nil, err
	}

	var token Token
	if err := json.Unmarshal(data, &token); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	return &token, nil
}

// GetValidToken returns a valid token, refreshing if necessary.
func (a *OAuth2Authenticator) GetValidToken(ctx context.Context) (*Token, error) {
	token, err := a.LoadToken()
	if err != nil {
		return nil, fmt.Errorf("no saved token: %w", err)
	}

	if token.Valid() {
		return token, nil
	}

	// Token expired, try to refresh
	if token.RefreshToken == "" {
		return nil, fmt.Errorf("token expired and no refresh token available")
	}

	newToken, err := a.Refresh(ctx, token.RefreshToken)
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	// Save the new token
	if err := a.SaveToken(newToken); err != nil {
		// Log but don't fail
		fmt.Printf("Warning: failed to save refreshed token: %v\n", err)
	}

	return newToken, nil
}
