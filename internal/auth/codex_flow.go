package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// codexHTTPClient is the HTTP client used for Codex device-flow requests.
// It mirrors deviceFlowHTTPClient: a client-level timeout so hung TCP
// connections don't block forever, with per-request context semantics on
// top of this cap.
var codexHTTPClient = &http.Client{Timeout: 30 * time.Second}

// Codex device-flow constants (from the hermes_cli reference implementation).
const (
	// codexVerifyURL is where the user enters the user code.
	codexVerifyURL = "https://auth.openai.com/codex/device"
	// codexRedirectURI is the registered redirect URI used in the PKCE
	// authorization_code exchange.
	codexRedirectURI = "https://auth.openai.com/deviceauth/callback"
)

// CodexDeviceResult holds the response from the Codex usercode endpoint.
type CodexDeviceResult struct {
	UserCode     string
	DeviceAuthID string
	VerifyURL    string        // always the codex/device URL constant
	Interval     time.Duration // max(3s, server interval)
}

// CodexAuthGrant holds the authorization grant returned by the Codex poll
// endpoint after the user approves the request.
type CodexAuthGrant struct {
	AuthorizationCode string
	CodeVerifier      string
}

// codexUsercodeResponse is the JSON response from the usercode endpoint.
type codexUsercodeResponse struct {
	UserCode     string `json:"user_code"`
	DeviceAuthID string `json:"device_auth_id"`
}

// codexPollResponse is the JSON response from the poll endpoint.
type codexPollResponse struct {
	AuthorizationCode string `json:"authorization_code"`
	CodeVerifier      string `json:"code_verifier"`
}

// StartCodexDeviceFlow initiates the Codex device flow by requesting a user
// code from the usercode endpoint. On HTTP 429 it retries up to 4 total
// attempts, sleeping per the Retry-After header (integer seconds) when
// parseable, else exponential backoff (2s, 4s, 8s), clamped to [1s, 60s].
func StartCodexDeviceFlow(ctx context.Context, userCodeEP, clientID string) (*CodexDeviceResult, error) {
	reqBody, err := json.Marshal(map[string]string{"client_id": clientID})
	if err != nil {
		return nil, fmt.Errorf("marshal usercode request: %w", err)
	}

	const maxAttempts = 4
	for attempt := 1; ; attempt++ {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, userCodeEP, strings.NewReader(string(reqBody)))
		if reqErr != nil {
			return nil, fmt.Errorf("create usercode request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, doErr := codexHTTPClient.Do(req)
		if doErr != nil {
			return nil, fmt.Errorf("usercode request failed: %w", doErr)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read usercode response: %w", readErr)
		}

		if resp.StatusCode == http.StatusTooManyRequests && attempt < maxAttempts {
			if sleepErr := sleepWithContext(ctx, retryDelay(resp.Header, attempt)); sleepErr != nil {
				return nil, sleepErr
			}
			continue
		}

		if resp.StatusCode == http.StatusTooManyRequests {
			return nil, fmt.Errorf("usercode endpoint rate limited (HTTP 429) after %d attempts; retry later", maxAttempts)
		}

		if resp.StatusCode != http.StatusOK {
			return nil, fmt.Errorf("usercode endpoint returned %d: %s", resp.StatusCode, string(respBody))
		}

		var payload codexUsercodeResponse
		if unmarshalErr := json.Unmarshal(respBody, &payload); unmarshalErr != nil {
			return nil, fmt.Errorf("parse usercode response: %w", unmarshalErr)
		}
		if payload.UserCode == "" || payload.DeviceAuthID == "" {
			return nil, fmt.Errorf("incomplete usercode response: missing user_code or device_auth_id")
		}

		interval := parseCodexInterval(respBody)
		if interval < 3*time.Second {
			interval = 3 * time.Second
		}

		return &CodexDeviceResult{
			UserCode:     payload.UserCode,
			DeviceAuthID: payload.DeviceAuthID,
			VerifyURL:    codexVerifyURL,
			Interval:     interval,
		}, nil
	}
}

// parseCodexInterval extracts the "interval" field (in seconds) from the
// usercode response body. The field arrives as a STRING per the reference
// implementation; a JSON number is also accepted defensively. Returns 0
// when absent, empty, or malformed (the caller applies the 3s floor).
func parseCodexInterval(body []byte) time.Duration {
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		return 0
	}

	v, ok := raw["interval"]
	if !ok || v == nil {
		return 0
	}

	var seconds int64
	switch val := v.(type) {
	case string:
		n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64)
		if err != nil {
			return 0
		}
		seconds = n
	case float64:
		seconds = int64(val)
	default:
		return 0
	}

	if seconds < 0 {
		seconds = 0
	}
	return time.Duration(seconds) * time.Second
}

// parseRetryAfterValue parses the Retry-After header as integer seconds.
// Returns 0,false when absent or invalid (other formats, e.g. HTTP dates,
// are not supported). Presence is reported separately so a parseable
// "Retry-After: 0" can be distinguished from an absent or invalid header.
func parseRetryAfterValue(h http.Header) (time.Duration, bool) {
	v := strings.TrimSpace(h.Get("Retry-After"))
	if v == "" {
		return 0, false
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 0 {
		return 0, false
	}
	return time.Duration(n) * time.Second, true
}

// retryDelay computes the sleep for a 429 retry: the Retry-After header
// when present and parseable (a value of 0 clamps up to the 1s minimum),
// else exponential backoff 2^attempt seconds, clamped to [1s, 60s].
func retryDelay(h http.Header, attempt int) time.Duration {
	var d time.Duration
	if ra, ok := parseRetryAfterValue(h); ok {
		d = ra
	} else {
		d = time.Duration(1<<uint(attempt)) * time.Second
	}
	if d < 1*time.Second {
		d = 1 * time.Second
	}
	if d > 60*time.Second {
		d = 60 * time.Second
	}
	return d
}

// sleepWithContext sleeps for d, returning ctx.Err() if the context is
// cancelled first.
func sleepWithContext(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// PollCodexAuthorization polls the Codex poll endpoint until the user
// authorizes the request (HTTP 200 carrying an authorization grant), the
// context is cancelled, or a 15-minute deadline elapses. HTTP 403 and 404
// mean authorization is still pending.
func PollCodexAuthorization(ctx context.Context, pollEP, deviceAuthID, userCode string, interval time.Duration) (*CodexAuthGrant, error) {
	deadline := time.Now().Add(15 * time.Minute)
	if interval <= 0 {
		interval = 3 * time.Second
	}

	reqBody, err := json.Marshal(map[string]string{
		"device_auth_id": deviceAuthID,
		"user_code":      userCode,
	})
	if err != nil {
		return nil, fmt.Errorf("marshal poll request: %w", err)
	}

	for {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}

		req, reqErr := http.NewRequestWithContext(ctx, http.MethodPost, pollEP, strings.NewReader(string(reqBody)))
		if reqErr != nil {
			return nil, fmt.Errorf("create poll request: %w", reqErr)
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Accept", "application/json")

		resp, doErr := codexHTTPClient.Do(req)
		if doErr != nil {
			return nil, fmt.Errorf("poll request failed: %w", doErr)
		}

		respBody, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return nil, fmt.Errorf("read poll response: %w", readErr)
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			var payload codexPollResponse
			if unmarshalErr := json.Unmarshal(respBody, &payload); unmarshalErr != nil {
				return nil, fmt.Errorf("parse poll response: %w", unmarshalErr)
			}
			if payload.AuthorizationCode == "" || payload.CodeVerifier == "" {
				return nil, fmt.Errorf("poll response missing authorization_code or code_verifier")
			}
			return &CodexAuthGrant{
				AuthorizationCode: payload.AuthorizationCode,
				CodeVerifier:      payload.CodeVerifier,
			}, nil
		case resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusNotFound:
			// Still pending; wait and retry below.
		default:
			return nil, fmt.Errorf("poll endpoint returned %d: %s", resp.StatusCode, string(respBody))
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("codex authorization timed out after 15 minutes")
		}

		if sleepErr := sleepWithContext(ctx, interval); sleepErr != nil {
			return nil, sleepErr
		}
	}
}

// ExchangeCodexToken exchanges the Codex authorization grant for tokens
// using the PKCE authorization_code grant against the token endpoint.
func ExchangeCodexToken(ctx context.Context, tokenEP, clientID string, grant *CodexAuthGrant) (*TokenResult, error) {
	form := url.Values{
		"grant_type":    {"authorization_code"},
		"code":          {grant.AuthorizationCode},
		"redirect_uri":  {codexRedirectURI},
		"client_id":     {clientID},
		"code_verifier": {grant.CodeVerifier},
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, tokenEP, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("create token request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.Header.Set("Accept", "application/json")

	resp, err := codexHTTPClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("token request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read token response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("token endpoint returned %d: %s", resp.StatusCode, string(respBody))
	}

	var tr tokenResponse
	if err := json.Unmarshal(respBody, &tr); err != nil {
		return nil, fmt.Errorf("parse token response: %w", err)
	}

	if tr.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token")
	}

	tokenType := tr.TokenType
	if tokenType == "" {
		tokenType = "Bearer"
	}

	scopes := []string{}
	if tr.Scope != "" {
		scopes = strings.Split(tr.Scope, " ")
	}

	return &TokenResult{
		AccessToken:  tr.AccessToken,
		RefreshToken: tr.RefreshToken,
		TokenType:    tokenType,
		Expiry:       time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second),
		Scopes:       scopes,
	}, nil
}
