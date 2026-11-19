package config

// Gate tests (foundation 00T, contract C2, 01-keys-contract.md T1): the
// seven new opt-in VoiceConfig fields exist with exact dual json+toml tags
// and land on their locked legacy defaults in DefaultConfig.
//
// RED style: reflection-gated so the config package keeps compiling and its
// existing suite stays runnable while the fields are absent. Every missing
// field is named in the failure; no t.Skip, no direct reference to a
// not-yet-existing field. The test is RED today and turns GREEN when the
// fields land, with zero edits to this file.
//
// Clause ids: C2 (master.md), 01-keys-contract.md T1, R4 (zero defaults).
// Locked defaults: all seven fields zero/empty in DefaultConfig().Voice.

import (
	"reflect"
	"testing"
)

// c2VoiceFields pins the exact field name/type/key of every new VoiceConfig
// field (contract C2). json and toml keys are identical per field.
var c2VoiceFields = []struct {
	field string
	key   string
	kind  reflect.Kind // reflect.Int for scalars, reflect.Slice for the vocab
}{
	{"SmartTurnMode", "smart_turn_mode", reflect.Int},
	{"ContextWarmWindowMs", "context_warm_window_ms", reflect.Int},
	{"WhisperServerAddr", "whisper_server_addr", reflect.String},
	{"DeepgramEndpointMs", "deepgram_endpoint_ms", reflect.Int},
	{"RecognitionVocabulary", "recognition_vocabulary", reflect.Slice},
	{"Qwen3ChunkSize", "qwen3_chunk_size", reflect.Int},
	{"ClipPrewarmDelayMs", "clip_prewarm_delay_ms", reflect.Int},
}

// TestVoiceConfigSmartTurnDefault: Given DefaultConfig(), When Voice is
// read, Then SmartTurnMode=0 and every new field is at its zero/empty
// default (R4: opt-in, byte-identical legacy boot).
func TestVoiceConfigSmartTurnDefault(t *testing.T) {
	rt := reflect.TypeFor[VoiceConfig]()

	// Part 1: every C2 field exists with the exact type and dual tags.
	for _, w := range c2VoiceFields {
		f, ok := rt.FieldByName(w.field)
		if !ok {
			t.Errorf("missing symbol VoiceConfig.%s (contract C2, 01-keys-contract T1)", w.field)
			continue
		}
		if f.Type.Kind() != w.kind {
			t.Errorf("VoiceConfig.%s is %s, want %s (contract C2)", w.field, f.Type.Kind(), w.kind)
		}
		if got := f.Tag.Get("json"); got != w.key {
			t.Errorf("VoiceConfig.%s json tag = %q, want %q", w.field, got, w.key)
		}
		if got := f.Tag.Get("toml"); got != w.key {
			t.Errorf("VoiceConfig.%s toml tag = %q, want %q", w.field, got, w.key)
		}
	}

	// Part 2: the shipped defaults are all zero/empty.
	voice := reflect.ValueOf(DefaultConfig()).Elem().FieldByName("Voice")
	if !voice.IsValid() {
		t.Fatal("Config has no Voice field")
	}
	for _, w := range c2VoiceFields {
		v := voice.FieldByName(w.field)
		if !v.IsValid() {
			t.Errorf("missing symbol VoiceConfig.%s at default read (contract C2)", w.field)
			continue
		}
		var zero bool
		switch w.kind {
		case reflect.Int:
			zero = v.Int() == 0
		case reflect.String:
			zero = v.String() == ""
		case reflect.Slice:
			zero = v.IsNil() || v.Len() == 0
		}
		if !zero {
			t.Errorf("DefaultConfig().Voice.%s = %v, want zero/empty (R4 legacy default)", w.field, v.Interface())
		}
	}
}
