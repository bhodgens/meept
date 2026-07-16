// Package http provides authentication middleware for the HTTP API.
package http

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
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

// extractKey checks the Authorization header (Bearer <key>) and, for
// WebSocket upgrade requests, the Sec-WebSocket-Protocol header
// (convention: "bearer.<key>" subprotocol).
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
	}

	return ""
}


// ExtractKeyFromRequest extracts the API key from HTTP request headers or
// Sec-WebSocket-Protocol header using the same logic as the APIKeyAuth middleware.
// Returns the key if found, empty string if missing.
// Priority: Authorization header > Sec-WebSocket-Protocol
func ExtractKeyFromRequest(r *http.Request) string {
	auth := r.Header.Get("Authorization")
	const bearerPrefix = "Bearer "
	if len(auth) > len(bearerPrefix) && strings.EqualFold(auth[:len(bearerPrefix)], bearerPrefix) {
		return auth[len(bearerPrefix):]
	}

	// For WebSocket clients, check Sec-WebSocket-Protocol header
	if strings.EqualFold(r.Header.Get("Upgrade"), "websocket") {
		if proto := r.Header.Get("Sec-WebSocket-Protocol"); proto != "" {
			for _, p := range strings.Split(proto, ",") {
				p = strings.TrimSpace(p)
				if strings.HasPrefix(p, "bearer.") {
					return p[len("bearer."):]
				}
			}
		}
	}
	return ""
}

// APIKeyFromContext retrieves API key from context.
func APIKeyFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(apiKeyContextKey).(string)
	return key, ok
}

// rateLimiter implements per-IP rate limiting using a token bucket algorithm.
// Default: 100 requests per minute per IP, burst of 20.
// Limiters are pruned after 10 minutes of inactivity to prevent unbounded growth.
type rateLimiter struct {
	mu         sync.RWMutex
	limiters   map[string]*rate.Limiter
	lastAccess map[string]time.Time
	r          rate.Limit // tokens per second
	burst      int
	pruneAfter time.Duration // Time after which inactive limiters are pruned
}

// pruneInterval is the interval between limiter pruning operations.
const pruneInterval = 5 * time.Minute

// newRateLimiter creates a rate limiter for per-IP rate limiting.
// r = requests per second, burst = max burst size.
func newRateLimiter(r float64, burst int) *rateLimiter {
	rl := &rateLimiter{
		limiters:   make(map[string]*rate.Limiter),
		lastAccess: make(map[string]time.Time),
		r:          rate.Limit(r),
		burst:      burst,
		pruneAfter: 10 * time.Minute,
	}
	// Start background pruner
	go rl.pruneLoop()
	return rl
}

// pruneLoop periodically removes inactive limiters to prevent unbounded growth.
func (rl *rateLimiter) pruneLoop() {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()
	for range ticker.C {
		rl.pruneInactive()
	}
}

// pruneInactive removes limiters that haven't been accessed within pruneAfter.
func (rl *rateLimiter) pruneInactive() {
	rl.mu.Lock()
	defer rl.mu.Unlock()
	now := time.Now()
	cutoff := now.Add(-rl.pruneAfter)
	for ip, last := range rl.lastAccess {
		if last.Before(cutoff) {
			delete(rl.limiters, ip)
			delete(rl.lastAccess, ip)
		}
	}
}

// getLimiter returns (or creates) a rate limiter for the given IP.
// Tracks last access time for pruning inactive limiters.
func (rl *rateLimiter) getLimiter(ip string) *rate.Limiter {
	// Fast path: read lock check (skip time tracking for performance)
	rl.mu.RLock()
	limiter, exists := rl.limiters[ip]
	rl.mu.RUnlock()

	if exists {
		// Update last access time lazily (only if old)
		rl.mu.Lock()
		rl.lastAccess[ip] = time.Now()
		rl.mu.Unlock()
		return limiter
	}

	// Slow path: create new limiter
	rl.mu.Lock()
	defer rl.mu.Unlock()

	// Double-check after acquiring write lock
	if limiter, exists = rl.limiters[ip]; exists {
		rl.lastAccess[ip] = time.Now()
		return limiter
	}

	limiter = rate.NewLimiter(rl.r, rl.burst)
	rl.limiters[ip] = limiter
	rl.lastAccess[ip] = time.Now()
	return limiter
}

// allow checks if a request from the given IP is allowed.
func (rl *rateLimiter) allow(ip string) bool {
	return rl.getLimiter(ip).Allow()
}

// RateLimitMiddleware creates middleware that rate limits requests per IP.
// limitPerMinute = max requests per minute, burst = max burst size.
// Example: 100 req/min with burst of 20 allows 20 rapid requests, then 100/min sustained.
func RateLimitMiddleware(limitPerMinute int, burst int) func(http.Handler) http.Handler {
	rl := newRateLimiter(float64(limitPerMinute)/60.0, burst)

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			// Skip rate limiting for health checks
			if r.URL.Path == "/health" || r.URL.Path == "/api/v1/health" {
				next.ServeHTTP(w, r)
				return
			}

			ip := extractClientIP(r)
			if !rl.allow(ip) {
				w.Header().Set("Content-Type", "application/json")
				w.WriteHeader(http.StatusTooManyRequests)
				_ = json.NewEncoder(w).Encode(map[string]string{
					"error":   "rate limit exceeded",
					"message": "too many requests, please slow down",
				})
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// extractClientIP extracts the client IP from the request.
// It checks X-Forwarded-For and X-Real-IP headers first (if behind a proxy),
// then falls back to RemoteAddr.
func extractClientIP(r *http.Request) string {
	// Check X-Forwarded-For header (can contain multiple IPs, first is the client)
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		ips := strings.Split(xff, ",")
		if len(ips) > 0 {
			return strings.TrimSpace(ips[0])
		}
	}

	// Check X-Real-IP header
	if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
		return realIP
	}

	// Fall back to RemoteAddr
	host, err := splitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// splitHostPort wraps net.SplitHostPort for cleaner error handling.
func splitHostPort(addr string) (string, error) {
	host, _, err := net.SplitHostPort(addr)
	return host, err
}
