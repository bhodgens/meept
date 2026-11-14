// internal/configui/sections_voice.go
package configui

import "strings"

// voiceJoinSlice renders a []string config field as a comma-separated
// display value; nil and empty slices both show as empty.
func voiceJoinSlice(vals []string) string {
	return strings.Join(vals, ", ")
}

func buildVoiceFields() []Field {
	cfg := loadMainConfigOrFallback()
	s := &cfg.Voice
	return []Field{
		NewToggleField("voice.enabled", "enabled", s.Enabled),
		NewNumberField("voice.idle_stop_seconds", "idle stop seconds", s.IdleStopSeconds),
		NewTextField("voice.onnx_runtime_lib", "onnx runtime lib", s.OnnxRuntimeLib),
		NewTextField("voice.stt_provider", "stt provider", s.STTProvider),
		NewTextField("voice.tts_provider", "tts provider", s.TTSProvider),
		NewTextField("voice.filler", "filler lines (comma separated)", voiceJoinSlice(s.Filler)),
		// C5 lane pools (plan 20261113-filler-clips leaf 05): the two
		// list-shape overrides + the new farewell pool ride the same
		// comma-separated text-field pattern as voice.filler (a
		// non-empty list beats the scalar on resolution). voice.greeting
		// is surfaced alongside them so all five lanes are editable from
		// one block.
		NewTextField("voice.greeting", "greeting line", s.GreetingLine),
		NewTextField("voice.silence_prompt_lines", "silence prompt lines (comma separated)", voiceJoinSlice(s.SilencePromptLines)),
		NewTextField("voice.fragment_clarify_lines", "fragment clarify lines (comma separated)", voiceJoinSlice(s.FragmentClarifyLines)),
		NewTextField("voice.farewell", "farewell lines (comma separated)", voiceJoinSlice(s.Farewell)),
		// Short-final gate + acoustic knobs (voice-peragent leaf 01
		// parity): every global [voice] gate key rides here; the
		// per-employee employee_leg rows are edited in the JSON5 file
		// (the GUI surface for rows is tracked separately, same
		// posture as the pre-leaf employee_leg block).
		NewNumberField("voice.turn_confirm_ms", "turn confirm ms", s.TurnConfirmMs),
		// Partial context warm (phase2-context-warm leaf 02, C6): the
		// warm debounce window IS turn_confirm_ms above, so only the
		// flag + token floor are separate knobs.
		NewToggleField("voice.context_warm", "context warm (partials pre-warm the LLM cache)", s.ContextWarm),
		NewNumberField("voice.context_warm_min_tokens", "context warm min tokens", s.ContextWarmMinTokens),
		NewNumberField("voice.min_turn_words", "min turn words", s.MinTurnWords),
		NewTextField("voice.exempt_phrases", "exempt phrases (comma separated)", voiceJoinSlice(s.ExemptPhrases)),
		NewToggleField("voice.energy_gate", "energy gate", s.EnergyGate),
		NewNumberField("voice.min_rms", "min rms", s.MinRMS),
		NewNumberField("voice.fragment_decay_s", "fragment decay s", s.FragmentDecayS),
		NewNumberField("voice.reply_exempt_s", "reply exempt s", s.ReplyExemptS),
		NewToggleField("voice.backchannel_soften", "backchannel soften", s.BackchannelSoften != nil && *s.BackchannelSoften),
		NewNumberField("voice.filler_grace_ms", "filler grace ms", s.FillerGraceMs),
		NewTextField("voice.supervisor_employee", "supervisor employee", s.SupervisorEmployee),
		NewToggleField("voice.supervisor_enabled", "supervisor enabled", s.SupervisorEnabled),
		NewTextField("voice.supervisor_model", "supervisor model", s.SupervisorModel),
		NewNumberField("voice.supervisor_timeout_ms", "supervisor timeout ms", s.SupervisorTimeoutMs),
		NewTextField("voice.voice_model", "voice model", s.VoiceModel),
		NewTextField("voice.public_stream_url", "public stream url", s.PublicStreamURL),
		NewTextField("voice.connection_id", "connection id", s.ConnectionID),
		NewTextField("voice.from_number", "from number", s.FromNumber),
		NewTextField("voice.allowed_numbers", "allowed numbers (comma separated)", voiceJoinSlice(s.AllowedNumbers)),
		NewNumberField("voice.timeout_secs", "timeout secs", s.TimeoutSecs),
		NewNumberField("voice.max_call_minutes", "max call minutes", s.MaxCallMinutes),
		NewNumberField("voice.silence_prompt_seconds", "silence prompt seconds", s.SilencePromptSeconds),
		NewTextField("voice.silence_prompt", "silence prompt line", s.SilencePrompt),
		NewNumberField("voice.silence_hangup_after_seconds", "silence hangup after seconds", s.SilenceHangupAfterSeconds),
		NewNumberField("voice.max_concurrent_calls", "max concurrent calls", s.MaxConcurrentCalls),
		NewNumberField("voice.reorder_window_frames", "reorder window frames", s.ReorderWindowFrames),
		NewNumberField("voice.reorder_gap_ms", "reorder gap ms", s.ReorderGapMs),
		NewTextField("voice.telnyx_public_key", "telnyx public key", s.TelnyxPublicKey),
		NewTextField("voice.default_leg", "default leg (phone|local)", s.DefaultLeg),
		NewTextField("voice.tts_model", "tts model", s.TTSModel),
		NewTextField("voice.tts_voice", "tts voice", s.TTSVoice),
		NewTextField("voice.stt_model", "stt model", s.STTModel),
		NewToggleField("voice.local.enabled", "local enabled", s.Local.Enabled),
		NewTextField("voice.local.stt_provider", "local stt provider", s.Local.STTProvider),
		NewTextField("voice.local.tts_provider", "local tts provider", s.Local.TTSProvider),
		NewTextField("voice.local.tts_model", "local tts model", s.Local.TTSModel),
		NewTextField("voice.local.tts_voice", "local tts voice", s.Local.TTSVoice),
		NewNumberField("voice.local.barge_in_rms", "local barge in rms", s.Local.BargeInRms),
		NewTextField("voice.local.name", "local session name", s.Local.Name),
		NewTextField("voice.child_binary_path", "child binary path", s.ChildBinaryPath),
		NewTextField("voice.loopback_plain_addr", "loopback plain addr", s.LoopbackPlainAddr),
	}
}
