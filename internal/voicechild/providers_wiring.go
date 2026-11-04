package voicechild

import (
	"context"
	"encoding/json"
	"fmt"
)

// STTFactory builds the STT adapter for one call from its key (C1 keys
// frame) and the setup frame's negotiated sample rate. cmd/meept-voice
// injects the leaf-08 adapter constructors (vendor/model closed over, so
// this package stays free of the providers import - providers imports
// voicechild for the interfaces); tests inject fakes. A non-nil error
// fails the call with a named error; the child and other calls stay up.
type STTFactory func(key string, sampleRate int) (STTProvider, error)

// TTSFactory mirrors STTFactory for the TTS side.
type TTSFactory func(key string, sampleRate int) (TTSProvider, error)

// constructProviders builds the pipeline's provider pair from the stored
// keys using the child's factories. Missing key material surfaces as the
// factory's named error (providers.ErrMissingSTTKey et al wrapped);
// nothing here logs key content.
func (c *Child) constructProviders(p *pipeline) error {
	c.mu.Lock()
	sttFactory, ttsFactory := c.sttFactory, c.ttsFactory
	c.mu.Unlock()

	p.playMu.Lock()
	sttKey, ttsKey := p.sttKey, p.ttsKey
	p.playMu.Unlock()

	if sttFactory != nil {
		stt, err := sttFactory(sttKey, p.sampleRt)
		if err != nil {
			return fmt.Errorf("voicechild: stt provider for call %s: %w", p.ccid, err)
		}
		p.playMu.Lock()
		p.stt = stt
		p.playMu.Unlock()
	}
	if ttsFactory != nil {
		tts, err := ttsFactory(ttsKey, p.sampleRt)
		if err != nil {
			return fmt.Errorf("voicechild: tts provider for call %s: %w", p.ccid, err)
		}
		p.playMu.Lock()
		p.tts = tts
		p.playMu.Unlock()
	}
	return nil
}

// keysWire is the daemon->child keys frame exactly as leaf 07 specified
// it: {"call_id":"<ccid>","event":"keys","stt_key":"...","tts_key":"..."}.
// The shared voiceframe.Frame deliberately carries no key fields (the
// daemon side never handles key material), so the child decodes the raw
// line here; KeysFrame's String/LogValue redaction (leaf 07) governs
// everything this value touches afterwards.
type keysWire struct {
	CallID string `json:"call_id"`
	Event  string `json:"event"`
	STTKey string `json:"stt_key"`
	TTSKey string `json:"tts_key"`
}

// handleKeysFrame decodes one raw keys line into the redacting KeysFrame
// and applies it.
func (c *Child) handleKeysFrame(line string) error {
	var w keysWire
	if err := json.Unmarshal([]byte(line), &w); err != nil {
		return fmt.Errorf("voicechild: decode keys frame: %w", err)
	}
	return c.handleKeys(KeysFrame{CallID: w.CallID, STTKey: w.STTKey, TTSKey: w.TTSKey})
}

// handleKeys routes a C1 keys frame: keys are stored on the pipeline
// (memory only, redaction intact per leaf 07) and the providers are
// constructed from them. An unknown call is logged and dropped, not an
// error; a construction failure (missing key) fails the call by tearing
// down only that pipeline.
func (c *Child) handleKeys(k KeysFrame) error {
	p, ok := c.getPipeline(k.CallID)
	if !ok {
		c.logger.Warn("voicechild: keys for unknown call", "call_id", k.CallID)
		return nil
	}
	p.applyKeys(k)
	if err := c.constructProviders(p); err != nil {
		c.logger.Error("voicechild: provider construction failed", "err", err, "call_id", k.CallID)
		// Fail the call, not the child: tear down only this pipeline.
		c.dropPipeline(k.CallID)
		p.close(context.Background())
		return nil
	}
	c.logger.Info("voicechild: providers constructed", "call_id", k.CallID)
	return nil
}
