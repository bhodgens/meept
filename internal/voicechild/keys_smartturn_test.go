package voicechild

// Gate tests (foundation 00T, feature F1, contract C1): the extended C1
// keys line carries two new additive selectors -- SmartTurnMode and
// ContextWarmWindowMs -- that decode on the child with legacy defaults.
//
// RED style (deliberate, matches keys_frame_extended_test.go): the contract
// symbols do not exist yet, so this package MUST keep compiling for the
// sibling suites. Every missing symbol is probed through reflection (types
// that exist today) and named in the failure; no direct reference to a
// not-yet-existing field, no t.Skip. The test is RED today because the
// symbols are absent; it turns GREEN the moment the fields land, with zero
// edits to this file.
//
// Clause ids: C1 (master.md), 01-keys-contract.md T2, R4 (legacy defaults).
// Locked decisions: SmartTurnMode int 0=default/1=kill-switch/2=consume;
// ContextWarmWindowMs int 0=legacy.

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// c1IntFields are the two C1 selector fields this foundation leaf gates:
// Go field name on KeysFrame/keysWire -> exact JSON wire key.
var c1IntFields = []struct {
	field   string
	wireKey string
}{
	{"SmartTurnMode", "smart_turn_mode"},
	{"ContextWarmWindowMs", "context_warm_window_ms"},
}

// intValueOf reads an int-typed field (int or *int) through reflection.
// exists=false means the field is absent on the type; a nil *int is the
// ABSENT-on-wire case and reads as the zero default (exists stays true).
func intValueOf(v reflect.Value) (int, bool) {
	if !v.IsValid() {
		return 0, false
	}
	if v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return 0, true
		}
		v = v.Elem()
	}
	switch v.Kind() {
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return int(v.Int()), true
	default:
		return 0, false
	}
}

// requireIntContract checks that a struct type carries each contract field
// with int (or *int) kind and the exact JSON key (omitempty suffix allowed).
func requireIntContract(t *testing.T, rt reflect.Type, label string, wantTag bool) {
	t.Helper()
	for _, c := range c1IntFields {
		f, ok := rt.FieldByName(c.field)
		if !ok {
			t.Errorf("missing symbol %s.%s (contract C1, 01-keys-contract T2): extended keys line cannot carry %s", label, c.field, c.wireKey)
			continue
		}
		ft := f.Type
		if ft.Kind() == reflect.Ptr {
			ft = ft.Elem()
		}
		if ft.Kind() != reflect.Int {
			t.Errorf("%s.%s is %s, want int (contract C1)", label, c.field, f.Type)
		}
		if wantTag {
			got := strings.Split(f.Tag.Get("json"), ",")[0]
			if got != c.wireKey {
				t.Errorf("%s.%s json tag = %q, want %q (contract C1 wire shape)", label, c.field, got, c.wireKey)
			}
		}
	}
}

// decodeIntFields unmarshals one raw keys line into a fresh keysWire via
// reflection and returns each contract field's effective int value. A
// missing field is reported by name and ok=false.
func decodeIntFields(t *testing.T, line string) (map[string]int, bool) {
	t.Helper()
	wv := reflect.New(reflect.TypeFor[keysWire]())
	if err := json.Unmarshal([]byte(line), wv.Interface()); err != nil {
		t.Fatalf("decode keys line: %v", err)
	}
	e := wv.Elem()
	out := make(map[string]int, len(c1IntFields))
	ok := true
	for _, c := range c1IntFields {
		v, present := intValueOf(e.FieldByName(c.field))
		if !present {
			t.Errorf("missing symbol keysWire.%s (contract C1): cannot decode %s from keys line", c.field, c.wireKey)
			ok = false
			continue
		}
		out[c.field] = v
	}
	return out, ok
}

// TestKeysFrameSmartTurnModeDefault: Given a legacy keys line (no new
// selectors), When decoded, Then SmartTurnMode=0 and ContextWarmWindowMs=0
// (R4: legacy wire reproduces today's behavior byte-identically).
func TestKeysFrameSmartTurnModeDefault(t *testing.T) {
	requireIntContract(t, reflect.TypeFor[KeysFrame](), "KeysFrame", false)
	requireIntContract(t, reflect.TypeFor[keysWire](), "keysWire", true)

	// The exact wire shape an old daemon emits (foundation 01-keys-contract
	// T2: absent selectors = legacy defaults).
	const legacyLine = `{"call_id":"kd1","event":"keys","stt_key":"s","tts_key":"t"}`
	got, ok := decodeIntFields(t, legacyLine)
	if !ok {
		t.Fatalf("legacy keys line cannot be decoded with the C1 selector contract")
	}
	for _, c := range c1IntFields {
		if got[c.field] != 0 {
			t.Errorf("legacy decode %s = %d, want 0 (R4 legacy default)", c.field, got[c.field])
		}
	}
}

// TestKeysFrameSmartTurnModeExplicit: Given an explicit mode=2 and a
// context-warm window of 1234 ms, When decoded, Then both values land on
// the KeysFrame decode seam unchanged.
func TestKeysFrameSmartTurnModeExplicit(t *testing.T) {
	requireIntContract(t, reflect.TypeFor[KeysFrame](), "KeysFrame", false)
	requireIntContract(t, reflect.TypeFor[keysWire](), "keysWire", true)

	const line = `{"call_id":"kd2","event":"keys","stt_key":"s","tts_key":"t",` +
		`"smart_turn_mode":2,"context_warm_window_ms":1234}`
	got, ok := decodeIntFields(t, line)
	if !ok {
		t.Fatalf("explicit keys line cannot be decoded with the C1 selector contract")
	}
	if got["SmartTurnMode"] != 2 {
		t.Errorf("decoded SmartTurnMode = %d, want 2 (explicit mode=2)", got["SmartTurnMode"])
	}
	if got["ContextWarmWindowMs"] != 1234 {
		t.Errorf("decoded ContextWarmWindowMs = %d, want 1234", got["ContextWarmWindowMs"])
	}
}
