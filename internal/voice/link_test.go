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
}

func newLinkFakeLeg(t *testing.T) *linkFakeLeg {
	t.Helper()
	dir := filepath.Join(shortDir(t), "linkpairs")
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
		f.acceptIn[ccid] = make(chan net.Conn, 1)
		go func() {
			c, aerr := inLn.Accept()
			if aerr == nil {
				f.acceptIn[ccid] <- c
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
	f.acceptIn[ccid] = make(chan net.Conn, 1)
	f.acceptOu[ccid] = make(chan net.Conn, 1)
	go func() {
		c, aerr := inLn.Accept()
		if aerr == nil {
			f.acceptIn[ccid] <- c
		}
	}()
	go func() {
		c, aerr := outLn.Accept()
		if aerr == nil {
			f.acceptOu[ccid] <- c
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
