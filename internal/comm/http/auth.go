// Package http provides authentication middleware for the HTTP API.
package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"log/slog"
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "missing authorization",
				"message": "provide API key via Authorization: Bearer <key> header",
			})
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
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTeapot) // 418 - distinctive error for bad API key
			_ = json.NewEncoder(w).Encode(map[string]string{
				"error":   "unauthorized",
				"message": "invalid API key",
			})
			return
		}

		ctx := context.WithValue(r.Context(), apiKeyContextKey, key)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// extractKey checks the Authorization header (Bearer <key>),
// the Sec-WebSocket-Protocol header (for WebSocket clients),
// and as a legacy fallback the ?token=<key> query parameter for WebSocket.
// The query param fallback logs a warning because credentials in the URL are
// visible in server/proxy access logs (Bug S1).
func (a *APIKeyAuth) extractKey(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	// Require the standard "Bearer <token>" scheme. A non-Bearer header
	// (e.g. "Basic <b64>") must NOT be accepted as a raw key — return ""
	// so it is treated as missing auth.
	const bearerPrefix = "Bearer "
	// Case-insensitive prefix match per RFC 7235.
	if len(auth) > len(bearerPrefix) && strings.EqualFold(auth[:len(bearerPrefix)], bearerPrefix) {
		return auth[len(bearerPrefix):]
	}

	// For WebSocket clients, check Sec-WebSocket-Protocol header.
	// Convention: client sends "bearer.<token>" as a subprotocol.
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
			for _, p := range strings.Split(proto, ",") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "bearer.") {
					return p[len("bearer."):]
				}
			}
		}

		// Legacy fallback: query param token.
		// WARNING: credentials in query params are logged in access logs.
		if token := r.URL.Query().Get("token"); token != "" {
			slog.Warn("websocket auth via query param (credentials visible in logs)",
				"remote", r.RemoteAddr,
				"hint", "use Authorization header or Sec-WebSocket-Protocol: bearer.<key>",
			)
			return token
		}
	}

	return ""
}

// APIKeyFromContext retrieves API key from context.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(string)
	return key, ok
}
