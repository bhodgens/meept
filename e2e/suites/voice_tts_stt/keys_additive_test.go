//go:build e2e

// Package voicettsstt is the root contract e2e gate (leaf 00T-root-e2e.md)
// for the voice TTS/STT optimization campaign. It pins the additive-wire
// contract (master C1/C2, audit refs R1/R2/R4): every new C1 keys selector
// and C2 VoiceConfig field is opt-in, and an unset knob leaves the legacy
// wire byte-identical.
//
// RED today: none of the new selector fields exist yet. The assertions look
// each field up through reflection (a missing field is a genuine
// missing-implementation RED, never a compile error that breaks the whole
// package's test build), then pin the real wire behavior: a zero-value frame
// must serialize with NONE of the new keys (R4 byte-identical-when-unset).
package voicettsstt

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/voicechild"
)

// c1Selectors is the frozen C1 selector block (master C1 +
// 01-foundation/01-keys-contract.md). Each row is (Go field name, wire json
// name, expected reflect.Kind). All six are `omitempty` on the wire.
var c1Selectors = []struct {
	field string
	wire  string
	kind  reflect.Kind
}{
	{"SmartTurnMode", "smart_turn_mode", reflect.Int},
	{"WhisperServerAddr", "whisper_server_addr", reflect.String},
	{"DeepgramEndpointMs", "deepgram_endpoint_ms", reflect.Int},
	{"Vocabulary", "vocabulary", reflect.Slice},
	{"Qwen3ChunkSize", "qwen3_chunk_size", reflect.Int},
	{"ClipPrewarmDelayMs", "clip_prewarm_delay_ms", reflect.Int},
}

func hasOmitEmpty(parts []string) bool {
	for _, p := range parts {
		if p == "omitempty" {
			return true
		}
	}
	return false
}

// TestKeysFrameLegacyWireHasNoNewSelectors - task T1 (root C1, R4).
// Given: a zero-value C1 KeysFrame (every new selector unset).
// When: the frame is serialized for the C1 wire.
// Then: none of smart_turn_mode / whisper_server_addr /
// deepgram_endpoint_ms / vocabulary / qwen3_chunk_size /
// clip_prewarm_delay_ms appears in the raw line, and every new selector
// exists on the frame with the exact name/type and an omitempty tag.
func TestKeysFrameLegacyWireHasNoNewSelectors(t *testing.T) {
	kfType := reflect.TypeOf(voicechild.KeysFrame{})

	// Contract: each new selector must exist with the frozen Go name/type
	// and an `omitempty` json tag. Today the fields are absent, so each
	// lookup reports a missing-implementation RED (no direct reference to a
	// not-yet-existing field, which would be a compile error).
	for _, sel := range c1Selectors {
		sf, ok := kfType.FieldByName(sel.field)
		if !ok {
			t.Errorf("C1: KeysFrame is missing selector field %q (wire %q): missing implementation", sel.field, sel.wire)
			continue
		}
		if sf.Type.Kind() != sel.kind {
			t.Errorf("C1: KeysFrame.%s kind = %s; want %s", sel.field, sf.Type.Kind(), sel.kind)
		}
		tag, hasTag := sf.Tag.Lookup("json")
		if !hasTag {
			t.Errorf("C1: KeysFrame.%s has no json tag; want %q with omitempty", sel.field, sel.wire)
			continue
		}
		parts := strings.Split(tag, ",")
		if parts[0] != sel.wire || !hasOmitEmpty(parts[1:]) {
			t.Errorf("C1: KeysFrame.%s json tag = %q; want name %q with omitempty (legacy wire must stay byte-identical)", sel.field, tag, sel.wire)
		}
	}

	// Behavioral pin: a zero-value frame must serialize with NONE of the
	// new wire keys. This holds today (no fields) and must keep holding once
	// the fields land with omitempty - that is the R4 invariant.
	raw, err := json.Marshal(voicechild.KeysFrame{})
	if err != nil {
		t.Fatalf("C1: marshal zero KeysFrame: %v", err)
	}
	var wire map[string]json.RawMessage
	if err := json.Unmarshal(raw, &wire); err != nil {
		t.Fatalf("C1: unmarshal keys frame: %v", err)
	}
	for _, sel := range c1Selectors {
		if _, present := wire[sel.wire]; present {
			t.Errorf("C1: zero-value KeysFrame serialized new key %q; unset selectors must be omitted (R4 byte-identical wire)", sel.wire)
		}
	}
}
