package daemon

// voice_ended_display_test.go — test gate 00T §3: the voice.call.ended
// payload shape after the contact-display swap (leaf 02).
//
// APPROACH CHOSEN: real-wiring publish + decode. The existing
// newVoiceWiringForTest seam builds a complete VoiceWiring (registry,
// employee stack, release fan-out) and publishCallEnded reads ONLY the
// registry snapshot + w.bus + w.logger, so driving
// w.releaseCall(ccid, outcome) with a bus attached exercises the ACTUAL
// publish path — no hand-built payload substitution. A seeded contact is
// wired via the pinned displayLookup field (O5 fallback, see
// caller_display_test.go); the resolved fields must render the contact
// name, and the raw number must be GONE (O4).
//
// FROZEN shape (master): CallerName json:"caller_name", CallerDisplay
// json:"caller_display"; raw Number field DELETED. Key-set assertions
// check ABSENCE of "number", not just mismatch (review checklist).

import (
	"encoding/json"
	"log/slog"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/contact"
	"github.com/caimlas/meept/internal/voice"
	"github.com/caimlas/meept/pkg/models"
)

var reEndedDigitRun7 = regexp.MustCompile(`\d{7,}`)

// TestVoiceCallEndedPayloadDisplayFieldsSeededContact drives the REAL
// publishCallEnded site with a live registry instance + seeded contact
// and decodes what actually hits the bus.
//
// Seam note: the full releaseCall fan-out Releases the registry BEFORE
// publishing (voice_wiring.go:904-906), so through that path the payload
// degrades to call_id+outcome by existing design (documented). Leaf 02
// swaps fields AT publishCallEnded's snapshot loop, so this gate drives
// publishCallEnded directly with the instance still live — the exact
// state the identity-field loop reads.
func TestVoiceCallEndedPayloadDisplayFieldsSeededContact(t *testing.T) {
	w, _ := newVoiceWiringForTest(t)
	msgBus := bus.New(nil, slog.Default())
	t.Cleanup(func() { msgBus.Close() })

	sub := msgBus.Subscribe("ended-display-test", TopicVoiceCallEnded.Name)
	if sub == nil {
		t.Fatal("bus.Subscribe returned nil")
	}
	t.Cleanup(func() { msgBus.Unsubscribe(sub) })

	w.bus = msgBus
	// Seeded contact behind the pinned O5 resolver seam (leaf 02 wires
	// resolveDisplay into publishCallEnded).
	w.displayLookup = &fakeDisplayLookup{
		found: true,
		rec:   contact.Record{DisplayName: "Jane Doe", Phone: "+15551235567"},
	}

	const ccid = "cc-ended-display"
	if _, err := w.Registry.Acquire(ccid, "phone-employee", "+15551235567"); err != nil {
		t.Fatalf("registry acquire: %v", err)
	}

	w.publishCallEnded(ccid, voice.OutcomeHangup)

	var raw string
	deadline := time.After(2 * time.Second)
	for raw == "" {
		var msg *models.BusMessage
		select {
		case msg = <-sub.Channel:
		case <-deadline:
			t.Fatal("voice.call.ended never reached the bus through releaseCall")
		}
		if msg == nil || !strings.Contains(string(msg.Payload), ccid) {
			continue
		}
		raw = string(msg.Payload)
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		t.Fatalf("decode voice.call.ended payload: %v", err)
	}

	keys := map[string]bool{}
	for k := range payload {
		keys[k] = true
	}

	// §3 key set: caller_name + caller_display PRESENT, number ABSENT.
	for _, want := range []string{"caller_name", "caller_display"} {
		if !keys[want] {
			t.Errorf("voice.call.ended payload missing %q (keys: %v) — leaf 02 RED", want, sortedKeys(keys))
		}
	}
	if keys["number"] {
		t.Errorf("voice.call.ended payload still carries raw \"number\" (keys: %v) — O4 removal is RED", sortedKeys(keys))
	}
	if v, ok := payload["caller_name"].(string); ok && v != "Jane Doe" {
		t.Errorf("caller_name = %q, want %q", v, "Jane Doe")
	}
	if v, ok := payload["caller_display"].(string); ok && strings.Contains(v, "5551235567") {
		t.Errorf("caller_display leaks the full number: %q", v)
	}

	// Unchanged identity fields (leaf 02 must not disturb them).
	if payload["call_id"] != ccid {
		t.Errorf("call_id = %v, want %q", payload["call_id"], ccid)
	}
	if payload["outcome"] != string(voice.OutcomeHangup) {
		t.Errorf("outcome = %v, want %q", payload["outcome"], voice.OutcomeHangup)
	}

	// Egress rule over the WHOLE payload: no \d{7,} run anywhere (the
	// registry target was a full E.164; only last-4 may survive).
	if reEndedDigitRun7.MatchString(raw) {
		t.Errorf("voice.call.ended payload contains a \\d{7,} run: %s", raw)
	}
}

// TestVoiceCallEndedPayloadShapeNoLookup pins the frozen struct shape
// directly (json round-trip of CallEndedEvent): RED until leaf 02 swaps
// Number for the two resolved fields. Complements the live-path test
// above so the tag contract is asserted even if wiring changes.
func TestVoiceCallEndedPayloadShapeNoLookup(t *testing.T) {
	ev := CallEndedEvent{
		CallID:        "cc-shape",
		Outcome:       string(voice.OutcomeHangup),
		CallerName:    "Jane Doe",
		CallerDisplay: "Jane Doe (•••5567)",
	}
	b, err := json.Marshal(ev)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var payload map[string]any
	if err := json.Unmarshal(b, &payload); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, want := range []string{"caller_name", "caller_display"} {
		if _, ok := payload[want]; !ok {
			t.Errorf("CallEndedEvent json shape missing %q: %s — leaf 02 RED", want, b)
		}
	}
	if _, ok := payload["number"]; ok {
		t.Errorf("CallEndedEvent json shape still has \"number\": %s — O4 removal RED", b)
	}
}

func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
