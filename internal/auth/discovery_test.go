package auth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestResolveTokenEndpoint(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("expected GET, got %s", r.Method)
		}
		if got := r.Header.Get("Accept"); got != "application/json" {
			t.Errorf("Accept = %q, want application/json", got)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"issuer":"https://auth.x.ai","token_endpoint":"https://auth.x.ai/oauth2/token"}`))
	}))
	defer srv.Close()

	ep, err := ResolveTokenEndpoint(context.Background(), srv.URL)
	if err != nil {
		t.Fatalf("ResolveTokenEndpoint: %v", err)
	}
	if ep != "https://auth.x.ai/oauth2/token" {
		t.Errorf("token endpoint = %q, want https://auth.x.ai/oauth2/token", ep)
	}
}

func TestResolveTokenEndpoint_Missing(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"issuer":"https://auth.x.ai"}`))
	}))
	defer srv.Close()

	if _, err := ResolveTokenEndpoint(context.Background(), srv.URL); err == nil {
		t.Fatal("expected error for response missing token_endpoint, got nil")
	}
}

func TestResolveTokenEndpoint_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "internal error", http.StatusInternalServerError)
	}))
	defer srv.Close()

	_, err := ResolveTokenEndpoint(context.Background(), srv.URL)
	if err == nil {
		t.Fatal("expected error for HTTP 500, got nil")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Errorf("error should mention status 500, got: %v", err)
	}
}
