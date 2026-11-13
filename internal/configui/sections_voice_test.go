package configui

// Voice-peragent leaf 01 parity: every new global [voice] acoustic key
// must be surfaced in the voice section, resolve against the real
// config schema (GetKeypath over DefaultConfig — the same reflection
// the editor uses), and render the loaded value.

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

func TestVoiceSectionAcousticKeysParity(t *testing.T) {
	fields := buildVoiceFields()
	byKey := map[string]Field{}
	for _, f := range fields {
		byKey[f.Key()] = f
	}
	want := []string{
		"voice.turn_confirm_ms", "voice.min_turn_words", "voice.exempt_phrases",
		"voice.energy_gate", "voice.min_rms", "voice.fragment_decay_s",
		"voice.reply_exempt_s", "voice.backchannel_soften", "voice.filler_grace_ms",
		// pre-existing knobs the leaf documents as row-overridable:
		"voice.silence_prompt_seconds", "voice.silence_prompt",
		"voice.silence_hangup_after_seconds", "voice.local.barge_in_rms",
	}
	for _, k := range want {
		if _, ok := byKey[k]; !ok {
			t.Errorf("voice section missing field %q (leaf-01 parity)", k)
		}
	}
	// Duplicate keys would silently drop one of them on save.
	seen := map[string]bool{}
	for _, f := range fields {
		if seen[f.Key()] {
			t.Errorf("duplicate voice field key %q", f.Key())
		}
		seen[f.Key()] = true
	}
}

// The acoustic keys must resolve through the editor's keypath
// reflection onto the real schema fields (a key that only exists in
// the UI would save-fail at runtime).
func TestVoiceAcousticKeypathsResolve(t *testing.T) {
	cfg := config.DefaultConfig()
	deref := func(b *bool) string {
		if b == nil {
			return ""
		}
		return "true"
	}
	cases := []struct {
		key, want string
	}{
		{"voice.min_turn_words", "0"},
		{"voice.energy_gate", "false"},
		{"voice.min_rms", "0"},
		{"voice.fragment_decay_s", "0"},
		{"voice.reply_exempt_s", "15"},
		{"voice.turn_confirm_ms", "900"},
		{"voice.filler_grace_ms", "2500"},
		{"voice.backchannel_soften", deref(cfg.Voice.BackchannelSoften)},
	}
	for _, c := range cases {
		got, err := GetKeypath(cfg, c.key)
		if err != nil {
			t.Errorf("GetKeypath(%s): %v (key does not resolve onto the schema)", c.key, err)
			continue
		}
		if got != c.want {
			t.Errorf("GetKeypath(%s) = %q, want %q", c.key, got, c.want)
		}
	}
	got, err := GetKeypath(cfg, "voice.exempt_phrases")
	if err != nil {
		t.Fatalf("GetKeypath(voice.exempt_phrases): %v", err)
	}
	for _, ph := range []string{"stop", "thanks", "go ahead"} {
		if !strings.Contains(got, ph) {
			t.Errorf("exempt_phrases keypath render %q missing %q", got, ph)
		}
	}
}
