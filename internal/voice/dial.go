// Outbound dial (C9): the employee-callable dial path. Order is
// load-bearing: policy gates -> audit row -> effects reserve -> POST /calls
// -> TrackOutbound -> registry Acquire -> no-answer timer. Any failure
// before the POST leaves no side effect; any failure after completes or
// abandons the effect with its outcome. A retry of the same request_id
// never rings twice: the effects ledger short-circuits it with the
// reserved result.
//
// The dial effect records "a call was placed" (receipt = call_control_id)
// and completes at POST success; the call's terminal outcome (hangup,
// no-answer) is registry release + Done-channel closure here and goal
// unparking in leaf 12, not a second ledger row.
package voice

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/callerid"
	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/voice/telnyx"
)

// Typed refusal errors. Every policy gate returns one of these BEFORE any
// HTTP call, so the employee surfaces a named reason, never a bare
// "dial failed".
var (
	ErrVoiceDisabled    = errors.New("voice: dialing disabled (voice.enabled=false)")
	ErrNumberNotAllowed = errors.New("voice: target number not in voice.allowed_numbers")
	ErrDailyCapReached  = errors.New("voice: outbound dial daily cap reached")
	ErrNoPublicURL      = errors.New("voice: voice.public_stream_url not configured")
	ErrNoConnectionID   = errors.New("voice: voice.connection_id not configured")
	ErrBusy             = errors.New("voice: call registry busy (over capacity or employee already live)")
	ErrDuplicateTarget  = errors.New("voice: a live call to this target already exists")
)

// DefaultDailyDialCap is retained as a deprecated compile-compatibility
// alias for the old build-time ceiling. The cap is now operator-configured
// via VoiceConfig.DailyDialCap (voice.daily_dial_cap); this constant is NOT
// consulted by Dial any more. 0 means unlimited in both shapes.
//
// Deprecated: wire cfg.DailyDialCap through the daemon instead.
const DefaultDailyDialCap = 0

// DailyCounter is a durable, restart-safe per-day counter. File shape:
// {"counts":{"<day>|<scope>":count}}. The day is part of the key so a
// local-day dial scope and a UTC-day SMS scope coexist in one file without
// one resetting the other at a midnight boundary. Old entries age out
// lazily (one row per scope per day the daemon is live). The file is
// written 0600 and replaced by atomic rename, so a crash mid-write leaves
// either the old file or the new one (never a truncated one). TryInc
// serializes read-modify-write with an OS file lock (flock/LockFileEx) for
// durable counters and with mu for in-memory counters. No sync.Mutex is
// held while file I/O runs; the OS lock makes the whole sequence atomic
// across processes.
//
// WHY FILE-BASED AND NOT A SQLite daily_counters TABLE: the three existing
// SQLite stores and their schema declarations are all OUTSIDE the file set
// this change may edit — internal/effects/ledger_sqlite.go:29
// (effectsLedgerSchema) + internal/effects/effects.go:117 (Ledger interface,
// implemented by MemoryLedger, SQLiteLedger AND test fakes), internal/voice/
// callstore.go:40 (callStoreSchema) and internal/sms/store.go:56
// (smsStoreSchema). The two wiring files in scope are restricted to config
// pass-through; plumbing a new SQL handle would mean editing components.go
// and those store packages — a broad change this follow-up must not make.
// The file path is DERIVED from the state dir (no new config field): the
// daemon passes config.MeeptPath("voice", "daily_counters.json"). A zero
// DailyCounter (path == "") keeps an in-memory map: that is the legacy
// in-process behavior for hand-built dialers, byte-identical when the cap
// is 0 (no counter I/O happens on any dial path).
type DailyCounter struct {
	path string // "" = in-memory only (not durable across a restart)
	mu   sync.Mutex
	mem  map[string]int // keyed by "<day>|<scope>"
}

type dailyCounterFile struct {
	Counts map[string]int `json:"counts"` // keyed by "<day>|<scope>"
}

// NewDailyCounter returns a counter persisted at path. The caller owns the
// lifecycle; the path must be stable across daemon restarts for the cap to
// survive. An empty path yields an in-memory-only counter.
func NewDailyCounter(path string) *DailyCounter { return &DailyCounter{path: path} }

// TryInc consumes one unit for (day, scope) unless cap > 0 and the current
// count already reached cap. It returns the count after the call (unchanged
// on refusal) and whether the unit was admitted. A refused attempt never
// writes the file, so it consumes no budget. A corrupt file is an error
// (fail closed) rather than a silent reset that would reopen the cap.
// readDailyCounts loads the durable count map outside the mutex: file I/O
// must never run while a lock is held (root coding-practices rule).
func readDailyCounts(path string) (map[string]int, error) {
	counts := map[string]int{}
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, fmt.Errorf("voice daily counter: read %s: %w", path, err)
	}
	if len(data) == 0 {
		return counts, nil
	}
	var f dailyCounterFile
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("voice daily counter: corrupt %s: %w", path, err)
	}
	if f.Counts != nil {
		counts = f.Counts
	}
	return counts, nil
}

// writeDailyCounts persists the count map atomically, outside the mutex.
// The caller must not mutate counts until this function returns.
func writeDailyCounts(path string, counts map[string]int) error {
	buf, err := json.Marshal(dailyCounterFile{Counts: counts})
	if err != nil {
		return fmt.Errorf("voice daily counter: encode: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("voice daily counter: mkdir: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".daily-counter-*")
	if err != nil {
		return fmt.Errorf("voice daily counter: temp: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() {
		tmp.Close()
		os.Remove(tmpName)
	}
	if err := tmp.Chmod(0o600); err != nil {
		cleanup()
		return fmt.Errorf("voice daily counter: chmod: %w", err)
	}
	if _, err := tmp.Write(buf); err != nil {
		cleanup()
		return fmt.Errorf("voice daily counter: write: %w", err)
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("voice daily counter: close: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		os.Remove(tmpName)
		return fmt.Errorf("voice daily counter: rename: %w", err)
	}
	return nil
}

func (c *DailyCounter) TryInc(day, scope string, cap int) (count int, admitted bool, err error) {
	key := day + "|" + scope

	// In-memory counters need only the in-process mutex. Durable counters
	// take an OS file lock FIRST, so read-check-write is atomic across
	// processes. No sync.Mutex is held while any file I/O runs.
	if c.path == "" {
		c.mu.Lock()
		if c.mem == nil {
			c.mem = map[string]int{}
		}
		cur := c.mem[key]
		if cap > 0 && cur >= cap {
			c.mu.Unlock()
			return cur, false, nil
		}
		cur++
		c.mem[key] = cur
		c.mu.Unlock()
		return cur, true, nil
	}

	// Create the parent dir BEFORE the lock file is opened: a fresh
	// scratch MEEPT_HOME has no voice/ dir yet, and the lock lives beside
	// the counter file. writeDailyCounts also ensures this for callers
	// that construct a counter lazily.
	if err := os.MkdirAll(filepath.Dir(c.path), 0o755); err != nil {
		return 0, false, fmt.Errorf("voice daily counter: mkdir: %w", err)
	}
	release, err := lockDailyCounterFile(c.path + ".lock")
	if err != nil {
		return 0, false, fmt.Errorf("voice daily counter: lock: %w", err)
	}
	defer release()

	counts, err := readDailyCounts(c.path)
	if err != nil {
		return 0, false, err
	}
	cur := counts[key]
	if cap > 0 && cur >= cap {
		return cur, false, nil
	}
	cur++
	counts[key] = cur
	if err := writeDailyCounts(c.path, counts); err != nil {
		return 0, false, err
	}
	return cur, true, nil
}

// DialRequest is one outbound-dial attempt. RequestID is the caller's
// idempotency key: a retry with the same (employee, target, request_id)
// returns the reserved result and never dials twice.
type DialRequest struct {
	EmployeeID string
	Target     string
	RequestID  string
}

// DialResult carries the new call and the channel that closes when the
// call reaches a terminal state (hangup, no-answer timeout, failure).
type DialResult struct {
	CallControlID string
	Done          <-chan struct{}
}

// DialConfig is the injected policy set for the dialer. TestBaseURL is a
// leaf-00T deviation shape: the HTTP origin refusal paths must never touch
// (gate tests point it at a zero-request server). Production dialing goes
// through the injected telnyx.CallController, never this field.
type DialConfig struct {
	Enabled         bool
	AllowedNumbers  []string
	PublicStreamURL string
	ConnectionID    string
	FromNumber      string
	// TimeoutSecs drives BOTH ring-time limits (audit 2026-11-06 M8):
	// the dialer's local no-answer timer AND the carrier-side
	// timeout_secs on the POST /calls body. Keeping the carrier limit on
	// the same config (voice.timeout_secs, default 30) means the two
	// races can never disagree about when a ring is dead; 0 falls back
	// to DefaultNoAnswerSecs on both sides.
	TimeoutSecs int
	// DailyDialCap is the operator-configured outbound-dial ceiling per
	// LOCAL calendar day (voice.daily_dial_cap). 0 = unlimited. It replaces
	// the old package-level DefaultDailyDialCap build constant.
	DailyDialCap int
	// Counter is the durable daily-dial counter. Nil degrades to an
	// in-process counter (legacy behavior for hand-built dialers); the
	// daemon passes a counter backed by $MEEPT_HOME/voice/daily_counters.json.
	Counter     *DailyCounter
	TestBaseURL string
	// Hold is the daemon-glue goal-hold seam (AcquireCallHold), the
	// outbound twin of LocalDeps.Hold. Invoked once after a successful
	// registry Acquire, before the no-answer timer, so ReleaseHook sees
	// the ccid. Nil degrades to no ended-state. The release itself rides
	// the manager release hook; this seam only acquires.
	Hold func(ccid, employeeID string)
}

// AuditWriter is the narrow audit seam (C9): one canonical row per dial
// attempt, written BEFORE DialOut so the attempt is attributable even if
// the POST fails. The daemon wiring adapts internal/auditlog.Store to this;
// internal/voice does not import auditlog.
type AuditWriter interface {
	WriteAudit(ctx context.Context, row map[string]any) error
}

// EffectsLedger is the narrow internal/effects seam (C9). Satisfied
// structurally by effects.MemoryLedger, effects.SQLiteLedger, and the
// gate-test capture wrapper. Claim records the employee claimant;
// later resolves must pass the same claimant.
type EffectsLedger interface {
	Claim(ctx context.Context, key string, meta effects.EffectMeta, who effects.Claimant) (bool, *effects.EffectRecord, error)
	// Get is the read-only lookup used by the pre-side-effect idempotency
	// check (issue #165 slice 3). MemoryLedger, SQLiteLedger and the gate
	// capture wrappers all satisfy it.
	Get(ctx context.Context, key string) (*effects.EffectRecord, error)
	RecordReceipt(ctx context.Context, key string, who effects.Claimant, receipt json.RawMessage) error
	Complete(ctx context.Context, key string, who effects.Claimant) error
	Abandon(ctx context.Context, key string, who effects.Claimant, reason string) error
}

// DialController is the dial-facing seam: telnyx.CallController plus
// DialOut (leaf-02 Client satisfies it; the countingController gate fake
// satisfies it structurally). The trailing timeoutSecs is the carrier-side
// timeout_secs resolved from voice.timeout_secs (audit 2026-11-06 M8).
type DialController interface {
	telnyx.CallController
	DialOut(ctx context.Context, to, from, connectionID string, timeoutSecs int) (string, error)
}

// compile-time proof that the leaf-02 client satisfies the dial seam.
var _ DialController = (*telnyx.Client)(nil)

// dialReceipt is the effect receipt: the reserved call_control_id a retry
// of the same request_id must return.
type dialReceipt struct {
	CallControlID string `json:"call_control_id"`
}

// DefaultNoAnswerSecs is the ring-time limit applied when config leaves
// TimeoutSecs unset: it matches the telnyx default timeout_secs (leaf-02
// dial request body) so the local timer and the carrier limit agree. It is
// exported because the telnyx DialOut call site consumes the same value for
// the carrier-side timeout_secs (audit 2026-11-06 M8 - the carrier field
// was previously hardcoded, ignoring voice.timeout_secs).
const DefaultNoAnswerSecs = 30

// NoAnswerSecs resolves the effective ring timeout: the configured
// TimeoutSecs when positive, else DefaultNoAnswerSecs. Both the dialer's
// no-answer timer and the DialOut timeout_secs ride this single resolution
// so the two limits can never disagree.
func (d *Dialer) NoAnswerSecs() int {
	if d.cfg.TimeoutSecs > 0 {
		return d.cfg.TimeoutSecs
	}
	return DefaultNoAnswerSecs
}

// Dialer places outbound calls under the C9 order. All fields are
// immutable after construction; mutable state (no-answer timers, per-call
// done channels) is mutex-guarded.
type Dialer struct {
	cfg    DialConfig
	tel    DialController
	mgr    *Manager
	reg    Registry
	fx     EffectsLedger
	audit  AuditWriter
	logger *slog.Logger
	// hold is DialConfig.Hold, copied at construction. Immutable after
	// NewDialer returns; nil is a no-op (degraded stack).
	hold func(ccid, employeeID string)

	mu       sync.Mutex
	timers   map[string]*time.Timer   // ccid -> no-answer timer (stopped on answered)
	answered map[string]bool          // ccid -> answer notification landed (bughunt 2026-11-07 voice F-2)
	dones    map[string]chan struct{} // ccid -> terminal channel (closed once)
	fin      map[string]bool          // ccid -> terminal cleanup already ran

	// Durable daily-dial counter (issue #165 follow-up). Nil when the cap
	// is 0 -> Dial never touches it (byte-identical legacy path). The
	// counter serializes its own read-modify-write; Dialer.mu is NOT held
	// across that I/O (root C4: no I/O under a mutex).
	counter *DailyCounter
}

// NewDialer returns a Dialer. mgr may be nil (registry-only tests, and the
// wiring leaf before the manager exists on the path); when non-nil, the
// no-answer timer reports FailOutcome(ccid, OutcomeNoAnswer) and terminal
// closure is observed through Manager.WaitCall. Non-nil mgr also WIRES the
// answered notification (audit 2026-11-06 H1): the dialer registers its
// NotifyAnswered as the manager's call.answered hook, so an outbound call
// picked up before timer expiry keeps its slot instead of eating a late
// no-answer release. The dialer owns its timer; the manager owns the
// webhook seam - connecting them at dialer construction keeps the whole
// contract inside the voice package.
func NewDialer(cfg DialConfig, tel DialController, mgr *Manager, reg Registry, fx EffectsLedger, audit AuditWriter, logger *slog.Logger) *Dialer {
	if logger == nil {
		logger = slog.Default()
	}
	d := &Dialer{
		cfg:      cfg,
		tel:      tel,
		mgr:      mgr,
		reg:      reg,
		fx:       fx,
		audit:    audit,
		logger:   logger,
		hold:     cfg.Hold,
		timers:   make(map[string]*time.Timer),
		answered: make(map[string]bool),
		dones:    make(map[string]chan struct{}),
		fin:      make(map[string]bool),
	}
	// Inert when the cap is 0: no counter is built and no counter I/O
	// happens on any dial path (byte-identical legacy behavior).
	if cfg.DailyDialCap > 0 {
		d.counter = cfg.Counter
		if d.counter == nil {
			d.counter = NewDailyCounter("") // in-memory fallback for hand-built dialers
		}
	}
	if mgr != nil {
		mgr.SetAnsweredHook(d.NotifyAnswered)
	}
	return d
}

// Dial places one outbound call under the pinned order:
//
//  1. enabled gate
//  2. audit row (direction outgoing, target, employee, request id) -
//     written before reserve and POST so the attempt is attributable even
//     if the POST fails
//  3. effects Claim keyed (employee_id, target, request_id) - a completed
//     prior short-circuits here with the reserved result (retry never
//     rings twice)
//  5. POST /calls via tel.DialOut
//  6. TrackOutbound + registry Acquire
//  7. receipt + Complete, then the no-answer timer
//
// Wait - steps 4 and 3: the gate block sits after the claim so the
// idempotent retry path short-circuits before any refusal; gate refusals
// abandon the claim (no leaked reservation). The allowlist/cap gate
// (1b) and the first duplicate-target check run BEFORE steps 2-3 so no
// refused dial leaves an audit row or effect (issue #165 slice 3).
func (d *Dialer) Dial(ctx context.Context, req DialRequest) (DialResult, error) {
	// -- 1. enabled gate --
	if !d.cfg.Enabled {
		return DialResult{}, ErrVoiceDisabled
	}
	target := NormalizeE164(req.Target)
	if req.EmployeeID == "" || req.RequestID == "" {
		return DialResult{}, fmt.Errorf("voice: dial: employee_id and request_id are required")
	}

	// -- 1b. OUTBOUND RECIPIENT GATE (issue #165 slice 3): the allowlist
	// and per-day dial cap fire BEFORE ANY side effect - no audit row, no
	// new effect reservation, no Telnyx POST. The sibling SendSMS2 path
	// pins the same ordering. An idempotent retry of an already-completed
	// effect short-circuits below BEFORE this gate (the ring already
	// happened; a re-check with a since-changed allowlist must not turn a
	// completed retry into a refusal). Empty allowlist keeps the historical
	// fail-closed posture (a dial target must be listed) so existing
	// deployments with no allowlist keep refusing, matching the C9 gate
	// tests; the operator-configurable "empty = all" dial posture is a
	// follow-up and must be decided together with the config owner.
	key := effects.EffectKey("voice.dial_out", req.EmployeeID, target, req.RequestID)
	if prior, err := d.fx.Get(ctx, key); err == nil && prior != nil && prior.State == effects.StateCompleted {
		return d.reservedResult(ctx, prior)
	}
	// Duplicate-live-target precedes the allowlist (frozen TestDialRefusals
	// seeds a live target that is NOT in cfg.AllowedNumbers and expects
	// ErrDuplicateTarget). It is a read-only registry check - zero side
	// effects - so it belongs in the pre-effect gate block and keeps that
	// pinned precedence. A completed retry above already short-circuited,
	// so its own live target never trips this.
	for _, inst := range d.reg.Live() {
		if NormalizeE164(inst.Target) == target {
			return DialResult{}, fmt.Errorf("%w: %s", ErrDuplicateTarget, target)
		}
	}
	if err := d.outboundPolicyGate(target); err != nil {
		return DialResult{}, err
	}

	// -- 2. audit row BEFORE reserve and POST (C9: the attempt is
	// attributable even if the POST fails). --
	if d.audit != nil {
		row := map[string]any{
			"direction":   "outgoing",
			"target":      target,
			"employee_id": req.EmployeeID,
			"request_id":  req.RequestID,
		}
		if err := d.audit.WriteAudit(ctx, row); err != nil {
			return DialResult{}, fmt.Errorf("voice: dial: audit: %w", err)
		}
	}

	// -- 3. effects reserve (C9: keyed (employee_id, target, request_id),
	// reserved BEFORE the POST; retry never rings twice). The completed-
	// prior short-circuit ran BEFORE the audit row and the recipient gate
	// (step 1b) so a legitimate retry of a live call is not refused by a
	// since-changed allowlist. Gate refusals after this point abandon the
	// claim so no reservation leaks. --
	payload, err := json.Marshal(map[string]string{
		"employee_id": req.EmployeeID,
		"target":      target,
		"request_id":  req.RequestID,
	})
	if err != nil {
		return DialResult{}, fmt.Errorf("voice: dial: encode effect payload: %w", err)
	}
	who := effects.Claimant{AgentID: req.EmployeeID}
	granted, prior, err := d.fx.Claim(ctx, key, effects.EffectMeta{
		Tool:      "voice.dial_out",
		SessionID: req.EmployeeID,
		Payload:   payload,
	}, who)
	if err != nil {
		return DialResult{}, fmt.Errorf("voice: dial: effect claim: %w", err)
	}
	if !granted {
		return d.reservedResult(ctx, prior)
	}

	// -- 4. remaining policy + registry gates (still zero HTTP): one live
	// call per target globally (ErrDuplicateTarget), public URL,
	// connection id, capacity, one live call per employee. The allowlist
	// and per-day cap were already enforced in step 1b, BEFORE the audit
	// row and effect reservation (issue #165 slice 3). Targets are
	// compared through NormalizeE164 (audit 2026-11-06 M7). --
	for _, inst := range d.reg.Live() {
		if NormalizeE164(inst.Target) == target {
			gateErr := fmt.Errorf("%w: %s", ErrDuplicateTarget, target)
			d.abandonEffect(ctx, key, who, gateErr)
			return DialResult{}, gateErr
		}
	}
	if d.cfg.PublicStreamURL == "" {
		d.abandonEffect(ctx, key, who, ErrNoPublicURL)
		return DialResult{}, ErrNoPublicURL
	}
	if d.cfg.ConnectionID == "" {
		d.abandonEffect(ctx, key, who, ErrNoConnectionID)
		return DialResult{}, ErrNoConnectionID
	}
	if gateErr := d.registryGate(target, req.EmployeeID); gateErr != nil {
		d.abandonEffect(ctx, key, who, gateErr)
		return DialResult{}, gateErr
	}

	// -- 5. POST /calls (timeout_secs rides the resolved voice.timeout_secs
	// so the carrier-side ring limit matches the local no-answer timer;
	// audit 2026-11-06 M8) --
	ccid, err := d.tel.DialOut(ctx, target, d.cfg.FromNumber, d.cfg.ConnectionID, d.NoAnswerSecs())
	if err != nil || ccid == "" {
		if err == nil {
			err = errors.New("telnyx dial out returned an empty call_control_id")
		}
		d.abandonEffect(ctx, key, who, err)
		return DialResult{}, fmt.Errorf("voice: dial: %w", err)
	}

	// -- 6. TrackOutbound + registry Acquire --
	if d.mgr != nil {
		if err := d.mgr.TrackOutbound(ccid); err != nil {
			d.abandonEffect(ctx, key, who, err)
			return DialResult{}, fmt.Errorf("voice: dial: track outbound: %w", err)
		}
	}
	done := make(chan struct{})
	d.mu.Lock()
	d.dones[ccid] = done
	d.mu.Unlock()
	if _, err := d.reg.Acquire(ccid, req.EmployeeID, target); err != nil {
		// Lost a race between the gate and the acquire: never leave the
		// leg ringing untracked - hang up, abandon, release.
		d.abandonEffect(ctx, key, who, err)
		d.finishCall(ccid)
		if hErr := d.tel.HangupCall(ctx, ccid); hErr != nil {
			d.logger.Error("voice: dial: hangup after acquire failure", "ccid", ccid, "err", hErr)
		}
		// Audit 2026-11-09 finding 2 (C-V1): TrackOutbound succeeded above,
		// so the manager holds m.outbound/m.done entries for this ccid.
		// Without a terminal FailOutcome they leak forever and the
		// release fan-out (goal unpark, WaitCall close, effects) never
		// fires. C-V1 pins the order: FailOutcome AFTER finishCall,
		// mirroring signalleg abandonOutbound (release, then the terminal
		// outcome). releaseOnce keeps this exactly-once: a carrier hangup
		// webhook arriving later for the dead leg is a no-op. Same shape
		// as the local-leg spawn/audio failure teardown (local.go).
		if d.mgr != nil {
			d.mgr.FailOutcome(ccid, OutcomeFailed, fmt.Sprintf("registry acquire: %v", err))
		}
		return DialResult{}, fmt.Errorf("voice: dial: registry acquire: %w", err)
	}
	// Goal hold once the slot is live, before the no-answer timer can
	// fire a terminal outcome. Refusals and the idempotent retry path
	// return above this line and must not hold. Release rides the
	// manager hook; do not release here.
	d.holdCall(ccid, req.EmployeeID)

	// -- 7. receipt + Complete: the irreversible effect (one ring) happened.
	// A later retry of this request_id returns this ccid without dialing. --
	receipt, err := json.Marshal(dialReceipt{CallControlID: ccid})
	if err == nil {
		err = d.fx.RecordReceipt(ctx, key, who, receipt)
	}
	if err == nil {
		err = d.fx.Complete(ctx, key, who)
	}
	if err != nil {
		// The ring happened; the ledger row is stuck but the call is live.
		// Surface the error, keep the call lifecycle running (timer + wait).
		d.logger.Error("voice: dial: effect receipt/complete", "key", key, "err", err)
	}

	d.armNoAnswerTimer(ccid)
	d.logger.Info("voice: outbound dial placed", "ccid", ccid, "target", target, "employee_id", req.EmployeeID)
	return DialResult{CallControlID: ccid, Done: done}, nil
}

// outboundPolicyGate is the pre-side-effect recipient gate for an outbound
// dial (issue #165 slice 3). It runs BEFORE the audit row, the effect
// reservation, and the Telnyx POST, so a refusal leaves zero trace on any
// external surface. It is intentionally a pure read (except the in-process
// daily counter, advanced only AFTER all gates pass). The two gates:
//
//  1. allowlist: every cfg.AllowedNumbers entry is normalized through
//     NormalizeE164 before comparison; the target must match one entry.
//     The list is deliberately fail-closed: an empty/nil list permits NO
//     target, matching the frozen C9 gate contract (TestDialRefusals pins
//     "target not allowed" for a nil list). Reconciling that with the
//     voice.allowed_numbers `null = all` config comment is a product
//     decision owned by the config side of issue #165 and requires the
//     pinned test to be updated by its owner - this slice does not change
//     the default posture, only the ORDER in which the existing gate runs.
//  2. per-day cap: operator-configured via VoiceConfig.DailyDialCap with a
//     DURABLE counter (survives a daemon restart). Dials that pass all
//     gates increment the counter; refusals do not.
func (d *Dialer) outboundPolicyGate(target string) error {
	allowed := false
	for _, n := range d.cfg.AllowedNumbers {
		if NormalizeE164(n) == target {
			allowed = true
			break
		}
	}
	if !allowed {
		return fmt.Errorf("%w: %q", ErrNumberNotAllowed, target)
	}
	if d.cfg.DailyDialCap > 0 && d.counter != nil {
		day := time.Now().Format("2006-01-02") // operator local day
		count, admitted, err := d.counter.TryInc(day, dialCounterScope, d.cfg.DailyDialCap)
		if err != nil {
			return fmt.Errorf("voice: dial: daily cap counter: %w", err)
		}
		if !admitted {
			return fmt.Errorf("%w: %d/%d today", ErrDailyCapReached, count, d.cfg.DailyDialCap)
		}
	}
	return nil
}

// dialCounterScope is the (kind, scope) key of the single global outbound
// dial counter: one ceiling per daemon per local day, matching the
// "voice.daily_dial_cap = outbound dials per day" operator contract. A
// per-employee scope would need a different config shape and is not this
// knob. The `kind` half keeps it distinct from any future SMS counter in
// a shared file.
const dialCounterScope = "voice.dial_out"

// reservedResult builds the retry answer from a prior ledger record.
// completed + receipt -> the reserved ccid, zero additional POSTs.
// claimed/receipted -> owned by another in-flight caller. abandoned -> the
// prior attempt died before ringing; the request_id is burned and the
// caller must issue a new one (this keeps "never rings twice" absolute).
func (d *Dialer) reservedResult(ctx context.Context, prior *effects.EffectRecord) (DialResult, error) {
	if prior == nil {
		return DialResult{}, fmt.Errorf("voice: dial: effect denied without prior record")
	}
	switch prior.State {
	case effects.StateCompleted:
		var r dialReceipt
		if err := json.Unmarshal(prior.Receipt, &r); err != nil || r.CallControlID == "" {
			return DialResult{}, fmt.Errorf("voice: dial: completed effect has no usable receipt")
		}
		return DialResult{CallControlID: r.CallControlID, Done: d.doneFor(ctx, r.CallControlID)}, nil
	case effects.StateClaimed, effects.StateReceipted:
		return DialResult{}, fmt.Errorf("voice: dial: request_id already in flight (%s)", prior.State)
	default: // abandoned
		return DialResult{}, fmt.Errorf("voice: dial: prior attempt for request_id was abandoned (%s); use a new request_id", prior.AbandonReason)
	}
}

// doneFor returns the live terminal channel for a ccid: the dialer's own
// channel when this process dialed it, else Manager.WaitCall, else a
// channel that never closes (state unknown after a restart).
func (d *Dialer) doneFor(ctx context.Context, ccid string) <-chan struct{} {
	d.mu.Lock()
	if ch, ok := d.dones[ccid]; ok {
		d.mu.Unlock()
		return ch
	}
	d.mu.Unlock()
	if d.mgr != nil {
		return d.mgr.WaitCall(ccid)
	}
	// Unknown-after-teardown with no manager seam (receipted-but-never-
	// tracked ccid, e.g. restart-mid-ring residual, finding 8): return a
	// CLOSED channel + warn instead of a fresh never-closing one — the
	// dial state is unknowable, and a silent hang punts the unknown to
	// the caller's ctx deadline. The mgr-backed path keeps the M3 posture
	// (audit 2026-11-06): unknown inbound ids never close.
	closed := make(chan struct{})
	close(closed)
	d.logger.Warn("voice: dial: done-state unknown after restart for ccid (bounded ctx is the caller's bound)", "ccid", ccid)
	return closed
}

// holdCall acquires the goal hold for a live outbound call when the seam
// is wired. Nil seam is a no-op (degraded stack: the row then never
// reaches ended). Called once per successful Acquire, matching
// LocalLeg.holdCall.
func (d *Dialer) holdCall(ccid, employeeID string) {
	if d.hold == nil || ccid == "" {
		return
	}
	d.hold(ccid, employeeID)
}

// registryGate enforces the C8 capacity and uniqueness rules against the
// live snapshot: cap, one live call per employee (ErrBusy), one live call
// per target globally (ErrDuplicateTarget). Live targets are compared
// through NormalizeE164 (audit 2026-11-06 M7) so a registry entry acquired
// in a non-canonical shape (a raw webhook `from`, a legacy row) still
// collides with its canonical dial-target twin - the duplicate-target rule
// is by number, not by string.
func (d *Dialer) registryGate(target, employeeID string) error {
	live := d.reg.Live()
	// Same sentinel as CallRegistry.Acquire: cap <= 0 means unlimited
	// (voice.max_concurrent_calls 0 = unlimited per config docs). The
	// pre-gate must not refuse every dial while Acquire would accept it
	// (bughunt 2026-11-06 voice F-3).
	if cap := d.registryCap(); cap > 0 && len(live) >= cap {
		return fmt.Errorf("%w: at capacity (%d)", ErrBusy, len(live))
	}
	for _, inst := range live {
		if NormalizeE164(inst.Target) == target {
			return fmt.Errorf("%w: %s", ErrDuplicateTarget, target)
		}
	}
	for _, inst := range live {
		if inst.EmployeeID == employeeID {
			return fmt.Errorf("%w: employee %s already has a live call", ErrBusy, employeeID)
		}
	}
	return nil
}

// registryCap returns the configured cap. Like CallRegistry.Acquire, a cap
// of 0 (or negative) means UNLIMITED - voice.max_concurrent_calls documents
// "0 = unlimited" (template config/meept.json5), so the gate semantics must
// match Acquire's sentinel or a cap-0 config refuses dials that Acquire
// would accept.
func (d *Dialer) registryCap() int { return d.reg.Cap() }

// abandonEffect marks a claimed effect dead (refused/failed dial: the ring
// never happened), so ReconcilePending finds no leaked reservation.
func (d *Dialer) abandonEffect(ctx context.Context, key string, who effects.Claimant, cause error) {
	if err := d.fx.Abandon(ctx, key, who, fmt.Sprintf("dial refused or failed: %v", cause)); err != nil {
		d.logger.Error("voice: dial: abandon effect", "key", key, "err", err)
	}
}

// armNoAnswerTimer starts the timeout_secs timer. On fire: FailOutcome with
// OutcomeNoAnswer through the manager (which fires its release hook and
// closes the WaitCall channel), then finishCall releases the registry slot
// and closes the dial result's Done channel. NotifyAnswered stops the
// timer when the call is picked up, so no late Fail lands on a live call.
func (d *Dialer) armNoAnswerTimer(ccid string) {
	secs := d.NoAnswerSecs()
	timer := time.NewTimer(time.Duration(secs) * time.Second)
	d.mu.Lock()
	// bughunt 2026-11-07 voice F-2: if the answer webhook landed before
	// this arm site (webhook beats DialOut's line-339 path), the call is
	// already answered — arming a no-answer timer would be wrong; skip it.
	if d.answered[ccid] {
		d.mu.Unlock()
		timer.Stop()
		return
	}
	d.timers[ccid] = timer
	d.mu.Unlock()

	go func() {
		var mgrDone <-chan struct{}
		if d.mgr != nil {
			mgrDone = d.mgr.WaitCall(ccid)
		}
		select {
		case <-timer.C:
			// bughunt 2026-11-07 voice F-2: the fire may win the race
			// against NotifyAnswered's timer.Stop (Stop returns false
			// once the value is buffered). Re-check answered at fire
			// time: an answered call keeps running, its no-answer never
			// lands. Do NOT return here — the call still needs terminal
			// bookkeeping: wait the manager's done channel (the hangup /
			// release signal) so finishCall still frees the registry
			// slot and closes the dial Done channel exactly once.
			//
			// audit 2026-11-09 finding 9 (C-V1 leaf 01): the re-check and
			// the FailOutcome fire are ONE atomic step under d.mu. The
			// old shape unlocked between check and fire, so a
			// NotifyAnswered landing in that window marked the call
			// answered while the already-past-check teardown completed
			// anyway — a no_answer outcome on a call that answered
			// before its teardown finished. NotifyAnswered now parks on
			// d.mu until the fire (or the refusal) is decided. Lock
			// order is one-way, dialer mu -> manager mu: the manager
			// never calls back into the dialer while holding m.mu
			// (notifyAnswered and fireRelease both run outside it).
			d.mu.Lock()
			answered := d.answered[ccid]
			if !answered && d.mgr != nil {
				d.mgr.FailOutcome(ccid, OutcomeNoAnswer, fmt.Sprintf("no answer within %ds", secs))
			}
			d.mu.Unlock()
			if answered {
				d.logger.Debug("voice: no-answer timer fired for answered call (ignored)", "ccid", ccid)
				if mgrDone != nil {
					// Real call lifecycle: hold the slot until the
					// manager's terminal event, then clean up once.
					<-mgrDone
					d.finishCall(ccid)
				}
				// mgr == nil (registry-only dialer): no lifecycle to
				// observe; leave the slot to the caller's own release,
				// never tear down an answered call.
				return
			}
			d.finishCall(ccid)
			d.logger.Info("voice: outbound dial no-answer", "ccid", ccid, "timeout_secs", secs)
		case <-mgrDone:
			// Hangup or manager-side failure: cancel the timer so it can
			// never fire a no-answer on a finished call.
			timer.Stop()
			d.finishCall(ccid)
		}
		d.mu.Lock()
		delete(d.timers, ccid)
		d.mu.Unlock()
	}()
}

// NotifyAnswered cancels the no-answer timer for ccid (call.answered on
// the outbound leg). The webhook wiring calls this on every answered event
// for a tracked outbound id. Unknown ccid is a no-op.
//
// bughunt 2026-11-07 voice F-2: Timer.Stop CANNOT un-arm a fire already in
// flight — when the expiry tick and the webhook race, the fire wins the
// select and FailOutcome(OutcomeNoAnswer) tears down a call that was
// answered in the same instant (3bf5431c M8 deliberately aligned the local
// timer and the carrier timeout_secs, which maximizes that simultaneity).
// The answered map is the second cancellation channel: NotifyAnswered
// marks the ccid answered (even before armNoAnswerTimer runs — the webhook
// can beat DialOut's arm site), and the fire branch refuses to fail an
// answered call.
func (d *Dialer) NotifyAnswered(ccid string) {
	d.mu.Lock()
	// A call already terminal-cleaned (finishCall ran) must not regain an
	// answered marker after finishCall's delete — that would leak the
	// entry for the ccid's whole daemon life.
	if !d.fin[ccid] {
		d.answered[ccid] = true
	}
	timer := d.timers[ccid]
	d.mu.Unlock()
	if timer != nil {
		timer.Stop()
	}
}

// finishCall runs the terminal cleanup exactly once per ccid: release the
// registry slot and close the Done channel (late observers see closure).
func (d *Dialer) finishCall(ccid string) {
	d.mu.Lock()
	if d.fin[ccid] {
		d.mu.Unlock()
		return
	}
	d.fin[ccid] = true
	ch := d.dones[ccid]
	delete(d.dones, ccid)
	delete(d.timers, ccid)
	// bughunt 2026-11-07 voice F-2: the answered marker is per-call
	// bookkeeping; drop it at terminal cleanup so the map cannot grow
	// across the daemon's dial history.
	delete(d.answered, ccid)
	d.mu.Unlock()

	d.reg.Release(ccid)
	if ch != nil {
		close(ch)
	}
}

// ---- C8 voice instance registry ----
//
// The C8 interface shape pinned in master.md, with the concrete registry
// the gate tests construct. Leaf 14 (registry wiring) re-homes this into
// the daemon components; the interface is the dependency seam every other
// consumer codes against.

// Leg values for Instance.Leg (CL6). The zero value of Leg is the phone
// leg, so existing Instance composite literals stay valid untouched.
// LegSignal is the Signal voice leg (C4): targets look like
// "signal:<uuid>" and NormalizeE164 NEVER runs on them (AcquireLeg
// normalizes only leg "phone", the same rule local:<label> rides).
const (
	LegPhone  = "phone"
	LegLocal  = "local"
	LegSignal = "signal"
)

// Instance is one live voice instance: the registry's unit of capacity.
// Leg is which leg the instance rides ("phone" or "local"; CL6). The zero
// value of Leg means "phone" so existing composite literals stay valid.
//
// Caller carries the resolved inbound-caller identity (caller-authz plan,
// frozen contract): stamped at the inbound answer site, read on every turn
// context. The zero Info is legal — "voice turn, caller unresolved" is a
// distinct state the enforcement layer gates as unapproved (decision O2),
// never a crash and never a prompt-rendered value (redaction is issue #126).
type Instance struct {
	CallControlID string
	EmployeeID    string
	Target        string
	Leg           string
	Caller        callerid.Info
}

// CallerResolver is the answer-time identity seam (caller-authz leaf 02):
// given the inbound leg number (E.164 on the phone leg), it resolves the
// caller's identity. The daemon implements it over the contact store
// (contacts.db is the system of record, decision O2); internal/voice never
// imports internal/contact. Fail-soft contract: a nil resolver or any
// error degrades to callerid.Info{Number: e164} with a WARN — answering
// is NEVER gated (decision O1), so no call path may block on or fail
// because of identity resolution.
type CallerResolver interface {
	ResolveCaller(ctx context.Context, e164 string) (callerid.Info, error)
}

// Registry tracks live calls: N concurrent calls need N slots
// (voice.max_concurrent_calls), one live call per employee, and one live
// call per target number globally.
type Registry interface {
	// Acquire registers a live instance on the PHONE leg (CL6
	// compatibility shape; the concrete CallRegistry wraps AcquireLeg).
	// ErrBusy over cap or when the employee already has a live call;
	// ErrDuplicateTarget when a live call to the same target already
	// exists.
	Acquire(ccid, employeeID, target string) (Instance, error)
	// AcquireLeg registers a live instance with an explicit leg (CL6:
	// "phone" or "local"; the empty string means "phone"). The same cap,
	// employee, and target rules apply across legs; the target is
	// normalized ONLY for the phone leg.
	AcquireLeg(ccid, employeeID, target, leg string) (Instance, error)
	// Release frees the slot and the target. Unknown ccid is a no-op.
	Release(ccid string)
	// Live reports the live instances.
	Live() []Instance
	// Cap is the configured instance capacity.
	Cap() int
}

// CallRegistry is the mutex-guarded Registry implementation.
type CallRegistry struct {
	cap int

	mu   sync.Mutex
	live map[string]Instance
}

// NewRegistry returns a CallRegistry with the given capacity. A cap of 0
// (or negative) means UNLIMITED - the same sentinel Acquire applies.
func NewRegistry(cap int) *CallRegistry {
	return &CallRegistry{cap: cap, live: make(map[string]Instance)}
}

// Acquire registers a live instance under the C8 rules on the PHONE leg
// (CL6 compatibility wrapper: the Registry interface keeps the 3-arg shape
// so existing implementors and test fakes do not all break at once). The
// target is normalized at this boundary (audit 2026-11-06 M7): every
// registry entry carries the same canonical E.164 shape, so direct Acquire
// callers (the inbound spawn seam passes a raw webhook `from`) and the
// dialer's pre-gate snapshot agree by construction instead of by each
// caller remembering to normalize.
func (r *CallRegistry) Acquire(ccid, employeeID, target string) (Instance, error) {
	return r.AcquireLeg(ccid, employeeID, target, LegPhone)
}

// AcquireLeg registers a live instance under the C8 rules with an explicit
// leg (CL6: "phone" or "local"; the empty string means "phone"). The target
// is normalized ONLY for the phone leg: local targets look like
// "local:<label>" and NormalizeE164 must never mangle them. For phone legs
// the same boundary normalization as Acquire applies (audit 2026-11-06
// M7). The cap, one-live-per-employee, and one-live-per-target rules are
// shared across legs: one employee, one conversation at a time, whatever
// the leg.
func (r *CallRegistry) AcquireLeg(ccid, employeeID, target, leg string) (Instance, error) {
	if leg == "" {
		leg = LegPhone
	}
	if leg == LegPhone {
		target = NormalizeE164(target)
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	// cap <= 0 means unlimited (the spec's max_concurrent_calls is an
	// optional cap; zero config value must not make every Acquire busy).
	if r.cap > 0 && len(r.live) >= r.cap {
		return Instance{}, fmt.Errorf("%w: %d/%d slots live", ErrBusy, len(r.live), r.cap)
	}
	for _, inst := range r.live {
		if inst.Target == target {
			return Instance{}, fmt.Errorf("%w: %s", ErrDuplicateTarget, target)
		}
	}
	for _, inst := range r.live {
		if inst.EmployeeID == employeeID {
			return Instance{}, fmt.Errorf("%w: employee %s already live", ErrBusy, employeeID)
		}
	}
	inst := Instance{CallControlID: ccid, EmployeeID: employeeID, Target: target, Leg: leg}
	r.live[ccid] = inst
	return inst, nil
}

// Release frees the slot and target. Unknown ccid is a no-op.
func (r *CallRegistry) Release(ccid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, ccid)
}

// SetCaller attaches the resolved inbound-caller identity (caller-authz
// leaf 02) to a live instance. The answer site Acquires first and resolves
// identity AFTER that (resolution is fail-soft and must never race the
// slot), so the identity rides the registry row the setup router reads.
// Unknown ccid is a no-op — a released call has no loop to stamp.
func (r *CallRegistry) SetCaller(ccid string, caller callerid.Info) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if inst, ok := r.live[ccid]; ok {
		inst.Caller = caller
		r.live[ccid] = inst
	}
}

// ResolveCallerOrFailSoft runs the resolver seam for one inbound answer and
// enforces the fail-soft contract in ONE place: nil resolver, a resolver
// error, or an empty resolution all degrade to callerid.Info{Number: e164}
// (unknown = unapproved, decision O2) with a WARN. It never returns an
// error — answering is never gated (decision O1), so the answer site can
// call it inline before holdPhoneCall without any failure branch.
func ResolveCallerOrFailSoft(ctx context.Context, res CallerResolver, e164 string, logger *slog.Logger) callerid.Info {
	if logger == nil {
		logger = slog.Default()
	}
	info := callerid.Info{Number: e164}
	if res == nil {
		logger.Warn("voice: caller resolver not wired; caller fail-soft (unapproved)", "number_present", e164 != "")
		return info
	}
	resolved, err := res.ResolveCaller(ctx, e164)
	if err != nil {
		logger.Warn("voice: caller resolution failed; caller fail-soft (unapproved)",
			"number_present", e164 != "", "err", err)
		return info
	}
	// Number always carries the leg number even if the resolver left it
	// blank (the gate's audit row keys on it).
	if resolved.Number == "" {
		resolved.Number = e164
	}
	return resolved
}

// Live snapshots the live instances. Each Instance carries its Leg
// ("phone" or "local"; CL6) so telemetry and gates can tell the legs
// apart; the cap and uniqueness rules are shared across legs.
func (r *CallRegistry) Live() []Instance {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]Instance, 0, len(r.live))
	for _, inst := range r.live {
		out = append(out, inst)
	}
	return out
}

// Cap is the configured instance capacity.
func (r *CallRegistry) Cap() int { return r.cap }

// ---- E.164 normalization ----

// NormalizeE164 normalizes a phone number for allowlist comparison.
//
// Pinned rule (leaf 11 T6): bare 10-digit numbers get the US default
// country code (+1); an 11-digit number starting with 1 gets a leading +;
// anything already starting with + is kept as-is after stripping spaces,
// dashes, parentheses, and dots; anything else is returned unchanged and
// simply never matches the allowlist (E.164 shapes only are dialable in
// practice). The same rule applies to allowlist entries, so an operator
// may write either form.
func NormalizeE164(number string) string {
	trimmed := strings.Map(func(r rune) rune {
		switch r {
		case ' ', '-', '(', ')', '.':
			return -1
		}
		return r
	}, strings.TrimSpace(number))
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "+") {
		return trimmed
	}
	digitsOnly := true
	for _, r := range trimmed {
		if r < '0' || r > '9' {
			digitsOnly = false
			break
		}
	}
	if !digitsOnly {
		return trimmed
	}
	switch len(trimmed) {
	case 10: // US default: bare 10-digit is a US number
		return "+1" + trimmed
	case 11: // 1NPANXXXXX with the trunk digit
		return "+" + trimmed
	default:
		return trimmed
	}
}
