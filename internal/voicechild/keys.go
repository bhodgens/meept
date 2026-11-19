package voicechild

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// KeysFrame carries provider keys AND vendor/model/base_url/voice
// selectors for one call, daemon->child (C1 keys shape):
//
//	{"call_id":"<ccid>","event":"keys","stt_key":"...","tts_key":"...",
//	 "stt_vendor":"...","stt_model":"...","stt_base_url":"...",
//	 "tts_vendor":"...","tts_model":"...","tts_base_url":"...","tts_voice":"..."}
//
// Key material lives ONLY in memory: never logged (String redacts), never
// written to disk, never passed through argv/env.
//
// Redaction scope note: the vendor/model/voice fields are NOT redacted in
// String/LogValue because none of them can carry a secret - vendor is an
// adapter name, model a public model id, voice a voice name. The
// base_url fields ARE scrubbed (audit fix 7): an OpenAI-compatible base
// URL is user-supplied config and its userinfo component (the authority
// between "://" and the first "/") can embed credential material; the
// credential half must never reach a log
// line. The stored ProviderConfig.BaseURL keeps the FULL URL — the
// adapter needs it to dial — only the display path scrubs.
type KeysFrame struct {
	CallID     string
	STTKey     string
	TTSKey     string
	STTVendor  string
	STTModel   string
	STTBaseURL string
	TTSVendor  string
	TTSModel   string
	TTSBaseURL string
	TTSVoice   string
	// TurnConfirmMs is the voice-startguard C1 aggregation window in
	// milliseconds (see pipeline.turnConfirm): the daemon sends config
	// voice.turn_confirm_ms; an ABSENT selector on an old daemon's line
	// decodes as defaultTurnConfirmMs (900) at handleKeysFrame, and an
	// explicit 0 selects legacy per-final dispatch.
	TurnConfirmMs int
	// MinTurnWords is the voice-microfrag short-final gate (see
	// pipeline.minTurnWords): the daemon sends config
	// voice.min_turn_words; an ABSENT selector on an old daemon's line
	// decodes as 0 (gate disabled, legacy behavior) and an explicit 0
	// is the same. Positive N drops FINALs whose stripped text has
	// fewer than N words. A negative value clamps to 0 at decode.
	MinTurnWords int
	// ---- voice-peragent leaf 01: the short-final gate improvements ----
	// (pipeline.shortGate; ALL inert while MinTurnWords is 0, so an
	// old-daemon/keys-less line reproduces the plain word-count gate
	// exactly.)
	//
	// ExemptPhrases: a stripped final whose whole residue matches one
	// entry (case-insensitive, outer punctuation trimmed) is never
	// dropped. Absent/nil or [] on the wire = NO exemptions (the
	// pre-leaf gate shape, byte-identical); the daemon always sends
	// the effective resolved set (DefaultConfig's shipped phrases or
	// the row's), so a real deployment's gate exempts the one-word
	// command set.
	ExemptPhrases []string
	// EnergyGate + MinRMS: with the gate on, a short final drops ONLY
	// when the utterance window's average mic RMS is below MinRMS.
	// Absent = false/0 = plain word-count gate.
	EnergyGate bool
	MinRMS     int
	// FragmentDecayS: after K consecutive drops, raise the effective N
	// by min(K, 3) while a drop happened within this many seconds.
	// Absent/0 = off.
	FragmentDecayS int
	// ReplyExemptS: a short final within this many seconds of the
	// child's own last spoken-audio activity is exempt (a reply, not
	// background). Absent on an old daemon line decodes to the child
	// default (15 s, defaultReplyExemptS); an explicit 0 disables.
	ReplyExemptS int
	// TTSRefHash is the leaf-03 (filler-clips) clip-identity input:
	// hex(sha256(clone reference audio bytes)), or "" for a preset
	// (non-clone) voice — the per-employee clone reference never crosses
	// C2 (the keys lane is its only wire, same posture as the raw keys),
	// so the hash rides instead. Absent on an old daemon's line = "" =
	// the non-clone empty-bytes case of Contract C1. It is NOT secret
	// material (a hash of public-ish audio); it still never logs (the
	// redaction posture covers the whole frame).
	TTSRefHash string
	// ---- voice TTS/STT optimization selectors (20261118, contract C1) ----
	// All seven are ADDITIVE and OPTIONAL on the wire (omitempty): zero/
	// empty = legacy byte-identical behavior. Each later branch consumes
	// one of these as its single seam.
	SmartTurnMode int `json:"smart_turn_mode,omitempty"` // 0 = current behavior; 1 = kill switch; 2 = consume verdict
	// WhisperServerAddr, when non-empty, selects a persistent whisper.cpp
	// server instead of the per-utterance exec path. Empty = legacy exec.
	WhisperServerAddr string `json:"whisper_server_addr,omitempty"`
	// DeepgramEndpointMs > 0 enables the authoritative speech_final
	// endpoint fast path; 0 keeps the legacy fixed-timer endpointing.
	DeepgramEndpointMs int `json:"deepgram_endpoint_ms,omitempty"`
	// Vocabulary primes critical entities on the STT request. Empty =
	// byte-identical legacy wire.
	Vocabulary []string `json:"vocabulary,omitempty"`
	// Qwen3ChunkSize overrides the qwen3 pcm-stream chunk_size; 0 => 4.
	Qwen3ChunkSize int `json:"qwen3_chunk_size,omitempty"`
	// ClipPrewarmDelayMs defers a queued clip render by this many ms after
	// the played edge; 0 = render immediately (legacy).
	ClipPrewarmDelayMs int `json:"clip_prewarm_delay_ms,omitempty"`
	// ContextWarmWindowMs overrides the partial-warm debounce window; 0 =
	// resolve from turn_confirm_ms (legacy).
	ContextWarmWindowMs int `json:"context_warm_window_ms,omitempty"`
}

// Redacted is what String prints instead of key material.
const Redacted = "[REDACTED]"

// String implements fmt.Stringer, REDACTING key material so the struct is
// safe to pass to any logger. The vendor/model/voice selectors print in
// the clear; base URLs print with userinfo scrubbed (see scrubURL).
func (k KeysFrame) String() string {
	return fmt.Sprintf("keys(call_id: %s, stt_key: %s, tts_key: %s, stt_vendor: %s, stt_model: %s, stt_base_url: %s, tts_vendor: %s, tts_model: %s, tts_base_url: %s, tts_voice: %s)",
		k.CallID, redact(k.STTKey), redact(k.TTSKey),
		k.STTVendor, k.STTModel, scrubURL(k.STTBaseURL),
		k.TTSVendor, k.TTSModel, scrubURL(k.TTSBaseURL), k.TTSVoice)
}

// LogValue routes the frame through a redaction-safe representation, so
// structured logging never captures key material either.
func (k KeysFrame) LogValue() fmt.Stringer { return redactedKeys{k} }

type redactedKeys struct{ k KeysFrame }

func (r redactedKeys) String() string {
	return fmt.Sprintf("call_id=%s stt_key=%s tts_key=%s stt_vendor=%s stt_model=%s stt_base_url=%s tts_vendor=%s tts_model=%s tts_base_url=%s tts_voice=%s",
		r.k.CallID, redact(r.k.STTKey), redact(r.k.TTSKey),
		r.k.STTVendor, r.k.STTModel, scrubURL(r.k.STTBaseURL),
		r.k.TTSVendor, r.k.TTSModel, scrubURL(r.k.TTSBaseURL), r.k.TTSVoice)
}

// redact reports the placeholder for non-empty secrets and "" untouched
// (an unset key is not material).
func redact(s string) string {
	if s == "" {
		return ""
	}
	return Redacted
}

// scrubURL removes userinfo credential material from a URL for display
// (audit fix 7): OpenAI-compatible base URLs are free-form config and a
// scheme://user:secret@host form embeds a secret in the endpoint
// string. Everything between "://" and the '@' that ends the authority is
// replaced with the redaction marker; the host/path stay in the clear
// (misconfiguration must stay debuggable). A URL with no '@' before the
// path is returned unchanged.
func scrubURL(u string) string {
	i := strings.Index(u, "://")
	if i < 0 {
		return u
	}
	authority := u[i+3:]
	// The authority ends at the first '/', '?' or '#'.
	end := len(authority)
	for j, r := range authority {
		if r == '/' || r == '?' || r == '#' {
			end = j
			break
		}
	}
	at := strings.LastIndex(authority[:end], "@")
	if at < 0 {
		return u
	}
	// Keep everything from '@' on (host:port/path): only the userinfo
	// half before the last '@' is credential material.
	return u[:i+3] + Redacted + authority[at:]
}

// openVocabularyPreserve is the pipeline's default C6 preserve hook (leaf
// 03): the child side has no employee-context catalog, so every token is
// presumed meaningful EXCEPT the known STT filler set — normalization
// collapses duplicates and filler runs and drops only bare fillers.
// Callers with a real catalog install a narrower hook via
// SetNormalizePreserve.
func openVocabularyPreserve(token string) bool {
	return !fillerTokens[token]
}

// SetNormalizePreserve installs the C6 preserve hook on one call's
// pipeline (leaf 03): tokens for which the hook returns true survive
// NormalizeUtterance's noise-token drop. Set-once semantics: the hook is
// config-shaped (employee context names), so later calls overwrite; the
// hook is read by the transcript consumer, which starts after setup.
func (p *pipeline) SetNormalizePreserve(preserve func(token string) bool) {
	p.normalizePreserve = preserve
}

// applyKeys stores provider key material and the vendor/model/base_url/
// voice selectors for the call in memory only (playMu-guarded, like the
// keys themselves). The setup frame's negotiated sample rate is stamped
// onto the stored configs by constructProviders, not here (a keys frame
// may legitimately arrive after setup).
func (p *pipeline) applyKeys(k KeysFrame) {
	p.playMu.Lock()
	p.sttKey = k.STTKey
	p.ttsKey = k.TTSKey
	// Clip-identity input (leaf 03, adjudication A4): the hex(sha256) of
	// the clone reference audio from the keys frame, decoded for the
	// Contract C1 ClipKey path. "" (old daemon) = non-clone = nil.
	p.ttsRefHash = decodeTTSRefHash(k.TTSRefHash)
	// Turn-aggregation window (C1): the keys frame carries the daemon's
	// resolved voice.turn_confirm_ms; absent-on-wire already decoded to
	// defaultTurnConfirmMs at handleKeysFrame. Applied before the
	// transcript session can produce any final (constructProviders +
	// startTranscripts run after applyKeys in handleKeys), and the
	// pipeline constructor seeds the same default so a keys-less call
	// still aggregates.
	p.turnConfirm = time.Duration(k.TurnConfirmMs) * time.Millisecond
	// Deepgram authoritative endpoint (F4): the opt-in knob that enables
	// fast-dispatch of the accumulated finals when Endpoint arrives. 0
	// (absent on an old daemon's wire) keeps the fixed-timer path.
	p.deepgramEndpointMs = k.DeepgramEndpointMs
	// Smart Turn mode (F1, 02-smart-turn.md): consumed by the async turn
	// factory gate and by finalConfirmFired. Stored under playMu; the
	// turn factory goroutine reads it only AFTER keysReady closes (which
	// happens after this assignment), so it always sees the real mode.
	p.smartTurnMode = k.SmartTurnMode
	p.smartTurnDeferred = false
	// Short-final gate (voice-microfrag): absent-on-wire already decoded
	// to 0 (disabled) at handleKeysFrame. Same playMu guard as
	// turnConfirm; applied before any transcript can arrive.
	p.minTurnWords = k.MinTurnWords
	// Gate improvements (voice-peragent leaf 01): stored as one
	// struct so the gate reads a consistent snapshot under playMu.
	// Absent-on-wire decoded to the today-shape defaults at
	// handleKeysFrame (nil exemptions -> the shipped default set at
	// the gate, energy off, decay off, reply window 15); every
	// improvement stays inert while minTurnWords is 0.
	p.exemptPhrases = k.ExemptPhrases
	p.energyGate = k.EnergyGate
	p.minRMS = k.MinRMS
	p.fragmentDecayS = k.FragmentDecayS
	p.replyExemptS = k.ReplyExemptS
	p.sttCfg = ProviderConfig{
		Vendor:             k.STTVendor,
		Model:              k.STTModel,
		BaseURL:            k.STTBaseURL,
		WhisperServerAddr:  k.WhisperServerAddr,
		DeepgramEndpointMs: k.DeepgramEndpointMs,
	}
	p.ttsCfg = ProviderConfig{
		Vendor:         k.TTSVendor,
		Model:          k.TTSModel,
		BaseURL:        k.TTSBaseURL,
		Voice:          k.TTSVoice,
		Qwen3ChunkSize: k.Qwen3ChunkSize,
	}
	p.playMu.Unlock()
	// Clip renders gate on provider identity, which only exists once the
	// keys frame has landed (A3): release any queued render worker exactly
	// once (a re-sent keys frame is idempotent through the Once). Guarded
	// for nil: hand-built unit-test pipelines may skip newPipeline. Moved
	// OUT of the playMu section: mutexio forbids Do-under-lock (the closure
	// must be allowed to grow I/O later without re-tripping the analyzer).
	if p.keysReady != nil {
		p.keysReadyOnce.Do(func() { close(p.keysReady) })
	}
}

// emitPrompt sends a C1 prompt frame to the daemon: final transcripts
// (last:true) start an employee turn; partials update the preview only.
// raw (leaf 03 T3): for finals, the RAW (un-normalized) transcript rides
// the From field — the daemon's binding persists the caller's actual
// words via AppendTranscript while the LLM turn runs on Text (already
// normalized by the caller). Empty raw (partials, or a normalize pass)
// omits the wire field (omitempty) so partial frames stay byte-identical.
func (c *Child) emitPrompt(ctx context.Context, ccid, text string, final bool, raw string) error {
	f := voiceframe.Frame{
		CallID: ccid,
		Event:  voiceframe.EventPrompt,
		Text:   text,
		From:   raw,
	}
	if final {
		last := true
		f.Last = &last
	}
	return c.sendFrame(ctx, f)
}

// emitInterrupt sends the barge-in interrupt frame for ccid.
func (c *Child) emitInterrupt(ctx context.Context, ccid string) error {
	return c.sendFrame(ctx, voiceframe.Frame{
		CallID: ccid,
		Event:  voiceframe.EventInterrupt,
	})
}

// emitPlayed sends the end-of-turn audio marker (C1 played edge): the
// pipeline just wrote the FINAL chunk of a last:true say frame's stream
// to the .out socket, so no more PCM can arrive for this turn. The
// daemon arms the splice's drain watch from this edge (exact wire shape
// {"call_id":"...","event":"played","last":true}).
func (c *Child) emitPlayed(ctx context.Context, ccid string) error {
	last := true
	return c.sendFrame(ctx, voiceframe.Frame{
		CallID: ccid,
		Event:  voiceframe.EventPlayed,
		Last:   &last,
	})
}

// sendFrame writes one frame line to the text socket.
func (c *Child) sendFrame(ctx context.Context, f voiceframe.Frame) error {
	b, err := voiceframe.EncodeFrame(f)
	if err != nil {
		return fmt.Errorf("voicechild: encode %s frame: %w", f.Event, err)
	}
	return c.writeLine(ctx, b)
}
