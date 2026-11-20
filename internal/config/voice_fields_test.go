package config

// Gate tests T6 (feature F6 config half, leaf 01): VoiceConfig gains
// STTModel, TTSModel, TTSBaseURL, TTSVoice with dual json+toml tags,
// defaulting to ""; the shipped template documents the keys.
//
// RECONCILE NOTE (orchestrator, cb8c8672): the voice-local-leg tree
// already added DefaultLeg, TTSModel, TTSVoice, STTModel. Those three
// parts of this test may already be GREEN; TTSBaseURL is the genuinely
// new field. The test asserts all four per the contract and the failure
// messages carry the RED classification.
//
// RED style: reflection-gated so the config package still compiles and
// its existing suite stays runnable. Missing symbols named in failures:
//   leaf 01: VoiceConfig.TTSBaseURL + template key tts_base_url

import (
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/pelletier/go-toml/v2"
)

// voiceFieldContract pins the four contract fields and their EXACT dual
// tags (leaf 01: "exact names/tags").
var voiceFieldContract = []struct {
	field   string
	jsonTag string
	tomlTag string
	note    string
}{
	{"STTModel", "stt_model", "stt_model", "added by local-leg tree; expected present"},
	{"TTSModel", "tts_model", "tts_model", "added by local-leg tree; expected present"},
	{"TTSBaseURL", "tts_base_url", "tts_base_url", "NEW in 20261106-voice-tierA leaf 01"},
	{"TTSVoice", "tts_voice", "tts_voice", "added by local-leg tree; expected present"},
}

// voiceFieldString reads one string field via reflection; ok=false when
// the contract field does not exist yet (named in the caller's failure).
func voiceFieldString(vc *VoiceConfig, name string) (string, bool) {
	v := reflect.ValueOf(vc).Elem().FieldByName(name)
	if !v.IsValid() || v.Kind() != reflect.String {
		return "", false
	}
	return v.String(), true
}

func setVoiceFieldString(vc *VoiceConfig, name, val string) bool {
	v := reflect.ValueOf(vc).Elem().FieldByName(name)
	if !v.IsValid() || v.Kind() != reflect.String {
		return false
	}
	v.SetString(val)
	return true
}

func TestVoiceTTSFields(t *testing.T) {
	vc := &VoiceConfig{}

	// Part 1: the four fields exist with exact dual tags.
	rt := reflect.TypeOf(*vc)
	for _, w := range voiceFieldContract {
		f, ok := rt.FieldByName(w.field)
		if !ok {
			t.Errorf("missing symbol VoiceConfig.%s (%s)", w.field, w.note)
			continue
		}
		if f.Type.Kind() != reflect.String {
			t.Errorf("VoiceConfig.%s is %s, want string", w.field, f.Type.Kind())
		}
		if got := f.Tag.Get("json"); got != w.jsonTag {
			t.Errorf("VoiceConfig.%s json tag = %q, want %q", w.field, got, w.jsonTag)
		}
		if got := f.Tag.Get("toml"); got != w.tomlTag {
			t.Errorf("VoiceConfig.%s toml tag = %q, want %q", w.field, got, w.tomlTag)
		}
	}

	// Part 2: json round-trip for whichever fields exist. A missing field
	// skips its round-trip check (already reported above).
	in := VoiceConfig{}
	setVoiceFieldString(&in, "STTModel", "nova-3")
	setVoiceFieldString(&in, "TTSModel", "Qwen/Qwen3-TTS-12Hz-0.6B-CustomVoice")
	setVoiceFieldString(&in, "TTSBaseURL", "http://127.0.0.1:8880")
	setVoiceFieldString(&in, "TTSVoice", "vivian")

	jb, jerr := json.Marshal(in)
	if jerr != nil {
		t.Fatalf("json.Marshal: %v", jerr)
	}
	var jout VoiceConfig
	if err := json.Unmarshal(jb, &jout); err != nil {
		t.Fatalf("json.Unmarshal: %v", err)
	}
	for _, w := range voiceFieldContract {
		want, setOK := voiceFieldString(&in, w.field)
		if !setOK {
			continue // missing symbol reported in part 1
		}
		got, gotOK := voiceFieldString(&jout, w.field)
		if !gotOK {
			t.Errorf("missing symbol VoiceConfig.%s: json round-trip impossible (%s)", w.field, w.note)
			continue
		}
		if got != want {
			t.Errorf("json round-trip %s = %q, want %q", w.field, got, want)
		}
		if !strings.Contains(string(jb), `"`+w.jsonTag+`":"`) {
			t.Errorf("marshaled json lacks key %q (tag contract)", w.jsonTag)
		}
	}
	if !strings.Contains(string(jb), `"tts_base_url":"http://127.0.0.1:8880"`) {
		t.Errorf("missing tag behavior: json lacks tts_base_url (leaf 01 field absent): %s", jb)
	}

	// Part 3: toml round-trip (pelletier/go-toml v2, the config loader).
	tb, terr := toml.Marshal(in)
	if terr != nil {
		t.Fatalf("toml.Marshal: %v", terr)
	}
	var tout VoiceConfig
	if err := toml.Unmarshal(tb, &tout); err != nil {
		t.Fatalf("toml.Unmarshal: %v", err)
	}
	for _, w := range voiceFieldContract {
		want, setOK := voiceFieldString(&in, w.field)
		if !setOK {
			continue
		}
		got, gotOK := voiceFieldString(&tout, w.field)
		if !gotOK {
			t.Errorf("missing symbol VoiceConfig.%s: toml round-trip impossible (%s)", w.field, w.note)
			continue
		}
		if got != want {
			t.Errorf("toml round-trip %s = %q, want %q", w.field, got, want)
		}
	}
	if !strings.Contains(string(tb), "tts_base_url") {
		t.Errorf("missing tag behavior: toml output lacks tts_base_url (leaf 01 field absent):\n%s", tb)
	}

	// Part 4: defaults "" (empty = vendor default, C4 semantics).
	var z VoiceConfig
	if got, ok := voiceFieldString(&z, "TTSBaseURL"); ok && got != "" {
		t.Errorf("zero-value TTSBaseURL = %q, want \"\" default", got)
	}
}

// TestVoiceTemplateDocumentsTTSFields pins leaf 01 T2: the shipped
// template documents the keys in [voice] (tts_base_url is the new key;
// the other three are expected present from the local-leg wave).
func TestVoiceTemplateDocumentsTTSFields(t *testing.T) {
	b, err := os.ReadFile(filepath.Join("..", "..", "config", "meept.json5"))
	if err != nil {
		t.Fatalf("read template: %v", err)
	}
	s := string(b)
	for _, key := range []string{"stt_model", "tts_model", "tts_voice"} {
		if !strings.Contains(s, `"`+key+`"`) {
			t.Errorf("template lacks voice.%s (expected present from local-leg wave)", key)
		}
	}
	if !strings.Contains(s, `"tts_base_url"`) {
		t.Error("missing template key voice.tts_base_url (leaf 01 T2: document the new field)")
	}
	if !strings.Contains(s, `"turn_confirm_ms"`) {
		t.Error("missing template key voice.turn_confirm_ms (startguard C1: document the aggregation window)")
	}
	if !strings.Contains(s, `"min_turn_words"`) {
		t.Error("missing template key voice.min_turn_words (voice-microfrag: document the short-final gate)")
	}
	for _, key := range []string{
		`"exempt_phrases"`, `"energy_gate"`, `"min_rms"`,
		`"fragment_decay_s"`, `"reply_exempt_s"`, `"backchannel_soften"`,
	} {
		if !strings.Contains(s, key) {
			t.Errorf("missing template key voice.%s (voice-peragent leaf 01: every gate knob documented)", key)
		}
	}
}

// TestVoiceAcousticDefaults (voice-peragent leaf 01): the shipped
// defaults pin. Every new gate knob is opt-in-DISABLED at the numeric
// false/0 default (docs/agents/opt-in-defaults.md) except the two
// documented nonzero values: reply_exempt_s 15 (a default that only
// binds once the gate is armed — inert at min_turn_words 0) and the
// populated exemption set (likewise inert). The exemption list is a
// fresh slice per DefaultConfig call (mutation-safety precedent:
// DefaultUnapprovedCallerAllowedTools).
func TestVoiceAcousticDefaults(t *testing.T) {
	c := DefaultConfig()
	v := c.Voice
	if v.MinTurnWords != 0 {
		t.Errorf("voice.min_turn_words default = %d, want 0 (gate off)", v.MinTurnWords)
	}
	if want := DefaultVoiceExemptPhrases(); len(v.ExemptPhrases) != len(want) {
		t.Fatalf("voice.exempt_phrases default = %v, want the shipped %d-item set", v.ExemptPhrases, len(want))
	}
	for _, ph := range []string{"stop", "cancel", "yes", "no", "thanks", "thank you",
		"okay", "ok", "mm-hm", "mmhm", "uh-huh", "go ahead"} {
		found := slices.Contains(v.ExemptPhrases, ph)
		if !found {
			t.Errorf("default exemption %q missing from %v", ph, v.ExemptPhrases)
		}
	}
	// Fresh-slice invariant: mutating one config never leaks into the next.
	c.Voice.ExemptPhrases[0] = "MUTATED"
	if DefaultConfig().Voice.ExemptPhrases[0] != "stop" {
		t.Error("DefaultVoiceExemptPhrases must return a fresh slice per call")
	}
	if v.EnergyGate != false {
		t.Errorf("voice.energy_gate default = %v, want false", v.EnergyGate)
	}
	if v.MinRMS != 0 {
		t.Errorf("voice.min_rms default = %d, want 0 (unused while the gate is off)", v.MinRMS)
	}
	if v.FragmentDecayS != 0 {
		t.Errorf("voice.fragment_decay_s default = %d, want 0 (off)", v.FragmentDecayS)
	}
	if v.ReplyExemptS != 15 {
		t.Errorf("voice.reply_exempt_s default = %d, want 15 (inert while min_turn_words is 0)", v.ReplyExemptS)
	}
	if v.BackchannelSoften != nil {
		t.Errorf("voice.backchannel_soften default = %v, want nil (tri-state: absent = off)", v.BackchannelSoften)
	}
}

// TestVoiceAcousticLoadSurvival: explicit values survive the JSON5
// load, negatives clamp to their disabled values at the boundary, and
// the shipped template parses with its documented gate keys.
func TestVoiceAcousticLoadSurvival(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "meept.json5")
	body := `{
  "voice": {
    "min_turn_words": 3,
    "exempt_phrases": ["pause"],
    "energy_gate": true,
    "min_rms": 1500,
    "fragment_decay_s": 30,
    "reply_exempt_s": 8,
    "backchannel_soften": true,
    "employee_leg": [
      { "employee": "tweak", "min_turn_words": 0, "energy_gate": false,
        "reply_exempt_s": 22, "barge_in_rms": 4200 },
    ],
  },
}`
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := LoadJSON5Config(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	v := c.Voice
	if v.MinTurnWords != 3 || !v.EnergyGate || v.MinRMS != 1500 ||
		v.FragmentDecayS != 30 || v.ReplyExemptS != 8 {
		t.Fatalf("explicit gate values lost on load: %+v", v)
	}
	if len(v.ExemptPhrases) != 1 || v.ExemptPhrases[0] != "pause" {
		t.Fatalf("exempt_phrases = %v, want the explicit list", v.ExemptPhrases)
	}
	if v.BackchannelSoften == nil || !*v.BackchannelSoften {
		t.Fatalf("backchannel_soften = %v, want explicit true", v.BackchannelSoften)
	}
	row := v.EmployeeLeg[0]
	if row.MinTurnWords == nil || *row.MinTurnWords != 0 {
		t.Fatal("row explicit 0 min_turn_words must survive as a pointer to 0")
	}
	if row.EnergyGate == nil || *row.EnergyGate {
		t.Fatal("row explicit false energy_gate must survive")
	}
	if row.ReplyExemptS == nil || *row.ReplyExemptS != 22 {
		t.Fatal("row reply_exempt_s must load")
	}
	if row.BargeInRms == nil || *row.BargeInRms != 4200 {
		t.Fatal("row barge_in_rms must load")
	}

	// Negative clamp at the load boundary (NormalizeVoiceAcousticDefaults).
	neg := filepath.Join(dir, "neg.json5")
	if err := os.WriteFile(neg, []byte(`{
  "voice": {
    "min_turn_words": -4,
    "min_rms": -10,
    "fragment_decay_s": -3,
    "reply_exempt_s": -9,
    "employee_leg": [{ "employee": "e", "min_turn_words": -2, "reply_exempt_s": -1 }],
  },
}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c2, err := LoadJSON5Config(neg)
	if err != nil {
		t.Fatalf("neg load: %v", err)
	}
	if c2.Voice.MinTurnWords != 0 || c2.Voice.MinRMS != 0 ||
		c2.Voice.FragmentDecayS != 0 || c2.Voice.ReplyExemptS != 0 {
		t.Fatalf("negatives must clamp to 0 (disabled), got %+v", c2.Voice)
	}
	r2 := c2.Voice.EmployeeLeg[0]
	if r2.MinTurnWords == nil || *r2.MinTurnWords != 0 || r2.ReplyExemptS == nil || *r2.ReplyExemptS != 0 {
		t.Fatal("row negative pointers must clamp to pointers-to-0")
	}

	// Template parses and reproduces the defaults.
	const templatePath = "../../config/meept.json5"
	abs, err := filepath.Abs(templatePath)
	if err != nil {
		t.Fatal(err)
	}
	tpl, err := LoadJSON5Config(abs)
	if err != nil {
		t.Fatalf("template parse: %v", err)
	}
	tv := tpl.Voice
	if tv.MinTurnWords != 0 || tv.EnergyGate || tv.MinRMS != 0 || tv.FragmentDecayS != 0 || tv.ReplyExemptS != 15 {
		t.Fatalf("template gate values must reproduce the shipped defaults, got %+v", tv)
	}
	if len(tv.ExemptPhrases) != 12 {
		t.Fatalf("template exempt_phrases = %v, want the 12-item shipped set", tv.ExemptPhrases)
	}
}

// TestNormalizeVoiceAcousticDefaultsIdempotent: the normalizer is a
// no-op on valid values (run twice, same result).
func TestNormalizeVoiceAcousticDefaultsIdempotent(t *testing.T) {
	v := DefaultConfig().Voice
	NormalizeVoiceAcousticDefaults(&v)
	before := v
	NormalizeVoiceAcousticDefaults(&v)
	if v.MinTurnWords != before.MinTurnWords || v.MinRMS != before.MinRMS ||
		v.ReplyExemptS != before.ReplyExemptS || v.FragmentDecayS != before.FragmentDecayS {
		t.Fatal("NormalizeVoiceAcousticDefaults must be idempotent on valid values")
	}
}

// TestNormalizeVoiceRumbleDefaults pins bughunt 2026-11-19 M18/H1: the
// documented 0.20-0.25 GPU-fraction safe range is ENFORCED at the config
// boundary (SpawnArgs used to render the value verbatim, so vLLM's unsafe
// 0.92 default could starve qwen3-tts on the shared RTX 3060).
func TestNormalizeVoiceRumbleDefaults(t *testing.T) {
	cases := []struct {
		name string
		in   float64
		want float64
	}{
		{"vllm_unsafe_default_clamps_to_max", 0.92, 0.25},
		{"above_range_clamps_to_max", 0.50, 0.25},
		{"just_above_max_clamps", 0.2500001, 0.25},
		{"zero_restores_frozen_default", 0, 0.20},
		{"negative_restores_frozen_default", -0.1, 0.20},
		{"in_range_low_passes", 0.20, 0.20},
		{"in_range_mid_passes", 0.22, 0.22},
		{"in_range_max_passes", 0.25, 0.25},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// An explicitly configured block: a non-GPU field is set so
			// the normalizer sees a configured block, not the zero value.
			r := VoiceRumbleRuntimeConfig{
				GPUMemoryUtilization: tc.in,
				MaxModelLen:          4096,
			}
			NormalizeVoiceRumbleDefaults(&r)
			if r.GPUMemoryUtilization != tc.want {
				t.Fatalf("gpu_memory_utilization %v -> %v, want %v", tc.in, r.GPUMemoryUtilization, tc.want)
			}
			if r.MaxModelLen != 4096 {
				t.Fatalf("normalizer touched an unrelated field: max_model_len = %d", r.MaxModelLen)
			}
		})
	}

	t.Run("nan_and_inf_restore_default", func(t *testing.T) {
		for _, bad := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
			r := VoiceRumbleRuntimeConfig{GPUMemoryUtilization: bad, MaxModelLen: 4096}
			NormalizeVoiceRumbleDefaults(&r)
			if r.GPUMemoryUtilization != RumbleGPUMemoryUtilizationDefault {
				t.Fatalf("gpu_memory_utilization %v -> %v, want the frozen default", bad, r.GPUMemoryUtilization)
			}
		}
	})

	t.Run("zero_value_block_keeps_frozen_defaults", func(t *testing.T) {
		// The file omits [voice.rumble_runtime] entirely: the zero-value
		// block must stay zero (the frozen defaults come from
		// DefaultConfig / DefaultVoiceRumbleRuntimeConfig, not from an
		// injection here).
		var r VoiceRumbleRuntimeConfig
		NormalizeVoiceRumbleDefaults(&r)
		if r != (VoiceRumbleRuntimeConfig{}) {
			t.Fatalf("zero-value block mutated: %+v", r)
		}
		// The DefaultConfig block is the frozen safe shape and passes
		// through untouched.
		def := DefaultConfig().Voice.RumbleRuntime
		before := def
		NormalizeVoiceRumbleDefaults(&def)
		if def != before {
			t.Fatalf("DefaultConfig rumble block mutated: %+v -> %+v", before, def)
		}
		if def.GPUMemoryUtilization != 0.20 || def.MaxModelLen != 4096 {
			t.Fatalf("frozen defaults drifted: %+v", def)
		}
	})

	t.Run("idempotent", func(t *testing.T) {
		r := VoiceRumbleRuntimeConfig{GPUMemoryUtilization: 0.92, MaxModelLen: 4096}
		NormalizeVoiceRumbleDefaults(&r)
		first := r
		NormalizeVoiceRumbleDefaults(&r)
		if r != first {
			t.Fatal("NormalizeVoiceRumbleDefaults must be idempotent")
		}
	})

	t.Run("nil_safe", func(t *testing.T) {
		NormalizeVoiceRumbleDefaults(nil) // must not panic
	})
}

// TestRumbleClampWiredAtDaemonBoundary pins that ValidateAll (the daemon's
// pre-start validation gate) runs the rumble clamp, so a config that
// skipped load-boundary normalization still renders safe spawn args.
func TestRumbleClampWiredAtDaemonBoundary(t *testing.T) {
	cfg := DefaultConfig()
	cfg.Voice.RumbleRuntime = VoiceRumbleRuntimeConfig{
		GPUMemoryUtilization: 0.92, // vLLM's unsafe default, explicitly configured
		MaxModelLen:          4096,
	}
	if err := cfg.ValidateAll(); err != nil {
		t.Fatalf("ValidateAll: %v", err)
	}
	if got := cfg.Voice.RumbleRuntime.GPUMemoryUtilization; got != 0.25 {
		t.Fatalf("ValidateAll left gpu_memory_utilization = %v, want the 0.25 clamp", got)
	}
}

// TestVoiceRumbleRuntimeConfigDefaults carries the surviving F5 gate
// (formerly daemon TestRumbleRuntimeConfigCap, deleted with the
// superseded spawn helpers): the ONE config source of truth pins the
// explicit safe GPU fraction, context cap, health path, and auto-stop
// default for operators running a legacy vLLM Rumble server manually.
func TestVoiceRumbleRuntimeConfigDefaults(t *testing.T) {
	cfg := DefaultVoiceRumbleRuntimeConfig()
	if cfg.GPUMemoryUtilization <= 0 {
		t.Fatal("rumble_runtime must set gpu_memory_utilization explicitly")
	}
	if cfg.GPUMemoryUtilization > RumbleGPUMemoryUtilizationMax {
		t.Fatalf("gpu_memory_utilization = %.2f, want <= %.2f (never 0.92)", cfg.GPUMemoryUtilization, RumbleGPUMemoryUtilizationMax)
	}
	if cfg.MaxModelLen <= 0 || cfg.MaxModelLen > 4096 {
		t.Fatalf("max_model_len = %d, want 1..4096", cfg.MaxModelLen)
	}
	if cfg.HealthPath != "/health" {
		t.Fatalf("health path = %q, want /health", cfg.HealthPath)
	}
	if !cfg.AutoStopOnExit {
		t.Fatal("rumble_runtime auto_stop_on_exit default must be true")
	}
	if cfg.EndpointKey != "rumble:"+RumbleLoopbackAddr {
		t.Fatalf("endpoint_key = %q, want rumble:%s", cfg.EndpointKey, RumbleLoopbackAddr)
	}
	// DefaultConfig ships the same frozen block.
	def := DefaultConfig().Voice.RumbleRuntime
	if def != cfg {
		t.Fatalf("DefaultConfig rumble block drifted: %+v vs %+v", def, cfg)
	}
}

// TestVoiceRumbleBaseURL pins the derived dial URL (bughunt H1): the
// address half of EndpointKey over http, with a frozen loopback fallback
// for a zero-value or malformed block. An explicit operator
// voice.stt_base_url always wins over this value (pinned daemon-side in
// TestResolveSTTBaseURL / TestVoiceKeysEmissionSTTBaseURL).
func TestVoiceRumbleBaseURL(t *testing.T) {
	cases := []struct {
		name string
		key  string
		want string
	}{
		{"frozen_default", "rumble:" + RumbleLoopbackAddr, "http://" + RumbleLoopbackAddr},
		{"empty_key_falls_back", "", "http://" + RumbleLoopbackAddr},
		{"custom_port", "rumble:127.0.0.1:8099", "http://127.0.0.1:8099"},
		{"custom_host", "rumble:192.168.1.10:8091", "http://192.168.1.10:8091"},
		{"malformed_no_port_falls_back", "rumble:localhost", "http://" + RumbleLoopbackAddr},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			r := VoiceRumbleRuntimeConfig{EndpointKey: tc.key}
			if got := r.BaseURL(); got != tc.want {
				t.Fatalf("BaseURL() with key %q = %q, want %q", tc.key, got, tc.want)
			}
		})
	}
	// A fully zero block (config omits rumble_runtime) derives the frozen
	// default too - the derivation must never yield an empty URL for a
	// rumble vendor (the child fails construction on an empty BaseURL).
	var zero VoiceRumbleRuntimeConfig
	if got := zero.BaseURL(); got != "http://"+RumbleLoopbackAddr {
		t.Fatalf("zero block BaseURL() = %q, want the frozen loopback default", got)
	}
}
