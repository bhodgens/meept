package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

func TestRefreshManager_RefreshesExpiringToken(t *testing.T) {
	// Create a token server that responds to refresh requests.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if r.Form.Get("grant_type") != "refresh_token" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"access_token": "refreshed-access-token",
			"token_type": "Bearer",
			"refresh_token": "new-refresh-token",
			"expires_in": 3600
		}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	// Register the provider so ResolveProviderConfig can find it.
	origProviders := OAuthProviders
	OAuthProviders = map[string]OAuthProviderConfig{
		"test-provider": {
			ClientIDDefault: "test-client",
			DeviceEP:        srv.URL + "/device",
			TokenEP:         srv.URL,
			Scopes:          []string{"read"},
			ProviderID:      "test-provider",
		},
	}
	defer func() { OAuthProviders = origProviders }()

	// Save a token that is about to expire (within the 10-minute default margin).
	store.Save("test-provider", &TokenResult{
		AccessToken:  "old-access-token",
		TokenType:    "Bearer",
		RefreshToken: "old-refresh-token",
		Expiry:       time.Now().Add(5 * time.Minute), // within margin
	})

	rm := NewRefreshManager(store, WithRefreshMargin(10*time.Minute))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Start with a short interval so the test doesn't take long.
	rm.Start(ctx, 100*time.Millisecond)

	// Wait for the refresh to happen.
	time.Sleep(500 * time.Millisecond)
	rm.Stop()

	// Verify the token was refreshed.
	loaded, err := store.Load("test-provider")
	if err != nil {
		t.Fatalf("Load after refresh: %v", err)
	}
	if loaded.AccessToken != "refreshed-access-token" {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, "refreshed-access-token")
	}
	if loaded.RefreshToken != "new-refresh-token" {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, "new-refresh-token")
	}
}

func TestRefreshManager_SkipsNonExpiringToken(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	refreshCalled := int32(0)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.StoreInt32(&refreshCalled, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	origProviders := OAuthProviders
	OAuthProviders = map[string]OAuthProviderConfig{
		"test-provider": {
			ClientIDDefault: "test-client",
			DeviceEP:        srv.URL + "/device",
			TokenEP:         srv.URL,
			ProviderID:      "test-provider",
		},
	}
	defer func() { OAuthProviders = origProviders }()

	// Token has 2 hours of validity — well beyond the margin.
	store.Save("test-provider", &TokenResult{
		AccessToken:  "still-valid",
		TokenType:    "Bearer",
		RefreshToken: "rt",
		Expiry:       time.Now().Add(2 * time.Hour),
	})

	rm := NewRefreshManager(store, WithRefreshMargin(10*time.Minute))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rm.Start(ctx, 100*time.Millisecond)
	time.Sleep(300 * time.Millisecond)
	rm.Stop()

	if atomic.LoadInt32(&refreshCalled) != 0 {
		t.Error("refresh should not have been called for non-expiring token")
	}
}

func TestRefreshManager_TracksFailures(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"error":"invalid_grant"}`))
	}))
	defer srv.Close()

	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	origProviders := OAuthProviders
	OAuthProviders = map[string]OAuthProviderConfig{
		"fail-provider": {
			ClientIDDefault: "test-client",
			DeviceEP:        srv.URL + "/device",
			TokenEP:         srv.URL,
			ProviderID:      "fail-provider",
		},
	}
	defer func() { OAuthProviders = origProviders }()

	// Token is expiring and will fail to refresh.
	store.Save("fail-provider", &TokenResult{
		AccessToken:  "expiring",
		TokenType:    "Bearer",
		RefreshToken: "bad-refresh",
		Expiry:       time.Now().Add(1 * time.Minute),
	})

	rm := NewRefreshManager(store, WithRefreshMargin(10*time.Minute))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rm.Start(ctx, 100*time.Millisecond)
	time.Sleep(500 * time.Millisecond)
	rm.Stop()

	// After 3 failures, the counter should be reset (logged as stale).
	if rm.Failures("fail-provider") != 0 {
		t.Errorf("expected failure count to be reset after stale warning, got %d", rm.Failures("fail-provider"))
	}
}

func TestRefreshManager_StopIdempotent(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	rm := NewRefreshManager(store)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	rm.Start(ctx, 1*time.Hour)
	rm.Stop()
	rm.Stop() // should not panic
}

func TestWithRefreshMargin(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	rm := NewRefreshManager(store, WithRefreshMargin(30*time.Minute))
	if rm.margin != 30*time.Minute {
		t.Errorf("margin = %v, want 30m", rm.margin)
	}
}
