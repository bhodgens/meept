package auth

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStartDeviceFlow_Success(t *testing.T) {
	wantResp := deviceCodeResponse{
		DeviceCode:      "dc_abc123",
		UserCode:        "ABCD-1234",
		VerificationURI: "https://github.com/login/device",
		ExpiresIn:       900,
		Interval:        5,
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(wantResp)
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "test-client-id",
		DeviceEP: srv.URL,
		Scopes:   []string{"models:read"},
	}

	result, err := StartDeviceFlow(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartDeviceFlow: %v", err)
	}

	if result.DeviceCode != wantResp.DeviceCode {
		t.Errorf("DeviceCode = %q, want %q", result.DeviceCode, wantResp.DeviceCode)
	}
	if result.UserCode != wantResp.UserCode {
		t.Errorf("UserCode = %q, want %q", result.UserCode, wantResp.UserCode)
	}
	if result.VerificationURI != wantResp.VerificationURI {
		t.Errorf("VerificationURI = %q, want %q", result.VerificationURI, wantResp.VerificationURI)
	}
	if result.Interval != 5*time.Second {
		t.Errorf("Interval = %v, want 5s", result.Interval)
	}
}

func TestStartDeviceFlow_DefaultInterval(t *testing.T) {
	resp := deviceCodeResponse{
		DeviceCode:      "dc_abc",
		UserCode:        "ABCD-1234",
		VerificationURI: "https://example.com",
		ExpiresIn:       600,
		Interval:        0, // server returns 0
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(resp)
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{ClientID: "c", DeviceEP: srv.URL}
	result, err := StartDeviceFlow(context.Background(), cfg)
	if err != nil {
		t.Fatalf("StartDeviceFlow: %v", err)
	}
	if result.Interval != 5*time.Second {
		t.Errorf("expected default interval of 5s, got %v", result.Interval)
	}
}

func TestStartDeviceFlow_ServerError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_client"}`))
	}))
	defer srv.Close()

	_, err := StartDeviceFlow(context.Background(), DeviceFlowConfig{
		ClientID: "c",
		DeviceEP: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error on server error")
	}
}

func TestStartDeviceFlow_IncompleteResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]string{
			"device_code": "dc_abc",
			// missing user_code and verification_uri
		})
	}))
	defer srv.Close()

	_, err := StartDeviceFlow(context.Background(), DeviceFlowConfig{
		ClientID: "c",
		DeviceEP: srv.URL,
	})
	if err == nil {
		t.Fatal("expected error on incomplete response")
	}
}

func writeTokenError(w http.ResponseWriter, code, desc string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusBadRequest)
	json.NewEncoder(w).Encode(tokenResponse{
		Error:     code,
		ErrorDesc: desc,
	})
}

func writeTokenSuccess(w http.ResponseWriter, at, rt string, expiresIn int, scope string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(tokenResponse{
		AccessToken:  at,
		TokenType:    "Bearer",
		RefreshToken: rt,
		ExpiresIn:    expiresIn,
		Scope:        scope,
	})
}

func TestPollForToken_AuthorizationPending(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount < 3 {
			writeTokenError(w, errAuthorizationPending, "waiting for user")
		} else {
			writeTokenSuccess(w, "at_success", "rt_success", 3600, "models:read")
		}
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  srv.URL,
	}

	result := &DeviceCodeResult{
		DeviceCode: "dc_abc",
		ExpiresAt:  time.Now().Add(5 * time.Minute),
		Interval:   50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	token, err := PollForToken(ctx, cfg, result)
	if err != nil {
		t.Fatalf("PollForToken: %v", err)
	}
	if token.AccessToken != "at_success" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "at_success")
	}
	if token.RefreshToken != "rt_success" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "rt_success")
	}
	if len(token.Scopes) != 1 || token.Scopes[0] != "models:read" {
		t.Errorf("Scopes = %v, want [models:read]", token.Scopes)
	}
}

func TestPollForToken_SlowDown(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		if callCount == 1 {
			writeTokenError(w, errSlowDown, "slow down")
		} else {
			writeTokenSuccess(w, "at_ok", "", 3600, "")
		}
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  srv.URL,
	}

	result := &DeviceCodeResult{
		DeviceCode: "dc_abc",
		ExpiresAt:  time.Now().Add(5 * time.Minute),
		Interval:   50 * time.Millisecond,
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	token, err := PollForToken(ctx, cfg, result)
	if err != nil {
		t.Fatalf("PollForToken: %v", err)
	}
	if token.AccessToken != "at_ok" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "at_ok")
	}
}

func TestPollForToken_AccessDenied(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTokenError(w, errAccessDenied, "user denied access")
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  srv.URL,
	}

	result := &DeviceCodeResult{
		DeviceCode: "dc_abc",
		ExpiresAt:  time.Now().Add(5 * time.Minute),
		Interval:   50 * time.Millisecond,
	}

	_, err := PollForToken(context.Background(), cfg, result)
	if err == nil {
		t.Fatal("expected error on access_denied")
	}
	var dfe *DeviceFlowError
	if !errors.As(err, &dfe) || dfe.Code != errAccessDenied {
		t.Errorf("expected DeviceFlowError with access_denied, got %v", err)
	}
}

func TestPollForToken_ContextCancelled(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTokenError(w, errAuthorizationPending, "pending")
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  srv.URL,
	}

	result := &DeviceCodeResult{
		DeviceCode: "dc_abc",
		ExpiresAt:  time.Now().Add(5 * time.Minute),
		Interval:   50 * time.Millisecond,
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	_, err := PollForToken(ctx, cfg, result)
	if err == nil {
		t.Fatal("expected error on context cancellation")
	}
}

func TestPollForToken_Expired(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeTokenError(w, errAuthorizationPending, "pending")
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  srv.URL,
	}

	// Already expired.
	result := &DeviceCodeResult{
		DeviceCode: "dc_abc",
		ExpiresAt:  time.Now().Add(-1 * time.Minute),
		Interval:   50 * time.Millisecond,
	}

	_, err := PollForToken(context.Background(), cfg, result)
	if err == nil {
		t.Fatal("expected error on expired device code")
	}
}

func TestRefreshTokenRequest_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("expected POST, got %s", r.Method)
		}
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			t.Errorf("grant_type = %q, want refresh_token", r.Form.Get("grant_type"))
		}
		if r.Form.Get("refresh_token") != "my-refresh-token" {
			t.Errorf("refresh_token = %q, want my-refresh-token", r.Form.Get("refresh_token"))
		}

		writeTokenSuccess(w, "new-access-token", "new-refresh-token", 7200, "")
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID:     "test-client",
		ClientSecret: "test-secret",
		TokenEP:      srv.URL,
	}

	token, err := RefreshTokenRequest(context.Background(), cfg, "my-refresh-token")
	if err != nil {
		t.Fatalf("RefreshTokenRequest: %v", err)
	}
	if token.AccessToken != "new-access-token" {
		t.Errorf("AccessToken = %q, want %q", token.AccessToken, "new-access-token")
	}
	if token.RefreshToken != "new-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", token.RefreshToken, "new-refresh-token")
	}
}

func TestRefreshTokenRequest_Failure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  srv.URL,
	}

	_, err := RefreshTokenRequest(context.Background(), cfg, "bad-token")
	if err == nil {
		t.Fatal("expected error on refresh failure")
	}
}

func TestDeviceFlowError_Error(t *testing.T) {
	e := &DeviceFlowError{Code: "access_denied", Description: "user said no"}
	msg := e.Error()
	if msg != "access_denied: user said no" {
		t.Errorf("Error() = %q, want %q", msg, "access_denied: user said no")
	}

	e2 := &DeviceFlowError{Code: "slow_down"}
	msg2 := e2.Error()
	if msg2 != "slow_down" {
		t.Errorf("Error() = %q, want %q", msg2, "slow_down")
	}
}
