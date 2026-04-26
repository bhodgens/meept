//go:build dev

package security

import (
	"crypto/tls"
)

// insecureSkipVerifyLocked creates a TLS config that skips certificate verification.
// WARNING: This file is only compiled when the "dev" build tag is set.
// In production builds, InsecureSkipVerify is nil and will panic if called,
// preventing accidental use of insecure TLS configurations in production.
// SEC-8 FIX: Build-tag protection prevents production compilation.
func insecureSkipVerifyLocked() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true,
		MinVersion:         tls.VersionTLS12,
	}
}
