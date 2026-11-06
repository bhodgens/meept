package telnyx

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// DefaultSignatureSkewWindow is the maximum allowed drift between a
// signature's telnyx-timestamp and now; anything beyond it is rejected as
// a replay.
const DefaultSignatureSkewWindow = 300 * time.Second

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

// verifySignedMessage is the shared core of both verifiers: decode the
// base64 signature from headers (a MISSING or undecodable header is an
// error, never a pass), enforce the timestamp skew window, then verify the
// Ed25519 signature over "{timestamp}|{message}".
func verifySignedMessage(message []byte, headers http.Header, pub ed25519.PublicKey, skewWindow time.Duration) error {
	sigB64 := headers.Get(headerSignature)
	if sigB64 == "" {
		return fmt.Errorf("telnyx signature: missing %s header", headerSignature)
	}
	ts := headers.Get(headerTimestamp)
	if ts == "" {
		return fmt.Errorf("telnyx signature: missing %s header", headerTimestamp)
	}
	sig, err := base64.StdEncoding.DecodeString(sigB64)
	if err != nil {
		return fmt.Errorf("telnyx signature: decode signature: %w", err)
	}
	tsUnix, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return fmt.Errorf("telnyx signature: parse timestamp: %w", err)
	}
	drift := time.Duration(tsUnix-time.Now().Unix()) * time.Second
	if drift < 0 {
		drift = -drift
	}
	if drift > skewWindow {
		return fmt.Errorf("telnyx signature: timestamp skew %s exceeds window %s", drift, skewWindow)
	}
	if len(sig) != ed25519.SignatureSize {
		return fmt.Errorf("telnyx signature: signature is %d bytes, want %d", len(sig), ed25519.SignatureSize)
	}
	if len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("telnyx signature: public key is %d bytes, want %d", len(pub), ed25519.PublicKeySize)
	}
	if !ed25519.Verify(pub, signedString(ts, message), sig) {
		return fmt.Errorf("telnyx signature: signature mismatch")
	}
	return nil
}

// VerifyWebhookSignature verifies a Telnyx API v2 webhook delivery:
// Ed25519 over the RAW request body (never a re-serialized object), signed
// as "{telnyx-timestamp}|{body}" with the account keypair. Headers carry
// telnyx-signature-ed25519 (base64) and telnyx-timestamp (unix seconds).
// A timestamp skewing more than DefaultSignatureSkewWindow from now is
// rejected as a replay; a missing header is always an error.
func VerifyWebhookSignature(body []byte, headers http.Header, pub ed25519.PublicKey) error {
	return verifySignedMessage(body, headers, pub, DefaultSignatureSkewWindow)
}

// VerifyMediaSocket verifies the signature headers on a media-socket
// handshake: Ed25519 over "{telnyx-timestamp}|{stream_url}" where
// streamURL is the EXACT url passed to streaming_start, including query.
// Header names and skew rules match VerifyWebhookSignature; a missing
// header is always an error.
func VerifyMediaSocket(headers http.Header, streamURL string, pub ed25519.PublicKey) error {
	return verifySignedMessage([]byte(streamURL), headers, pub, DefaultSignatureSkewWindow)
}
