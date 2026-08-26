// identity.go provides the store-backed authentication middleware used in
// multi-user mode. When the server is configured with a non-nil AuthStore,
// this middleware REPLACES the legacy flat-key APIKeyAuth: presented keys are
// validated against the shared users store and a validated request carries
// an *auth.Identity in its context, retrievable via IdentityFromContext.
//
// Middleware ORDER is unchanged (rate limit → auth): s.middleware composes
// auth after rate limiting exactly as before; only the handler swapped in
// differs. Health/OPTIONS exemptions mirror APIKeyAuth.Middleware.
package http

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/session"
)

// ctxIdentityKey is the unexported context key type carrying the
// authenticated principal. Distinct from apiKeyContextKey so both values can
// coexist without collision.
type ctxIdentityKey struct{}

// AuthStore validates raw API keys against the multi-user users store.
//
// A typed-nil *auth.Store is treated as absent by NewServer's nil guard, so
// implementations of this interface are always live stores.
type AuthStore interface {
	Validate(rawKey string, now time.Time) (*auth.Identity, error)
}

// StoreAuth wraps an auth store as authentication middleware. It enforces
// the same bearer-key extraction rules (Authorization header first,
// Sec-WebSocket-Protocol "bearer.<key>" subprotocol for WebSocket clients)
// as the legacy APIKeyAuth middleware.
type StoreAuth struct {
	store AuthStore
}

// NewStoreAuth creates store-backed authentication middleware. A nil store
// yields a middleware that rejects everything (callers must nil-guard before
// wiring it — see Server.middleware).
func NewStoreAuth(store AuthStore) *StoreAuth {
	return &StoreAuth{store: store}
}

// Middleware validates the presented key against the users store and returns
// the wrapped handler chain.
//
// Response contract:
//   - missing key            → 401 (same shape as legacy middleware)
//   - unknown/invalid key    → 418 "invalid or expired API key"
//   - expired key            → 418 with expiry-specific message
//   - valid key              → next handler with identity in context
func (a *StoreAuth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip auth for OPTIONS (CORS preflight) and health checks —
		// identical exemptions to the legacy APIKeyAuth middleware.
		if r.Method == http.MethodOptions {
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/health" || r.URL.Path == "/api/v1/health" {
			next.ServeHTTP(w, r)
			return
		}

		key := ExtractKeyFromRequest(r)
		if key == "" {
			writeAuthError(w, http.StatusUnauthorized, "missing authorization",
				"provide API key via Authorization: Bearer *** header")
			return
		}

		identity, err := a.store.Validate(key, time.Now())
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusTeapot) // 418 - distinctive error for bad API key
			msg := "invalid API key"
			switch {
			case errors.Is(err, auth.ErrExpiredKey):
				msg = "API key expired"
			case errors.Is(err, auth.ErrEmptyKey), errors.Is(err, auth.ErrUnknownKey):
				msg = "invalid API key"
			}
			writeAuthError(w, http.StatusTeapot, "unauthorized", msg)
			return
		}

		ctx := context.WithValue(r.Context(), ctxIdentityKey{}, identity) //nolint:staticcheck // keyed by unexported type per project convention
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// IdentityFromContext retrieves the authenticated principal from a request
// context. The second return is false when no authenticated identity is
// attached (legacy single-key mode, unauthenticated health checks, etc.).
func IdentityFromContext(ctx context.Context) (*auth.Identity, bool) {
	id, ok := ctx.Value(ctxIdentityKey{}).(*auth.Identity)
	if id == nil {
		return nil, false
	}
	return id, ok
}

// viewerFromContext converts a request-context identity into a session
// ownership Viewer. Returns nil when no identity is present (legacy mode),
// which downstream stores treat as unfiltered.
func viewerFromContext(ctx context.Context) *session.Viewer {
	identity, ok := IdentityFromContext(ctx)
	if !ok || identity == nil || identity.UserID == "" {
		return nil
	}
	return session.NewViewer(identity.UserID)
}

// writeAuthError emits the JSON auth-error body. An encode failure here
// means the client is gone or hostile after the status line was already
// written; there is no meaningful recovery. The error is logged at debug
// level so failures are never fully silent.
func writeAuthError(w http.ResponseWriter, status int, errKind, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if encErr := json.NewEncoder(w).Encode(map[string]string{"error": errKind, "message": msg}); encErr != nil {
		slog.Debug("auth error body encode failed", "error", encErr)
	}
}
