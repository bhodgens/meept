// Package http provides authentication middleware for the HTTP API.
package http

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
)

type contextKey string

const apiKeyContextKey contextKey = "api_key"

// APIKeyAuth middleware validates API key from Authorization header.
type APIKeyAuth struct {
	validKeys []string
}

// NewAPIKeyAuth creates API key authentication with provided keys.
func NewAPIKeyAuth(keys []string) *APIKeyAuth {
	vk := make([]string, len(keys))
	copy(vk, keys)
	return &APIKeyAuth{validKeys: vk}
}

// Middleware validates API key and returns modified handler chain.
func (a *APIKeyAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for OPTIONS (CORS preflight) and health checks
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/health" || r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}

		key := a.extractKey(r)
		if key == "" {
			http.Error(w, `{"error": "missing authorization"}`, http.StatusUnauthorized)
			return
		}

		// Constant-time comparison to prevent timing attacks
		valid := false
		for _, validKey := range a.validKeys {
			if subtle.ConstantTimeCompare([]byte(key), []byte(validKey)) == 1 {
				valid = true
				break
			}
		}
		if !valid {
			http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractKey checks the Authorization header (Bearer <key> or <key>),
// and for WebSocket upgrade requests also checks ?token=<key>.
func (a *APIKeyAuth) extractKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	if auth != "" {
		return strings.TrimPrefix(auth, "Bearer ")
	}

	// For WebSocket clients that cannot set custom headers, allow token in query param.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		return r.URL.Query().Get("token")
	}

	return ""
}

// APIKeyFromContext retrieves API key from context.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(string)
	return key, ok
}
