package voicechild

// Local-leg leaf 02 tests: INCOMING-audio RMS VAD barge-in for the local
// leg (CL3 matrix). Phone legs keep tierA's STT-partial trigger; the RMS
// path must be unreachable when local == false.
//
// The tap sits on the pumpAudioIn path: caller-mic PCM arriving on the .in
// socket trips the latch while playback is live. The agent's OWN outgoing
// TTS never trips it (the pre-fix bug: loud agent speech interrupted
// itself); it only maintains the echo reference, which the guard compares
// against so speaker-through-mic echo does not self-trip either.
//
// Determinism: the stub TTS is chunked and PARKED (chunkedTTS): it hands
// out N 640-byte L16 chunks of a fixed sample, then blocks until the test
// closes park. Every written chunk is counted off the .out socket with
// io.ReadFull, and mic audio is injected as full-size writes on the .in
// socket, so a tripped (or not-tripped) VAD is observed without timing
// races.

import (
	"context"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// loudSample/quietSample: little-endian int16 sample bytes for the chunked
// TTS. 8000 is far above the 3000 test threshold; 100 is far below it.
// callerSample is the loud INCOMING mic level: above the 8000 outgoing
// echo reference by more than the 130% echo margin (8000^2*1.30 =
// 8.32e7 < 12000^2 = 1.44e8) so a caller talking over playback trips,
// while a faithful 8000 echo (ratio 1.00) is guarded.
const (
	loudSample   = 8000
	quietSample  = 100
	callerSample = 12000
	rmsThresh    = 3000
)

// chunkedTTS is a test TTS: N chunks of a fixed constant sample, then it
// parks the stream (blocks on park) until the test closes it, so playback
// stays alive and chunk writes are fully deterministic.
type chunkedTTS struct {
	lo, hi byte // little-endian sample bytes
	chunks int  // chunks handed out before parking
	park   chan struct{}
}

func newChunkedTTS(sample int, chunks int) *chunkedTTS {
	return &chunkedTTS{
		lo:     byte(sample),      //nolint:gosec // G115: test fixture splits an int16 sample into LE bytes
		hi:     byte(sample >> 8), //nolint:gosec // G115: test fixture splits an int16 sample into LE bytes
		chunks: chunks,
		park:   make(chan struct{}),
	}
}

func (t *chunkedTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	return io.NopCloser(&chunkedReader{t: t}), nil
}

type chunkedReader struct {
	t *chunkedTTS
	n int
}

func (r *chunkedReader) Read(p []byte) (int, error) {
	if r.n >= r.t.chunks {
		<-r.t.park // parked: playback holds until the test releases
		return 0, io.EOF
	}
	r.n++
	// One full chunk of the constant little-endian sample.
	for i := 0; i+1 < len(p); i += 2 {
		p[i] = r.t.lo
		p[i+1] = r.t.hi
	}
	return len(p), nil
}

func (r *chunkedReader) Close() error { return nil }

// newRMSChild mirrors newSTTWiredChild but returns the .in and .out
// sockets so the tests can write mic PCM in and count exactly how many
// chunks playback wrote out, plus the recording STT to prove the prompt
// lane keeps flowing during playback, and stamps the open-frame profile
// (leaf 03 fields) onto the pipeline before any say.
func newRMSChild(t *testing.T, ccid string, local bool, threshold int) (*fakeDaemon, *Child, *pipeline, net.Conn, net.Conn, *recordingSTT) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok-rms")
	stt := newRecordingSTT()
	child.SetProviderFactories(
		func(cfg ProviderConfig, key string) (STTProvider, error) {
			if key != "stt-secret" {
				return nil, errors.New("test: want stt-secret key")
			}
			return stt, nil
		},
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
	sendRaw(t, childConn, `{"call_id":"`+ccid+`","event":"keys","stt_key":"stt-secret","tts_key":"tts-secret"}`)
	t.Cleanup(func() {
		if err := childConn.Close(); err != nil {
			t.Errorf("close child conn: %v", err)
		}
		if runErr := <-runDone; runErr != nil {
			t.Errorf("Run returned error on close: %v", runErr)
		}
	})
	waitFor(t, func() bool { return stt.streamCalls() == 1 })
	// Open-frame profile (leaf 03 fields; leaf 02 stamps them directly -
	// the open-frame path is leaf 01/03's gate, TestOpenCarriesProfile).
	p.local = local
	p.bargeInRms = threshold
	return d, child, p, in, out, stt
}

// drainOut reads exactly want bytes off the .out socket (a deterministic
// "playback wrote N chunks" signal); fails the test if they never arrive.
func drainOut(t *testing.T, out net.Conn, want int) {
	t.Helper()
	_ = out.SetReadDeadline(time.Now().Add(5 * time.Second))
	buf := make([]byte, want)
	if _, err := io.ReadFull(out, buf); err != nil {
		t.Fatalf(".out socket never delivered %d bytes: %v", want, err)
	}
}

// writeMic injects n chunks of a constant-int16 sample as caller-microphone
// PCM on the daemon side of the .in socket.
func writeMic(t *testing.T, in net.Conn, sample int, n int) {
	t.Helper()
	chunk := make([]byte, 640)
	for i := 0; i+1 < len(chunk); i += 2 {
		binary.LittleEndian.PutUint16(chunk[i:], uint16(int16(sample))) //nolint:gosec // G115: test injects a constant int16 sample
	}
	for i := range n {
		if _, err := in.Write(chunk); err != nil {
			t.Fatalf("write mic .in chunk %d: %v", i, err)
		}
	}
}

// assertNoInterruptWithin fails the test if any interrupt frame shows up
// in the fake daemon FIFO within `grace`; used for absence pins (the
// emit-off-lock goroutine only needs microseconds on a loopback unix
// socket, so 30 ms is a 1000x-margin bound, not a measurement window).
func assertNoInterruptWithin(t *testing.T, d *fakeDaemon, grace time.Duration, msg string) {
	t.Helper()
	deadline := time.Now().Add(grace)
	for time.Now().Before(deadline) {
		if n := countQueuedInterrupts(d); n != 0 {
			t.Fatalf("%s: %d interrupt(s) appeared", msg, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("%s: %d interrupt(s) appeared", msg, n)
	}
}

// countQueuedInterrupts counts interrupt frames in the fake daemon FIFO
// without popping (total-across-scenario assertions).
func countQueuedInterrupts(d *fakeDaemon) int {
	d.mu.Lock()
	defer d.mu.Unlock()
	n := 0
	for _, f := range d.queue {
		if f.Event == voiceframe.EventInterrupt {
			n++
		}
	}
	return n
}

// TestRMSChunk (T1): the RMS helper on known waveforms - integer-sqrt-free
// compare of the s16le sum-of-squares against threshold^2 * sampleCount.
func TestRMSChunk(t *testing.T) {
	mk := func(sample int, samples int, oddTail bool) []byte {
		b := make([]byte, samples*2)
		for i := 0; i+1 < len(b); i += 2 {
			binary.LittleEndian.PutUint16(b[i:], uint16(int16(sample))) //nolint:gosec // G115: test fixture PCM encode of a constant sample
		}
		if oddTail {
			b = append(b, 0xFF) // trailing half-sample: must be ignored
		}
		return b
	}

	if rmsChunkOver(mk(0, 320, false), rmsThresh) {
		t.Fatalf("digital silence rated over threshold %d", rmsThresh)
	}
	if rmsChunkOver(mk(quietSample, 320, false), rmsThresh) {
		t.Fatalf("amplitude %d rated over threshold %d", quietSample, rmsThresh)
	}
	if !rmsChunkOver(mk(loudSample, 320, false), rmsThresh) {
		t.Fatalf("constant amplitude %d rated under threshold %d", loudSample, rmsThresh)
	}
	// Sign flips do not change the square: alternating +/- loud is loud.
	alt := mk(loudSample, 320, false)
	for i := 2; i+1 < len(alt); i += 4 {
		// Flip every second sample to its negative via two's complement.
		neg := int16(-loudSample)
		binary.LittleEndian.PutUint16(alt[i:], uint16(neg)) //nolint:gosec // G115: test fixture flips the sample sign via two's complement
	}
	if !rmsChunkOver(alt, rmsThresh) {
		t.Fatalf("alternating +/- %d rated under threshold %d", loudSample, rmsThresh)
	}
	// At-threshold trips (>= compare: sustained AT the threshold is loud).
	if !rmsChunkOver(mk(rmsThresh, 320, false), rmsThresh) {
		t.Fatalf("amplitude exactly at threshold %d must trip (>=)", rmsThresh)
	}
	// A loud half-chunk (160 samples) is still over: energy scales with
	// sampleCount on both sides of the compare.
	if !rmsChunkOver(mk(loudSample, 160, false), rmsThresh) {
		t.Fatalf("half-chunk of amplitude %d rated under threshold %d", loudSample, rmsThresh)
	}
	// Odd trailing byte ignored: a quiet chunk + tail stays quiet.
	if rmsChunkOver(mk(quietSample, 320, true), rmsThresh) {
		t.Fatalf("odd trailing byte counted as a sample")
	}
	// Empty and half-empty chunks are never loud.
	if rmsChunkOver(nil, rmsThresh) {
		t.Fatalf("nil chunk rated loud")
	}
	if rmsChunkOver([]byte{0x00}, rmsThresh) {
		t.Fatalf("single-byte chunk rated loud")
	}
	// bargeInRms <= 0 disables the VAD (named no-op): even full-scale is
	// quiet when the threshold is unset.
	if rmsChunkOver(mk(32767, 320, false), 0) {
		t.Fatalf("threshold 0 must disable the VAD (got loud)")
	}
	if rmsChunkOver(mk(32767, 320, false), -5) {
		t.Fatalf("negative threshold must disable the VAD (got loud)")
	}
}

// TestRMSBargeIn (T2): local leg, speaking, loud INCOMING caller PCM on
// the .in socket -> exactly one interrupt frame per barge-in; loud
// OUTGOING TTS with silent input must NOT trip (the pre-fix self-interrupt
// bug); quiet input -> none; after the latch re-arms on a new say, a
// second barge-in emits the second interrupt. The STT/prompt lane keeps
// flowing during playback too: mic chunks still reach the bound STT
// session while the agent talks.
func TestRMSBargeIn(t *testing.T) {
	const ccid = "rms1"
	const chunks = 12 // the trip window is 10 chunks; 2 write after it
	d, child, p, in, out, stt := newRMSChild(t, ccid, true, rmsThresh)

	// Barge-in 1: LOUD outgoing TTS (what the old tap watched) must not
	// self-trip; loud INCOMING mic audio while speaking trips the
	// sustained window through the pump tap.
	gate1 := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate1)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "one moment"}); err != nil {
		t.Fatalf("handleSay 1: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	writeMic(t, in, callerSample, chunks) // caller talks over the agent
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("first frame under loud INCOMING audio = %q, want interrupt", f.Event)
	}
	// The caller's mic PCM still rode the STT lane during playback.
	waitFor(t, func() bool { return len(stt.gotChunks()) >= chunks })
	// FIFO playback (audit fix 1): utterance 2 starts only after
	// utterance 1's stream ends — release the park, which EOFs gate1.
	close(gate1.park)
	drainOut(t, out, chunks*640) // the whole stream still wrote out
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("latch did not hold: %d extra interrupts after one barge-in", n)
	}

	// Loud OUTGOING TTS with SILENT input: the tap never sees the agent's
	// own audio, so no interrupt. (Old behavior pinned the wrong way:
	// loud playback self-tripped.)
	gate2 := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate2)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "let me continue"}); err != nil {
		t.Fatalf("handleSay 2: %v", err)
	}
	drainOut(t, out, chunks*640)
	assertNoInterruptWithin(t, d, 30*time.Millisecond,
		"loud outgoing TTS with silent mic tripped (agent must not interrupt itself)")
	close(gate2.park)

	// Quiet incoming audio while speaking: window never fills, no
	// interrupt.
	gate3 := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate3)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "where were we"}); err != nil {
		t.Fatalf("handleSay 3: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	writeMic(t, in, quietSample, chunks)
	drainOut(t, out, chunks*640)
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("quiet incoming audio emitted %d interrupts, want 0", n)
	}
	close(gate3.park)

	// Barge-in 2: the new say re-armed the latch; loud INCOMING audio
	// trips again.
	gate4 := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate4)
	defer close(gate4.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "ok, back"}); err != nil {
		t.Fatalf("handleSay 4: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	writeMic(t, in, callerSample, chunks)
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("second barge-in frame = %q, want interrupt", f.Event)
	}
	drainOut(t, out, chunks*640)
	// Whole scenario: exactly 2 interrupts (both popped above; nothing left).
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("scenario queued %d unexpected interrupts, want 2 total", n)
	}
}

// TestRMSIncomingWhileNotSpeaking: loud mic audio with NO playback live
// must not emit an interrupt (the VAD is gated to speaking, like the
// phone leg's partial trigger) — and it still flows to STT.
func TestRMSIncomingWhileNotSpeaking(t *testing.T) {
	const ccid = "rms5"
	d, _, _, in, _, stt := newRMSChild(t, ccid, true, rmsThresh)

	writeMic(t, in, callerSample, 14)
	waitFor(t, func() bool { return len(stt.gotChunks()) >= 14 })
	assertNoInterruptWithin(t, d, 30*time.Millisecond, "idle loud mic tripped (barge-in is playback-gated)")
}

// TestRMSEchoGuard: the local-leg mic replaying the agent's OWN loud TTS
// at speaker level (an 8000-sample echo of the 8000-sample outbound
// chunk, below the 130% echo margin) must NOT self-trip, while the same
// playback with a genuinely louder caller (callerSample) does. The echo
// case feeds incoming audio identical to the outbound stream: without the
// guard the latch would trip and the agent would interrupt itself again.
func TestRMSEchoGuard(t *testing.T) {
	const ccid = "rms6"
	const chunks = 12
	d, child, p, in, out, _ := newRMSChild(t, ccid, true, rmsThresh)

	gate := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "watch this echo"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	// One outbound chunk lands before any echo is injected: the guard
	// only has a speaker reference once playbackStream has recorded one
	// (zero reference = agent silent = trip allowed, by design).
	drainOut(t, out, 640)
	waitFor(t, func() bool { return p.isSpeaking() })
	// Feed MORE than the window of exact echoes: every incoming chunk
	// carries the same energy as the outbound reference.
	writeMic(t, in, loudSample, 20)
	// Drain the rest of the outbound stream (the pacer keeps writing
	// chunks; the reference stays a live loud chunk throughout).
	drainOut(t, out, (chunks-1)*640)
	close(gate.park)
	assertNoInterruptWithin(t, d, 30*time.Millisecond, "faithful speaker echo tripped (echo guard)")
}

// TestRMSDisabledOnPhoneLeg (T3): local == false never evaluates the RMS
// path - the same loud INCOMING mic audio that trips a local leg emits no
// interrupt frames at all (proved by frame silence under loud input; the
// tierA STT-partial trigger is untouched - the recording STT emits
// nothing).
func TestRMSDisabledOnPhoneLeg(t *testing.T) {
	const ccid = "rms2"
	const chunks = 14 // more than the trip window: loud the whole way
	d, child, p, in, out, stt := newRMSChild(t, ccid, false, rmsThresh)

	gate := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "long utterance"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	writeMic(t, in, callerSample, chunks) // would trip a local leg
	drainOut(t, out, chunks*640)          // all loud chunks reached the daemon
	waitFor(t, func() bool { return len(stt.gotChunks()) >= chunks })
	assertNoInterruptWithin(t, d, 30*time.Millisecond, "phone leg ran the RMS VAD (must not)")
	close(gate.park)
}

// TestRMSBargeInC5FrameOrder (C5 on the local leg): loud INCOMING audio
// trips the RMS latch while an STT partial for the same caller utterance
// arrives on the transcript lane. Whichever goroutine wins the race, the
// interrupt frame must land strictly BEFORE the partial's prompt frame,
// exactly once across both trigger paths (the RMS latch and noteBargeIn
// share p.interrupted). The RMS path emits the interrupt off the pump
// goroutine and the pendingInterrupt handshake blocks the transcript
// consumer's prompt until that write completes; if the partial path
// latches first, noteBargeIn emits inline before its own prompt. Both
// orderings are FIFO-correct, so the assertion holds under -race.
func TestRMSBargeInC5FrameOrder(t *testing.T) {
	const ccid = "rms7"
	const chunks = 12
	d, child, p, in, _, stt := newRMSChild(t, ccid, true, rmsThresh)

	gate := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "speaking now"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	writeMic(t, in, callerSample, chunks) // trips the RMS latch (async emit)
	stt.emit(Transcript{Text: "actually", Final: false})

	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("first frame = %q, want interrupt (C5: interrupt strictly before prompt)", f.Event)
	}
	f = d.waitFrame(t)
	if f.Event != voiceframe.EventPrompt || f.Text != "actually" {
		t.Fatalf("second frame = %q %q, want prompt \"actually\"", f.Event, f.Text)
	}
	assertNoInterruptWithin(t, d, 30*time.Millisecond, "RMS trip + partial emitted a second interrupt")
}

// TestRMSDisabledZeroThreshold: local leg with bargeInRms <= 0 - the named
// no-op: loud incoming audio never trips, even over the full window.
func TestRMSDisabledZeroThreshold(t *testing.T) {
	const ccid = "rms3"
	const chunks = 14
	d, child, p, in, _, _ := newRMSChild(t, ccid, true, 0)

	gate := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "loud but unmonitored"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	waitFor(t, func() bool { return p.isSpeaking() })
	writeMic(t, in, callerSample, chunks)
	assertNoInterruptWithin(t, d, 30*time.Millisecond, "threshold 0 ran the VAD (named no-op)")
}
