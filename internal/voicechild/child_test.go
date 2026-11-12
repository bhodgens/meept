package voicechild

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voiceframe"
)

// shortTempDir returns a short-lived temp dir. macOS caps unix socket
// paths at 104 bytes and $TMPDIR on macOS is itself ~70 bytes, so tests
// use /tmp directly with a short prefix.
func shortTempDir(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("/tmp", "vc")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if len(dir) > 60 {
		t.Fatalf("temp dir %s too long for unix socket paths", dir)
	}
	return dir
}

// fakeDaemon stands in for the daemon: a text socket listener recording
// frames the child emits (decoded, plus the raw lines for wire-shape
// assertions), and per-call audio pair listeners. The child's FIRST line
// is its start-token proof (never a frame), so serveText consumes it into
// authLine and records only subsequent lines as frames.
type fakeDaemon struct {
	t           *testing.T
	dir         string
	textLn      net.Listener
	mu          sync.Mutex
	queue       []voiceframe.Frame
	rawQueue    []string
	authLine    string // first child line: the start-token proof
	frameSignal chan struct{}
	inLn        map[string]net.Listener
	outLn       map[string]net.Listener
	accIn       map[string]net.Conn // accepted C2 conns by ccid (acceptAll is idempotent)
	accOut      map[string]net.Conn
}

func newFakeDaemon(t *testing.T, dir string) *fakeDaemon {
	t.Helper()
	textPath := filepath.Join(dir, "t.sock")
	ln, err := net.Listen("unix", textPath)
	if err != nil {
		t.Fatalf("listen text: %v", err)
	}
	d := &fakeDaemon{
		t:           t,
		dir:         dir,
		textLn:      ln,
		frameSignal: make(chan struct{}, 64),
		inLn:        make(map[string]net.Listener),
		outLn:       make(map[string]net.Listener),
		accIn:       make(map[string]net.Conn),
		accOut:      make(map[string]net.Conn),
	}
	return d
}

// listenPair creates audio pair listeners for ccid.
func (d *fakeDaemon) listenPair(ccid string) {
	d.t.Helper()
	inPath := filepath.Join(d.dir, ccid+".in")
	outPath := filepath.Join(d.dir, ccid+".out")
	inLn, err := net.Listen("unix", inPath)
	if err != nil {
		d.t.Fatalf("listen in %s: %v", inPath, err)
	}
	outLn, err := net.Listen("unix", outPath)
	if err != nil {
		d.t.Fatalf("listen out %s: %v", outPath, err)
	}
	d.mu.Lock()
	d.inLn[ccid] = inLn
	d.outLn[ccid] = outLn
	d.mu.Unlock()
}

// acceptAll accepts one connection per audio listener (child dials both).
// Idempotent per ccid: the child dials each socket exactly once, so a
// second call returns the conns already accepted (some call sites accept
// early and re-acquire `in` later for writes).
func (d *fakeDaemon) acceptAll(ccid string) (in, out net.Conn) {
	d.t.Helper()
	d.mu.Lock()
	inLn, outLn := d.inLn[ccid], d.outLn[ccid]
	in, out = d.accIn[ccid], d.accOut[ccid]
	d.mu.Unlock()
	if in != nil && out != nil {
		return in, out
	}
	var err error
	if in == nil {
		in, err = inLn.Accept()
		if err != nil {
			d.t.Fatalf("accept in: %v", err)
		}
		d.mu.Lock()
		d.accIn[ccid] = in
		d.mu.Unlock()
	}
	if out == nil {
		out, err = outLn.Accept()
		if err != nil {
			d.t.Fatalf("accept out: %v", err)
		}
		d.mu.Lock()
		d.accOut[ccid] = out
		d.mu.Unlock()
	}
	return in, out
}

// sendLine writes one frame line to the child connection.
func (d *fakeDaemon) sendLine(conn net.Conn, f voiceframe.Frame) {
	d.t.Helper()
	b, err := voiceframe.EncodeFrame(f)
	if err != nil {
		d.t.Fatalf("encode: %v", err)
	}
	if _, err := conn.Write(b); err != nil {
		d.t.Fatalf("write: %v", err)
	}
}

// waitFrame waits for and pops the next child-emitted frame (FIFO).
func (d *fakeDaemon) waitFrame(t *testing.T) voiceframe.Frame {
	t.Helper()
	select {
	case <-d.frameSignal:
	case <-time.After(testPollDeadline):
		d.mu.Lock()
		evs := make([]string, len(d.queue))
		for i, f := range d.queue {
			evs[i] = f.Event
		}
		d.mu.Unlock()
		t.Fatalf("timed out waiting for child frame after %s; queue=%v", testPollDeadline, evs)
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.queue[0]
	d.queue = d.queue[1:]
	return f
}

// serveText accepts the child's text connection and records frames it
// sends. The FIRST line is the child's start-token proof (the daemon
// seam consumes it; it is never a C1 frame), so it is captured into
// authLine and excluded from the frame/raw queues.
func (d *fakeDaemon) serveText() net.Conn {
	d.t.Helper()
	conn, err := d.textLn.Accept()
	if err != nil {
		d.t.Fatalf("accept text: %v", err)
	}
	go func() {
		br := bufio.NewScanner(conn)
		first := true
		for br.Scan() {
			if first {
				first = false
				d.mu.Lock()
				d.authLine = br.Text()
				d.mu.Unlock()
				continue
			}
			d.mu.Lock()
			d.rawQueue = append(d.rawQueue, br.Text())
			d.mu.Unlock()
			f, err := voiceframe.DecodeFrame(br.Bytes())
			if err != nil {
				continue
			}
			d.mu.Lock()
			d.queue = append(d.queue, f)
			d.mu.Unlock()
			select {
			case d.frameSignal <- struct{}{}:
			default:
			}
		}
	}()
	return conn
}

// waitAuthLine waits for the child's first line (the start-token proof)
// and returns it.
func (d *fakeDaemon) waitAuthLine(t *testing.T) string {
	t.Helper()
	deadline := time.Now().Add(testPollDeadline)
	for time.Now().Before(deadline) {
		d.mu.Lock()
		line := d.authLine
		d.mu.Unlock()
		if line != "" {
			return line
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("child never sent its start-token proof line")
	return ""
}

// waitRawLine waits for and pops the next raw child-emitted line. Wire
// shape is a contract (byte-exact JSON keys), so some assertions must see
// the line itself, not the decoded struct.
func (d *fakeDaemon) waitRawLine(t *testing.T) string {
	t.Helper()
	select {
	case <-d.frameSignal:
	case <-time.After(testPollDeadline):
		t.Fatalf("timed out waiting for child frame")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if len(d.rawQueue) == 0 {
		t.Fatalf("no raw child line queued")
	}
	line := d.rawQueue[0]
	d.rawQueue = d.rawQueue[1:]
	return line
}

// newTestChild wires a Child against the fake daemon with a token on stdin.
func newTestChild(t *testing.T, d *fakeDaemon, token string) *Child {
	t.Helper()
	return New(ChildConfig{
		TextSocketPath: filepath.Join(d.dir, "t.sock"),
		AudioDir:       d.dir,
		TokenReader:    strings.NewReader(token + "\n"),
	})
}

// T2: token from stdin (never argv/env), setup registers a pipeline.
func TestSetupRegistersPipeline(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc1")

	child := newTestChild(t, d, "tok-123")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	// serveText accepts only after Run has started dialing.
	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc1", Event: voiceframe.EventSetup,
		From: "+15550001", To: "+15550002", Direction: "inbound",
		Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("cc1")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()

	waitFor(t, func() bool { return child.pipelineCount() == 1 })

	if p, ok := child.getPipeline("cc1"); !ok || p == nil {
		t.Fatalf("pipeline for cc1 not registered")
	}
	if child.tokenLoad() != "tok-123" {
		t.Fatalf("token not read from stdin reader")
	}
	// The daemon-side seam consumes the child's FIRST line as the
	// start-token proof (it never reaches the frame queue).
	if got := d.waitAuthLine(t); got != "tok-123" {
		t.Fatalf("first child line = %q, want the token proof tok-123", got)
	}

	// End tears the pipeline down (T2).
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc1", Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 0 })

	ctxCancelErr := func() error {
		// Shut down: cancel the run via context by closing the daemon conn.
		_ = childConn.Close()
		select {
		case err := <-runDone:
			return err
		case <-time.After(testPollDeadline):
			t.Fatalf("Run did not return after socket close")
			return nil
		}
	}()
	if ctxCancelErr != nil {
		t.Fatalf("Run returned error: %v", ctxCancelErr)
	}
}

// T3: stub transcript final -> Frame{prompt, last:true}; partial -> last:false.
func TestPromptFrames(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()
	// Accept returning does not mean Run has published the text conn:
	// Dial unblocks Accept, then the child goroutine assigns c.conn.
	// emitPrompt before that assignment is "text socket not connected".
	waitFor(t, child.textConnected)

	// no setup frame: emit directly against the connected child
	if err := child.emitPrompt(context.Background(), "cc9", "partial words", false); err != nil {
		t.Fatalf("emitPrompt partial: %v", err)
	}
	if err := child.emitPrompt(context.Background(), "cc9", "final words", true); err != nil {
		t.Fatalf("emitPrompt final: %v", err)
	}

	got := d.waitFrame(t)
	if got.Event != voiceframe.EventPrompt || got.CallID != "cc9" || got.Text != "partial words" {
		t.Fatalf("partial frame wrong: %+v", got)
	}
	if got.Last != nil {
		t.Fatalf("partial prompt must omit last, got %v", *got.Last)
	}
	got = d.waitFrame(t)
	if got.Text != "final words" || got.Last == nil || !*got.Last {
		t.Fatalf("final frame wrong: %+v", got)
	}

	// Byte-exact C1 shape check.
	b, encErr := voiceframe.EncodeFrame(got)
	if encErr != nil {
		t.Fatalf("encode final prompt frame: %v", encErr)
	}
	want := `{"call_id":"cc9","event":"prompt","text":"final words","last":true}` + "\n"
	if string(b) != want {
		t.Fatalf("final prompt wire bytes:\n got %s\nwant %s", b, want)
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	runErr := <-runDone
	if runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
}

// T4: say -> stub TTS PCM reaches the .out socket; clear drops the rest.
func TestSayClear(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc2")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc2", Event: voiceframe.EventSetup,
		Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("cc2")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline("cc2")
	gate := newGatedTTS()
	swapTTS(p, gate)

	d.sendLine(childConn, voiceframe.Frame{CallID: "cc2", Event: voiceframe.EventSay, Text: "hello there", Last: lastPtr(true)}) //nolint:modernize // newexpr would emit new(bool)=false; true is intended
	gotFirst := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 640)
		n, _ := io.ReadFull(out, buf)
		gotFirst <- buf[:n]
	}()

	var first []byte
	select {
	case first = <-gotFirst:
	case <-time.After(testPollDeadline):
		t.Fatalf("no audio on .out socket after say")
	}
	if len(first) != 640 {
		t.Fatalf("first chunk = %d bytes, want 640 (20ms L16@16k)", len(first))
	}
	// First chunk content came from the gated reader (fill byte 1).
	if first[0] != 1 {
		t.Fatalf("first chunk content unexpected: %d", first[0])
	}

	// clear while playback is parked between chunks: the pending chunk is
	// dropped, writes stop, .out stays silent.
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc2", Event: voiceframe.EventClear})
	waitFor(t, p.isDropped)
	close(gate.releaseSecond) // let the reader offer the second chunk

	_ = out.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
	drain := make([]byte, 1280)
	n, _ := out.Read(drain)
	if n != 0 {
		t.Fatalf("clear did not stop .out writes: got %d extra bytes", n)
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
}

// T5: caller speech during playback emits interrupt exactly once.
func TestInterrupt(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("cc3")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	childConn := d.serveText()

	d.sendLine(childConn, voiceframe.Frame{
		CallID: "cc3", Event: voiceframe.EventSetup,
		Codec: "L16", SampleRate: 16000,
	})
	in, out := d.acceptAll("cc3")
	defer func() { _ = in.Close() }()
	defer func() { _ = out.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })
	p, _ := child.getPipeline("cc3")
	gate := newGatedTTS()
	swapTTS(p, gate)

	// Start playback through the real say path; the gated TTS parks the
	// stream after the first chunk, so playback stays active.
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc3", Event: voiceframe.EventSay, Text: "long utterance", Last: lastPtr(true)}) //nolint:modernize // newexpr would emit new(bool)=false; true is intended
	select {
	case <-gate.enteredPlay:
	case <-time.After(testPollDeadline):
		t.Fatalf("playback never started")
	}
	waitFor(t, p.isSpeaking)

	// Barge-in: VAD-during-playback stub signal fires.
	if !p.noteBargeIn() {
		t.Fatalf("first barge-in during playback must arm interrupt")
	}
	if err := child.emitInterrupt(context.Background(), "cc3"); err != nil {
		t.Fatalf("emitInterrupt: %v", err)
	}
	got := d.waitFrame(t)
	if got.Event != voiceframe.EventInterrupt || got.CallID != "cc3" {
		t.Fatalf("interrupt frame wrong: %+v", got)
	}

	// Second VAD hit in the same barge-in must NOT re-emit.
	if p.noteBargeIn() {
		t.Fatalf("interrupt must fire once per barge-in")
	}

	// New say restarts playback and re-arms (the inline re-arm in
	// playbackStream: interrupted/rmsRun/speaking reset under playMu).
	p.playMu.Lock()
	p.interrupted = false
	p.rmsRun = 0
	p.rmsEnergyRun = 0
	p.speaking = true
	p.playMu.Unlock()
	if !p.noteBargeIn() {
		t.Fatalf("barge-in after playback restart must arm interrupt again")
	}

	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	if runErr := <-runDone; runErr != nil {
		t.Fatalf("Run returned error on close: %v", runErr)
	}
}

// T6: keys frame formatting redacts key material.
func TestKeysRedaction(t *testing.T) {
	k := KeysFrame{CallID: "cc4", STTKey: "sk-secret-stt-123", TTSKey: "sk-secret-tts-456"}
	s := k.String()
	for _, secret := range []string{"sk-secret-stt-123", "sk-secret-tts-456"} {
		if strings.Contains(s, secret) {
			t.Fatalf("String() leaked key material: %s", s)
		}
	}
	if !strings.Contains(s, Redacted) || !strings.Contains(s, "cc4") {
		t.Fatalf("String() missing redaction marker or call id: %s", s)
	}

	// The struct feeds loggers directly; simulate a log-formatted rendering.
	formatted := fmt.Sprintf("%v", k)
	if strings.Contains(formatted, "sk-secret") {
		t.Fatalf("%%v formatting leaked key material: %s", formatted)
	}
}

// TestPerFrameErrorKeepsChildAlive (audit fix 4): one bad frame — here a
// setup whose audio dial fails (the .out socket listener never exists),
// then a malformed line, then a keys decode failure — must NEVER end
// serve/Run. Every other live call keeps working; Run exits only when the
// text socket itself dies. This pins the 'fail the call, not the child'
// posture that handleKeys already implemented for construction failures.
func TestPerFrameErrorKeepsChildAlive(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	d.listenPair("good")

	child := newTestChild(t, d, "tok")
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()
	childConn := d.serveText()

	// A healthy call first.
	d.sendLine(childConn, voiceframe.Frame{
		CallID: "good", Event: voiceframe.EventSetup, Codec: "L16", SampleRate: 16000,
	})
	gin, gout := d.acceptAll("good")
	defer func() { _ = gin.Close() }()
	defer func() { _ = gout.Close() }()
	waitFor(t, func() bool { return child.pipelineCount() == 1 })

	// 1) setup for a call whose audio pair cannot be dialed: the
	// "deaddial" socket files are never listened on. Pre-fix this
	// error propagated out of serve into Run and os.Exit'd the child.
	d.sendLine(childConn, voiceframe.Frame{
		CallID: "deaddial", Event: "setup", Codec: "L16", SampleRate: 16000,
	})
	// Give the handler time to fail; the child must still be serving.
	time.Sleep(200 * time.Millisecond)

	// 2) a malformed JSON line: decode failure.
	if _, err := childConn.Write([]byte("{not json\n")); err != nil {
		t.Fatalf("write malformed: %v", err)
	}
	// 3) a keys line that cannot decode.
	sendRaw(t, childConn, `{"call_id":"good","event":"keys","stt_key":`)

	time.Sleep(200 * time.Millisecond)

	// The child is alive: the healthy pipeline survived all three frames
	// and a fresh say on it still produces audio.
	if _, ok := child.getPipeline("good"); !ok {
		t.Fatalf("healthy pipeline gone after per-frame errors")
	}
	d.sendLine(childConn, voiceframe.Frame{CallID: "good", Event: voiceframe.EventSay, Text: "still alive", Last: lastPtr(true)}) //nolint:modernize // newexpr would emit new(bool)=false; true is intended
	first := make(chan int, 1)
	go func() {
		_ = gout.SetReadDeadline(time.Now().Add(testPollDeadline))
		buf := make([]byte, 640)
		n, _ := gout.Read(buf)
		first <- n
	}()
	select {
	case n := <-first:
		if n != 640 {
			t.Fatalf("surviving call got %d bytes, want 640", n)
		}
	case <-time.After(testPollDeadline):
		t.Fatalf("FIX 4 VIOLATION: child stopped serving after a bad frame (or the whole suite died with it)")
	}

	// A second failing setup (empty call_id) — also non-fatal.
	d.sendLine(childConn, voiceframe.Frame{CallID: "", Event: voiceframe.EventSetup})
	time.Sleep(100 * time.Millisecond)

	// Run returns nil ONLY when the text socket dies.
	if err := childConn.Close(); err != nil {
		t.Fatalf("close child conn: %v", err)
	}
	select {
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run returned error after clean close: %v", err)
		}
	case <-time.After(testPollDeadline):
		t.Fatalf("Run did not return after socket close")
	}
}

// T2 negative: empty start token fails Run before dialing.
func TestEmptyTokenFails(t *testing.T) {
	dir := shortTempDir(t)
	d := newFakeDaemon(t, dir)
	child := New(ChildConfig{
		TextSocketPath: filepath.Join(d.dir, "t.sock"),
		AudioDir:       d.dir,
		TokenReader:    strings.NewReader("   \n"),
	})
	if err := child.Run(context.Background()); err == nil {
		t.Fatalf("Run with empty token must fail")
	}
}

// gatedTTS is a test TTS whose stream hands out the first 640-byte chunk
// immediately, then blocks on second-read until releaseSecond is closed
// (then one more 640-byte chunk, then EOF). enteredPlay closes on the first
// read so tests know playback has begun.
type gatedTTS struct {
	releaseSecond chan struct{}
	enteredPlay   chan struct{}
}

func newGatedTTS() *gatedTTS {
	return &gatedTTS{releaseSecond: make(chan struct{}), enteredPlay: make(chan struct{})}
}

func (g *gatedTTS) Speak(ctx context.Context, text string) (io.ReadCloser, error) {
	return io.NopCloser(&gReader{g: g}), nil
}

type gReader struct {
	g *gatedTTS
	n int
}

func (r *gReader) Read(p []byte) (int, error) {
	switch r.n {
	case 0:
		r.n = 1
		select {
		case <-r.g.enteredPlay:
		default:
			close(r.g.enteredPlay)
		}
		return fill(p, 1), nil
	case 1:
		r.n = 2
		<-r.g.releaseSecond
		return fill(p, 2), nil
	default:
		return 0, io.EOF
	}
}

func (r *gReader) Close() error { return nil }

func fill(p []byte, b byte) int {
	n := min(len(p), 640)
	for i := range n {
		p[i] = b
	}
	return n
}

// isDropped reports whether clear has landed on the pipeline.
func (p *pipeline) isDropped() bool {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.dropQueued
}

// assertNoBytesWithin fails if the .out socket delivers ANY bytes within
// the window — the post-clear silence contract (barge-in drops the
// interrupted generation's audio, prefetched or otherwise, and stale
// bytes must never reach the daemon).
func assertNoBytesWithin(t *testing.T, out net.Conn, window time.Duration, msg string) {
	t.Helper()
	_ = out.SetReadDeadline(time.Now().Add(window))
	buf := make([]byte, 4096)
	n, _ := out.Read(buf)
	if n != 0 {
		t.Fatalf("%s: got %d bytes on .out within %s", msg, n, window)
	}
}

// isTransStarted reports whether the transcript session claim landed
// (transMu regime — tests must not read the field bare under -race).
func (p *pipeline) isTransStarted() bool {
	p.transMu.Lock()
	defer p.transMu.Unlock()
	return p.transStarted
}

// queuedSayCount reports how many say frames sit in the FIFO (test seam
// for the single-worker serialization audit, fix 1).
func (p *pipeline) queuedSayCount() int {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return len(p.sayQueue)
}

// pendingSayCount reports every say frame the worker still owes audio
// to: queued drafts PLUS the prefetch syntheses in flight (they left
// sayQueue at prefetch launch but are still unplayed frames).
// Tests that pin "clear drops everything queued" must count this, not
// len(sayQueue): with bounded prefetch the queue alone no longer holds
// the whole pending set.
func (p *pipeline) pendingSayCount() int {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return len(p.sayQueue) + len(p.synthJobs)
}

// synthActive reports whether a synthesis is outstanding (head frame
// generating or prefetch in flight) — the test seam for the prefetch
// depth bound: never more than prefetchDepth, and only ever frames
// whose predecessor entered the synth queue (strict dequeue order).
func (p *pipeline) synthActive() bool {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return len(p.synthJobs) > 0
}

// synthQueueDepth reports the number of outstanding synth jobs — the
// test seam that pins the bounded queue (prefetchDepth).
func (p *pipeline) synthQueueDepth() int {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return len(p.synthJobs)
}

// swapTTS installs a test TTS under playMu — the same lock regime the
// production code uses for provider fields (audit fix 2), so a test swap
// can never race the playback worker's ttsProvider() read under -race.
func swapTTS(p *pipeline, tts TTSProvider) {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	p.tts = tts
}

// helpers

func lastPtr(v bool) *bool { return &v } //nolint:modernize // newexpr suggests new(bool)=false; lastPtr preserves the caller's value

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(testPollDeadline)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within %s", testPollDeadline)
}

// testPollDeadline is the load-tolerant deadline for the voicechild test
// helpers (audit VLC-1/VS-3). The polls wait on real goroutine progress
// (playback state flips, socket bytes, queued frames) — progress the
// production pacer and scheduler deliver in microseconds when idle but
// seconds-scale jitter under full-package or -p 2 host load, where the
// old 5s windows flaked rotatingly ("documented host-bound REDs"; each
// failure passed isolated). The window is generous against LOAD, not
// against a broken property: a genuinely broken path still waits on an
// event that can never fire and fails at the deadline, so 30s fails
// nothing a 5s window would have caught.
const testPollDeadline = 30 * time.Second

// isSpeaking reports whether the pipeline has playback active.
func (p *pipeline) isSpeaking() bool {
	p.playMu.Lock()
	defer p.playMu.Unlock()
	return p.speaking
}

// tokenLoad exposes the stored start token (test seam).
func (c *Child) tokenLoad() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

// textConnected reports whether Run has published the text socket.
// serveText's Accept can return before that assignment.
func (c *Child) textConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

var (
	_ = json.Marshal  // keep encoding/json linked for wire assertions
	_ = io.ReadFull   // keep io linked
	_ context.Context // keep context linked
)

// T-token-dial: the child proves its start token as the FIRST line on the
// text socket, immediately after dialing and BEFORE any C1 frame (the
// daemon's auth gate consumes that line; a frame-first child would be
// refused). Asserted against a raw byte-capturing listener so the ORDER is
// pinned at the wire level, not through the fake daemon's line framing.
func TestChildSendsTokenLineFirst(t *testing.T) {
	dir := shortTempDir(t)
	sockPath := filepath.Join(dir, "t.sock")
	ln, err := net.Listen("unix", sockPath)
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { _ = ln.Close() })

	captured := make(chan []byte, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			captured <- nil
			return
		}
		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		out := make([]byte, n)
		copy(out, buf[:n])
		captured <- out
		_ = conn.Close()
	}()

	child := New(ChildConfig{
		TextSocketPath: sockPath,
		AudioDir:       dir,
		TokenReader:    strings.NewReader("tok-first-42\n"),
	})
	runDone := make(chan error, 1)
	go func() { runDone <- child.Run(context.Background()) }()

	select {
	case b := <-captured:
		if b == nil {
			t.Fatalf("accept failed")
		}
		if string(b) != "tok-first-42\n" {
			t.Fatalf("first bytes on the text socket = %q, want %q", string(b), "tok-first-42\n")
		}
	case <-time.After(testPollDeadline):
		t.Fatal("child never sent its start-token proof line")
	}

	// The connection died (listener closed): Run ends; the token was the
	// ONLY thing sent (no frame bytes landed before the close).
	_ = ln.Close()
	select {
	case <-runDone:
	case <-time.After(testPollDeadline):
		t.Fatal("Run did not return after the auth listener closed")
	}
}

// T-token-stdin-shape: readToken keeps the stdin seam exactly as contracted
// (line read, whitespace-trimmed, empty refused) — the token the child
// writes on the socket is the trimmed stdin line byte-for-byte.
func TestTokenIsTrimmedStdinLine(t *testing.T) {
	tok, err := readToken(strings.NewReader("  abc123\r\n"))
	if err != nil || tok != "abc123" {
		t.Fatalf("readToken = %q, %v; want abc123, nil", tok, err)
	}
}
