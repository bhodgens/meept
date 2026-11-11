package daemon

// voice_local_events_rpc_test.go - the callEventsSeam projection test
// (voice-events tree): the daemon adapter maps the internal/voice ring
// read onto rpc.VoiceCallEvent rows and degrades to nil seams without a
// ring. No wiring boot needed - the seam is a pure projection.

import (
	"testing"

	"github.com/caimlas/meept/internal/voice"
)

func TestCallEventsSeamProjection(t *testing.T) {
	ring := voice.NewTelemetryRing(8, 4)
	ring.Append("cc-1", voice.EventInput{Kind: voice.EventKindHeard, Text: "hello", Words: 1})
	ring.Append("cc-1", voice.EventInput{Kind: voice.EventKindFinal, Words: 4, Ms: 120})

	w := &VoiceWiring{callEvents: ring}
	seam := callEventsSeam(w)
	if seam == nil {
		t.Fatal("callEventsSeam nil with a ring")
	}
	events, truncated, found := seam("cc-1", 0, 0)
	if !found || truncated {
		t.Fatalf("found=%v truncated=%v", found, truncated)
	}
	if len(events) != 2 || events[0].Kind != "heard" || events[1].Kind != "turn_final" {
		t.Fatalf("projection = %+v", events)
	}
	if events[0].Seq != 1 || events[1].Seq != 2 || events[0].At == "" || events[1].Ms != 120 {
		t.Fatalf("row fields = %+v / %+v", events[0], events[1])
	}
	// Unknown ccid: found=false, empty list (never nil - the handler
	// normalises but the seam must not surprise it).
	events, _, found = seam("cc-nope", 0, 0)
	if found || len(events) != 0 {
		t.Fatalf("unknown ccid: found=%v events=%+v", found, events)
	}
}

func TestCallEventsSeamNilSafe(t *testing.T) {
	if seam := callEventsSeam(nil); seam != nil {
		t.Fatal("nil wiring must give a nil seam (disabled posture)")
	}
	if seam := callEventsSeam(&VoiceWiring{}); seam != nil {
		t.Fatal("ringless wiring must give a nil seam")
	}
	// The seams builder with a ringless-but-live wiring still arms only
	// Events when the leg is nil (a phone-only boot must still answer the
	// RPC without the "method unwired" envelope).
	seams := voiceLocalSeams(&VoiceWiring{callEvents: voice.NewTelemetryRing(4, 4)}, nil, testLogger(t))
	if seams.Open != nil {
		t.Fatal("nil local leg must keep Open nil (disabled refusal)")
	}
	if seams.Events == nil {
		t.Fatal("a ring present must arm voice.local.events even with the local leg disabled")
	}
	_, _, found := seams.Events("cc-x", 0, 0)
	if found {
		t.Fatal("empty ring: found must stay false")
	}
}

func TestVoiceLocalSeamsNilWiring(t *testing.T) {
	seams := voiceLocalSeams(nil, nil, testLogger(t))
	if seams.Events != nil || seams.Open != nil {
		t.Fatal("nil wiring must give all-nil seams")
	}
}
