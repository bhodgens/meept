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
	// DeepgramEndpointMs > 0 enables the authoritative speech_final /
	// UtteranceEnd fast-dispatch path (contract C1, feature F4). 0 keeps
	// the legacy fixed-timer endpointing byte-identical.
	DeepgramEndpointMs int
	// WhisperServerAddr, when non-empty, selects the persistent whisper.cpp
	// server transport (no per-utterance subprocess). Empty = the legacy
	// byte-identical exec path. Additive/opt-in (contract C1, feature F2).
	WhisperServerAddr string
	// Qwen3ChunkSize overrides the qwen3 pcm-stream chunk_size; 0 => the
	// default qwen3PCMChunkSize (4, byte-identical legacy). Positive wins.
	Qwen3ChunkSize int
	// Vocabulary primes critical entities on the STT request (contract C1,
	// feature F5). nil/empty keeps both wire requests BYTE-IDENTICAL to
	// legacy (R4); non-empty adds Deepgram keyterm params and a Whisper
	// -p prompt. Additive/opt-in.
	Vocabulary []string
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
	// TurnConfirmMs is a pointer so the child can tell an ABSENT
	// selector (old daemon line -> the aggregation default,
	// defaultTurnConfirmMs) from an explicit 0 (operator chose legacy
	// per-final dispatch). A negative value clamps to 0 at decode.
	TurnConfirmMs *int `json:"turn_confirm_ms"`
	// MinTurnWords is the voice-microfrag short-final gate selector.
	// Unlike turn_confirm_ms its ABSENT state is the DISABLED default
	// (0), the same as an explicit 0 — the gate only does something at
	// N >= 1, so absent/explicit-0 collapse to legacy behavior and a
	// pointer is the honest wire shape for "default = zero value". A
	// negative value clamps to 0.
	MinTurnWords *int `json:"min_turn_words"`
	// ---- voice-peragent leaf 01: short-final gate improvements ----
	// All ADDITIVE optional keys: an old daemon line omits them and
	// the gate stays exactly its pre-leaf shape (no exemptions; energy
	// off; decay off; reply window = defaultReplyExemptS). The current
	// daemon always sends the effective resolved exemption set.
	ExemptPhrases  []string `json:"exempt_phrases"`
	EnergyGate     *bool    `json:"energy_gate,omitempty"`
	MinRMS         *int     `json:"min_rms,omitempty"`
	FragmentDecayS *int     `json:"fragment_decay_s,omitempty"`
	ReplyExemptS   *int     `json:"reply_exempt_s,omitempty"`
	// TTSRefHash (leaf 03, filler clips, ADDITIVE optional): the
	// hex(sha256) of the clone reference audio for the configured TTS
	// voice ("" = non-clone / old daemon). Together with the vendor/
	// model/voice selectors it is the Contract C1 identity input the
	// daemon planner hashed the clip keys with; the child stores it
	// decoded on the pipeline (p.ttsRefHash) for the byte-equality
	// contract test between the two key computations.
	TTSRefHash string `json:"tts_ref_hash"`
	// ---- voice TTS/STT optimization selectors (20261118, contract C1) ----
	// All ADDITIVE/OPTIONAL: absent on an old daemon line decodes as the
	// zero value (= legacy behavior). The daemon sends them omitempty,
	// so a zero-value frame never carries these keys on the wire.
	SmartTurnMode       int      `json:"smart_turn_mode"`
	WhisperServerAddr   string   `json:"whisper_server_addr"`
	DeepgramEndpointMs  int      `json:"deepgram_endpoint_ms"`
	Vocabulary          []string `json:"vocabulary"`
	Qwen3ChunkSize      int      `json:"qwen3_chunk_size"`
	ClipPrewarmDelayMs  int      `json:"clip_prewarm_delay_ms"`
	ContextWarmWindowMs int      `json:"context_warm_window_ms"`
}

// handleKeysFrame decodes one raw keys line into the redacting KeysFrame
// and applies it.
func (c *Child) handleKeysFrame(line string) error {
	var w keysWire
	if err := json.Unmarshal([]byte(line), &w); err != nil {
		return fmt.Errorf("voicechild: decode keys frame: %w", err)
	}
	// Absent turn_confirm_ms (old daemon line) = the aggregation
	// default; explicit 0 = legacy per-final dispatch; negative clamps
	// to legacy (the config load boundary clamps too — belt and braces
	// on a wire value).
	turnConfirmMs := defaultTurnConfirmMs
	if w.TurnConfirmMs != nil {
		turnConfirmMs = max(*w.TurnConfirmMs, 0)
	}
	// Absent min_turn_words (old daemon line) = 0 = gate disabled
	// (legacy behavior); explicit 0 the same; negative clamps to 0 —
	// the config load boundary clamps too (belt and braces on a wire
	// value).
	minTurnWords := 0
	if w.MinTurnWords != nil {
		minTurnWords = max(*w.MinTurnWords, 0)
	}
	// Gate improvements (voice-peragent leaf 01). Absent-on-wire
	// (old daemon) lands on the gate's today-shape defaults: nil
	// exemptions (the gate then falls back to the shipped default
	// set), energy off, decay off, reply window = defaultReplyExemptS.
	// Negatives clamp to the disabled value on every numeric field.
	exemptPhrases := w.ExemptPhrases
	energyGate := false
	if w.EnergyGate != nil {
		energyGate = *w.EnergyGate
	}
	minRMS := 0
	if w.MinRMS != nil && *w.MinRMS > 0 {
		minRMS = *w.MinRMS
	}
	fragmentDecayS := 0
	if w.FragmentDecayS != nil && *w.FragmentDecayS > 0 {
		fragmentDecayS = *w.FragmentDecayS
	}
	replyExemptS := defaultReplyExemptS
	if w.ReplyExemptS != nil {
		replyExemptS = max(*w.ReplyExemptS, 0)
	}
	return c.handleKeys(KeysFrame{
		CallID:         w.CallID,
		STTKey:         w.STTKey,
		TTSKey:         w.TTSKey,
		STTVendor:      w.STTVendor,
		STTModel:       w.STTModel,
		STTBaseURL:     w.STTBaseURL,
		TTSVendor:      w.TTSVendor,
		TTSModel:       w.TTSModel,
		TTSBaseURL:     w.TTSBaseURL,
		TTSVoice:       w.TTSVoice,
		TurnConfirmMs:  turnConfirmMs,
		MinTurnWords:   minTurnWords,
		ExemptPhrases:  exemptPhrases,
		EnergyGate:     energyGate,
		MinRMS:         minRMS,
		FragmentDecayS: fragmentDecayS,
		ReplyExemptS:   replyExemptS,
		TTSRefHash:     w.TTSRefHash,
		// TTS/STT optimization selectors: absent-on-wire decodes as the
		// zero value, which is every knob's legacy behavior.
		SmartTurnMode:       w.SmartTurnMode,
		WhisperServerAddr:   w.WhisperServerAddr,
		DeepgramEndpointMs:  w.DeepgramEndpointMs,
		Vocabulary:          w.Vocabulary,
		Qwen3ChunkSize:      w.Qwen3ChunkSize,
		ClipPrewarmDelayMs:  w.ClipPrewarmDelayMs,
		ContextWarmWindowMs: w.ContextWarmWindowMs,
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
	// Leaf 05 hook: if setup/start ran before keys (the normal daemon
	// order), the transcript session was still unbound - start it now that
	// the real provider exists. The once-guard makes this a no-op when
	// keys arrived first (start already bound it).
	p.startTranscripts()
	return nil
}
