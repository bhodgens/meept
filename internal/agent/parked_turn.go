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
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
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
