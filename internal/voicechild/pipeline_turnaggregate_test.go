package voicechild

// Voice-startguard C1 gate tests: TURN AGGREGATION. The provider keeps
// flushing segments at 500 ms wall-idle (needed for link pumps —
// segmentation stays untouched); the pipeline aggregates consecutive
// FINAL transcripts into ONE prompt frame. Live-call evidence
// (local-496a3fd0d2ab3fc6): the operator paused 300-600 ms mid-sentence
// for a breath, "Yeah, I'm not sure." finalized and dispatched a reply,
// then the continuation "Do you think you could explain to me?"
// dispatched a SECOND reply while he was still talking — agents
// answering fragments of one sentence.
//
// Contracts pinned here:
//   - two finals inside the window -> exactly ONE prompt frame, text
//     aggregated single-space, telemetry INFO 'final prompt dispatched
//     aggregated' with call_id + parts count + char length.
//   - the live-log breath pattern -> ONE dispatch total.
//   - turn_confirm_ms = 0 -> legacy per-final dispatch (raw text, no
//     trim, no aggregation — byte-identical to pre-C1).
//   - absent selector (old daemon line) -> child default 900 (aggregation ON).
//   - clear() DROPS the pending buffer (documented choice: an
//     interrupted operator's partial rides the old dispatch-cancel).
//   - transcript-stream EOF flushes the pending buffer exactly once.
//   - hangup (pipeline close) flushes the pending buffer.
//   - non-speech / blank / duplicate filters keep working per FINAL,
//     before aggregation: a dropped part never enters the buffer.
//   - the mic-hold: a fresh lastMicLoudAt stamp at window expiry
//     re-arms instead of dispatching, bounded at maxMicHoldRearms.
//   - the pumpAudioIn lane stamps lastMicLoudAt on the LOOSER floor
//     (bargeInRms/4) while forwarding PCM.
//   - the 8 s age cap force-dispatches the aggregated buffer.
//
// Fixture style: newAggChild (recordingSTT lane wired like
// newRMSChild/child_test.go). Timers use 30-60 ms windows; sleeps stay
// under ~100 ms except bounded absence windows.

import (
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// newAggChild builds a live child + recordingSTT pipeline whose keys
// line carries the given turn_confirm_ms selector (nil = absent = the
// old-daemon wire shape -> the child default). The STT session is bound
// and playback idle: caller emits finals through stt.emit and observes
// the fake daemon's frame FIFO.
func newAggChild(t *testing.T, ccid string, confirmMs *int) (*fakeDaemon, *Child, *pipeline, *recordingSTT) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok-agg")
	stt := newRecordingSTT()
	child.SetProviderFactories(
		func(cfg ProviderConfig, key string) (STTProvider, error) { return stt, nil },
		func(cfg ProviderConfig, key string) (TTSProvider, error) { return stubTTS{}, nil },
	)
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() {
		_ = in.Close()
		_ = out.Close()
	})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline(ccid)
	if !ok {
		t.Fatalf("pipeline %s missing after setup", ccid)
	}
	sel := ""
	if confirmMs != nil {
		sel = `,"turn_confirm_ms":` + itoaTest(*confirmMs)
	}
	sendRaw(t, childConn, `{"call_id":"`+ccid+`","event":"keys","stt_key":"stt-secret","tts_key":"tts-secret"`+sel+`}`)
	t.Cleanup(func() {
		if err := childConn.Close(); err != nil {
			t.Errorf("close child conn: %v", err)
		}
		if runErr := <-runDone; runErr != nil {
			t.Errorf("Run returned error on close: %v", runErr)
		}
	})
	waitFor(t, func() bool { return stt.streamCalls() == 1 })
	return d, child, p, stt
}

func itoaTest(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [24]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

// popPrompts drains the FIFO and returns every prompt frame.
func popPrompts(d *fakeDaemon) []voiceframe.Frame {
	d.mu.Lock()
	defer d.mu.Unlock()
	var out []voiceframe.Frame
	for _, f := range d.queue {
		if f.Event == voiceframe.EventPrompt {
			out = append(out, f)
		}
	}
	d.queue = nil
	d.rawQueue = nil
	return out
}

// absentWindow fails the test if any prompt frame shows up within `grace`
// (absence pin — the aggregation must hold, not dispatch early).
func absentWindow(t *testing.T, d *fakeDaemon, grace time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if n := countQueuedPrompts(d); n != 0 {
			t.Fatalf("%s: %d prompt frame(s) appeared", msg, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := countQueuedPrompts(d); n != 0 {
		t.Fatalf("%s: %d prompt frame(s) appeared", msg, n)
	}
}

// pendingState reads (text, parts, timerArmed) under playMu.
func pendingState(p *pipeline) (string, int, bool) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.pendingFinal, p.pendingFinalParts, p.pendingFinalTimer != nil
}

func micLoud(p *pipeline) bool {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return !p.lastMicLoudAt.IsZero()
}

// TestAggregationTwoFinalsOnePrompt: two finals inside the window
// (40 ms, driven back-to-back) produce EXACTLY ONE final prompt frame
// with the single-space-joined text, the INFO telemetry line with
// call_id + parts=2 + char length, and a cleared pending buffer.
func TestAggregationTwoFinalsOnePrompt(t *testing.T) {
	const ccid = "agg2f"
	win := 40
	d, _, p, stt := newAggChild(t, ccid, &win)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	stt.emit(Transcript{Text: "alpha", Final: true})
	stt.emit(Transcript{Text: "beta", Final: true})
	// Inside the window nothing may dispatch (the fence).
	absentWindow(t, d, 15*time.Millisecond, "finals dispatched before the confirm window expired")

	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	frames := popPrompts(d)
	if len(frames) != 1 {
		t.Fatalf("prompt frames = %d, want exactly 1", len(frames))
	}
	f := frames[0]
	if f.Event != voiceframe.EventPrompt || f.Text != "alpha beta" || f.Last == nil || !*f.Last {
		t.Fatalf("aggregated prompt = %q %q last=%v, want final prompt \"alpha beta\"", f.Event, f.Text, f.Last)
	}
	if f.CallID != ccid {
		t.Errorf("call_id = %q, want %q", f.CallID, ccid)
	}
	text := logger.everything()
	for _, want := range []string{"voicechild: final prompt dispatched aggregated", "call_id=" + ccid, "parts=2", "chars=10"} {
		if !strings.Contains(text, want) {
			t.Fatalf("aggregation telemetry lacks %q; log:\n%s", want, text)
		}
	}
	if txt, parts, armed := pendingState(p); txt != "" || parts != 0 || armed {
		t.Fatalf("pending buffer after dispatch: text=%q parts=%d timer=%v, want cleared", txt, parts, armed)
	}
}

// TestAggregationBreathPatternSingleDispatch: the live-call pattern of
// local-496a3fd0d2ab3fc6 verbatim — the fragment "Yeah, I'm not sure."
// then the continuation "Do you think you could explain to me?" inside
// one window. ONE dispatch total, the full sentence aggregated.
func TestAggregationBreathPatternSingleDispatch(t *testing.T) {
	const ccid = "aggbreath"
	win := 60
	d, _, _, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "Yeah, I'm not sure.", Final: true})
	stt.emit(Transcript{Text: "Do you think you could explain to me?", Final: true})

	waitFor(t, func() bool { return countQueuedPrompts(d) >= 1 })
	time.Sleep(80 * time.Millisecond) // past the window: a second dispatch could have landed
	var finals []voiceframe.Frame
	for _, f := range popPrompts(d) {
		if f.Last != nil && *f.Last {
			finals = append(finals, f)
		}
	}
	if len(finals) != 1 {
		t.Fatalf("final prompt frames = %d, want exactly 1 (the fragment fence: one sentence, one employee turn)", len(finals))
	}
	want := "Yeah, I'm not sure. Do you think you could explain to me?"
	if finals[0].Text != want {
		t.Fatalf("aggregated text = %q, want %q", finals[0].Text, want)
	}
}

// TestAggregationLegacyZeroDispatchesPerFinal: turn_confirm_ms = 0 keeps
// the pre-C1 shape exactly — immediate per-final dispatch, ORIGINAL
// (untrimmed) text, nothing buffered, no aggregation log. The
// backward-compat pin.
func TestAggregationLegacyZeroDispatchesPerFinal(t *testing.T) {
	const ccid = "agglegacy"
	win := 0
	d, _, p, stt := newAggChild(t, ccid, &win)
	logger := newCaptureLogger()
	p.logger = logger.logger()

	stt.emit(Transcript{Text: "  hello there  ", Final: true})
	f := d.waitFrame(t) // arrives immediately — no timer involved
	if f.Event != voiceframe.EventPrompt || f.Text != "  hello there  " || f.Last == nil || !*f.Last {
		t.Fatalf("legacy final = %q %q last=%v, want final prompt with the ORIGINAL (untrimmed) text", f.Event, f.Text, f.Last)
	}
	stt.emit(Transcript{Text: "second", Final: true})
	f = d.waitFrame(t)
	if f.Text != "second" {
		t.Fatalf("legacy second final = %q, want per-final dispatch (no aggregation)", f.Text)
	}
	if txt, parts, armed := pendingState(p); txt != "" || parts != 0 || armed {
		t.Fatalf("legacy mode buffered a final: text=%q parts=%d timer=%v", txt, parts, armed)
	}
	if strings.Contains(logger.everything(), "aggregated") {
		t.Fatalf("legacy mode logged aggregation telemetry:\n%s", logger.everything())
	}
}

// TestAggregationAbsentSelectorUsesDefault: an old daemon's keys line
// (no turn_confirm_ms field at all) leaves the child on the 900 ms
// default — aggregation stays ON (wire-compat rule, C1 point 7).
func TestAggregationAbsentSelectorUsesDefault(t *testing.T) {
	const ccid = "aggdefault"
	d, _, p, stt := newAggChild(t, ccid, nil)

	if got := p.confirmWindow(); got != time.Duration(defaultTurnConfirmMs)*time.Millisecond {
		t.Fatalf("confirmWindow after absent selector = %v, want %dms default", got, defaultTurnConfirmMs)
	}
	stt.emit(Transcript{Text: "one", Final: true})
	absentWindow(t, d, 30*time.Millisecond, "absent-selector pipeline dispatched per-final (default window must aggregate)")
}

// TestAggregationNegativeClampsLegacy: a negative wire value clamps to
// 0 = legacy at decode (no negative timer windows).
func TestAggregationNegativeClampsLegacy(t *testing.T) {
	const ccid = "aggneg"
	neg := -5
	_, _, p, _ := newAggChild(t, ccid, &neg)
	if got := p.confirmWindow(); got != 0 {
		t.Fatalf("confirmWindow after negative selector = %v, want 0 (clamped legacy)", got)
	}
}

// TestAggregationClearDropsPending: clear() drops the pending buffer —
// the documented choice (an interrupted operator's partial utterance is
// cancelled exactly like the old per-final dispatch cancel). After the
// clear, even window expiry emits nothing.
func TestAggregationClearDropsPending(t *testing.T) {
	const ccid = "aggclear"
	win := 40
	d, child, p, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "half a thought", Final: true})
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "half a thought" && parts == 1
	})
	child.handleClear(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventClear})
	if txt, parts, armed := pendingState(p); txt != "" || parts != 0 || armed {
		t.Fatalf("clear left the pending buffer: text=%q parts=%d timer=%v, want dropped", txt, parts, armed)
	}
	absentWindow(t, d, 150*time.Millisecond, "clear-dropped text dispatched after the window")
}

// TestAggregationEOFTranscriptStreamFlushes: the transcript channel
// closing (provider stream end) while text is pending dispatches it
// exactly once — the caller's last utterance is never swallowed. The
// window is LONG (2 s): only the EOF flush can dispatch. Driven through
// the direct consumeTranscripts seam (driveFinal style): the test owns
// both the send and the channel close, so the EOF ordering is
// Go-synchronized (a socket-mediated pump EOF is race-detector-hostile
// across the kernel boundary — see the emit/defer-close pair in the
// recordingSTT fixture).
func TestAggregationEOFTranscriptStreamFlushes(t *testing.T) {
	const ccid = "aggeof"
	win := 2000
	d, _, p, _ := newAggChild(t, ccid, &win)

	trans := make(chan Transcript, 1)
	done := make(chan struct{})
	go func() {
		p.consumeTranscripts(trans)
		close(done)
	}()
	trans <- Transcript{Text: "trailing words", Final: true}
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "trailing words" && parts == 1
	})
	close(trans) // session EOF: the consumer loop exits into the flush
	select {
	case <-done:
	case <-time.After(testPollDeadline):
		t.Fatal("consumeTranscripts never returned on channel EOF")
	}
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	time.Sleep(60 * time.Millisecond)
	frames := popPrompts(d)
	if len(frames) != 1 {
		t.Fatalf("prompt frames after stream EOF = %d, want exactly 1 (exactly-once flush)", len(frames))
	}
	if frames[0].Text != "trailing words" || frames[0].Last == nil || !*frames[0].Last {
		t.Fatalf("EOF flush frame = %q last=%v, want final prompt \"trailing words\"", frames[0].Text, frames[0].Last)
	}
}

// TestAggregationHangupFlushes: pipeline close (hangup) flushes the
// pending buffer over the live socket before teardown cancels it.
func TestAggregationHangupFlushes(t *testing.T) {
	const ccid = "agghangup"
	win := 2000
	d, child, p, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "final words", Final: true})
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "final words" && parts == 1
	})
	if _, ok := child.dropPipeline(ccid); !ok {
		t.Fatal("pipeline gone before hangup")
	}
	p.close(context.Background())
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	frames := popPrompts(d)
	if len(frames) != 1 || frames[0].Text != "final words" || frames[0].Last == nil || !*frames[0].Last {
		t.Fatalf("hangup flush frames = %+v, want one final prompt \"final words\"", frames)
	}
}

// TestAggregationFiltersApplyPerPart: the non-speech and duplicate
// filters keep working on EACH part before aggregation — a bracket token
// inside the window never enters the buffer, and a repeated final is
// dropped, not joined. "hello" + "(music)" + "hello" + "world"
// aggregates to "hello world" with parts=2.
func TestAggregationFiltersApplyPerPart(t *testing.T) {
	const ccid = "aggfilter"
	win := 40
	d, _, p, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "hello", Final: true})
	stt.emit(Transcript{Text: "(music)", Final: true}) // non-speech: never enters the buffer
	stt.emit(Transcript{Text: "hello", Final: true})   // exact duplicate within the evidence window: dropped
	stt.emit(Transcript{Text: "world", Final: true})
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "hello world" && parts == 2
	})
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	frames := popPrompts(d)
	if len(frames) != 1 || frames[0].Text != "hello world" {
		t.Fatalf("filtered aggregation = %+v, want one prompt \"hello world\" (bracket/duplicate parts dropped per-final)", frames)
	}
}

// TestAggregationPartialsUnaffected: previews flow per-partial with no
// last flag, immediately, and never touch the pending buffer.
func TestAggregationPartialsUnaffected(t *testing.T) {
	const ccid = "aggpartial"
	win := 40
	d, _, p, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "hello", Final: true})
	stt.emit(Transcript{Text: "hello w", Final: false})
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "hello w" || f.Last != nil {
		t.Fatalf("partial preview = %q %q last=%v, want preview prompt with no last flag", f.Event, f.Text, f.Last)
	}
	if txt, parts, _ := pendingState(p); txt != "hello" || parts != 1 {
		t.Fatalf("partial touched the pending buffer: text=%q parts=%d, want the final alone pending", txt, parts)
	}
	stt.emit(Transcript{Text: "world", Final: true})
	waitFor(t, func() bool { return countQueuedPrompts(d) >= 1 })
	joined := false
	for _, f := range popPrompts(d) {
		if f.Text == "hello world" && f.Last != nil && *f.Last {
			joined = true
		}
	}
	if !joined {
		t.Fatal("aggregated final never dispatched after the partial interleaving")
	}
}

// TestAggregationMicHoldBounded: window expiry with a FRESH mic stamp
// re-arms instead of dispatching, bounded at maxMicHoldRearms — a
// perpetually-noisy line can never stall dispatch forever. Driven
// deterministically through the timer closure (no scheduling race):
// accumulate with a long window, stamp the mic live, fire the closure by
// hand.
func TestAggregationMicHoldBounded(t *testing.T) {
	const ccid = "agghold"
	win := 2000
	d, _, p, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "spoken", Final: true})
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "spoken" && parts == 1
	})

	stampMic := func() {
		p.playMu.Lock()
		p.lastMicLoudAt = time.Now()
		p.playMu.Unlock()
	}
	fire := func() {
		p.playMu.Lock()
		seq := p.pendingFinalSeq
		p.playMu.Unlock()
		p.finalConfirmFired(seq)
	}

	// Re-arms 1..maxMicHoldRearms: fresh mic each fire -> no dispatch,
	// buffer intact, budget burns.
	for i := 1; i <= maxMicHoldRearms; i++ {
		stampMic()
		fire()
		if n := countQueuedPrompts(d); n != 0 {
			t.Fatalf("mic hold %d dispatched (%d prompt frames); fresh mic must re-arm, not dispatch", i, n)
		}
		if txt, _, _ := pendingState(p); txt != "spoken" {
			t.Fatalf("re-armed buffer lost its text: %q", txt)
		}
	}
	// Budget exhausted: a FRESH stamp can no longer stop the dispatch
	// (bounded: at most maxMicHoldRearms re-arms, then it lands).
	stampMic()
	fire()
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	frames := popPrompts(d)
	if len(frames) != 1 || frames[0].Text != "spoken" || frames[0].Last == nil || !*frames[0].Last {
		t.Fatalf("bounded-hold dispatch frames = %+v, want one final prompt \"spoken\"", frames)
	}

	// A mic stamp OLDER than the hold window never holds: silence after
	// the breath dispatches on the first fire.
	stt.emit(Transcript{Text: "next", Final: true})
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "next" && parts == 1
	})
	p.playMu.Lock()
	p.lastMicLoudAt = time.Now().Add(-micLoudHoldWindow - time.Second)
	p.playMu.Unlock()
	fire()
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	if frames := popPrompts(d); len(frames) != 1 || frames[0].Text != "next" {
		t.Fatalf("stale-mic fire frames = %+v, want one final prompt \"next\"", frames)
	}
}

// TestAggregationMicHoldLivePump: the pumpAudioIn lane stamps
// lastMicLoudAt via the LOOSER floor (bargeInRms/4): conversational-
// level mic PCM flowing through the real socket pump leaves the hold
// evidence set, and the noteMicEnergy helper itself never stamps on
// silence. The bounded-hold dispatch TIMING is pinned deterministically
// in TestAggregationMicHoldBounded (closure-driven); here we pin the
// WIRING (a timing absence on a live 40 ms window would flake under
// package load — the pump stamping a fresh window is scheduling-
// sensitive, not contract-sensitive).
func TestAggregationMicHoldLivePump(t *testing.T) {
	const ccid = "aggholdpump"
	win := 2000
	d, _, p, stt := newAggChild(t, ccid, &win)
	drain := make(chan struct{})
	p.local = true
	p.bargeInRms = rmsThresh

	// Floor logic named: noteMicEnergy never stamps silence.
	if micLoud(p) {
		t.Fatal("lastMicLoudAt set before any audio")
	}
	p.noteMicEnergy(make([]byte, 640))
	if micLoud(p) {
		t.Fatal("noteMicEnergy stamped a digital-silence chunk (floor compare broken)")
	}

	in, _ := d.acceptAll(ccid)
	go func() {
		for {
			select {
			case <-drain:
				return
			default:
			}
			chunk := make([]byte, 640)
			for i := 0; i+1 < len(chunk); i += 2 {
				binary.LittleEndian.PutUint16(chunk[i:], uint16(int16(callerSample))) //nolint:gosec // G115: test injects a constant int16 sample
			}
			if _, err := in.Write(chunk); err != nil {
				return // socket died with the test teardown
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()
	// The pump lane stamped it: C1 point 4 wiring proven on the real
	// socket path.
	waitFor(t, func() bool { return micLoud(p) })

	// With the mic hot, a final enters the buffer (the fence is armed)
	// — and the hold keeps it un-dispatched past the naive first window
	// in the common case, while the bounded re-arms guarantee it
	// eventually lands. Assert only the deterministic halves: buffered
	// now, dispatched exactly once eventually.
	stt.emit(Transcript{Text: "held open", Final: true})
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "held open" && parts == 1
	})
	close(drain) // mic quiet: the bounded holds finish and the dispatch lands
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	frames := popPrompts(d)
	if len(frames) != 1 || frames[0].Text != "held open" || frames[0].Last == nil || !*frames[0].Last {
		t.Fatalf("post-hold dispatch frames = %+v, want one final prompt \"held open\"", frames)
	}
}

// TestAggregationCapForceDispatch: the 8 s buffer cap force-dispatches
// what aggregated (defensive bound — a never-pausing monologue still
// reaches the employee). Driven with an aged pendingFirstAt so the next
// accumulation trips the cap without waiting 8 s.
func TestAggregationCapForceDispatch(t *testing.T) {
	const ccid = "aggcap"
	win := 2000
	d, _, p, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "first", Final: true})
	waitFor(t, func() bool {
		txt, parts, _ := pendingState(p)
		return txt == "first" && parts == 1
	})
	p.playMu.Lock()
	p.pendingFirstAt = time.Now().Add(-turnAggregationMaxAge - time.Second)
	p.playMu.Unlock()

	stt.emit(Transcript{Text: "second", Final: true})
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	frames := popPrompts(d)
	if len(frames) != 1 || frames[0].Text != "first second" || frames[0].Last == nil || !*frames[0].Last {
		t.Fatalf("cap dispatch frames = %+v, want one final prompt \"first second\"", frames)
	}
	if txt, parts, _ := pendingState(p); txt != "" || parts != 0 {
		t.Fatalf("cap left a buffer: text=%q parts=%d", txt, parts)
	}
}

// TestAggregationStampsWatermarkAndEvidence: the aggregated dispatch
// behaves like a real final dispatch for the RMS barge-in lane —
// lastDispatchAt stamped after the emit, and the arrival evidence
// (lastFinalAt) predates it: exactly the shape the defect 2026-11-11
// watermark requires, unchanged by C1.
func TestAggregationStampsWatermarkAndEvidence(t *testing.T) {
	const ccid = "aggwatermark"
	win := 40
	d, _, p, stt := newAggChild(t, ccid, &win)

	stt.emit(Transcript{Text: "question text", Final: true})
	waitFor(t, func() bool { return countQueuedPrompts(d) == 1 })
	popPrompts(d)
	zero, dispAt := dispatchState(p)
	if zero {
		t.Fatal("aggregated dispatch never stamped lastDispatchAt (watermark must follow the emit)")
	}
	p.playMu.Lock()
	evAt := p.lastFinalAt
	p.playMu.Unlock()
	if evAt.After(dispAt) {
		t.Fatalf("evidence stamp %v post-dates its own dispatch %v (fixture invariant)", evAt, dispAt)
	}
}
