package voicechild

// Local-leg leaf 02 tests: playback-side RMS VAD barge-in for the local
// leg (CL3 matrix). Phone legs keep tierA's STT-partial trigger; the RMS
// path must be unreachable when local == false.
//
// RED style: TestRMSChunk compiles against the new helper symbol (compile
// failure is the RED); the wiring tests compile against existing symbols
// (handleSay, emitInterrupt, noteBargeIn) and fail on the MISSING TRIGGER:
// with no RMS hook in playback(), loud injected PCM produces no interrupt
// frames on a local leg.
//
// Determinism: the stub TTS is chunked and PARKED (chunkedTTS): it hands
// out N 640-byte L16 chunks of a fixed sample, then blocks until the test
// closes park. Every written chunk is counted off the .out socket with
// io.ReadFull, so a tripped (or not-tripped) VAD is observed without
// timing races.

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
const (
	loudSample  = 8000
	quietSample = 100
	rmsThresh   = 3000
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
		lo:     byte(sample),
		hi:     byte(sample >> 8),
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

// newRMSChild mirrors newSTTWiredChild but returns the .out socket so the
// tests can count exactly how many chunks playback wrote, and stamps the
// open-frame profile (leaf 03 fields) onto the pipeline before any say.
func newRMSChild(t *testing.T, ccid string, local bool, threshold int) (*fakeDaemon, *Child, *pipeline, net.Conn) {
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
	return d, child, p, out
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
			binary.LittleEndian.PutUint16(b[i:], uint16(int16(sample)))
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
		binary.LittleEndian.PutUint16(alt[i:], uint16(neg))
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

// TestRMSBargeIn (T2): local leg, speaking, loud caller PCM through the
// playback path -> exactly one interrupt frame per barge-in; quiet audio
// -> none; after the latch re-arms on a new say, a second barge-in emits
// the second interrupt.
func TestRMSBargeIn(t *testing.T) {
	const ccid = "rms1"
	const chunks = 12 // 10 trip the window; 2 more write after the trip
	d, child, p, out := newRMSChild(t, ccid, true, rmsThresh)

	// Barge-in 1: loud chunks while speaking trip the sustained window.
	gate1 := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate1)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "one moment"}); err != nil {
		t.Fatalf("handleSay 1: %v", err)
	}
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventInterrupt {
		t.Fatalf("first frame under sustained loud playback = %q, want interrupt", f.Event)
	}
	drainOut(t, out, chunks*640) // the whole stream still wrote out
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("latch did not hold: %d extra interrupts after one barge-in", n)
	}
	// FIFO playback (audit fix 1): utterance 2 starts only after
	// utterance 1's stream ends — release the park, which EOFs gate1.
	close(gate1.park)

	// Quiet audio while speaking: window never fills, no interrupt.
	gate2 := newChunkedTTS(quietSample, chunks)
	swapTTS(p, gate2)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "where were we"}); err != nil {
		t.Fatalf("handleSay 2: %v", err)
	}
	drainOut(t, out, chunks*640)
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("quiet audio emitted %d interrupts, want 0", n)
	}
	close(gate2.park)

	// Barge-in 2: the new say re-armed the latch; loud audio trips again.
	gate3 := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate3)
	defer close(gate3.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "ok, back"}); err != nil {
		t.Fatalf("handleSay 3: %v", err)
	}
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

// TestRMSDisabledOnPhoneLeg (T3): local == false never evaluates the RMS
// path - the same loud playback that trips a local leg emits no interrupt
// frames at all (proved by frame silence under loud input; the tierA
// STT-partial trigger is untouched - the recording STT emits nothing).
func TestRMSDisabledOnPhoneLeg(t *testing.T) {
	const ccid = "rms2"
	const chunks = 14 // more than the trip window: loud the whole way
	d, child, p, out := newRMSChild(t, ccid, false, rmsThresh)

	gate := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "long utterance"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	drainOut(t, out, chunks*640) // all loud chunks reached the daemon
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("phone leg emitted %d interrupts from loud playback, want 0 (RMS must not run)", n)
	}
}

// TestRMSDisabledZeroThreshold: local leg with bargeInRms <= 0 - the named
// no-op: loud playback never trips, even over the full window.
func TestRMSDisabledZeroThreshold(t *testing.T) {
	const ccid = "rms3"
	const chunks = 14
	d, child, p, out := newRMSChild(t, ccid, true, 0)

	gate := newChunkedTTS(loudSample, chunks)
	swapTTS(p, gate)
	defer close(gate.park)
	if err := child.handleSay(voiceframe.Frame{CallID: ccid, Event: voiceframe.EventSay, Text: "loud but unmonitored"}); err != nil {
		t.Fatalf("handleSay: %v", err)
	}
	drainOut(t, out, chunks*640)
	if n := countQueuedInterrupts(d); n != 0 {
		t.Fatalf("threshold 0 emitted %d interrupts, want 0 (VAD disabled)", n)
	}
}
