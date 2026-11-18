package voicechild

// Leaf 00T gate tests (Task 5) — child-side clip render + playback,
// pinning root Contract C3 behavior for the voice child (leaf 03 owns
// the implementation):
//
//   - C3 say frame: {"event":"say","call_id":"...","text":"Just a
//     sec.","last":false,"clip_id":"<64 hex>"} — clip_id present + store
//     hit: play cached PCM (ZERO provider calls); clip_id present + miss
//     or absent: today's live Speak(text) path, byte-identical fallback.
//   - C3 clip_render: {"event":"clip_render","call_id":"<ccid>","clips":
//     [{"key":"<64 hex>","text":"Just a sec."}]} — render misses through
//     the installed TTSProvider, persist via clipstore.
//   - C3 clip_ready: {"event":"clip_ready","call_id":"<ccid>","key":"<64
//     hex>","ok":true|false} — one emission per render job result.
//
// The store is the REAL internal/voice/clipstore package rooted at
// $MEEPT_HOME/voice_clips (t.Setenv MEEPT_HOME per test), so daemon-side
// and child-side key agreement is exercised against the C1 mirror in
// internal/voice/clipstore, not a fixture.
//
// EXPECTED RED (pre-impl, gate leaf 00T): every clip_render say/watch
// assertion fails with a timeout ("no clip_ready frame ...") because the
// child does not yet handle clip events or read ClipID — missing-
// implementation reasons, never compile errors (Frame.ClipID /
// EventClipRender / EventClipReady land with leaf 03; the voiceframe
// gate file pins their wire shapes).

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice/clipstore"
	"github.com/caimlas/meept/internal/voiceframe"
)

// gateC1Key mirrors Contract C1 independently of the implementation
// helper (same construction as the clipstore gate test).
func gateC1Key(text, vendor, model, voice string, refHash []byte) string {
	inner := sha256.Sum256(refHash)
	preimage := "v1" + "\x1f" + text + "\x1f" + vendor + "\x1f" + model + "\x1f" + voice + "\x1f" + hex.EncodeToString(inner[:])
	sum := sha256.Sum256([]byte(preimage))
	return hex.EncodeToString(sum[:])
}

const (
	gateVendor = "qwen3"
	gateModel  = "gate-model"
	gateVoice  = "gate-voice"
)

// ---- fake provider (call recorder, injected via C3 factories) ----

type gateTTS struct {
	mu       sync.Mutex
	texts    []string
	out      []byte
	failWith error
}

func newGateTTS(n int) *gateTTS {
	pcm := make([]byte, n)
	for i := range pcm {
		pcm[i] = byte(0x40 + i%7)
	}
	return &gateTTS{out: pcm}
}

func (t *gateTTS) Speak(_ context.Context, text string) (io.ReadCloser, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.texts = append(t.texts, text)
	if t.failWith != nil {
		return nil, t.failWith
	}
	// Copy: readers may be drained concurrently by render + say paths.
	return io.NopCloser(bytes.NewReader(append([]byte(nil), t.out...))), nil
}

func (t *gateTTS) calls() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.texts)
}

func (t *gateTTS) lastText() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	if len(t.texts) == 0 {
		return ""
	}
	return t.texts[len(t.texts)-1]
}

func (t *gateTTS) setFail(err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	t.failWith = err
}

type gateSTT struct{}

func (gateSTT) Stream(ctx context.Context, pcm <-chan []byte) (<-chan Transcript, error) {
	out := make(chan Transcript)
	go func() {
		for {
			select {
			case <-ctx.Done():
				close(out)
				return
			case _, ok := <-pcm:
				if !ok {
					close(out)
					return
				}
			}
		}
	}()
	return out, nil
}

// gateChild wires a real Child whose C3 provider factories hand out the
// recorder, so EVERY provider construction (say synth AND clip render)
// flows through the same counted fake. Returns the fake daemon, the
// daemon-side text conn, the pipeline, and the .out socket.
type gateHarness struct {
	d    *fakeDaemon
	conn net.Conn
	p    *pipeline
	out  net.Conn
}

func newGateChild(t *testing.T, ccid string, tts *gateTTS) *gateHarness {
	t.Helper()
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair(ccid)
	child := newTestChild(t, d, "tok")
	child.SetProviderFactories(
		func(_ ProviderConfig, _ string) (STTProvider, error) { return gateSTT{}, nil },
		func(_ ProviderConfig, _ string) (TTSProvider, error) { return tts, nil },
	)
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()
	d.sendLine(childConn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll(ccid)
	t.Cleanup(func() {
		_ = in.Close()  //nolint:errcheck // teardown
		_ = out.Close() //nolint:errcheck // teardown
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
	// Keys line (identity the render must reuse for C1 agreement). The
	// child processes frames in wire order, so anything sent after this
	// sees a constructed provider.
	gateSendRaw(t, childConn, `{"call_id":"`+ccid+`","event":"keys","stt_key":"s","tts_key":"",`+
		`"tts_vendor":"`+gateVendor+`","tts_model":"`+gateModel+`","tts_voice":"`+gateVoice+`"}`)
	// keys processed before the next send (single reader goroutine +
	// write ordering: the child's text reader is sequential).
	return &gateHarness{d: d, conn: childConn, p: p, out: out}
}

func gateSendRaw(t *testing.T, w interface{ Write([]byte) (int, error) }, line string) {
	t.Helper()
	if _, err := w.Write([]byte(line + "\n")); err != nil {
		t.Fatalf("raw write: %v", err)
	}
}

// sendSay writes a say frame (struct path: exercises Frame.ClipID
// encoding end-to-end).
func (g *gateHarness) sendSay(ccid, text, clipID string, last bool) {
	g.d.sendLine(g.conn, voiceframe.Frame{
		CallID: ccid, Event: voiceframe.EventSay, Text: text,
		Last: new(last), ClipID: clipID,
	})
}

func (g *gateHarness) sendClipRender(ccid string, specs ...struct {
	key  string
	text string
}) {
	var b strings.Builder
	b.WriteString(`{"event":"clip_render","call_id":"` + ccid + `","clips":[`)
	for i, s := range specs {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"key":"` + s.key + `","text":"` + s.text + `"}`)
	}
	b.WriteString(`]}`)
	if _, err := g.conn.Write(append([]byte(b.String()), '\n')); err != nil {
		panic("clip_render raw write: " + err.Error()) //nolint:forbidigo // test helper
	}
}

// gateReady decodes one clip_ready wire line.
type gateReady struct {
	CallID string `json:"call_id"`
	Key    string `json:"key"`
	OK     bool   `json:"ok"`
	raw    string
}

// ---- GH-1: say with clip_id HIT => zero provider calls; audio bytes ==
// the stored PCM (leaf 03 checklist).
func TestClipGateSayHitZeroProviderCallsAndStoredBytes(t *testing.T) {
	const ccid = "clipgate-hit"
	t.Setenv("MEEPT_HOME", t.TempDir())

	stored := bytes.Repeat([]byte{0xAB, 0x00}, 640) // 1280 B = 2 x 640-chunks
	key := gateC1Key("Just a sec.", gateVendor, gateModel, gateVoice, nil)
	st := clipstore.NewStore()
	if err := st.Put(key, clipstore.Meta{
		Key: key, Text: "Just a sec.", Vendor: gateVendor, Model: gateModel, Voice: gateVoice,
		SampleRate: 16000, Bytes: int64(len(stored)),
	}, bytes.NewReader(stored)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tts := newGateTTS(3 * 640)
	g := newGateChild(t, ccid, tts)
	g.sendSay(ccid, "Just a sec.", key, false)

	got := gateDrain(t, g.out, 2, 640)
	if !bytes.Equal(got, stored) {
		t.Fatalf("clip-hit audio != stored pcm (%d bytes vs %d)", len(got), len(stored))
	}
	if n := tts.calls(); n != 0 {
		t.Fatalf("clip hit made %d provider Speak calls, want 0 (R1: zero live synth on hit)", n)
	}
}

// ---- GH-2: say with clip_id MISS => provider called EXACTLY ONCE with
// frame.Text; today's byte-identical fallback, NO opportunistic
// backfill (orchestrator review item).
func TestClipGateSayMissFallsBackToLiveSpeak(t *testing.T) {
	const ccid = "clipgate-miss"
	t.Setenv("MEEPT_HOME", t.TempDir())

	tts := newGateTTS(2 * 640)
	g := newGateChild(t, ccid, tts)

	bogus := strings.Repeat("f", 32) + strings.Repeat("0", 32)
	g.sendSay(ccid, "One moment.", bogus, false)

	got := gateDrain(t, g.out, 2, 640)
	if !bytes.Equal(got, tts.out) {
		t.Fatalf("clip-miss fallback audio != provider output — live Speak path must stay byte-identical")
	}
	if n := tts.calls(); n != 1 {
		t.Fatalf("clip miss made %d Speak calls, want exactly 1", n)
	}
	if tx := tts.lastText(); tx != "One moment." {
		t.Fatalf("fallback Speak text = %q, want frame.Text %q", tx, "One moment.")
	}
	// Missed clip must NOT be backfilled opportunistically.
	if st := clipstore.NewStore(); st.Has(bogus) {
		t.Error("clip miss opportunistically backfilled the store (decided out of scope)")
	}
}

// ---- GH-3: clip_render for a MISS key => one provider synth of the
// spec text, stored under the frame's key, and exactly one clip_ready
// ok:true on the text socket with the key echoed 64-hex verbatim.
func TestClipGateRenderMissStoresAndEmitsClipReady(t *testing.T) {
	const ccid = "clipgate-render"
	t.Setenv("MEEPT_HOME", t.TempDir())

	text := "Still there?"
	wantKey := gateC1Key(text, gateVendor, gateModel, gateVoice, nil)

	tts := newGateTTS(4 * 640)
	g := newGateChild(t, ccid, tts)
	g.sendClipRender(ccid, struct {
		key  string
		text string
	}{wantKey, text})

	line := gateWaitReady(t, g.d, ccid, wantKey)
	if !line.OK {
		t.Fatalf("clip_ready ok:false for a successful render: %s", line.raw)
	}
	if n := tts.calls(); n != 1 {
		t.Fatalf("render made %d provider calls, want 1", n)
	}
	if tx := tts.lastText(); tx != text {
		t.Fatalf("render synth text = %q, want spec text %q", tx, text)
	}
	st := clipstore.NewStore()
	if !st.Has(wantKey) {
		t.Fatalf("clip not in store after ok:true clip_ready (C2 pair under the frame key)")
	}
	rc, meta, err := st.Get(wantKey)
	if err != nil {
		t.Fatalf("Get after render: %v", err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // test sink
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read stored pcm: %v", err)
	}
	if len(got) == 0 {
		t.Fatal("stored pcm empty after render")
	}
	if meta.Text != text {
		t.Fatalf("stored meta text = %q, want %q (C2 meta)", meta.Text, text)
	}
}

// ---- GH-4: clip_render for an EXISTING key => zero synth work (leaf 03
// checklist). AMBIGUITY (reported): C3 does not pin whether an existing
// key re-emits clip_ready; leaf 03's landed shape skips silently. Pinned
// here only: NO provider calls and the stored bytes untouched. If a
// clip_ready arrives for the existing key it must be ok:true.
func TestClipGateRenderExistingKeyDoesNoSynthWork(t *testing.T) {
	const ccid = "clipgate-existing"
	t.Setenv("MEEPT_HOME", t.TempDir())

	key := gateC1Key("Talk soon.", gateVendor, gateModel, gateVoice, nil)
	want := bytes.Repeat([]byte{0x01, 0x02}, 320)
	st := clipstore.NewStore()
	if err := st.Put(key, clipstore.Meta{Key: key, Text: "Talk soon.", SampleRate: 16000, Bytes: int64(len(want))}, bytes.NewReader(want)); err != nil {
		t.Fatalf("seed: %v", err)
	}

	tts := newGateTTS(4 * 640)
	g := newGateChild(t, ccid, tts)
	g.sendClipRender(ccid, struct {
		key  string
		text string
	}{key, "Talk soon."})

	// Give any (incorrect) render every chance to fire, then assert none
	// happened and the stored pair is untouched.
	time.Sleep(1500 * time.Millisecond)
	if n := tts.calls(); n != 0 {
		t.Fatalf("existing key caused %d provider calls, want 0 (C2 hit = no synth)", n)
	}
	rc, _, err := st.Get(key)
	if err != nil {
		t.Fatalf("existing clip lost after render pass: %v", err)
	}
	defer func() { _ = rc.Close() }() //nolint:errcheck // test sink
	got, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("existing clip bytes rewritten by a render pass")
	}
	for _, r := range gateDrainReadys(g.d, ccid) {
		if r.Key == key && !r.OK {
			t.Fatalf("clip_ready ok:false for an existing (valid) key: %s", r.raw)
		}
	}
}

// ---- GH-5: render FAILURE => clip_ready ok:false and NOTHING stored;
// the turn lane is never touched (a later live say still synthesizes).
func TestClipGateRenderFailureEmitsClipReadyNotOK(t *testing.T) {
	const ccid = "clipgate-fail"
	t.Setenv("MEEPT_HOME", t.TempDir())

	text := "Goodbye!"
	wantKey := gateC1Key(text, gateVendor, gateModel, gateVoice, nil)

	tts := newGateTTS(640)
	g := newGateChild(t, ccid, tts)
	tts.setFail(errors.New("clipgate: synth down"))
	g.sendClipRender(ccid, struct {
		key  string
		text string
	}{wantKey, text})

	line := gateWaitReady(t, g.d, ccid, wantKey)
	if line.OK {
		t.Fatalf("clip_ready ok:true after a failed render: %s", line.raw)
	}
	if st := clipstore.NewStore(); st.Has(wantKey) {
		t.Fatal("failed render still stored a clip")
	}
	// Turn lane untouched: the failure did not tear down the pipeline and
	// a subsequent live say synthesizes through the (repaired) provider.
	tts.setFail(nil)
	g.sendSay(ccid, "fallback still works", "", false)
	gateDrain(t, g.out, 1, 640)
}

// ---- helpers ----

func gateWaitReady(t *testing.T, d *fakeDaemon, ccid, key string) gateReady {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for time.Now().Before(deadline) {
		line := gatePopIfReady(d)
		if line == "" {
			select {
			case <-d.frameSignal:
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		var probe struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil || probe.Event != "clip_ready" {
			continue // tolerate unrelated child frames (played etc.)
		}
		var cr gateReady
		if err := json.Unmarshal([]byte(line), &cr); err != nil {
			t.Fatalf("clip_ready line malformed: %v (%s)", err, line)
		}
		cr.raw = line
		if cr.CallID != ccid {
			t.Fatalf("clip_ready call_id = %q, want %q", cr.CallID, ccid)
		}
		if cr.Key != key {
			t.Fatalf("clip_ready key = %q, want %q (C1 key echoed verbatim, 64-hex)", cr.Key, key)
		}
		if _, err := hex.DecodeString(cr.Key); err != nil || len(cr.Key) != 64 {
			t.Fatalf("clip_ready key not 64-hex: %q", cr.Key)
		}
		return cr
	}
	t.Fatalf("no clip_ready frame for key %s within 30s (clip_render handling missing = RED for implementation reasons)", key[:12])
	return gateReady{}
}

func gateDrainReadys(d *fakeDaemon, ccid string) []gateReady {
	d.mu.Lock()
	lines := append([]string(nil), d.rawQueue...)
	d.rawQueue = nil
	d.mu.Unlock()
	var out []gateReady
	for _, line := range lines {
		var probe struct {
			Event string `json:"event"`
		}
		if err := json.Unmarshal([]byte(line), &probe); err != nil || probe.Event != "clip_ready" {
			continue
		}
		var cr gateReady
		if err := json.Unmarshal([]byte(line), &cr); err != nil {
			continue
		}
		cr.raw = line
		if cr.CallID == ccid {
			out = append(out, cr)
		}
	}
	return out
}

func gatePopIfReady(d *fakeDaemon) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.rawQueue) == 0 {
		return ""
	}
	line := d.rawQueue[0]
	d.rawQueue = d.rawQueue[1:]
	return line
}

// gateDrain reads exactly chunks x size bytes off the .out socket.
func gateDrain(t *testing.T, out interface {
	Read([]byte) (int, error)
	SetReadDeadline(time.Time) error
}, chunks, size int) []byte {
	t.Helper()
	got := make([]byte, 0, chunks*size)
	buf := make([]byte, size)
	for i := range chunks {
		_ = out.SetReadDeadline(time.Now().Add(30 * time.Second))
		n, err := io.ReadFull(out, buf)
		if err != nil {
			t.Fatalf("drain chunk %d (got %d bytes): %v", i, n, err)
		}
		got = append(got, buf...)
	}
	return got
}
