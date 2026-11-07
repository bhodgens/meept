package voicechild

import (
	"context"
	"encoding/json"
	"fmt"
)

// ProviderConfig selects and shapes a vendor adapter for one call (C2).
// It is carried to the factories on the C1 keys frame: one keys line
// fully determines which adapters construct and where they point.
//
// This struct is canonical in voicechild (the factories live here and
// providers imports voicechild for the interfaces - an import in the
// other direction would cycle); internal/voicechild/providers re-exports
// it as a type alias, so the C2 shape is identical on both sides.
type ProviderConfig struct {
	Vendor     string // "deepgram" (STT) | "openai" | "qwen3" (TTS)
	Model      string // empty = vendor default
	BaseURL    string // empty = vendor default (openai: api.openai.com; qwen3: required)
	Voice      string // TTS voice name; empty = vendor default
	SampleRate int    // negotiated rate (s16le mono PCM)
	Language   string // BCP-47 tag; empty = vendor default
}

// STTFactory builds the STT adapter for one call from the call's provider
// config (C1 keys frame; C3 signature) and its key. cmd/meept-voice
// injects the real adapter constructors (dispatching on cfg.Vendor, so
// this package stays free of the providers import - providers imports
// voicechild for the interfaces); tests inject fakes. A non-nil error
// fails the call with a named error; the child and other calls stay up.
type STTFactory func(cfg ProviderConfig, key string) (STTProvider, error)

// TTSFactory mirrors STTFactory for the TTS side.
type TTSFactory func(cfg ProviderConfig, key string) (TTSProvider, error)

// constructProviders builds the pipeline's provider pair from the stored
// keys and config using the child's factories. Missing key material
// surfaces as the factory's named error (providers.ErrMissingSTTKey et al
// wrapped); nothing here logs key content. The negotiated sample rate
// rides the config (C3): the setup frame's rate is applied here, after
// applyKeys stored the wire fields.
func (c *Child) constructProviders(p *pipeline) error {
	c.mu.Lock()
	sttFactory, ttsFactory := c.sttFactory, c.ttsFactory
	c.mu.Unlock()

	p.playMu.Lock()
	sttKey, ttsKey := p.sttKey, p.ttsKey
	sttCfg, ttsCfg := p.sttCfg, p.ttsCfg
	p.playMu.Unlock()
	sttCfg.SampleRate = p.sampleRt
	ttsCfg.SampleRate = p.sampleRt

	if sttFactory != nil {
		stt, err := sttFactory(sttCfg, sttKey)
		if err != nil {
			return fmt.Errorf("voicechild: stt provider for call %s: %w", p.ccid, err)
		}
		p.playMu.Lock()
		p.stt = stt
		p.playMu.Unlock()
	}
	if ttsFactory != nil {
		tts, err := ttsFactory(ttsCfg, ttsKey)
		if err != nil {
			return fmt.Errorf("voicechild: tts provider for call %s: %w", p.ccid, err)
		}
		p.playMu.Lock()
		p.tts = tts
		p.playMu.Unlock()
	}
	return nil
}

// keysWire is the daemon->child keys frame exactly as the C1 contract
// specifies it: the two key fields plus the OPTIONAL vendor/model/
// base_url/voice selectors (absent = ""; old daemons that send only the
// keys keep working - C1/D4 compatibility). The shared voiceframe.Frame
// deliberately carries no key fields (the daemon side never handles key
// material), so the child decodes the raw line here; KeysFrame's
// String/LogValue redaction governs everything this value touches
// afterwards.
type keysWire struct {
	CallID     string `json:"call_id"`
	Event      string `json:"event"`
	STTKey     string `json:"stt_key"`
	TTSKey     string `json:"tts_key"`
	STTVendor  string `json:"stt_vendor"`
	STTModel   string `json:"stt_model"`
	STTBaseURL string `json:"stt_base_url"`
	TTSVendor  string `json:"tts_vendor"`
	TTSModel   string `json:"tts_model"`
	TTSBaseURL string `json:"tts_base_url"`
	TTSVoice   string `json:"tts_voice"`
}

// handleKeysFrame decodes one raw keys line into the redacting KeysFrame
// and applies it.
func (c *Child) handleKeysFrame(line string) error {
	var w keysWire
	if err := json.Unmarshal([]byte(line), &w); err != nil {
		return fmt.Errorf("voicechild: decode keys frame: %w", err)
	}
	return c.handleKeys(KeysFrame{
		CallID:     w.CallID,
		STTKey:     w.STTKey,
		TTSKey:     w.TTSKey,
		STTVendor:  w.STTVendor,
		STTModel:   w.STTModel,
		STTBaseURL: w.STTBaseURL,
		TTSVendor:  w.TTSVendor,
		TTSModel:   w.TTSModel,
		TTSBaseURL: w.TTSBaseURL,
		TTSVoice:   w.TTSVoice,
	})
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
