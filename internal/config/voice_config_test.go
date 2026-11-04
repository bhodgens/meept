package config

import "testing"

// TestVoiceConfigDefaults pins the C3 voice config defaults verbatim from
// docs/plans/20261102-voice-sidecar/master.md. Every value below is quoted
// from contract C3; changing one of them is a contract change, not a test
// tweak. Keys with no stated default in C3 are asserted only where the
// contract states the fail-closed posture (unset public key, empty loopback
// addr, empty child binary override).
func TestVoiceConfigDefaults(t *testing.T) {
	cfg := DefaultConfig()
	v := cfg.Voice

	if v.Enabled != false {
		t.Errorf("voice.enabled default = %v, want false", v.Enabled)
	}
	if v.IdleStopSeconds != 30 {
		t.Errorf("voice.idle_stop_seconds default = %d, want 30", v.IdleStopSeconds)
	}
	if v.SupervisorEnabled != true {
		t.Errorf("voice.supervisor_enabled default = %v, want true", v.SupervisorEnabled)
	}
	if v.SupervisorTimeoutMs != 400 {
		t.Errorf("voice.supervisor_timeout_ms default = %d, want 400", v.SupervisorTimeoutMs)
	}
	if v.TimeoutSecs != 30 {
		t.Errorf("voice.timeout_secs default = %d, want 30", v.TimeoutSecs)
	}
	if v.MaxCallMinutes != 15 {
		t.Errorf("voice.max_call_minutes default = %d, want 15", v.MaxCallMinutes)
	}
	if v.SilencePromptSeconds != 45 {
		t.Errorf("voice.silence_prompt_seconds default = %d, want 45", v.SilencePromptSeconds)
	}
	if v.ReorderWindowFrames != 10 {
		t.Errorf("voice.reorder_window_frames default = %d, want 10", v.ReorderWindowFrames)
	}
	if v.ReorderGapMs != 60 {
		t.Errorf("voice.reorder_gap_ms default = %d, want 60", v.ReorderGapMs)
	}
	if len(v.Filler) != 1 || v.Filler[0] != "Hold on, let me check." {
		t.Errorf("voice.filler default = %q, want exactly [\"Hold on, let me check.\"]", v.Filler)
	}

	// Fail-closed posture keys: unset unless configured (C3).
	if v.TelnyxPublicKey != "" {
		t.Errorf("voice.telnyx_public_key default = %q, want empty (unset key = routes 503, fail-closed)", v.TelnyxPublicKey)
	}
	if v.DefaultEmployee != "" {
		t.Errorf("voice.default_employee default = %q, want empty (the phone employee is operator-configured)", v.DefaultEmployee)
	}
	if v.LoopbackPlainAddr != "" {
		t.Errorf("voice.loopback_plain_addr default = %q, want empty (no extra listener by default)", v.LoopbackPlainAddr)
	}
	if v.ChildBinaryPath != "" {
		t.Errorf("voice.child_binary_path default = %q, want empty (daemon-adjacent lookup)", v.ChildBinaryPath)
	}
	if v.PublicStreamURL != "" {
		t.Errorf("voice.public_stream_url default = %q, want empty (refusal until configured)", v.PublicStreamURL)
	}
	if v.ConnectionID != "" {
		t.Errorf("voice.connection_id default = %q, want empty", v.ConnectionID)
	}
}
