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
	"sync"
	"sync/atomic"
	"time"

	"github.com/caimlas/meept/pkg/id"
)

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
}

// Link is one live link handle: both ccids plus Close/Status sugar.
type Link struct {
	ID        string
	ACCID     string
	BCCID     string
	AEmployee string
	BEmployee string

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
func (m *LinkManager) Open(ctx context.Context, employeeA, employeeB string) (*Link, error) {
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

	conns, derr := m.dialAll(ctx, aCall, bCall)
	if derr != nil {
		m.closeConns(conns)
		if endErr := m.deps.Leg.End(aCall.CallID, "link: dial failed"); endErr != nil {
			m.log.Debug("voice: link unwind: end side a on dial failure", "ccid", aCall.CallID, "err", endErr)
		}
		if endErr := m.deps.Leg.End(bCall.CallID, "link: dial failed"); endErr != nil {
			m.log.Debug("voice: link unwind: end side b on dial failure", "ccid", bCall.CallID, "err", endErr)
		}
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
	}
	// CROSS-connect, byte-exact: A's speaker -> B's mic, B's speaker ->
	// A's mic. io.Copy is the whole contract (no re-chunking: the daemon
	// already paces each leg's outbound at one 640 B chunk per 20 ms).
	entry.pumps.Add(2)
	go func() {
		defer entry.pumps.Done()
		m.pumpStream(pumpCtx, entry.aSpk, entry.bMic, linkID, "a->b")
	}()
	go func() {
		defer entry.pumps.Done()
		m.pumpStream(pumpCtx, entry.bSpk, entry.aMic, linkID, "b->a")
	}()

	m.mu.Lock()
	m.sweepEndedLocked()
	m.links[linkID] = entry
	m.mu.Unlock()

	m.log.Info("voice: link open", "link_id", linkID,
		"a_ccid", aCall.CallID, "b_ccid", bCall.CallID,
		"a_employee", employeeA, "b_employee", employeeB)
	return &Link{
		ID:        linkID,
		ACCID:     aCall.CallID,
		BCCID:     bCall.CallID,
		AEmployee: employeeA,
		BEmployee: employeeB,
		mgr:       m,
	}, nil
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
func (m *LinkManager) pumpStream(ctx context.Context, src, dst net.Conn, linkID, direction string) {
	var bytes atomic.Uint64
	var firstOnce sync.Once
	started := time.Now()
	cs := &countingSrc{src: src, n: &bytes, onFirst: func() {
		firstOnce.Do(func() {
			m.log.Info("voice: link pump first byte", "link_id", linkID,
				"dir", direction, "after_ms", time.Since(started).Milliseconds())
		})
	}}
	done := make(chan struct{})
	go func() {
		defer close(done)
		_, err := io.Copy(dst, cs)
		copied := bytes.Load()
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
	_ = started
}

// countingSrc is an io.Reader wrapper that counts bytes and fires onFirst
// the first time it hands out a non-empty read.
type countingSrc struct {
	src     io.Reader
	n       *atomic.Uint64
	onFirst func()
}

func (c *countingSrc) Read(p []byte) (int, error) {
	n, err := c.src.Read(p)
	if n > 0 {
		c.n.Add(uint64(n))
		c.onFirst()
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
	// FailOutcome fan-out owns registry/binding/childproc release).
	entry.cancel()
	entry.closeConns.Do(func() {
		m.closeConns([4]net.Conn{entry.aMic, entry.aSpk, entry.bMic, entry.bSpk})
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
	}
	return out
}
