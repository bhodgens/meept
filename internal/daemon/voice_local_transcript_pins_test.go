package daemon

// voice_local_transcript_pins_test.go - the voice.local.transcript seam
// (end-of-call CLI transcript print, call local-53b347f028f0a43f):
//   - callTranscriptSeam projects the CallStore's EndedCall onto the RPC
//     rows (role/at/employee/session), unknown ccid + store read failure
//     degrade to found=false, and NO store (typed-nil guard!) gives a nil
//     seam - the handler answers the empty envelope.
//   - silenceAudioActiveFor arms the talk loop's AudioActive seam for
//     LOCAL legs only (phone/signal/zero-value stay nil = byte-identical
//     silence path), and the wired seam reads the nil-safe splice tail
//     (no live splice = not audible).

import (
	"context"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/rpc"
	"github.com/caimlas/meept/internal/voice"
)

func newTranscriptStore(t *testing.T) *voice.CallStore {
	t.Helper()
	store, err := voice.NewCallStore(filepath.Join(t.TempDir(), "calls.db"), slog.New(slog.DiscardHandler))
	if err != nil {
		t.Fatalf("NewCallStore: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func TestCallTranscriptSeamProjection(t *testing.T) {
	store := newTranscriptStore(t)
	ctx := context.Background()
	if err := store.Start(ctx, voice.CallRecord{
		CallID: "local-tp-1", Number: "local:human-test",
		EmployeeID: "companion", Direction: "outgoing", SessionID: "session-tp",
	}); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if err := store.AppendTranscript(ctx, "local-tp-1", voice.TranscriptLine{Role: "user", Text: "hello", At: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	if err := store.AppendTranscript(ctx, "local-tp-1", voice.TranscriptLine{Role: "assistant", Text: "hi back", At: time.Now().UTC()}); err != nil {
		t.Fatalf("AppendTranscript: %v", err)
	}
	w := &VoiceWiring{Employee: &VoiceEmployeeWiring{store: store}}
	seam := callTranscriptSeam(w)
	if seam == nil {
		t.Fatal("callTranscriptSeam nil with a store")
	}
	emp, sess, lines, found := seam(ctx, "local-tp-1")
	if !found || emp != "companion" || sess != "session-tp" {
		t.Fatalf("found=%v emp=%q sess=%q", found, emp, sess)
	}
	if len(lines) != 2 || lines[0].Role != "user" || lines[0].Text != "hello" || lines[1].Role != "assistant" {
		t.Fatalf("lines = %+v", lines)
	}
	if lines[0].At == "" {
		t.Fatal("line At must render RFC3339, not empty")
	}
	// Unknown call: found=false, nil lines (the handler normalises).
	if _, _, lines, found := seam(ctx, "local-nope"); found || len(lines) != 0 {
		t.Fatalf("unknown ccid: found=%v lines=%+v", found, lines)
	}
}

func TestCallTranscriptSeamTypedNilSafe(t *testing.T) {
	// A nil *voice.CallStore must NOT box into a non-nil interface here:
	// linkTranscriptSourceFor guards it, so the seam is nil and the
	// handler answers the empty envelope (the typed-nil wiring rule).
	if seam := callTranscriptSeam(nil); seam != nil {
		t.Fatal("nil wiring must give a nil transcript seam")
	}
	if seam := callTranscriptSeam(&VoiceWiring{}); seam != nil {
		t.Fatal("employee-less wiring must give a nil transcript seam")
	}
	if seam := callTranscriptSeam(&VoiceWiring{Employee: &VoiceEmployeeWiring{}}); seam != nil {
		t.Fatal("nil store boxed into the interface must give a nil seam (typed-nil guard)")
	}
	// voiceLocalSeams arms Transcript alongside Events whenever a store
	// exists - INCLUDING the disabled-leg posture (the transcript print
	// is read-only and call-scoped; it never needs the local leg).
	w := &VoiceWiring{Employee: &VoiceEmployeeWiring{store: newTranscriptStore(t)}}
	seams := voiceLocalSeams(w, nil, testLogger(t))
	if seams.Transcript == nil {
		t.Fatal("a store must arm voice.local.transcript even with the local leg disabled")
	}
	if seams.Open != nil {
		t.Fatal("the disabled leg must keep Open nil (named refusal)")
	}
	// Ringless + storeless wiring: nothing arms except the named refusals.
	seams2 := voiceLocalSeams(&VoiceWiring{}, nil, testLogger(t))
	if seams2.Transcript != nil || seams2.Events != nil {
		t.Fatal("a storeless/ringless wiring arms no read seam")
	}
}

func TestSilenceAudioActiveForLegs(t *testing.T) {
	// Non-local legs (phone zero value, "phone", "signal") = nil seam:
	// the talk loop's silence path is byte-identical there.
	for _, leg := range []string{"", voice.LegPhone, "signal"} {
		if fn := silenceAudioActiveFor(leg); fn != nil {
			t.Fatalf("leg %q must NOT arm the AudioActive seam", leg)
		}
	}
	fn := silenceAudioActiveFor(voice.LegLocal)
	if fn == nil {
		t.Fatal("local leg must arm the AudioActive seam")
	}
	// With no live splice registered (the daemon process has none in
	// this unit), the seam reads "not audible" - LocalSpliceFor nil +
	// TailActive nil receiver must never panic.
	if fn("local-nosplice", 10*time.Second) {
		t.Fatal("a call with no live splice must read not-audible")
	}
	// And the loop-level posture: with the seam wired and NO audio, the
	// silence fire proceeds immediately (mirrors the voice-package nil-
	// seam pin from the other side - the wiring never flips true here).
	loop := voice.NewTalkLoop(voice.TalkDeps{AudioActive: fn, Logger: testLogger(t)},
		voice.TalkLoopConfig{SilencePromptSeconds: 5})
	if loop == nil {
		t.Fatal("NewTalkLoop with the AudioActive seam must build")
	}
	_ = rpc.ErrVoiceLocalDisabled // surface-shape import pin (seam rows are rpc-typed)
}

func TestSilenceCallerActiveForLegs(t *testing.T) {
	for _, leg := range []string{"", voice.LegPhone, "signal"} {
		if fn := silenceCallerActiveFor(leg); fn != nil {
			t.Fatalf("leg %q must NOT arm the CallerActive seam", leg)
		}
	}
	fn := silenceCallerActiveFor(voice.LegLocal)
	if fn == nil {
		t.Fatal("local leg must arm the CallerActive seam")
	}
	if fn("local-nosplice", time.Second) {
		t.Fatal("a call with no live splice must read not-speaking")
	}
}
