package rpc

// voice_local_events_test.go - handler tests for voice.local.events (the
// operator-telemetry read). Seam fakes only, zero voice-package types
// (the rpc -> voice edge would close the import cycle).

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestVoiceLocalEventsOK(t *testing.T) {
	f := &fakeSeams{eventsFound: true}
	f.eventsRows = []VoiceCallEvent{
		{Seq: 4, Kind: "heard", Text: "hi", At: "2026-11-11T10:00:00Z"},
		{Seq: 5, Kind: "turn_final", Words: 3, Ms: 900, At: "2026-11-11T10:00:02Z"},
	}
	h := NewVoiceLocalHandler(f.seams())
	raw, err := json.Marshal(map[string]any{"call_id": "local-x", "cursor": 3})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	res, err := h.handleEvents(context.Background(), raw)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	out, ok := res.(VoiceLocalEventsResult)
	if !ok || len(out.Events) != 2 || out.Cursor != 5 || !out.Found {
		t.Fatalf("unexpected result: %+v", out)
	}
	// Wire tags pinned.
	b, err := json.Marshal(out)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{"call_id", "events", "cursor", "found", "seq", "kind"} {
		if !strings.Contains(string(b), `"`+key+`"`) {
			t.Fatalf("json missing %s: %s", key, b)
		}
	}
}

func TestVoiceLocalEventsMissingCallID(t *testing.T) {
	f := &fakeSeams{}
	h := NewVoiceLocalHandler(f.seams())
	_, err := h.handleEvents(context.Background(), []byte(`{}`))
	if !errors.Is(err, ErrVoiceLocalUnknownCall) {
		t.Fatalf("want ErrVoiceLocalUnknownCall, got %v", err)
	}
	if f.eventsCalls != 0 {
		t.Fatalf("seam must not fire without call_id: %d", f.eventsCalls)
	}
}

func TestVoiceLocalEventsUnwiredIsEmptyEnvelope(t *testing.T) {
	// Zero seams (disabled posture): the empty envelope, never an error -
	// the CLI poller must stay quiet on a ring-less daemon.
	h := NewVoiceLocalHandler(VoiceLocalSeams{})
	res, err := h.handleEvents(context.Background(), []byte(`{"call_id":"local-x"}`))
	if err != nil {
		t.Fatalf("unwired events: %v", err)
	}
	out := res.(VoiceLocalEventsResult)
	if out.Found || len(out.Events) != 0 {
		t.Fatalf("want empty not-found envelope, got %+v", out)
	}
	// Events is never nil on the wire (empty list, not absent).
	b, _ := json.Marshal(out)
	if !strings.Contains(string(b), `"events":[]`) {
		t.Fatalf("events must serialize as []: %s", b)
	}
}

func TestVoiceLocalEventsRinglessCall(t *testing.T) {
	f := &fakeSeams{} // seam present, call unknown -> found false
	h := NewVoiceLocalHandler(f.seams())
	res, err := h.handleEvents(context.Background(), []byte(`{"call_id":"local-ghost","cursor":0}`))
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	out := res.(VoiceLocalEventsResult)
	if out.Found || len(out.Events) != 0 || out.Cursor != 0 {
		t.Fatalf("ringless call: %+v", out)
	}
}

func TestVoiceLocalEventsTruncatedPassesThrough(t *testing.T) {
	f := &fakeSeams{eventsTruncated: true, eventsFound: true}
	h := NewVoiceLocalHandler(f.seams())
	res, _ := h.handleEvents(context.Background(), []byte(`{"call_id":"local-x","cursor":0}`))
	out := res.(VoiceLocalEventsResult)
	if !out.Truncated {
		t.Fatalf("truncated flag lost: %+v", out)
	}
}
