package config

import (
	"os"
	"path/filepath"
	"testing"
)

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
	// Leaf 02 (plan 20261111-voice-turn2): dedicated dead-air nudge + the
	// hangup window after it. H default 45 == S default 45, so the
	// endpoints (nudge@45, hangup@90) are byte-identical to the legacy
	// double-window — flipping them is a contract change, not a tweak.
	if v.SilencePrompt != "Are you still there?" {
		t.Errorf("voice.silence_prompt default = %q, want \"Are you still there?\"", v.SilencePrompt)
	}
	if v.SilenceHangupAfterSeconds != 45 {
		t.Errorf("voice.silence_hangup_after_seconds default = %d, want 45 (S+H = the legacy 45/90 endpoints)", v.SilenceHangupAfterSeconds)
	}
	if v.TurnTimeoutSeconds != 0 {
		t.Errorf("voice.turn_timeout_seconds default = %d, want 0 (opt-in: disabled unless configured)", v.TurnTimeoutSeconds)
	}
	if v.ReorderWindowFrames != 10 {
		t.Errorf("voice.reorder_window_frames default = %d, want 10", v.ReorderWindowFrames)
	}
	if v.ReorderGapMs != 60 {
		t.Errorf("voice.reorder_gap_ms default = %d, want 60", v.ReorderGapMs)
	}
	if v.TurnConfirmMs != 900 {
		t.Errorf("voice.turn_confirm_ms default = %d, want 900 (startguard C1: aggregation ON out of the box; 0 = legacy per-final dispatch)", v.TurnConfirmMs)
	}
	if v.FillerGraceMs != 2500 {
		t.Errorf("voice.filler_grace_ms default = %d, want 2500", v.FillerGraceMs)
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

	// Leg + voice selection defaults (local-leg plan CL4): a config-less
	// boot keeps phone-only behavior byte-identically.
	if v.DefaultLeg != "phone" {
		t.Errorf("voice.default_leg default = %q, want phone", v.DefaultLeg)
	}
	if v.TTSModel != "" {
		t.Errorf("voice.tts_model default = %q, want empty", v.TTSModel)
	}
	if v.TTSVoice != "" {
		t.Errorf("voice.tts_voice default = %q, want empty", v.TTSVoice)
	}
	if v.STTModel != "" {
		t.Errorf("voice.stt_model default = %q, want empty", v.STTModel)
	}
	if v.Local.Enabled != false {
		t.Errorf("voice.local.enabled default = %v, want false (opt-in master switch)", v.Local.Enabled)
	}
	if v.Local.STTProvider != "" {
		t.Errorf("voice.local.stt_provider default = %q, want empty (= voice.stt_provider)", v.Local.STTProvider)
	}
	if v.Local.TTSProvider != "" {
		t.Errorf("voice.local.tts_provider default = %q, want empty (= voice.tts_provider)", v.Local.TTSProvider)
	}
	if v.Local.TTSModel != "" {
		t.Errorf("voice.local.tts_model default = %q, want empty", v.Local.TTSModel)
	}
	if v.Local.TTSVoice != "" {
		t.Errorf("voice.local.tts_voice default = %q, want empty", v.Local.TTSVoice)
	}
	if v.Local.BargeInRms != 3000 {
		t.Errorf("voice.local.barge_in_rms default = %d, want 3000", v.Local.BargeInRms)
	}
	if v.Local.Name != "" {
		t.Errorf("voice.local.name default = %q, want empty (= hostname)", v.Local.Name)
	}
}

// TestSilenceNudgeConfig_TemplateParses keeps the shipped config/meept.json5
// template honest for the leaf-02 silence keys (plan 20261111-voice-turn2):
// the template must parse AND carry the legacy-reproducing defaults
// (S=45, H=45 => nudge@45, hangup@90 exactly as before the split). Mirrors
// TestTurnWatchdogConfig_TemplateParses.
func TestSilenceNudgeConfig_TemplateParses(t *testing.T) {
	const templatePath = "../../config/meept.json5"
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("template not found from test cwd: %v", err)
	}
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	if err := LoadJSON5(abs, c); err != nil {
		t.Fatalf("template meept.json5 failed to parse: %v", err)
	}

	if c.Voice.SilencePromptSeconds != 45 {
		t.Errorf("template voice.silence_prompt_seconds = %d, want 45 (template reproduces today's endpoint)", c.Voice.SilencePromptSeconds)
	}
	if c.Voice.SilencePrompt != "Are you still there?" {
		t.Errorf("template voice.silence_prompt = %q, want \"Are you still there?\"", c.Voice.SilencePrompt)
	}
	if c.Voice.SilenceHangupAfterSeconds != 45 {
		t.Errorf("template voice.silence_hangup_after_seconds = %d, want 45 (S+H = legacy 45/90)", c.Voice.SilenceHangupAfterSeconds)
	}
}

// TestSilenceNudgeConfig_ExplicitValuesSurviveLoad proves an operator who
// sets the live-config shape (S=10, H=45 -> nudge@10, hangup@55) is not
// clobbered back to the defaults by the load machinery.
func TestSilenceNudgeConfig_ExplicitValuesSurviveLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	body := `{
  "voice": {
    "silence_prompt_seconds": 10,
    "silence_prompt": "Are you still there?",
    "silence_hangup_after_seconds": 45,
  },
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	c := DefaultConfig()
	if err := LoadJSON5(path, c); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Voice.SilencePromptSeconds != 10 {
		t.Errorf("silence_prompt_seconds = %d, want 10 (explicit value must survive)", c.Voice.SilencePromptSeconds)
	}
	if c.Voice.SilencePrompt != "Are you still there?" {
		t.Errorf("silence_prompt = %q, want the configured line", c.Voice.SilencePrompt)
	}
	if c.Voice.SilenceHangupAfterSeconds != 45 {
		t.Errorf("silence_hangup_after_seconds = %d, want 45", c.Voice.SilenceHangupAfterSeconds)
	}
}

// TestMinTurnWordsConfig (voice-microfrag leaf 01): voice.min_turn_words
// defaults to 0 — the short-final gate is OPT-IN (docs/agents/opt-in-
// defaults.md): word filters are wrong by default because an operator
// command like "Stop." is one word, so only a loud-room operator flips
// it. Pins: DefaultConfig = 0, explicit load survives, template ships
// the key at 0 and parses.
func TestMinTurnWordsConfig(t *testing.T) {
	if got := DefaultConfig().Voice.MinTurnWords; got != 0 {
		t.Errorf("voice.min_turn_words default = %d, want 0 (gate disabled out of the box — operator-tunable loud-room filter)", got)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	body := `{
  "voice": {
    "min_turn_words": 2,
  },
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c := DefaultConfig()
	if err := LoadJSON5(path, c); err != nil {
		t.Fatalf("load: %v", err)
	}
	if c.Voice.MinTurnWords != 2 {
		t.Errorf("min_turn_words = %d, want 2 (explicit value must survive the load)", c.Voice.MinTurnWords)
	}

	// Template ships the key at the disabled default and still parses.
	const templatePath = "../../config/meept.json5"
	if _, err := os.Stat(templatePath); err != nil {
		t.Skipf("template not found from test cwd: %v", err)
	}
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	tpl := DefaultConfig()
	if err := LoadJSON5(abs, tpl); err != nil {
		t.Fatalf("template meept.json5 failed to parse: %v", err)
	}
	if tpl.Voice.MinTurnWords != 0 {
		t.Errorf("template voice.min_turn_words = %d, want 0 (template reproduces today's behavior: gate off)", tpl.Voice.MinTurnWords)
	}
}
