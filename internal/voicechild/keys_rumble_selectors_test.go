package voicechild

// Gate test (leaf 00T, feature F5, contract C4): the additive Rumble
// keys-frame selectors (chat_template_* and stt_partial_ms) must ride the
// daemon->child C1 keys line and land on both keysWire (decode) and
// KeysFrame (carry-forward). Missing fields are probed via reflection so
// this package keeps compiling before leaf 01 lands them; each absent
// symbol is named in the failure. Absent-on-wire must decode to zero/nil.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

type rumbleSelector struct {
	field   string
	wireKey string
	kind    reflect.Kind // element kind for pointer fields
	ptr     bool
}

var rumbleSelectors = []rumbleSelector{
	{"ChatTemplateLanguage", "chat_template_language", reflect.String, false},
	{"ChatTemplateDigits", "chat_template_digits", reflect.Bool, true},
	{"ChatTemplateFiller", "chat_template_filler", reflect.Bool, true},
	{"ChatTemplateVerbatim", "chat_template_verbatim", reflect.Bool, true},
	{"STTPartialMs", "stt_partial_ms", reflect.Int, true},
}

// readSelector reads one selector field off a decoded keysWire via
// reflection. ok=false means the symbol is absent or the wrong type; a nil
// pointer field reads as a nil `any` (the ABSENT-on-wire case).
func readSelector(v reflect.Value, s rumbleSelector) (any, bool) {
	f := v.FieldByName(s.field)
	if !f.IsValid() {
		return nil, false
	}
	if s.ptr {
		if f.Kind() != reflect.Ptr {
			return nil, false
		}
		if f.IsNil() {
			return nil, true
		}
		f = f.Elem()
	}
	if f.Kind() != s.kind {
		return nil, false
	}
	switch s.kind {
	case reflect.String:
		return f.String(), true
	case reflect.Bool:
		return f.Bool(), true
	case reflect.Int:
		return int(f.Int()), true
	}
	return nil, false
}

func TestKeysFrameRumbleSelectors(t *testing.T) {
	wireT := reflect.TypeFor[keysWire]()
	frameT := reflect.TypeFor[KeysFrame]()

	// 1. Both structs carry every selector with the right type; keysWire
	//    (the decode target of handleKeysFrame) carries the exact wire tag.
	for _, s := range rumbleSelectors {
		for _, side := range []struct {
			rt      reflect.Type
			label   string
			wantTag bool
		}{{wireT, "keysWire", true}, {frameT, "KeysFrame", false}} {
			f, ok := side.rt.FieldByName(s.field)
			if !ok {
				t.Errorf("missing symbol %s.%s (contract C4 F5)", side.label, s.field)
				continue
			}
			typ := f.Type
			if s.ptr {
				if typ.Kind() != reflect.Ptr || typ.Elem().Kind() != s.kind {
					t.Errorf("%s.%s is %s, want *%s (contract C4)", side.label, s.field, typ, s.kind)
				}
			} else if typ.Kind() != s.kind {
				t.Errorf("%s.%s is %s, want %s (contract C4)", side.label, s.field, typ, s.kind)
			}
			if side.wantTag {
				got := strings.Split(f.Tag.Get("json"), ",")[0]
				if got != s.wireKey {
					t.Errorf("keysWire.%s json tag = %q, want %q (contract C4 wire shape)", s.field, got, s.wireKey)
				}
			}
		}
	}
	if t.Failed() {
		t.Fatalf("C4 selector fields missing - cannot gate the keys line")
	}

	// 2. Explicit line decodes into keysWire (handleKeysFrame's decode path)
	//    with the exact selector values.
	const line = `{"call_id":"kx-rumble-live","event":"keys","stt_key":"s","tts_key":"t",` +
		`"chat_template_language":"English","chat_template_digits":false,` +
		`"chat_template_filler":true,"chat_template_verbatim":false,"stt_partial_ms":250}`
	wv := reflect.New(reflect.TypeFor[keysWire]())
	if err := json.Unmarshal([]byte(line), wv.Interface()); err != nil {
		t.Fatalf("decode explicit keys line: %v", err)
	}
	want := map[string]any{
		"ChatTemplateLanguage": "English",
		"ChatTemplateDigits":   false,
		"ChatTemplateFiller":   true,
		"ChatTemplateVerbatim": false,
		"STTPartialMs":         250,
	}
	for _, s := range rumbleSelectors {
		got, ok := readSelector(wv.Elem(), s)
		if !ok {
			t.Errorf("cannot read decoded keysWire.%s", s.field)
			continue
		}
		if got != want[s.field] {
			t.Errorf("decoded keysWire.%s = %v, want %v (contract C4)", s.field, got, want[s.field])
		}
	}

	// 3. Absent-on-wire (legacy daemon line) = zero/nil for every selector.
	const legacy = `{"call_id":"kx-rumble-legacy","event":"keys","stt_key":"s","tts_key":"t"}`
	lv := reflect.New(reflect.TypeFor[keysWire]())
	if err := json.Unmarshal([]byte(legacy), lv.Interface()); err != nil {
		t.Fatalf("decode legacy keys line: %v", err)
	}
	for _, s := range rumbleSelectors {
		got, ok := readSelector(lv.Elem(), s)
		if !ok {
			t.Errorf("cannot read legacy keysWire.%s", s.field)
			continue
		}
		if s.ptr {
			if got != nil {
				t.Errorf("legacy keysWire.%s = %v, want nil (absent-on-wire)", s.field, got)
			}
		} else if got != "" {
			t.Errorf("legacy keysWire.%s = %v, want empty string (absent-on-wire)", s.field, got)
		}
	}

	// 4. The live decode path accepts the line without error.
	_, child, _, _ := newKeysTestPipeline(t, "kx-rumble-live")
	if err := child.handleKeysFrame(line); err != nil {
		t.Fatalf("handleKeysFrame with selectors: %v", err)
	}
}
