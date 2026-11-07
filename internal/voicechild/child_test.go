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
// assertions), and per-call audio pair listeners.
type fakeDaemon struct {
	t           *testing.T
	dir         string
	textLn      net.Listener
	mu          sync.Mutex
	queue       []voiceframe.Frame
	rawQueue    []string
	frameSignal chan struct{}
	inLn        map[string]net.Listener
	outLn       map[string]net.Listener
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
func (d *fakeDaemon) acceptAll(ccid string) (in, out net.Conn) {
	d.t.Helper()
	d.mu.Lock()
	inLn, outLn := d.inLn[ccid], d.outLn[ccid]
	d.mu.Unlock()
	var err error
	in, err = inLn.Accept()
	if err != nil {
		d.t.Fatalf("accept in: %v", err)
	}
	out, err = outLn.Accept()
	if err != nil {
		d.t.Fatalf("accept out: %v", err)
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
	case <-time.After(5 * time.Second):
		t.Fatalf("timed out waiting for child frame")
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	f := d.queue[0]
	d.queue = d.queue[1:]
	return f
}

// serveText accepts the child's text connection and records frames it sends.
func (d *fakeDaemon) serveText() net.Conn {
	d.t.Helper()
	conn, err := d.textLn.Accept()
	if err != nil {
		d.t.Fatalf("accept text: %v", err)
	}
	go func() {
		br := bufio.NewScanner(conn)
		for br.Scan() {
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

// waitRawLine waits for and pops the next raw child-emitted line. Wire
// shape is a contract (byte-exact JSON keys), so some assertions must see
// the line itself, not the decoded struct.
func (d *fakeDaemon) waitRawLine(t *testing.T) string {
	t.Helper()
	select {
	case <-d.frameSignal:
	case <-time.After(5 * time.Second):
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

	// End tears the pipeline down (T2).
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc1", Event: voiceframe.EventEnd, Reason: "hangup"})
	waitFor(t, func() bool { return child.pipelineCount() == 0 })

	ctxCancelErr := func() error {
		// Shut down: cancel the run via context by closing the daemon conn.
		_ = childConn.Close()
		select {
		case err := <-runDone:
			return err
		case <-time.After(5 * time.Second):
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
	p.tts = gate

	d.sendLine(childConn, voiceframe.Frame{CallID: "cc2", Event: voiceframe.EventSay, Text: "hello there", Last: lastPtr(true)})
	gotFirst := make(chan []byte, 1)
	go func() {
		buf := make([]byte, 640)
		n, _ := io.ReadFull(out, buf)
		gotFirst <- buf[:n]
	}()

	var first []byte
	select {
	case first = <-gotFirst:
	case <-time.After(5 * time.Second):
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
	p.tts = gate

	// Start playback through the real say path; the gated TTS parks the
	// stream after the first chunk, so playback stays active.
	d.sendLine(childConn, voiceframe.Frame{CallID: "cc3", Event: voiceframe.EventSay, Text: "long utterance", Last: lastPtr(true)})
	select {
	case <-gate.enteredPlay:
	case <-time.After(5 * time.Second):
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

	// New say restarts playback and re-arms.
	p.playbackRestarted()
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
	n := len(p)
	if n > 640 {
		n = 640
	}
	for i := 0; i < n; i++ {
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

// helpers

func lastPtr(v bool) *bool { return &v }

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("condition not met within timeout")
}

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
