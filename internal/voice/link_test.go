package voice

// link_test.go - leaf 03 tests (deadair-link plan
// docs/plans/20261110-voice-deadair-link/03-voice-link.md): the
// LinkManager bridge contract. No real child, no daemon: a FAKE leg
// seam (recording Opens/Ends, serving REAL unix pair listeners so
// LinkManager's production dialUnix + io.Copy pumps run unchanged) plus
// a fake transcript store. Pinned properties: up-front refusals
// (same-employee, empty ids - zero side effects), full unwind on
// second-open and dial failure (no leaked leg, no leaked conn),
// byte-exact CROSS-connectivity in BOTH directions, idempotent Close,
// status turn-count reads + store-failure degrade, retention after
// close, unknown-link named errors, distinct per-link labels.

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func linkDiscard() *slog.Logger { return slog.New(slog.DiscardHandler) }

// linkFakeLeg is the LinkLegOpener test double. Every successful Open
// gets two unix listeners standing in for the leg's CLIENT pair sockets:
// the accepted .in conn is where the bridge WRITES the leg's mic bytes,
// the accepted .out conn is where the test injects the leg's speaker
// bytes for the bridge to READ. That keeps the production dialer
// (dialUnix) and the io.Copy pumps exactly as shipped.
type linkFakeLeg struct {
	t   *testing.T
	dir string

	mu       sync.Mutex
	opens    []string // "employee|label" per Open
	ends     []string // "ccid|reason" per End
	acceptIn map[string]chan net.Conn
	acceptOu map[string]chan net.Conn
	listens  []net.Listener
	seq      int
	// failOpenN refuses the Nth Open (1-based) with openErr; 0 = never.
	failOpenN int
	openErr   error
	// badOutPathFor names the employee whose OutPath points at a
	// non-existent socket file -> the bridge's dial fails mid-way and
	// the unwind must clean up everything.
	badOutPathFor string
	// barePaths records Opens + returns pair paths WITHOUT standing up
	// listeners (for the monitor path-limit test: a dir too long for
	// sun_path must still let both legs "open" so the guard, which
	// runs before dialAll, is what fires).
	barePaths bool
}

func newLinkFakeLeg(t *testing.T) *linkFakeLeg {
	t.Helper()
	return newLinkFakeLegWithDir(t, filepath.Join(shortDir(t), "linkpairs"))
}

func newLinkFakeLegWithDir(t *testing.T, dir string) *linkFakeLeg {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("pairs dir: %v", err)
	}
	f := &linkFakeLeg{
		t:        t,
		dir:      dir,
		acceptIn: make(map[string]chan net.Conn),
		acceptOu: make(map[string]chan net.Conn),
		openErr:  errors.New("employee already live"),
	}
	t.Cleanup(func() {
		f.mu.Lock()
		ln := f.listens
		f.listens = nil
		f.mu.Unlock()
		for _, l := range ln {
			_ = l.Close()
		}
	})
	return f
}

func (f *linkFakeLeg) Open(_ context.Context, employeeID, label string, _ ...string) (LocalCall, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.opens = append(f.opens, employeeID+"|"+label)
	f.seq++
	ccid := fmt.Sprintf("local-link%04d", f.seq)
	if f.barePaths {
		return LocalCall{
			CallID:  ccid,
			InPath:  filepath.Join(f.dir, ccid+".c.in"),
			OutPath: filepath.Join(f.dir, ccid+".c.out"),
		}, nil
	}
	if f.failOpenN > 0 && len(f.opens) == f.failOpenN {
		return LocalCall{}, f.openErr
	}
	inPath := filepath.Join(f.dir, ccid+".c.in")
	outPath := filepath.Join(f.dir, ccid+".c.out")
	if f.badOutPathFor == employeeID {
		// Only the .in pair listens: the bridge dials A.in, A.out,
		// B.in successfully and FAILS on B.out (a non-existent socket
		// file) - the mid-way dial-failure shape the unwind test pins.
		for _, p := range []string{inPath} {
			_ = os.Remove(p)
		}
		inLn, err := net.Listen("unix", inPath)
		if err != nil {
			return LocalCall{}, fmt.Errorf("fake listen in: %w", err)
		}
		f.listens = append(f.listens, inLn)
		inCh := make(chan net.Conn, 1)
		f.acceptIn[ccid] = inCh
		go func() {
			c, aerr := inLn.Accept()
			if aerr == nil {
				inCh <- c
			}
		}()
		outPath = filepath.Join(f.dir, "gone-"+ccid+".c.out")
		return LocalCall{CallID: ccid, InPath: inPath, OutPath: outPath}, nil
	}
	for _, p := range []string{inPath, outPath} {
		_ = os.Remove(p)
	}
	inLn, err := net.Listen("unix", inPath)
	if err != nil {
		return LocalCall{}, fmt.Errorf("fake listen in: %w", err)
	}
	f.listens = append(f.listens, inLn)
	outLn, err := net.Listen("unix", outPath)
	if err != nil {
		// inLn stays in f.listens: the test cleanup owns listener
		// lifetimes. Closing it here held the mutex across socket I/O
		// and tripped the mutexio gate (2026-11-10).
		return LocalCall{}, fmt.Errorf("fake listen out: %w", err)
	}
	f.listens = append(f.listens, outLn)
	inCh := make(chan net.Conn, 1)
	outCh := make(chan net.Conn, 1)
	f.acceptIn[ccid] = inCh
	f.acceptOu[ccid] = outCh
	// The goroutines close over the LOCAL channel refs: touching the
	// f.acceptIn/f.acceptOu maps from the accept goroutine without the
	// mutex is a map race (found under -race; the map access belongs
	// to Open, the send to the goroutine).
	go func() {
		c, aerr := inLn.Accept()
		if aerr == nil {
			inCh <- c
		}
	}()
	go func() {
		c, aerr := outLn.Accept()
		if aerr == nil {
			outCh <- c
		}
	}()
	return LocalCall{CallID: ccid, InPath: inPath, OutPath: outPath}, nil
}

func (f *linkFakeLeg) End(ccid, reason string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.ends = append(f.ends, ccid+"|"+reason)
	return nil
}

func (f *linkFakeLeg) openSnap() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.opens...)
}

func (f *linkFakeLeg) endSnap() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ends...)
}

func (f *linkFakeLeg) failCCIDs() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]string, 0, f.seq)
	for i := 1; i <= f.seq; i++ {
		out = append(out, fmt.Sprintf("local-link%04d", i))
	}
	return out
}

// accepted returns the conn the bridge dialed for one leg's pair side,
// with a bounded deadline (the bridge dials all four sockets right
// after Open returns, so seconds are generous).
func (f *linkFakeLeg) accepted(ccid string, mic bool) net.Conn {
	f.mu.Lock()
	ch := f.acceptIn[ccid]
	if !mic {
		ch = f.acceptOu[ccid]
	}
	f.mu.Unlock()
	if ch == nil {
		f.t.Fatalf("no pair recorded for %s", ccid)
	}
	select {
	case c := <-ch:
		return c
	case <-time.After(5 * time.Second):
		f.t.Fatalf("bridge never dialed %s (mic=%v)", ccid, mic)
		return nil
	}
}

// linkFakeStore is the LinkTranscriptReader double.
type linkFakeStore struct {
	mu    sync.Mutex
	calls map[string][]TranscriptLine
	err   error
}

func (s *linkFakeStore) EndedCall(_ context.Context, callID string) (CallRecord, []TranscriptLine, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.err != nil {
		return CallRecord{}, nil, false, s.err
	}
	lines := s.calls[callID]
	return CallRecord{CallID: callID}, lines, len(lines) > 0, nil
}

func (s *linkFakeStore) set(callID string, lines []TranscriptLine) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls[callID] = lines
}

func (s *linkFakeStore) breakRead(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.err = err
}

func linkLine(role, text string) TranscriptLine {
	return TranscriptLine{Role: role, Text: text, At: time.Now().UTC()}
}

// newLinkTestManager wires a LinkManager over the fake leg (production
// dialer intact) and a fake store.
func newLinkTestManager(t *testing.T, leg *linkFakeLeg, store LinkTranscriptReader) *LinkManager {
	t.Helper()
	m, err := NewLinkManager(LinkDeps{
		Leg:         leg,
		Store:       store,
		DialTimeout: 3 * time.Second,
		RetainEnded: time.Hour,
		Logger:      linkDiscard(),
	})
	if err != nil {
		t.Fatalf("NewLinkManager: %v", err)
	}
	return m
}

func TestNewLinkManagerRequiresLeg(t *testing.T) {
	if _, err := NewLinkManager(LinkDeps{}); err == nil {
		t.Fatal("NewLinkManager with nil leg: want error")
	}
}

func TestLinkOpenRefusesSameEmployee(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	_, err := m.Open(context.Background(), "companion", "companion")
	if !errors.Is(err, ErrLinkSameEmployee) {
		t.Fatalf("same-employee Open = %v, want ErrLinkSameEmployee", err)
	}
	// Zero side effects: no Open reached the leg at all.
	if got := leg.openSnap(); len(got) != 0 {
		t.Fatalf("refusal opened legs: %v", got)
	}
}

func TestLinkOpenRefusesEmptyEmployee(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	if _, err := m.Open(context.Background(), "", "b"); !errors.Is(err, ErrNoEmployee) {
		t.Fatalf("empty a = %v, want ErrNoEmployee", err)
	}
	if _, err := m.Open(context.Background(), "a", ""); !errors.Is(err, ErrNoEmployee) {
		t.Fatalf("empty b = %v, want ErrNoEmployee", err)
	}
	if got := leg.openSnap(); len(got) != 0 {
		t.Fatalf("refusal opened legs: %v", got)
	}
}

// TestLinkBridgeByteExact is the whole contract of the leaf: PCM written
// into A's speaker socket arrives on B's mic socket BYTE-EXACT, and vice
// versa, through the two crossed io.Copy pumps.
func TestLinkBridgeByteExact(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	link, err := m.Open(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = m.Close(link.ID, "test teardown") })

	// The bridge dialed four pair sockets; grab the fake leg's accepted
	// ends. aSpk/bSpk = where the test injects each leg's speaker PCM;
	// aMic/bMic = where the bridge delivers the OTHER leg's speech.
	aMic := leg.accepted(link.ACCID, true)
	defer aMic.Close()
	aSpk := leg.accepted(link.ACCID, false)
	defer aSpk.Close()
	bMic := leg.accepted(link.BCCID, true)
	defer bMic.Close()
	bSpk := leg.accepted(link.BCCID, false)
	defer bSpk.Close()

	// Mic-join silence seed (turn-taking leaf): the bridge leads each
	// mic with exact zero chunks BEFORE the pumps run, so both splices'
	// first-byte join gate never depends on greeting audio (B no longer
	// greets). Drain + assert the seed, then the byte-exact pins run
	// against the real speech tail.
	linkDrainSilenceSeed(t, bMic)
	linkDrainSilenceSeed(t, aMic)

	// A speaks -> B's mic receives, byte-exact.
	sentA := linkSpeakerChunks(t, aSpk)
	gotB := linkReadN(t, bMic, len(sentA))
	if string(gotB) != string(sentA) {
		t.Fatalf("a->b bridge corrupted bytes: got %d B, want %d B exact", len(gotB), len(sentA))
	}
	// B speaks -> A's mic receives, byte-exact.
	sentB := linkSpeakerChunks(t, bSpk)
	gotA := linkReadN(t, aMic, len(sentB))
	if string(gotA) != string(sentB) {
		t.Fatalf("b->a bridge corrupted bytes: got %d B, want %d B exact", len(gotA), len(sentB))
	}
}

// linkDrainSilenceSeed consumes the bridge's mic-join silence seed
// (linkSilenceSeedChunks exact 640 B L16 chunks of digital silence) and
// pins it is silent + whole-chunk (torn chunk framing would corrupt the
// downstream splice).
func linkDrainSilenceSeed(t *testing.T, mic io.Reader) {
	t.Helper()
	want := linkSilenceSeedChunks * 640
	got := linkReadN(t, mic, want)
	linkAssertSilencePrefix(t, got, "bridge mic seed")
}

// linkAssertSilencePrefix pins a captured byte run is all zeros (the
// digital-silence mic-join seed - any non-zero byte there means the
// bridge injected audio the contract says it must not).
func linkAssertSilencePrefix(t *testing.T, got []byte, what string) {
	t.Helper()
	for i, b := range got {
		if b != 0 {
			t.Fatalf("%s byte %d = %d, want 0 (seed must be digital silence)", what, i, b)
		}
	}
}

// linkSpeakerChunks writes two realistic L16@16k chunks (640 B each,
// the codec-derived chunk the daemon paces per 20 ms) and returns the
// exact bytes sent.
func linkSpeakerChunks(t *testing.T, w io.Writer) []byte {
	t.Helper()
	var payload []byte
	for i := range 2 {
		chunk := make([]byte, 640)
		for j := range chunk {
			chunk[j] = byte((i*640 + j) % 251) // deterministic pattern
		}
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("write speaker chunk: %v", err)
		}
		payload = append(payload, chunk...)
	}
	return payload
}

func linkReadN(t *testing.T, r io.Reader, n int) []byte {
	t.Helper()
	got := make([]byte, n)
	deadline := time.Now().Add(5 * time.Second)
	for rd := 0; rd < n; {
		if time.Now().After(deadline) {
			t.Fatalf("read timeout: %d/%d bytes", rd, n)
		}
		c, err := r.Read(got[rd:])
		if c > 0 {
			rd += c
			continue
		}
		if err != nil {
			t.Fatalf("read bridge: %v", err)
		}
	}
	return got
}

func TestLinkOpenUnwindsSecondLegFailure(t *testing.T) {
	leg := newLinkFakeLeg(t)
	leg.failOpenN = 2 // side b refuses (e.g. employee busy)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	_, err := m.Open(context.Background(), "alice", "bob")
	if err == nil || !errors.Is(err, leg.openErr) {
		t.Fatalf("Open = %v, want the wrapped leg refusal", err)
	}
	ends := leg.endSnap()
	if len(ends) != 1 {
		t.Fatalf("side a not unwound on b failure: ends=%v, want exactly 1", ends)
	}
	aCCID := leg.failCCIDs()[0]
	if !strings.HasPrefix(ends[0], aCCID+"|") {
		t.Fatalf("unwind ended %q, want side a (%s)", ends[0], aCCID)
	}
	got := leg.openSnap()
	if len(got) != 2 {
		t.Fatalf("opens = %v, want both sides attempted", got)
	}
	// The label pins (leaf doc): side a opens with a "link-a-" label.
	if !strings.HasPrefix(got[0], "alice|link-a-") || !strings.HasPrefix(got[1], "bob|link-b-") {
		t.Fatalf("labels = %v, want link-a-/link-b- shapes", got)
	}
}

func TestLinkOpenUnwindsDialFailure(t *testing.T) {
	leg := newLinkFakeLeg(t)
	leg.badOutPathFor = "bob" // b's speaker path dials a non-existent socket
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	_, err := m.Open(context.Background(), "alice", "bob")
	if err == nil {
		t.Fatal("Open with a dead pair path: want dial failure")
	}
	if !strings.Contains(err.Error(), "dial") {
		t.Fatalf("Open error %q, want a dial failure", err)
	}
	// BOTH legs ended (the failure came after both Opens succeeded).
	ends := leg.endSnap()
	if len(ends) != 2 {
		t.Fatalf("dial-failure unwind: ends=%v, want both legs ended", ends)
	}
	// The conns already dialed (a.mic, a.speaker, b.mic) got closed by
	// the unwind: the fake leg's accepted ends observe EOF/error
	// instead of staying idle-open.
	ccids := leg.failCCIDs()
	for _, probe := range []struct {
		ccid string
		mic  bool
	}{
		{ccids[0], true}, {ccids[0], false}, {ccids[1], true},
	} {
		c := leg.accepted(probe.ccid, probe.mic)
		_ = c.SetReadDeadline(time.Now().Add(3 * time.Second))
		buf := make([]byte, 1)
		if _, rerr := c.Read(buf); rerr == nil {
			_ = c.Close()
			t.Fatalf("bridge conn for %s (mic=%v) still open after dial-failure unwind", probe.ccid, probe.mic)
		}
		_ = c.Close()
	}
}

func TestLinkCloseIdempotentAndUnknown(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	link, err := m.Open(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if err := m.Close(link.ID, "first"); err != nil {
		t.Fatalf("first Close: %v", err)
	}
	if err := m.Close(link.ID, "second"); err != nil {
		t.Fatalf("double Close = %v, want idempotent nil", err)
	}
	if ends := leg.endSnap(); len(ends) != 2 {
		t.Fatalf("End fan-out = %v, want exactly one per leg", ends)
	}
	// Retention: Status stays queryable after Close (the CLI's final
	// transcript read depends on this).
	if _, err := m.Status(context.Background(), link.ID); err != nil {
		t.Fatalf("Status inside retention after Close: %v", err)
	}
	if len(m.LiveLinks()) != 0 {
		t.Fatal("closed link still listed as live")
	}
	if err := m.Close("link-never-existed", "x"); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("Close unknown = %v, want ErrLinkNotFound", err)
	}
	if _, err := m.Status(context.Background(), "link-never-existed"); !errors.Is(err, ErrLinkNotFound) {
		t.Fatalf("Status unknown = %v, want ErrLinkNotFound", err)
	}
}

func TestLinkStatusCountsTurns(t *testing.T) {
	leg := newLinkFakeLeg(t)
	store := &linkFakeStore{calls: map[string][]TranscriptLine{}}
	m := newLinkTestManager(t, leg, store)
	link, err := m.Open(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	t.Cleanup(func() { _ = m.Close(link.ID, "test teardown") })
	store.set(link.ACCID, []TranscriptLine{linkLine("assistant", "hello"), linkLine("user", "hi"), linkLine("assistant", "weather")})
	store.set(link.BCCID, []TranscriptLine{linkLine("assistant", "greetings")})

	res, err := m.Status(context.Background(), link.ID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if res.A.AssistantTurns != 2 || res.A.UserTurns != 1 {
		t.Fatalf("side a counts = %d/%d, want 2/1", res.A.AssistantTurns, res.A.UserTurns)
	}
	if res.B.AssistantTurns != 1 || res.B.UserTurns != 0 {
		t.Fatalf("side b counts = %d/%d, want 1/0", res.B.AssistantTurns, res.B.UserTurns)
	}
	if len(res.A.Transcript) != 3 {
		t.Fatalf("side a transcript = %d lines, want 3", len(res.A.Transcript))
	}
	if res.Ended {
		t.Fatal("live link reports Ended=true")
	}
	if res.A.EmployeeID != "alice" || res.B.EmployeeID != "bob" {
		t.Fatalf("side employee routing = %q/%q, want alice/bob", res.A.EmployeeID, res.B.EmployeeID)
	}
	if len(m.LiveLinks()) != 1 {
		t.Fatalf("LiveLinks = %d, want 1", len(m.LiveLinks()))
	}

	// A failing store degrades to zero counts, never an error (a status
	// poll must not abort a harness run mid-link).
	store.breakRead(errors.New("db gone"))
	res, err = m.Status(context.Background(), link.ID)
	if err != nil {
		t.Fatalf("Status with failing store = %v, want degraded zero counts", err)
	}
	if res.A.AssistantTurns != 0 || len(res.A.Transcript) != 0 {
		t.Fatalf("store failure leaked counts into status: %+v", res.A)
	}
}

// linkRecorder is a tiny slog handler that keeps INFO+ records in memory
// so a test can assert the pump telemetry lines.
type linkRecorder struct {
	mu      sync.Mutex
	records []slog.Record
}

func (r *linkRecorder) Enabled(_ context.Context, level slog.Level) bool {
	return level >= slog.LevelInfo
}

func (r *linkRecorder) Handle(_ context.Context, rec slog.Record) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.records = append(r.records, rec.Clone())
	return nil
}

func (r *linkRecorder) WithAttrs(_ []slog.Attr) slog.Handler { return r }
func (r *linkRecorder) WithGroup(_ string) slog.Handler      { return r }

// lines snapshots "msg k=v" strings for assertions.
func (r *linkRecorder) lines() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]string, 0, len(r.records))
	for _, rec := range r.records {
		line := rec.Message
		rec.Attrs(func(a slog.Attr) bool {
			line += " " + a.Key + "=" + a.Value.String()
			return true
		})
		out = append(out, line)
	}
	return out
}

func (r *linkRecorder) contains(msgAndAttr string) bool {
	msg, attr, _ := strings.Cut(msgAndAttr, "|")
	for _, l := range r.lines() {
		if strings.Contains(l, msg) && strings.Contains(l, attr) {
			return true
		}
	}
	return false
}

// TestLinkRealLegEndToEndBothDirections is the link-72a04aebac963cdb
// regression: TWO REAL LocalLeg calls (real splice, real AudioPairs, the
// fixture's fake child dialing each pair after Open - exactly the live
// ordering that half-killed side B), driven through the real
// LinkManager.Open/dialUnix/pumps. Child-side PCM written on A's .out
// must arrive at B's child .in (and vice versa), i.e. the WHOLE crossed
// path: splice outbound (pump survives the pre-accept child-dial window)
// -> pace -> client conn -> io.Copy pump -> other leg's client conn ->
// splice inbound -> child .in. It also pins the pump telemetry: both
// directions log their first byte while both legs are live.
func TestLinkRealLegEndToEndBothDirections(t *testing.T) {
	f := newLegFixture(t)
	ctx := context.Background()

	rec := &linkRecorder{}
	m, err := NewLinkManager(LinkDeps{
		Leg:         f.leg,
		DialTimeout: 3 * time.Second,
		RetainEnded: time.Hour,
		Logger:      slog.New(rec),
	})
	if err != nil {
		t.Fatalf("NewLinkManager: %v", err)
	}
	link, err := m.Open(ctx, "companion", "secretary")
	if err != nil {
		t.Fatalf("link Open: %v", err)
	}

	// Fake children dial their pairs AFTER Open (live ordering: the
	// splice's outbound pump hits the pre-accept window first - the
	// link-72a04aebac963cdb race) and CAPTURE everything written
	// toward them on .in.
	aChildOut := linkDialChildPairCapturing(t, link.ACCID)
	bChildOut := linkDialChildPairCapturing(t, link.BCCID)

	// Mic-join silence seed (turn-taking leaf): the bridge leads each
	// mic with exact zero chunks so a leg's join gate never depends on
	// greeting audio (B no longer greets). The seed therefore arrives at
	// each child .in BEFORE the crossed speech - wait for seed + payload
	// and compare the payload tail, pinning the seed prefix is silence.
	seedBytes := linkSilenceSeedChunks * 640

	// A's child speaks (greeting-equivalent): 8 exact 640 B chunks.
	sentA := linkWriteChunks(t, aChildOut, 8)
	// B's child .in must receive them byte-exact - proves A's paced
	// outbound survived the pre-accept window AND the a->b pump moved
	// bytes into B's splice inbound.
	gotB := linkWaitBytes(t, func() []byte { return localChildInBytes(link.BCCID) }, seedBytes+len(sentA))
	linkAssertSilencePrefix(t, gotB[:seedBytes], "a->b seed")
	if string(gotB[seedBytes:seedBytes+len(sentA)]) != string(sentA) {
		t.Fatalf("a->b e2e corrupted: got %d B, want %d B exact after seed", len(gotB), seedBytes+len(sentA))
	}

	// Mirror direction: B's child speaks, A's child .in receives.
	sentB := linkWriteChunks(t, bChildOut, 8)
	gotA := linkWaitBytes(t, func() []byte { return localChildInBytes(link.ACCID) }, seedBytes+len(sentB))
	linkAssertSilencePrefix(t, gotA[:seedBytes], "b->a seed")
	if string(gotA[len(gotA)-len(sentB):]) != string(sentB) {
		t.Fatalf("b->a e2e corrupted: got %d B tail, want %d B exact", len(gotA), len(sentB))
	}

	// Telemetry: BOTH pump directions demonstrated a first byte while
	// both legs are live (the link-run self-explanation the post-mortem
	// asked for).
	if !rec.contains("voice: link pump first byte|dir=a->b") {
		t.Errorf("missing a->b pump first-byte telemetry; lines=%v", rec.lines())
	}
	if !rec.contains("voice: link pump first byte|dir=b->a") {
		t.Errorf("missing b->a pump first-byte telemetry; lines=%v", rec.lines())
	}

	if err := m.Close(link.ID, "test teardown"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Pump end-lines carry the copied totals (bytes= > 0 on a healthy
	// direction). Poll briefly: Close cancels the pumps but does not
	// join them (same posture as the production teardown).
	endedDeadline := time.Now().Add(3 * time.Second)
	for {
		ended := 0
		for _, l := range rec.lines() {
			if strings.Contains(l, "voice: link pump ended") && !strings.Contains(l, "with error") {
				ended++
			}
		}
		if ended >= 2 {
			break
		}
		if time.Now().After(endedDeadline) {
			t.Fatalf("pump end telemetry lines = %d, want 2; lines=%v", ended, rec.lines())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// linkDialChildPairCapturing dials the LOCAL LEG's child pair under
// SocketDir like the fixture's dialPair, but captures everything the
// splice writes toward the child on .in (the shared childInCapture map
// used by localChildInBytes) instead of discarding it. Returns the
// child-side .out conn — the injection point for that leg's speech.
// This is what makes the crossed end-to-end assertion possible: bytes
// OUT of one leg's child socket must land IN the other leg's child
// socket after two splices + two bridge pumps.
func linkDialChildPairCapturing(t *testing.T, ccid string) net.Conn {
	t.Helper()
	dir := SocketDir()
	childInMu.Lock()
	delete(childInCapture, ccid)
	childInMu.Unlock()
	var c1, c2 net.Conn
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var e1, e2 error
		if c1 == nil {
			c1, e1 = net.Dial("unix", filepath.Join(dir, ccid+".in"))
		}
		if c2 == nil {
			c2, e2 = net.Dial("unix", filepath.Join(dir, ccid+".out"))
		}
		if e1 == nil && e2 == nil {
			go func() {
				buf := make([]byte, 8192)
				for {
					n, err := c1.Read(buf)
					if n > 0 {
						childInMu.Lock()
						childInCapture[ccid] = append(childInCapture[ccid], buf[:n]...)
						childInMu.Unlock()
					}
					if err != nil {
						return
					}
				}
			}()
			t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
			return c2
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("could not dial child pair for %s", ccid)
	return nil
}

// linkWriteChunks writes n deterministic 640 B chunks and returns the
// exact bytes sent.
func linkWriteChunks(t *testing.T, w io.Writer, n int) []byte {
	t.Helper()
	var payload []byte
	for i := range n {
		chunk := make([]byte, 640)
		for j := range chunk {
			chunk[j] = byte((i*640 + j) % 251)
		}
		if _, err := w.Write(chunk); err != nil {
			t.Fatalf("write child chunk: %v", err)
		}
		payload = append(payload, chunk...)
	}
	return payload
}

// linkWaitBytes polls snap until it holds >= want bytes (bounded).
func linkWaitBytes(t *testing.T, snap func() []byte, want int) []byte {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		got := snap()
		if len(got) >= want {
			return got
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d bytes downstream (have %d)", want, len(snap()))
	return nil
}

func TestLinkConcurrentLabelsAreDistinct(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	l1, err := m.Open(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("Open 1: %v", err)
	}
	l2, err := m.Open(context.Background(), "carol", "dave")
	if err != nil {
		t.Fatalf("Open 2: %v", err)
	}
	t.Cleanup(func() {
		_ = m.Close(l1.ID, "teardown")
		_ = m.Close(l2.ID, "teardown")
	})
	opens := leg.openSnap()
	want := []string{"alice|link-a-", "bob|link-b-", "carol|link-a-", "dave|link-b-"}
	for i, o := range opens {
		if !strings.HasPrefix(o, want[i]) {
			t.Fatalf("open %d label = %q, want prefix %q", i, o, want[i])
		}
	}
	// The two links' label suffixes differ (the link id rides them), so
	// the registry's one-live-per-target rule never collides between
	// concurrent links.
	if opens[0] == opens[2] || opens[1] == opens[3] {
		t.Fatalf("concurrent links reused a label: %v", opens)
	}
	if l1.ID == l2.ID {
		t.Fatal("link ids collided")
	}
}

// ---------------------------------------------------------------------------
// Monitor tap (audible speaker tap): Monitor=true stands up two unix
// listeners in the pair dir and tees each pump direction's bytes. Pinned:
// both directions carry the driven PCM to dialed monitor clients; no
// stall / no error when no client is attached (late dialer gets LIVE
// bytes, old ones may drop - the bytes-may-drop contract); teardown
// removes the socket files; a sun_path overflow refuses with the named
// error and unwinds both legs.
// ---------------------------------------------------------------------------

// monitorDial dials one tap path with a bounded retry (the listener
// exists before Open returns, so one dial normally succeeds instantly)
// and waits until the bridge's accept goroutine has STORED the conn -
// without that wait the first tee batch could race the accept and drop
// (the bytes-may-drop contract makes that legal, so assertions that
// demand exact bytes must pin attachment first).
func monitorDial(t *testing.T, m *LinkManager, linkID, path string, sideA bool) net.Conn {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		c, err := net.Dial("unix", path)
		if err == nil {
			t.Cleanup(func() { _ = c.Close() })
			waitMonitorStored(t, m, linkID, sideA)
			return c
		}
		if time.Now().After(deadline) {
			t.Fatalf("dial monitor socket %s: %v", path, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// waitMonitorStored polls the link entry's monitor listener until the
// accepted conn is stored (same-package test access; the manager mutex
// guards the map, the listener mutex guards the conn).
func waitMonitorStored(t *testing.T, m *LinkManager, linkID string, sideA bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		m.mu.Lock()
		entry := m.links[linkID]
		var ml *monitorListener
		if entry != nil {
			if sideA {
				ml = entry.monA
			} else {
				ml = entry.monB
			}
		}
		m.mu.Unlock()
		if ml != nil && ml.current() != nil {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("monitor tap conn never stored for %s (sideA=%v)", linkID, sideA)
		}
		time.Sleep(2 * time.Millisecond)
	}
}

func TestLinkMonitorTeesBothDirections(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	link, err := m.Open(context.Background(), "alice", "bob", LinkOpenOptions{Monitor: true})
	if err != nil {
		t.Fatalf("Open(monitor): %v", err)
	}
	t.Cleanup(func() { _ = m.Close(link.ID, "test teardown") })
	if link.MonitorAPath == "" || link.MonitorBPath == "" {
		t.Fatalf("monitor paths empty: %+v", link)
	}

	// The tap socket files live in the pair dir with the .mon-a/.mon-b
	// suffix beside the link id.
	if filepath.Dir(link.MonitorAPath) != leg.dir ||
		!strings.HasSuffix(link.MonitorAPath, ".mon-a") ||
		!strings.HasSuffix(link.MonitorBPath, ".mon-b") ||
		!strings.Contains(filepath.Base(link.MonitorAPath), link.ID) {
		t.Fatalf("monitor paths %q/%q not <pairdir %q>/%s.mon-{a,b}", link.MonitorAPath, link.MonitorBPath, leg.dir, link.ID)
	}

	aMon := monitorDial(t, m, link.ID, link.MonitorAPath, true)
	bMon := monitorDial(t, m, link.ID, link.MonitorBPath, false)

	aSpk := leg.accepted(link.ACCID, false)
	defer aSpk.Close()
	bSpk := leg.accepted(link.BCCID, false)
	defer bSpk.Close()

	// A speaks: bytes ride BOTH the bridge (B's mic) and the a->b tap.
	sentA := linkSpeakerChunks(t, aSpk)
	gotTapA := linkReadN(t, aMon, len(sentA))
	if string(gotTapA) != string(sentA) {
		t.Fatalf("monitor a->b tee corrupted: got %d B, want %d B exact", len(gotTapA), len(sentA))
	}
	// B speaks: the b->a tap mirrors.
	sentB := linkSpeakerChunks(t, bSpk)
	gotTapB := linkReadN(t, bMon, len(sentB))
	if string(gotTapB) != string(sentB) {
		t.Fatalf("monitor b->a tee corrupted: got %d B, want %d B exact", len(gotTapB), len(sentB))
	}

	// Teardown: Close kills listeners + tapped conns (the monitor client
	// sees EOF, the socket files vanish).
	if err := m.Close(link.ID, "test close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	_ = aMon.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := aMon.Read(make([]byte, 1)); err == nil {
		t.Fatal("monitor a conn still live after Close")
	}
	for _, p := range []string{link.MonitorAPath, link.MonitorBPath} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("monitor socket %s survived Close (err=%v)", p, err)
		}
	}
}

// TestLinkMonitorNoClientNoStall pins the bytes-may-drop contract: PCM
// flows across the bridge with NO monitor client attached, the pumps
// never stall or error, and a late dialer receives live bytes afterwards
// (the pre-dial bytes are gone - asserted loosely on purpose).
func TestLinkMonitorNoClientNoStall(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	link, err := m.Open(context.Background(), "alice", "bob", LinkOpenOptions{Monitor: true})
	if err != nil {
		t.Fatalf("Open(monitor): %v", err)
	}
	t.Cleanup(func() { _ = m.Close(link.ID, "test teardown") })

	aSpk := leg.accepted(link.ACCID, false)
	defer aSpk.Close()
	bMic := leg.accepted(link.BCCID, true)
	defer bMic.Close()

	// Drain the bridge's mic-join silence seed first (see
	// TestLinkBridgeByteExact).
	linkDrainSilenceSeed(t, bMic)

	// Drive 20 chunks while nobody listens on the tap socket. The
	// bridge must stay byte-exact regardless.
	var sent []byte
	sent = append(sent, linkSpeakerChunks(t, aSpk)...)
	sent = append(sent, linkSpeakerChunks(t, aSpk)...)
	got := linkReadN(t, bMic, len(sent))
	if string(got) != string(sent) {
		t.Fatalf("bridge corrupted with no monitor client: got %d B, want %d B", len(got), len(sent))
	}

	// Late dialer: new speech must reach the tap (pre-dial bytes may be
	// dropped - no assertion on them).
	late := monitorDial(t, m, link.ID, link.MonitorAPath, true)
	sentLate := linkSpeakerChunks(t, aSpk)
	gotLate := linkReadN(t, late, len(sentLate))
	if string(gotLate) != string(sentLate) {
		t.Fatalf("late monitor dialer missed live bytes: got %d B, want %d B exact tail", len(gotLate), len(sentLate))
	}
}

// TestLinkMonitorSecondClientRefused pins set-once: after one tap
// client, a second dialer is accepted-then-closed (the AudioPair
// posture) while the first keeps its stream.
func TestLinkMonitorSecondClientRefused(t *testing.T) {
	leg := newLinkFakeLeg(t)
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	link, err := m.Open(context.Background(), "alice", "bob", LinkOpenOptions{Monitor: true})
	if err != nil {
		t.Fatalf("Open(monitor): %v", err)
	}
	t.Cleanup(func() { _ = m.Close(link.ID, "test teardown") })

	first := monitorDial(t, m, link.ID, link.MonitorAPath, true)
	second := monitorDial(t, m, link.ID, link.MonitorAPath, true)
	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	if _, err := second.Read(make([]byte, 1)); err == nil {
		t.Fatal("second monitor client kept alive; want immediate close")
	}

	aSpk := leg.accepted(link.ACCID, false)
	defer aSpk.Close()
	sent := linkSpeakerChunks(t, aSpk)
	got := linkReadN(t, first, len(sent))
	if string(got) != string(sent) {
		t.Fatalf("first monitor client lost bytes after a refused second dial")
	}
}

// TestLinkMonitorPathTooLong drives the sun_path guard: a pair dir whose
// prefix sits right at the limit makes the derived .mon-a path overflow,
// and Open must refuse with the named error AFTER unwinding both legs
// with zero listeners created.
func TestLinkMonitorPathTooLong(t *testing.T) {
	dir := filepath.Join(shortDir(t), strings.Repeat("d", 95)) // dir itself < limit, joined path > 100
	leg := newLinkFakeLegWithDir(t, dir)
	leg.barePaths = true // no real sockets: the guard must fire BEFORE any dial
	m := newLinkTestManager(t, leg, &linkFakeStore{calls: map[string][]TranscriptLine{}})
	_, err := m.Open(context.Background(), "alice", "bob", LinkOpenOptions{Monitor: true})
	if !errors.Is(err, ErrLinkMonitorPathTooLong) {
		t.Fatalf("Open = %v, want ErrLinkMonitorPathTooLong", err)
	}
	ends := leg.endSnap()
	if len(ends) != 2 {
		t.Fatalf("path refusal unwound %d legs, want 2 (both): %v", len(ends), ends)
	}
	// Zero monitor listeners: nothing matching *.mon-* exists in the dir.
	entries, rerr := os.ReadDir(dir)
	if rerr != nil {
		t.Fatalf("readdir: %v", rerr)
	}
	for _, e := range entries {
		if strings.Contains(e.Name(), ".mon-") {
			t.Fatalf("monitor socket leaked past the path guard: %s", e.Name())
		}
	}
}

// ---------------------------------------------------------------------------
// Half-duplex turn-taking coordinator (turn-taking leaf): the coordinator
// pins drive a FAKE turn-state stream against the real seam closures; the
// loop-side park/flush semantics are pinned above (fake HoldPrompt, no
// coordinator).
// ---------------------------------------------------------------------------

// loopHeldPrompt reads the loop's parked-prompt slot (in-package test
// seam - heldMu guards only the swap).
func loopHeldPrompt(l *TalkLoop) bool {
	l.heldMu.Lock()
	defer l.heldMu.Unlock()
	return l.held != nil
}

// syncBuffer is a mutex-guarded bytes.Buffer: the coordinator tests read
// log lines while the bound loops' goroutines still write them.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newTestCoordinator(margin time.Duration, logger *slog.Logger) *LinkTurnCoordinator {
	return NewLinkTurnCoordinator("link-test", margin, logger)
}

func TestLinkCoordinatorGreetingSuppression(t *testing.T) {
	c := newTestCoordinator(10*time.Millisecond, linkDiscard())
	if c.SuppressesGreeting(LinkSideA) {
		t.Fatal("side A must NOT suppress its greeting (A greets, B opens silent)")
	}
	if !c.SuppressesGreeting(LinkSideB) {
		t.Fatal("side B must suppress its greeting (single-greeting contract)")
	}
}

func TestLinkCoordinatorHoldDecision(t *testing.T) {
	c := newTestCoordinator(time.Hour, linkDiscard()) // margin never fires in this test
	holdA := c.HoldPromptFor(LinkSideA)
	holdB := c.HoldPromptFor(LinkSideB)
	finalA := c.TurnStateFor(LinkSideA)

	if holdA == nil || holdB == nil {
		t.Fatal("bound sides must get a non-nil HoldPrompt")
	}
	// Idle link: A may speak, and the allow CLAIMS the speaker slot.
	if !holdA(Instance{CallControlID: "cc-a"}, func() {}) {
		t.Fatal("first A prompt with an idle peer must be allowed")
	}
	// While A holds the floor, B's prompt parks.
	if holdB(Instance{CallControlID: "cc-b"}, func() {}) {
		t.Fatal("B prompt while A holds the floor must be held")
	}
	// A's own second prompt is NOT held by itself (same-side supersede
	// is the loop's legacy rule, not the coordinator's).
	if !holdA(Instance{CallControlID: "cc-a"}, func() {}) {
		t.Fatal("same-side prompt must not be held by its own turn")
	}
	// A's turn_final clears BUSY but NOT the floor: the pacer may still
	// hold A's paced audio tail (drain-driven floor, leaf 02). B's next
	// prompt must still park until the drain edge (or the fallback
	// margin) releases the floor.
	finalA("cc-a", 1, TurnKindFinal)
	c.mu.Lock()
	busyA := c.a.busy
	heldA := c.a.floorHeld
	c.mu.Unlock()
	if busyA {
		t.Fatal("A still busy after turn_final")
	}
	if !heldA {
		t.Fatal("A floor must stay held after turn_final (audio tail still pacing)")
	}
	if holdB(Instance{CallControlID: "cc-b"}, func() {}) {
		t.Fatal("B prompt right after A's turn_final must stay held (pre-drain talk-over guard)")
	}
	// The drain edge + tail pad frees the floor: B's next prompt lands.
	c.NoteAudioDrained("cc-a")
	waitFor(t, 2*time.Second, "floor freed after drain+pad", func() bool {
		c.mu.Lock()
		held := c.a.floorHeld
		c.mu.Unlock()
		return !held
	})
	if !holdB(Instance{CallControlID: "cc-b"}, func() {}) {
		t.Fatal("B prompt after A's drained floor must be allowed")
	}
}

// TestLinkCoordinatorHeldPromptReleasesAfterMargin wires TWO real talk
// loops to the coordinator exactly like the daemon's routeFrame does for
// a link, drives a fake A turn-state stream, and pins the full release
// path: B parks while A is mid-turn, the drain margin must elapse before
// the release, and the parked dispatch then runs with prompt_released.
func TestLinkCoordinatorHeldPromptReleasesAfterMargin(t *testing.T) {
	var buf syncBuffer
	logger := slog.New(slog.NewTextHandler(&buf, nil))
	sink := &recordSink{}
	sup := &cannedSupervisor{}
	turnsB := &holdTurns{reply: "Answer for B."}
	turnsA := &holdTurns{reply: "Answer for A."}

	c := newTestCoordinator(80*time.Millisecond, linkDiscard())

	// REAL B loop, bound like routeFrame does (HoldPrompt + TurnState +
	// Bind).
	loopB := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turnsB,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     logger,
		HoldPrompt: c.HoldPromptFor(LinkSideB),
		TurnState:  c.TurnStateFor(LinkSideB),
	}, TalkLoopConfig{})
	c.Bind(LinkSideB, loopB)
	// REAL A loop with the single-greeting suppression honored.
	loopA := NewTalkLoop(TalkDeps{
		Supervisor: sup,
		Turns:      turnsA,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       sink.Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     logger,
		HoldPrompt: c.HoldPromptFor(LinkSideA),
		TurnState:  c.TurnStateFor(LinkSideA),
	}, TalkLoopConfig{SuppressGreeting: true})
	c.Bind(LinkSideA, loopA)

	cancelB, doneB := runLoop(t, loopB, "cc-b")
	defer func() { cancelB(); <-doneB }()
	cancelA, doneA := runLoop(t, loopA, "cc-a")
	defer func() { cancelA(); <-doneA }()

	// B parks its question while A is mid-turn (the fake stream marks
	// the floor taken).
	c.TurnStateFor(LinkSideA)("cc-a", 1, TurnKindStart)
	loopB.frames <- finalPrompt("cc-b", "question while A talks")
	waitFor(t, 2*time.Second, "park", func() bool {
		return loopHeldPrompt(loopB) && turnsB.count() == 0
	})

	// A finalizes; the margin must elapse BEFORE the release: sample
	// early and assert still parked.
	start := time.Now()
	c.TurnStateFor(LinkSideA)("cc-a", 1, TurnKindFinal)
	time.Sleep(20 * time.Millisecond)
	if turnsB.count() != 0 || !loopHeldPrompt(loopB) {
		t.Fatal("B released before the drain margin elapsed")
	}
	waitFor(t, 3*time.Second, "B released after margin", func() bool { return turnsB.count() == 1 })
	if elapsed := time.Since(start); elapsed < 80*time.Millisecond {
		t.Fatalf("release at %v < margin 80ms", elapsed)
	}

	// Telemetry pins: prompt_held then prompt_released, each with ccid.
	logs := buf.String()
	for _, want := range []string{`kind=prompt_held`, `ccid=cc-b`, `kind=prompt_released`} {
		if !strings.Contains(logs, want) {
			t.Errorf("telemetry missing %q:\n%s", want, logs)
		}
	}
	// B's released turn spoke its reply.
	waitFor(t, 2*time.Second, "B reply spoken", func() bool { return len(sink.sayTexts()) >= 1 })
}

// TestLinkCoordinatorIdleReleasesHeldPromptImmediately pins the barge-in
// edge: a speaker going idle WITHOUT a final (nothing is playing)
// releases the parked prompt at once - no margin wait.
func TestLinkCoordinatorIdleReleasesHeldPromptImmediately(t *testing.T) {
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(time.Hour, linkDiscard()) // margin never fires
	loopB := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turnsB,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
		HoldPrompt: c.HoldPromptFor(LinkSideB),
		TurnState:  c.TurnStateFor(LinkSideB),
	}, TalkLoopConfig{})
	c.Bind(LinkSideB, loopB)
	cancelB, doneB := runLoop(t, loopB, "cc-b2")
	defer func() { cancelB(); <-doneB }()

	c.TurnStateFor(LinkSideA)("cc-a", 1, TurnKindStart)
	loopB.frames <- finalPrompt("cc-b2", "parked under A")
	waitFor(t, 2*time.Second, "park", func() bool { return loopHeldPrompt(loopB) })

	c.TurnStateFor(LinkSideA)("cc-a", 1, TurnKindIdle)
	waitFor(t, 2*time.Second, "idle release", func() bool { return turnsB.count() == 1 })
}

// TestLinkCoordinatorUnbindReleasesHeldPrompt pins teardown: A's loop
// dying must not strand B's parked prompt.
func TestLinkCoordinatorUnbindReleasesHeldPrompt(t *testing.T) {
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(time.Hour, linkDiscard())
	loopB := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turnsB,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
		HoldPrompt: c.HoldPromptFor(LinkSideB),
		TurnState:  c.TurnStateFor(LinkSideB),
	}, TalkLoopConfig{})
	c.Bind(LinkSideB, loopB)
	cancelB, doneB := runLoop(t, loopB, "cc-b3")
	defer func() { cancelB(); <-doneB }()

	c.TurnStateFor(LinkSideA)("cc-a", 1, TurnKindStart)
	loopB.frames <- finalPrompt("cc-b3", "parked under A")
	waitFor(t, 2*time.Second, "park", func() bool { return loopHeldPrompt(loopB) })

	c.Unbind(LinkSideA)
	waitFor(t, 2*time.Second, "unbind release", func() bool { return turnsB.count() == 1 })
}

// TestLinkCoordinatorStaleMarginFireNoRelease pins the generation
// guard: A finalizes (margin armed), then A starts a NEW turn before
// the fire - the stale margin must NOT release B's park (the floor is
// taken again).
func TestLinkCoordinatorStaleMarginFireNoRelease(t *testing.T) {
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(40*time.Millisecond, linkDiscard())
	loopB := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turnsB,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     testLogger(),
		HoldPrompt: c.HoldPromptFor(LinkSideB),
		TurnState:  c.TurnStateFor(LinkSideB),
	}, TalkLoopConfig{})
	c.Bind(LinkSideB, loopB)
	cancelB, doneB := runLoop(t, loopB, "cc-b4")
	defer func() { cancelB(); <-doneB }()

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a", 1, TurnKindStart)
	loopB.frames <- finalPrompt("cc-b4", "parked")
	waitFor(t, 2*time.Second, "park", func() bool { return loopHeldPrompt(loopB) })

	stateA("cc-a", 1, TurnKindFinal)
	stateA("cc-a", 2, TurnKindStart) // A speaks again before the margin fires
	time.Sleep(120 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatal("stale margin released B while A re-took the floor")
	}
	if !loopHeldPrompt(loopB) {
		t.Fatal("park vanished without a release")
	}
	// A's second turn finalizes -> THIS margin releases.
	stateA("cc-a", 2, TurnKindFinal)
	waitFor(t, 2*time.Second, "second margin releases", func() bool { return turnsB.count() == 1 })
}

func TestLinkCoordinatorRegistryRoundTrip(t *testing.T) {
	reg := NewLinkTurnRegistry(linkDiscard())
	c := newTestCoordinator(time.Second, linkDiscard())
	reg.Register("local:link-a-x", "link-x", LinkSideA, c)
	reg.Register("local:link-b-x", "link-x", LinkSideB, c)

	got, side := reg.Lookup("local:link-a-x")
	if got != c || side != LinkSideA {
		t.Fatalf("Lookup a = %v/%q", got, side)
	}
	// Non-link targets never match (plain local/phone calls are
	// unaffected by the seam).
	if got, _ := reg.Lookup("local:plain"); got != nil {
		t.Fatal("plain target must not resolve a coordinator")
	}
	if got, _ := reg.Lookup(""); got != nil {
		t.Fatal("empty target must not resolve")
	}
	reg.Unregister("local:link-a-x", "local:link-b-x")
	if got, _ := reg.Lookup("local:link-a-x"); got != nil {
		t.Fatal("unregistered target still resolves")
	}
	// Idempotent second unregister.
	reg.Unregister("local:link-a-x", "local:link-b-x")
}

// TestLinkOpenRegistersCoordinatorTargets drives the real LinkManager:
// a link's registry targets resolve to a live coordinator BEFORE the
// legs' setup frames could arrive, side b carries the greeting
// suppression, and Close tears the registration down.
func TestLinkOpenRegistersCoordinatorTargets(t *testing.T) {
	leg := newLinkFakeLeg(t)
	reg := NewLinkTurnRegistry(linkDiscard())
	m, err := NewLinkManager(LinkDeps{
		Leg:         leg,
		Store:       &linkFakeStore{calls: map[string][]TranscriptLine{}},
		DialTimeout: 3 * time.Second,
		RetainEnded: time.Hour,
		Turns:       reg,
		TurnMargin:  25 * time.Millisecond,
		Logger:      linkDiscard(),
	})
	if err != nil {
		t.Fatalf("NewLinkManager: %v", err)
	}
	link, err := m.Open(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	suffix := link.ID[len("link-"):]
	coordA, sideA := reg.Lookup("local:link-a-" + suffix)
	coordB, sideB := reg.Lookup("local:link-b-" + suffix)
	if coordA == nil || coordB == nil || coordA != coordB {
		t.Fatalf("link targets unresolved: a=%v b=%v", coordA, coordB)
	}
	if sideA != LinkSideA || sideB != LinkSideB {
		t.Fatalf("sides = %q/%q, want a/b", sideA, sideB)
	}
	if !coordB.SuppressesGreeting(sideB) {
		t.Fatal("greeting suppression must land on side b")
	}
	if coordA.SuppressesGreeting(sideA) {
		t.Fatal("side a must greet (exactly one greeting per link)")
	}
	if err := m.Close(link.ID, "test close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got, _ := reg.Lookup("local:link-a-" + suffix); got != nil {
		t.Fatal("Close left the coordinator registered")
	}
	if got, _ := reg.Lookup("local:link-b-" + suffix); got != nil {
		t.Fatal("Close left the b-side coordinator registered")
	}
}

// TestLinkOpenUnwindUnregisters covers the refusal path: a failed second
// leg open must drop the pre-registered targets (no stale coordinator
// for a never-existing link).
func TestLinkOpenUnwindUnregisters(t *testing.T) {
	leg := newLinkFakeLeg(t)
	leg.failOpenN = 2
	reg := NewLinkTurnRegistry(linkDiscard())
	m, err := NewLinkManager(LinkDeps{
		Leg:         leg,
		Store:       &linkFakeStore{calls: map[string][]TranscriptLine{}},
		DialTimeout: 3 * time.Second,
		RetainEnded: time.Hour,
		Turns:       reg,
		Logger:      linkDiscard(),
	})
	if err != nil {
		t.Fatalf("NewLinkManager: %v", err)
	}
	if _, err := m.Open(context.Background(), "alice", "bob"); err == nil {
		t.Fatal("Open must refuse")
	}
	reg.mu.Lock()
	n := len(reg.byTarget)
	reg.mu.Unlock()
	if n != 0 {
		t.Fatalf("%d coordinator targets leaked past an unwound Open", n)
	}
}

// ---------------------------------------------------------------------------
// Drain-driven floor (turn-taking leaf 02): the floor releases on the
// splice's AudioDrained edge (+ tail pad) or on the LONG fallback margin,
// whichever comes first. turn_final alone must never free the floor early.
// ---------------------------------------------------------------------------

// bindSideBLoop wires a REAL B loop to the coordinator (park path) and
// parks a prompt under a mid-turn A. Returns the loop + turn counters.
func bindSideBLoop(t *testing.T, c *LinkTurnCoordinator, buf *syncBuffer, turnsB *holdTurns) *TalkLoop {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(buf, nil))
	loopB := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turnsB,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     logger,
		HoldPrompt: c.HoldPromptFor(LinkSideB),
		TurnState:  c.TurnStateFor(LinkSideB),
	}, TalkLoopConfig{})
	c.Bind(LinkSideB, loopB)
	cancelB, doneB := runLoop(t, loopB, "cc-b-drain")
	t.Cleanup(func() { cancelB(); <-doneB })
	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindStart)
	loopB.frames <- finalPrompt("cc-b-drain", "parked under A")
	waitFor(t, 2*time.Second, "park", func() bool { return loopHeldPrompt(loopB) })
	return loopB
}

// T-DR1: the drain edge fires LATE (longer than the legacy 600 ms margin
// would have allowed) - the coordinator must ride the drain, not a short
// margin: B stays parked while the floor is held, releases at
// drain + pad (never before), and the telemetry carries reason=drained.
func TestLinkCoordinatorFloorReleasesOnLateDrain(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	// Fallback margin deliberately LONG: proves the release is the DRAIN's
	// doing, not the margin's. (Production default is 4 s; same shape.) The
	// coordinator shares the buffer logger: floor_released telemetry is
	// asserted from it.
	c := newTestCoordinator(time.Hour, slog.New(slog.NewTextHandler(&buf, nil)))
	bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal) // text-side final: floor STAYS held

	// A fake "pacing tail" outlasts the old 600 ms window entirely.
	time.Sleep(300 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatal("B dispatched on turn_final alone: the drain contract is broken")
	}

	// The drain edge lands late; release must follow at pad (250 ms),
	// and NOT earlier.
	c.NoteAudioDrained("cc-a-drain")
	time.Sleep(120 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatal("B released before the tail pad elapsed")
	}
	waitFor(t, 3*time.Second, "B released after drain+pad", func() bool { return turnsB.count() == 1 })
	logs := buf.String()
	if !strings.Contains(logs, "kind=floor_released") {
		t.Fatalf("telemetry missing floor_released:\n%s", logs)
	}
	if !strings.Contains(logs, "reason=drained") {
		t.Fatalf("telemetry reason must be drained:\n%s", logs)
	}
	if strings.Contains(logs, "reason=margin") {
		t.Fatalf("margin must not have fired (fallback was 1h):\n%s", logs)
	}
}

// T-DR2: the drain edge fires EARLY (right after turn_final) - the floor
// releases promptly at drain + pad, well before the fallback margin.
func TestLinkCoordinatorFloorReleasesPromptlyOnEarlyDrain(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(time.Hour, slog.New(slog.NewTextHandler(&buf, nil)))
	bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	start := time.Now()
	stateA("cc-a-drain", 1, TurnKindFinal)
	c.NoteAudioDrained("cc-a-drain") // drained immediately (short turn)
	waitFor(t, 3*time.Second, "prompt drain release", func() bool { return turnsB.count() == 1 })
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("drain release took %v, want promptly after pad (250ms)", elapsed)
	}
	if elapsed < linkFloorTailPad {
		t.Fatalf("drain release at %v < tail pad %v", elapsed, linkFloorTailPad)
	}
	if logs := buf.String(); !strings.Contains(logs, "reason=drained") {
		t.Fatalf("telemetry missing reason=drained:\n%s", logs)
	}
}

// T-DR3: no drain signal at all (splice died mid-turn) - the LONG
// fallback margin bounds the hold and releases with reason=margin.
func TestLinkCoordinatorFloorFallbackMarginReleases(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(150*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil))) // short fallback for the test
	bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal)
	time.Sleep(60 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatal("B released before the fallback margin")
	}
	waitFor(t, 3*time.Second, "fallback margin release", func() bool { return turnsB.count() == 1 })
	if logs := buf.String(); !strings.Contains(logs, "reason=margin") {
		t.Fatalf("telemetry missing reason=margin:\n%s", logs)
	}
}

// T-DR4 (both-sides race): A finalizes AND A's audio drains while B's
// flush signal is already queued - B's parked prompt re-gates against the
// floor, and B dispatches only after A's drain + pad. Also pins the FRESH
// claim path: a brand-new B prompt during A's drain tail parks instead of
// claiming (the measured talk-over channel).
func TestLinkCoordinatorDrainRaceGatesFreshClaims(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(time.Hour, linkDiscard())
	loopB := bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal)
	// Drain + queued flush race: fire the drain and ping the loop
	// simultaneously - the floor stays held until pad, then releases.
	c.NoteAudioDrained("cc-a-drain")
	loopB.FlushHeld()
	time.Sleep(120 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatal("B dispatched during A's drain tail (both-sides bug)")
	}
	waitFor(t, 3*time.Second, "release after drain+pad despite queued flush", func() bool { return turnsB.count() == 1 })

	// Fresh-claim gate: A speaks again (floor held), finalizes, and does
	// NOT drain yet - a brand-new B prompt must park, not claim.
	//
	// CORRECTED-CONTRACT UPDATE (holder-only release, link-b9d221d8d8cddcce):
	// B's flushed turn above made B the real HOLDER (its loop's claim
	// gated through, its dispatch ran a full turn, and its turn_final
	// armed B's margin). The old phase-2 shape drove A's seq-2 through a
	// BARE turn_start edge that silently overwrote the floor - the exact
	// uncoordinated-edge channel the fix closes: a turn_start from a side
	// whose peer holds the floor is a LISTENER edge and must NOT steal
	// the floor. The gate is now driven through the CLAIM path like a
	// real loop does: drain B's tail first (the holder's audio truth),
	// then A claims, finals, and does NOT drain yet.
	// Wait for B's dispatched turn to reach its quiet final (holder=b,
	// not busy) BEFORE draining it: a drain that lands while B is still
	// mid-turn is a no-op by design, and under package load the loop's
	// turn_final edge can lag the dispatch.
	waitFor(t, 3*time.Second, "b's turn reached its quiet final", func() bool {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.holder == LinkSideB && !c.b.busy && c.b.floorHeld
	})
	c.NoteAudioDrained("cc-b-drain") // B's dispatched turn drains
	waitFor(t, 3*time.Second, "b's drained tail releases the floor", func() bool {
		c.mu.Lock()
		free := c.holder == "" && !c.b.floorHeld
		c.mu.Unlock()
		return free
	})
	holdA := c.HoldPromptFor(LinkSideA)
	if !holdA(Instance{CallControlID: "cc-a-drain"}, func() {}) {
		t.Fatal("A claim after B's drained release must be allowed")
	}
	stateA("cc-a-drain", 2, TurnKindStart) // idempotent confirmation of A's claim
	stateA("cc-a-drain", 2, TurnKindFinal)
	holdB := c.HoldPromptFor(LinkSideB)
	if holdB(Instance{CallControlID: "cc-b-fresh"}, func() {}) {
		t.Fatal("fresh B prompt claimed the floor while A's audio tail still paced")
	}
	// Drain A's second turn -> the floor frees and the claim lands.
	c.NoteAudioDrained("cc-a-drain")
	waitFor(t, 3*time.Second, "second drain frees the floor", func() bool {
		c.mu.Lock()
		held := c.a.floorHeld
		c.mu.Unlock()
		return !held
	})
	if !holdB(Instance{CallControlID: "cc-b-fresh"}, func() {}) {
		t.Fatal("fresh B prompt must be allowed once A's floor drained")
	}
}

// T-DR5: stale drain edges are no-ops - a drain for a ccid that does not
// match any floor-holding side, or arriving after that side went mid-turn
// again, must not release anything.
func TestLinkCoordinatorStaleDrainIgnored(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(time.Hour, linkDiscard())
	bindSideBLoop(t, c, &buf, turnsB)

	// Foreign ccid: no side holds it.
	c.NoteAudioDrained("cc-nobody")
	// A re-took the floor (mid-turn): a drain for A is stale while busy.
	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 2, TurnKindStart)
	c.NoteAudioDrained("cc-a-drain")
	time.Sleep(400 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatal("stale drain released the floor")
	}
	// The live chain still releases: final + drain.
	stateA("cc-a-drain", 2, TurnKindFinal)
	c.NoteAudioDrained("cc-a-drain")
	waitFor(t, 3*time.Second, "live drain release after re-take", func() bool { return turnsB.count() == 1 })
}

// T-DR6: the FULL seam end to end inside the package - a real local
// splice (paced child audio to a net.Pipe client) wired through
// wiredAudioDrain onto a real coordinator, exactly as LocalLeg.Open does
// for a link leg (registry pre-registered by the link manager before the
// leg opens). Pins: the floor stays held while audio paces, the turn_final
// TEXT edge only EXPECTS the C1 played edge (no blind arm - the
// mid-synth-gap early release it caused on link-2fdc3f5fd8d81805), the
// played edge arms the watch, the drain fires from the real outbound
// quiescence, and the release lands with reason=drained.
func TestLinkDrainSeamEndToEnd(t *testing.T) {
	var buf syncBuffer
	c := NewLinkTurnCoordinator("link-drain-e2e", time.Hour, slog.New(slog.NewTextHandler(&buf, nil)))
	reg := NewLinkTurnRegistry(linkDiscard())
	const ccid = "ls-drain-e2e"
	const target = "local:link-a-e2e"
	reg.Register(target, "link-drain-e2e", LinkSideA, c)

	drainFn := wiredAudioDrain(reg, target)
	if drainFn == nil {
		t.Fatal("link target must resolve a drain callback")
	}
	if wiredAudioDrain(reg, "local:plain") != nil {
		t.Fatal("a plain local target must resolve NO drain callback")
	}

	spliceCfg := LocalSpliceConfig{DrainQuiet: 150 * time.Millisecond, DrainQuietFinal: 60 * time.Millisecond}
	p, micSide, _, _, cancel, done := startDrainSplice(t, ccid, spliceCfg, drainFn)
	_ = p
	waitForSplice(t, ccid) // registration races the splice goroutine; the
	// production arm (turn_final) always lands long after Leg.Open registered.
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("splice did not exit")
		}
	}()

	// Side A claims the floor (fresh prompt path - no loop needed for
	// the state machine), speaks, and finalizes.
	holdA := c.HoldPromptFor(LinkSideA)
	if !holdA(Instance{CallControlID: ccid}, func() {}) {
		t.Fatal("first A claim must be allowed")
	}
	c.TurnStateFor(LinkSideA)(ccid, 1, TurnKindStart)

	// Feed paced audio: the pump chunks must drain to the client.
	stopRead := make(chan struct{})
	go func() {
		buf := make([]byte, 640)
		for {
			select {
			case <-stopRead:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, err := micSide.Read(buf); err != nil {
				var ne net.Error
				if errors.As(err, &ne) && ne.Timeout() {
					continue
				}
				return
			}
		}
	}()
	defer close(stopRead)
	for range 8 {
		if err := feedChild(ccid); err != nil {
			t.Fatalf("feedChild: %v", err)
		}
	}
	c.TurnStateFor(LinkSideA)(ccid, 1, TurnKindFinal)

	// turn_final alone must NOT arm the watch any more (the played-edge
	// contract): with the pacer quiet, a blind arm would fire on the
	// 150 ms quiet window mid-reply. The splice records expect-final
	// instead; the release only becomes possible once the C1 played
	// edge lands (here: the same call MarkFinalPlayed that
	// routeFrame makes for a live local call).
	if err := feedChild(ccid); err != nil {
		t.Fatalf("feedChild: %v", err)
	}
	sp := LocalSpliceFor(ccid)
	sp.mu.Lock()
	expectSet, armedYet := sp.drainExpectFinal, sp.drainArmed
	sp.mu.Unlock()
	if !expectSet {
		t.Fatal("turn_final must set the expect-final state on the splice")
	}
	if armedYet {
		t.Fatal("turn_final must NOT blind-arm the drain watch (mid-synth-gap early release)")
	}
	if !sp.MarkFinalPlayed() {
		t.Fatal("played edge must arm the drain watch on a live link splice")
	}

	// The floor must still be held while the audio tail paces (8 chunks
	// ~ 160 ms + the 150 ms quiet window).
	time.Sleep(80 * time.Millisecond)
	c.mu.Lock()
	heldDuringTail := c.a.floorHeld
	c.mu.Unlock()
	if !heldDuringTail {
		t.Fatal("floor released while paced audio was still in the pacer")
	}

	// Real drain -> real release with reason=drained. Poll the
	// TELEMETRY (not the latch flip: releaseFloor clears floorHeld
	// under the lock before logging, so a state poll can win the log
	// write).
	waitFor(t, 4*time.Second, "floor released via real drain edge", func() bool {
		logs := buf.String()
		return strings.Contains(logs, "kind=floor_released") && strings.Contains(logs, "reason=drained")
	})
	// A stale re-fire (same ccid, floor no longer held) is a no-op.
	c.NoteAudioDrained(ccid)
	c.mu.Lock()
	reHeld := c.a.floorHeld
	c.mu.Unlock()
	if reHeld {
		t.Fatal("stale drain re-held the floor")
	}

	// TURN 2 (the measured production shape): A claims again and
	// finalizes with a NON-640-ALIGNED tail — 4 whole chunks plus a
	// 260-byte partial. A real child's stream length is never
	// 640-aligned, so the pump's carry stays non-zero for the rest of
	// the call; the old carry==0 predicate made this release ride the
	// margin every time (0/5 drained across link-6eb490abd8349958).
	// The floor must still free on the real drain edge.
	if !c.HoldPromptFor(LinkSideA)(Instance{CallControlID: ccid}, func() {}) {
		t.Fatal("A re-claim after the first drain must be allowed")
	}
	c.TurnStateFor(LinkSideA)(ccid, 2, TurnKindStart)
	for range 4 {
		if err := feedChild(ccid); err != nil {
			t.Fatalf("feedChild turn-2: %v", err)
		}
	}
	c.TurnStateFor(LinkSideA)(ccid, 2, TurnKindFinal)
	if err := feedChildBytes(ccid, 260); err != nil {
		t.Fatalf("partial tail: %v", err)
	}
	// The played edge for turn 2 (the child's last stream byte left
	// the socket): arms the watch in final mode again.
	if !sp.MarkFinalPlayed() {
		t.Fatal("played edge must arm the drain watch for turn 2")
	}
	waitFor(t, 6*time.Second, "turn-2 floor released via real drain with stuck carry", func() bool {
		return strings.Count(buf.String(), "kind=floor_released") >= 2
	})
	sp.mu.Lock()
	carryStuck, disarmed := sp.audioCarry.Load(), !sp.drainArmed
	expectAfterFire := sp.drainExpectFinal
	sp.mu.Unlock()
	if carryStuck == 0 {
		t.Fatal("test shape broken: partial tail must leave non-zero carry")
	}
	if !disarmed {
		t.Fatal("watch must self-disarm after firing (next turn_final re-arms fresh)")
	}
	if expectAfterFire {
		t.Fatal("fire must consume the expect-final flag (a stale park must not re-open the turn)")
	}
	if strings.Contains(buf.String(), "reason=margin") {
		t.Fatal("margin fired before the drain edge (fallback beat the real signal)")
	}
}

// T-DR7: the fallback margin DEFAULT is the watchdog, not the normal
// release path — measured tails run 5-10 s (write_ms=11136), so a 4 s
// margin beat the drain to every long turn's release.
func TestLinkFloorFallbackDefaultIsWatchdogOnly(t *testing.T) {
	if linkFloorFallbackDefault != 12*time.Second {
		t.Fatalf("fallback margin = %v, want 12 s (drain must be the normal release; margin only bounds a dead splice)", linkFloorFallbackDefault)
	}
	c := NewLinkTurnCoordinator("link-margin-default", 0, linkDiscard())
	if c.margin != linkFloorFallbackDefault {
		t.Fatalf("zero-margin coordinator fell back to %v, want %v", c.margin, linkFloorFallbackDefault)
	}
}

// T-DR8: a turn_final whose splice is missing (never registered) must
// WARN with kind=drain_arm_missed — the instrumentation that separates
// "never armed" from "armed, never quiescent" in the live logs.
func TestLinkCoordinatorWarnsWhenDrainArmMisses(t *testing.T) {
	var buf syncBuffer
	c := newTestCoordinator(time.Second, slog.New(slog.NewTextHandler(&buf, nil)))
	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-no-splice", 1, TurnKindStart)
	stateA("cc-no-splice", 1, TurnKindFinal)
	waitFor(t, 3*time.Second, "margin release", func() bool {
		return strings.Contains(buf.String(), "kind=floor_released")
	})
	logs := buf.String()
	if !strings.Contains(logs, "kind=drain_arm_missed") || !strings.Contains(logs, "no live splice") {
		t.Fatalf("missing-splice arm must WARN with the reason:\n%s", logs)
	}
	if strings.Contains(logs, "drain watch armed") {
		t.Fatal("no splice should produce an armed log")
	}
}

// T-DR9: THE measured production defect, end to end on a real splice:
// a two-sentence reply whose inter-sentence synth gap (here 2 s of
// silence at the .out socket — the measured band was 1.4-3.8 s) exceeds
// the quiet window must NOT release the floor at the gap. The old blind
// arm fired mid-reply here (link-2fdc3f5fd8d81805: 60 short both-audible
// runs, 10.94 s total overlap). The release happens only after the child's
// C1 played edge arms the watch.
func TestLinkCoordinatorNoDrainedReleaseDuringSynthGap(t *testing.T) {
	var buf syncBuffer
	c := NewLinkTurnCoordinator("link-synthgap", time.Hour, slog.New(slog.NewTextHandler(&buf, nil)))
	reg := NewLinkTurnRegistry(linkDiscard())
	const ccid = "ls-synthgap"
	const target = "local:link-a-synthgap"
	reg.Register(target, "link-synthgap", LinkSideB, c)

	drainFn := wiredAudioDrain(reg, target)
	if drainFn == nil {
		t.Fatal("link target must resolve a drain callback")
	}
	// Production-shaped windows: 1 s blind quiet (never used now) and
	// the 500 ms final quiet; the synth gap below dwarfs both.
	spliceCfg := LocalSpliceConfig{DrainQuiet: 300 * time.Millisecond, DrainQuietFinal: 150 * time.Millisecond}
	_, micSide, _, _, cancel, done := startDrainSplice(t, ccid, spliceCfg, drainFn)
	waitForSplice(t, ccid)
	defer func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("splice did not exit")
		}
	}()

	// Client consumes paced audio continuously (the CLI twin).
	stopRead := make(chan struct{})
	go func() {
		b := make([]byte, 640)
		for {
			select {
			case <-stopRead:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			_, _ = micSide.Read(b)
		}
	}()
	defer close(stopRead)

	// A claims and speaks sentence 1.
	if !c.HoldPromptFor(LinkSideA)(Instance{CallControlID: ccid}, func() {}) {
		t.Fatal("first A claim must be allowed")
	}
	c.TurnStateFor(LinkSideA)(ccid, 1, TurnKindStart)
	for range 5 {
		if err := feedChild(ccid); err != nil {
			t.Fatalf("feedChild sentence 1: %v", err)
		}
	}
	// The daemon's text pipeline flushes (turn_final) while sentence 2 is
	// still synthesizing at the child — the exact mid-reply edge.
	c.TurnStateFor(LinkSideA)(ccid, 1, TurnKindFinal)

	// Synth gap: NO further child bytes for 2 s — the measured defect
	// band (1.4-3.8 s) and more than 13x the final quiet window. The
	// floor must stay held for the whole gap.
	gapStart := time.Now()
	time.Sleep(2 * time.Second)
	if strings.Contains(buf.String(), "kind=floor_released") {
		t.Fatalf("floor released during a %v ms synth gap before the played edge:\n%s",
			time.Since(gapStart).Milliseconds(), buf.String())
	}
	c.mu.Lock()
	held := c.a.floorHeld
	c.mu.Unlock()
	if !held {
		t.Fatal("floor must stay held until the played edge (mid-reply release is the bug)")
	}

	// Sentence 2 plays and the child sends the played edge after its
	// final byte (routeFrame's MarkFinalPlayed for a live local call).
	for range 4 {
		if err := feedChild(ccid); err != nil {
			t.Fatalf("feedChild sentence 2: %v", err)
		}
	}
	sp := LocalSpliceFor(ccid)
	if !sp.MarkFinalPlayed() {
		t.Fatal("played edge must arm the drain watch")
	}
	waitFor(t, 4*time.Second, "floor released only via the played-armed drain", func() bool {
		logs := buf.String()
		return strings.Contains(logs, "kind=floor_released") && strings.Contains(logs, "reason=drained")
	})
	if strings.Contains(buf.String(), "reason=margin") {
		t.Fatal("margin released instead of the drain (watchdog beat the real edge)")
	}
}

// ---------------------------------------------------------------------------
// Holder-only release (link-b9d221d8d8cddcce, measured 13.70 s overlap /
// 685 both-sides windows): the coordinator must ONLY ever release the
// floor held by the SPEAKER side. A non-holder's turn_final is a
// LISTENING side's chained response to speech it heard while the peer
// still holds - it arms no expect-final, no margin, and no release for
// itself. The live defect: side B (not the holder - A had been speaking
// since ~07:32:54) set expect-final on ITS splice at 07:33:08 and a
// reason=margin release fired 'side=b' at 07:33:20, twelve seconds after
// B's own turn_final and eleven seconds BEFORE A's played edge
// (07:33:31) - handing B the floor while A was still audible.
// ---------------------------------------------------------------------------

// assertFloorInvariant reads the coordinator's slots under the lock and
// pins the holder-only shape: at most one side floorHeld, floorHeld is
// exactly the holder marker's mirror, and a busy side never coexists
// with the PEER's held floor (the both-speak precondition).
func assertFloorInvariant(t *testing.T, c *LinkTurnCoordinator, where string) {
	t.Helper()
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.a.floorHeld && c.b.floorHeld {
		t.Fatalf("%s: both sides floorHeld", where)
	}
	if c.holder == LinkSideA && !c.a.floorHeld {
		t.Fatalf("%s: holder=a but a.floorHeld=false", where)
	}
	if c.holder == LinkSideB && !c.b.floorHeld {
		t.Fatalf("%s: holder=b but b.floorHeld=false", where)
	}
	if c.holder == "" && (c.a.floorHeld || c.b.floorHeld) {
		t.Fatalf("%s: floor held with no holder marker", where)
	}
	if c.a.busy && c.b.floorHeld {
		t.Fatalf("%s: a busy while b holds the floor", where)
	}
	if c.b.busy && c.a.floorHeld {
		t.Fatalf("%s: b busy while a holds the floor", where)
	}
}

// T-HR1 (task test a): the exact live bug. A is the floor HOLDER and
// speaking; B's chained LISTENER turn_final edge (the non-holder's
// response to speech it heard) must NOT arm any release path for
// itself, must NOT release a side=b floor, and must NOT clear A's
// floor. After 2x margin elapses B's parked prompt is STILL parked;
// A's drained edge then releases A's floor and flushes B.
func TestLinkCoordinatorNonHolderFinalCannotReleaseFloor(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	loopB := bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	stateB := c.TurnStateFor(LinkSideB)

	// B's chained listener edges: a bare turn_start + turn_final while
	// A holds (the 07:33:08 expect-final shape). Bookkeeping only.
	stateB("cc-b-drain", 2, TurnKindStart)
	stateB("cc-b-drain", 2, TurnKindFinal)

	c.mu.Lock()
	bArmed := c.b.marginTimer != nil || c.b.releaseTimer != nil
	bFloor := c.b.floorHeld
	aFloor := c.a.floorHeld
	holder := c.holder
	c.mu.Unlock()
	if bArmed {
		t.Fatal("non-holder final armed a release/margin timer (the 07:33:20 bug)")
	}
	if bFloor {
		t.Fatal("non-holder side must not hold the floor")
	}
	if !aFloor || holder != LinkSideA {
		t.Fatalf("non-holder edges cleared the holder's floor: floor=%v holder=%q", aFloor, holder)
	}
	assertFloorInvariant(t, c, "after non-holder final")

	// Let 2x the margin elapse: the old code would have fired a
	// side=b margin release by now. Nothing may release.
	time.Sleep(200 * time.Millisecond)
	logs := buf.String()
	if strings.Contains(logs, "kind=floor_released") {
		t.Fatalf("floor released while the holder still spoke:\n%s", logs)
	}
	if turnsB.count() != 0 && !loopHeldPrompt(loopB) {
		t.Fatal("B's parked prompt vanished without a release")
	}

	// A's audio drains -> A (the holder) releases -> B flushes.
	stateA("cc-a-drain", 1, TurnKindFinal)
	c.NoteAudioDrained("cc-a-drain")
	waitFor(t, 3*time.Second, "holder drain releases and flushes B", func() bool {
		return turnsB.count() == 1
	})
	logs = buf.String()
	if !strings.Contains(logs, "side=a") || !strings.Contains(logs, "kind=floor_released") {
		t.Fatalf("release telemetry must name the holder side:\n%s", logs)
	}
	// The FIRST floor_released line must name the holder (side=a). The
	// measured defect emitted `side=b kind=floor_released` while A still
	// held; a LATER side=b release is legitimate (B's own turn becomes
	// the holder's after its flushed dispatch), so only the first
	// release is pinned.
	first := ""
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, "kind=floor_released") {
			first = line
			break
		}
	}
	if !strings.Contains(first, "side=a") {
		t.Fatalf("first floor_released must be the holder side a (measured defect was side=b): %s", first)
	}
	assertFloorInvariant(t, c, "after holder release")
}

// T-HR2 (task test b): HOLDER CHAINING. A's LLM keeps dispatching new
// sub-turns while B's speech arrives: A's subturn N+1 dispatches while
// subturn N's tail drains. The floor must STAY with the holder (its
// live subturn re-claims it at turn_start) and the peer flush is
// DEFERRED to the holder's next drained release with no new dispatch
// (task rule 2). Behavioral pin: after the chained turn_start, a drain
// edge for the previous burst may not release; B stays parked; when A
// finally ends its chain (quiet final, no new dispatch) the next drain
// releases side=a and flushes B.
func TestLinkCoordinatorHolderChainKeepsFloorAndDefersPeer(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(time.Hour, slog.New(slog.NewTextHandler(&buf, nil)))
	loopB := bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)

	// A subturn 1 finalizes; the LLM chains subturn 2 BEFORE the tail
	// drains (the 07:32:54 -> 07:32:55 chain shape: seq 3 dispatch 1.2
	// s after the release that should have been A's quiet point).
	stateA("cc-a-drain", 1, TurnKindFinal)
	stateA("cc-a-drain", 2, TurnKindStart)
	c.mu.Lock()
	chainOK := c.a.busy && c.a.reDispatched && c.holder == LinkSideA && c.a.floorHeld
	c.mu.Unlock()
	if !chainOK {
		t.Fatal("chained holder must stay busy + reDispatched + floor + holder")
	}

	// Subturn 1's drain edge lands while subturn 2 is LIVE: the busy
	// gate rejects the match, so nothing arms a release; the floor
	// stays with A and B stays parked.
	c.NoteAudioDrained("cc-a-drain")
	time.Sleep(120 * time.Millisecond)
	if strings.Contains(buf.String(), "kind=floor_released") {
		t.Fatalf("floor released mid-chain (peer would flush between subturns):\n%s", buf.String())
	}
	if !loopHeldPrompt(loopB) || turnsB.count() != 0 {
		t.Fatal("B's park dispatched or vanished during the holder chain")
	}
	assertFloorInvariant(t, c, "mid-chain")

	// A's chain ends: subturn 2 finalizes with NO new dispatch, then
	// its drained edge fires -> the release lands, floor + holder
	// clear, B flushes.
	stateA("cc-a-drain", 2, TurnKindFinal)
	c.NoteAudioDrained("cc-a-drain")
	waitFor(t, 3*time.Second, "chain-close drain releases + flushes B", func() bool {
		return turnsB.count() == 1
	})
	// A's floor is gone; B's dispatched turn may already have RE-CLAIMED
	// the floor (holder=b is legal the moment B's claim gates through) —
	// assert only that A no longer holds and the invariant still binds.
	c.mu.Lock()
	aClear := !c.a.floorHeld && !c.a.busy && (c.holder == "" || c.holder == LinkSideB)
	c.mu.Unlock()
	if !aClear {
		t.Fatal("quiet-final drain release must clear A's floor + holder marker")
	}
	logs := buf.String()
	if !strings.Contains(logs, "side=a") || !strings.Contains(logs, "kind=floor_released") {
		t.Fatalf("release telemetry must name the holder side a:\n%s", logs)
	}
	for line := range strings.SplitSeq(logs, "\n") {
		if strings.Contains(line, "kind=floor_released") && strings.Contains(line, "side=b") {
			t.Fatalf("a side=b release fired during an a-held chain: %s", line)
		}
	}
	assertFloorInvariant(t, c, "after chain close")
}

// T-HR2b: the releaseFloor chain GUARD itself (defensive second gate):
// a pad-armed release whose holder reDispatched flag is set (the
// interleaving where the live subturn's drain raced the pad timer) must
// DEFER: kind=floor_release_deferred, floor stays with the holder, peer
// flush skipped, the consumed arm invalidated. The chain's live
// subturn then finalizes and its own margin watchdog (50 ms here)
// provides the bounded release - the defer branch never re-arms a
// timer itself (that would loop: the fire would hit the same
// reDispatched guard and defer again).
func TestLinkCoordinatorReleaseFloorChainGuardDefers(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	loopB := bindSideBLoop(t, c, &buf, turnsB)

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal)
	c.NoteAudioDrained("cc-a-drain") // pad-armed release for subturn 1

	// A chains subturn 2 (turn_start after the final -> reDispatched),
	// then the pad timer's arm fires while subturn 2 is LIVE: the busy
	// gate rejects the arm match... to pin the GUARD itself, fire the
	// funnel directly at the live arm instead (exactly what the pad
	// timer callback would do if busy had cleared in between).
	stateA("cc-a-drain", 2, TurnKindStart)
	c.mu.Lock()
	c.a.busy = false // subturn 2's text stream flushed early: busy=false, floor+chain still set
	gen := c.a.releaseGen
	c.mu.Unlock()
	c.releaseFloor(LinkSideA, "cc-a-drain", 1, gen, "drained")

	waitFor(t, 2*time.Second, "deferred telemetry", func() bool {
		return strings.Contains(buf.String(), "kind=floor_release_deferred")
	})
	c.mu.Lock()
	held := c.a.floorHeld && c.holder == LinkSideA
	c.mu.Unlock()
	if !held {
		t.Fatal("deferred release must keep the floor with the chained holder")
	}
	if turnsB.count() != 0 || !loopHeldPrompt(loopB) {
		t.Fatal("peer flush fired despite chained-holder defer")
	}

	// Subturn 2 finalizes with NO new dispatch: its own margin arms and
	// the bounded watchdog releases for real.
	stateA("cc-a-drain", 2, TurnKindFinal)
	waitFor(t, 3*time.Second, "chain-close margin releases", func() bool {
		return strings.Contains(buf.String(), "kind=floor_released")
	})
	waitFor(t, 3*time.Second, "deferred flush lands on release", func() bool {
		return turnsB.count() == 1
	})
	assertFloorInvariant(t, c, "after guard-deferred release")
}

// startSeamSplice stands up a REAL local splice for one coordinator
// side, exactly like LocalLeg.Open wires a link leg: registry target
// pre-registered, drain callback resolved through wiredAudioDrain,
// client pipe drained by a reader goroutine. The caller feeds child
// PCM with feedChild(ccid) and can arm the drain watch with
// LocalSpliceFor(ccid).MarkFinalPlayed().
func startSeamSplice(t *testing.T, c *LinkTurnCoordinator, side, ccid string) *runLocalSplice {
	t.Helper()
	target := "local:link-" + side + "-" + ccid
	reg := NewLinkTurnRegistry(linkDiscard())
	reg.Register(target, "link-seam", side, c)
	drainFn := wiredAudioDrain(reg, target)
	if drainFn == nil {
		t.Fatal("link target must resolve a drain callback")
	}
	cfg := LocalSpliceConfig{DrainQuiet: 150 * time.Millisecond, DrainQuietFinal: 60 * time.Millisecond}
	p, micSide, _, _, cancel, done := startDrainSplice(t, ccid, cfg, drainFn)
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("seam splice did not exit")
		}
		_ = p
	})
	// Client consumer: paced audio must leave the pipe or the pacer
	// stalls and pending never reaches zero.
	stopRead := make(chan struct{})
	go func() {
		b := make([]byte, 640)
		for {
			select {
			case <-stopRead:
				return
			default:
			}
			_ = micSide.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
			if _, err := micSide.Read(b); err != nil {
				var ne net.Error
				if !errors.As(err, &ne) || !ne.Timeout() {
					return
				}
			}
		}
	}()
	t.Cleanup(func() { close(stopRead) })
	return waitForSplice(t, ccid)
}

// T-HR3 (task test c): the margin fires ONLY for the HOLDER, and is
// audio-aware (holder-no-played watchdog). (1) A non-holder's final
// arms no margin timer at all (the 07:33:20 side=b margin shape).
// (2) A holder with no live splice (old-child/denied shape) rides the
// margin to a normal reason=margin release. (3) A holder whose splice
// tail is still AUDIBLE at the fire gets ONE re-arm (kind=
// margin_rearmed) instead of a talk-over cut, and the second
// consecutive fire releases anyway - the hold is bounded at 2 x
// margin even when the played edge is lost forever.
func TestLinkCoordinatorMarginIsHolderOnlyAndAudioAware(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(120*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	bindSideBLoop(t, c, &buf, turnsB)

	// (1) B's chained non-holder final: bookkeeping only, NO margin.
	c.TurnStateFor(LinkSideB)("cc-b-drain", 2, TurnKindFinal)
	c.mu.Lock()
	bMargin := c.b.marginTimer != nil
	c.mu.Unlock()
	if bMargin {
		t.Fatal("non-holder final armed a margin timer (the 07:33:20 bug)")
	}

	// (2) Holder A finalizes with NO live splice for cc-a-drain ->
	// TailActive false -> the margin releases normally (watchdog role).
	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal)
	time.Sleep(60 * time.Millisecond)
	if turnsB.count() != 0 {
		t.Fatal("B released before the fallback margin")
	}
	waitFor(t, 3*time.Second, "holder margin watchdog releases", func() bool {
		return turnsB.count() == 1
	})
	if !strings.Contains(buf.String(), "reason=margin") {
		t.Fatalf("margin telemetry missing:\n%s", buf.String())
	}

	// (3) Live-tail shape on a REAL splice: A holds and finalizes; the
	// child keeps pumping bytes across the margin window so the first
	// fire finds TailActive true -> rearm only, floor stays held. A
	// FRESH buffer: stage (2)'s released floor lets B's own turn run
	// and arm its own margin on the shared coordinator's loop set —
	// counting floor_released lines from the old buffer would race
	// those unrelated releases.
	var tailBuf syncBuffer
	c2 := newTestCoordinator(150*time.Millisecond, slog.New(slog.NewTextHandler(&tailBuf, nil)))
	const ccid = "ls-margin-tail"
	startSeamSplice(t, c2, LinkSideA, ccid)
	if !c2.HoldPromptFor(LinkSideA)(Instance{CallControlID: ccid}, func() {}) {
		t.Fatal("A claim must be allowed on the idle coordinator")
	}
	c2.TurnStateFor(LinkSideA)(ccid, 1, TurnKindStart)
	c2.TurnStateFor(LinkSideA)(ccid, 1, TurnKindFinal)
	deadline := time.Now().Add(220 * time.Millisecond)
	for time.Now().Before(deadline) {
		_ = feedChild(ccid)
		time.Sleep(20 * time.Millisecond)
	}
	// First fire (~150 ms post-final) landed inside the live window.
	waitFor(t, 2*time.Second, "margin re-armed for a live tail", func() bool {
		return strings.Contains(tailBuf.String(), "kind=margin_rearmed")
	})
	c2.mu.Lock()
	stillHeld := c2.a.floorHeld && c2.holder == LinkSideA
	c2.mu.Unlock()
	if !stillHeld {
		t.Fatal("re-armed margin must keep the holder's floor (no tail cut)")
	}
	if strings.Contains(tailBuf.String(), "kind=floor_released") {
		t.Fatalf("margin cut a live tail (floor_released despite TailActive):\n%s", tailBuf.String())
	}
	// Feeding stopped: the second fire releases regardless - bounded.
	waitFor(t, 4*time.Second, "second margin fire releases (bounded hold)", func() bool {
		return strings.Contains(tailBuf.String(), "kind=floor_released")
	})
	if !strings.Contains(tailBuf.String(), "reason=margin") {
		t.Fatalf("bounded release must carry reason=margin:\n%s", tailBuf.String())
	}
}

// T-HR4 (task test d): the full fake conversation through the real
// seams, reproducing the 07:32-07:34 window of link-b9d221d8d8cddcce:
// A (fake state machine on the claim path + a REAL splice) holds the
// floor and chains sub-turns while a long synth backlog keeps its audio
// tail alive; B is a REAL talk loop whose final prompt parks. The
// measured defect: A's subturn-5 margin fired while A's tail was still
// audible (played edge came 11 s later) and B dispatched over A's next
// sentences. Corrected shape asserted here:
//   - B parks while A holds;
//   - A's margin fires DURING the live tail -> RE-ARM only (no release),
//     B stays parked, zero both-speak;
//   - A's tail finishes + played edge -> drained release, holder clears;
//   - B's parked prompt then dispatches with A quiet; every B dispatch
//     observes an A-floor-free coordinator (the zero-both-speak gate).
func TestLinkCoordinatorConversationNoBothSpeakWindows(t *testing.T) {
	var buf syncBuffer
	c := newTestCoordinator(200*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	const ccidA, ccidB = "ls-conv-a", "ls-conv-b"
	startSeamSplice(t, c, LinkSideA, ccidA)
	startSeamSplice(t, c, LinkSideB, ccidB)

	// B: real loop, counting dispatches with the both-speak gate: no
	// dispatch may run while A's slot holds the floor.
	var bGateMu sync.Mutex
	bViolations := 0
	gatedB := &speakGateTurns{
		inner:      &holdTurns{reply: "B answer."},
		floorA:     func() bool { c.mu.Lock(); defer c.mu.Unlock(); return c.a.floorHeld || c.a.busy },
		violations: &bViolations,
		gateMu:     &bGateMu,
	}
	loopB := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      gatedB,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     slog.New(slog.NewTextHandler(&buf, nil)),
		HoldPrompt: c.HoldPromptFor(LinkSideB),
		TurnState:  c.TurnStateFor(LinkSideB),
	}, TalkLoopConfig{})
	c.Bind(LinkSideB, loopB)
	cancelB, doneB := runLoop(t, loopB, ccidB)
	defer func() { cancelB(); <-doneB }()

	// A claims the floor and starts subturn 1 (the 07:32:54 shape).
	if !c.HoldPromptFor(LinkSideA)(Instance{CallControlID: ccidA}, func() {}) {
		t.Fatal("A's first claim must be allowed on the idle link")
	}
	stateA := c.TurnStateFor(LinkSideA)
	stateA(ccidA, 1, TurnKindStart)

	// B's question arrives mid-A-turn: parks (prompt_held).
	loopB.frames <- finalPrompt(ccidB, "question while A talks")
	waitFor(t, 2*time.Second, "B parks under A", func() bool {
		return loopHeldPrompt(loopB) && gatedB.count() == 0
	})

	// A finalizes subturn 1 and CHAINS subturn 2 (the 07:33:06->07:33:08
	// chain shape). The chained turn_start CANCELS subturn 1's margin arm
	// (A is speaking again) - the only live margin from here belongs to
	// the chain's LAST final.
	stateA(ccidA, 1, TurnKindFinal)
	stateA(ccidA, 2, TurnKindStart)
	for range 6 {
		if err := feedChild(ccidA); err != nil {
			t.Fatalf("feedChild A: %v", err)
		}
	}

	// A's chain ENDS: subturn 2 finalizes with no further dispatch ->
	// the holder's margin re-arms off this final. One batched child
	// feed of 30 chunks = 600 ms of paced backlog (all pumped into the
	// splice instantly, paced out at 20 ms/chunk): the margin fires at
	// 200 ms with pending>0 -> TailActive -> RE-ARM only (no cut), B
	// stays parked; the second fire at ~400 ms also sees pending>0 ->
	// releases regardless (the bounded hold: the played edge was lost,
	// old-child shape). The release beats the drain watch on purpose:
	// the watch needs pending==0 first (the backlog is ~200 ms deep at
	// fire2), so fire2 is provably the margin's bounded release.
	// (Under -race load the paced backlog only shrinks at real time
	// (20 ms/chunk), so fire1 at ~200 ms sees ~20 chunks pending with
	// any scheduler drift — TailActive is deterministic while the
	// backlog plays out.)
	stateA(ccidA, 2, TurnKindFinal)
	for range 30 {
		if err := feedChild(ccidA); err != nil {
			t.Fatalf("feedChild backlog: %v", err)
		}
	}
	waitFor(t, 2*time.Second, "margin re-armed over A's live backlog", func() bool {
		for line := range strings.SplitSeq(buf.String(), "\n") {
			if strings.Contains(line, "kind=margin_rearmed") && strings.Contains(line, "side=a") {
				return true
			}
		}
		return false
	})
	if gatedB.count() != 0 || !loopHeldPrompt(loopB) {
		t.Fatal("B dispatched while A's tail was still audible (both-speak)")
	}
	c.mu.Lock()
	stillHeld := c.a.floorHeld && c.holder == LinkSideA
	c.mu.Unlock()
	if !stillHeld {
		t.Fatal("re-armed margin must keep A's floor")
	}

	// The SECOND consecutive fire releases regardless - the hold is
	// bounded at 2 x margin even with a lost played edge (the
	// old-child-binary shape the margin exists for). Poll the
	// TELEMETRY line (not the state flip): releaseFloor clears the
	// floor under the lock and logs AFTER unlocking, so a state poll
	// can win the log write under package load.
	//
	// The measured defect is the FIRST release naming side=b while A
	// still held; B's OWN later release (after it became the holder
	// through a gated claim) is legal. Pin: the first floor_released
	// line must name side=a.
	waitFor(t, 4*time.Second, "first floor_released names the holder side a", func() bool {
		for line := range strings.SplitSeq(buf.String(), "\n") {
			if strings.Contains(line, "kind=floor_released") {
				return strings.Contains(line, "side=a")
			}
		}
		return false
	})

	// B's parked prompt flushes and dispatches while A is quiet.
	waitFor(t, 3*time.Second, "B flushes and dispatches after A's release", func() bool {
		return gatedB.count() == 1
	})
	if bViolations != 0 {
		t.Fatalf("%d both-speak dispatch windows (B ran while A busy/held)", bViolations)
	}
	assertFloorInvariant(t, c, "end of conversation")
}

// speakGateTurns wraps a TurnRunner and counts every dispatch that starts
// while the gate predicate still reports the PEER speaking (busy or
// floor-held): the zero-both-speak-windows assertion for the
// conversation test.
type speakGateTurns struct {
	inner      TurnRunner
	floorA     func() bool
	gateMu     *sync.Mutex
	violations *int
}

func (g *speakGateTurns) Run(ctx context.Context, sess SessionHandle, prompt string) (string, error) {
	if g.floorA() {
		g.gateMu.Lock()
		*g.violations++
		g.gateMu.Unlock()
	}
	return g.inner.Run(ctx, sess, prompt)
}

func (g *speakGateTurns) count() int { return g.inner.(*holdTurns).count() }

// ---------------------------------------------------------------------------
// voice.backchannel_soften (#130) — A/B toggle pinning.
//
// Toggle OFF (the default everywhere above): the existing coordinator
// suite runs UNMODIFIED and green — byte-identical today. Toggle ON:
// soft pad/margin values, short-parked-backchannel flush deferral at
// non-drained releases, full-question releases unaffected, and the
// never-drop force-flush backstop.
// ---------------------------------------------------------------------------

// bindSideBLoopText is bindSideBLoop with a parameterised parked text
// (the soften classifier reads the word count of the ACTUAL parked
// prompt, so the short-vs-full split must be drivable).
func bindSideBLoopText(t *testing.T, c *LinkTurnCoordinator, buf *syncBuffer, turnsB *holdTurns, heldText string) *TalkLoop {
	t.Helper()
	logger := slog.New(slog.NewTextHandler(buf, nil))
	loopB := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turnsB,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     logger,
		HoldPrompt: c.HoldPromptFor(LinkSideB),
		TurnState:  c.TurnStateFor(LinkSideB),
	}, TalkLoopConfig{})
	c.Bind(LinkSideB, loopB)
	cancelB, doneB := runLoop(t, loopB, "cc-b-drain")
	t.Cleanup(func() { cancelB(); <-doneB })
	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindStart)
	loopB.frames <- finalPrompt("cc-b-drain", heldText)
	waitFor(t, 2*time.Second, "park "+heldText, func() bool { return loopHeldPrompt(loopB) })
	return loopB
}

// BS-1: toggle ON applies the soft pad + soft margin (defaults only —
// an explicitly tuned TurnMargin stays untouched), toggle OFF (and a
// never-softened coordinator) keeps today's exact constants. Also pins
// the manager seam: LinkDeps.BackchannelSoften lands on every link's
// coordinator at Open, before the legs exist.
func TestBackchannelSoftenPadAndMarginValues(t *testing.T) {
	// The soft constants themselves pin the plan's literal values.
	if softFloorTailPad != 700*time.Millisecond {
		t.Fatalf("softFloorTailPad = %v, want 700ms", softFloorTailPad)
	}
	if softFloorFallbackMargin != 15*time.Second {
		t.Fatalf("softFloorFallbackMargin = %v, want 15s", softFloorFallbackMargin)
	}
	// ON with production defaults: 250ms -> 700ms pad, 12s -> 15s margin.
	c := NewLinkTurnCoordinator("link-soften-vals", 0, linkDiscard())
	c.SetBackchannelSoften(true)
	c.mu.Lock()
	pad, margin := c.pad, c.margin
	c.mu.Unlock()
	if pad != softFloorTailPad {
		t.Fatalf("soft pad = %v, want softFloorTailPad (700ms)", pad)
	}
	if margin != softFloorFallbackMargin {
		t.Fatalf("soft margin = %v, want softFloorFallbackMargin (15s)", margin)
	}

	// ON with an explicitly tuned margin: margin preserved, pad soft.
	c2 := newTestCoordinator(80*time.Millisecond, linkDiscard())
	c2.SetBackchannelSoften(true)
	c2.mu.Lock()
	pad2, margin2 := c2.pad, c2.margin
	c2.mu.Unlock()
	if pad2 != softFloorTailPad {
		t.Fatalf("soft pad with tuned margin = %v, want 700ms", pad2)
	}
	if margin2 != 80*time.Millisecond {
		t.Fatalf("tuned margin overwritten by soften: %v", margin2)
	}

	// OFF: byte-identical constants.
	c3 := NewLinkTurnCoordinator("link-soften-off", 0, linkDiscard())
	c3.mu.Lock()
	if c3.pad != linkFloorTailPad || c3.margin != linkFloorFallbackDefault || c3.soften {
		c3.mu.Unlock()
		t.Fatal("toggle-off coordinator drifted from today's constants")
	}
	c3.mu.Unlock()

	// Manager seam: LinkDeps.BackchannelSoften arms every link's
	// coordinator at Open.
	leg := newLinkFakeLeg(t)
	reg := NewLinkTurnRegistry(linkDiscard())
	m, err := NewLinkManager(LinkDeps{
		Leg:               leg,
		Store:             &linkFakeStore{calls: map[string][]TranscriptLine{}},
		Turns:             reg,
		DialTimeout:       3 * time.Second,
		RetainEnded:       time.Hour,
		TurnMargin:        25 * time.Millisecond,
		BackchannelSoften: true,
		Logger:            linkDiscard(),
	})
	if err != nil {
		t.Fatalf("NewLinkManager: %v", err)
	}
	link, err := m.Open(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	coord, _ := reg.Lookup("local:link-a-" + link.ID[len("link-"):])
	if coord == nil {
		t.Fatal("coordinator unresolved")
	}
	coord.mu.Lock()
	on, padOpen := coord.soften, coord.pad
	coord.mu.Unlock()
	if !on || padOpen != softFloorTailPad {
		t.Fatalf("LinkDeps.BackchannelSoften did not reach the Open coordinator (soften=%v pad=%v)", on, padOpen)
	}
	if err := m.Close(link.ID, "test close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
}

// BS-2: the core rule. With soften ON, the holder's FALLBACK MARGIN
// release (not the played-drain edge) does NOT flush a SHORT parked
// backchannel: the park survives (never dropped), and flushes at the
// holder's NEXT drained release.
func TestBackchannelSoftenMarginReleaseDefersShortPark(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	c.SetBackchannelSoften(true)
	loopB := bindSideBLoopText(t, c, &buf, turnsB, "Sure.")

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal) // no live splice -> margin fires in 80ms

	waitFor(t, 3*time.Second, "margin release fired", func() bool {
		return strings.Contains(buf.String(), "kind=floor_released") &&
			strings.Contains(buf.String(), "reason=margin")
	})
	// Soften pin: the floor IS released, but the short park is NOT
	// flushed — kind=backchannel_softened and B still parked.
	logs := buf.String()
	if !strings.Contains(logs, "kind=backchannel_softened") {
		t.Fatalf("short park was not softened at the margin release:\\n%s", logs)
	}
	if turnsB.count() != 0 {
		t.Fatal("short backchannel flushed at a non-drained margin release")
	}
	if !loopHeldPrompt(loopB) {
		t.Fatal("softened park VANISHED (never-drop violation)")
	}
	c.mu.Lock()
	softened := c.softenTimer != nil && c.softenSide == LinkSideB
	c.mu.Unlock()
	if !softened {
		t.Fatal("force-flush backstop not armed on defer")
	}

	// A's next burst quiet-finals and DRAINS: the real release flushes B.
	// (The 80 ms test margin would beat the 700 ms soft pad to the
	// funnel — the margin's bounded-fire path is already pinned above —
	// so stand it down here and drive the drained edge deterministically.)
	stateA("cc-a-drain", 2, TurnKindStart)
	stateA("cc-a-drain", 2, TurnKindFinal)
	c.mu.Lock()
	if c.a.marginTimer != nil {
		c.a.marginTimer.Stop()
		c.a.marginTimer = nil
	}
	c.a.marginGen++ // invalidate any in-flight fire: the drain edge is the release path here
	c.mu.Unlock()
	c.NoteAudioDrained("cc-a-drain")
	waitFor(t, 3*time.Second, "drained release flushes the softened park", func() bool {
		return turnsB.count() == 1
	})
	if !strings.Contains(buf.String(), "reason=drained") {
		t.Fatalf("flush must land on the drained release:\\n%s", buf.String())
	}
	if strings.Contains(buf.String(), "kind=prompt_dropped") {
		t.Fatalf("backchannel dropped: %s", buf.String())
	}
	c.mu.Lock()
	timerDown := c.softenTimer == nil && c.softenSide == ""
	c.mu.Unlock()
	if !timerDown {
		t.Fatal("drained release must stand the force-flush down")
	}
}

// BS-3: a FULL (> backchannelMaxWords words) parked question flushes
// on the margin release exactly as today, even with soften ON.
func TestBackchannelSoftenFullQuestionUnaffected(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	c.SetBackchannelSoften(true)
	bindSideBLoopText(t, c, &buf, turnsB, "Can you explain the third one?")

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal)
	waitFor(t, 3*time.Second, "full question flushes at margin release", func() bool {
		return turnsB.count() == 1
	})
	if strings.Contains(buf.String(), "kind=backchannel_softened") {
		t.Fatalf("full question was softened (must flush on any release):\\n%s", buf.String())
	}
}

// BS-4 (toggle-off parity pin): the SAME margin-release shape with the
// toggle OFF flushes the short park immediately — today's byte-
// identical behavior.
func TestBackchannelSoftenOffShortParkFlushesToday(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	// No SetBackchannelSoften call at all: the zero state.
	bindSideBLoopText(t, c, &buf, turnsB, "Sure.")

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal)
	waitFor(t, 3*time.Second, "margin release flushes short park (today)", func() bool {
		return turnsB.count() == 1
	})
	if strings.Contains(buf.String(), "kind=backchannel_softened") {
		t.Fatalf("soften fired with the toggle OFF:\\n%s", buf.String())
	}
}

// BS-5 (never-drop backstop): soften ON, short park deferred, and the
// holder NEVER produces another drained release — the bounded
// softenMaxDefer force-flush fires and the backchannel dispatches
// anyway. (Production bound is 10 s; the seam var lets the pin drive
// it on test time.)
func TestBackchannelSoftenForceFlushNeverDies(t *testing.T) {
	oldDefer := softenMaxDefer
	softenMaxDefer = 150 * time.Millisecond
	defer func() { softenMaxDefer = oldDefer }()

	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	c.SetBackchannelSoften(true)
	bindSideBLoopText(t, c, &buf, turnsB, "mm-hm")

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal) // margin -> softened defer, no further A edges
	waitFor(t, 3*time.Second, "force-flush backstop fires", func() bool {
		return turnsB.count() == 1
	})
	logs := buf.String()
	if !strings.Contains(logs, "kind=backchannel_forced_flush") {
		t.Fatalf("forced flush telemetry missing:\\n%s", logs)
	}
	if strings.Contains(logs, "kind=prompt_dropped") {
		t.Fatalf("backchannel dropped instead of force-flushed:\\n%s", logs)
	}
}

// BS-6: the exact live overlap shape (link-9ee64c5fe7d651e9): A long
// reply -> chains a subturn -> the DEFERRED release's watchdog fires
// mid-reply (holder hung, no live edges) and releases the floor. The
// table pins the three-way behavior split: short park + soften ON
// waits for A's drained edge; short park + OFF flushes at the
// watchdog (today); full question + ON flushes at the watchdog.
func TestBackchannelSoftenLiveChainWatchdogTable(t *testing.T) {
	cases := []struct {
		name     string
		heldText string
		soften   bool
	}{
		{"short-on-defers", "Sure.", true},
		{"short-off-flushes", "Sure.", false},
		{"full-on-flushes", "Can you say that again about the budget?", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var buf syncBuffer
			turnsB := &holdTurns{reply: "ok."}
			c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
			c.SetBackchannelSoften(tc.soften)
			bindSideBLoopText(t, c, &buf, turnsB, tc.heldText)

			stateA := c.TurnStateFor(LinkSideA)
			// A's burst 1 finalizes, drains... then chains burst 2
			// (T-HR2b shape): the pad release DEFERS (holder chained)
			// and arms the one-shot floor_watchdog at one margin.
			stateA("cc-a-drain", 1, TurnKindFinal)
			c.NoteAudioDrained("cc-a-drain")
			stateA("cc-a-drain", 2, TurnKindStart)
			c.mu.Lock()
			c.a.busy = false // burst 2's text stream flushed: floor+chain stand, busy clears
			gen := c.a.releaseGen
			c.mu.Unlock()
			c.releaseFloor(LinkSideA, "cc-a-drain", 1, gen, "drained") // hits the chain guard -> defer
			waitFor(t, 2*time.Second, "chain defer telemetry", func() bool {
				return strings.Contains(buf.String(), "kind=floor_release_deferred")
			})

			// The WATCHDOG fires while A's reply-chain is still mid-
			// reply (the hung-LLM backstop) and releases the floor.
			waitFor(t, 3*time.Second, "watchdog release", func() bool {
				return strings.Contains(buf.String(), "reason=floor_watchdog") &&
					strings.Contains(buf.String(), "kind=floor_released")
			})

			defers := tc.soften && len(strings.Fields(tc.heldText)) <= 2
			if defers {
				if turnsB.count() != 0 || !loopHeldPrompt(firstBoundLoop(c, LinkSideB)) {
					t.Fatal("short park flushed at the watchdog release under soften (overlap regression)")
				}
				if !strings.Contains(buf.String(), "kind=backchannel_softened") {
					t.Fatalf("soften defer telemetry missing:\\n%s", buf.String())
				}
			} else {
				waitFor(t, 3*time.Second, "flush at watchdog release (today's path)", func() bool {
					return turnsB.count() == 1
				})
				if strings.Contains(buf.String(), "kind=backchannel_softened") {
					t.Fatalf("unexpected soften defer:\\n%s", buf.String())
				}
			}

			if !defers {
				return
			}
			// A's chain continues and finally DRAINS quiet: B flushes
			// there, still never dropped. (Stand the 80 ms test
			// margin down so the drained edge — the flush path under
			// soften — wins the race against the soft 700 ms pad,
			// exactly as it does in production where the margin is
			// 15 s.)
			stateA("cc-a-drain", 3, TurnKindStart)
			stateA("cc-a-drain", 3, TurnKindFinal)
			c.mu.Lock()
			if c.a.marginTimer != nil {
				c.a.marginTimer.Stop()
				c.a.marginTimer = nil
			}
			c.a.marginGen++
			c.mu.Unlock()
			c.NoteAudioDrained("cc-a-drain")
			waitFor(t, 3*time.Second, "drained edge flushes the shortened park", func() bool {
				return turnsB.count() == 1
			})
			if strings.Contains(buf.String(), "kind=prompt_dropped") {
				t.Fatalf("backchannel dropped across the whole chain:\\n%s", buf.String())
			}
		})
	}
}

// firstBoundLoop returns the side's bound loop (test accessor mirroring
// the coordinator's slot).
func firstBoundLoop(c *LinkTurnCoordinator, side string) *TalkLoop {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sideLocked(side).loop
}

// ---------------------------------------------------------------------------
// Per-employee backchannel soften (#143) — divergent voice.employee_leg
// rows across a link's two sides.
//
// PEER-SIDE SEMANTICS (as implemented in link.go): the soften consult at
// every release point keys on the PARKED (peer) side's own row value —
// side X's short backchannel defers (and its flush's pad/margin soften)
// iff X's employee row softens, observed by the coordinator when the
// OPPOSITE employee (the holder) releases. Uniform rows / nil resolver /
// no rows stay on the exact global SetBackchannelSoften path.
// ---------------------------------------------------------------------------

// bindSideLoopText is bindSideBLoopText parameterised by the PARKED side:
// the holder is the other side. The holder takes the floor with a fake
// turn_start edge; a short final prompt then parks on the bound loop.
func bindSideLoopText(t *testing.T, c *LinkTurnCoordinator, buf *syncBuffer, turns *holdTurns, parkedSide, parkCCID, heldText string) *TalkLoop {
	t.Helper()
	holderSide := LinkSideA
	if parkedSide == LinkSideA {
		holderSide = LinkSideB
	}
	logger := slog.New(slog.NewTextHandler(buf, nil))
	loop := NewTalkLoop(TalkDeps{
		Supervisor: &cannedSupervisor{},
		Turns:      turns,
		SessionFor: constSession(SessionHandle{ID: "s", ConversationID: "c"}),
		Send:       (&recordSink{}).Send,
		OnEnd:      func(ccid, reason string) {},
		Logger:     logger,
		HoldPrompt: c.HoldPromptFor(parkedSide),
		TurnState:  c.TurnStateFor(parkedSide),
	}, TalkLoopConfig{})
	c.Bind(parkedSide, loop)
	cancelLoop, done := runLoop(t, loop, parkCCID)
	t.Cleanup(func() { cancelLoop(); <-done })
	holderState := c.TurnStateFor(holderSide)
	holderState(holderStateCCID(holderSide), 1, TurnKindStart)
	loop.frames <- finalPrompt(parkCCID, heldText)
	waitFor(t, 2*time.Second, "park "+heldText, func() bool { return loopHeldPrompt(loop) })
	return loop
}

func holderStateCCID(side string) string {
	if side == LinkSideA {
		return "cc-a-drain"
	}
	return "cc-b-drain"
}

// #143-1 (uniform parity): SetBackchannelSoftenSides with EQUAL flags is
// exactly the global SetBackchannelSoften path — (true,true) lands the
// soft pad/margin + global soften latch with NO per-side mode;
// (false,false) keeps the untouched zero state.
func TestBackchannelSoftenSidesUniformEqualsGlobal(t *testing.T) {
	gOn := NewLinkTurnCoordinator("link-143-uniform-on", 0, linkDiscard())
	gOn.SetBackchannelSoften(true)
	sOn := NewLinkTurnCoordinator("link-143-sides-on", 0, linkDiscard())
	sOn.SetBackchannelSoftenSides(true, true)

	gOff := NewLinkTurnCoordinator("link-143-uniform-off", 0, linkDiscard())
	sOff := NewLinkTurnCoordinator("link-143-sides-off", 0, linkDiscard())
	sOff.SetBackchannelSoftenSides(false, false)

	type snap struct {
		soften, perSide, a, b bool
		pad, margin           time.Duration
	}
	take := func(c *LinkTurnCoordinator) snap {
		c.mu.Lock()
		defer c.mu.Unlock()
		return snap{c.soften, c.softenPerSide, c.softenA, c.softenB, c.pad, c.margin}
	}
	if gs, ss := take(gOn), take(sOn); gs != ss {
		t.Fatalf("uniform (true,true) drifted from the global ON path: global=%+v sides=%+v", gs, ss)
	}
	if ss := take(sOn); !ss.soften || ss.perSide || ss.pad != softFloorTailPad || ss.margin != softFloorFallbackMargin {
		t.Fatalf("uniform (true,true) must take the global ON shape, got %+v", ss)
	}
	if gs, ss := take(gOff), take(sOff); gs != ss {
		t.Fatalf("uniform (false,false) drifted from the OFF zero state: global=%+v sides=%+v", gs, ss)
	}
	if ss := take(sOff); ss.soften || ss.perSide || ss.pad != linkFloorTailPad || ss.margin != linkFloorFallbackDefault {
		t.Fatalf("uniform (false,false) must stay byte-identical OFF, got %+v", ss)
	}
}

// #143-2 (divergent flags + timing consults): A-row=true / B-row=false
// arms per-side mode with the coordinator-level pad/margin UNTOUCHED,
// and the arm-time consults split by holder: a B-held release (parked
// peer = A, A's row softens) takes the soft pad/margin; an A-held
// release (parked peer = B) stays hard. An explicitly tuned margin
// still wins (mirrors the TurnMargin rule of the global path).
func TestBackchannelSoftenSidesDivergentTimingConsults(t *testing.T) {
	c := NewLinkTurnCoordinator("link-143-divergent", 0, linkDiscard())
	c.SetBackchannelSoftenSides(true, false)
	c.mu.Lock()
	defer c.mu.Unlock()
	if !c.softenPerSide || c.soften || c.softenA != true || c.softenB != false {
		t.Fatalf("divergent flags wrong: perSide=%v global=%v a=%v b=%v", c.softenPerSide, c.soften, c.softenA, c.softenB)
	}
	if c.pad != linkFloorTailPad || c.margin != linkFloorFallbackDefault {
		t.Fatalf("divergent mode must NOT swap the coordinator-level pad/margin: pad=%v margin=%v", c.pad, c.margin)
	}
	// Holder B releases A's parked backchannel: peer (A) row softens ->
	// soft pad + soft margin (both still carry the production defaults).
	if p := c.padForLocked(LinkSideB); p != softFloorTailPad {
		t.Fatalf("padForLocked(B) = %v, want softFloorTailPad (parked A's row softens)", p)
	}
	if m := c.marginForLocked(LinkSideB); m != softFloorFallbackMargin {
		t.Fatalf("marginForLocked(B) = %v, want softFloorFallbackMargin", m)
	}
	// Holder A releases B's parked backchannel: B's row is false -> hard.
	if p := c.padForLocked(LinkSideA); p != linkFloorTailPad {
		t.Fatalf("padForLocked(A) = %v, want hard linkFloorTailPad (parked B's row is off)", p)
	}
	if m := c.marginForLocked(LinkSideA); m != linkFloorFallbackDefault {
		t.Fatalf("marginForLocked(A) = %v, want hard default", m)
	}
	// Explicitly tuned margin is respected untouched (same rule the
	// global path pins in BS-1).
	c2 := newTestCoordinator(80*time.Millisecond, linkDiscard())
	c2.SetBackchannelSoftenSides(true, false)
	c2.mu.Lock()
	defer c2.mu.Unlock()
	if m := c2.marginForLocked(LinkSideB); m != 80*time.Millisecond {
		t.Fatalf("tuned margin overridden by per-side soften: %v", m)
	}
}

// #143-3 (behavior, the named A-row=true / B-row=false case): side A's
// short backchannel IS softened at side B's margin release — A's park
// survives the non-drained release (kind=backchannel_softened, force-
// flush armed for side A) and flushes at B's next drained release.
func TestBackchannelSoftenSidesParkedRowDefersAtPeerRelease(t *testing.T) {
	var buf syncBuffer
	turnsA := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	c.SetBackchannelSoftenSides(true, false) // A row ON, B row OFF
	loopA := bindSideLoopText(t, c, &buf, turnsA, LinkSideA, "cc-a-park", "Sure.")

	stateB := c.TurnStateFor(LinkSideB)
	stateB("cc-b-drain", 1, TurnKindFinal) // holder B final -> margin fires in 80ms (no live splice)

	waitFor(t, 3*time.Second, "margin release fired", func() bool {
		return strings.Contains(buf.String(), "kind=floor_released") &&
			strings.Contains(buf.String(), "reason=margin")
	})
	logs := buf.String()
	if !strings.Contains(logs, "kind=backchannel_softened") {
		t.Fatalf("A's short park was not softened at B's margin release (A-row=true):\n%s", logs)
	}
	if turnsA.count() != 0 {
		t.Fatal("A's backchannel flushed at a non-drained release despite A's row softening")
	}
	if !loopHeldPrompt(loopA) {
		t.Fatal("softened park VANISHED (never-drop violation)")
	}
	c.mu.Lock()
	armedForA := c.softenTimer != nil && c.softenSide == LinkSideA
	c.mu.Unlock()
	if !armedForA {
		t.Fatal("force-flush backstop not armed for side A")
	}

	// B's next burst quiet-finals and DRAINS: the real release flushes
	// A's park (margin stood down so the drained edge wins, BS-2 shape).
	stateB("cc-b-drain", 2, TurnKindStart)
	stateB("cc-b-drain", 2, TurnKindFinal)
	c.mu.Lock()
	if c.b.marginTimer != nil {
		c.b.marginTimer.Stop()
		c.b.marginTimer = nil
	}
	c.b.marginGen++
	c.mu.Unlock()
	c.NoteAudioDrained("cc-b-drain")
	waitFor(t, 3*time.Second, "B's drained release flushes A's softened park", func() bool {
		return turnsA.count() == 1
	})
	if strings.Contains(buf.String(), "kind=prompt_dropped") {
		t.Fatalf("backchannel dropped: %s", buf.String())
	}
}

// #143-4 (behavior, the complement): the SAME divergent coordinator
// (A-row=true / B-row=false) must flush side B's short park on ANY
// release — B's row is the gate for B's own backchannel, so B rides
// today's hard timing at A's margin release.
func TestBackchannelSoftenSidesNonSofteningPeerFlushesToday(t *testing.T) {
	var buf syncBuffer
	turnsB := &holdTurns{reply: "ok."}
	c := newTestCoordinator(80*time.Millisecond, slog.New(slog.NewTextHandler(&buf, nil)))
	c.SetBackchannelSoftenSides(true, false)
	bindSideLoopText(t, c, &buf, turnsB, LinkSideB, "cc-b-park", "Sure.")

	stateA := c.TurnStateFor(LinkSideA)
	stateA("cc-a-drain", 1, TurnKindFinal) // holder A final -> margin release
	waitFor(t, 3*time.Second, "margin release flushes B's short park (B row off)", func() bool {
		return turnsB.count() == 1
	})
	if strings.Contains(buf.String(), "kind=backchannel_softened") {
		t.Fatalf("soften fired for a side whose row is OFF:\n%s", buf.String())
	}
}

// #143-5 (manager seam): LinkDeps.BackchannelSoftenFor resolves per side
// from the side's employee id at Open (divergent rows land divergent
// flags on the coordinator); a nil resolver keeps the global
// LinkDeps.BackchannelSoften bool (pre-#143 shape, byte-identical).
func TestLinkManagerOpenResolvesBackchannelSoftenPerSide(t *testing.T) {
	leg := newLinkFakeLeg(t)
	var seen []string
	reg := NewLinkTurnRegistry(linkDiscard())
	m, err := NewLinkManager(LinkDeps{
		Leg:         leg,
		Store:       &linkFakeStore{calls: map[string][]TranscriptLine{}},
		Turns:       reg,
		DialTimeout: 3 * time.Second,
		RetainEnded: time.Hour,
		TurnMargin:  25 * time.Millisecond,
		// A-row (alice) softens, B-row (bob) does not; the global
		// bool is deliberately opposite to prove the resolver wins.
		BackchannelSoften: true,
		BackchannelSoftenFor: func(empID string) bool {
			seen = append(seen, empID)
			return empID == "alice"
		},
		Logger: linkDiscard(),
	})
	if err != nil {
		t.Fatalf("NewLinkManager: %v", err)
	}
	link, err := m.Open(context.Background(), "alice", "bob")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if len(seen) != 2 || seen[0] != "alice" || seen[1] != "bob" {
		t.Fatalf("resolver did not get the per-side employee ids: %v", seen)
	}
	coord, _ := reg.Lookup("local:link-a-" + link.ID[len("link-"):])
	if coord == nil {
		t.Fatal("coordinator unresolved")
	}
	coord.mu.Lock()
	perSide, softA, softB, globalOn := coord.softenPerSide, coord.softenA, coord.softenB, coord.soften
	coord.mu.Unlock()
	if !perSide || !softA || softB || globalOn {
		t.Fatalf("divergent rows did not land per-side (perSide=%v a=%v b=%v global=%v)", perSide, softA, softB, globalOn)
	}
	if err := m.Close(link.ID, "test close"); err != nil {
		t.Fatalf("Close: %v", err)
	}

	// Uniform resolutions take the global path shape: a resolver that
	// answers false for both sides (no rows) lands the exact OFF state.
	reg2 := NewLinkTurnRegistry(linkDiscard())
	m2, err := NewLinkManager(LinkDeps{
		Leg:                  leg,
		Store:                &linkFakeStore{calls: map[string][]TranscriptLine{}},
		Turns:                reg2,
		DialTimeout:          3 * time.Second,
		RetainEnded:          time.Hour,
		TurnMargin:           25 * time.Millisecond,
		BackchannelSoftenFor: func(string) bool { return false },
		Logger:               linkDiscard(),
	})
	if err != nil {
		t.Fatalf("NewLinkManager (no-rows): %v", err)
	}
	link2, err := m2.Open(context.Background(), "carol", "dave")
	if err != nil {
		t.Fatalf("Open (no-rows): %v", err)
	}
	coord2, _ := reg2.Lookup("local:link-a-" + link2.ID[len("link-"):])
	coord2.mu.Lock()
	off := !coord2.soften && !coord2.softenPerSide && coord2.pad == linkFloorTailPad && coord2.margin == 25*time.Millisecond
	coord2.mu.Unlock()
	if !off {
		t.Fatal("no-row resolver drifted from the global OFF path")
	}
	if err := m2.Close(link2.ID, "test close"); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The nil-resolver global-true path stays byte-identical: covered
	// by the manager-seam pin inside TestBackchannelSoftenPadAndMargin
	// Values (BS-1) — left unmodified on purpose.
}
