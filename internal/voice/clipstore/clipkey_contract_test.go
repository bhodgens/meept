package clipstore

// Contract C1 (FROZEN, quoted verbatim from
// docs/plans/20261113-filler-clips-and-context-warm/master.md):
//
//	clipKey = hex(sha256(
//	    "v1" \x1f text \x1f tts_vendor \x1f tts_model \x1f tts_voice \x1f hex(sha256(reference_audio_bytes))))
//
//	"reference_audio_bytes is the clone reference the configured TTS voice
//	uses; for non-clone voices use the empty string's pre-image (hash of
//	zero bytes). Owner: leaf p1-01 exposes
//	func ClipKey(text, vendor, model, voice string, refHash []byte) string"
//
// This is the 00T gate pin: the expected values below are computed by a
// STANDALONE mirror in this file (and cross-checked against hex literals
// produced OUTSIDE Go entirely — see c1GatePinnedVectors), deliberately
// independent of the implementation helper. The byte-for-byte ClipKey
// agreement between the daemon planner (leaf 02) and the child renderer
// (leaf 03) is pinned HERE, not in either impl leaf.

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// c1Mirror recomputes the C1 formula from scratch (no clipstore code):
// any implementation drift from this exact construction — separator,
// field order, "v1" prefix, or the inner hash-of-refHash nesting — fails.
func c1Mirror(text, vendor, model, voice string, refHash []byte) string {
	if refHash == nil {
		refHash = []byte{} // non-clone: hash of zero bytes per C1
	}
	inner := sha256.Sum256(refHash)
	refHex := hex.EncodeToString(inner[:])
	preimage := "v1" + "\x1f" + text + "\x1f" + vendor + "\x1f" + model + "\x1f" + voice + "\x1f" + refHex
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])
}

// c1GatePinnedVectors are C1 outputs computed independently of BOTH the
// implementation and this file's mirror (Python hashlib, plan-session
// 2026-11-13). They catch a drift in the mirror itself.
var c1GatePinnedVectors = map[string]string{
	// ClipKey("Just a sec.", "qwen3",
	//   "Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice", "vivian", nil)
	"filler-nonclone": "668ae471895b27afe336829ba50e1637f1db59c508ee813234663373f6f6ff77",
	// ClipKey("", "openai", "tts-1", "alloy", nil)
	"empty-text-nonclone": "c08013dfec3d50e6d1b08a5285b6ea83fe45d2c26b9c4942764b3e032f8c1b5a",
	// ClipKey("Hello there!", "qwen3", "qwen3-tts-base",
	//   "my-cloned-voice", sha256-bytes-of-[0,1,2,255,254,'r','e','f'])
	"greeting-clone": "1084597cc66b7568946b29622b52b3c013b7f26996766e79b70f074eb655f6ea",
	// ClipKey("Hallo, bis bald! – tschüss", "qwen3", "model-x",
	//   "voice-y", nil)  — UTF-8 text must hash over its raw bytes
	"farewell-utf8": "4d80946cbff4cff28087269086c75b4ea944c2a96975a95dd87205790929ee14",
	// ClipKey("", "", "", "", nil)
	"empty-components": "dd80df14c12b8c179b0b5167b623ae248ab8764bc80f2a29ee2f131195ca4646",
}

// CK-1: ClipKey output must equal the independent C1 mirror AND the
// externally pinned hex literals for a set of frozen inputs.
func TestClipKeyGateMatchesFrozenC1Formula(t *testing.T) {
	refAudio := []byte{0x00, 0x01, 0x02, 0xff, 0xfe, 'r', 'e', 'f'}
	cases := []struct {
		name                       string
		text, vendor, model, voice string
		refHash                    []byte
	}{
		{"filler-nonclone", "Just a sec.", "qwen3", "Qwen/Qwen3-TTS-12Hz-1.7B-CustomVoice", "vivian", nil},
		{"empty-text-nonclone", "", "openai", "tts-1", "alloy", nil},
		{"greeting-clone", "Hello there!", "qwen3", "qwen3-tts-base", "my-cloned-voice", refAudio},
		{"farewell-utf8", "Hallo, bis bald! \u2013 tsch\u00fcss", "qwen3", "model-x", "voice-y", nil},
		{"empty-components", "", "", "", "", nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			mirror := c1Mirror(tc.text, tc.vendor, tc.model, tc.voice, tc.refHash)
			if pinned, ok := c1GatePinnedVectors[tc.name]; ok && pinned != mirror {
				t.Fatalf("gate self-check: mirror %q != externally pinned %q for %s", mirror, pinned, tc.name)
			}
			got := ClipKey(tc.text, tc.vendor, tc.model, tc.voice, tc.refHash)
			if got != mirror {
				t.Fatalf("ClipKey(%q, %q, %q, %q, ref=%d bytes) = %q, want %q (C1 mirror)",
					tc.text, tc.vendor, tc.model, tc.voice, len(tc.refHash), got, mirror)
			}
			if len(got) != 64 {
				t.Fatalf("ClipKey must be 64-hex (sha256), got %d chars: %q", len(got), got)
			}
		})
	}
}

// CK-2: clone-vs-preset reference hash (C1 "hash of zero bytes"). A
// non-clone voice hashes EXACTLY like sha256(nil refAudioBytes): the
// inner layer is the hash of the reference AUDIO, keyed with the hex of
// that digest — never the raw bytes.
func TestClipKeyGateNonCloneIsZeroByteReference(t *testing.T) {
	zeroRef := sha256.Sum256(nil)
	preimage := "v1" + "\x1f" + "Bye!" + "\x1f" + "qwen3" + "\x1f" + "m" + "\x1f" + "v" + "\x1f" + hex.EncodeToString(zeroRef[:])
	want := sha256.Sum256([]byte(preimage))
	got := ClipKey("Bye!", "qwen3", "m", "v", nil)
	if got != hex.EncodeToString(want[:]) {
		t.Fatalf("non-clone ClipKey = %q, want %q", got, hex.EncodeToString(want[:]))
	}
}

// CK-3: invalidation matrix — changing ANY single component flips the
// key (root R3: voice/model/text/reference-audio change => new key =>
// regenerate), and nil vs empty refHash is the SAME identity.
func TestClipKeyGateInvalidationMatrix(t *testing.T) {
	base := ClipKey("Bye!", "qwen3", "model-a", "voice-a", []byte("refA"))
	flips := map[string]string{
		"text":            ClipKey("Bye?", "qwen3", "model-a", "voice-a", []byte("refA")),
		"vendor":          ClipKey("Bye!", "openai", "model-a", "voice-a", []byte("refA")),
		"model":           ClipKey("Bye!", "qwen3", "model-b", "voice-a", []byte("refA")),
		"voice":           ClipKey("Bye!", "qwen3", "model-a", "voice-b", []byte("refA")),
		"refaudio":        ClipKey("Bye!", "qwen3", "model-a", "voice-a", []byte("refB")),
		"clone->nonclone": ClipKey("Bye!", "qwen3", "model-a", "voice-a", nil),
	}
	for name, k := range flips {
		if k == base {
			t.Fatalf("component %q changed but the key did not (both %q)", name, k)
		}
	}
	emptyRef := ClipKey("x", "v", "m", "voice", []byte{})
	nilRef := ClipKey("x", "v", "m", "voice", nil)
	if emptyRef != nilRef {
		t.Fatalf("nil vs empty refHash must be identical keys: %q vs %q", emptyRef, nilRef)
	}
}
