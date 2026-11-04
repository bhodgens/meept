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
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/effects"
	"github.com/caimlas/meept/internal/voice/telnyx"
)

// Typed refusal errors. Every policy gate returns one of these BEFORE any
// HTTP call, so the employee surfaces a named reason, never a bare
// "dial failed".
var (
	ErrVoiceDisabled    = errors.New("voice: dialing disabled (voice.enabled=false)")
	ErrNumberNotAllowed = errors.New("voice: target number not in voice.allowed_numbers")
	ErrNoPublicURL      = errors.New("voice: voice.public_stream_url not configured")
	ErrNoConnectionID   = errors.New("voice: voice.connection_id not configured")
	ErrBusy             = errors.New("voice: call registry busy (over capacity or employee already live)")
	ErrDuplicateTarget  = errors.New("voice: a live call to this target already exists")
)

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
	TimeoutSecs     int
	TestBaseURL     string
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
// gate-test capture wrapper.
type EffectsLedger interface {
	Claim(ctx context.Context, key string, meta effects.EffectMeta) (bool, *effects.EffectRecord, error)
	RecordReceipt(ctx context.Context, key string, receipt json.RawMessage) error
	Complete(ctx context.Context, key string) error
	Abandon(ctx context.Context, key string, reason string) error
}

// DialController is the dial-facing seam: telnyx.CallController plus
// DialOut (leaf-02 Client satisfies it; the countingController gate fake
// satisfies it structurally).
type DialController interface {
	telnyx.CallController
	DialOut(ctx context.Context, to, from, connectionID string) (string, error)
}

// compile-time proof that the leaf-02 client satisfies the dial seam.
var _ DialController = (*telnyx.Client)(nil)

// dialReceipt is the effect receipt: the reserved call_control_id a retry
// of the same request_id must return.
type dialReceipt struct {
	CallControlID string `json:"call_control_id"`
}

// defaultNoAnswerSecs matches telnyx timeout_secs default (leaf-02 dial
// request body) when config leaves TimeoutSecs unset.
const defaultNoAnswerSecs = 30

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

	mu     sync.Mutex
	timers map[string]*time.Timer   // ccid -> no-answer timer (stopped on answered)
	dones  map[string]chan struct{} // ccid -> terminal channel (closed once)
	fin    map[string]bool          // ccid -> terminal cleanup already ran
}

// NewDialer returns a Dialer. mgr may be nil (registry-only tests, and the
// wiring leaf before the manager exists on the path); when non-nil, the
// no-answer timer reports FailOutcome(ccid, OutcomeNoAnswer) and terminal
// closure is observed through Manager.WaitCall.
func NewDialer(cfg DialConfig, tel DialController, mgr *Manager, reg Registry, fx EffectsLedger, audit AuditWriter, logger *slog.Logger) *Dialer {
	if logger == nil {
		logger = slog.Default()
	}
	return &Dialer{
		cfg:    cfg,
		tel:    tel,
		mgr:    mgr,
		reg:    reg,
		fx:     fx,
		audit:  audit,
		logger: logger,
		timers: make(map[string]*time.Timer),
		dones:  make(map[string]chan struct{}),
		fin:    make(map[string]bool),
	}
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
//  4. remaining policy + registry gates (duplicate live target,
//     allowlist, public URL, connection id, capacity, one live call per
//     employee) - typed refusals, zero HTTP; each abandons the claim so
//     no reservation leaks
//  5. POST /calls via tel.DialOut
//  6. TrackOutbound + registry Acquire
//  7. receipt + Complete, then the no-answer timer
//
// Wait - steps 4 and 3: the gate block sits after the claim so the
// idempotent retry path short-circuits before any refusal; gate refusals
// abandon the claim (no leaked reservation).
func (d *Dialer) Dial(ctx context.Context, req DialRequest) (DialResult, error) {
	// -- 1. enabled gate --
	if !d.cfg.Enabled {
		return DialResult{}, ErrVoiceDisabled
	}
	target := NormalizeE164(req.Target)
	if req.EmployeeID == "" || req.RequestID == "" {
		return DialResult{}, fmt.Errorf("voice: dial: employee_id and request_id are required")
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
	// prior short-circuit runs BEFORE the duplicate-target and allowlist
	// gates: a legitimate retry of a live call must return the reserved
	// result, not a refusal. Gate refusals after this point abandon the
	// claim so no reservation leaks. --
	key := effects.EffectKey("voice.dial_out", req.EmployeeID, target, req.RequestID)
	payload, err := json.Marshal(map[string]string{
		"employee_id": req.EmployeeID,
		"target":      target,
		"request_id":  req.RequestID,
	})
	if err != nil {
		return DialResult{}, fmt.Errorf("voice: dial: encode effect payload: %w", err)
	}
	granted, prior, err := d.fx.Claim(ctx, key, effects.EffectMeta{
		Tool:      "voice.dial_out",
		SessionID: req.EmployeeID,
		Payload:   payload,
	})
	if err != nil {
		return DialResult{}, fmt.Errorf("voice: dial: effect claim: %w", err)
	}
	if !granted {
		return d.reservedResult(ctx, prior)
	}

	// -- 4. remaining policy + registry gates (still zero HTTP): one live
	// call per target globally (ErrDuplicateTarget, pinned BEFORE the
	// allowlist check by TestDialRefusals), allowlist, public URL,
	// connection id, capacity, one live call per employee. --
	for _, inst := range d.reg.Live() {
		if inst.Target == target {
			gateErr := fmt.Errorf("%w: %s", ErrDuplicateTarget, target)
			d.abandonEffect(ctx, key, gateErr)
			return DialResult{}, gateErr
		}
	}
	allowed := false
	for _, n := range d.cfg.AllowedNumbers {
		if NormalizeE164(n) == target {
			allowed = true
			break
		}
	}
	if !allowed {
		d.abandonEffect(ctx, key, ErrNumberNotAllowed)
		return DialResult{}, fmt.Errorf("%w: %q", ErrNumberNotAllowed, req.Target)
	}
	if d.cfg.PublicStreamURL == "" {
		d.abandonEffect(ctx, key, ErrNoPublicURL)
		return DialResult{}, ErrNoPublicURL
	}
	if d.cfg.ConnectionID == "" {
		d.abandonEffect(ctx, key, ErrNoConnectionID)
		return DialResult{}, ErrNoConnectionID
	}
	if gateErr := d.registryGate(target, req.EmployeeID); gateErr != nil {
		d.abandonEffect(ctx, key, gateErr)
		return DialResult{}, gateErr
	}

	// -- 5. POST /calls --
	ccid, err := d.tel.DialOut(ctx, target, d.cfg.FromNumber, d.cfg.ConnectionID)
	if err != nil || ccid == "" {
		if err == nil {
			err = errors.New("telnyx dial out returned an empty call_control_id")
		}
		d.abandonEffect(ctx, key, err)
		return DialResult{}, fmt.Errorf("voice: dial: %w", err)
	}

	// -- 6. TrackOutbound + registry Acquire --
	if d.mgr != nil {
		if err := d.mgr.TrackOutbound(ccid); err != nil {
			d.abandonEffect(ctx, key, err)
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
		d.abandonEffect(ctx, key, err)
		d.finishCall(ccid)
		if hErr := d.tel.HangupCall(ctx, ccid); hErr != nil {
			d.logger.Error("voice: dial: hangup after acquire failure", "ccid", ccid, "err", hErr)
		}
		return DialResult{}, fmt.Errorf("voice: dial: registry acquire: %w", err)
	}

	// -- 7. receipt + Complete: the irreversible effect (one ring) happened.
	// A later retry of this request_id returns this ccid without dialing. --
	receipt, err := json.Marshal(dialReceipt{CallControlID: ccid})
	if err == nil {
		err = d.fx.RecordReceipt(ctx, key, receipt)
	}
	if err == nil {
		err = d.fx.Complete(ctx, key)
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
	return make(chan struct{})
}

// registryGate enforces the C8 capacity and uniqueness rules against the
// live snapshot: cap, one live call per employee (ErrBusy), one live call
// per target globally (ErrDuplicateTarget).
func (d *Dialer) registryGate(target, employeeID string) error {
	live := d.reg.Live()
	if len(live) >= d.registryCap() {
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

// registryCap returns the configured cap; 0 means zero capacity (every
// Acquire is refused) - there is no "unlimited" sentinel.
func (d *Dialer) registryCap() int { return d.reg.Cap() }

// abandonEffect marks a claimed effect dead (refused/failed dial: the ring
// never happened), so ReconcilePending finds no leaked reservation.
func (d *Dialer) abandonEffect(ctx context.Context, key string, cause error) {
	if err := d.fx.Abandon(ctx, key, fmt.Sprintf("dial refused or failed: %v", cause)); err != nil {
		d.logger.Error("voice: dial: abandon effect", "key", key, "err", err)
	}
}

// armNoAnswerTimer starts the timeout_secs timer. On fire: FailOutcome with
// OutcomeNoAnswer through the manager (which fires its release hook and
// closes the WaitCall channel), then finishCall releases the registry slot
// and closes the dial result's Done channel. NotifyAnswered stops the
// timer when the call is picked up, so no late Fail lands on a live call.
func (d *Dialer) armNoAnswerTimer(ccid string) {
	secs := d.cfg.TimeoutSecs
	if secs <= 0 {
		secs = defaultNoAnswerSecs
	}
	timer := time.NewTimer(time.Duration(secs) * time.Second)
	d.mu.Lock()
	d.timers[ccid] = timer
	d.mu.Unlock()

	go func() {
		var mgrDone <-chan struct{}
		if d.mgr != nil {
			mgrDone = d.mgr.WaitCall(ccid)
		}
		select {
		case <-timer.C:
			if d.mgr != nil {
				d.mgr.FailOutcome(ccid, OutcomeNoAnswer, fmt.Sprintf("no answer within %ds", secs))
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
func (d *Dialer) NotifyAnswered(ccid string) {
	d.mu.Lock()
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

// Instance is one live voice instance: the registry's unit of capacity.
type Instance struct {
	CallControlID string
	EmployeeID    string
	Target        string
}

// Registry tracks live calls: N concurrent calls need N slots
// (voice.max_concurrent_calls), one live call per employee, and one live
// call per target number globally.
type Registry interface {
	// Acquire registers a live instance. ErrBusy over cap or when the
	// employee already has a live call; ErrDuplicateTarget when a live
	// call to the same target already exists.
	Acquire(ccid, employeeID, target string) (Instance, error)
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
// refuses every Acquire with ErrBusy.
func NewRegistry(cap int) *CallRegistry {
	return &CallRegistry{cap: cap, live: make(map[string]Instance)}
}

// Acquire registers a live instance under the C8 rules.
func (r *CallRegistry) Acquire(ccid, employeeID, target string) (Instance, error) {
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
	inst := Instance{CallControlID: ccid, EmployeeID: employeeID, Target: target}
	r.live[ccid] = inst
	return inst, nil
}

// Release frees the slot and target. Unknown ccid is a no-op.
func (r *CallRegistry) Release(ccid string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.live, ccid)
}

// Live snapshots the live instances.
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
