package web

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoAuth(t *testing.T) {
	auth := NoAuth{}
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if !auth.Authenticate(req) {
		t.Fatalf("NoAuth should always authenticate")
	}
}

func TestBearerAuth_Valid(t *testing.T) {
	auth := NewBearerAuth("token1", "token2")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer token1")
	if !auth.Authenticate(req) {
		t.Fatalf("expected valid authentication")
	}
}

func TestBearerAuth_Invalid(t *testing.T) {
	auth := NewBearerAuth("token1")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer wrong-token")
	if auth.Authenticate(req) {
		t.Fatalf("expected invalid authentication")
	}
}

func TestBearerAuth_MissingHeader(t *testing.T) {
	auth := NewBearerAuth("token1")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if auth.Authenticate(req) {
		t.Fatalf("expected failure with missing header")
	}
}

func TestBearerAuth_WrongScheme(t *testing.T) {
	auth := NewBearerAuth("token1")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Basic dXNlcjpwYXNz")
	if auth.Authenticate(req) {
		t.Fatalf("expected failure with wrong scheme")
	}
}

func TestBasicAuth_Valid(t *testing.T) {
	auth := NewBasicAuth(map[string]string{"admin": "secret"})
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.SetBasicAuth("admin", "secret")
	if !auth.Authenticate(req) {
		t.Fatalf("expected valid authentication")
	}
}

func TestBasicAuth_WrongPassword(t *testing.T) {
	auth := NewBasicAuth(map[string]string{"admin": "secret"})
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.SetBasicAuth("admin", "wrong")
	if auth.Authenticate(req) {
		t.Fatalf("expected invalid authentication")
	}
}

func TestBasicAuth_UnknownUser(t *testing.T) {
	auth := NewBasicAuth(map[string]string{"admin": "secret"})
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.SetBasicAuth("unknown", "secret")
	if auth.Authenticate(req) {
		t.Fatalf("expected invalid authentication")
	}
}

func TestAPIKeyAuth_Header(t *testing.T) {
	auth := NewAPIKeyAuth([]string{"key1", "key2"}, "", "")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-API-Key", "key1")
	if !auth.Authenticate(req) {
		t.Fatalf("expected valid authentication via header")
	}
}

func TestAPIKeyAuth_QueryParam(t *testing.T) {
	auth := NewAPIKeyAuth([]string{"key1"}, "", "")
	req := httptest.NewRequest(http.MethodGet, "/?api_key=key1", http.NoBody)
	if !auth.Authenticate(req) {
		t.Fatalf("expected valid authentication via query param")
	}
}

func TestAPIKeyAuth_InvalidKey(t *testing.T) {
	auth := NewAPIKeyAuth([]string{"key1"}, "", "")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-API-Key", "wrong")
	if auth.Authenticate(req) {
		t.Fatalf("expected invalid authentication")
	}
}

func TestAPIKeyAuth_CustomHeader(t *testing.T) {
	auth := NewAPIKeyAuth([]string{"key1"}, "X-Custom-Key", "")
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("X-Custom-Key", "key1")
	if !auth.Authenticate(req) {
		t.Fatalf("expected valid authentication via custom header")
	}
}

func TestChainAuth(t *testing.T) {
	bearer := NewBearerAuth("token1")
	apiKey := NewAPIKeyAuth([]string{"key1"}, "", "")
	auth := NewChainAuth(bearer, apiKey)

	// Test bearer auth works
	req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req.Header.Set("Authorization", "Bearer token1")
	if !auth.Authenticate(req) {
		t.Fatalf("expected valid bearer auth through chain")
	}

	// Test API key auth works
	req2 := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	req2.Header.Set("X-API-Key", "key1")
	if !auth.Authenticate(req2) {
		t.Fatalf("expected valid API key auth through chain")
	}

	// Test invalid auth fails
	req3 := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
	if auth.Authenticate(req3) {
		t.Fatalf("expected failure with no credentials")
	}
}

func TestIPWhitelistAuth(t *testing.T) {
	auth := NewIPWhitelistAuth("127.0.0.1", "10.0.0.1")

	tests := []struct {
		name    string
		remote  string
		forward string
		realIP  string
		want    bool
	}{
		{"direct match", "127.0.0.1:1234", "", "", true},
		{"forwarded for", "1.2.3.4:1234", "10.0.0.1", "", true},
		{"real ip", "1.2.3.4:1234", "", "10.0.0.1", true},
		{"no match", "1.2.3.4:1234", "", "", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/", http.NoBody)
			req.RemoteAddr = tt.remote
			if tt.forward != "" {
				req.Header.Set("X-Forwarded-For", tt.forward)
			}
			if tt.realIP != "" {
				req.Header.Set("X-Real-IP", tt.realIP)
			}
			if got := auth.Authenticate(req); got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
