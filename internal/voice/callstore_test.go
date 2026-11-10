package voice

// Leaf 12 T1: call store schema + roundtrip + LOCAL-day filter.

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sync"
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
	if !errors.Is(err, ErrCallNotFound) {
		t.Fatalf("AppendTranscript unknown = %v; want ErrCallNotFound", err)
	}
}

// TestCallStoreAppendSurvivesLegacyBlankTranscript pins the COALESCE
// guard: a row whose transcript column is ” (pre-schema-default shape)
// appends into a fresh array instead of erroring on malformed JSON.
func TestCallStoreAppendSurvivesLegacyBlankTranscript(t *testing.T) {
	s := newTestCallStore(t)
	ctx := context.Background()
	if err := s.Start(ctx, CallRecord{CallID: "cc-blank", Number: "+1", EmployeeID: "e"}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE voice_calls SET transcript = '' WHERE call_id = 'cc-blank'`); err != nil {
		t.Fatalf("legacy blank write: %v", err)
	}
	if err := s.AppendTranscript(ctx, "cc-blank", TranscriptLine{Role: "user", Text: "x", At: time.Now().UTC()}); err != nil {
		t.Fatalf("append onto blank transcript: %v", err)
	}
	_, lines, ok, err := s.EndedCall(ctx, "cc-blank")
	if err != nil || !ok || len(lines) != 1 || lines[0].Text != "x" {
		t.Fatalf("EndedCall after blank-append: ok:%v lines:%+v err:%v", ok, lines, err)
	}
}

// TestBindingFailedTurnStillRecordsUserRow is the SECOND half of defect
// B's write path (the link-393ebd86996a0dfd drain run, side b seq=1: the
// employee's turn FAILED with "conversation token budget exhausted", but
// the caller's question — and the spoken filler — were real audio the
// link carried; RunTurn returned at the error before touching the
// transcript, so voice.link.status reported 0 user / 0 assistant for a
// full poll cycle while '[b secretary] speaking' had been announced).
//
// Contract under test: the USER row records what the caller said and is
// appended BEFORE the LLM step, so a failed or cancelled turn never
// loses the caller side of the exchange; the ASSISTANT row keeps its
// current semantics (recorded only when a reply was produced — a
// cancelled mid-draft turn records nothing, which is correct).
//
// RED against the current binding.go (RunTurn's early error return at
// the "voice binding: turn for call" line skips both appends): the fix
// lives in internal/voice/binding.go, outside this fence — see the
// handoff diff.
func TestBindingFailedTurnStillRecordsUserRow(t *testing.T) {
	store := newTestCallStore(t)
	sessions := newFakeSessions()
	progress := &captureProgress{}
	failingTurns := &fakeTurns{reply: "reply two", fail: errors.New("conversation token budget exhausted")}
	b, err := NewBinding(BindingDeps{
		Calls:    store,
		Sessions: sessions,
		Turns:    failingTurns,
		Progress: progress,
	})
	if err != nil {
		t.Fatalf("NewBinding: %v", err)
	}
	ctx := context.Background()
	if _, err := b.OnCallStart(ctx, "cc-fail", "+155****0010", "emp-fail", DirectionIncoming, DayBrief{}); err != nil {
		t.Fatalf("OnCallStart: %v", err)
	}

	// Turn 1: the LLM step fails (the seq=1 shape). The caller's question
	// was real: its user row must be in calls.db.
	if _, err := b.RunTurn(ctx, "cc-fail", "hello, can you hear me"); err == nil {
		t.Fatal("RunTurn must surface the runner error")
	}
	_, lines, ok, err := store.EndedCall(ctx, "cc-fail")
	if err != nil || !ok {
		t.Fatalf("EndedCall = ok:%v err:%v", ok, err)
	}
	if len(lines) == 0 {
		t.Fatal("failed turn recorded NO rows - binding.go must append the user row BEFORE the LLM step (defect B)")
	}
	if len(lines) != 1 || lines[0].Role != "user" || lines[0].Text != "hello, can you hear me" {
		t.Fatalf("after failed turn: transcript = %+v; want exactly one user row (the caller's spoken question)", lines)
	}
}

// TestCallStoreLateFinalAppendSurvivesCancel reproduces defect B (the
// link-393ebd86996a0dfd drain run): a turn whose final lands while the
// call's context is already cancelled (barge-in supersede / hangup /
// teardown racing the reply) lost its transcript rows PERMANENTLY —
// AppendTranscript ran BeginTx on the caller's cancelled turnCtx, failed
// with "context canceled", and the caller's late reply never reached
// calls.db. voice.link.status then reported turn counts that lagged (or
// silently undercounted) the audio the caller actually heard.
//
// Shape: one call, two quick consecutive turns. Turn 1 commits on a live
// ctx; turn 2's final lands AFTER the turn ctx was cancelled, and even
// after End stamped the row terminal. All spoken lines must be present,
// in order, when the link status reader runs EndedCall at the end.
func TestCallStoreLateFinalAppendSurvivesCancel(t *testing.T) {
	s := newTestCallStore(t)
	ctx := context.Background()
	if err := s.Start(ctx, CallRecord{
		CallID: "cc-lag", Number: "+155****0008", EmployeeID: "emp-lag",
		Direction: DirectionIncoming, SessionID: "sess-lag",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Turn 1 (user + assistant) commits while the call ctx is alive.
	if err := s.AppendTranscript(ctx, "cc-lag", TranscriptLine{Role: "user", Text: "first utterance", At: time.Now().UTC()}); err != nil {
		t.Fatalf("append turn 1 user: %v", err)
	}
	if err := s.AppendTranscript(ctx, "cc-lag", TranscriptLine{Role: "assistant", Text: "first reply", At: time.Now().UTC()}); err != nil {
		t.Fatalf("append turn 1 assistant: %v", err)
	}

	// The teardown race: turn 2's LLM reply came back after the talk
	// loop cancelled the turn context AND the release fan-out ended the
	// call. The appends ride THAT dead ctx (binding.RunTurn passes it
	// straight through) and must still commit — recording a turn the
	// caller already heard is a bookkeeping write, not a live-call
	// action.
	deadCtx, cancel := context.WithCancel(ctx)
	cancel()
	if err := s.End(ctx, "cc-lag", "hangup"); err != nil {
		t.Fatalf("End: %v", err)
	}
	if err := s.AppendTranscript(deadCtx, "cc-lag", TranscriptLine{Role: "user", Text: "second utterance", At: time.Now().UTC()}); err != nil {
		t.Fatalf("append turn 2 user on cancelled ctx: %v", err)
	}
	if err := s.AppendTranscript(deadCtx, "cc-lag", TranscriptLine{Role: "assistant", Text: "second reply", At: time.Now().UTC()}); err != nil {
		t.Fatalf("append turn 2 assistant on cancelled ctx: %v", err)
	}

	// The status reader's view (link.go sideStatus -> EndedCall): every
	// spoken turn, in order.
	_, lines, ok, err := s.EndedCall(ctx, "cc-lag")
	if err != nil || !ok {
		t.Fatalf("EndedCall = ok:%v err:%v", ok, err)
	}
	wantText := []string{"first utterance", "first reply", "second utterance", "second reply"}
	wantRole := []string{"user", "assistant", "user", "assistant"}
	if len(lines) != 4 {
		t.Fatalf("transcript rows = %d, want 4 (late finals lost): %+v", len(lines), lines)
	}
	for i := range lines {
		if lines[i].Text != wantText[i] || lines[i].Role != wantRole[i] {
			t.Errorf("line %d = %q/%q, want %q/%q", i, lines[i].Role, lines[i].Text, wantRole[i], wantText[i])
		}
	}
}

// TestCallStoreConcurrentAppendsAreAtomic stress-tests the lost-update
// class on the transcript array: N goroutines each write a user+assistant
// pair through the real store while readers poll EndedCall. The single-
// UPDATE json_insert append must keep every line; a read-modify-write
// append racing across processes could drop whole pairs.
func TestCallStoreConcurrentAppendsAreAtomic(t *testing.T) {
	s := newTestCallStore(t)
	ctx := context.Background()
	if err := s.Start(ctx, CallRecord{
		CallID: "cc-race", Number: "+155****0009", EmployeeID: "emp-race",
		Direction: DirectionIncoming,
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}

	const pairs = 12
	var wg sync.WaitGroup
	for i := 0; i < pairs; i++ {
		wg.Add(1)
		go func(k int) {
			defer wg.Done()
			if err := s.AppendTranscript(ctx, "cc-race", TranscriptLine{Role: "user", Text: fmt.Sprintf("q-%d", k), At: time.Now().UTC()}); err != nil {
				t.Errorf("append user %d: %v", k, err)
			}
			if err := s.AppendTranscript(ctx, "cc-race", TranscriptLine{Role: "assistant", Text: fmt.Sprintf("a-%d", k), At: time.Now().UTC()}); err != nil {
				t.Errorf("append assistant %d: %v", k, err)
			}
		}(i)
	}
	// Concurrent readers (the link status poll shape) must never see a
	// torn/invalid transcript either.
	stop := make(chan struct{})
	var readers sync.WaitGroup
	for r := 0; r < 2; r++ {
		readers.Add(1)
		go func() {
			defer readers.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				if _, lines, _, err := s.EndedCall(ctx, "cc-race"); err != nil {
					t.Errorf("concurrent EndedCall: %v", err)
					return
				} else if lines != nil {
					for _, l := range lines {
						if l.Role != "user" && l.Role != "assistant" {
							t.Errorf("torn transcript line: %+v", l)
						}
					}
				}
			}
		}()
	}
	wg.Wait()
	close(stop)
	readers.Wait()

	_, lines, ok, err := s.EndedCall(ctx, "cc-race")
	if err != nil || !ok {
		t.Fatalf("EndedCall = ok:%v err:%v", ok, err)
	}
	if len(lines) != 2*pairs {
		t.Fatalf("transcript rows = %d, want %d (lost update)", len(lines), 2*pairs)
	}
	seen := map[string]bool{}
	for _, l := range lines {
		seen[l.Text] = true
	}
	for i := 0; i < pairs; i++ {
		if q := fmt.Sprintf("q-%d", i); !seen[q] {
			t.Errorf("missing user line %q", q)
		}
		if a := fmt.Sprintf("a-%d", i); !seen[a] {
			t.Errorf("missing assistant line %q", a)
		}
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
