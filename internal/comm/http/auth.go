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
	validKeys map[string]bool
}

// NewAPIKeyAuth creates API key authentication with provided keys.
func NewAPIKeyAuth(keys []string) *APIKeyAuth {
	validKeys := make(map[string]bool)
	for _, key := range keys {
		validKeys[key] = true
	}
	return &APIKeyAuth{validKeys: validKeys}
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

		auth := r.Header.Get("Authorization")
		if auth == "" {
			http.Error(w, `{"error": "missing authorization"}`, http.StatusUnauthorized)
			return
		}

		// Support "Bearer <key>" or just "<key>"
		key := strings.TrimPrefix(auth, "Bearer ")

		// Check if key is valid
		if !a.validKeys[key] {
			// Constant-time comparison to prevent timing attacks
			valid := false
			for validKey := range a.validKeys {
				if subtle.ConstantTimeCompare([]byte(key), []byte(validKey)) == 1 {
					valid = true
					break
				}
			}
			if !valid {
				http.Error(w, `{"error": "unauthorized"}`, http.StatusUnauthorized)
				return
			}
		}

		ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// APIKeyFromContext retrieves API key from context.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(string)
	return key, ok
}
