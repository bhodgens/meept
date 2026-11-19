//go:build e2e

// Root contract gate - task T2 (root C2, audit R4): legacy config defaults
// are unchanged and every new additive field defaults to its zero value.
package voicettsstt

import (
	"reflect"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/config"
)

// c2Fields is the frozen C2 additive config block (master C2 +
// 01-foundation/01-keys-contract.md). Each row is (Go field name, json name,
// expected reflect.Kind). All default to the zero value.
var c2Fields = []struct {
	field string
	wire  string
	kind  reflect.Kind
}{
	{"SmartTurnMode", "smart_turn_mode", reflect.Int},
	{"WhisperServerAddr", "whisper_server_addr", reflect.String},
	{"DeepgramEndpointMs", "deepgram_endpoint_ms", reflect.Int},
	{"RecognitionVocabulary", "recognition_vocabulary", reflect.Slice},
	{"Qwen3ChunkSize", "qwen3_chunk_size", reflect.Int},
	{"ClipPrewarmDelayMs", "clip_prewarm_delay_ms", reflect.Int},
	{"ContextWarmWindowMs", "context_warm_window_ms", reflect.Int},
}

// assertZero reads the field through reflect's typed getters and fails
// unless it holds the zero value for its kind.
func assertZero(t *testing.T, name string, fv reflect.Value, kind reflect.Kind) {
	t.Helper()
	switch kind {
	case reflect.Int:
		if got := fv.Int(); got != 0 {
			t.Errorf("C2: VoiceConfig.%s default = %d; want 0 (opt-in / R4 legacy default)", name, got)
		}
	case reflect.String:
		if got := fv.String(); got != "" {
			t.Errorf("C2: VoiceConfig.%s default = %q; want empty (opt-in / R4 legacy default)", name, got)
		}
	case reflect.Slice:
		if !fv.IsNil() && fv.Len() != 0 {
			t.Errorf("C2: VoiceConfig.%s default = non-empty slice (len %d); want nil/empty (opt-in / R4 legacy default)", name, fv.Len())
		}
	default:
		if !fv.IsZero() {
			t.Errorf("C2: VoiceConfig.%s default = %v; want zero value", name, fv.Interface())
		}
	}
}

// TestVoiceConfigLegacyDefaults - task T2.
// Given: config.DefaultConfig().
// When: the Voice block is read.
// Then: TurnConfirmMs=900, FragmentConfirmMs=3000, and every new C2 field
// is its zero value with the exact frozen name/type/json tag.
func TestVoiceConfigLegacyDefaults(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg == nil {
		t.Fatal("C2: config.DefaultConfig() returned nil")
	}
	v := cfg.Voice

	// Legacy defaults (present today - direct assertions are safe).
	if v.TurnConfirmMs != 900 {
		t.Errorf("C2: VoiceConfig.TurnConfirmMs = %d; want legacy default 900", v.TurnConfirmMs)
	}
	if v.FragmentConfirmMs != 3000 {
		t.Errorf("C2: VoiceConfig.FragmentConfirmMs = %d; want legacy default 3000", v.FragmentConfirmMs)
	}

	// New additive fields: reflection lookups so the package compiles today
	// and reports missing-implementation REDs instead of syntax errors.
	rv := reflect.ValueOf(v)
	rt := rv.Type()
	for _, f := range c2Fields {
		fv := rv.FieldByName(f.field)
		if !fv.IsValid() {
			t.Errorf("C2: VoiceConfig is missing field %q (json %q): missing implementation", f.field, f.wire)
			continue
		}
		if fv.Kind() != f.kind {
			t.Errorf("C2: VoiceConfig.%s kind = %s; want %s", f.field, fv.Kind(), f.kind)
			continue
		}
		sf, _ := rt.FieldByName(f.field)
		name := strings.Split(sf.Tag.Get("json"), ",")[0]
		if name != f.wire {
			t.Errorf("C2: VoiceConfig.%s json name = %q; want %q", f.field, name, f.wire)
		}
		assertZero(t, f.field, fv, f.kind)
	}
}
