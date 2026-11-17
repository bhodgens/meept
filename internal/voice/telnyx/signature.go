package telnyx

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// DefaultSignatureSkewWindow is the maximum allowed age of a signature's
// telnyx-timestamp relative to now; anything older is rejected as a replay.
const DefaultSignatureSkewWindow = 300 * time.Second

// DefaultSignatureFutureSkewWindow is the maximum clock drift a
// telnyx-timestamp may run AHEAD of now. The window is intentionally
// asymmetric — generous into the past (provider redelivery), tight into the
// future (a future timestamp is clock skew at best, a replay-harvesting
// vector at worst). Ported asymmetry from strategist-usa
// go-web-hooks/internal/webhook/signature.go:247-263 (5m past / 1m future);
// the frozen meept contract pins 300s past / 60s future.
const DefaultSignatureFutureSkewWindow = 60 * time.Second

// spkiDERHeader is the ASN.1 DER encoding prefix for ED25519 SPKI keys.
// Structure: SEQUENCE { SEQUENCE { OID 1.3.101.112 }, BIT STRING { public key } }.
// Ported verbatim from strategist-usa go-web-hooks
// internal/webhook/signature.go:28 (ED25519_SPKI_HEADER). 12 bytes; the full
// SPKI key is header+raw = 44 bytes. (Named spkiDERHeader, not the strategist
// name, so it cannot collide with the gate test's ed25519SPKIHeader helper.)
var spkiDERHeader = []byte{0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00}

// Webhook / media-socket signature headers (Telnyx Ed25519 scheme).
const (
	headerSignature = "Telnyx-Signature-Ed25519"
	headerTimestamp = "Telnyx-Timestamp"
)

// signedString builds the string the account private key signs:
// "{telnyx-timestamp}|{message}" where message is the raw webhook body
// (webhooks) or the exact stream URL including query (media sockets).
func signedString(timestamp string, message []byte) []byte {
	signed := make([]byte, 0, len(timestamp)+1+len(message))
	signed = append(signed, timestamp...)
	signed = append(signed, '|')
	signed = append(signed, message...)
	return signed
}

// parseTelnyxTimestamp parses a Telnyx timestamp header into a time.Time:
// unix-seconds primary, RFC3339 fallback. strconv.ParseInt is used
// deliberately — it rejects trailing garbage like "123abc" that
// fmt.Sscanf("%d", &x) silently accepts as 123.
//
// Ported from strategist-usa go-web-hooks/internal/webhook/signature.go:229-245.
func parseTelnyxTimestamp(ts string) (time.Time, error) {
	if unix, err := strconv.ParseInt(ts, 10, 64); err == nil {
		return time.Unix(unix, 0), nil
	}
	parsed, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		return time.Time{}, fmt.Errorf("telnyx signature: parse timestamp %q: not unix-seconds or RFC3339", ts)
	}
	return parsed, nil
}

// checkTimestampSkew enforces the asymmetric replay window against an
// already-parsed timestamp: reject anything older than maxPast or newer than
// now+maxFuture. Ported from strategist-usa
// go-web-hooks/internal/webhook/signature.go:247-263.
func checkTimestampSkew(ts time.Time, maxPast, maxFuture time.Duration) error {
	age := time.Since(ts)
	if age > maxPast {
		return fmt.Errorf("telnyx signature: timestamp is %s old, exceeds replay window %s", age.Truncate(time.Second), maxPast)
	}
	if age < -maxFuture {
		return fmt.Errorf("telnyx signature: timestamp is %s in the future, exceeds clock-drift window %s", (-age).Truncate(time.Second), maxFuture)
	}
	return nil
}

// decodeSignature decodes and length-checks a base64 Ed25519 signature
// header (64 bytes). Empty or undecodable headers are errors, never passes.
func decodeSignature(sigB64 string) ([]byte, error) {
	if sigB64 == "" {
		return nil, fmt.Errorf("telnyx signature: missing %s header", headerSignature)
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return nil, fmt.Errorf("telnyx signature: decode signature: %w", err)
	}
	if len(sig) != ed25519.SignatureSize {
		return nil, fmt.Errorf("telnyx signature: signature is %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	return sig, nil
}

// normalizePublicKey converts a stored Ed25519 public key into a usable
// ed25519.PublicKey. Accepts either a raw 32-byte key (used directly) or a
// 44-byte SPKI DER key whose first 12 bytes must equal spkiDERHeader.
// Anything else — including 44 bytes with a corrupted or absent header — is
// rejected, never blindly trusted.
//
// The SPKI branch detaches the extracted key with a copy: returning an alias
// into pubKey's backing array would let a later mutation of the caller's
// buffer retroactively corrupt the verified key (intermittent verification
// failures, extremely hard to reproduce).
//
// Ported from strategist-usa go-web-hooks/internal/webhook/signature.go:291-309
// (switch branch incl. the detach-copy fix). The header compare is NOT
// constant-time — acceptable: the header is a fixed, public, well-known
// value, not secret material.
func normalizePublicKey(pubKey []byte) (ed25519.PublicKey, error) {
	switch {
	case len(pubKey) == ed25519.PublicKeySize:
		// Raw 32-byte key — use directly (same behavior as the legacy v1 path).
		return ed25519.PublicKey(pubKey), nil
	case len(pubKey) == 44 && bytes.Equal(pubKey[:12], spkiDERHeader):
		// SPKI DER — extract the raw key, copying to detach the alias.
		pub := make([]byte, ed25519.PublicKeySize)
		copy(pub, pubKey[12:])
		return ed25519.PublicKey(pub), nil
	default:
		return nil, fmt.Errorf("telnyx signature: invalid public key length %d (want raw %d-byte or 44-byte SPKI)",
			len(pubKey), ed25519.PublicKeySize)
	}
}

// verifyWebhookCore is the shared verification pipeline behind both
// entrypoints: header/signature hygiene, timestamp parse (unix or RFC3339),
// asymmetric skew, key normalization, Ed25519 verify over
// "{timestamp}|{message}".
func verifyWebhookCore(message []byte, sigB64, tsHeader string, pubKey []byte, maxPast, maxFuture time.Duration) error {
	sig, err := decodeSignature(sigB64)
	if err != nil {
		return err
	}
	if tsHeader == "" {
		return fmt.Errorf("telnyx signature: missing %s header", headerTimestamp)
	}
	ts, err := parseTelnyxTimestamp(tsHeader)
	if err != nil {
		return err
	}
	if err := checkTimestampSkew(ts, maxPast, maxFuture); err != nil {
		return err
	}
	pub, err := normalizePublicKey(pubKey)
	if err != nil {
		return err
	}
	if !ed25519.Verify(pub, signedString(tsHeader, message), sig) {
		return fmt.Errorf("telnyx signature: signature mismatch")
	}
	return nil
}

// VerifyWebhook is the signature-v2 entrypoint (frozen contract, plan
// 20261110-telnyx-texting master §1 item 1): verifies a Telnyx webhook
// delivery given the already-extracted header values and the RAW body.
//
//	pubKey:  raw 32-byte Ed25519 key OR 44-byte SPKI DER key
//	sigB64:  base64 signature from Telnyx-Signature-Ed25519
//	tsHeader: unix-seconds OR RFC3339 timestamp from Telnyx-Timestamp
//	body:    the exact raw bytes received (verify-then-parse; never a
//	         re-serialized object)
//
// The skew window is asymmetric: 300s into the past, 60s into the future
// (v1's symmetric ±300 must NOT survive here — a future-dated replay is a
// replay-harvesting vector that the past window alone would admit).
func VerifyWebhook(pubKey []byte, sigB64, tsHeader string, body []byte) error {
	return verifyWebhookCore(body, sigB64, tsHeader, pubKey,
		DefaultSignatureSkewWindow, DefaultSignatureFutureSkewWindow)
}

// verifySignedMessage is the shared core of the v1 header-based verifiers:
// pull the signature/timestamp headers off an http.Header, then run the v2
// core. Raw-32-byte ed25519.PublicKey inputs take the same accept path as
// before (byte-identical behavior for the existing key type).
func verifySignedMessage(message []byte, headers http.Header, pub ed25519.PublicKey, maxPast, maxFuture time.Duration) error {
	return verifyWebhookCore(message, headers.Get(headerSignature), headers.Get(headerTimestamp),
		[]byte(pub), maxPast, maxFuture)
}

// VerifyWebhookSignature verifies a Telnyx API v2 webhook delivery:
// Ed25519 over the RAW request body (never a re-serialized object), signed
// as "{telnyx-timestamp}|{body}" with the account keypair. Headers carry
// telnyx-signature-ed25519 (base64) and telnyx-timestamp (unix seconds,
// RFC3339 accepted as fallback). The skew window is asymmetric —
// DefaultSignatureSkewWindow into the past, DefaultSignatureFutureSkewWindow
// into the future; a missing header is always an error.
func VerifyWebhookSignature(body []byte, headers http.Header, pub ed25519.PublicKey) error {
	return verifySignedMessage(body, headers, pub, DefaultSignatureSkewWindow, DefaultSignatureFutureSkewWindow)
}

// VerifyMediaSocket verifies the signature headers on a media-socket
// handshake: Ed25519 over "{telnyx-timestamp}|{stream_url}" where
// streamURL is the EXACT url passed to streaming_start, including query.
// Header names and skew rules match VerifyWebhookSignature; a missing
// header is always an error.
func VerifyMediaSocket(headers http.Header, streamURL string, pub ed25519.PublicKey) error {
	return verifySignedMessage([]byte(streamURL), headers, pub, DefaultSignatureSkewWindow, DefaultSignatureFutureSkewWindow)
}

// constantTimeSignatureMatch compares an expected signature against a list
// of candidate signatures in constant time.
//
// subtle.ConstantTimeCompare returns 0 immediately when its inputs differ in
// length, which leaks the length of the expected value through timing when
// callers iterate over variable-length candidates. To eliminate this leak we
// hash both sides with SHA-256 and compare the resulting fixed-length 32-byte
// digests. Every candidate is hashed and compared regardless of length or
// content, so timing only reveals "no match" vs "match" and nothing about the
// expected signature itself.
//
// Note: an empty candidate is NOT short-circuited. sha256.Sum256 of the empty
// string is well-defined, and special-casing it would make iteration cost
// depend on candidate content — defeating the constant-time property. An
// empty expected value NEVER matches (guard preserved from the source port).
//
// Ported from strategist-usa
// go-web-hooks/internal/webhook/signature.go:431-463.
func constantTimeSignatureMatch(expected string, candidates []string) bool {
	if expected == "" || len(candidates) == 0 {
		return false
	}
	expectedSum := sha256.Sum256([]byte(expected))
	matched := byte(0)
	for _, candidate := range candidates {
		candidateSum := sha256.Sum256([]byte(candidate))
		// OR the comparison result into matched; subtle.ConstantTimeCompare
		// returns 0 or 1. OR-ing is constant-time and avoids short-circuit.
		if cmp := subtle.ConstantTimeCompare(expectedSum[:], candidateSum[:]); cmp == 0 || cmp == 1 {
			matched |= byte(cmp)
		}
	}
	return matched == 1
}
