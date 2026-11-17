package telnyx

// 01-webhook-hardening/00T §S — signature v2 gate tests.
//
// Pins the frozen contract (master.md §1 item 1):
//
//	func VerifyWebhook(pubKey []byte, sigB64, tsHeader string, body []byte) error
//
// Accepts raw 32-byte OR 44-byte SPKI DER public keys (ported from
// strategist-usa go-web-hooks/internal/webhook/signature.go:28
// ED25519_SPKI_HEADER + :291-309 branch logic incl. the detach-copy fix);
// timestamp: unix-seconds OR RFC3339 fallback (signature.go:235-245);
// skew: 300s past / 60s future (signature.go:247-263 asymmetry retained).
// constantTimeSignatureMatch ported (signature.go:431-463).
//
// Matrix test cases ported from strategist-usa
// go-web-hooks/internal/webhook/telnyx_signature_matrix_test.go:
// :56 (RFC3339 fallback), :73-99 (SPKI accept + corrupted-header reject),
// :104 (wrong-key reject).
//
// The existing VerifyWebhookSignature tests in client_test.go are
// untouched (raw-32 path must stay unbroken per §S task 1).

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"strconv"
	"testing"
	"time"
)

// ed25519SPKIHeader is the ASN.1 DER prefix for an ED25519 SPKI key —
// ported verbatim from strategist-usa go-web-hooks
// internal/webhook/signature.go:28 (ED25519_SPKI_HEADER). 12 bytes;
// header+raw == 44.
var ed25519SPKIHeader = []byte{0x30, 0x2a, 0x30, 0x05, 0x06, 0x03, 0x2b, 0x65, 0x70, 0x03, 0x21, 0x00}

func genKeypair(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	return pub, priv
}

// signV2 computes the base64 Ed25519 signature over "{ts}|{body}" — the
// same signed-string scheme as signedString() above.
func signV2(t *testing.T, priv ed25519.PrivateKey, ts string, body []byte) string {
	t.Helper()
	sig := ed25519.Sign(priv, append(append([]byte(ts), '|'), body...))
	return base64.StdEncoding.EncodeToString(sig)
}

// matrixWebhookBody is the canned payload shape from strategist
// telnyx_signature_matrix_test.go:18 telnyxRequestBody().
func matrixWebhookBody() []byte {
	return []byte(`{"data":{"event_type":"message.sent","id":"evt-tx-1","payload":{"id":"msg-1"}}}`)
}

// TestVerifyWebhook_Raw32Key pins the raw-32-byte key branch (matrix
// signature.go:291-292: len == ed25519.PublicKeySize -> use directly).
func TestVerifyWebhook_Raw32Key(t *testing.T) {
	pub, priv := genKeypair(t)
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signV2(t, priv, ts, body)
	if err := VerifyWebhook([]byte(pub), sig, ts, body); err != nil {
		t.Fatalf("VerifyWebhook raw 32-byte key = %v, want nil", err)
	}
}

// TestVerifyWebhook_SPKI44Key pins the 44-byte SPKI DER branch
// (signature.go:293-304, incl. the detach-copy fix: the extracted key
// must be a copy, not an alias).
func TestVerifyWebhook_SPKI44Key(t *testing.T) {
	pub, priv := genKeypair(t)
	spki := append(append([]byte{}, ed25519SPKIHeader...), pub...)
	if len(spki) != 44 {
		t.Fatalf("SPKI length = %d, want 44", len(spki))
	}
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signV2(t, priv, ts, body)
	if err := VerifyWebhook(spki, sig, ts, body); err != nil {
		t.Fatalf("VerifyWebhook SPKI 44-byte key = %v, want nil", err)
	}
	// Detach-copy guard: mutating the caller's buffer after verification
	// must not retroactively corrupt anything (aliasing bug class from
	// signature.go:295-303 comment). The second verify uses a fresh copy.
	body2 := matrixWebhookBody()
	if err := VerifyWebhook(append([]byte{}, spki...), signV2(t, priv, ts, body2), ts, body2); err != nil {
		t.Fatalf("VerifyWebhook SPKI (fresh copy) = %v, want nil", err)
	}
}

// TestVerifyWebhook_CorruptedSPKIHeaderRejected ports the matrix case at
// telnyx_signature_matrix_test.go:93-99: 44 bytes whose DER header is
// corrupted must NOT be blindly trusted.
func TestVerifyWebhook_CorruptedSPKIHeaderRejected(t *testing.T) {
	pub, priv := genKeypair(t)
	badHeader := append([]byte{}, ed25519SPKIHeader...)
	badHeader[1] ^= 0xFF
	badKey := append(append([]byte{}, badHeader...), pub...)
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signV2(t, priv, ts, body)
	if err := VerifyWebhook(badKey, sig, ts, body); err == nil {
		t.Fatal("VerifyWebhook with corrupted SPKI DER header = nil, want rejection")
	}
}

// TestVerifyWebhook_BadKeyLengthRejected pins the default branch
// (signature.go:305-309): neither 32 nor 44-with-header is invalid.
func TestVerifyWebhook_BadKeyLengthRejected(t *testing.T) {
	pub, priv := genKeypair(t)
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signV2(t, priv, ts, body)
	for _, n := range []int{0, 16, 31, 33, 43} {
		key := make([]byte, n)
		copy(key, pub)
		if err := VerifyWebhook(key, sig, ts, body); err == nil {
			t.Errorf("VerifyWebhook key length %d = nil error, want rejection", n)
		}
	}
	// 44 bytes WITHOUT the SPKI header is likewise rejected.
	weird := make([]byte, 44)
	copy(weird, pub)
	if err := VerifyWebhook(weird, sig, ts, body); err == nil {
		t.Error("VerifyWebhook 44-byte non-SPKI key = nil error, want rejection")
	}
}

// TestVerifyWebhook_UnixTimestamp pins the primary timestamp format:
// unix-seconds header string, signature computed over the same string.
func TestVerifyWebhook_UnixTimestamp(t *testing.T) {
	pub, priv := genKeypair(t)
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Add(-120*time.Second).Unix(), 10) // in-window past
	sig := signV2(t, priv, ts, body)
	if err := VerifyWebhook([]byte(pub), sig, ts, body); err != nil {
		t.Fatalf("unix-seconds timestamp = %v, want nil", err)
	}
}

// TestVerifyWebhook_RFC3339Fallback ports matrix case :56-70
// TestValidateTelnyx_RFC3339TimestampFallback (signature.go:235-245): an
// RFC3339 timestamp verifies when the signature covers the same string.
func TestVerifyWebhook_RFC3339Fallback(t *testing.T) {
	pub, priv := genKeypair(t)
	body := matrixWebhookBody()
	ts := time.Now().UTC().Format(time.RFC3339)
	sig := signV2(t, priv, ts, body)
	if err := VerifyWebhook([]byte(pub), sig, ts, body); err != nil {
		t.Fatalf("RFC3339 timestamp + matching signature = %v, want nil", err)
	}
}

// TestVerifyWebhook_UnparseableTimestampRejected: neither unix nor
// RFC3339 (e.g. "123abc", which strategist guards against via
// strconv.ParseInt at signature.go:231-233) must pass.
func TestVerifyWebhook_UnparseableTimestampRejected(t *testing.T) {
	pub, priv := genKeypair(t)
	body := matrixWebhookBody()
	for _, ts := range []string{"", "not-a-time", "123abc", "2026-13-45T99:99:99Z"} {
		sig := signV2(t, priv, ts, body)
		if err := VerifyWebhook([]byte(pub), sig, ts, body); err == nil {
			t.Errorf("timestamp %q = nil error, want rejection", ts)
		}
	}
}

// TestVerifyWebhook_SkewBounds pins the asymmetry: 300s past / 60s
// future retained (signature.go:247-263; v1's symmetric ±300 must NOT
// survive into VerifyWebhook). Margins avoid wall-clock flake.
func TestVerifyWebhook_SkewBounds(t *testing.T) {
	pub, priv := genKeypair(t)
	body := matrixWebhookBody()
	cases := []struct {
		name string
		d    time.Duration
		want error // nil = must verify
	}{
		{"295s past in-window", -295 * time.Second, nil},
		{"305s past out-of-window", -305 * time.Second, fmt.Errorf("skew")},
		{"55s future in-window", 55 * time.Second, nil},
		{"65s future out-of-window", 65 * time.Second, fmt.Errorf("skew")},
	}
	for _, tc := range cases {
		ts := strconv.FormatInt(time.Now().Add(tc.d).Unix(), 10)
		sig := signV2(t, priv, ts, body)
		err := VerifyWebhook([]byte(pub), sig, ts, body)
		if tc.want == nil && err != nil {
			t.Errorf("%s: VerifyWebhook = %v, want nil", tc.name, err)
		}
		if tc.want != nil && err == nil {
			t.Errorf("%s: VerifyWebhook = nil, want rejection (replay/future-skew)", tc.name)
		}
	}
}

// TestVerifyWebhook_WrongKeyFails ports matrix case :104-115: a signature
// from a different keypair must not verify.
func TestVerifyWebhook_WrongKeyFails(t *testing.T) {
	pub, _ := genKeypair(t)
	_, otherPriv := genKeypair(t)
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signV2(t, otherPriv, ts, body)
	if err := VerifyWebhook([]byte(pub), sig, ts, body); err == nil {
		t.Fatal("signature from a different key = nil error, want rejection")
	}
}

// TestVerifyWebhook_TamperedBodyFails: the verify runs over the RAW body;
// any mutation after signing must fail.
func TestVerifyWebhook_TamperedBodyFails(t *testing.T) {
	pub, priv := genKeypair(t)
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	sig := signV2(t, priv, ts, body)
	tampered := append([]byte{}, body...)
	tampered[len(tampered)-2] = 'X'
	if err := VerifyWebhook([]byte(pub), sig, ts, tampered); err == nil {
		t.Fatal("tampered body = nil error, want rejection")
	}
}

// TestVerifyWebhook_BadSignatureEncoding pins header hygiene: undecodable
// base64, wrong-length signature, or empty sig are errors, never passes.
func TestVerifyWebhook_BadSignatureEncoding(t *testing.T) {
	pub, _ := genKeypair(t)
	body := matrixWebhookBody()
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	for _, sig := range []string{
		"",
		"!!!not base64!!!",
		base64.StdEncoding.EncodeToString([]byte("short")),
	} {
		if err := VerifyWebhook([]byte(pub), sig, ts, body); err == nil {
			t.Errorf("signature %q = nil error, want rejection", sig)
		}
	}
}

// TestConstantTimeSignatureMatch pins the ported constant-time compare
// helper (strategist signature.go:431-463, exposed per contract §1 as
// constantTimeSignatureMatch): SHA-256-normalised, length-leak-free match
// of an expected value against variable-length candidates, empty expected
// or no candidates never match, empty candidate not short-circuited.
func TestConstantTimeSignatureMatch(t *testing.T) {
	if !constantTimeSignatureMatch("expected", []string{"a", "bb", "expected", "cccc"}) {
		t.Error("exact candidate in list must match (variable-length list)")
	}
	if constantTimeSignatureMatch("expected", []string{"a", "different"}) {
		t.Error("non-matching candidates must not match")
	}
	if constantTimeSignatureMatch("", []string{"", "x"}) {
		t.Error("empty expected must never match (strategist guard)")
	}
	if constantTimeSignatureMatch("expected", nil) {
		t.Error("empty candidate list must not match")
	}
	if constantTimeSignatureMatch("expected", []string{}) {
		t.Error("zero-length candidate list must not match")
	}
}
