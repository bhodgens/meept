// Package web provides the HTTP API server for meept.
package web

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// Authenticator is the interface for request authentication.
type Authenticator interface {
	Authenticate(r *http.Request) bool
}

// NoAuth allows all requests.
type NoAuth struct{}

// Authenticate always returns true.
func (NoAuth) Authenticate(r *http.Request) bool {
	return true
}

// BearerAuth authenticates using a Bearer token.
type BearerAuth struct {
	tokens []string
}

// NewBearerAuth creates a new BearerAuth with the given tokens.
func NewBearerAuth(tokens ...string) *BearerAuth {
	return &BearerAuth{tokens: tokens}
}

// Authenticate checks the Authorization header for a valid Bearer token.
func (a *BearerAuth) Authenticate(r *http.Request) bool {
	auth := r.Header.Get("Authorization")
	if auth == "" {
		return false
	}

	parts := strings.SplitN(auth, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
		return false
	}

	token := parts[1]
	// Use constant-time comparison to prevent timing attacks
	for _, validToken := range a.tokens {
		if subtle.ConstantTimeCompare([]byte(token), []byte(validToken)) == 1 {
			return true
		}
	}
	return false
}

// BasicAuth authenticates using HTTP Basic Auth.
// Passwords are stored as bcrypt hashes.
type BasicAuth struct {
	users map[string]string // username -> bcrypt hash
}

// NewBasicAuth creates a new BasicAuth with the given plaintext credentials.
// Passwords are automatically hashed with bcrypt.
func NewBasicAuth(users map[string]string) *BasicAuth {
	hashed := make(map[string]string, len(users))
	for username, password := range users {
		hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
		if err != nil {
			// If bcrypt fails, skip this user (should not happen with valid input)
			continue
		}
		hashed[username] = string(hash)
	}
	return &BasicAuth{users: hashed}
}

// SetCredentials sets or updates credentials for a user.
// The password is hashed with bcrypt before storage.
func (a *BasicAuth) SetCredentials(username, password string) error {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	a.users[username] = string(hash)
	return nil
}

// Authenticate checks the Authorization header for valid Basic credentials.
func (a *BasicAuth) Authenticate(r *http.Request) bool {
	username, password, ok := r.BasicAuth()
	if !ok {
		return false
	}

	hashedPassword, exists := a.users[username]
	if !exists {
		return false
	}

	// bcrypt comparison is inherently constant-time
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password)) == nil
}

// APIKeyAuth authenticates using an API key header or query parameter.
type APIKeyAuth struct {
	keys       []string
	headerName string
	queryParam string
}

// NewAPIKeyAuth creates a new APIKeyAuth.
func NewAPIKeyAuth(keys []string, headerName, queryParam string) *APIKeyAuth {
	if headerName == "" {
		headerName = "X-API-Key"
	}
	if queryParam == "" {
		queryParam = "api_key"
	}

	return &APIKeyAuth{
		keys:       keys,
		headerName: headerName,
		queryParam: queryParam,
	}
}

// Authenticate checks for a valid API key in header or query param.
func (a *APIKeyAuth) Authenticate(r *http.Request) bool {
	// Check header first
	if key := r.Header.Get(a.headerName); key != "" {
		return a.constantTimeKeyCheck(key)
	}

	// Check query parameter
	if key := r.URL.Query().Get(a.queryParam); key != "" {
		return a.constantTimeKeyCheck(key)
	}

	return false
}

// constantTimeKeyCheck checks if the provided key matches any valid key using
// constant-time comparison to prevent timing attacks.
func (a *APIKeyAuth) constantTimeKeyCheck(key string) bool {
	for _, validKey := range a.keys {
		if subtle.ConstantTimeCompare([]byte(key), []byte(validKey)) == 1 {
			return true
		}
	}
	return false
}

// ChainAuth tries multiple authenticators in order.
type ChainAuth struct {
	authenticators []Authenticator
}

// NewChainAuth creates a new ChainAuth.
func NewChainAuth(authenticators ...Authenticator) *ChainAuth {
	return &ChainAuth{authenticators: authenticators}
}

// Authenticate returns true if any authenticator succeeds.
func (a *ChainAuth) Authenticate(r *http.Request) bool {
	for _, auth := range a.authenticators {
		if auth.Authenticate(r) {
			return true
		}
	}
	return false
}

// IPWhitelistAuth authenticates based on client IP.
type IPWhitelistAuth struct {
	allowed      map[string]bool
	trustedProxy bool // If true, X-Forwarded-For and X-Real-IP headers are trusted.
}

// NewIPWhitelistAuth creates a new IPWhitelistAuth.
func NewIPWhitelistAuth(ips ...string) *IPWhitelistAuth {
	m := make(map[string]bool, len(ips))
	for _, ip := range ips {
		m[ip] = true
	}
	return &IPWhitelistAuth{allowed: m}
}

// NewIPWhitelistAuthWithProxy creates a new IPWhitelistAuth that trusts
// forwarded headers (use only when the server is behind a known trusted proxy).
func NewIPWhitelistAuthWithProxy(ips ...string) *IPWhitelistAuth {
	auth := NewIPWhitelistAuth(ips...)
	auth.trustedProxy = true
	return auth
}

// Authenticate checks if the client IP is whitelisted.
func (a *IPWhitelistAuth) Authenticate(r *http.Request) bool {
	ip := a.extractIP(r)
	return a.allowed[ip]
}

// extractIP returns the client IP. When trustedProxy is false (the default),
// only r.RemoteAddr is used to prevent header spoofing. When trustedProxy is
// true, X-Forwarded-For and X-Real-IP headers are consulted.
func (a *IPWhitelistAuth) extractIP(r *http.Request) string {
	if a.trustedProxy {
		// Check X-Forwarded-For header first
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			ips := strings.Split(xff, ",")
			if len(ips) > 0 {
				ip := strings.TrimSpace(ips[0])
				if a.allowed[ip] {
					return ip
				}
			}
		}

		// Check X-Real-IP header
		if realIP := r.Header.Get("X-Real-IP"); realIP != "" {
			if a.allowed[realIP] {
				return realIP
			}
		}
	}

	// Use RemoteAddr as the primary/authoritative source.
	// Use net.SplitHostPort to correctly handle IPv6 addresses (e.g. "[::1]:12345").
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		host = r.RemoteAddr
	}
	ip := strings.Trim(host, "[]")
	return ip
}
