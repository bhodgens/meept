package daemon

// Daemon-side producer of the extended C1 keys frame (voice-tierA leaf 03;
// contracts C1/C4 in docs/plans/20261106-voice-tierA-qwen3-tts/master.md).
//
// The child has consumed keys lines since the sidecar's leaf 08, but no
// daemon code ever sent one, so real providers never constructed in
// production and every call ran on the stub STT/TTS. This file closes that
// producer half: sendKeysFrame builds the extended C1 keys line from
// VoiceConfig + broker secrets and sends it once per call, immediately
// after routeFrame registers the call's talk loop.
//
// Semantics (C4/D2):
//   - vendor/model/base_url/voice come from the top-level voice.* config
//     fields; an empty vendor falls back to the hardcoded default adapter
//     (deepgram / openai) so existing deployments keep today's behavior.
//   - stt_key resolves deepgram_api_key, tts_key resolves openai_api_key
//     via Broker.Value (in-process only; allowlisted in
//     scripts/audit-secrets-value-callers.py). A missing secret or broker
//     sends "" - the child factory then fails THAT call
//     (ErrMissingSTTKey/ErrMissingTTSKey); the child and other calls stay
//     up. qwen3 is keyless and ignores tts_key entirely.
//   - A send error is a warn log and nothing more: the call continues on
//     stub audio (degrade, never teardown; supervisor fail-open posture).
//   - Key material is never logged and never appears at info+: presence
//     booleans ride the debug line only.

import (
	"encoding/json"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/voice"
)

// keysWire is the daemon-side C1 keys JSON shape. Field-for-field identical
// to the child decoder's keysWire (internal/voicechild/providers_wiring.go):
// no omitempty anywhere, because an empty cloud key is SENT as "" (the
// child factory fails the call) rather than skipped, and the gate tests pin
// tts_base_url presence even when empty.
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
	// TurnConfirmMs is the voice-startguard C1 selector: config
	// voice.turn_confirm_ms (default 900) — the child aggregates
	// consecutive FINAL transcripts into ONE employee-turn prompt while
	// finals keep landing inside this window. Sent always (no omitempty,
	// the keys-wire convention); an old child ignores the extra field,
	// and an old DAEMON line (field absent child-side) leaves the child
	// on its own default. 0 = legacy per-final dispatch.
	TurnConfirmMs int `json:"turn_confirm_ms"`
	// MinTurnWords is the voice-microfrag short-final gate selector:
	// the RESOLVED per-employee voice.min_turn_words (voice-peragent
	// leaf 01: employee_leg row > global; default 0 = disabled). Sent
	// always (no omitempty, the keys-wire convention); an old child
	// ignores the extra field, and an old daemon line (field absent
	// child-side) decodes as 0 — the gate's disabled default, so
	// absent and explicit-0 are the same legacy behavior.
	MinTurnWords int `json:"min_turn_words"`
	// ---- voice-peragent leaf 01: the short-final gate improvements ----
	// All ADDITIVE optional keys (backward-compatible on the C1 wire):
	// a keys-less or old-daemon line leaves the child on the gate's
	// today-shape defaults (empty exemptions, no energy gate, no decay,
	// reply window 15). Absent-vs-explicit distinction rides pointers
	// where the defaults differ from the zero value.
	//
	// ExemptPhrases is the effective exemption set for this call
	// (resolved row > global). nil is normalized to
	// config.DefaultVoiceExemptPhrases() at the emit site, so the
	// line ALWAYS carries the effective set and the child's absent-
	// on-wire fallback (no exemptions = the legacy gate) never binds
	// behind a current daemon.
	ExemptPhrases []string `json:"exempt_phrases"`
	// EnergyGate+MinRMS: loudness-aware short-final gate (see config
	// voice.energy_gate / voice.min_rms). energy_gate rides a pointer:
	// absent = false (the plain word-count gate).
	EnergyGate *bool `json:"energy_gate,omitempty"`
	MinRMS     int   `json:"min_rms,omitempty"`
	// FragmentDecayS: consecutive-drop escalation window (0 = off).
	FragmentDecayS int `json:"fragment_decay_s,omitempty"`
	// ReplyExemptS rides a pointer because its DEFAULT is 15, not the
	// wire-zero: absent (old daemon) = the child's own 15 s default;
	// an explicit 0 disables the reply exemption.
	ReplyExemptS *int `json:"reply_exempt_s,omitempty"`
	// TTSRefHash is the filler-clip identity input (plan
	// 20261113-filler-clips leaf 02, Contract C1): hex(sha256(reference
	// audio bytes)) for a qwen3 CLONE voice, "" for a preset voice.
	// Sent always (no omitempty, the keys-wire convention); an old
	// child ignores the extra field. The qwen3 endpoint is an
	// OpenAI-compatible shim and the clone REFERENCE never rides a
	// request — this hash feeds ONLY clip-key identity, so two
	// deployments sharing model+voice name but different reference
	// audio get different clip keys. The child hex-decodes it back to
	// the bytes the daemon's planner fed clipstore.ClipKey
	// (voicechild.decodeTTSRefHash — byte agreement pinned there).
	TTSRefHash string `json:"tts_ref_hash"`
	// ---- voice TTS/STT optimization selectors (20261118, C1) ----
	// ADDITIVE + OMITEMPTY: unlike the older selectors above, zero/empty
	// MUST leave the wire byte-identical to a pre-change daemon line, so
	// these carry omitempty. An old child ignores unknown keys; a current
	// child decodes absent as the legacy zero value.
	SmartTurnMode       int      `json:"smart_turn_mode,omitempty"`
	WhisperServerAddr   string   `json:"whisper_server_addr,omitempty"`
	DeepgramEndpointMs  int      `json:"deepgram_endpoint_ms,omitempty"`
	Vocabulary          []string `json:"vocabulary,omitempty"`
	Qwen3ChunkSize      int      `json:"qwen3_chunk_size,omitempty"`
	ClipPrewarmDelayMs  int      `json:"clip_prewarm_delay_ms,omitempty"`
	ContextWarmWindowMs int      `json:"context_warm_window_ms,omitempty"`
	// ---- rumble streaming/kwargs selectors (C4, ADDITIVE + OMITEMPTY) ----
	// Absent on the wire = zero/nil = legacy. The emit site waits for
	// leaf 03's VoiceConfig C6 fields (ChatTemplateLanguage,
	// ChatTemplateDigits/Filler/Verbatim, STTPartialMs); until then these
	// stay zero/nil so the daemon line remains byte-identical.
	ChatTemplateLanguage string `json:"chat_template_language,omitempty"`
	ChatTemplateDigits   *bool  `json:"chat_template_digits,omitempty"`
	ChatTemplateFiller   *bool  `json:"chat_template_filler,omitempty"`
	ChatTemplateVerbatim *bool  `json:"chat_template_verbatim,omitempty"`
	STTPartialMs         *int   `json:"stt_partial_ms,omitempty"`
}

// Default vendor adapters when the config fields are empty (C4: preserves
// today's intent - the adapters the child fell back to before per-call
// vendors existed).
const (
	defaultSTTVendor = "deepgram"
	defaultTTSVendor = "openai"
)

// Text-socket keys event value (C1). The keys event is deliberately NOT in
// voiceframe.Frame: the frame codec carries no key material, and the keys
// line is decoded raw child-side (exactly as before this leaf).
const keysEvent = "keys"

// sendKeysFrame builds and sends the extended C1 keys frame for ccid.
// Called once per call from routeFrame's EventSetup branch AFTER the talk
// loop registers; the loop-level idempotency guard (F-6) already returned
// on duplicate setups before reaching us, so duplicates never re-send.
//
// Local-leg leaf 04 (CL4): the selector fields come from ResolveProfile
// for THIS call's employee + leg (voice.employee_leg row > per-leg config
// > provider default), not from the global [voice] block directly. The
// registry row is the authority for employee and leg (acquire pinned
// them); a ccid missing from the registry resolves with an empty
// employeeID, which lands on the same global values as before - the
// phone-leg default config reproduces byte-identically. Base URLs stay
// global: per the frozen tree split they are config, not per-agent
// routing, and Profile carries none by design.
func (w *VoiceWiring) sendKeysFrame(ccid string) {
	employeeID, leg := "", voice.LegPhone
	for _, inst := range w.Registry.Live() {
		if inst.CallControlID == ccid {
			employeeID = inst.EmployeeID
			if inst.Leg != "" {
				leg = inst.Leg
			}
			break
		}
	}
	// VoiceConfig snapshot: the LIVE one once a daemon.reloaded
	// republished it (leaf 02), the construction value otherwise. Same
	// profile/acoustics resolution as before — only the source moves.
	vcfg := w.currentVoiceCfg()
	p := voice.ResolveProfile(vcfg, employeeID, "", leg)
	// Acoustic controls (voice-peragent leaf 01): resolve the whole
	// per-employee set ONCE per call (employee_leg row > global,
	// voice.ResolveAcoustics) and let every gate selector read it. The
	// exemption list always lands on an effective set at the emit site
	// (a hand-built config with nil ExemptPhrases sends the shipped
	// default set; a loaded config carries DefaultConfig's or the
	// user's list), so the child never has to guess between "absent"
	// and "empty = no exemptions".
	ac := voice.ResolveAcoustics(vcfg, employeeID)
	if ac.ExemptPhrases == nil {
		ac.ExemptPhrases = config.DefaultVoiceExemptPhrases()
	}
	energyGate := ac.EnergyGate
	replyExemptS := ac.ReplyExemptS
	kw := keysWire{
		CallID:     ccid,
		Event:      keysEvent,
		STTVendor:  p.SttVendor,
		STTModel:   p.SttModel,
		TTSVendor:  p.TtsVendor,
		TTSModel:   p.TtsModel,
		TTSBaseURL: vcfg.TTSBaseURL,
		TTSVoice:   p.TtsVoice,
		// Turn-aggregation window (voice-startguard C1) — per-agent
		// since voice-peragent leaf 01 (employee_leg.turn_confirm_ms
		// row override, nil inherits global). A loaded config carries
		// voice.turn_confirm_ms (DefaultConfig: 900; explicit 0 =
		// legacy per-final dispatch). A hand-built VoiceConfig that
		// never set the field sends 0: the legacy shape, which is the
		// exact pre-C1 behavior old call sites had.
		TurnConfirmMs: ac.TurnConfirmMs,
		// Short-final gate (voice-microfrag; row-overridable since
		// leaf 01): the resolved min_turn_words. Default 0 = disabled.
		MinTurnWords: ac.MinTurnWords,
		// Gate improvements (voice-peragent leaf 01) — all inert while
		// MinTurnWords is 0, so zero-config lines stay byte-compatible
		// with the plain word-count gate even when the keys ride.
		ExemptPhrases:  ac.ExemptPhrases,
		EnergyGate:     &energyGate,
		MinRMS:         ac.MinRMS,
		FragmentDecayS: ac.FragmentDecayS,
		ReplyExemptS:   &replyExemptS,
		// Clip identity (leaf 02): the SAME ref-hash bytes the planner
		// fed ClipKey for this call ("" = preset/non-clone). The hash
		// is content-addressed, never the file path or its bytes.
		TTSRefHash: refHashHex(w.ttsRefHash(vcfg)),
		// TTS/STT optimization selectors (C1): from the live VoiceConfig
		// snapshot; all zero/empty by default so the wire stays byte-
		// identical to a pre-change daemon line (omitempty on each).
		SmartTurnMode:       vcfg.SmartTurnMode,
		WhisperServerAddr:   vcfg.WhisperServerAddr,
		DeepgramEndpointMs:  vcfg.DeepgramEndpointMs,
		Vocabulary:          vcfg.RecognitionVocabulary,
		Qwen3ChunkSize:      vcfg.Qwen3ChunkSize,
		ClipPrewarmDelayMs:  vcfg.ClipPrewarmDelayMs,
		ContextWarmWindowMs: vcfg.ContextWarmWindowMs,
	}
	if kw.STTVendor == "" {
		kw.STTVendor = defaultSTTVendor
	}
	if kw.TTSVendor == "" {
		kw.TTSVendor = defaultTTSVendor
	}
	// STT base URL: when set, the child dials Deepgram (or any
	// wire-compatible STT) at this endpoint instead of the cloud default
	// - the same contract TTS honors for tts_base_url.
	kw.STTBaseURL = vcfg.STTBaseURL
	if w.broker == nil {
		// Loud WARN in the ResolveVoiceSecrets style: a broker-less
		// install must still run, but every cloud-provider call would
		// fail child-side provider construction without its keys.
		w.logger.Warn("voice wiring: no secrets broker configured; " +
			"cloud STT/TTS keys sent empty (declare deepgram_api_key and " +
			"openai_api_key under [secrets.sources]); calls on keyed " +
			"vendors fail their provider construction child-side")
	} else {
		kw.STTKey = w.voiceSecret("deepgram_api_key")
		kw.TTSKey = w.voiceSecret("openai_api_key")
	}
	line, err := json.Marshal(kw)
	if err != nil {
		w.logger.Warn("voice wiring: keys frame encode failed; call continues without provider keys",
			"ccid", ccid, "err", err)
		return
	}
	// Send failure degrades: warn and continue on stub audio, never
	// tear the call down.
	if err := w.TextServer.SendRaw(append(line, '\n')); err != nil {
		w.logger.Warn("voice wiring: keys frame send failed; call continues on stub audio (degrade, not teardown)",
			"ccid", ccid, "err", err)
		return
	}
	// Presence booleans only - never the values, never their lengths.
	w.logger.Debug("voice wiring: keys frame sent",
		"ccid", ccid,
		"stt_key_present", kw.STTKey != "",
		"tts_key_present", kw.TTSKey != "")
}

// voiceSecret resolves one cloud provider secret in-process via
// Broker.Value (allowlisted in scripts/audit-secrets-value-callers.py).
// The value moves from broker memory into the outgoing C1 keys line ONLY -
// never a log line, env entry, command argument, or bus payload. A lookup
// error sends "" (child factory fails that call) with a debug log naming
// the secret, not the value.
func (w *VoiceWiring) voiceSecret(name string) string {
	val, err := w.broker.Value(name)
	if err != nil {
		w.logger.Debug("voice wiring: cloud key unresolvable; sending empty", "secret", name, "err", err)
		return ""
	}
	return val
}
