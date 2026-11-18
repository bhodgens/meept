package voice

// Telemetry contract pins (C-10, C-20): truncated means ONLY genuine
// eviction, HasMore reports the limit cap separately, and HasRing
// distinguishes "ring exists but empty" from "ring evicted / never
// existed" so release fan-out cannot resurrect evicted rings.
import "testing"

// A limit-capped backlog read retains every row: not truncated, has-more.
// Cursor 2 == droppedEnd (seqs 1-2 were evicted), so the read starts at
// the window's edge and the cap (not eviction) stops it after two rows.
func TestTelemetryRingLimitCapIsNotTruncated(t *testing.T) {
	r := NewTelemetryRing(4, 8)
	for range 6 {
		r.Append("cc-cap", EventInput{Kind: EventKindHeard, Text: "t"})
	}
	events, truncated, hasMore, found := r.Since("cc-cap", 2, 2)
	if !found {
		t.Fatal("ring must be found")
	}
	if truncated {
		t.Fatal("limit-capped read must NOT report truncated (C-10)")
	}
	if !hasMore {
		t.Fatal("limit-capped read must report hasMore")
	}
	if len(events) != 2 || events[0].Seq != 3 || events[1].Seq != 4 {
		t.Fatalf("limited read = %+v, want seqs 3,4", events)
	}
	// Sanity: the same window without the cap is hasMore=false.
	events, truncated, hasMore, _ = r.Since("cc-cap", 2, 0)
	if truncated || hasMore || len(events) != 4 {
		t.Fatalf("uncapped read = %d events truncated=%v hasMore=%v, want 4/false/false", len(events), truncated, hasMore)
	}
}

// Genuine eviction: cursor below the retained window start.
func TestTelemetryRingEvictionTruncates(t *testing.T) {
	r := NewTelemetryRing(4, 8)
	for range 10 {
		r.Append("cc-ev", EventInput{Kind: EventKindHeard})
	}
	_, truncated, _, found := r.Since("cc-ev", 0, 0)
	if !found || !truncated {
		t.Fatalf("cursor 0 after overflow: truncated=%v found=%v, want true/true", truncated, found)
	}
	// At the retained window's start (droppedEnd == 6): not truncated.
	if _, truncated, _, _ = r.Since("cc-ev", 6, 0); truncated {
		t.Fatal("cursor at droppedEnd must not report truncated")
	}
	// cursor==0 on a NON-wrapped ring (count < cap): healthy first poll.
	r2 := NewTelemetryRing(64, 8)
	for range 5 {
		r2.Append("cc-fresh", EventInput{Kind: EventKindDispatch})
	}
	events, truncated, _, found := r2.Since("cc-fresh", 0, 0)
	if !found || truncated || len(events) != 5 {
		t.Fatalf("healthy first poll: truncated=%v events=%d, want false/5", truncated, len(events))
	}
}

// HasRing: true for a live (even empty) ring, false for evicted/unknown.
func TestTelemetryRingHasRing(t *testing.T) {
	r := NewTelemetryRing(2, 2)
	r.Append("cc-a", EventInput{Kind: EventKindHeard})
	r.Append("cc-b", EventInput{Kind: EventKindHeard})
	r.Append("cc-c", EventInput{Kind: EventKindHeard}) // evicts cc-a
	var nilRing *TelemetryRing
	if nilRing.HasRing("cc-a") {
		t.Fatal("nil ring must report no ring")
	}
	if r.HasRing("cc-nope") {
		t.Fatal("unknown ccid must report no ring")
	}
	// cc-a was FIFO-evicted: no live ring (Since not-found) but the
	// ever-recorded probe stays true (C-20: release must not resurrect).
	if _, _, _, found := r.Since("cc-a", 0, 0); found {
		t.Fatal("evicted ccid must not read as a live ring in Since")
	}
	if !r.HasRing("cc-b") || !r.HasRing("cc-c") {
		t.Fatal("retained ccids must report a ring")
	}
	// Append-created ring with zero retained reads is still a ring.
	r.Append("cc-d", EventInput{Kind: EventKindHeard})
	if !r.HasRing("cc-d") {
		t.Fatal("appended ccid must report a ring")
	}
	// An evicted ccid still reports HasRing=true (ever-recorded probe,
	// C-20) while Since reads not-found (live ring).
	if !r.HasRing("cc-a") {
		t.Fatal("evicted ccid must still report HasRing=true")
	}
	if _, _, _, found := r.Since("cc-a", 0, 0); found {
		t.Fatal("evicted ccid must not read as a live ring in Since")
	}
}
