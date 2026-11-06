package voice

// Leaf 12 T1: call store schema + roundtrip + LOCAL-day filter.

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestCallStore(t *testing.T) *CallStore {
	t.Helper()
	s, err := NewCallStore(filepath.Join(t.TempDir(), "calls.db"), nil)
	if err != nil {
		t.Fatalf("NewCallStore: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestCallStoreStartEndRoundtrip(t *testing.T) {
	s := newTestCallStore(t)
	ctx := context.Background()

	err := s.Start(ctx, CallRecord{
		CallID: "cc-1", Number: "+15551230001", EmployeeID: "emp-a",
		Direction: DirectionIncoming, SessionID: "session-x",
	})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	// Duplicate call id is an error (one row per call).
	if err := s.Start(ctx, CallRecord{CallID: "cc-1"}); err == nil {
		t.Fatal("duplicate Start must error")
	}
	if err := s.AppendTranscript(ctx, "cc-1", TranscriptLine{Role: "user", Text: "hello there", At: time.Now()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if err := s.AppendTranscript(ctx, "cc-1", TranscriptLine{Role: "assistant", Text: "hi", At: time.Now()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if err := s.End(ctx, "cc-1", "hangup"); err != nil {
		t.Fatalf("End: %v", err)
	}
	// Unknown call id end is a no-op.
	if err := s.End(ctx, "cc-unknown", "hangup"); err != nil {
		t.Fatalf("End unknown: %v", err)
	}

	got, ok := s.SessionIDFor("+15551230001", "emp-a")
	if !ok || got != "session-x" {
		t.Fatalf("SessionIDFor = %q,%v; want session-x,true", got, ok)
	}
	if _, ok := s.SessionIDFor("+15551239999", "emp-a"); ok {
		t.Fatal("SessionIDFor for unknown number must be false")
	}

	today, err := s.TodayTranscripts(ctx, "+15551230001")
	if err != nil {
		t.Fatalf("TodayTranscripts: %v", err)
	}
	if len(today) != 1 {
		t.Fatalf("TodayTranscripts len = %d; want 1", len(today))
	}
	if today[0].Number != "+15551230001" {
		t.Errorf("Number = %q", today[0].Number)
	}
	if want := "hangup: hello there"; today[0].Summary != want {
		t.Errorf("Summary = %q; want %q", today[0].Summary, want)
	}
}

func TestCallStoreAppendUnknownCall(t *testing.T) {
	s := newTestCallStore(t)
	err := s.AppendTranscript(context.Background(), "cc-none", TranscriptLine{Role: "user", Text: "x"})
	if err != ErrCallNotFound {
		t.Fatalf("AppendTranscript unknown = %v; want ErrCallNotFound", err)
	}
}

func TestCallStoreTodayTranscriptsLocalDay(t *testing.T) {
	// TZ differential: the local-day boundary must track the store's
	// location, not UTC. Run the same scenario under two zones with an
	// offset far enough apart that UTC day keys would diverge.
	for _, zone := range []string{"Pacific/Kiritimati", "Pacific/Pago_Pago"} {
		t.Run(zone, func(t *testing.T) {
			loc, err := time.LoadLocation(zone)
			if err != nil {
				t.Skipf("load %s: %v", zone, err)
			}
			s := newTestCallStore(t)
			s.loc = loc
			ctx := context.Background()

			now := time.Now().In(loc)
			midnight := time.Date(now.Year(), now.Month(), now.Day(), 0, 5, 0, 0, loc)
			yesterday := midnight.Add(-2 * time.Hour)

			if err := s.Start(ctx, CallRecord{CallID: "cc-old", Number: "+15551230002", StartedAt: yesterday}); err != nil {
				t.Fatalf("Start old: %v", err)
			}
			if err := s.Start(ctx, CallRecord{CallID: "cc-new", Number: "+15551230002", StartedAt: midnight}); err != nil {
				t.Fatalf("Start new: %v", err)
			}
			// A different number's call today must not leak in.
			if err := s.Start(ctx, CallRecord{CallID: "cc-other", Number: "+15551230003", StartedAt: now}); err != nil {
				t.Fatalf("Start other: %v", err)
			}

			got, err := s.TodayTranscripts(ctx, "+15551230002")
			if err != nil {
				t.Fatalf("TodayTranscripts: %v", err)
			}
			if len(got) != 1 {
				t.Fatalf("TodayTranscripts len = %d; want 1 (yesterday filtered out)", len(got))
			}
			if got[0].Number != "+15551230002" {
				t.Errorf("Number = %q", got[0].Number)
			}
		})
	}
}
