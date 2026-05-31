// Package http provides security hardening for the HTTP API.
package http

import (
	"crypto/tls"
	"fmt"
	"net/http"

	"github.com/caimlas/meept/pkg/tlsutil"
)

// SecurityHeadersConfig controls which security headers are added to responses.
type SecurityHeadersConfig struct {
	EnableHSTS            bool   // Strict-Transport-Security
	HSTSMaxAge            int    // max-age in seconds (default: 31536000 = 1 year)
	HSTSIncludeSubdomains bool   // includeSubDomains directive
	EnableFrameOptions    bool   // X-Frame-Options
	EnableContentTypeOpts bool   // X-Content-Type-Options
	EnableReferrerPolicy  bool   // Referrer-Policy
	EnableCSP             bool   // Content-Security-Policy
}

// DefaultSecurityHeaders returns conservative defaults for a local API.
func DefaultSecurityHeaders() SecurityHeadersConfig {
	return SecurityHeadersConfig{
		EnableHSTS:            true,
		HSTSMaxAge:            31536000, // 1 year
		HSTSIncludeSubdomains: true,
		EnableFrameOptions:    true,
		EnableContentTypeOpts: true,
		EnableReferrerPolicy:  true,
		EnableCSP:             true,
	}
}

// SecurityHeadersMiddleware injects security headers into every response.
func SecurityHeadersMiddleware(cfg SecurityHeadersConfig) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if cfg.EnableHSTS {
				hsts := "max-age=" + fmt.Sprintf("%d", cfg.HSTSMaxAge)
				if cfg.HSTSIncludeSubdomains {
					hsts += "; includeSubDomains"
				}
				w.Header().Set("Strict-Transport-Security", hsts)
			}
			if cfg.EnableFrameOptions {
				w.Header().Set("X-Frame-Options", "DENY")
			}
			if cfg.EnableContentTypeOpts {
				w.Header().Set("X-Content-Type-Options", "nosniff")
			}
			if cfg.EnableReferrerPolicy {
				w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
			}
			if cfg.EnableCSP {
				w.Header().Set("Content-Security-Policy",
					"default-src 'self'; script-src 'none'; object-src 'none'; frame-ancestors 'none'; base-uri 'self'")
			}
			next.ServeHTTP(w, r)
		})
	}
}

// ModernCiphers is the preferred list of TLS cipher suites for TLS 1.2.
// TLS 1.3 ciphers are not configurable in Go, so this only affects TLS 1.2.
var ModernCiphers = []uint16{
	tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
	tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
	tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
}

// BuildTLSConfig creates a hardened tls.Config for the server.
func BuildTLSConfig(minVersion uint16, clientAuth tls.ClientAuthType) *tls.Config {
	return &tls.Config{
		MinVersion:   minVersion,
		CipherSuites: ModernCiphers,
		CurvePreferences: []tls.CurveID{
			tls.X25519,
			tls.CurveP256,
			tls.CurveP384,
		},
		ClientAuth: clientAuth,
	}
}

// Re-export tlsutil helpers for server-side fingerprint management.
// Clients should import pkg/tlsutil directly.
var (
	LoadCertFingerprint    = tlsutil.LoadCertFingerprint
	SaveFingerprint        = tlsutil.SaveFingerprint
	LoadExpectedFingerprint = tlsutil.LoadExpectedFingerprint
)
