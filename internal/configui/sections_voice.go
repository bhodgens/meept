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
