package voice

// link.go - the two-agent voice link (deadair-link plan leaf 03,
// docs/plans/20261110-voice-deadair-link/03-voice-link.md): cross-connect
// TWO local-leg calls in-process so one employee's spoken PCM becomes the
// other employee's microphone and vice versa. Real TTS -> real socket PCM
// -> real STT -> real employee turns on both sides; no human, no ffmpeg,
// no oto. This is the loop-testing harness for the jitter + dead-air
// acceptance.
//
// Architecture (in-daemon bridge; the CLI never touches audio):
//
//   - LocalLeg.Open is used UNCHANGED (leaf 03 touches no local.go line).
//     Two calls open with labels "link-a-<id>" / "link-b-<id>" so the
//     registry targets ("local:link-a-...", "local:link-b-...") stay
//     distinct from each other AND from phone/local: labels; NormalizeE164
//     never runs for local labels (dial.go leg rules, C-registry/CL6).
//   - The bridge then dials BOTH client pair sockets for each leg exactly
//     like the CLI does (net.Dial("unix", InPath/OutPath)): A's OutPath
//     conn READS A's speaker bytes, B's InPath conn WRITES B's mic bytes,
//     and io.Copy crosses them; the second pump mirrors the direction.
//     Each leg sees precisely what a human client would - no splice,
//     frame, or registry change.
//   - Close ends both calls (LocalLeg.End, idempotent), cancels the pump
//     ctx, and closes the four dialed conns.
//   - Optional audible monitor (Open opts.Monitor): two extra unix
//     listeners <pair dir>/<link id>.mon-a / .mon-b carry a NON-BLOCKING
//     tee of each pump direction so the operator's CLI can play BOTH
//     employees through speakers. The tap drops bytes whenever no
//     client is attached or a tap write fails - monitor traffic NEVER
//     stalls or gates the bridge itself.
//
// Deliberate link-mode notes (pinned here, not accidents):
//
//   - Barge-in. The local leg's child-side RMS barge-in is driven by the
//     voice.local.barge_in_rms knob baked into LocalLegConfig AT wiring
//     construction (internal/daemon/voice_wiring.go NewLocalLeg) - there
//     is NO per-call override seam on LocalLeg.Open (the variadic is the
//     TtsVoice arg only). So machine speech arriving at the OTHER leg's
//     mic CAN trip that leg's RMS window. The trip is STT-gated with
//     one-shot evidence (AGENTS.md C11: a trip zeroes lastFinalAt, a
//     second window needs a NEW final transcript, and the echo guard
//     requires window energy > 130% of the last outbound chunk), so a
//     real transcript interrupts and the interrupted chain re-forms from
//     what the other employee said - which is exactly the conversational
//     semantics a link wants. Where a run must NOT interrupt, the
//     operator applies the config-level workaround (voice.employee_leg
//     rows / voice.local.barge_in_rms=0); leaf 03 deliberately ships no
//     per-call knob because the seam does not exist and local.go is
//     frozen for this leaf.
//   - Greetings still play (fact-free, talk-loop seeded) - the greeting
//     audio crossing into the other leg's mic is the conversation SEED:
//     it also satisfies the splice's client-join grace (a first mic
//     byte within JoinTimeout, default 10 s), so neither leg reaps with
//     ErrNoClient as long as the child answers setup promptly.
//   - The silence reaper (45 s talk-loop timer) stays ENABLED: it is the
//     dead-air circuit breaker, and the acceptance run must prove it
//     fires or never has to.
//   - Registry capacity: max_concurrent_calls caps the instance; a link
//     consumes TWO slots. With the default cap 2 a link fills the
//     registry (no further calls until it closes).
//
// Teardown rides LocalLeg.End -> Manager.FailOutcome -> the normal
// release fan-out (registry release, binding end, childproc note) exactly
// once per call; the link adds only the conn closes and pump cancel.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

// maxMonitorSocketPathLen bounds a monitor socket path's byte length.
// Unix sockaddr_un sun_path is 103 usable bytes on macOS (socket.go C2
// note); the monitor suffix is checked at CREATE time and a longer path
// is the named ErrLinkMonitorPathTooLong refusal (zero side effects:
// no listener is created, both legs are unwound).
const maxMonitorSocketPathLen = 100

// ErrLinkMonitorPathTooLong is the named Monitor refusal when the
// derived monitor socket path (<pair dir>/<link id>.mon-a|.mon-b) would
// overflow the kernel sun_path limit.
var ErrLinkMonitorPathTooLong = errors.New("voice: voice link: monitor socket path exceeds the unix sun_path limit")

// monitorWriteDeadline bounds one tap write. A monitor client that
// stops draining must NEVER stall the bridge: the write fails at the
// deadline and the tap drops permanently (logged once).
const monitorWriteDeadline = 250 * time.Millisecond

// ErrLinkSameEmployee is the named up-front refusal when both sides name
// the same employee: the registry's one-live-call-per-employee rule
// would reject the second Open anyway, but the link checks BEFORE any
// side effect so the harness gets a clear message instead of ErrBusy.
var ErrLinkSameEmployee = errors.New("voice: voice link: employee_a and employee_b must be different employees")

// ErrLinkNotFound is returned by Close/Status for a link id this manager
// never opened (and no longer retains).
var ErrLinkNotFound = errors.New("voice: voice link: unknown link")

const (
	// linkDialTimeoutDefault bounds one pair-socket dial.
	linkDialTimeoutDefault = 5 * time.Second
	// linkRetainEndedDefault keeps a CLOSED link's entry queryable so a
	// CLI that closes then polls once more still gets the final turn
	// counts and transcripts (the calls.db row survives; the entry only
	// carries routing ids).
	linkRetainEndedDefault = 30 * time.Minute
)

// LinkLegOpener is the leg seam the manager drives. The concrete
// *LocalLeg satisfies it; tests inject a fake. (Typed-nil note per the
// repo rule: callers MUST pass a non-nil concrete - the daemon adapter
// checks w.Local != nil before constructing the manager.)
type LinkLegOpener interface {
	Open(ctx context.Context, employeeID, label string, voiceOverride ...string) (LocalCall, error)
	End(ccid, reason string) error
}

// LinkOpenOptions tunes one Open. Zero value = the pre-monitor shape
// (byte-compatible: the Open variadic keeps old callers compiling).
type LinkOpenOptions struct {
	// Monitor stands up two unix listeners (one per side) that carry a
	// NON-BLOCKING tee of each direction's PCM to the operator's
	// speakers. The listener paths derive from the pair-socket dir:
	// <dir>/<linkID>.mon-a / .mon-b. A monitor client is OPTIONAL:
	// nothing ever stalls the bridge waiting for one, and tap bytes are
	// dropped while no client is attached or after a tap write error.
	Monitor bool
}

// LinkTranscriptReader is the call-store seam for per-side turn counts.
// *CallStore satisfies it (EndedCall is read-only and reads live rows
// just as well as ended ones - the name is historical). Nil degrades
// Status to zero counts with empty transcripts (never a failure).
type LinkTranscriptReader interface {
	EndedCall(ctx context.Context, callID string) (CallRecord, []TranscriptLine, bool, error)
}

// LinkDeps carries the injected edges. All optional except Leg
// (constructor-guarded): Store is the transcript reader, Dial replaces
// the unix dialer (tests), DialTimeout/RetainEnded tune the defaults.
type LinkDeps struct {
	Leg         LinkLegOpener
	Store       LinkTranscriptReader
	Dial        func(ctx context.Context, path string) (net.Conn, error)
	DialTimeout time.Duration
	RetainEnded time.Duration
	Logger      *slog.Logger
}

// LinkSide is one side's live status: routing ids plus turn counts read
// through the transcript seam.
type LinkSide struct {
	CallID         string
	EmployeeID     string
	AssistantTurns int
	UserTurns      int
	Transcript     []TranscriptLine
}

// LinkStatusResult is one link's status snapshot (both sides + ended).
type LinkStatusResult struct {
	LinkID string
	Ended  bool
	A      LinkSide
	B      LinkSide
}

// linkEntry is the manager's per-link state. Conns and ctx are set once
// at Open; mu (the manager's) guards the ended flag and retention clock.
// monA/monB are the optional monitor listeners (nil when Monitor=false);
// the tapA/tapB writers read the accepted conn through each listener's
// own mutex, never the manager's.
type linkEntry struct {
	id         string
	aCall      LocalCall
	bCall      LocalCall
	aEmployee  string
	bEmployee  string
	aMic       net.Conn // A's client InPath - the bridge WRITES B's speech here
	aSpk       net.Conn // A's client OutPath - the bridge READS A's speech from here
	bMic       net.Conn
	bSpk       net.Conn
	cancel     context.CancelFunc
	pumps      sync.WaitGroup
	ended      bool
	endedAt    time.Time
	closeConns sync.Once
	monA       *monitorListener // tee sink for the a->b direction
	monB       *monitorListener // tee sink for the b->a direction
}

// tapA/tapB hand the pump the tee sink for one direction. A nil
// listener (Monitor off) yields a nil tap, which pumpStream skips
// entirely - the zero-monitor path stays the plain io.Copy it always
// was.
func (e *linkEntry) tapA() *monitorTap {
	if e.monA == nil {
		return nil
	}
	return &monitorTap{ln: e.monA}
}

func (e *linkEntry) tapB() *monitorTap {
	if e.monB == nil {
		return nil
	}
	return &monitorTap{ln: e.monB}
}

// monitorListener is one direction's monitor unix listener: a single
// accept goroutine stores exactly ONE accepted conn (set-once; a second
// client is accepted and immediately closed - the pair-socket posture
// from AudioPair, mirrored). close() is idempotent.
type monitorListener struct {
	path string
	ln   net.Listener

	mu     sync.Mutex
	conn   net.Conn // set once by the accept goroutine; nil until a client dials
	closed bool
}

func newMonitorListener(path string) (*monitorListener, error) {
	// Stale files from a crashed previous daemon must not wedge Listen
	// (NewAudioPair's defensive remove, mirrored).
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return nil, fmt.Errorf("voice: voice link: remove stale monitor socket %s: %w", path, err)
	}
	ln, err := net.Listen("unix", path)
	if err != nil {
		return nil, fmt.Errorf("voice: voice link: listen monitor socket %s: %w", path, err)
	}
	// Owner-only: the tap carries call audio, the pair sockets' dir-mode
	// posture applies. A failed chmod is cosmetic (the dir is already
	// user-owned), not fatal - mirror AudioPair's tolerance.
	_ = os.Chmod(path, 0o600)
	ml := &monitorListener{path: path, ln: ln}
	go func() {
		conn, acceptErr := ln.Accept()
		if acceptErr != nil {
			// Listener closed during teardown: the expected path.
			return
		}
		ml.mu.Lock()
		if ml.closed || ml.conn != nil {
			ml.mu.Unlock()
			_ = conn.Close()
			return
		}
		ml.conn = conn
		ml.mu.Unlock()
		// A second client still needs retiring: after the first is
		// stored, keep accepting ONLY to close extras fast (no backlog
		// of half-open monitor conns).
		for {
			extra, err := ln.Accept()
			if err != nil {
				return
			}
			_ = extra.Close()
		}
	}()
	return ml, nil
}

// current returns the accepted conn (nil when no client has dialed).
func (ml *monitorListener) current() net.Conn {
	ml.mu.Lock()
	defer ml.mu.Unlock()
	return ml.conn
}

// close tears down the listener + accepted conn and removes the socket
// file. Idempotent; safe concurrently with the accept goroutine.
func (ml *monitorListener) close() {
	ml.mu.Lock()
	if ml.closed {
		ml.mu.Unlock()
		return
	}
	ml.closed = true
	conn := ml.conn
	ml.conn = nil
	ln := ml.ln
	ml.mu.Unlock()
	if ln != nil {
		_ = ln.Close()
	}
	if conn != nil {
		_ = conn.Close()
	}
	_ = os.Remove(ml.path)
}

// monitorTap is pumpStream's tee destination for one direction. It is
// NOT an io.Writer with normal semantics: a Write NEVER returns an error
// the pump should act on (the bridge must not stall or die over monitor
// audio). Instead it resolves the accepted conn per write, bounds every
// write with monitorWriteDeadline, and on ANY failure (no conn yet,
// deadline, socket error) marks itself dead - later Writes become
// no-ops, and the drop is logged once by pumpStream's observer. Bytes
// written before a client dials are simply gone (the monitor is a live
// tap, not a recorder - the bytes-may-drop contract).
type monitorTap struct {
	ln   *monitorListener
	dead atomic.Bool
}

func (t *monitorTap) Write(p []byte) (int, error) {
	if t == nil || t.dead.Load() {
		return len(p), nil
	}
	conn := t.ln.current()
	if conn == nil {
		// No monitor client yet: drop silently (attaching is
		// on-demand; this is not an error state).
		return len(p), nil
	}
	_ = conn.SetWriteDeadline(time.Now().Add(monitorWriteDeadline))
	n, err := conn.Write(p)
	if err != nil {
		// Stale/unread monitor conn: fall back to dropping for the
		// rest of the link (logged once by the pump).
		t.dead.Store(true)
		return n, err
	}
	return n, nil
}

// Link is one live link handle: both ccids plus Close/Status sugar.
// MonitorAPath/MonitorBPath name the monitor tee sockets when
// LinkOpenOptions.Monitor was set ("" otherwise); a CLI monitor client
// dials them like any pair socket. The a->b direction (A's speech into
// B's mic) rides MonitorAPath; b->a rides MonitorBPath.
type Link struct {
	ID           string
	ACCID        string
	BCCID        string
	AEmployee    string
	BEmployee    string
	MonitorAPath string
	MonitorBPath string

	mgr *LinkManager
}

// Close tears the link down (idempotent; a second Close is nil).
func (l *Link) Close(reason string) error { return l.mgr.Close(l.ID, reason) }

// Status snapshots both sides' turn counts + transcripts.
func (l *Link) Status(ctx context.Context) (LinkStatusResult, error) {
	return l.mgr.Status(ctx, l.ID)
}

// LinkManager owns the bridge. All mutable state is mutex-guarded; deps
// are immutable after construction.
type LinkManager struct {
	deps        LinkDeps
	log         *slog.Logger
	dialTimeout time.Duration
	retain      time.Duration

	mu    sync.Mutex
	links map[string]*linkEntry
}

// NewLinkManager constructs the manager. A nil leg is a construction
// failure (the bridge is meaningless without it); every other zero
// field takes its default.
func NewLinkManager(deps LinkDeps) (*LinkManager, error) {
	if deps.Leg == nil {
		return nil, errors.New("voice: voice link: local leg is required")
	}
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	m := &LinkManager{
		deps:        deps,
		log:         deps.Logger,
		dialTimeout: deps.DialTimeout,
		retain:      deps.RetainEnded,
		links:       make(map[string]*linkEntry),
	}
	if m.dialTimeout <= 0 {
		m.dialTimeout = linkDialTimeoutDefault
	}
	if m.retain <= 0 {
		m.retain = linkRetainEndedDefault
	}
	if m.deps.Dial == nil {
		m.deps.Dial = m.dialUnix
	}
	return m, nil
}

// dialUnix is the default dialer: exactly the CLI's
// net.Dial("unix", path) pattern, ctx-bounded.
func (m *LinkManager) dialUnix(ctx context.Context, path string) (net.Conn, error) {
	d := net.Dialer{Timeout: m.dialTimeout}
	dialCtx, cancel := context.WithTimeout(ctx, m.dialTimeout)
	defer cancel()
	conn, err := d.DialContext(dialCtx, "unix", path)
	if err != nil {
		return nil, fmt.Errorf("dial pair socket %s: %w", path, err)
	}
	return conn, nil
}

// Open cross-connects two local-leg calls. Refusal order (zero side
// effects before each): empty ids -> same employee. Every later failure
// unwinds fully (dialed conns closed, opened legs ended) so a partial
// link never leaks a live call.
//
// opts (variadic; first entry only) carries LinkOpenOptions. With
// Monitor=true the bridge additionally stands up two unix listeners in
// the pair-socket dir and tees each pump direction's PCM into them
// (see monitorTap below). The variadic keeps every pre-monitor caller
// byte-compatible.
func (m *LinkManager) Open(ctx context.Context, employeeA, employeeB string, opts ...LinkOpenOptions) (*Link, error) {
	var option LinkOpenOptions
	if len(opts) > 0 {
		option = opts[0]
	}
	if employeeA == "" || employeeB == "" {
		return nil, fmt.Errorf("%w: both employee ids are required", ErrNoEmployee)
	}
	if employeeA == employeeB {
		return nil, ErrLinkSameEmployee
	}

	linkID := id.Generate("link-")
	// Label suffix keeps two concurrent links from colliding on the
	// registry's one-live-per-TARGET rule while preserving the distinct
	// "link-a"/"link-b" shapes the leaf doc pins.
	suffix := linkID[len("link-"):]

	aCall, err := m.deps.Leg.Open(ctx, employeeA, "link-a-"+suffix)
	if err != nil {
		return nil, fmt.Errorf("voice: voice link: open side a (%s): %w", employeeA, err)
	}
	bCall, err := m.deps.Leg.Open(ctx, employeeB, "link-b-"+suffix)
	if err != nil {
		// Unwind the first leg: End is idempotent, the release fan-out
		// does the rest. The unwind error never masks the refusal.
		if endErr := m.deps.Leg.End(aCall.CallID, "link: side b open failed"); endErr != nil {
			m.log.Debug("voice: link unwind: end side a", "ccid", aCall.CallID, "err", endErr)
		}
		return nil, fmt.Errorf("voice: voice link: open side b (%s): %w", employeeB, err)
	}

	// Monitor listeners BEFORE dialing (the pair dir comes from the
	// opened legs; both legs share SocketDir so either OutPath works).
	// A sun_path overflow refuses here with ZERO side effects beyond the
	// two legs, which the shared unwind below ends.
	var monA, monB *monitorListener
	var monAPath, monBPath string
	if option.Monitor {
		dir := filepath.Dir(aCall.OutPath)
		monAPath = filepath.Join(dir, linkID+".mon-a")
		monBPath = filepath.Join(dir, linkID+".mon-b")
		if len(monAPath) > maxMonitorSocketPathLen || len(monBPath) > maxMonitorSocketPathLen {
			m.unwindBoth(aCall, bCall, "link: monitor path too long")
			return nil, fmt.Errorf("%w: %s", ErrLinkMonitorPathTooLong, monAPath)
		}
		var lerr error
		monA, lerr = newMonitorListener(monAPath)
		if lerr != nil {
			m.unwindBoth(aCall, bCall, "link: monitor listen failed")
			return nil, fmt.Errorf("voice: voice link: %w", lerr)
		}
		monB, lerr = newMonitorListener(monBPath)
		if lerr != nil {
			monA.close()
			m.unwindBoth(aCall, bCall, "link: monitor listen failed")
			return nil, fmt.Errorf("voice: voice link: %w", lerr)
		}
	}

	conns, derr := m.dialAll(ctx, aCall, bCall)
	if derr != nil {
		m.closeConns(conns)
		if monA != nil {
			monA.close()
		}
		if monB != nil {
			monB.close()
		}
		m.unwindBoth(aCall, bCall, "link: dial failed")
		return nil, fmt.Errorf("voice: voice link: %w", derr)
	}

	pumpCtx, pumpCancel := context.WithCancel(context.Background())
	entry := &linkEntry{
		id:        linkID,
		aCall:     aCall,
		bCall:     bCall,
		aEmployee: employeeA,
		bEmployee: employeeB,
		aMic:      conns[0],
		aSpk:      conns[1],
		bMic:      conns[2],
		bSpk:      conns[3],
		cancel:    pumpCancel,
		monA:      monA,
		monB:      monB,
	}
	// CROSS-connect, byte-exact: A's speaker -> B's mic, B's speaker ->
	// A's mic. io.Copy is the whole contract (no re-chunking: the daemon
	// already paces each leg's outbound at one 640 B chunk per 20 ms).
	// The monitor tap (nil when Monitor=false) copies every byte the
	// pump moves, non-blocking: a slow or absent monitor client only
	// ever loses tap bytes, never bridge bytes.
	entry.pumps.Add(2)
	go func() {
		defer entry.pumps.Done()
		m.pumpStream(pumpCtx, entry.aSpk, entry.bMic, linkID, "a->b", entry.tapA())
	}()
	go func() {
		defer entry.pumps.Done()
		m.pumpStream(pumpCtx, entry.bSpk, entry.aMic, linkID, "b->a", entry.tapB())
	}()

	m.mu.Lock()
	m.sweepEndedLocked()
	m.links[linkID] = entry
	m.mu.Unlock()

	m.log.Info("voice: link open", "link_id", linkID,
		"a_ccid", aCall.CallID, "b_ccid", bCall.CallID,
		"a_employee", employeeA, "b_employee", employeeB,
		"monitor", option.Monitor)
	return &Link{
		ID:           linkID,
		ACCID:        aCall.CallID,
		BCCID:        bCall.CallID,
		AEmployee:    employeeA,
		BEmployee:    employeeB,
		MonitorAPath: monAPath,
		MonitorBPath: monBPath,
		mgr:          m,
	}, nil
}

// unwindBoth ends both legs with the same reason (each End is
// idempotent; a failure on one never masks the other - debug-logged).
func (m *LinkManager) unwindBoth(aCall, bCall LocalCall, reason string) {
	if endErr := m.deps.Leg.End(aCall.CallID, reason); endErr != nil {
		m.log.Debug("voice: link unwind: end side a", "ccid", aCall.CallID, "err", endErr)
	}
	if endErr := m.deps.Leg.End(bCall.CallID, reason); endErr != nil {
		m.log.Debug("voice: link unwind: end side b", "ccid", bCall.CallID, "err", endErr)
	}
}

// dialAll dials the four client pair sockets in a fixed order:
// [A.mic, A.speaker, B.mic, B.speaker]. A failure mid-way closes
// whatever already dialed and surfaces the named dial error.
func (m *LinkManager) dialAll(ctx context.Context, aCall, bCall LocalCall) ([4]net.Conn, error) {
	paths := [4]string{aCall.InPath, aCall.OutPath, bCall.InPath, bCall.OutPath}
	var conns [4]net.Conn
	for i, p := range paths {
		conn, err := m.deps.Dial(ctx, p)
		if err != nil {
			for j := 0; j < i; j++ {
				if conns[j] != nil {
					_ = conns[j].Close()
				}
			}
			return conns, fmt.Errorf("dial side %s (%s): %w", []string{"a.mic", "a.speaker", "b.mic", "b.speaker"}[i], p, err)
		}
		conns[i] = conn
	}
	return conns, nil
}

func (m *LinkManager) closeConns(conns [4]net.Conn) {
	for _, c := range conns {
		if c != nil {
			_ = c.Close()
		}
	}
}

// pumpStream copies src -> dst until EOF, a socket error (the leg ended:
// the pair Close breaks the copy - the expected teardown path, logged at
// debug only), or ctx cancel (which closes both conns to unblock the
// copy, never holding a lock across that I/O).
//
// Telemetry (link-72a04aebac963cdb post-mortem: a half-dead splice was
// only inferable from a +0 ms cadence summary): every pump logs its
// FIRST byte at INFO (the moment a direction demonstrably carries PCM)
// and its end with the total bytes copied, so the next live link run
// self-explains which direction died and when.
//
// tee (optional, nil = no monitor) is the monitor tap: every byte read
// from src is copied into it through a separate writer goroutine fed by
// a capacity-1 channel (latest-batch-wins when the tap lags), so a
// stalled monitor client can never slow or stall the io.Copy - the
// bridge's byte-exact contract is untouched by the tap. A tap write
// error marks the tap dead (drop for the rest of the link, logged
// once). Monitor bytes NEVER gate link correctness.
func (m *LinkManager) pumpStream(ctx context.Context, src, dst net.Conn, linkID, direction string, tee *monitorTap) {
	var bytes atomic.Uint64
	var firstOnce sync.Once
	started := time.Now()
	cs := &countingSrc{src: src, n: &bytes, onFirst: func() {
		firstOnce.Do(func() {
			m.log.Info("voice: link pump first byte", "link_id", linkID,
				"dir", direction, "after_ms", time.Since(started).Milliseconds())
		})
	}}

	var (
		teeDone  chan struct{}
		teeBatch chan []byte
		teeStop  chan struct{}
		teeDrop  atomic.Uint64
	)
	if tee != nil {
		teeBatch = make(chan []byte, 1)
		teeDone = make(chan struct{})
		teeStop = make(chan struct{})
		cs.onTee = func(p []byte) {
			// Copy first: the buffer belongs to io.Copy's read loop and
			// is reused on the next read.
			cp := make([]byte, len(p))
			copy(cp, p)
			select {
			case teeBatch <- cp:
			default:
				// Tap writer is behind: drop THIS batch (live-tap
				// semantics, never block the bridge).
				teeDrop.Add(1)
			}
		}
		go func() {
			defer close(teeDone)
			for {
				select {
				case batch := <-teeBatch:
					if _, err := tee.Write(batch); err != nil {
						// First (and only) tap socket death: log once,
						// the tap self-disarms to drops afterwards.
						m.log.Info("voice: link monitor tap dropped", "link_id", linkID,
							"dir", direction, "err", err)
					}
				case <-teeStop:
					// Pump finished (EOF, socket error, or teardown):
					// drain whatever already queued best-effort - each
					// tap Write is deadline-bounded, so this never
					// hangs the pump's exit.
					for {
						select {
						case batch := <-teeBatch:
							_, _ = tee.Write(batch)
						default:
							return
						}
					}
				}
			}
		}()
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := io.Copy(dst, cs)
		copied := bytes.Load()
		if teeDrop.Load() > 0 {
			m.log.Debug("voice: link monitor tap drops", "link_id", linkID,
				"dir", direction, "drops", teeDrop.Load())
		}
		if err != nil && ctx.Err() == nil {
			// A real mid-stream break while the link is supposed to be
			// live. Copy stops either way; the legs stay up (their own
			// reapers bound the wreckage) but this is loud enough to
			// catch in a harness run.
			m.log.Warn("voice: link pump ended with error", "link_id", linkID, "dir", direction,
				"bytes", copied, "err", err)
		} else {
			m.log.Info("voice: link pump ended", "link_id", linkID, "dir", direction, "bytes", copied)
		}
	}()
	select {
	case <-done:
	case <-ctx.Done():
		_ = src.Close()
		_ = dst.Close()
		<-done
	}
	if teeStop != nil {
		// Pump is finished: stop the tap writer (it drains the last
		// queued batch), then join it before returning.
		close(teeStop)
		<-teeDone
	}
	_ = started
}

// countingSrc is an io.Reader wrapper that counts bytes and fires onFirst
// the first time it hands out a non-empty read. onTee (optional) mirrors
// every non-empty read to the monitor tap.
type countingSrc struct {
	src     io.Reader
	n       *atomic.Uint64
	onFirst func()
	onTee   func([]byte)
}

func (c *countingSrc) Read(p []byte) (int, error) {
	n, err := c.src.Read(p)
	if n > 0 {
		c.n.Add(uint64(n))
		c.onFirst()
		if c.onTee != nil {
			c.onTee(p[:n])
		}
	}
	return n, err
}

// Close ends both calls and kills the pumps. Idempotent (a second Close
// is nil); an unknown link id is ErrLinkNotFound. The entry stays
// retained (queryable via Status) for RetainEnded.
func (m *LinkManager) Close(linkID, reason string) error {
	m.mu.Lock()
	entry := m.links[linkID]
	if entry == nil {
		m.mu.Unlock()
		return fmt.Errorf("%w: %s", ErrLinkNotFound, linkID)
	}
	if entry.ended {
		m.mu.Unlock()
		return nil
	}
	entry.ended = true
	entry.endedAt = time.Now()
	aCCID, bCCID := entry.aCall.CallID, entry.bCall.CallID
	m.mu.Unlock()

	// Teardown OUTSIDE the lock (repo rule: no I/O under mutex). Order
	// mirrors a client hangup: kill the pumps + conns first so nothing
	// writes into a closing pair, then End the legs (idempotent; the
	// FailOutcome fan-out owns registry/binding/childproc release). The
	// monitor listeners + tapped conns go last: closing them while a
	// pump still drains its tee channel only drops bytes (the tap is
	// non-gating by contract).
	entry.cancel()
	entry.closeConns.Do(func() {
		m.closeConns([4]net.Conn{entry.aMic, entry.aSpk, entry.bMic, entry.bSpk})
		if entry.monA != nil {
			entry.monA.close()
		}
		if entry.monB != nil {
			entry.monB.close()
		}
	})
	if err := m.deps.Leg.End(aCCID, reason); err != nil {
		m.log.Debug("voice: link close: end side a", "ccid", aCCID, "err", err)
	}
	if err := m.deps.Leg.End(bCCID, reason); err != nil {
		m.log.Debug("voice: link close: end side b", "ccid", bCCID, "err", err)
	}
	m.log.Info("voice: link closed", "link_id", linkID, "a_ccid", aCCID, "b_ccid", bCCID, "reason", reason)
	return nil
}

// Status snapshots both sides. Works after Close for the retention
// window (transcripts ride the calls.db row, which outlives the entry);
// an unknown/expired link is ErrLinkNotFound.
func (m *LinkManager) Status(ctx context.Context, linkID string) (LinkStatusResult, error) {
	m.mu.Lock()
	entry := m.links[linkID]
	if entry == nil {
		m.mu.Unlock()
		return LinkStatusResult{}, fmt.Errorf("%w: %s", ErrLinkNotFound, linkID)
	}
	aCCID, bCCID := entry.aCall.CallID, entry.bCall.CallID
	aEmp, bEmp := entry.aEmployee, entry.bEmployee
	ended := entry.ended
	m.mu.Unlock()

	res := LinkStatusResult{LinkID: linkID, Ended: ended}
	res.A = m.sideStatus(ctx, aCCID, aEmp)
	res.B = m.sideStatus(ctx, bCCID, bEmp)
	return res, nil
}

// sideStatus reads one call's transcript through the store seam. A nil
// or failing seam degrades to zero counts (a status poll must never
// abort a harness run mid-link).
func (m *LinkManager) sideStatus(ctx context.Context, ccid, employee string) LinkSide {
	side := LinkSide{CallID: ccid, EmployeeID: employee}
	if m.deps.Store == nil {
		return side
	}
	_, lines, _, err := m.deps.Store.EndedCall(ctx, ccid)
	if err != nil {
		m.log.Debug("voice: link status: transcript read", "ccid", ccid, "err", err)
		return side
	}
	for _, l := range lines {
		switch l.Role {
		case "assistant":
			side.AssistantTurns++
		case "user":
			side.UserTurns++
		}
	}
	side.Transcript = lines
	return side
}

// sweepEndedLocked drops closed entries past the retention window.
// Called under m.mu from Open (cheap: the map is tiny); callers must
// hold the mutex.
func (m *LinkManager) sweepEndedLocked() {
	cutoff := time.Now().Add(-m.retain)
	for id, entry := range m.links {
		if entry.ended && entry.endedAt.Before(cutoff) {
			delete(m.links, id)
		}
	}
}

// LiveLinks snapshots the currently-open (not yet closed) links for
// observability (voice.link status from the CLI's side).
func (m *LinkManager) LiveLinks() []Link {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Link, 0, len(m.links))
	for _, entry := range m.links {
		if entry.ended {
			continue
		}
		out = append(out, Link{
			ID:        entry.id,
			ACCID:     entry.aCall.CallID,
			BCCID:     entry.bCall.CallID,
			AEmployee: entry.aEmployee,
			BEmployee: entry.bEmployee,
			mgr:       m,
		})
		if entry.monA != nil {
			out[len(out)-1].MonitorAPath = entry.monA.path
		}
		if entry.monB != nil {
			out[len(out)-1].MonitorBPath = entry.monB.path
		}
	}
	return out
}
