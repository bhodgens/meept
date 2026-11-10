package signalleg

// Leaf 11a tests T4 (audio adapter) + the state machine / Accept-End
// pins that need a live leg fixture (T2/T3/T5 registry-side assertions
// live in internal/voice/signal_leg_test.go, the committed gate).
//
// Fakes only - no real signal-cli, no real child, no sqlite. The audio
// pins follow the localsplice test shape (net.Pipe client side, real
// AudioPair child side, timing asserted with an injectable pace clock).

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/signal"
	"github.com/caimlas/meept/internal/voice"
)

// discardLog keeps leaf logs out of test output.
func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

// shortAudioDir dodges the macOS sun_path limit for AudioPair sockets.
// The base ("/tmp/mpt-sl") keeps <base>/<ccid>.in|.out far below the
// 103-byte kernel limit even for the longest test names.
func shortAudioDir(t *testing.T) string {
	t.Helper()
	const base = "/tmp/mpt-sl"
	if err := os.MkdirAll(base, 0o700); err != nil {
		t.Fatalf("short base: %v", err)
	}
	dir, err := os.MkdirTemp(base, "s")
	if err != nil {
		t.Fatalf("MkdirTemp: %v", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	return dir
}

// fakePaceClock records paced-write intervals instead of sleeping (the
// leaf's "timing asserted with fake clock" rule).
type fakePaceClock struct {
	mu      sync.Mutex
	waits   []time.Duration
	ctxErr  error
	fireNow chan struct{}
	perWait func()
}

func newFakePaceClock() *fakePaceClock {
	return &fakePaceClock{fireNow: make(chan struct{}, 4096)}
}

func (c *fakePaceClock) wait(ctx context.Context, d time.Duration) error {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	per := c.perWait
	c.mu.Unlock()
	if per != nil {
		per()
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.fireNow:
		return nil
	}
}

func (c *fakePaceClock) intervals() []time.Duration {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]time.Duration(nil), c.waits...)
}

// audioFixture wires a real AudioPair with a net.Pipe socket on the
// adapter side and a draining child dialing the pair.
type audioFixture struct {
	t         *testing.T
	pair      *voice.AudioPair
	socket    net.Conn
	remote    net.Conn // test-side end of the pipe (reads outbound writes)
	childConn net.Conn // fakes the child dialing the C2 pair (.in)
	childOut  net.Conn // fakes the child's .out write end
}

func newAudioFixture(t *testing.T, ccid string) *audioFixture {
	t.Helper()
	t.Setenv("MEEPT_HOME", shortAudioDir(t))
	pair, err := voice.NewAudioPair(voice.SocketDir(), ccid)
	if err != nil {
		t.Fatalf("NewAudioPair: %v", err)
	}
	t.Cleanup(func() { _ = pair.Close() })
	socket, remote := net.Pipe()
	// A "child" dials the pair so WriteToChild/ReadFromChild have conns.
	cin, err := net.Dial("unix", filepath.Join(voice.SocketDir(), ccid+".in"))
	if err != nil {
		t.Fatalf("child dial .in: %v", err)
	}
	cout, err := net.Dial("unix", filepath.Join(voice.SocketDir(), ccid+".out"))
	if err != nil {
		t.Fatalf("child dial .out: %v", err)
	}
	t.Cleanup(func() {
		_ = cin.Close()  //nolint:errcheck // test teardown
		_ = cout.Close() //nolint:errcheck // test teardown
	})
	return &audioFixture{t: t, pair: pair, socket: socket, remote: remote, childConn: cin, childOut: cout}
}

// TestResamplerRateAndDrift pins the resample math: 48k -> 16k maps
// every 3 input samples to exactly 1 output sample across MANY chunks
// (no drift: total output sample count tracks input/3 over the stream).
func TestResamplerRateAndDrift(t *testing.T) {
	r, err := NewResampler(48000, 16000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	total := 0
	const chunks = 50
	for i := 0; i < chunks; i++ {
		// One 20 ms wire chunk at 48k = 960 samples = 1920 bytes.
		src := make([]byte, 1920)
		for s := range 960 {
			binary.LittleEndian.PutUint16(src[2*s:], uint16(s)) //nolint:gosec // G115: test sample
		}
		out := r.Write(src)
		total += len(out) / 2
	}
	// 50 chunks x 960 samples / 3 = 16000 output samples expected
	// (allow a +-2 boundary-rounding slack across chunk edges).
	if total < 15998 || total > 16002 {
		t.Errorf("48k->16k over %d chunks: got %d output samples, want ~16000 (drift check)", chunks, total)
	}
}

// TestResamplerPassthrough pins the 16k->16k identity (the RingRTC I/O
// leg that already negotiates 16k): bytes byte-identical.
func TestResamplerPassthrough(t *testing.T) {
	r, err := NewResampler(16000, 16000)
	if err != nil {
		t.Fatalf("NewResampler: %v", err)
	}
	src := make([]byte, 640)
	for s := range 320 {
		binary.LittleEndian.PutUint16(src[2*s:], uint16(s*7)) //nolint:gosec // G115: test sample
	}
	out := r.Write(src)
	if len(out) != len(src) {
		t.Fatalf("passthrough length = %d, want %d", len(out), len(src))
	}
	for i := range src {
		if out[i] != src[i] {
			t.Fatalf("passthrough byte %d = %d, want %d", i, out[i], src[i])
		}
	}
}

// TestResamplerInvalidRate pins the named refusal: zero/negative rate
// is ErrAudioSourceRate (never a guessed ratio).
func TestResamplerInvalidRate(t *testing.T) {
	if _, err := NewResampler(0, 16000); !errors.Is(err, ErrAudioSourceRate) {
		t.Errorf("src 0: err = %v, want ErrAudioSourceRate", err)
	}
	if _, err := NewResampler(48000, -1); !errors.Is(err, ErrAudioSourceRate) {
		t.Errorf("dst -1: err = %v, want ErrAudioSourceRate", err)
	}
}

// TestOutboundAudioPacedL16 pins T4's outbound Given/When/Then: child
// PCM (L16@16k, 640 B chunks) reaches the socket as 48k wire chunks and
// every write was preceded by a 20 ms pace tick (fake clock), and the
// wire chunk is 640*3 bytes (20 ms at 48k) while the CHILD only ever
// saw 640 B writes (H2: the wire codec never sizes child chunks).
func TestOutboundAudioPacedL16(t *testing.T) {
	f := newAudioFixture(t, "signal-audio-out")
	clock := newFakePaceClock()
	// Auto-advance the fake clock: each wait immediately fires (the
	// recorded INTERVAL is the assertion; no real sleeping).
	clock.mu.Lock()
	clock.perWait = func() { clock.fireNow <- struct{}{} }
	clock.mu.Unlock()

	done := make(chan error, 1)
	go func() {
		done <- runOutboundAudio(context.Background(), f.pair, f.socket, 48000, "signal-audio-out", discardLog(), clock.wait)
	}()

	// Feed 3 child chunks (3 x 640 B = 1920 B = 20 ms @48k) through the
	// CHILD's .out write end (ReadFromChild's source - WriteToChild is
	// the daemon->child .in direction, the inbound path's sink).
	childChunk := make([]byte, 640)
	for s := range 320 {
		binary.LittleEndian.PutUint16(childChunk[2*s:], uint16(1000+s)) //nolint:gosec // G115: test sample
	}
	go func() {
		for range 3 {
			if _, err := f.childOut.Write(childChunk); err != nil {
				t.Errorf("child .out write: %v", err)
				return
			}
		}
	}()

	// The remote (socket) end must eventually see 1920 bytes (one wire
	// chunk at 48k), written after a 20 ms pace tick.
	buf := make([]byte, 1920)
	_ = f.remote.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(f.remote, buf); err != nil {
		t.Fatalf("read paced wire chunk: %v", err)
	}

	// Pace ticks: the fake clock saw at least one 20 ms interval before
	// the first write (the wait happens BEFORE the write).
	waits := clock.intervals()
	if len(waits) == 0 {
		t.Fatalf("no pace ticks recorded (pacedWrite bypassed the wait seam)")
	}
	for _, d := range waits {
		if d != audioPace {
			t.Errorf("pace interval = %s, want %s", d, audioPace)
		}
	}

	// Tear down: close the socket so the pump's next write errors out.
	_ = f.socket.Close()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, ErrAudioBackpressure) && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrClosedPipe) {
			t.Errorf("outbound pump ended with %v, want a named backpressure/closed-pipe error", err)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("outbound pump did not end after socket close")
	}
}

// TestOutboundAudioBackpressureEndsLeg pins the CL7 rule: a socket
// write error surfaces as ErrAudioBackpressure (the leg's terminal
// named error), never swallowed. The child .out write end is CLOSED so
// the pump's poll loop sees EOF immediately and the write path runs.
func TestOutboundAudioBackpressureEndsLeg(t *testing.T) {
	f := newAudioFixture(t, "signal-audio-bp")
	clock := newFakePaceClock()
	// Auto-advance the fake clock (the pace wait must not block).
	clock.mu.Lock()
	clock.perWait = func() { clock.fireNow <- struct{}{} }
	clock.mu.Unlock()
	// Break the socket BEFORE the pump writes: the first paced write
	// must fail with the named error.
	_ = f.socket.Close()
	// Feed one chunk through the child .out end, then close it so the
	// pump's read poll sees EOF instead of polling forever.
	go func() {
		_, _ = f.childOut.Write(make([]byte, 640)) //nolint:errcheck // broken-pipe feed
		_ = f.childOut.Close()
	}()

	err := runOutboundAudio(context.Background(), f.pair, f.socket, 48000, "signal-audio-bp", discardLog(), clock.wait)
	if !errors.Is(err, ErrAudioBackpressure) {
		t.Fatalf("runOutboundAudio err = %v, want ErrAudioBackpressure", err)
	}
}

// TestInboundAudioChunked640 pins T4's inbound side: socket PCM at 48k
// reaches the child as exact 640 B L16@16k chunks (resampled 3:1), and
// a partial tail is held (never flushed).
func TestInboundAudioChunked640(t *testing.T) {
	f := newAudioFixture(t, "signal-audio-in")

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		done <- runInboundAudio(ctx, f.pair, f.socket, 48000, "signal-audio-in", discardLog())
	}()

	// Send 5 wire chunk-halves (each 640 B at 48k) with gaps so the
	// synchronous pipe delivers: 5 x 640 B = 3200 B @48k = 1600 samples
	// -> 533 samples @16k -> 1066 B -> ONE full 640 B child chunk (the
	// 426 B tail held, never flushed).
	wireChunk := make([]byte, 640)
	go func() {
		for range 5 {
			time.Sleep(20 * time.Millisecond)
			// Write to REMOTE (the producer end); the adapter reads
			// the socket (consumer) end. Writing to the read end of a
			// net.Pipe wedges forever.
			if _, err := f.remote.Write(wireChunk); err != nil {
				t.Errorf("socket write: %v", err)
				return
			}
		}
	}()

	got := 0
	buf := make([]byte, 640)
	_ = f.childConn.SetReadDeadline(time.Now().Add(5 * time.Second))
	for got < 1 {
		n, err := f.childConn.Read(buf)
		if err != nil {
			t.Fatalf("read child chunk %d (got %d bytes): %v", got, n, err)
		}
		got += n
	}
	// Chunk SIZE pin: the child NEVER sees non-640 B chunks; the first
	// delivered unit is exactly one full chunk.
	if got < 640 {
		t.Errorf("child got %d bytes, want a full 640 B chunk first (partial tails must be held)", got)
	}
	// Resample ratio pin: 5 wire chunks (3200 B @48k) yield ~1066 B
	// @16k -> exactly ONE child chunk before the held tail (a wrong
	// ratio would flush two chunks or none).
	time.Sleep(100 * time.Millisecond)
	extra := make([]byte, 640)
	_ = f.childConn.SetReadDeadline(time.Now().Add(200 * time.Millisecond))
	n, _ := f.childConn.Read(extra)
	if n != 0 {
		t.Errorf("child got %d extra bytes, want 0 (the 426 B tail must be HELD, not flushed)", n)
	}

	// Shutdown: ctx cancel (the End path), not socket close - the pump
	// HOLDS on socket EOF (a far-end pause may precede RECONNECTING).
	cancel()
	select {
	case err := <-done:
		if err != nil && !errors.Is(err, context.Canceled) {
			t.Errorf("inbound pump ended with %v, want context.Canceled", err)
		}
	case <-time.After(5 * time.Second):
		t.Errorf("inbound pump did not end on ctx cancel")
	}
}

// TestOutcomeByReason pins the reason->outcome mapping (busy/timeout
// DISTINCT from hard failure per leaf 05/11 FailOutcome rule).
func TestOutcomeByReason(t *testing.T) {
	cases := []struct {
		reason string
		want   voice.Outcome
	}{
		{"busy", OutcomeBusy},
		{"BUSY", OutcomeBusy},
		{"timeout", voice.OutcomeNoAnswer},
		{"hangup", voice.OutcomeHangup},
		{"declined", voice.OutcomeHangup},
		{"", voice.OutcomeFailed},
		{"weird-carrier-state", voice.OutcomeFailed},
	}
	for _, c := range cases {
		if got := OutcomeByReason(c.reason); got != c.want {
			t.Errorf("OutcomeByReason(%q) = %q, want %q", c.reason, got, c.want)
		}
	}
}

// TestIsSignalCcid pins the prefix shape test (End's foreign-id
// distinguisher, mirroring isLocalCcid).
func TestIsSignalCcid(t *testing.T) {
	if !isSignalCcid("signal-abc123") {
		t.Errorf("isSignalCcid(signal-abc123) = false, want true")
	}
	if isSignalCcid("signal-") {
		t.Errorf("bare prefix counts as foreign (len guard), want false")
	}
	if isSignalCcid("local-abc123") {
		t.Errorf("local ccid must be foreign to the signal leg")
	}
	if isSignalCcid("phone-abc123") {
		t.Errorf("phone ccid must be foreign to the signal leg")
	}
}

// --- leg-level state machine tests (live Leg with fake seams) ---

// fakeEffects records the EffectsSeam calls (phone-outbound posture
// order pins).
type fakeEffects struct {
	mu      sync.Mutex
	reserve []string
	abandon []string
	grant   bool
}

func (f *fakeEffects) Reserve(_ context.Context, key, _ string) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reserve = append(f.reserve, key)
	return f.grant, nil
}

func (f *fakeEffects) Complete(_ context.Context, _ string, _ string) error { return nil }

func (f *fakeEffects) Abandon(_ context.Context, key, _, _ string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.abandon = append(f.abandon, key)
	return nil
}

// fakeAudit records audit rows.
type fakeAudit struct {
	mu   sync.Mutex
	rows []map[string]any
}

func (f *fakeAudit) WriteAudit(_ context.Context, row map[string]any) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.rows = append(f.rows, row)
	return nil
}

// fakeRegistry wraps the real CallRegistry (the shared-rules pin).
type fakeRegistry struct {
	inner *voice.CallRegistry
}

func (r *fakeRegistry) Acquire(ccid, employeeID, target string) (voice.Instance, error) {
	return r.inner.Acquire(ccid, employeeID, target)
}

func (r *fakeRegistry) AcquireLeg(ccid, employeeID, target, leg string) (voice.Instance, error) {
	return r.inner.AcquireLeg(ccid, employeeID, target, leg)
}

func (r *fakeRegistry) Release(ccid string)    { r.inner.Release(ccid) }
func (r *fakeRegistry) Live() []voice.Instance { return r.inner.Live() }
func (r *fakeRegistry) Cap() int               { return r.inner.Cap() }

// newTestLeg assembles a live Leg over fakes. The signal client is a
// REAL client dialed against an inline json-rpc stub (StartCall must
// actually round-trip for the gate-order pins to mean anything).
func newTestLeg(t *testing.T, allow func(string) bool) (*Leg, *fakeEffects, *fakeAudit, func(uuid string)) {
	t.Helper()
	t.Setenv("MEEPT_HOME", shortAudioDir(t))
	// Pre-create the voice sock dir under the (already short) MEEPT_HOME
	// so AudioPair paths stay far below the sun_path limit.
	if err := os.MkdirAll(voice.SocketDir(), 0o755); err != nil {
		t.Fatalf("sock dir: %v", err)
	}

	// Inline signal-cli stub: answers every request with {callId:
	// "call-stub-<n>"} for startCall, null otherwise; tolerates the
	// version handshake probe.
	rpcLn, err := net.Listen("unix", filepath.Join(shortAudioDir(t), "sig.sock"))
	if err != nil {
		t.Fatalf("stub listen: %v", err)
	}
	t.Cleanup(func() { _ = rpcLn.Close() })
	var startMu sync.Mutex
	startCalls := make([]string, 0, 4)
	go func() {
		for {
			conn, aerr := rpcLn.Accept()
			if aerr != nil {
				return
			}
			go func(c net.Conn) {
				defer func() { _ = c.Close() }() //nolint:errcheck // test stub
				sc := make([]byte, 0, 4096)
				buf := make([]byte, 4096)
				seq := 0
				for {
					n, rerr := c.Read(buf)
					if rerr != nil {
						return
					}
					sc = append(sc, buf[:n]...)
					for {
						idx := indexByte(sc, '\n')
						if idx < 0 {
							break
						}
						line := sc[:idx]
						sc = sc[idx+1:]
						var req struct {
							ID     int    `json:"id"`
							Method string `json:"method"`
						}
						if jsonErr := jsonUnmarshalInto(line, &req); jsonErr != nil {
							continue
						}
						result := "null"
						if req.Method == "startCall" {
							startMu.Lock()
							seq++
							startCalls = append(startCalls, "call-stub")
							startMu.Unlock()
							result = `{"callId":"call-stub-` + testItoa(seq) + `"}`
						}
						resp := `{"jsonrpc":"2.0","id":` + testItoa(req.ID) + `,"result":` + result + `}` + "\n"
						_, _ = c.Write([]byte(resp)) //nolint:errcheck // test stub
					}
				}
			}(conn)
		}
	}()
	client, err := signal.Dial(rpcLn.Addr().String())
	if err != nil {
		t.Fatalf("signal.Dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() }) //nolint:errcheck // test teardown

	fx := &fakeEffects{grant: true}
	audit := &fakeAudit{}
	reg := &fakeRegistry{inner: voice.NewRegistry(4)}
	mgr := voice.NewManager(voice.ManagerConfig{}, nil, nil)
	// The wiring installs the release hook (releaseCall: registry
	// release + binding end); the test mirror releases the registry
	// through it, so the state machine's FailOutcome fan-out is honest.
	mgr.SetReleaseHook(func(ccid string, _ voice.Outcome) {
		reg.Release(ccid)
	})
	leg, lerr := New(Deps{
		Client:     client,
		Registry:   reg,
		Manager:    mgr,
		TextServer: voice.NewTextServer(filepath.Join(shortAudioDir(t), "text.sock"), discardLog()),
		Allowlist:  allow,
		Effects:    fx,
		Audit:      audit,
		// Short ringing watchdog (C-V3 gate knob, leg_gate_test.go T7):
		// well under the gate's 2s poll, and far above the milliseconds
		// the event-driven tests take between Place and their terminal
		// assertion, so no test's call times out mid-scenario.
		RingingTimeout: time.Second,
		Logger:         discardLog(),
	})
	if lerr != nil {
		t.Fatalf("New: %v", lerr)
	}
	return leg, fx, audit, func(uuid string) {
		startMu.Lock()
		defer startMu.Unlock()
		_ = uuid
	}
}

// indexByte is strings.IndexByte (kept local: one helper, no import).
func indexByte(b []byte, c byte) int {
	for i, x := range b {
		if x == c {
			return i
		}
	}
	return -1
}

// testItoa is strconv.Itoa for small positive ints.
func testItoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [8]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10) //nolint:gosec // G115: digit
		n /= 10
	}
	return string(b[i:])
}

// jsonUnmarshalInto is encoding/json.Unmarshal.
func jsonUnmarshalInto(data []byte, v any) error {
	return json.Unmarshal(data, v)
}

// TestPlaceGateOrder pins T2's canonical Given/When/Then: an
// un-allowlisted uuid refuses with ErrNotAllowedlisted AND no StartCall
// AND no audit row (zero side effects before any gate).
func TestPlaceGateOrder(t *testing.T) {
	leg, fx, audit, _ := newTestLeg(t, func(uuid string) bool {
		return uuid == "allowed-uuid"
	})

	_, err := leg.Place(context.Background(), "emp-1", "not-allowed-uuid", "hello")
	if !errors.Is(err, ErrNotAllowedlisted) {
		t.Fatalf("Place(not-allowed) err = %v, want ErrNotAllowedlisted", err)
	}
	if len(audit.rows) != 0 {
		t.Errorf("audit rows = %d, want 0 (allowlist refusal precedes the audit row)", len(audit.rows))
	}
	if len(fx.reserve) != 0 {
		t.Errorf("effect reserves = %d, want 0 (allowlist refusal precedes the reserve)", len(fx.reserve))
	}

	// Employee + uuid gates.
	if _, err := leg.Place(context.Background(), "", "allowed-uuid", ""); !errors.Is(err, ErrNoEmployee) {
		t.Errorf("Place(empty employee) err = %v, want ErrNoEmployee", err)
	}
	if _, err := leg.Place(context.Background(), "emp-1", "", ""); !errors.Is(err, ErrNoTarget) {
		t.Errorf("Place(empty uuid) err = %v, want ErrNoTarget", err)
	}
}

// TestPlaceHappyPathAndRegistry pins the registry entry shape: the
// target stored VERBATIM "signal:<uuid>" with leg "signal", the ccid
// carries the "signal-" prefix, and the effect reserve + audit row ran
// before StartCall (phone-outbound posture).
func TestPlaceHappyPathAndRegistry(t *testing.T) {
	leg, fx, audit, _ := newTestLeg(t, func(uuid string) bool { return true })

	ccid, err := leg.Place(context.Background(), "emp-1", "aaaa1111-2222-3333-4444-555555555555", "hello")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if len(ccid) <= len("signal-") || ccid[:len("signal-")] != "signal-" {
		t.Errorf("ccid = %q, want \"signal-\" prefix", ccid)
	}
	live := leg.deps.Registry.Live()
	if len(live) != 1 {
		t.Fatalf("registry live = %d, want 1", len(live))
	}
	inst := live[0]
	if inst.Target != "signal:aaaa1111-2222-3333-4444-555555555555" {
		t.Errorf("target = %q, want verbatim signal:<uuid> (no E.164 mangle)", inst.Target)
	}
	if inst.Leg != voice.LegSignal {
		t.Errorf("leg = %q, want %q", inst.Leg, voice.LegSignal)
	}
	if len(fx.reserve) != 1 {
		t.Errorf("effect reserves = %d, want 1 (BEFORE StartCall)", len(fx.reserve))
	}
	if len(audit.rows) != 1 {
		t.Errorf("audit rows = %d, want 1 (BEFORE StartCall)", len(audit.rows))
	}
}

// TestEndIdempotentAndForeign pins T5: double End is nil; a foreign id
// is ErrUnknownSignalCall.
func TestEndIdempotentAndForeign(t *testing.T) {
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })

	ccid, err := leg.Place(context.Background(), "emp-1", "bbbb2222-2222-3333-4444-555555555555", "")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	if err := leg.End(ccid); err != nil {
		t.Fatalf("first End: %v", err)
	}
	if err := leg.End(ccid); err != nil {
		t.Errorf("double End = %v, want nil (idempotent)", err)
	}
	if err := leg.End("local-not-ours"); !errors.Is(err, ErrUnknownSignalCall) {
		t.Errorf("foreign End = %v, want ErrUnknownSignalCall", err)
	}
	if len(leg.Live()) != 0 {
		t.Errorf("Live after End = %d, want 0 (teardown reaped the call)", len(leg.Live()))
	}
}

// TestStateMachineEndedBusy pins T3's canonical Given/When/Then: a
// CONNECTED then ENDED(busy) event sequence runs FailOutcome with the
// busy-distinct outcome and releases the registry slot.
func TestStateMachineEndedBusy(t *testing.T) {
	leg, _, _, _ := newTestLeg(t, func(string) bool { return true })
	reg := leg.deps.Registry

	ccid, err := leg.Place(context.Background(), "emp-9", "cccc3333-2222-3333-4444-555555555555", "")
	if err != nil {
		t.Fatalf("Place: %v", err)
	}
	// The stub assigned call-stub-1 (the only startCall on this client).
	st := leg.stateByCallID("call-stub-1")
	if st == nil {
		t.Fatalf("state machine did not map call-stub-1 to the placed call")
	}
	if st.ccid != ccid {
		t.Errorf("mapped ccid = %q, want %q", st.ccid, ccid)
	}

	// CONNECTED (tolerated: no adapter socket -> stub-audio degrade).
	leg.handleCallEvent(signal.CallEvent{
		EnvelopeSourceUUID: "cccc3333-2222-3333-4444-555555555555",
		CallID:             "call-stub-1",
		State:              signal.CallEventStateConnected,
	})
	// RECONNECTING tolerated (no teardown).
	leg.handleCallEvent(signal.CallEvent{
		CallID: "call-stub-1",
		State:  signal.CallEventStateReconnecting,
	})
	if len(reg.Live()) != 1 {
		t.Errorf("RECONNECTING tore the call down; live = %d, want 1", len(reg.Live()))
	}
	// ENDED(busy) -> the busy-distinct outcome path runs FailOutcome
	// (nil-carrier manager: fireRelease has no hook here, but the
	// once-only teardown completed) and the registry releases.
	leg.handleCallEvent(signal.CallEvent{
		CallID: "call-stub-1",
		State:  signal.CallEventStateEnded,
		Reason: "busy",
	})
	if got := len(reg.Live()); got != 0 {
		t.Errorf("ENDED(busy) left %d live calls, want 0 (registry released)", got)
	}
}

// TestNewNilDeps pins the construction refusals (local.go:191 posture).
func TestNewNilDeps(t *testing.T) {
	reg := voice.NewRegistry(4)
	mgr := voice.NewManager(voice.ManagerConfig{}, nil, nil)
	ts := voice.NewTextServer(filepath.Join(shortAudioDir(t), "x.sock"), discardLog())
	client, err := signal.Dial(func() string {
		ln, lerr := net.Listen("unix", filepath.Join(shortAudioDir(t), "nil.sock"))
		if lerr != nil {
			t.Fatalf("listen: %v", lerr)
		}
		t.Cleanup(func() { _ = ln.Close() })
		go func() {
			for {
				c, aerr := ln.Accept()
				if aerr != nil {
					return
				}
				go func(cc net.Conn) {
					defer func() { _ = cc.Close() }() //nolint:errcheck // stub
					b := make([]byte, 4096)
					for {
						n, rerr := cc.Read(b)
						if rerr != nil {
							return
						}
						// Reply null result to every line.
						idx := -1
						for j, ch := range b[:n] {
							if ch == '\n' {
								idx = j
								break
							}
						}
						if idx < 0 {
							continue
						}
						line := b[:idx]
						var req struct {
							ID int `json:"id"`
						}
						_ = json.Unmarshal(line, &req) //nolint:errcheck // stub
						resp := `{"jsonrpc":"2.0","id":` + testItoa(req.ID) + `,"result":null}` + "\n"
						_, _ = cc.Write([]byte(resp)) //nolint:errcheck // stub
					}
				}(c)
			}
		}()
		return ln.Addr().String()
	}())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() }) //nolint:errcheck // test teardown

	if _, err := New(Deps{Registry: nil, Manager: mgr, TextServer: ts, Client: client}); err == nil {
		t.Errorf("nil Registry accepted")
	}
	if _, err := New(Deps{Registry: reg, Manager: nil, TextServer: ts, Client: client}); err == nil {
		t.Errorf("nil Manager accepted")
	}
	if _, err := New(Deps{Registry: reg, Manager: mgr, TextServer: nil, Client: client}); err == nil {
		t.Errorf("nil TextServer accepted")
	}
	if _, err := New(Deps{Registry: reg, Manager: mgr, TextServer: ts, Client: nil}); err == nil {
		t.Errorf("nil Client accepted")
	}
}
