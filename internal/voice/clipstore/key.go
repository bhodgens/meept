// Package clipstore is the content-addressed cache of pre-rendered speech
// clips (filler/greeting/silence/farewell PCM) under
// $MEEPT_HOME/voice_clips/, per Contracts C1 + C2 of plan tree
// docs/plans/20261113-filler-clips-and-context-warm/master.md.
//
// Contract C1 (clip key formula, pinned — both branches' consumers copy
// verbatim):
//
//	clipKey = hex(sha256(
//	    "v1" \x1f text \x1f tts_vendor \x1f tts_model \x1f tts_voice \x1f hex(sha256(reference_audio_bytes))))
//
// reference_audio_bytes is the clone reference the configured TTS voice
// uses; for non-clone voices pass a nil/empty refHash (the inner hash of
// zero bytes). Contract C2 (store layout):
//
//	dir:  config.MeeptPath("voice_clips")   // honors MEEPT_HOME
//	pcm:  <clipKey>.pcm                     raw s16le PCM at the vendor's native output rate
//	meta: <clipKey>.json  {key, text, vendor, model, voice, sample_rate, bytes, created_at}
//
// No index file; the store is content-addressed. Presence of a valid
// <key>.pcm with a matching meta bytes field is the ONLY cache-hit
// condition. Corrupt/short file = miss (delete, regenerate).
package clipstore

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// keySep is the unit-separator (0x1f) field delimiter pinned by Contract
// C1. It is chosen so no printable text a TTS lane can speak (or vendor /
// model / voice selector) can collide with a field boundary.
const keySep = "\x1f"

// keyPrefix is the version marker pinned by Contract C1. A formula change
// bumps this and invalidates every stored clip.
const keyPrefix = "v1"

// ClipKey implements Contract C1 EXACTLY:
//
//	hex(sha256("v1" + \x1f + text + \x1f + vendor +
//	           \x1f + model + \x1f + voice + \x1f + hex(sha256(refHash))))
//
// refHash is the sha256 of the clone reference audio the configured TTS
// voice uses (NOT the raw reference bytes — callers hash once, this
// function re-hashes to hex per the formula). A nil/empty refHash is the
// non-clone voice case and contributes the hash-of-zero-bytes inner hex,
// exactly like hashing empty reference audio. Any component change flips
// the key; the same inputs always yield the same key.
func ClipKey(text, vendor, model, voice string, refHash []byte) string {
	inner := sha256.Sum256(refHash)
	fields := strings.Join([]string{
		keyPrefix, text, vendor, model, voice, hex.EncodeToString(inner[:]),
	}, keySep)
	key := sha256.Sum256([]byte(fields))
	return hex.EncodeToString(key[:])
}
