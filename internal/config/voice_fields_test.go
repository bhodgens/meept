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
	"os"
	"path/filepath"
	"reflect"
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
}
