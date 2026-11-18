package clipstore

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func digestHex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// TestClipKeyPinnedVectors pins Contract C1 byte-exactly. The expected
// hex values were computed independently (shell sha256 over the
// \x1f-joined pre-image), so a formula drift in either direction fails.
func TestClipKeyPinnedVectors(t *testing.T) {
	ref := []byte("REF")
	got := ClipKey("Just a sec.", "qwen3", "qwen3-tts-flash", "MyVoice-1", ref)
	want := "fd2c3570dfae9ed83b3c70c4bcb0b797f03761565af5d0751f2048d7db70b870"
	if got != want {
		t.Errorf("ClipKey pinned vector:\n got %s\nwant %s", got, want)
	}

	// Non-clone voice: empty refHash contributes hex(sha256("")) inner.
	gotEmpty := ClipKey("Hello!", "openai", "tts-1", "alloy", nil)
	wantEmpty := "746c1897b586d9fcbaaaa6372a74094b310cdec8edb6710b9fe2a68b02d18095"
	if gotEmpty != wantEmpty {
		t.Errorf("ClipKey empty-ref vector:\n got %s\nwant %s", gotEmpty, wantEmpty)
	}

	// nil and empty refHash must be the same key (hash of zero bytes).
	if ClipKey("Hello!", "openai", "tts-1", "alloy", []byte{}) != gotEmpty {
		t.Error("nil vs empty refHash must not flip the key")
	}
}

// TestClipKeyPreImageStructure re-derives the key from the C1 pre-image
// field-by-field to pin the concatenation ORDER (v1, text, vendor, model,
// voice, inner-hex) — the review checklist greps this order.
func TestClipKeyPreImageStructure(t *testing.T) {
	text, vendor, model, voice := "Bye!", "qwen3", "m2", "v2"
	refHash := []byte("reference-audio-bytes")

	inner := refHashHex(refHash)
	pre := strings.Join([]string{"v1", text, vendor, model, voice, inner}, "\x1f")
	want := ClipKey(text, vendor, model, voice, refHash)

	got := digestHex([]byte(pre))
	if got != want {
		t.Errorf("pre-image re-derivation mismatch:\n got %s\nwant %s", got, want)
	}
	if len(want) != keyHexLen || strings.ToLower(want) != want {
		t.Errorf("key must be %d lowercase hex chars, got %q", keyHexLen, want)
	}
}

func TestClipKeyStableAcrossCalls(t *testing.T) {
	a := ClipKey("x", "y", "z", "w", []byte("r"))
	for range 100 {
		if ClipKey("x", "y", "z", "w", []byte("r")) != a {
			t.Fatal("ClipKey is not deterministic")
		}
	}
}

// TestClipKeyComponentFlip: changing ANY single component flips the key.
func TestClipKeyComponentFlip(t *testing.T) {
	base := ClipKey("text", "vendor", "model", "voice", []byte("ref"))
	flips := []struct {
		name                       string
		text, vendor, model, voice string
		ref                        []byte
	}{
		{"text", "text2", "vendor", "model", "voice", []byte("ref")},
		{"vendor", "text", "vendor2", "model", "voice", []byte("ref")},
		{"model", "text", "vendor", "model2", "voice", []byte("ref")},
		{"voice", "text", "vendor", "model", "voice2", []byte("ref")},
		{"refHash", "text", "vendor", "model", "voice", []byte("ref2")},
		{"ref-nil-vs-set", "text", "vendor", "model", "voice", nil},
	}
	for _, f := range flips {
		if got := ClipKey(f.text, f.vendor, f.model, f.voice, f.ref); got == base {
			t.Errorf("changing %s did not flip the key", f.name)
		}
	}
}

// TestClipKeyFieldBoundary: the C1 pre-image is a raw \x1f join, so
// injectivity is guaranteed for fields WITHOUT \x1f (every real spoken
// line, vendor, model, voice selector — \x1f is a control char no TTS
// text carries). This pins that distinct well-formed inputs yield
// distinct keys, and documents that the join shape is the frozen
// contract, not an encoding of ours to "improve".
func TestClipKeyFieldBoundary(t *testing.T) {
	a := ClipKey("just a sec", "vendor", "model", "voice", nil)
	b := ClipKey("hello there", "vendor", "model", "voice", nil)
	c := ClipKey("just a sec", "other-vendor", "model", "voice", nil)
	if a == b || a == c || b == c {
		t.Error("distinct well-formed inputs aliased")
	}
	// Reconstruct the exact C1 pre-image shape byte-for-byte.
	preImage := "v1" + keySep + "just a sec" + keySep + "vendor" + keySep +
		"model" + keySep + "voice" + keySep + refHashHex(nil)
	if got := digestHex([]byte(preImage)); got != a {
		t.Errorf("pre-image shape drift:\n got %s\nwant %s", got, a)
	}
}
