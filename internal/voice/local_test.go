package voice

// Leaf 07 tests (local-leg plan docs/plans/20261106-voice-local-leg/
// 07-local-leg.md): the LocalLeg lifecycle unit. Fakes only - no real
// child, no telnyx, no sqlite. Order pins mirror dial.go DialFlow steps
// 3-8 MINUS steps 1-2 (no effects claim, no audit row) and MINUS
// effects/audit everywhere (CL5 deliberate difference, pinned
// structurally by the nil-carrier construction in every fixture).

import (
	"bufio"
	"context"
	"errors"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// localLegDiscard keeps leaf logs out of test output.
func localLegDiscard() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

// legFakeRegistry wraps the real CallRegistry and records every Acquire
// and Release so tests can pin order and leg (T1/T3).
type legFakeRegistry struct {
	inner    *CallRegistry
	mu       sync.Mutex
	acquired []string
	released []string
}

func newLegFakeRegistry(cap int) *legFakeRegistry {
	return &legFakeRegistry{inner: NewRegistry(cap)}
}

func (r *legFakeRegistry) Acquire(ccid, employeeID, target string) (Instance, error) {
	return r.AcquireLeg(ccid, employeeID, target, LegPhone)
}

func (r *legFakeRegistry) AcquireLeg(ccid, employeeID, target, leg string) (Instance, error) {
	r.mu.Lock()
	r.acquired = append(r.acquired, ccid+"|"+target+"|"+leg)
	r.mu.Unlock()
	return r.inner.AcquireLeg(ccid, employeeID, target, leg)
}

func (r *legFakeRegistry) Release(ccid string) {
	r.mu.Lock()
	r.released = append(r.released, ccid)
	r.mu.Unlock()
	r.inner.Release(ccid)
}

func (r *legFakeRegistry) Live() []Instance { return r.inner.Live() }
func (r *legFakeRegistry) Cap() int         { return r.inner.Cap() }

func (r *legFakeRegistry) acquiredSnap() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.acquired...)
}

func (r *legFakeRegistry) releasedSnap() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.released...)
}

// legFakeChild is the ChildProc recording seam.
type legFakeChild struct {
	mu     sync.Mutex
	starts []string
	ends   []string
}

func (f *legFakeChild) NoteCallStart(ccid string) {
	f.mu.Lock()
	f.starts = append(f.starts, ccid)
	f.mu.Unlock()
}

func (f *legFakeChild) NoteCallEnd(ccid string) {
	f.mu.Lock()
	f.ends = append(f.ends, ccid)
	f.mu.Unlock()
}

func (f *legFakeChild) endsSnap() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.ends...)
}

// holdRecorder records goal-hold acquisitions through the LocalDeps.Hold
// seam (gap closure: Open must acquire the hold so the release fan-out's
// ReleaseHook carries the call row to 'ended').
type holdRecorder struct {
	mu  sync.Mutex
	got []string
}

func (r *holdRecorder) add(ccid, employee string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.got = append(r.got, ccid+"|"+employee)
}

func (r *holdRecorder) acquired() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.got...)
}

// legFixture wires a LocalLeg over a fake registry, a real ChildProc seam
// recorder, a real Manager (NIL carrier: the CL5 compile-pin - local.go
// must never touch a carrier), a real TextServer with a fake child
// attached, a real tmpdir audio dir, and a recording brief builder.
type legFixture struct {
	t           *testing.T
	leg         *LocalLeg
	reg         *legFakeRegistry
	child       *legFakeChild
	childReader *bufio.Reader
	audioDir    string
	briefMu     sync.Mutex
	briefs      []string
	hold        *holdRecorder
}

func newLegFixture(t *testing.T) *legFixture {
	t.Helper()
	t.Setenv("MEEPT_HOME", shortDir(t))

	reg := newLegFakeRegistry(2)
	child := &legFakeChild{}

	audioDir := filepath.Join(shortDir(t), "audio")
	if err := os.MkdirAll(audioDir, 0o755); err != nil {
		t.Fatalf("audio dir: %v", err)
	}
	sockPath := filepath.Join(shortDir(t), "leg-text.sock")
	srv := NewTextServer(sockPath, localLegDiscard())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	waitForSocket := func() {
		t.Helper()
		deadline := time.Now().Add(2 * time.Second)
		for time.Now().Before(deadline) {
			if _, err := os.Stat(sockPath); err == nil {
				return
			}
			time.Sleep(2 * time.Millisecond)
		}
		t.Fatalf("leg text socket never appeared")
	}
	waitForSocket()
	t.Cleanup(func() {
		cancel()
		srv.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	// Fake child claims the text socket so Send has a transport.
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("fake child dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	// The manager: nil carrier (CL5 seam) with a release hook mirroring
	// the daemon's releaseCall sinks (childproc note + registry release)
	// - in production the wiring installs that hook; the leg's End only
	// fires FailOutcome and rides it.
	manager := NewManager(ManagerConfig{}, nil, nil)

	f := &legFixture{
		t:           t,
		reg:         reg,
		child:       child,
		childReader: bufio.NewReader(conn),
		audioDir:    audioDir,
		hold:        &holdRecorder{},
	}
	manager.SetReleaseHook(func(ccid string, _ Outcome) {
		child.NoteCallEnd(ccid)
		reg.Release(ccid)
	})
	briefBuilder := func(_ context.Context, employeeID, number string) (DayBrief, error) {
		f.briefMu.Lock()
		f.briefs = append(f.briefs, employeeID+"|"+number)
		f.briefMu.Unlock()
		return DayBrief{DurableMemory: "prefers plain answers"}, nil
	}
	leg, err := NewLocalLeg(
		LocalLegConfig{Enabled: true, Label: "legbox", BargeInRms: 3000},
		LocalDeps{
			Registry:        reg,
			ChildProc:       child,
			TextServer:      srv,
			Manager:         manager, // nil carrier: CL5 nil-carrier seam
			Binding:         nil,     // binding rides the daemon glue; nil tolerated
			DayBriefBuilder: briefBuilder,
			Profile:         nil,
			// The goal-hold seam: records acquisitions (the daemon
			// wires AcquireCallHold here).
			Hold:   f.hold.add,
			Logger: localLegDiscard(),
		})
	if err != nil {
		t.Fatalf("NewLocalLeg: %v", err)
	}
	f.leg = leg
	return f
}

// briefCalls snapshots the DayBriefBuilder invocations.
func (f *legFixture) briefCalls() []string {
	f.briefMu.Lock()
	defer f.briefMu.Unlock()
	return append([]string(nil), f.briefs...)
}

// readLine reads one C1 line off the fake child with a bounded deadline.
func (f *legFixture) readLine() (string, error) {
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := f.childReader.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case r := <-ch:
		return r.line, r.err
	case <-time.After(5 * time.Second):
		return "", errors.New("timeout reading C1 line from fake child")
	}
}

// readOpenFrame returns the first open-event line the fake child sees.
func (f *legFixture) readOpenFrame() string {
	f.t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		line, err := f.readLine()
		if err != nil {
			f.t.Fatalf("read open frame: %v", err)
		}
		if strings.Contains(line, `"open"`) {
			return line
		}
	}
	f.t.Fatal("no open frame within 5s")
	return ""
}

// dialPair dials the pair under SocketDir like the live child does
// (prevents the splice join timeout) and returns the child-side .out
// conn. The leg creates the pair in SocketDir (voice.SocketDir), not in
// the fixture's scratch audio dir.
func (f *legFixture) dialPair(t *testing.T, ccid string) net.Conn {
	t.Helper()
	dir := SocketDir()
	var lastErr error
	// Hold an already-dialed conn across retries: the pair's accept
	// goroutine claims the FIRST conn per socket (socket.go NewAudioPair);
	// closing it to retry both poisons the pair - the retry conn is never
	// accepted. Retry only the missing side (the .out socket file lands
	// one statement after .in's bind, so an early .out dial races it).
	var c1, c2 net.Conn
	deadline := time.Now().Add(2 * time.Second)
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
				buf := make([]byte, 4096)
				for {
					if _, err := c1.Read(buf); err != nil {
						break
					}
				}
			}()
			t.Cleanup(func() { _ = c1.Close(); _ = c2.Close() })
			return c2
		}
		lastErr = e1
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("could not dial audio pair for %s: %v (exists in=%v out=%v)", ccid, lastErr,
		func() bool { _, err := os.Stat(filepath.Join(dir, ccid+".in")); return err == nil }(),
		func() bool { _, err := os.Stat(filepath.Join(dir, ccid+".out")); return err == nil }())
	return nil
}

// T1: Open happy path - order Acquire -> TrackOutbound -> pair -> open
// frame; LocalCall fields correct; splice running (pair dialable).
func TestLocalLegOpen(t *testing.T) {
	f := newLegFixture(t)

	call, err := f.leg.Open(context.Background(), "emp-open", "legbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	// ccid shape: local- + 16 hex chars (pkg/id.Generate's payload).
	if !strings.HasPrefix(call.CallID, "local-") {
		t.Errorf("CallID = %q, want prefix local-", call.CallID)
	}
	if len(call.CallID) != len("local-")+32/2 {
		t.Errorf("CallID = %q, want local- + a pkg/id-sized suffix", call.CallID)
	}

	// Pair paths under SocketDir, both files exist, sun_path-safe. The
	// open result exposes the CLIENT pair (<ccid>.c.in/.c.out); the
	// child pair (<ccid>.in/.out) stays daemon-side.
	wantIn := filepath.Join(SocketDir(), call.CallID+".c.in")
	wantOut := filepath.Join(SocketDir(), call.CallID+".c.out")
	if call.InPath != wantIn || call.OutPath != wantOut {
		t.Errorf("paths = (%s, %s), want (%s, %s)", call.InPath, call.OutPath, wantIn, wantOut)
	}
	for _, p := range []string{call.InPath, call.OutPath} {
		if len(p) >= 103 {
			t.Errorf("path %s exceeds the 103-byte sun_path limit", p)
		}
		if _, err := os.Stat(p); err != nil {
			t.Errorf("pair file %s missing: %v", p, err)
		}
	}

	// Registry acquire: leg=local, target verbatim local:<label>.
	acq := f.reg.acquiredSnap()
	if len(acq) != 1 {
		t.Fatalf("acquires = %v, want exactly one", acq)
	}
	if acq[0] != call.CallID+"|local:legbox|"+LegLocal {
		t.Errorf("acquire = %q, want %q (ccid|target|leg)", acq[0], call.CallID+"|local:legbox|"+LegLocal)
	}

	// Manager tracks the ccid (FailOutcome/WaitCall/release fan-out ride it).
	if !f.leg.deps.Manager.IsOutbound(call.CallID) {
		t.Errorf("ccid %s not tracked outbound (TrackOutbound must run)", call.CallID)
	}

	// Open frame on C1: direction local, to local:<label>, local:true,
	// barge_in_rms, L16@16000, profile fields from CL4 resolution.
	line := f.readOpenFrame()
	want := `{"call_id":"` + call.CallID + `","event":"open","to":"local:legbox","direction":"local","codec":"L16","sample_rate":16000,"local":true,"barge_in_rms":3000}` + "\n"
	if line != want {
		t.Errorf("open line:\n got %s\nwant %s", line, want)
	}

	// Splice started: the join grace is armed; a client dial keeps the
	// leg alive past a short probe (no ErrNoClient teardown within it).
	client := f.dialPair(t, call.CallID)
	time.Sleep(150 * time.Millisecond)
	select {
	case <-f.leg.deps.Manager.WaitCall(call.CallID):
		t.Errorf("call %s ended while a client was joined (splice must keep running)", call.CallID)
	default:
	}
	_ = client

	if err := f.leg.End(call.CallID, "test done"); err != nil {
		t.Errorf("End: %v", err)
	}
}

// T1b (gap closure): Open acquires the goal hold once, keyed by the
// call's ccid and employee. The release fan-out's ReleaseHook gates its
// OnCallEnd on this heldCalls entry, so without the acquire the calls.db
// row never reaches 'ended'.
func TestLocalLegOpenAcquiresHold(t *testing.T) {
	f := newLegFixture(t)
	call, err := f.leg.Open(context.Background(), "emp-hold", "holdbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}

	got := f.hold.acquired()
	if len(got) != 1 {
		t.Fatalf("hold acquired %d times; want exactly one: %v", len(got), got)
	}
	if want := call.CallID + "|emp-hold"; got[0] != want {
		t.Fatalf("hold = %q, want %q (ccid|employee)", got[0], want)
	}

	if err := f.leg.End(call.CallID, "test done"); err != nil {
		t.Errorf("End: %v", err)
	}
}

// Refused opens must not acquire a hold (no ccid to hold): the seam is
// only reached for a successfully-registered live call.
func TestLocalLegNoHoldOnRefusedOpen(t *testing.T) {
	f := newLegFixture(t)
	if _, err := f.leg.Open(context.Background(), "", "holdbox"); err == nil {
		t.Fatal("Open(empty employee) must refuse")
	}
	if got := f.hold.acquired(); len(got) != 0 {
		t.Fatalf("refused open acquired holds: %v", got)
	}
}

// T2: gates - disabled config refuses; empty employee refuses with a
// named error; duplicate-target and busy surface registry errors
// unchanged; a nil DayBriefBuilder still opens (brief empty, no panic).
func TestLocalLegGates(t *testing.T) {
	f := newLegFixture(t)
	ctx := context.Background()

	// Empty employee: named error, zero side effects.
	if _, err := f.leg.Open(ctx, "", "legbox"); err == nil || !strings.Contains(err.Error(), "employee") {
		t.Fatalf("Open(empty employee) = %v, want a named employee error", err)
	}
	if n := len(f.reg.acquiredSnap()); n != 0 {
		t.Errorf("refused open left %d acquires", n)
	}

	// Live call: a second Open for ANOTHER employee at the same target
	// hits the registry's duplicate-target rule (shared across legs).
	if _, err := f.leg.Open(ctx, "emp-one", "legbox"); err != nil {
		t.Fatalf("seed Open: %v", err)
	}
	if _, err := f.leg.Open(ctx, "emp-two", "legbox"); !errors.Is(err, ErrDuplicateTarget) {
		t.Errorf("second Open at same local target = %v, want ErrDuplicateTarget (registry rule shared across legs)", err)
	}
	// Same employee again: ErrBusy (one live call per employee).
	if _, err := f.leg.Open(ctx, "emp-one", "otherbox"); !errors.Is(err, ErrBusy) {
		t.Errorf("second Open for live employee = %v, want ErrBusy", err)
	}

	// Nil DayBriefBuilder: Open still succeeds, brief stays empty.
	f2 := newLegFixture(t)
	f2.leg.deps.DayBriefBuilder = nil
	call, err := f2.leg.Open(ctx, "emp-nilbrief", "legbox")
	if err != nil {
		t.Fatalf("Open with nil brief builder: %v", err)
	}
	if len(f2.briefCalls()) != 0 {
		t.Errorf("nil builder invoked %d times, want 0", len(f2.briefCalls()))
	}
	if err := f2.leg.End(call.CallID, "done"); err != nil {
		t.Errorf("End: %v", err)
	}

	// Disabled config refuses before anything else.
	f3 := newLegFixture(t)
	f3.leg.cfg.Enabled = false
	if _, err := f3.leg.Open(ctx, "emp-x", "legbox"); err == nil {
		t.Fatal("Open on a disabled leg must refuse")
	}
	if n := len(f3.reg.acquiredSnap()); n != 0 {
		t.Errorf("disabled Open left %d acquires", n)
	}
}

// T3: End fans out once - FailOutcome lands (release hook via Manager),
// the registry releases, childproc NoteCallEnd runs; double End is
// idempotent (second is a nil no-op).
func TestLocalLegEnd(t *testing.T) {
	f := newLegFixture(t)
	ctx := context.Background()

	call, err := f.leg.Open(ctx, "emp-end", "legbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	f.dialPair(t, call.CallID) // join the splice so End cancels live loops

	// Observe the manager's release through WaitCall.
	done := f.leg.deps.Manager.WaitCall(call.CallID)

	if err := f.leg.End(call.CallID, "unit test end"); err != nil {
		t.Fatalf("End: %v", err)
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("WaitCall did not close within 2s of End (FailOutcome must land)")
	}

	// Registry released exactly once.
	rel := f.reg.releasedSnap()
	if len(rel) != 1 || rel[0] != call.CallID {
		t.Errorf("releases = %v, want exactly [%s]", rel, call.CallID)
	}

	// ChildProc NoteCallEnd ran (childproc idle accounting via the leg's
	// teardown, mirroring releaseCall's sink order).
	deadline := time.Now().Add(2 * time.Second)
	for len(f.child.endsSnap()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if ends := f.child.endsSnap(); len(ends) != 1 || ends[0] != call.CallID {
		t.Errorf("childproc ends = %v, want exactly [%s]", ends, call.CallID)
	}

	// Double End: nil-idempotent.
	if err := f.leg.End(call.CallID, "second end"); err != nil {
		t.Errorf("double End = %v, want nil (idempotent)", err)
	}
	rel = f.reg.releasedSnap()
	if len(rel) != 1 {
		t.Errorf("double End produced %d releases, want 1", len(rel))
	}

	// Unknown ccid End: nil no-op, no panic.
	if err := f.leg.End("local-does-not-exist", "ghost"); err != nil {
		t.Errorf("End(unknown ccid) = %v, want nil", err)
	}
}

// T5 (voice-side pin): DayBriefBuilder runs at Open with (employeeID,
// "local:<label>") - the CL5 mirror of the phone site's BuildDayBrief
// call. The direction the binding row carries is asserted daemon-side
// (leaf 07 T5's daemon test); here we pin the (employee, number) shape
// and the once-per-call invocation.
func TestLocalLegBrief(t *testing.T) {
	f := newLegFixture(t)
	call, err := f.leg.Open(context.Background(), "emp-brief", "legbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for len(f.briefCalls()) == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	calls := f.briefCalls()
	if len(calls) != 1 {
		t.Fatalf("brief builder calls = %v, want exactly one", calls)
	}
	if calls[0] != "emp-brief|local:legbox" {
		t.Errorf("brief call = %q, want %q (direction local, number local:<label>)", calls[0], "emp-brief|local:legbox")
	}
	if err := f.leg.End(call.CallID, "done"); err != nil {
		t.Errorf("End: %v", err)
	}
}

// T2/CL5 structural pin: NewLocalLeg REFUSES a nil Manager or nil
// Registry or nil TextServer (typed-nil guard on every deps field, repo
// rule) - and accepts a nil carrier-bearing Manager, because LocalLeg
// must have NO carrier path at all. The compile-level proof that
// local.go cannot reach telnyx is the file itself: local.go imports no
// telnyx package (enforced by build; the package-level import_test.go
// pin scans imports).
func TestLocalLegDepsGuards(t *testing.T) {
	base := LocalLegConfig{Enabled: true}
	ok := LocalDeps{
		Registry:   NewRegistry(1),
		TextServer: NewTextServer(filepath.Join(shortDir(t), "g.sock"), localLegDiscard()),
		Manager:    NewManager(ManagerConfig{}, nil, nil),
		Logger:     localLegDiscard(),
	}

	if _, err := NewLocalLeg(base, LocalDeps{}); err == nil {
		t.Error("NewLocalLeg with nil Registry must refuse")
	}
	if _, err := NewLocalLeg(base, LocalDeps{Registry: ok.Registry}); err == nil {
		t.Error("NewLocalLeg with nil TextServer must refuse")
	}
	d := ok
	d.Registry = nil
	if _, err := NewLocalLeg(base, d); err == nil {
		t.Error("NewLocalLeg with nil Registry must refuse (mutated variant)")
	}
	d = ok
	d.TextServer = nil
	if _, err := NewLocalLeg(base, d); err == nil {
		t.Error("NewLocalLeg with nil TextServer must refuse (mutated variant)")
	}
	d = ok
	d.Manager = nil
	if _, err := NewLocalLeg(base, d); err == nil {
		t.Error("NewLocalLeg with nil Manager must refuse (manager carries the release fan-out)")
	}
	// Valid construction over the nil-carrier manager: accepted.
	if _, err := NewLocalLeg(base, ok); err != nil {
		t.Errorf("NewLocalLeg over nil-carrier Manager = %v, want accepted", err)
	}
}

// TestLocalLegVoiceOverride pins the CL4 top layer: Open's variadic
// voiceOverride (the voice_call tool's `voice` arg, leaf 09 LocalFunc)
// beats the profile resolver's tts_voice in the open frame. No override
// leaves the resolver's value untouched.
func TestLocalLegVoiceOverride(t *testing.T) {
	f := newLegFixture(t)

	// A dedicated leg with a profile resolver: the fixture leg stays
	// profile-free (its byte-exact open-frame pin depends on that).
	sockPath := filepath.Join(shortDir(t), "leg-ovr.sock")
	srv := NewTextServer(sockPath, localLegDiscard())
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- srv.Start(ctx) }()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(sockPath); err == nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Cleanup(func() {
		cancel()
		srv.Close()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
		}
	})
	conn, err := net.Dial("unix", sockPath)
	if err != nil {
		t.Fatalf("fake child dial: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	childReader := bufio.NewReader(conn)

	manager := NewManager(ManagerConfig{}, nil, nil)
	manager.SetReleaseHook(func(ccid string, _ Outcome) {
		f.child.NoteCallEnd(ccid)
		f.reg.Release(ccid)
	})
	leg, err := NewLocalLeg(
		LocalLegConfig{Enabled: true, Label: "legbox", BargeInRms: 3000},
		LocalDeps{
			Registry:        f.reg,
			ChildProc:       f.child,
			TextServer:      srv,
			Manager:         manager,
			DayBriefBuilder: func(context.Context, string, string) (DayBrief, error) { return DayBrief{}, nil },
			Profile:         func(string) Profile { return Profile{TtsVendor: "qwen3", TtsVoice: "resolved-default"} },
			Logger:          localLegDiscard(),
		})
	if err != nil {
		t.Fatalf("NewLocalLeg: %v", err)
	}

	first, err := leg.Open(context.Background(), "emp-ovr", "legbox")
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	frame := readOvrFrame(t, childReader)
	if !strings.Contains(frame, `"tts_voice":"resolved-default"`) {
		t.Errorf("open frame without override = %s, want resolver tts_voice", frame)
	}

	// One live call per target (CL6): end the first call before opening
	// the override variant.
	if err := leg.End(first.CallID, "test-done"); err != nil {
		t.Fatalf("End: %v", err)
	}
	if _, err := leg.Open(context.Background(), "emp-ovr", "legbox", "nova-2"); err != nil {
		t.Fatalf("Open with override: %v", err)
	}
	frame = readOvrFrame(t, childReader)
	if !strings.Contains(frame, `"tts_voice":"nova-2"`) {
		t.Errorf("open frame with override = %s, want tts_voice nova-2", frame)
	}
}

// readOvrFrame reads one open-event line off the override test's child.
func readOvrFrame(t *testing.T, r *bufio.Reader) string {
	t.Helper()
	type result struct {
		line string
		err  error
	}
	ch := make(chan result, 1)
	go func() {
		line, err := r.ReadString('\n')
		ch <- result{line, err}
	}()
	select {
	case res := <-ch:
		if res.err != nil {
			t.Fatalf("read open frame: %v", res.err)
		}
		return res.line
	case <-time.After(5 * time.Second):
		t.Fatalf("timeout reading open frame")
	}
	return ""
}
