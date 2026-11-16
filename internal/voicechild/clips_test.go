package voicechild

// Filler-clip child tests (plan 20261113-filler-clips-and-context-warm,
// leaf 03; contracts C1-C3 + orchestrator adjudications A1-A5):
//
//   TestClipSayHitZeroSpeak             — say with clip_id store hit: the
//     provider is NEVER called (A2) and the stored PCM reaches the .out
//     socket through the unchanged playback lane.
//   TestClipSayMissFallsToSpeak         — clip_id without a store entry:
//     today's live Speak(frame.Text) path runs byte-identically.
//   TestClipSayNoClipIDUnchanged        — frames without clip_id never
//     touch the store (back-compat).
//   TestClipRenderWarmNoSynth           — clip_render for an existing
//     key answers clip_ready ok:true with zero synth.
//   TestClipRenderMissRendersAndStores  — miss renders through the
//     provider, stores with Meta.SampleRate=16000 (A1), acks ok:true.
//   TestClipRenderGatedOnKeys           — renders queued before the keys
//     frame only start after keys lands (A3).
//   TestClipRenderConcurrencyOne        — renders run strictly serially.
//   TestClipRenderTeardownCancels       — end frame kills a blocked
//     render via the call-scoped ctx; nothing lands in the store.
//   TestClipBargeInClearsLikeSynth      — clear over cached audio aborts
//     the stream exactly like synthesized audio (no played edge).
//   TestClipRateResampleTo8k            — A1 playback: store@16000 into
//     a PCMU@8k call goes through the injected resampler seam.
//   TestRefHashDecodeContract           — A4 keys-frame tts_ref_hash
//     decoding ("" = non-clone nil; malformed degrades to nil).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voiceframe"
)

// recordingTTS counts Speak calls (with the text) and hands out fresh
// readers per call so the byte identity is test-controlled.
type recordingTTS struct {
	mu      sync.Mutex
	texts   []string
	started chan string // notified per Speak (buffered; nil-safe)
}

func newRecordingTTS() *recordingTTS {
	return &recordingTTS{started: make(chan string, 64)}
}

func (t *recordingTTS) Speak(_ context.Context, text string) (io.ReadCloser, error) {
	t.mu.Lock()
	t.texts = append(t.texts, text)
	t.mu.Unlock()
	select {
	case t.started <- text:
	default:
	}
	pcm := make([]byte, 1280) // two 640-byte chunks
	for i := range pcm {
		pcm[i] = 0x77 // synth-marker byte, distinct from clip fill
	}
	return io.NopCloser(newSliceReader(pcm)), nil
}

func (t *recordingTTS) calls() []string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return append([]string(nil), t.texts...)
}

func (t *recordingTTS) count() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.texts)
}

// clipFixture wires newPaceChild with a test clip store rooted in the
// child's temp dir (so R4's MEEPT_HOME resolution stays out of the test).
func clipFixture(t *testing.T, ccid string) (*fakeDaemon, *Child, net.Conn, *pipeline, net.Conn, *clipstore.Store) {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	store := clipstore.NewStoreAt(filepath.Join(dir, "clips"))
	child := New(ChildConfig{
		TextSocketPath: filepath.Join(dir, "t.sock"),
		AudioDir:       dir,
		TokenReader:    strings.NewReader("tok\n"),
	})
	child.SetClipStore(store)
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() {
		_ = in.Close()
		_ = out.Close()
		_ = childConn.Close()
		if err := <-runDone; err != nil {
			t.Errorf("Run returned error on close: %v", err)
		}
	})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, ok := child.getPipeline(ccid)
	if !ok {
		t.Fatalf("pipeline %s missing", ccid)
	}
	return d, child, childConn, p, out, store
}

// sendKeys lands a minimal keys frame on the child so applyKeys closes
// keysReady (the render gate, A3). No factories -> constructProviders is
// a no-op pair of log lines; the test's swapped TTS stays installed.
func sendKeys(t *testing.T, childConn net.Conn, ccid string) {
	t.Helper()
	sendRaw(t, childConn, `{"call_id":"`+ccid+`","event":"keys","stt_key":"s","tts_key":"t"}`)
}

// putClip stores a warm clip: fill repeated `chunks` times 640 bytes,
// meta at the canonical 16 kHz (A1).
func putClip(t *testing.T, store *clipstore.Store, key string, fill byte, chunks int) []byte {
	t.Helper()
	pcm := bytes.Repeat([]byte{fill}, chunks*640)
	meta := clipstore.Meta{Text: "warm", Vendor: "v", Model: "m", Voice: "w", SampleRate: 16000}
	if err := store.Put(key, meta, bytes.NewReader(pcm)); err != nil {
		t.Fatalf("seed clip %s: %v", key, err)
	}
	return pcm
}

const clipKeyHex = "fd2c3570dfae9ed83b3c70c4bcb0b797f03761565af5d0751f2048d7db70b870"

func TestClipSayHitZeroSpeak(t *testing.T) {
	const ccid = "clip-hit"
	d, _, childConn, p, out, store := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)

	pcm := putClip(t, store, clipKeyHex, 0x41, 2)

	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: "Just a sec.",
		Last: lastPtr(true), ClipID: clipKeyHex,
	})

	// Exactly the stored bytes leave the .out socket (two 640-byte chunks).
	buf := make([]byte, 1280)
	_ = out.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(out, buf); err != nil {
		t.Fatalf("read clip audio: %v", err)
	}
	if !bytes.Equal(buf, pcm) {
		t.Fatalf("played bytes != stored pcm (prefix): %x vs %x", buf, pcm)
	}

	// Zero provider calls: the hit replaces Speak entirely (A2).
	// The played edge proves the stream ran to natural EOF through the
	// UNCHANGED lane (A2/R6: pacing + played semantics identical).
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventPlayed || f.Last == nil || !*f.Last {
		t.Fatalf("want played edge for final clip stream, got %+v", f)
	}
	if n := fake.count(); n != 0 {
		t.Fatalf("clip hit triggered %d Speak calls, want 0", n)
	}
	if extra := drainFrames(d); len(extra) != 0 {
		t.Fatalf("unexpected extra frames: %+v", extra)
	}
}

func TestClipSayMissFallsToSpeak(t *testing.T) {
	const ccid = "clip-miss"
	d, _, childConn, p, out, _ := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)

	// clip_id set, store empty (dir never created) -> live path.
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: "Hello there.",
		Last: lastPtr(false), ClipID: clipKeyHex,
	})
	buf := make([]byte, 1280)
	_ = out.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(out, buf); err != nil {
		t.Fatalf("read fallback audio: %v", err)
	}
	if got := fake.calls(); len(got) != 1 || got[0] != "Hello there." {
		t.Fatalf("miss must Speak once with frame.Text, got %v", got)
	}
	_ = d // no played for a last:false stream
}

func TestClipSayNoClipIDUnchanged(t *testing.T) {
	const ccid = "clip-absent"
	d, _, childConn, p, out, store := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)

	// A say WITHOUT clip_id must never touch the store, even when the
	// exact same text is warm under a key (today's path byte-identical).
	putClip(t, store, clipKeyHex, 0x41, 2)
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: "Just a sec.",
		Last: lastPtr(false),
	})
	buf := make([]byte, 1280)
	_ = out.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(out, buf); err != nil {
		t.Fatalf("read fallback audio: %v", err)
	}
	if got := fake.calls(); len(got) != 1 || got[0] != "Just a sec." {
		t.Fatalf("clip-less say must Speak once unchanged, got %v", got)
	}
	if buf[0] != 0x77 {
		t.Fatalf("clip-less say played clip bytes despite no clip_id")
	}
	_ = d
}

func TestClipRenderWarmNoSynth(t *testing.T) {
	const ccid = "clip-warm"
	d, _, childConn, p, _, store := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)
	putClip(t, store, clipKeyHex, 0x41, 1)

	sendKeys(t, childConn, ccid)
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventClipRender,
		Clips: []voiceframe.ClipSpec{{Key: clipKeyHex, Text: "Just a sec."}},
	})

	f := d.waitFrame(t)
	if f.Event != voiceframe.EventClipReady {
		t.Fatalf("frame = %+v, want clip_ready", f)
	}
	if f.CallID != ccid || f.Key != clipKeyHex || f.OK == nil || !*f.OK {
		t.Fatalf("clip_ready wire shape drift: %+v", f)
	}
	if n := fake.count(); n != 0 {
		t.Fatalf("warm key triggered %d Speak calls, want 0", n)
	}
}

func TestClipRenderMissRendersAndStores(t *testing.T) {
	const ccid = "clip-render"
	d, _, childConn, p, out, store := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)
	sendKeys(t, childConn, ccid)

	const key = "aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11aa11"
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventClipRender,
		Clips: []voiceframe.ClipSpec{{Key: key, Text: "Bye!"}},
	})

	f := d.waitFrame(t)
	if f.Event != voiceframe.EventClipReady || f.Key != key || f.OK == nil || !*f.OK {
		t.Fatalf("want clip_ready ok:true for %s, got %+v", key, f)
	}
	if got := fake.calls(); len(got) != 1 || got[0] != "Bye!" {
		t.Fatalf("miss must render frame text once, got %v", got)
	}
	if !store.Has(key) {
		t.Fatalf("render did not persist clip %s", key)
	}
	_, meta, err := store.Get(key)
	if err != nil {
		t.Fatalf("get rendered clip: %v", err)
	}
	if meta.SampleRate != 16000 {
		t.Fatalf("meta sample rate = %d, want canonical 16000 (A1)", meta.SampleRate)
	}
	if meta.Text != "Bye!" {
		t.Fatalf("meta text = %q, want the render text", meta.Text)
	}

	// The rendered clip now serves a later say with ZERO extra Speak:
	// render-then-replay end to end. The stored bytes are the fake's
	// 1280 x 0x77 pattern.
	before := fake.count()
	buf := make([]byte, 1280)
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: "Bye!",
		Last: lastPtr(false), ClipID: key,
	})
	_ = out.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(out, buf); err != nil {
		t.Fatalf("read replayed clip: %v", err)
	}
	if n := fake.count(); n != before {
		t.Fatalf("replay of a warm clip triggered %d extra Speak calls", n-before)
	}
	for _, b := range buf {
		if b != 0x77 {
			t.Fatalf("replayed bytes != the rendered fake audio")
		}
	}
}

func TestClipRenderGatedOnKeys(t *testing.T) {
	const ccid = "clip-gate"
	d, _, childConn, p, _, store := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)

	const key = "bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22bb22"
	// Render request BEFORE the keys frame: must queue, not synthesize.
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventClipRender,
		Clips: []voiceframe.ClipSpec{{Key: key, Text: "One sec."}},
	})
	time.Sleep(150 * time.Millisecond)
	if n := fake.count(); n != 0 {
		t.Fatalf("render started before keys: %d Speak calls (A3 gate broken)", n)
	}

	sendKeys(t, childConn, ccid)
	f := d.waitFrame(t)
	if f.Event != voiceframe.EventClipReady || f.Key != key || f.OK == nil || !*f.OK {
		t.Fatalf("queued render did not run after keys: %+v", f)
	}
	if n := fake.count(); n != 1 {
		t.Fatalf("post-keys render Speak calls = %d, want 1", n)
	}
	if !store.Has(key) {
		t.Fatalf("queued render did not persist")
	}
}

// serialGateTTS blocks every Speak on a shared release channel and
// tracks high-water concurrency, so a render loop that ever lets two
// Speak overlap fails maxFlight > 1.
type serialGateTTS struct {
	mu        sync.Mutex
	entered   int
	inFlight  int
	maxFlight int
	texts     []string
	release   chan struct{} // closed by the test to let current Speak(s) return
}

func newSerialGateTTS() *serialGateTTS {
	return &serialGateTTS{release: make(chan struct{})}
}

func (s *serialGateTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	s.mu.Lock()
	s.entered++
	s.inFlight++
	if s.inFlight > s.maxFlight {
		s.maxFlight = s.inFlight
	}
	s.texts = append(s.texts, text)
	release := s.release
	s.mu.Unlock()
	select {
	case <-release:
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
		pcm := bytes.Repeat([]byte{0x55}, 640)
		return io.NopCloser(bytes.NewReader(pcm)), nil
	case <-ctx.Done():
		s.mu.Lock()
		s.inFlight--
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

func (s *serialGateTTS) snapshot() (entered, maxFlight int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.entered, s.maxFlight
}

// unblockCurrent closes the gate the Speaks currently blocked on are
// reading, and swaps in a fresh gate for the next batch.
func (s *serialGateTTS) unblockCurrent() {
	s.mu.Lock()
	old := s.release
	s.release = make(chan struct{})
	s.mu.Unlock()
	close(old)
}

func TestClipRenderConcurrencyOne(t *testing.T) {
	const ccid = "clip-conc"
	d, _, childConn, p, _, _ := clipFixture(t, ccid)
	gate := newSerialGateTTS()
	swapTTS(p, gate)
	sendKeys(t, childConn, ccid)

	specs := []voiceframe.ClipSpec{
		{Key: strings.Repeat("a", 64), Text: "a"},
		{Key: strings.Repeat("b", 64), Text: "b"},
		{Key: strings.Repeat("c", 64), Text: "c"},
	}
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventClipRender, Clips: specs,
	})

	// The first render enters; the worker must never start a second
	// Speak while the first blocks.
	waitFor(t, func() bool { entered, _ := gate.snapshot(); return entered == 1 })
	time.Sleep(150 * time.Millisecond)
	if entered, max := gate.snapshot(); entered != 1 || max != 1 {
		t.Fatalf("render concurrency broken: entered=%d maxFlight=%d, want 1/1", entered, max)
	}

	// Drain the queue one render at a time. Each release is armed only
	// after the matching Speak has ENTERED (unblocking blind would let a
	// release land on the not-yet-created next channel).
	for i := range specs {
		waitFor(t, func() bool { entered, _ := gate.snapshot(); return entered == i+1 })
		gate.unblockCurrent()
	}
	waitFor(t, func() bool { entered, _ := gate.snapshot(); return entered == 3 })
	time.Sleep(100 * time.Millisecond)
	if entered, max := gate.snapshot(); entered != 3 || max != 1 {
		t.Fatalf("serial render failed: entered=%d maxFlight=%d, want 3/1", entered, max)
	}

	// Three clip_ready acks (FIFO by key).
	for _, spec := range specs {
		f := d.waitFrame(t)
		if f.Event != voiceframe.EventClipReady || f.Key != spec.Key || f.OK == nil || !*f.OK {
			t.Fatalf("clip_ready drift for %s: %+v", spec.Key, f)
		}
	}
}

// slowBlockTTS hangs Speak on ctx forever — the teardown test's provider:
// a render that survives pipeline close never returns.
type slowBlockTTS struct {
	entered chan struct{}
	once    sync.Once
}

func (s *slowBlockTTS) Speak(ctx context.Context, _ string) (io.ReadCloser, error) {
	s.once.Do(func() { close(s.entered) })
	<-ctx.Done()
	return nil, ctx.Err()
}

func TestClipRenderTeardownCancels(t *testing.T) {
	const ccid = "clip-teardown"
	d, _, childConn, p, _, store := clipFixture(t, ccid)
	block := &slowBlockTTS{entered: make(chan struct{})}
	swapTTS(p, block)
	sendKeys(t, childConn, ccid)

	const key = "cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33cc33"
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventClipRender,
		Clips: []voiceframe.ClipSpec{{Key: key, Text: "hangs"}},
	})
	// The render entered Speak and is blocked on the call ctx.
	select {
	case <-block.entered:
	case <-time.After(testPollDeadline):
		t.Fatal("render never started")
	}

	// End frame: the pipeline tears down; the blocked render must die
	// with it (adjudication A3: a pending render NEVER outlives teardown).
	d.sendLine(childConn, voiceframe.Frame{CallID: ccid, Event: voiceframe.EventEnd, Reason: "test"})
	waitFor(t, func() bool {
		select {
		case <-p.ctx.Done():
			return true
		default:
			return false
		}
	})
	// No clip landed: the store pair for the hung key must not exist.
	if store.Has(key) {
		t.Fatal("torn-down render persisted a clip")
	}
}

func TestClipBargeInClearsLikeSynth(t *testing.T) {
	const ccid = "clip-barge"
	d, _, childConn, p, out, store := clipFixture(t, ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)

	// Long cached clip: 40 chunks of 640 B at the paced 2 ms drain.
	pcm := putClip(t, store, clipKeyHex, 0x42, 40)
	_ = pcm

	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: "A long line.",
		Last: lastPtr(true), ClipID: clipKeyHex,
	})
	// Drain a few chunks so the stream is mid-flight, then clear.
	paceDrain(t, out, 4, 2*time.Millisecond)
	clearLine(t, d, childConn, ccid)

	// The interrupted clip stream must never emit played (identical to
	// the synth posture, TestNoPlayedOnBargeInAbort).
	time.Sleep(300 * time.Millisecond)
	for _, f := range drainFrames(d) {
		if f.Event == voiceframe.EventPlayed {
			t.Fatalf("played emitted after barge-in over a cached clip: %+v", f)
		}
	}
	if fake.count() != 0 {
		t.Fatalf("cached clip fell through to Speak: %+v", fake.calls())
	}
}

func TestClipRateResampleTo8k(t *testing.T) {
	const ccid = "clip-8k"
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	store := clipstore.NewStoreAt(filepath.Join(dir, "clips"))
	child := New(ChildConfig{
		TextSocketPath: filepath.Join(dir, "t.sock"),
		AudioDir:       dir,
		TokenReader:    strings.NewReader("tok\n"),
	})
	child.SetClipStore(store)

	// Injected resampler stub (production wires providers.ResampleS16Mono;
	// the voicechild package must stay jargo-free, so the seam is faked).
	var sawIn, sawOut int
	var sawMu sync.Mutex
	child.SetResampler(func(src io.Reader, inRate, outRate int) (io.ReadCloser, error) {
		sawMu.Lock()
		sawIn, sawOut = inRate, outRate
		sawMu.Unlock()
		// Stub conversion: 16k -> 8k is exactly half the samples.
		b, err := io.ReadAll(src)
		if err != nil {
			return nil, err
		}
		return io.NopCloser(bytes.NewReader(b[:len(b)/2])), nil
	})

	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	// PCMU@8k negotiated call.
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSetup, Codec: "PCMU", SampleRate: 8000,
	})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() {
		_ = in.Close()
		_ = out.Close()
		_ = childConn.Close()
		if err := <-runDone; err != nil {
			t.Errorf("Run returned error on close: %v", err)
		}
	})
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline(ccid)
	fake := newRecordingTTS()
	swapTTS(p, fake)

	pcm := putClip(t, store, clipKeyHex, 0x43, 2) // 1280 bytes @16k

	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: "Still there?",
		Last: lastPtr(true), ClipID: clipKeyHex,
	})
	// 1280 -> 640 bytes after the 16k->8k stub halving, paced 160 B/chunk.
	got := make([]byte, 640)
	_ = out.SetReadDeadline(time.Now().Add(10 * time.Second))
	if _, err := io.ReadFull(out, got); err != nil {
		t.Fatalf("read resampled clip audio: %v", err)
	}
	if !bytes.Equal(got, pcm[:640]) {
		t.Fatalf("resampled prefix drift: %x", got[:8])
	}
	sawMu.Lock()
	inR, outR := sawIn, sawOut
	sawMu.Unlock()
	if inR != 16000 || outR != 8000 {
		t.Fatalf("resampler called with %d->%d, want 16000->8000 (A1)", inR, outR)
	}
	if fake.count() != 0 {
		t.Fatalf("resampled hit must not fall through to Speak: %d calls", fake.count())
	}
}

func TestRefHashDecodeContract(t *testing.T) {
	// A4: "" -> nil (non-clone); valid hex -> the raw sha256 bytes the
	// daemon's planner fed ClipKey; malformed hex degrades to nil rather
	// than failing the call.
	if got := decodeTTSRefHash(""); got != nil {
		t.Fatalf("empty refHash = %x, want nil", got)
	}
	sum := sha256.Sum256([]byte("reference-audio-bytes"))
	hx := hex.EncodeToString(sum[:])
	got := decodeTTSRefHash(hx)
	if !bytes.Equal(got, sum[:]) {
		t.Fatalf("decoded refHash != sha256(reference bytes)")
	}
	if decodeTTSRefHash("zz-no-hex") != nil {
		t.Fatal("malformed hex must degrade to nil")
	}

	// Byte-equality with the daemon side is structural: both sides call
	// the SAME clipstore.ClipKey with the same five inputs (leaf 02's
	// planner and any future child-side recomputation), so agreement is
	// pinned by the frozen vectors in internal/voice/clipstore/key_test.go.
	k := clipstore.ClipKey("Just a sec.", "qwen3", "qwen3-tts-flash", "MyVoice-1", []byte("REF"))
	if k != clipKeyHex {
		t.Fatalf("ClipKey drifted from the frozen C1 vector: %s", k)
	}
}
