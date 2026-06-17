package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// DeviceCodeResult holds the response from the device authorization endpoint.
type DeviceCodeResult struct {
	DeviceCode      string
	UserCode        string
	VerificationURI string
	ExpiresAt       time.Time
	Interval        time.Duration
}

// TokenResult holds a successful token response from the token endpoint.
type TokenResult struct {
	AccessToken  string    `json:"access_token"`
	TokenType    string    `json:"token_type"`
	RefreshToken string    `json:"refresh_token,omitempty"`
	Expiry       time.Time `json:"expiry"`
	Scopes       []string  `json:"scopes,omitempty"`
}

// DeviceFlowConfig holds the configuration needed to perform the device code flow.
type DeviceFlowConfig struct {
	ClientID     string
	ClientSecret string // empty for GitHub (public client)
	DeviceEP     string // device authorization endpoint
	TokenEP      string // token endpoint
	Scopes       []string
}

// deviceCodeRequest is the JSON payload sent to the device authorization endpoint.
type deviceCodeRequest struct {
	ClientID string `json:"client_id"`
	Scope    string `json:"scope,omitempty"`
}

// deviceCodeResponse is the JSON response from the device authorization endpoint.
type deviceCodeResponse struct {
	DeviceCode              string `json:"device_code"`
	UserCode                string `json:"user_code"`
	VerificationURI         string `json:"verification_uri"`
	VerificationURIComplete string `json:"verification_uri_complete,omitempty"`
	ExpiresIn               int    `json:"expires_in"`
	Interval                int    `json:"interval"`
}

// tokenResponse is the JSON response from the token endpoint.
type tokenResponse struct {
	AccessToken  string `json:"access_token"`
	TokenType    string `json:"token_type"`
	RefreshToken string `json:"refresh_token"`
	ExpiresIn    int    `json:"expires_in"`
	Scope        string `json:"scope,omitempty"`
	Error        string `json:"error,omitempty"`
	ErrorDesc    string `json:"error_description,omitempty"`
}

// RFC 8628 error codes.
const (
	errAuthorizationPending = "authorization_pending"
	errSlowDown             = "slow_down"
	errExpiredToken         = "expired_token"
	errAccessDenied         = "access_denied"
)

// DeviceFlowError represents an error returned during the device code flow.
type DeviceFlowError struct {
	Code        string
	Description string
}

func (e *DeviceFlowError) Error() string {
	msg := e.Code
	if e.Description != "" {
		msg += ": " + e.Description
	}
	return msg
}

// StartDeviceFlow initiates the RFC 8628 device code flow.
// It sends a POST request to the device authorization endpoint and returns
// the device code, user code, verification URI, and polling interval.
func StartDeviceFlow(ctx context.Context, cfg DeviceFlowConfig) (*DeviceCodeResult, error) {
	scopeStr := strings.Join(cfg.Scopes, " ")
	reqBody := deviceCodeRequest{
		ClientID: cfg.ClientID,
		Scope:    scopeStr,
	}
	bodyBytes, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal device code request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.DeviceEP, strings.NewReader(string(bodyBytes)))
	if err != nil {
		return nil, fmt.Errorf("create device code request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("device code request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read device code response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("device code endpoint returned %d: %s", resp.StatusCode, string(respBody))
	}

	var dcr deviceCodeResponse
	if err := json.Unmarshal(respBody, &dcr); err != nil {
		return nil, fmt.Errorf("parse device code response: %w", err)
	}

	if dcr.DeviceCode == "" || dcr.UserCode == "" || dcr.VerificationURI == "" {
		return nil, fmt.Errorf("incomplete device code response: missing required fields")
	}

	interval := time.Duration(dcr.Interval) * time.Second
	if interval == 0 {
		interval = 5 * time.Second
	}

	return &DeviceCodeResult{
		DeviceCode:      dcr.DeviceCode,
		UserCode:        dcr.UserCode,
		VerificationURI: dcr.VerificationURI,
		ExpiresAt:       time.Now().Add(time.Duration(dcr.ExpiresIn) * time.Second),
		Interval:        interval,
	}, nil
}

// PollForToken polls the token endpoint until the user authorizes the request
// or an error occurs. It handles RFC 8628 error codes:
//   - authorization_pending: continue polling
//   - slow_down: increase interval by 5 seconds
//   - expired_token: return error
//   - access_denied: return error
func PollForToken(ctx context.Context, cfg DeviceFlowConfig, result *DeviceCodeResult) (*TokenResult, error) {
	interval := result.Interval

	for {
		// Check if the device code has expired before each poll.
		if time.Now().After(result.ExpiresAt) {
			return nil, &DeviceFlowError{
				Code:        errExpiredToken,
				Description: "device code expired before authorization completed",
			}
		}

		// Check if context was cancelled before polling.
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		token, err := pollOnce(ctx, cfg, result.DeviceCode)
		if err != nil {
			var dfe *DeviceFlowError
			if errors.As(err, &dfe) {
				switch dfe.Code {
				case errAuthorizationPending:
					// Wait for the polling interval before retrying.
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(interval):
						continue
					}
				case errSlowDown:
					interval += 5 * time.Second
					slog.Debug("slow_down received, increasing interval", "new_interval", interval)
					select {
					case <-ctx.Done():
						return nil, ctx.Err()
					case <-time.After(interval):
						continue
					}
				default:
					// expired_token, access_denied, or unknown error code.
					return nil, err
				}
			}
			// Non-DeviceFlowError (e.g., network error, context error).
			return nil, err
		}

		slog.Info("device code flow complete", "provider", cfg.ClientID)
		return token, nil
	}
}

// pollOnce sends a single token request. It returns the token on success,
// a DeviceFlowError for known RFC 8628 errors, or a generic error.
func pollOnce(ctx context.Context, cfg DeviceFlowConfig, deviceCode string) (*TokenResult, error) {
	form := url.Values{
		"client_id":   {cfg.ClientID},
		"grant_type":  {"urn:ietf:params:oauth:grant-type:device_code"},
		"device_code": {deviceCode},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEP, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		var tr tokenResponse
		if err := json.Unmarshal(respBody, &tr); err != nil {
			return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(respBody))
		}
		if tr.Error != "" {
			return nil, &DeviceFlowError{
				Code:        tr.Error,
				Description: tr.ErrorDesc,
			}
		}
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	scopes := []string{}
	if tr.Scope != "" {
		scopes = strings.Split(tr.Scope, " ")
	}

	return &TokenResult{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		Scopes:       scopes,
	}, nil
}

// RefreshTokenRequest performs a token refresh using the refresh_token grant.
func RefreshTokenRequest(ctx context.Context, cfg DeviceFlowConfig, refreshToken string) (*TokenResult, error) {
	form := url.Values{
		"client_id":     {cfg.ClientID},
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
	}
	if cfg.ClientSecret != "" {
		form.Set("client_secret", cfg.ClientSecret)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.TokenEP, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read refresh response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("refresh endpoint returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("parse refresh response: %w", err)
	}

	if tr.AccessToken == "" {
		return nil, fmt.Errorf("refresh response missing access_token")
	}

	scopes := []string{}
	if tr.Scope != "" {
		scopes = strings.Split(tr.Scope, " ")
	}

	return &TokenResult{
		AccessToken:  tr.AccessToken,
		TokenType:    tr.TokenType,
		RefreshToken: tr.RefreshToken,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		Scopes:       scopes,
	}, nil
}
