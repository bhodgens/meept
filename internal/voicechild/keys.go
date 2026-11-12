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
	// Turn-aggregation window (C1): the keys frame carries the daemon's
	// resolved voice.turn_confirm_ms; absent-on-wire already decoded to
	// defaultTurnConfirmMs at handleKeysFrame. Applied before the
	// transcript session can produce any final (constructProviders +
	// startTranscripts run after applyKeys in handleKeys), and the
	// pipeline constructor seeds the same default so a keys-less call
	// still aggregates.
	p.turnConfirm = time.Duration(k.TurnConfirmMs) * time.Millisecond
	p.sttCfg = ProviderConfig{
		Vendor:  k.STTVendor,
		Model:   k.STTModel,
		BaseURL: k.STTBaseURL,
	}
	p.ttsCfg = ProviderConfig{
		Vendor:  k.TTSVendor,
		Model:   k.TTSModel,
		BaseURL: k.TTSBaseURL,
		Voice:   k.TTSVoice,
	}
	p.playMu.Unlock()
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
