// Package tlsutil provides certificate fingerprinting and pinning utilities.
package tlsutil

import (
	"crypto/sha256"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"fmt"
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
	for line := range strings.SplitSeq(string(data), "\n") {
		if after, ok := strings.CutPrefix(line, "cert:"); ok {
			certFP = after
		}
		if after, ok := strings.CutPrefix(line, "spki:"); ok {
			spkiFP = after
		}
	}
	return strings.TrimSpace(certFP), strings.TrimSpace(spkiFP), nil
}

