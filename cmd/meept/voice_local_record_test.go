package main

// voice_local_record_test.go - leaf 02 (C2) pins on the live-local
// speaker chain:
//
//   1. warm-before-dial ORDER: the silent 20 ms burst must reach the
//      player BEFORE the .out socket is dialed (lazy oto context init
//      ate the first ~150 ms of the greeting on live call
//      local-496a3fd0d2ab3fc6; the monitor's twin was
//      link-d52f032b6e010882). setupLiveSpeaker takes fake player +
//      fake dial seams so the order is observable headlessly.
//   2. the recorder tee: warm-up bytes NEVER enter the .raw
//      (skip-first, monitor convention), real drain bytes do in exact
//      order, and teardownLiveSpeaker closes the recorder file only
//      after the drain goroutine returned.
//   3. --record flag validation: refused in file mode (--in/--out),
//      live mode creates the DIR, unset = zero side effect.

import (
	"bytes"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/voice"
)

// seqPlayer is the ordered fake for these tests: it appends every Play
// burst under its own mutex, and the fake dial seam appends to the SAME
// event log (shared seq), so a "warm play precedes dial" assertion is
// race-free. (voice_link_test.go's fakePlayer has no event ordering and
// no error injection, hence this sibling.)
type seqPlayer struct {
	seq *playSeq
	err error // when set, Play returns it (cold-device simulation)
}

func (p *seqPlayer) Play(burst []byte) error {
	if p.err != nil {
		return p.err
	}
	p.seq.event("play:" + strconv.Itoa(len(burst)))
	cp := append([]byte(nil), burst...)
	p.seq.mu.Lock()
	p.seq.bursts = append(p.seq.bursts, cp)
	p.seq.mu.Unlock()
	return nil
}
func (p *seqPlayer) Clear() {}
func (p *seqPlayer) Close() {
	p.seq.mu.Lock()
	defer p.seq.mu.Unlock()
	p.seq.closes++
}

// playSeq is the shared event log between the fake player and the fake
// dial seam.
type playSeq struct {
	mu     sync.Mutex
	events []string
	bursts [][]byte
	closes int
}

func (s *playSeq) event(e string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.events = append(s.events, e)
}

func (s *playSeq) snapshot() ([]string, [][]byte, int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.events...), append([][]byte(nil), s.bursts...), len(s.bursts)
}

// fakeSpeakerDial returns a dial seam that dials NOTHING real: it hands
// back one end of an in-memory net.Pipe (the "socket" the drain reads)
// and the other end for the test to feed bytes into. The dial's position
// in the event log is the point of the seam.
func fakeSpeakerDial(seq *playSeq, fed *net.Conn) func(network, addr string) (net.Conn, error) {
	return func(network, addr string) (net.Conn, error) {
		seq.event("dial:" + network + " " + addr)
		drainSide, feedSide := net.Pipe()
		*fed = feedSide
		return drainSide, nil
	}
}

// isZero reports whether b is all-zero bytes (the warm burst must be silent).
func isZero(b []byte) bool { return len(bytes.TrimLeft(b, "\x00")) == 0 }

// ---------------------------------------------------------------------------
// 1. warm-before-dial order
// ---------------------------------------------------------------------------

func TestSetupLiveSpeakerWarmsBeforeDial(t *testing.T) {
	t.Parallel()
	seq := &playSeq{}
	var feed net.Conn
	open := voiceLocalOpenResult{CallID: "local-test1", OutPath: "/no/such/sock"}

	lspk, err := setupLiveSpeaker(open, "secretary", "",
		func() voice.Player { return &seqPlayer{seq: seq} },
		fakeSpeakerDial(seq, &feed))
	if err != nil {
		t.Fatalf("setupLiveSpeaker: %v", err)
	}
	defer teardownLiveSpeaker(lspk, closedChan())

	events, bursts, n := seq.snapshot()
	if len(events) < 2 {
		t.Fatalf("events = %v, want warm play then dial", events)
	}
	if events[0] != "play:"+strconv.Itoa(liveWarmBurstBytes) {
		t.Fatalf("first event = %q, want the %d B silent warm-up burst", events[0], liveWarmBurstBytes)
	}
	if !strings.HasPrefix(events[1], "dial:") {
		t.Fatalf("dial must FOLLOW the warm-up (open-truncation regression): events = %v", events)
	}
	if n != 1 || !isZero(bursts[0]) {
		t.Fatalf("bursts = %v, want exactly one all-zero %d B warm burst", bursts, liveWarmBurstBytes)
	}
	if lspk.rec != nil {
		t.Fatal("recorder must be nil with --record off (zero behaviour change)")
	}
}

func TestSetupLiveSpeakerWarmFailureNeverDials(t *testing.T) {
	t.Parallel()
	seq := &playSeq{}
	var feed net.Conn
	want := errors.New("no audio device")
	open := voiceLocalOpenResult{CallID: "local-test2", OutPath: "/no/such/sock"}

	_, err := setupLiveSpeaker(open, "secretary", "",
		func() voice.Player { return &seqPlayer{seq: seq, err: want} },
		fakeSpeakerDial(seq, &feed))
	if !errors.Is(err, want) {
		t.Fatalf("warm failure = %v, want wrapped %v", err, want)
	}
	events, _, _ := seq.snapshot()
	for _, e := range events {
		if strings.HasPrefix(e, "dial:") {
			t.Fatalf("a cold player must NEVER dial (the defect this pins): events = %v", events)
		}
	}
}

// ---------------------------------------------------------------------------
// 2. the --record tee on the live local leg
// ---------------------------------------------------------------------------

func TestSetupLiveSpeakerRecordsWithoutWarmBytes(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seq := &playSeq{}
	var feed net.Conn
	open := voiceLocalOpenResult{CallID: "local-rec1", OutPath: "/no/such/sock"}

	lspk, err := setupLiveSpeaker(open, "secretary", dir,
		func() voice.Player { return &seqPlayer{seq: seq} },
		fakeSpeakerDial(seq, &feed))
	if err != nil {
		t.Fatalf("setupLiveSpeaker: %v", err)
	}
	events, _, _ := seq.snapshot()
	if events[0] != "play:"+strconv.Itoa(liveWarmBurstBytes) || !strings.HasPrefix(events[1], "dial:") {
		t.Fatalf("recording must keep the warm-before-dial order: %v", events)
	}

	// Feed real audio through the same drain the live loop uses (the
	// 21-chunk stream crosses the backlog bound, so mid-stream
	// handoffs AND the EOF tail flush all go through the recorder).
	// net.Pipe is unbuffered/synchronous, so every write pairs with a
	// drain Read: closing the FEED end after the full write is the
	// real socket-EOF shape, and the drain then returns on its own
	// before teardown runs (no feed/teardown race).
	speech := bytes.Repeat([]byte{0x07}, 21*liveSpeakerReadChunk)
	done := make(chan error, 1)
	returned := make(chan struct{})
	go func() { drainLiveSpeaker(lspk.conn, lspk.sink, done); close(returned) }()
	go func() {
		_, _ = feed.Write(speech)
		_ = feed.Close()
	}()
	<-returned // the drain saw the EOF; every byte is in the recorder
	if derr := <-done; derr != nil {
		t.Fatalf("drain: %v", derr)
	}

	teardownLiveSpeaker(lspk, returned)
	raw, err := os.ReadFile(filepath.Join(dir, "local-rec1.raw"))
	if err != nil {
		t.Fatalf("record file: %v", err)
	}
	if !bytes.Equal(raw, speech) {
		t.Fatalf("recording = %d B, want the exact %d B fed (gap-free, in order, warm bytes excluded)", len(raw), len(speech))
	}
	metaBytes, err := os.ReadFile(filepath.Join(dir, "local-rec1-meta.json"))
	if err != nil {
		t.Fatalf("meta file: %v", err)
	}
	var meta struct {
		CallID     string `json:"call_id"`
		Employee   string `json:"employee"`
		StartedAt  string `json:"started_at"`
		SampleRate int    `json:"sample_rate"`
	}
	if err := json.Unmarshal(metaBytes, &meta); err != nil {
		t.Fatalf("meta JSON: %v (%q)", err, metaBytes)
	}
	if meta.CallID != "local-rec1" || meta.Employee != "secretary" || meta.SampleRate != 16000 || meta.StartedAt == "" {
		t.Fatalf("meta = %+v, want call_id/employee/started_at/sample_rate 16000", meta)
	}
	_, _, n := seq.snapshot()
	if n < 2 {
		t.Fatalf("player bursts = %d, want warm-up + drain bursts", n)
	}
}

// TestTeardownLiveSpeakerEndsDrain pins the teardown contract: closing
// the .out conn ends the blocked drain (no 2 s timeout burn), the
// recorder file is closed after the drain returned, and the device is
// closed once.
func TestTeardownLiveSpeakerEndsDrain(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	seq := &playSeq{}
	var feed net.Conn
	open := voiceLocalOpenResult{CallID: "local-td1", OutPath: "/unused"}
	lspk, err := setupLiveSpeaker(open, "secretary", dir,
		func() voice.Player { return &seqPlayer{seq: seq} },
		fakeSpeakerDial(seq, &feed))
	if err != nil {
		t.Fatalf("setupLiveSpeaker: %v", err)
	}
	done := make(chan error, 1)
	returned := make(chan struct{})
	// The drain blocks on the pipe until teardown's conn.Close ends it.
	go func() { drainLiveSpeaker(lspk.conn, lspk.sink, done); close(returned) }()
	if _, err := feed.Write(bytes.Repeat([]byte{0x03}, liveSpeakerReadChunk)); err != nil {
		t.Fatalf("write: %v", err)
	}

	start := time.Now()
	teardownLiveSpeaker(lspk, returned)
	if time.Since(start) > time.Second {
		t.Fatalf("teardown took %v: must end the drain via conn.Close, not burn the 2 s wait", time.Since(start))
	}
	if derr := <-done; derr != nil {
		t.Fatalf("drain end should be a clean EOF-shaped nil: %v", derr)
	}
	if _, err := os.Stat(filepath.Join(dir, "local-td1.raw")); err != nil {
		t.Fatalf("recorder file must exist (teardown closed it): %v", err)
	}
	if seq.closes != 1 {
		t.Fatalf("player device closes = %d, want 1", seq.closes)
	}
}

// closedChan returns an already-closed chan struct{} - the shape
// teardownLiveSpeaker sees when the drain has long returned.
func closedChan() chan struct{} {
	ch := make(chan struct{})
	close(ch)
	return ch
}

// ---------------------------------------------------------------------------
// 3. --record flag validation on `voice local`
// ---------------------------------------------------------------------------

func TestVoiceLocalRecordFlagValidation(t *testing.T) {
	t.Parallel()
	// --record with file mode (--out) is refused with the named error.
	f := localFlags{employee: "secretary", outPath: "a.wav", record: "/tmp/x"}
	err := f.validate()
	if err == nil || !strings.Contains(err.Error(), "--record requires live mode") {
		t.Fatalf("validate(record + --out) = %v, want --record requires live mode", err)
	}
	// ...and with --in too.
	f = localFlags{employee: "secretary", inPath: "q.wav", record: "/tmp/x"}
	if err := f.validate(); err == nil || !strings.Contains(err.Error(), "--record requires live mode") {
		t.Fatalf("validate(record + --in) = %v, want --record requires live mode", err)
	}
	// --record live with a NON-EXISTENT dir: validate creates it.
	dir := filepath.Join(t.TempDir(), "nested", "rec")
	f = localFlags{employee: "secretary", record: dir}
	if err := f.validate(); err != nil {
		t.Fatalf("validate(record live) = %v, want nil", err)
	}
	if st, serr := os.Stat(dir); serr != nil || !st.IsDir() {
		t.Fatalf("--record dir not created: stat = %v", serr)
	}
	// record == "" default: zero side effect, no error.
	f = localFlags{employee: "secretary"}
	if err := f.validate(); err != nil {
		t.Fatalf("validate(no record) = %v, want nil", err)
	}
	// --employee gate still binds (and NOT for --list).
	if err := (localFlags{}).validate(); err == nil || !strings.Contains(err.Error(), "--employee is required") {
		t.Fatalf("validate(no employee) = %v, want --employee is required", err)
	}
	if err := (localFlags{list: true}).validate(); err != nil {
		t.Fatalf("validate(--list) = %v, want nil", err)
	}
	// The --record flag is registered on the command surface.
	cmd := newVoiceLocalCmd()
	if _, err := cmd.Flags().GetString("record"); err != nil {
		t.Fatalf("--record flag not registered: %v", err)
	}
	// CLI wiring: the file-mode refusal surfaces through cobra RunE
	// BEFORE any daemon connect (validate runs first; a bad combo never
	// reaches RPC).
	cmd.SetArgs([]string{"--employee", "secretary", "--out", "a.wav", "--record", "/tmp/x"})
	if err := cmd.Execute(); err == nil || !strings.Contains(err.Error(), "--record requires live mode") {
		t.Fatalf("Execute(record + --out) = %v, want refusal", err)
	}
}

// compile-time pin: the fake player satisfies the same seam the live
// loop drives.
var _ voice.Player = (*seqPlayer)(nil)
