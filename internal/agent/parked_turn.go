package agent

// TurnParker is the class-agnostic generalization of QuotaResumeWatcher
// (tree 03 leaf 01, master Contract 1 / SHARED-CONVENTIONS §4.5): it parks
// ParkedTurnRecords of any llm.FailureClass and resumes them (oldest-first,
// sequentially) once their resume time passes, via the caller-supplied
// callback. QuotaResumeWatcher (quota_resume.go) is a thin delegating
// wrapper over one TurnParker, byte-identical for class=FailureQuota.
//
// Scheduling mirrors the quota watcher exactly:
//   - Park refuses zero/past resume times and over-MaxWait waits return
//     false at the wrapper layer; the general Park soft-stops a record
//     whose ResumeAt is beyond MaxWait by scheduling it at now+MaxWait
//     (min(ResumeAt, now+MaxWait), §4.5) and returning true. Consumers
//     that must refuse instead (quota) pre-check before delegating.
//   - maxWait <= 0 falls back to llm.DefaultQuotaMaxWait (same default
//     source as the quota watcher, now per-parker).
//
// The parker is memory-only: records parked when Stop runs are dropped
// (logged at warn) and are not persisted across restarts — mirroring
// QuotaResumeWatcher, which holds no persistence either (quota blocks are
// in-memory, so a restart re-probes providers anyway).

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/models"
)

// DefaultTurnParkerPollInterval is the re-check cadence for parked turn
// records. It is the canonical source for DefaultQuotaResumePollInterval
// (quota_resume.go), which remains the exported quota-facing alias.
const DefaultTurnParkerPollInterval = 10 * time.Minute

// ParkedTurnRecord is the class-agnostic parked-turn record (frozen;
// SHARED-CONVENTIONS §4.5 / master Contract 1). NAME GUARD: the type is
// ParkedTurnRecord, NOT ParkedTurn — package agent already declares
// ParkedTurn in budget_resume.go (budget watcher); a second ParkedTurn
// would be a duplicate-type compile error.
//
// TurnPayload carries the class-specific original request as JSON so the
// resume router (tree 03 leaves 02/03) can re-run the turn without
// reconstructing history. For class=FailureQuota the encoding is the
// quota watcher's stored fields (see quota_resume.go quotaTurnPayload);
// class=FailureThrottle's encoding is frozen by tree 03 leaf 02.
type ParkedTurnRecord struct {
	ConversationID string
	SessionID      string
	AgentID        string
	Class          llm.FailureClass // quota | throttle (server classes only)
	ResumeAt       time.Time
	Attempt        int
	MaxAttempts    int
	TurnPayload    json.RawMessage // class-specific original request
}

// TurnParker parks ParkedTurnRecords and retries them once their resume
// time passes. It polls on a fixed interval; when the earliest resume time
// among parked records has passed, it drains the queue (oldest first,
// sequentially — a slow resume delays later resumes rather than reordering
// them) by invoking the resume callback.
//
// MaxWait soft-stop: records whose remaining wait exceeds MaxWait are
// parked anyway but scheduled at now+MaxWait (min(ResumeAt, now+MaxWait)).
// Consumers that must refuse instead of rescheduling (chat quota) check
// the wait before calling Park.
//
// A panicking resume callback is recovered, logged, and its record
// dropped — the watcher keeps serving later records. The parker is
// best-effort: records parked when Stop runs are dropped (logged at warn)
// and are not persisted across restarts.
type TurnParker struct {
	logger       *slog.Logger
	pollInterval time.Duration
	maxWait      time.Duration

	// parkEventBus, when set, receives the park/resume/give-up lifecycle
	// events (leaf 04 Task 1) on the EXISTING agent.quota_wait topic —
	// D9 observability without a new topic or WS prefix. Bus is published
	// to outside the mutex; installed once at wiring time.
	parkEventBus parkEventBus

	mu         sync.Mutex
	parked     []ParkedTurnRecord
	resumeFunc func(ctx context.Context, turn ParkedTurnRecord)

	cancel   context.CancelFunc
	wg       sync.WaitGroup // polling + resume goroutines (Stop barrier)
	resumeWG sync.WaitGroup // resume goroutines only (drainDue barrier)

	// nowFunc overrides time.Now for scheduling decisions (test seam;
	// nil means time.Now). Set before Start, read without the mutex —
	// same regime as pollInterval/maxWait on the original watcher.
	nowFunc func() time.Time
	// testDrainGate, when non-nil, is consulted at the top of each
	// drainDue pass so tests can step the injected clock between drains
	// deterministically. It must not block indefinitely. Production code
	// never sets it (nil = always proceed).
	testDrainGate func() bool
}

// NewTurnParker creates a parker. resume is invoked (in a background
// goroutine) for each parked record once its resume time passes; it is
// responsible for re-running the turn and routing the result. A nil resume
// disables auto-resume (Park becomes a no-op). maxWait <= 0 falls back to
// llm.DefaultQuotaMaxWait.
func NewTurnParker(logger *slog.Logger, resume func(context.Context, ParkedTurnRecord), maxWait time.Duration) *TurnParker {
	if logger == nil {
		logger = slog.Default()
	}
	if maxWait <= 0 {
		maxWait = llm.DefaultQuotaMaxWait
	}
	return &TurnParker{
		logger:       logger,
		pollInterval: DefaultTurnParkerPollInterval,
		maxWait:      maxWait,
		resumeFunc:   resume,
	}
}

// now resolves the injected (or real) clock.
func (p *TurnParker) now() time.Time {
	if p.nowFunc != nil {
		return p.nowFunc()
	}
	return time.Now()
}

// SetPollInterval overrides the re-check cadence (config plumbing seam).
// Must be called before Start.
func (p *TurnParker) SetPollInterval(d time.Duration) {
	if p == nil || d <= 0 {
		return
	}
	p.pollInterval = d
}

// SetResumeFunc swaps the resume callback. Only intended for wiring setups
// that build a parker before the resume router exists (tree 03 leaf 02: the
// loop-installed class dispatcher replaces the constructor callback). Safe
// before Start; after Start it takes effect from the next drainDue pass.
func (p *TurnParker) SetResumeFunc(resume func(context.Context, ParkedTurnRecord)) {
	if p == nil || resume == nil {
		return
	}
	p.mu.Lock()
	p.resumeFunc = resume
	p.mu.Unlock()
}

// Start begins the background polling loop. Safe to call once; subsequent
// calls are no-ops.
func (p *TurnParker) Start(ctx context.Context) {
	if p == nil || p.resumeFunc == nil {
		return
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.mu.Unlock()
		return // already started
	}
	runCtx, cancel := context.WithCancel(ctx)
	p.cancel = cancel
	p.mu.Unlock()

	p.wg.Add(1)
	go func() {
		defer p.wg.Done()
		ticker := time.NewTicker(p.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				p.drainDue(runCtx)
			}
		}
	}()
	p.logger.Info("turn parker started",
		"poll_interval", p.pollInterval,
		"max_wait", p.maxWait,
	)
}

// Stop halts the polling loop and waits for it to exit. Records that have
// not yet resumed are logged and dropped.
func (p *TurnParker) Stop() {
	if p == nil {
		return
	}
	p.mu.Lock()
	if p.cancel != nil {
		p.cancel()
		p.cancel = nil
	}
	remaining := len(p.parked)
	p.mu.Unlock()

	p.wg.Wait()
	if remaining > 0 {
		p.logger.Warn("turn parker stopped with parked records dropped", "dropped", remaining)
	}
}

// Park enqueues a record for later retry once its resume time passes.
// Returns false (and does nothing) when:
//   - auto-resume is not configured, or
//   - the resume time is unknown/zero (cannot schedule), or
//   - the resume time has already passed (already due; caller should
//     retry directly).
//
// A record whose wait exceeds MaxWait is soft-stopped rather than
// refused: it is parked, scheduled at now+MaxWait
// (min(ResumeAt, now+MaxWait), SHARED-CONVENTIONS §4.5), and Park returns
// true. Consumers that must refuse over-MaxWait waits (chat quota)
// pre-check before delegating.
func (p *TurnParker) Park(turn ParkedTurnRecord) bool {
	if p == nil || p.resumeFunc == nil {
		return false
	}
	if turn.ResumeAt.IsZero() {
		return false
	}
	now := p.now()
	wait := turn.ResumeAt.Sub(now)
	if wait <= 0 {
		return false // already due; caller should retry directly
	}
	scheduledAt := turn.ResumeAt
	if wait > p.maxWait {
		// MaxWait soft-stop: cap the wait at now+maxWait.
		scheduledAt = now.Add(p.maxWait)
		p.logger.Info("park wait exceeds max_wait — scheduling at max_wait",
			"class", turn.Class,
			"wait", wait,
			"max_wait", p.maxWait,
		)
	}
	turn.ResumeAt = scheduledAt
	p.mu.Lock()
	p.parked = append(p.parked, turn)
	n := len(p.parked)
	// Keep the queue ordered by resume time so drainDue can pop from the
	// front without scanning (typical case: homogeneous resume times).
	for i := n - 1; i > 0; i-- {
		if p.parked[i].ResumeAt.Before(p.parked[i-1].ResumeAt) {
			p.parked[i], p.parked[i-1] = p.parked[i-1], p.parked[i]
		} else {
			break
		}
	}
	p.mu.Unlock()
	p.logger.Info("parked turn pending resume",
		"class", turn.Class,
		"session_id", turn.SessionID,
		"resume_at", turn.ResumeAt.Format(time.RFC3339),
		"queued", n,
	)
	return true
}

// Pending returns the number of currently parked records.
func (p *TurnParker) Pending() int {
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.parked)
}

// Next returns the earliest resume time among parked records of the given
// failure class (leaf 04's per-class surface), and whether any exist.
func (p *TurnParker) Next(class llm.FailureClass) (time.Time, bool) {
	if p == nil {
		return time.Time{}, false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	var earliest time.Time
	for _, turn := range p.parked {
		if turn.Class != class {
			continue
		}
		if earliest.IsZero() || turn.ResumeAt.Before(earliest) {
			earliest = turn.ResumeAt
		}
	}
	return earliest, !earliest.IsZero()
}

// ParkWaitInfo is one failure class's parked-turn summary for the status
// surfaces (tree 03 leaf 04 Task 1): Next is the earliest scheduled resume
// among that class's parked records and Pending is how many are parked.
type ParkWaitInfo struct {
	Class   llm.FailureClass
	Next    time.Time
	Pending int
}

// WaitInfo returns one ParkWaitInfo per failure class that currently has
// parked records, sorted by Next (earliest resume first) so surfaces render
// deterministically. An empty/nil parker returns nil. Collected under the
// parker mutex; callers operate on the returned slice after release
// (mutexio convention). Mirrors Next/Pending (leaf 01) — this is the ONE
// query the TUI/GUI quota_wait labels consume.
func (p *TurnParker) WaitInfo() []ParkWaitInfo {
	if p == nil {
		return nil
	}
	p.mu.Lock()
	byClass := make(map[llm.FailureClass]ParkWaitInfo)
	for _, turn := range p.parked {
		info, ok := byClass[turn.Class]
		if !ok {
			byClass[turn.Class] = ParkWaitInfo{Class: turn.Class, Next: turn.ResumeAt, Pending: 1}
			continue
		}
		if turn.ResumeAt.Before(info.Next) {
			info.Next = turn.ResumeAt
		}
		info.Pending++
		byClass[turn.Class] = info
	}
	p.mu.Unlock()

	if len(byClass) == 0 {
		return nil
	}
	infos := make([]ParkWaitInfo, 0, len(byClass))
	for _, info := range byClass {
		infos = append(infos, info)
	}
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Next.Before(infos[j].Next)
	})
	return infos
}

// drainDue resumes every parked record whose resume time has passed,
// oldest-first, sequentially: a slow resume delays later resumes rather
// than reordering them. Called from the polling loop; its cadence bounds
// the delay. Records whose time has not passed stay parked.
func (p *TurnParker) drainDue(ctx context.Context) {
	if gate := p.testDrainGate; gate != nil && !gate() {
		return
	}
	p.mu.Lock()
	resume := p.resumeFunc
	now := p.now()
	dueIdx := 0
	for dueIdx < len(p.parked) && !p.parked[dueIdx].ResumeAt.After(now) {
		dueIdx++
	}
	if dueIdx == 0 {
		p.mu.Unlock()
		return
	}
	due := p.parked[:dueIdx]
	remaining := make([]ParkedTurnRecord, len(p.parked)-dueIdx)
	copy(remaining, p.parked[dueIdx:])
	p.parked = remaining
	p.mu.Unlock()
	p.logger.Info("resume window passed — resuming parked records",
		"class", due[0].Class,
		"count", len(due),
	)
	// Resume sequentially: the doc contract (and the ordering test) is
	// oldest-first, which parallel goroutine launches cannot guarantee.
	// Each resume runs synchronously; a slow resume delays later resumes
	// rather than reordering them. resumeWG is retained for Stop()'s
	// barrier and is already complete when this returns. A panicking
	// callback is recovered and its record dropped so one bad resume
	// cannot take down the watcher (or the daemon).
	for _, turn := range due {
		p.wg.Add(1)
		p.resumeWG.Add(1)
		func() {
			defer p.wg.Done()
			defer p.resumeWG.Done()
			defer func() {
				if r := recover(); r != nil {
					p.logger.Error("resume callback panicked — record dropped",
						"class", turn.Class,
						"session_id", turn.SessionID,
						"panic", r,
					)
				}
			}()
			resume(ctx, turn)
		}()
	}
	// Wait for the resumed records so tests can observe the resume
	// synchronously. resumeWG is disjoint from the polling goroutine's wg
	// (waiting on wg here would deadlock with Stop).
	p.resumeWG.Wait()
}

// --- Park/resume/give-up lifecycle events (tree 03 leaf 04 Task 1, D9) ------

// ParkTurnEvent is the bus payload for parked-turn lifecycle events. It
// rides the EXISTING "agent.quota_wait" topic — the episode tracker's topic
// — with a class-carrying payload, per the leaf contract: zero new topics,
// so the WS "agent.quota" prefix match keeps classifying every park event
// as agent_progress (AGENTS.md WS classification invariant). Fields mirror
// QuotaEvent (quota_episode.go) where they overlap so the TUI/Flutter quota
// handlers consume both event shapes through one code path: agent_id, to,
// reason, model_id, provider_id. The class key is the wire vocabulary of
// parkClassString ("quota"|"throttle"); resume_at is RFC3339; waited is a
// Go duration string (a display-input value, never formatted client-side
// into wall-clock claims).
type ParkTurnEvent struct {
	AgentID    string    `json:"agent_id"`
	To         string    `json:"to,omitempty"`
	Reason     string    `json:"reason"`
	Class      string    `json:"class,omitempty"`
	ResumeAt   string    `json:"resume_at,omitempty"` // RFC3339
	Waited     string    `json:"waited,omitempty"`    // Go duration string
	SessionID  string    `json:"session_id,omitempty"`
	ModelID    string    `json:"model_id,omitempty"`
	ProviderID string    `json:"provider_id,omitempty"`
	Timestamp  time.Time `json:"timestamp"`
}

// parkEventBus is the published-to interface subset of *bus.MessageBus the
// parker needs (bus.Publish(topic, msg)). A concrete field would import
// internal/bus here; the tests install a real *bus.MessageBus.
type parkEventBus interface {
	Publish(topic string, msg *models.BusMessage) int
}

// Park lifecycle reason strings (leaf contract): parked on a provider wait
// (reason follows the class — "quota_wait" | "throttle_wait"), resumed into
// the loop, or abandoned past MaxWait.
const (
	ReasonQuotaWait       = "quota_wait"       // park (class=quota)
	ReasonThrottleWait    = "throttle_wait"    // park (class=throttle)
	ReasonThrottleResumed = "throttle_resumed" // resume (class=throttle)
	ReasonThrottleGiveUp  = "throttle_give_up" // give-up past MaxWait
)

// parkClassString is the wire vocabulary for a FailureClass on park events
// (the lowercase class names used across the resilience forest). FailureNone
// and server_error/fatal records are never parked, so only the two parkable
// classes are mapped; anything else is "" (key omitted on the wire).
func parkClassString(class llm.FailureClass) string {
	switch class {
	case llm.FailureQuota:
		return "quota"
	case llm.FailureThrottle:
		return "throttle"
	default:
		return ""
	}
}

// SetParkEventBus installs the bus park/resume/give-up events publish to on
// the EXISTING agent.quota_wait topic (leaf 04 Task 1). Nil disables
// publishing (the default; parking stays silent). Daemon wiring calls this
// once at construction, before Start, so no lock is needed afterwards
// (same regime as pollInterval/maxWait).
func (p *TurnParker) SetParkEventBus(b parkEventBus) {
	if p == nil || b == nil {
		return
	}
	p.parkEventBus = b
}

// publishParkEvent marshals ev and publishes it on the agent.quota_wait
// topic when a park event bus is wired. Best-effort like every parker side
// channel: a marshal failure is logged at warn and never affects parking.
// Nil-receiver safe: the emit* callers run at park/resume/give-up sites that
// may run with an unwired parker.
func (p *TurnParker) publishParkEvent(ev ParkTurnEvent) {
	if p == nil || p.parkEventBus == nil {
		return
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, ev.AgentID, ev)
	if err != nil {
		p.logger.Warn("park event marshal failed — event dropped",
			"reason", ev.Reason,
			"error", err,
		)
		return
	}
	msg.Topic = "agent.quota_wait"
	p.parkEventBus.Publish("agent.quota_wait", msg)
}

// emitParkEvent publishes the parked event for rec; the reason follows the
// class ("quota_wait" | "throttle_wait" per the leaf contract). modelID/
// providerID ride from the park call site (they live in the class payload,
// not the frozen ParkedTurnRecord) so surfaces can show which model is
// waiting (D9).
func (p *TurnParker) emitParkEvent(rec ParkedTurnRecord, modelID, providerID string) {
	reason := ReasonQuotaWait
	if rec.Class == llm.FailureThrottle {
		reason = ReasonThrottleWait
	}
	p.publishParkEvent(ParkTurnEvent{
		AgentID:    rec.AgentID,
		To:         "quota_wait",
		Reason:     reason,
		Class:      parkClassString(rec.Class),
		ResumeAt:   rec.ResumeAt.Format(time.RFC3339),
		SessionID:  rec.SessionID,
		ModelID:    modelID,
		ProviderID: providerID,
		Timestamp:  p.now(),
	})
}

// emitResumeEvent publishes the resumed event for rec with the time it
// actually waited (now - ResumeAt).
func (p *TurnParker) emitResumeEvent(rec ParkedTurnRecord) {
	p.publishParkEvent(ParkTurnEvent{
		AgentID:   rec.AgentID,
		To:        "running",
		Reason:    ReasonThrottleResumed,
		Class:     parkClassString(rec.Class),
		Waited:    p.now().Sub(rec.ResumeAt).String(),
		SessionID: rec.SessionID,
		Timestamp: p.now(),
	})
}

// emitGiveUpEvent publishes the give-up event for an abandoned wait (the D8
// ThrottleGiveUpError surface). To stays empty: a give-up is a failure
// surface, not a parked-state change.
func (p *TurnParker) emitGiveUpEvent(agentID, modelID, providerID string, waited time.Duration) {
	p.publishParkEvent(ParkTurnEvent{
		AgentID:    agentID,
		Reason:     ReasonThrottleGiveUp,
		Class:      parkClassString(llm.FailureThrottle),
		Waited:     waited.String(),
		ModelID:    modelID,
		ProviderID: providerID,
		Timestamp:  p.now(),
	})
}

// EmitParkEvent is the exported park-event hook for park sites outside the
// agent package (the employee goal-loop parker shares this TurnParker; leaf
// 04 wires its emission through the same agent.quota_wait topic, D9).
// modelID/providerID may be empty when the park site has no model context.
func (p *TurnParker) EmitParkEvent(rec ParkedTurnRecord, modelID, providerID string) {
	p.emitParkEvent(rec, modelID, providerID)
}

// EmitResumeEvent is the exported resume-event hook, symmetric to
// EmitParkEvent: call it from external resume sites right before the turn
// re-enters its loop.
func (p *TurnParker) EmitResumeEvent(rec ParkedTurnRecord) {
	p.emitResumeEvent(rec)
}
