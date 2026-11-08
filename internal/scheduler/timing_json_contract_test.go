package scheduler

import (
	"encoding/json"
	"testing"
	"time"
)

// TimingSpec JSON contract (DSH-9a): kind serializes as contracted
// snake_case and unmarshals back from both the snake_case and legacy forms.
func TestTimingSpecJSONContract(t *testing.T) {
	at := time.Date(2026, 12, 1, 9, 0, 0, 0, time.UTC)
	specs := []*TimingSpec{
		{Kind: TimingAt, At: &at},
		{Kind: TimingDaily, Time: "09:00:00", TimeZone: "UTC"},
		{Kind: TimingWeekly, Time: "09:00:00", Weekdays: []int{1, 4}},
		{Kind: TimingEvery, Every: 90 * time.Second},
		{Kind: TimingRRule, Time: "09:00:00", TimeZone: "America/Denver",
			RRule: "DTSTART;TZID=America/Denver:20261201T090000\nRRULE:FREQ=WEEKLY;BYDAY=MO,TH"},
	}
	for _, in := range specs {
		raw, err := json.Marshal(in)
		if err != nil {
			t.Fatalf("marshal %s: %v", in.Kind, err)
		}
		var out TimingSpec
		if err := json.Unmarshal(raw, &out); err != nil {
			t.Fatalf("roundtrip %s: %v", in.Kind, err)
		}
		if out.Kind != in.Kind {
			t.Errorf("kind %s roundtripped as %s", in.Kind, out.Kind)
		}
	}

	// snake_case wire values are the contract.
	var got map[string]any
	raw, _ := json.Marshal(&TimingSpec{Kind: TimingAt, At: &at})
	_ = json.Unmarshal(raw, &got)
	if got["kind"] != "at" {
		t.Errorf("at kind wire value = %v, want \"at\"", got["kind"])
	}
	raw, _ = json.Marshal(&TimingSpec{Kind: TimingRRule})
	_ = json.Unmarshal(raw, &got)
	if got["kind"] != "rrule" {
		t.Errorf("rrule kind wire value = %v, want \"rrule\"", got["kind"])
	}

	// Legacy capitalized JSON (older persisted jobs.json) still loads.
	legacy := []byte(`{"kind":"TimingOneShot","at":"` + at.Format(time.RFC3339Nano) + `"}`)
	var legacySpec TimingSpec
	if err := json.Unmarshal(legacy, &legacySpec); err != nil {
		t.Fatalf("legacy kind unmarshal: %v", err)
	}
	if legacySpec.Kind != TimingAt {
		t.Errorf("legacy kind = %s, want %s", legacySpec.Kind, TimingAt)
	}
}
