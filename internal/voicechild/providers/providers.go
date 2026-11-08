// Package providers holds the real STT/TTS adapters behind the leaf-07
// voicechild interfaces (leaf 08, assumption A1): Deepgram streaming STT
// and OpenAI TTS.
//
// Wrap-vs-handroll decision (T1), per vendor, against jargo v0.1.0:
//
//   - STT: HAND-ROLL the vendor session, WRAP jargo's wsutil dialer. jargo
//     ships provider/deepgram.NewSTT, but it returns a full pipeline
//     processor (*stt.StreamService) whose lifecycle is frame-driven
//     (StartFrame/VAD frames/pipeline Setup) and whose reconnect policy is
//     wsservice's DefaultMaxRetries=3. Neither fits the leaf-07 channel
//     interface or the leaf's ONE-retry-within-call policy, and leaf 07
//     deliberately kept the C2 unix-socket transport seam outside jargo's
//     rtc transport. The protocol itself (binary audio in, JSON Results
//     out over /v1/listen) is small and stable, so the adapter keeps
//     jargo's wsutil (handshake classification, bounded close) and speaks
//     the wire format directly.
//
//   - TTS: WRAP jargo's wire shape, same conclusion as STT by a shorter
//     route: jargo's OpenAI TTS (provider/openai/chat) is a tts.Base
//     synthesizer that only runs inside a pipeline context. The HTTP
//     request it makes (POST /v1/audio/speech, response_format "pcm")
//     is one call, so the adapter issues it directly and consumes the
//     response stream incrementally - first audio is yielded as it
//     arrives, never buffered whole (T3 contract).
//
// Keys arrive via the C1 keys frame at call setup and live in process
// memory only: never env, never argv, never logged (TestNoKeyLogs pins
// construction logging to vendor+model).
package providers

import (
	"errors"

	"github.com/caimlas/meept/internal/voicechild"
)

// ProviderConfig selects and shapes a vendor adapter. It mirrors the leaf
// contract: vendor name, model, per-instance API endpoint, TTS voice, the
// negotiated output sample rate, and the recognition language.
//
// The canonical struct lives in voicechild (it rides the C3 factory
// signatures there); this alias keeps the C2 name importable as
// providers.ProviderConfig without a second copy of the shape.
type ProviderConfig = voicechild.ProviderConfig

// Vendor names for NewTTS dispatch (C2: "deepgram" is STT-only).
const (
	TTSVendorOpenAI = "openai"
	TTSVendorQwen3  = "qwen3"
)

// Errors named per the leaf: a missing key fails the call with a
// constructor error; the child process and other calls stay up.
var (
	// ErrMissingSTTKey is returned by NewSTT when the keys frame carried
	// no STT key.
	ErrMissingSTTKey = errors.New("voicechild/providers: missing STT key for call")
	// ErrMissingTTSKey is returned by NewTTS when the keys frame carried
	// no TTS key (cloud vendors only - qwen3 is keyless, D2).
	ErrMissingTTSKey = errors.New("voicechild/providers: missing TTS key for call")
	// ErrMissingTTSBaseURL is returned by NewTTS for vendor qwen3 when the
	// keys frame carried no base URL: a local OpenAI-compatible server has
	// no meaningful default endpoint to fall back to (leaf 04 contract).
	ErrMissingTTSBaseURL = errors.New("voicechild/providers: missing TTS base URL for call")
	// ErrUnknownVendor is returned when ProviderConfig.Vendor names no
	// adapter this package builds.
	ErrUnknownVendor = errors.New("voicechild/providers: unknown vendor")
)

// Compile-time pins: the adapters satisfy the leaf-07 interfaces exactly.
var (
	_ voicechild.STTProvider = (*DeepgramSTT)(nil)
	_ voicechild.TTSProvider = (*OpenAITTS)(nil)
)

// sttVendorDefaultModel and ttsVendorDefaultModel are the A1 defaults when
// ProviderConfig.Model is empty.
const (
	sttVendorDefaultModel = "nova-3"
	ttsVendorDefaultModel = "gpt-4o-mini-tts"
)

// ttsDefaultVoice is the TTS voice when ProviderConfig.Voice is empty
// (vendor default for the OpenAI-compatible request shape).
const ttsDefaultVoice = "alloy"
