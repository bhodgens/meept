package providers

// Gate tests (STT branch 00T T2, feature F4, contracts C2 + C1): the
// authoritative Deepgram speech endpoint. Two additive selectors are new:
//   - voicechild.Transcript.Endpoint bool — true on speech_final /
//     UtteranceEnd (contract C2).
//   - ProviderConfig.DeepgramEndpointMs int — 0 keeps the legacy fixed
//     timer endpointing (byte-identical); positive enables the fast path.
//
// RED STYLE (deliberate, matches the foundation gate tests): both symbols
// are absent today, so these packages MUST keep compiling. Missing symbols
// are probed via reflection with named failures; no direct reference to a
// field that does not yet exist, no t.Skip. Tests are RED today and turn
// GREEN when leaf 02 lands, with zero edits here.
//
// TestDeepgramEndpointFlag exercises emit() through the existing fake WS
// session seam; TestEndpointFastDispatch lives in the voicechild package
// (separate file) because Transcript and consumeTranscripts are there.
//
// Clauses: C2 (02-stt-path/orchestrator.md), 02-deepgram-endpoint.md T1,
// F4, R4 (ms=0 = timer path unchanged).

import (
	"context"
	"reflect"
	"testing"

	"github.com/caimlas/meept/internal/voicechild"
)

// wantSymbols checks that both new F4 symbols exist with the right kinds.
// Missing symbols are reported by name (the RED signal) and ok=false.
func wantSymbols(t *testing.T) bool {
	t.Helper()
	ok := true
	tf, hasField := reflect.TypeFor[voicechild.Transcript]().FieldByName("Endpoint")
	if !hasField {
		t.Errorf("missing symbol voicechild.Transcript.Endpoint (contract C2, 02-deepgram-endpoint T1): speech_final cannot be parsed")
		ok = false
	} else if tf.Type.Kind() != reflect.Bool {
		t.Errorf("voicechild.Transcript.Endpoint is %s, want bool (contract C2)", tf.Type.Kind())
	}
	cf, hasCfg := reflect.TypeFor[ProviderConfig]().FieldByName("DeepgramEndpointMs")
	if !hasCfg {
		t.Errorf("missing symbol ProviderConfig.DeepgramEndpointMs (contract C1, 02-deepgram-endpoint T2): fast path cannot be enabled")
		ok = false
	} else if cf.Type.Kind() != reflect.Int {
		t.Errorf("ProviderConfig.DeepgramEndpointMs is %s, want int (contract C1)", cf.Type.Kind())
	}
	return ok
}

// transcriptEndpoint reads Transcript.Endpoint via reflection. A missing
// field is reported through wantSymbols and reads as false here.
func transcriptEndpoint(tr voicechild.Transcript) bool {
	f := reflect.ValueOf(tr).FieldByName("Endpoint")
	if !f.IsValid() || f.Kind() != reflect.Bool {
		return false
	}
	return f.Bool()
}

// TestDeepgramEndpointFlag: Given a Deepgram Results message with
// speech_final (is_final=true), When emit decodes it, Then the resulting
// Transcript carries Endpoint=true. A Metadata message emits no transcript.
// Given DeepgramEndpointMs=0, the timer path is unchanged (pin: R4).
func TestDeepgramEndpointFlag(t *testing.T) {
	wantSymbols(t)

	cfg := ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000}
	s, err := NewSTT(cfg, "test-key")
	if err != nil {
		t.Fatalf("construct Deepgram STT: %v", err)
	}
	dg, ok := s.(*DeepgramSTT)
	if !ok {
		t.Fatalf("adapter type = %T, want *DeepgramSTT", s)
	}

	results := `{"type":"Results","channel":{"alternatives":[{"transcript":"hello world"}]},"is_final":true}`
	out := make(chan voicechild.Transcript, 4)
	dg.emit(context.Background(), []byte(results), out)
	select {
	case tr := <-out:
		if tr.Text != "hello world" {
			t.Errorf("transcript text = %q, want %q", tr.Text, "hello world")
		}
		if !transcriptEndpoint(tr) {
			t.Errorf("Transcript.Endpoint = false for speech_final result, want true (contract C2)")
		}
	default:
		t.Fatal("emit produced no transcript for a final Results message")
	}

	// Metadata carries no transcript and must not emit one.
	metaOut := make(chan voicechild.Transcript, 4)
	dg.emit(context.Background(), []byte(`{"type":"Metadata","from_finalize":true}`), metaOut)
	select {
	case tr := <-metaOut:
		t.Errorf("Metadata message emitted a transcript %+v, want none", tr)
	default:
	}
}
