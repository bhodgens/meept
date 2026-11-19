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
	"strings"
	"testing"
	"time"

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

// TestDeepgramEndpointFlag: contract C2 — Transcript.Endpoint is true
// ONLY on an authoritative speech end: a speech_final Results message or
// a latched UtteranceEnd. A plain is_final segment is a finalized
// SEGMENT that may precede more speech: Final=true but Endpoint=false
// (bughunt 2026-11-19 H4 corrected this gate — the old assertion pinned
// Endpoint=true for is_final-only, which was the contract violation).
// A Metadata message emits no transcript. Given DeepgramEndpointMs=0,
// the timer path is unchanged (pin: R4).
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

	// Plain is_final segment: Final=true, Endpoint=FALSE (contract C2).
	results := `{"type":"Results","channel":{"alternatives":[{"transcript":"hello world"}]},"is_final":true}`
	out := make(chan voicechild.Transcript, 4)
	dg.emit(context.Background(), []byte(results), out)
	select {
	case tr := <-out:
		if tr.Text != "hello world" {
			t.Errorf("transcript text = %q, want %q", tr.Text, "hello world")
		}
		if !tr.Final {
			t.Errorf("Transcript.Final = false for is_final segment, want true")
		}
		if transcriptEndpoint(tr) {
			t.Errorf("Transcript.Endpoint = true for is_final-only segment, want false (contract C2: endpoint only on speech_final/UtteranceEnd)")
		}
	default:
		t.Fatal("emit produced no transcript for a final Results message")
	}

	// speech_final Results: Final=true AND Endpoint=true.
	sf := `{"type":"Results","channel":{"alternatives":[{"transcript":"that is all"}]},"is_final":true,"speech_final":true}`
	dg.emit(context.Background(), []byte(sf), out)
	select {
	case tr := <-out:
		if !tr.Final || !transcriptEndpoint(tr) {
			t.Errorf("speech_final transcript = %+v, want Final=true Endpoint=true (contract C2)", tr)
		}
	default:
		t.Fatal("emit produced no transcript for a speech_final Results message")
	}

	// UtteranceEnd latches onto the NEXT result (even an interim one):
	// the marker itself emits nothing, the decorated result is an
	// authoritative endpoint.
	dg.emit(context.Background(), []byte(`{"type":"UtteranceEnd","last_word_end":1.5}`), out)
	select {
	case tr := <-out:
		t.Fatalf("UtteranceEnd marker emitted a transcript %+v, want none", tr)
	default:
	}
	interim := `{"type":"Results","channel":{"alternatives":[{"transcript":"next utterance tail"}]},"is_final":false}`
	dg.emit(context.Background(), []byte(interim), out)
	select {
	case tr := <-out:
		if !tr.Final || !transcriptEndpoint(tr) {
			t.Errorf("UtteranceEnd-latched transcript = %+v, want Final=true Endpoint=true (contract C2)", tr)
		}
	default:
		t.Fatal("emit produced no transcript after a latched UtteranceEnd")
	}
	// The latch is consumed: a following plain interim stays non-endpoint.
	dg.emit(context.Background(), []byte(`{"type":"Results","channel":{"alternatives":[{"transcript":"more speech"}]},"is_final":false}`), out)
	select {
	case tr := <-out:
		if tr.Final || transcriptEndpoint(tr) {
			t.Errorf("post-latch interim = %+v, want Final=false Endpoint=false (latch consumed once)", tr)
		}
	default:
		t.Fatal("emit produced no transcript for the post-latch interim")
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

// TestDeepgramUtteranceEndQuery pins the wire half of F4 (bughunt H4):
// DeepgramEndpointMs>0 must send utterance_end_ms so Deepgram actually
// emits UtteranceEnd (without it the latch lane is dead); 0 keeps the
// query byte-identical to legacy (R4).
func TestDeepgramUtteranceEndQuery(t *testing.T) {
	legacy := &DeepgramSTT{cfg: ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000}}
	if path := legacy.listenPath(); strings.Contains(path, "utterance_end_ms") {
		t.Errorf("legacy listenPath carries utterance_end_ms: %s (R4: knob=0 must be byte-identical)", path)
	}
	armed := &DeepgramSTT{cfg: ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000, DeepgramEndpointMs: 1200}}
	path := armed.listenPath()
	if !strings.Contains(path, "utterance_end_ms=1200") {
		t.Errorf("listenPath with DeepgramEndpointMs=1200 = %s, want utterance_end_ms=1200 (the knob's value must reach the wire)", path)
	}
}

// TestDeepgramReconnectClearsEndpointLatch pins the reconnect half of
// bughunt H4: a UtteranceEnd latched on a DROPPED session must not
// survive the one-retry reconnect and promote the next utterance's
// first interim to a final/endpoint.
func TestDeepgramReconnectClearsEndpointLatch(t *testing.T) {
	s := &DeepgramSTT{
		cfg:    ProviderConfig{Vendor: "deepgram", Model: "nova-3", SampleRate: 16000},
		apiKey: "test-key",
	}
	// Session 1: deliver a UtteranceEnd (latches), then drop.
	// Session 2: deliver a plain interim, then end the stream.
	first := &scriptedWSSession{msgs: [][]byte{
		[]byte(`{"type":"UtteranceEnd","last_word_end":1.0}`),
	}}
	second := &scriptedWSSession{msgs: [][]byte{
		[]byte(`{"type":"Results","channel":{"alternatives":[{"transcript":"fresh start"}]},"is_final":false}`),
	}}
	sessions := []wsSession{first, second}
	dials := 0
	s.setSTTDialer(func(ctx context.Context, base, path string, header wsHeader) (wsSession, error) {
		if dials >= len(sessions) {
			return nil, context.Canceled
		}
		sess := sessions[dials]
		dials++
		return sess, nil
	})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pcm := make(chan []byte) // stays open: no Finalize flush interferes
	out, err := s.Stream(ctx, pcm)
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	select {
	case tr, ok := <-out:
		if !ok {
			t.Fatal("stream closed before the post-reconnect interim arrived")
		}
		if tr.Text != "fresh start" {
			t.Fatalf("transcript = %q, want %q", tr.Text, "fresh start")
		}
		if tr.Final || tr.Endpoint {
			t.Fatalf("post-reconnect interim = %+v, want Final=false Endpoint=false (stale UtteranceEnd latch survived the reconnect)", tr)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no transcript after reconnect")
	}
	cancel()
	for range out {
	}
}

// scriptedWSSession hands out a fixed message list then fails Read
// (session drop); Write is a no-op sink. Used by the reconnect pin.
type scriptedWSSession struct {
	msgs [][]byte
	i    int
}

func (s *scriptedWSSession) Write(ctx context.Context, messageType int, data []byte) error {
	return nil
}

func (s *scriptedWSSession) Read(ctx context.Context) (int, []byte, error) {
	if s.i < len(s.msgs) {
		m := s.msgs[s.i]
		s.i++
		return 1, m, nil
	}
	return 0, nil, context.Canceled // drop: triggers run()'s single retry
}

func (s *scriptedWSSession) Close() error { return nil }
