// Package tlsutil provides certificate fingerprinting and pinning utilities.
package tlsutil

import (
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
	"net/http"
	"os"
	"strings"
)

// Fingerprint returns the SHA-256 fingerprint of a certificate's raw DER bytes.
func Fingerprint(cert *x509.Certificate) string {
	h := sha256.Sum256(cert.Raw)
	return hex.EncodeToString(h[:])
}

// FingerprintSPKI returns the SHA-256 fingerprint of the Subject Public Key Info.
// This is the preferred pinning target because it survives certificate renewal
// when the same key pair is reused.
func FingerprintSPKI(cert *x509.Certificate) string {
	h := sha256.Sum256(cert.RawSubjectPublicKeyInfo)
	return hex.EncodeToString(h[:])
}

// LoadCertFingerprint computes fingerprints from a PEM-encoded certificate file.
func LoadCertFingerprint(certPath string) (certFP, spkiFP string, err error) {
	data, err := os.ReadFile(certPath)
	if err != nil {
		return "", "", fmt.Errorf("read cert: %w", err)
	}
	block, _ := pem.Decode(data)
	if block == nil {
		return "", "", fmt.Errorf("no PEM block found in %s", certPath)
	}
	cert, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return "", "", fmt.Errorf("parse cert: %w", err)
	}
	return Fingerprint(cert), FingerprintSPKI(cert), nil
}

// SaveFingerprint writes the certificate fingerprint to a file for client discovery.
func SaveFingerprint(path, certFP, spkiFP string) error {
	return os.WriteFile(path, []byte(fmt.Sprintf("cert:%s\nspki:%s\n", certFP, spkiFP)), 0o644) //nolint:gosec
}

// LoadExpectedFingerprint reads the saved fingerprint from disk.
func LoadExpectedFingerprint(path string) (certFP, spkiFP string, err error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", "", err
	}
	lines := strings.Split(string(data), "\n")
	for _, line := range lines {
		if strings.HasPrefix(line, "cert:") {
			certFP = strings.TrimPrefix(line, "cert:")
		}
		if strings.HasPrefix(line, "spki:") {
			spkiFP = strings.TrimPrefix(line, "spki:")
		}
	}
	return strings.TrimSpace(certFP), strings.TrimSpace(spkiFP), nil
}

// PinningVerifier returns a TLS verify callback that pins the server cert to
// an expected fingerprint. Use with crypto/tls.Config.InsecureSkipVerify and
// VerifyConnection.
type PinningVerifier struct {
	ExpectedCertFP string // SHA-256 of raw certificate
	ExpectedSPKIFP string // SHA-256 of RawSubjectPublicKeyInfo
}

// VerifyConnection implements tls.Config.VerifyConnection.
func (pv *PinningVerifier) VerifyConnection(cs tls.ConnectionState) error {
	if len(cs.PeerCertificates) == 0 {
		return fmt.Errorf("no peer certificates presented")
	}
	peer := cs.PeerCertificates[0]
	if pv.ExpectedCertFP != "" {
		actual := Fingerprint(peer)
		if actual != pv.ExpectedCertFP {
			return fmt.Errorf("certificate fingerprint mismatch")
		}
	}
	if pv.ExpectedSPKIFP != "" {
		actual := FingerprintSPKI(peer)
		if actual != pv.ExpectedSPKIFP {
			return fmt.Errorf("SPKI fingerprint mismatch")
		}
	}
	return nil
}

// PinTransport returns an http.RoundTripper that skips CA verification and
// instead verifies the certificate fingerprint directly.
func (pv *PinningVerifier) PinTransport(base http.RoundTripper) http.RoundTripper {
	if base == nil {
		base = http.DefaultTransport
	}
	tt, ok := base.(*http.Transport)
	if !ok {
		return base
	}
	transport := tt.Clone()
	transport.TLSClientConfig = &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // we do custom pinning in VerifyConnection
		VerifyConnection:   pv.VerifyConnection,
		MinVersion:         tls.VersionTLS12,
	}
	return transport
}
