// Package security provides security-related functionality for meept.
package security

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"os"
)

// TLSConfig represents TLS configuration options.
type TLSConfig struct {
	CertFile   string // Path to certificate file
	KeyFile    string // Path to private key file
	CAFile     string // Path to CA certificate file (for client verification)
	MinVersion uint16 // Minimum TLS version (default: TLS 1.2)
	MaxVersion uint16 // Maximum TLS version (default: TLS 1.3)
	VerifyMode string // "none", "optional", "require" (for mTLS)
}

// DefaultTLSConfig returns a secure default TLS configuration.
func DefaultTLSConfig() TLSConfig {
	return TLSConfig{
		MinVersion: tls.VersionTLS12,
		MaxVersion: tls.VersionTLS13,
		VerifyMode: "require",
	}
}

// ServerTLSConfig creates a tls.Config for server use.
func ServerTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	if cfg.CertFile == "" || cfg.KeyFile == "" {
		return nil, fmt.Errorf("certificate and key files are required")
	}

	cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
		MinVersion:   cfg.MinVersion,
		MaxVersion:   cfg.MaxVersion,
	}

	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}
	if tlsConfig.MaxVersion == 0 {
		tlsConfig.MaxVersion = tls.VersionTLS13
	}

	// Configure client authentication (mTLS)
	if cfg.CAFile != "" {
		caData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.ClientCAs = caPool

		switch cfg.VerifyMode {
		case "none":
			tlsConfig.ClientAuth = tls.NoClientCert
		case "optional":
			tlsConfig.ClientAuth = tls.VerifyClientCertIfGiven
		case "require", "":
			tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
		default:
			return nil, fmt.Errorf("invalid verify mode: %s", cfg.VerifyMode)
		}
	}

	// Use secure cipher suites
	tlsConfig.CipherSuites = []uint16{
		tls.TLS_ECDHE_ECDSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_RSA_WITH_AES_256_GCM_SHA384,
		tls.TLS_ECDHE_ECDSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_RSA_WITH_CHACHA20_POLY1305_SHA256,
		tls.TLS_ECDHE_ECDSA_WITH_AES_128_GCM_SHA256,
		tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256,
	}

	return tlsConfig, nil
}

// ClientTLSConfig creates a tls.Config for client use.
func ClientTLSConfig(cfg TLSConfig) (*tls.Config, error) {
	tlsConfig := &tls.Config{
		MinVersion: cfg.MinVersion,
		MaxVersion: cfg.MaxVersion,
	}

	if tlsConfig.MinVersion == 0 {
		tlsConfig.MinVersion = tls.VersionTLS12
	}
	if tlsConfig.MaxVersion == 0 {
		tlsConfig.MaxVersion = tls.VersionTLS13
	}

	// Load client certificate if provided (for mTLS)
	if cfg.CertFile != "" && cfg.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(cfg.CertFile, cfg.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load client certificate: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{cert}
	}

	// Load CA certificate if provided
	if cfg.CAFile != "" {
		caData, err := os.ReadFile(cfg.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}

		caPool := x509.NewCertPool()
		if !caPool.AppendCertsFromPEM(caData) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		tlsConfig.RootCAs = caPool
	}

	return tlsConfig, nil
}

// InsecureSkipVerify creates a TLS config that skips certificate verification.
// WARNING: This should only be used for testing/development.
func InsecureSkipVerify() *tls.Config {
	return &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // development mode only; production uses proper TLS
		MinVersion:         tls.VersionTLS12,
	}
}

// VersionString returns a human-readable TLS version string.
func VersionString(version uint16) string {
	switch version {
	case tls.VersionTLS10:
		return "TLS 1.0"
	case tls.VersionTLS11:
		return "TLS 1.1"
	case tls.VersionTLS12:
		return "TLS 1.2"
	case tls.VersionTLS13:
		return "TLS 1.3"
	default:
		return fmt.Sprintf("Unknown (0x%04x)", version)
	}
}
