package voicechild

import (
	"context"
	"fmt"

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
// Redaction scope note: the vendor/model/base_url/voice fields are NOT
// redacted in String/LogValue because none of them can carry a secret -
// vendor is an adapter name, model a public model id, voice a voice name,
// and base_url is an endpoint (loopback in the local qwen3 case); a URL
// never carries credential material (auth rides the key fields or, for
// keyless qwen3, no header at all). Redacting them would make provider
// misconfiguration undebuggable for no secrecy gain.
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
}

// Redacted is what String prints instead of key material.
const Redacted = "[REDACTED]"

// String implements fmt.Stringer, REDACTING key material so the struct is
// safe to pass to any logger. The vendor/model/base_url/voice selectors
// print in the clear (see the type comment for why they are not secrets).
func (k KeysFrame) String() string {
	return fmt.Sprintf("keys(call_id: %s, stt_key: %s, tts_key: %s, stt_vendor: %s, stt_model: %s, stt_base_url: %s, tts_vendor: %s, tts_model: %s, tts_base_url: %s, tts_voice: %s)",
		k.CallID, redact(k.STTKey), redact(k.TTSKey),
		k.STTVendor, k.STTModel, k.STTBaseURL,
		k.TTSVendor, k.TTSModel, k.TTSBaseURL, k.TTSVoice)
}

// LogValue routes the frame through a redaction-safe representation, so
// structured logging never captures key material either.
func (k KeysFrame) LogValue() fmt.Stringer { return redactedKeys{k} }

type redactedKeys struct{ k KeysFrame }

func (r redactedKeys) String() string {
	return fmt.Sprintf("call_id=%s stt_key=%s tts_key=%s stt_vendor=%s stt_model=%s stt_base_url=%s tts_vendor=%s tts_model=%s tts_base_url=%s tts_voice=%s",
		r.k.CallID, redact(r.k.STTKey), redact(r.k.TTSKey),
		r.k.STTVendor, r.k.STTModel, r.k.STTBaseURL,
		r.k.TTSVendor, r.k.TTSModel, r.k.TTSBaseURL, r.k.TTSVoice)
}

// redact reports the placeholder for non-empty secrets and "" untouched
// (an unset key is not material).
func redact(s string) string {
	if s == "" {
		return ""
	}
	return Redacted
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
func (c *Child) emitPrompt(ctx context.Context, ccid, text string, final bool) error {
	f := voiceframe.Frame{
		CallID: ccid,
		Event:  voiceframe.EventPrompt,
		Text:   text,
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

// sendFrame writes one frame line to the text socket.
func (c *Child) sendFrame(ctx context.Context, f voiceframe.Frame) error {
	b, err := voiceframe.EncodeFrame(f)
	if err != nil {
		return fmt.Errorf("voicechild: encode %s frame: %w", f.Event, err)
	}
	return c.writeLine(ctx, b)
}
