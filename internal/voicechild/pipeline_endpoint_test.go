package voicechild

// Gate test (STT branch 00T T2, feature F4, contract C2 + C1): the
// authoritative Deepgram speech endpoint drives immediate dispatch of all
// accumulated finals, behind the opt-in DeepgramEndpointMs knob.
//
// RED STYLE (deliberate, matches the foundation gate tests): the
// Transcript.Endpoint field does not exist yet, so this package MUST keep
// compiling. The missing symbol is probed via reflection with a named
// failure; no direct reference to a field that does not yet exist, no
// t.Skip. The test is RED today and turns GREEN when leaf 02 lands, with
// zero edits here.
//
// Clauses: C2 (02-stt-path/orchestrator.md), 02-deepgram-endpoint.md T2,
// F4, R4 (DeepgramEndpointMs=0 => timer path unchanged).

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// transcriptWithEndpoint builds a Transcript{Text, Final, Endpoint} through
// reflection so this file compiles before the Endpoint field exists. When
// the field is absent, returns ok=false (the RED condition).
func transcriptWithEndpoint(text string, final, endpoint bool) (Transcript, bool) {
	tr := Transcript{Text: text, Final: final}
	v := reflect.ValueOf(&tr).Elem()
	f := v.FieldByName("Endpoint")
	if !f.IsValid() || f.Kind() != reflect.Bool || !f.CanSet() {
		return tr, false
	}
	f.SetBool(endpoint)
	return tr, true
}

// TestEndpointFastDispatch: Given DeepgramEndpointMs>0 with pending final
// parts accumulated inside the confirm window, When a transcript carrying
// Endpoint=true is consumed, Then ALL parts are dispatched immediately in
// ONE aggregated prompt (fast dispatch), without waiting for the timer.
// Given DeepgramEndpointMs=0, the timer path is unchanged (R4 pin): a final
// does not emit until the confirm window fires.
func TestEndpointFastDispatch(t *testing.T) {
	// Contract C2: Transcript.Endpoint must exist (RED today).
	ef, ok := reflect.TypeFor[Transcript]().FieldByName("Endpoint")
	if !ok {
		t.Fatalf("missing symbol voicechild.Transcript.Endpoint (contract C2, 02-deepgram-endpoint T2): authoritative endpoint cannot reach the pipeline")
	}
	if ef.Type.Kind() != reflect.Bool {
		t.Fatalf("voicechild.Transcript.Endpoint is %s, want bool (contract C2)", ef.Type.Kind())
	}
	// Contract C1: the opt-in knob must exist on ProviderConfig and KeysFrame.
	if _, ok := reflect.TypeFor[ProviderConfig]().FieldByName("DeepgramEndpointMs"); !ok {
		t.Errorf("missing symbol ProviderConfig.DeepgramEndpointMs (contract C1): fast path cannot be enabled")
	}
	if _, ok := reflect.TypeFor[KeysFrame]().FieldByName("DeepgramEndpointMs"); !ok {
		t.Errorf("missing symbol KeysFrame.DeepgramEndpointMs (contract C1): keys frame cannot carry the knob")
	}

	// Build a live child + recordingSTT pipeline with a LONG confirm window
	// (2 s) so a dispatch before the window fires is unambiguously the
	// endpoint fast path, not the legacy timer.
	const ccid = "ep1"
	d, _, p, stt := newAggChild(t, ccid, intPtr(2000))
	_ = d
	_ = p
	_ = stt

	// Part 1: two finals inside the window accumulate with NO prompt frame.
	// (The endpoint fast path is what will flush them; pin the accumulation
	// first so the test cannot pass on an accidental per-final dispatch.)
	first := Transcript{Text: "first part", Final: true}
	second := Transcript{Text: "second part", Final: true}
	driveTranscript(p, ccid, first)
	driveTranscript(p, ccid, second)
	if text, parts := pendingFinalState(p); text == "" || parts != 2 {
		t.Fatalf("after two finals pending = (%q, %d), want (\"...first part second part\", 2)", text, parts)
	}
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("prompts appeared before endpoint: %d, want 0 (timer must not have fired)", n)
	}

	// Part 2: an Endpoint=true final flushes ALL parts immediately.
	ep, built := transcriptWithEndpoint("final part", true, true)
	if !built {
		t.Fatal("cannot construct Transcript.Endpoint: missing field (contract C2)")
	}
	driveTranscript(p, ccid, ep)
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt {
		t.Fatalf("frame event = %q, want prompt", f.Event)
	}
	joined := strings.ToLower(f.Text)
	for _, want := range []string{"first part", "second part", "final part"} {
		if !strings.Contains(joined, want) {
			t.Errorf("endpoint-dispatched prompt %q missing accumulated part %q", f.Text, want)
		}
	}
	if f.Last == nil || !*f.Last {
		t.Errorf("endpoint-dispatched prompt last = %v, want true (final turn)", f.Last)
	}
	// All parts dispatched in ONE frame: no second prompt should appear.
	if n := countQueuedPrompts(d); n != 0 {
		t.Errorf("endpoint fast path left %d extra prompt frame(s), want 0 (single aggregated dispatch)", n)
	}
}

// intPtr returns a pointer to n (test helper).
func intPtr(n int) *int { return &n }

// driveTranscript runs one Transcript through consumeTranscripts and waits
// for the consumer goroutine to finish, mirroring driveFinal.
func driveTranscript(p *pipeline, ccid string, tr Transcript) {
	trans := make(chan Transcript, 1)
	trans <- tr
	close(trans)
	done := make(chan struct{})
	go func() {
		p.consumeTranscripts(trans)
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(testPollDeadline):
		p.cancel()
		<-done
	}
}

// pendingFinalState reads (text, parts) under playMu.
func pendingFinalState(p *pipeline) (string, int) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.pendingFinal, p.pendingFinalParts
}

// TestEndpointFastDispatchDisabled: Given DeepgramEndpointMs=0 (legacy),
// When an Endpoint=true final is consumed, Then it does NOT fast-dispatch —
// the confirm-window timer remains the only dispatcher (R4 pin). The
// endpoint flag is inert until the opt-in knob enables it.
func TestEndpointFastDispatchDisabled(t *testing.T) {
	ef, ok := reflect.TypeFor[Transcript]().FieldByName("Endpoint")
	if !ok || ef.Type.Kind() != reflect.Bool {
		t.Fatalf("missing symbol voicechild.Transcript.Endpoint (contract C2): endpoint flag absent")
	}

	// confirmMs=0 => legacy per-final dispatch path: every final emits
	// its own prompt immediately (byte-identical pre-C1 behavior).
	const ccid = "ep2"
	d, _, p, _ := newAggChild(t, ccid, intPtr(0))
	_ = p

	tr, built := transcriptWithEndpoint("legacy final", true, true)
	if !built {
		t.Fatal("cannot construct Transcript.Endpoint: missing field (contract C2)")
	}
	driveTranscript(p, ccid, tr)
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "legacy final" {
		t.Fatalf("legacy final frame = %q %q, want prompt \"legacy final\"", f.Event, f.Text)
	}
	// The endpoint flag must not create an extra dispatch on the legacy
	// path: exactly one prompt for one final.
	if n := countQueuedPrompts(d); n != 0 {
		t.Errorf("legacy path with Endpoint=true left %d extra prompt(s), want 0 (flag inert when knob=0)", n)
	}
}
