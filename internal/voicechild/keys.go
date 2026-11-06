package voicechild

import (
	"context"
	"fmt"

	"github.com/caimlas/meept/internal/voiceframe"
)

// KeysFrame carries provider keys for one call, daemon->child (C1 keys
// shape): {"call_id":"<ccid>","event":"keys","stt_key":"...","tts_key":"..."}.
//
// Key material lives ONLY in memory: never logged (String redacts), never
// written to disk, never passed through argv/env.
type KeysFrame struct {
	CallID string
	STTKey string
	TTSKey string
}

// Redacted is what String prints instead of key material.
const Redacted = "[REDACTED]"

// String implements fmt.Stringer, REDACTING key material so the struct is
// safe to pass to any logger.
func (k KeysFrame) String() string {
	return fmt.Sprintf("keys(call_id: %s, stt_key: %s, tts_key: %s)",
		k.CallID, redact(k.STTKey), redact(k.TTSKey))
}

// LogValue routes the frame through a redaction-safe representation, so
// structured logging never captures key material either.
func (k KeysFrame) LogValue() fmt.Stringer { return redactedKeys{k} }

type redactedKeys struct{ k KeysFrame }

func (r redactedKeys) String() string {
	return fmt.Sprintf("call_id=%s stt_key=%s tts_key=%s",
		r.k.CallID, redact(r.k.STTKey), redact(r.k.TTSKey))
}

// redact reports the placeholder for non-empty secrets and "" untouched
// (an unset key is not material).
func redact(s string) string {
	if s == "" {
		return ""
	}
	return Redacted
}

// applyKeys stores provider key material for the call in memory only.
// Skeleton behavior this leaf: the pipeline remembers the keys for leaf 08's
// provider constructors; nothing is logged or persisted.
func (p *pipeline) applyKeys(k KeysFrame) {
	p.playMu.Lock()
	p.sttKey = k.STTKey
	p.ttsKey = k.TTSKey
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
