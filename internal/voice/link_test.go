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
	outLn, err := net.Listen("unix", outPath)
	if err != nil {
		_ = inLn.Close()
		return LocalCall{}, fmt.Errorf("fake listen out: %w", err)
	}
	f.listens = append(f.listens, inLn, outLn)
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
	for i := 0; i < 2; i++ {
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
	for i := 0; i < n; i++ {
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
	stateA("cc-a-drain", 2, TurnKindStart)
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
// leg opens). Pins: the floor stays held while audio paces, the drain
// edge fires from the real outbound quiescence, and the release lands
// with reason=drained.
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

	p, micSide, _, _, cancel, done := startDrainSplice(t, ccid, LocalSpliceConfig{DrainQuiet: 150 * time.Millisecond}, drainFn)
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
	splice := LocalSpliceFor(ccid)
	waitFor(t, 6*time.Second, "turn-2 floor released via real drain with stuck carry", func() bool {
		return strings.Count(buf.String(), "kind=floor_released") >= 2
	})
	splice.mu.Lock()
	carryStuck, disarmed := splice.audioCarry.Load(), !splice.drainArmed
	splice.mu.Unlock()
	if carryStuck == 0 {
		t.Fatal("test shape broken: partial tail must leave non-zero carry")
	}
	if !disarmed {
		t.Fatal("watch must self-disarm after firing (next turn_final re-arms fresh)")
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
