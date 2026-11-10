package rpc

// voice_local_display_test.go — test gate 00T §4: voice.local.status
// rows carry the contact-display fields (leaf 02, server surface swaps).
//
// FROZEN shape (master §Interface Contracts): the raw single target
// string becomes a display pair — TargetName json:"target_name",
// TargetDisplay json:"target_display"; the raw target is REMOVED from
// the wire (O4: survey found no consumer routes on it).
//
// RED discipline: the ONLY undefined symbols here are the two new
// VoiceCallStatus fields (TargetName / TargetDisplay) — owned by leaf
// 02's swap at internal/rpc/voice_local.go + the daemon fill site
// (voice_local_rpc.go). Driven through the existing fakeSeams handler
// pattern (voice_local_test.go), no voice-package types.

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

var reStatusDigitRun7 = regexp.MustCompile(`\d{7,}`)

func TestVoiceLocalStatusRowDisplayFields(t *testing.T) {
	f := &fakeSeams{statusRows: []VoiceCallStatus{
		{
			CallID:        "cc-phone",
			EmployeeID:    "eve",
			TargetName:    "Jane Doe",
			TargetDisplay: "Jane Doe (•••5567)",
			SinceRFC3339:  "2026-11-09T10:00:00Z",
			Leg:           "phone",
		},
		{
			CallID:        "cc-unknown",
			EmployeeID:    "eve",
			TargetName:    "Unknown caller",
			TargetDisplay: "Unknown caller (•••0001)",
			SinceRFC3339:  "2026-11-09T10:05:00Z",
			Leg:           "phone",
		},
	}}
	h := NewVoiceLocalHandler(f.seams())
	res, err := h.handleStatus(context.Background(), nil)
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	st, ok := res.(VoiceLocalStatusResult)
	if !ok {
		t.Fatalf("want VoiceLocalStatusResult, got %T", res)
	}
	if len(st.Calls) != 2 {
		t.Fatalf("want 2 rows, got %d", len(st.Calls))
	}

	b, err := json.Marshal(st)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(b)

	// §4 key set: target_name + target_display PRESENT on the wire.
	for _, key := range []string{"target_name", "target_display"} {
		if !strings.Contains(s, `"`+key+`"`) {
			t.Errorf("status row json missing %q: %s — leaf 02 RED", key, s)
		}
	}
	// Raw target REMOVED (absence, not mismatch — review checklist).
	if strings.Contains(s, `"target":`) {
		t.Errorf("status row json still carries raw \"target\": %s — O4 removal RED", s)
	}
	// No full digit run anywhere in the payload (last-4 is the max;
	// since_rfc3339's digits are <7 consecutive).
	if reStatusDigitRun7.MatchString(s) {
		t.Errorf("status row json contains a \\d{7,} run: %s", s)
	}

	// Field round-trip values survive the seam -> handler -> result path.
	if st.Calls[0].TargetName != "Jane Doe" {
		t.Errorf("row0 TargetName = %q, want %q", st.Calls[0].TargetName, "Jane Doe")
	}
	if !strings.Contains(st.Calls[0].TargetDisplay, "5567") {
		t.Errorf("row0 TargetDisplay missing last-4 form: %q", st.Calls[0].TargetDisplay)
	}
}

// TestVoiceLocalStatusDisplayFieldJSONTags pins the exact per-row JSON
// shape with a single marshalled row, key-set style (presence AND the
// two frozen names byte-exact).
func TestVoiceLocalStatusDisplayFieldJSONTags(t *testing.T) {
	row := VoiceCallStatus{
		CallID:        "cc-tag",
		EmployeeID:    "eve",
		TargetName:    "Jane Doe",
		TargetDisplay: "Jane Doe (•••5567)",
		Leg:           "phone",
	}
	b, err := json.Marshal(row)
	if err != nil {
		t.Fatalf("marshal row: %v", err)
	}
	var keys map[string]json.RawMessage
	if err := json.Unmarshal(b, &keys); err != nil {
		t.Fatalf("unmarshal row: %v", err)
	}
	for _, want := range []string{"call_id", "employee_id", "target_name", "target_display", "leg"} {
		if _, ok := keys[want]; !ok {
			t.Errorf("row json missing %q: %s — leaf 02 RED", want, b)
		}
	}
	if _, ok := keys["target"]; ok {
		t.Errorf("row json still has raw \"target\": %s — O4 removal RED", b)
	}
}
