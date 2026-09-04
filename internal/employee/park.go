package employee

// Goal-loop provider-wait parking (tree 03 leaf 03, DECISIONS.md D9).
//
// The GoalLoop's reflector LLM calls (ASSESS in Assess, REFLECT in
// reflectViaLLM) can surface llm.QuotaResetError,
// llm.ErrAllModelsQuotaBlocked, or llm.ThrottleBackoffError — provider
// waits that are the machine being patient, not loop failures. On a
// classified provider wait the EPISODE parks: the turn's re-entry data
// (trigger / plan / execution result, by phase) is snapshotted into a
// ParkedTurnRecord and handed to the daemon's ONE shared
// agent.TurnParker (leaf 01; wired via WithEpisodeParker). The parker's
// resume callback re-enters the parked phase when the resume time
// passes. Park returns true with NO error: the episode did not fail —
// it is waiting.
//
// Give-up semantics (D9): a wait whose schedule is unknown or lands at/
// after the BackoffPlan horizon (llm.DefaultBackoffPlan.GiveUpAt) is a
// give-up — the ORIGINAL error propagates byte-identically to the
// pre-existing failure handling (failure counters, HealthDecayFunc,
// auto-pause). The shared parker itself soft-stops over-MaxWait records
// at now+MaxWait (leaf 01 §4.5), so a slightly-under-horizon wait can
// wake early; a resumed episode that hits a fresh provider error simply
// re-parks with a fresh schedule.
//
// EPISODE-STATE DEVIATION (documented per the leaf): the goal loop has
// NO pre-existing paused/deferred episode mechanism — Assess/Reflect are
// one-shot calls on the scheduler/webhook Trigger path (manager.go
// Trigger → loop.Decide; scheduler_jobs.go runAssessForEmployee), and
// the only pause concept is whole-EMPLOYEE pause (PauseFunc), which is
// far too coarse for per-episode waits. The parked episode is therefore
// represented by the ParkedTurnRecord held in the shared parker (the
// documented new state), with goalTurnPayload as its re-entry snapshot.
// No parallel episode store and no new bus topics.
//
// The parker is memory-only (leaf 01): a daemon restart drops parked
// episodes; the periodic scheduler (ScheduleAssessJobs) naturally
// re-assesses those employees on its next tick.

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

// pausedRepollInterval is how long a paused employee's parked episode
// waits before re-checking the pause state (H6 resume gate). The episode
// is NOT dropped while the employee is paused — pause is not cancel — it
// re-parks on this cadence until the employee resumes (or the payload is
// superseded by a newer park via the DedupKey).
const pausedRepollInterval = 15 * time.Minute

// goalTurnPayload is the TurnPayload encoding for parked goal-loop
// episodes. Phase selects the resume re-entry point:
//   - "assess": re-dispatch Assess (tier-2 propose) from the trigger.
//   - "tier1": re-run decideTier1 (assess + execute + reflect) from the
//     trigger.
//   - "tier3": re-run decideTier3 from the trigger.
//   - "approve": re-run ApproveAndExecute on the approved plan.
//   - "reflect": re-run Reflect on the finished execution. Counter drift
//     of at most one failure/success step is accepted; the health
//     verdict and its persistence converge on the resumed call.
type goalTurnPayload struct {
	Phase   string                  `json:"phase"`
	Trigger *TriggerEvent           `json:"trigger,omitempty"`
	Plan    *PlanRef                `json:"plan,omitempty"`
	Result  *bot.BotExecutionResult `json:"result,omitempty"`
}

// EpisodeParker parks goal-loop episodes on provider waits via the
// daemon's shared agent.TurnParker and re-dispatches them on resume. It
// is embedded in each GoalLoop via WithEpisodeParker; a nil parker means
// parking is disabled and provider waits keep their legacy error
// behaviour.
//
// policy is the SAME failure-policy config the LLM clients use (the
// daemon wires cfg.LLM.FailurePolicy onto llm.FailurePolicyConfig); the
// schedule comes from llm.DefaultBackoffPlan (quota base includes the
// D5 402 extra) with the server-provided reset/RetryAt winning when
// later, never past the horizon (D8).
type EpisodeParker struct {
	turns  *agent.TurnParker
	policy llm.FailurePolicyConfig
	logger *slog.Logger

	// H4 dedup (bughunt 2026-09-04): employee+phase+trigger-identity →
	// scheduled resume time for currently-parked episodes. Guarded by
	// dedupMu; entries are added on park and removed when the shared
	// parker drains the record (RemoveParkDedup callback below) or the
	// park is refused.
	dedupMu        sync.Mutex
	parkedEpisodes map[string]time.Time
}

// NewEpisodeParker wires a GoalLoop-facing parker over the shared
// TurnParker. turns may be nil (parking disabled; Park is a no-op and
// provider waits propagate as before).
func NewEpisodeParker(turns *agent.TurnParker, policy llm.FailurePolicyConfig, logger *slog.Logger) *EpisodeParker {
	if logger == nil {
		logger = slog.Default()
	}
	return &EpisodeParker{
		turns:          turns,
		policy:         policy,
		logger:         logger,
		parkedEpisodes: make(map[string]time.Time),
	}
}

// SetTurnParker installs (or replaces) the underlying shared parker.
// Used by daemon wiring when the parker is constructed after the loops.
func (p *EpisodeParker) SetTurnParker(turns *agent.TurnParker) {
	if p == nil {
		return
	}
	p.turns = turns
}

// WithEpisodeParker installs provider-wait parking on this loop. A nil
// parker is ignored (parking disabled = legacy error behaviour). All
// employee loops must share the SAME EpisodeParker/TurnParker instance
// (the leaf's no-second-TurnParker rule).
func (l *GoalLoop) WithEpisodeParker(parker *EpisodeParker) *GoalLoop {
	if parker != nil {
		l.parker = parker
	}
	return l
}

// providerWaitSchedule resolves (resumeAt, giveUp) for a classified
// provider-wait error at the given now:
//   - QuotaResetError: the server reset (ResetAt, or derived from
//     RetryAfter) wins; a wait at/after the quota plan horizon is a
//     give-up (D8/D9). An unknown or already-passed reset cannot be
//     waited on — give-up so the caller surfaces the error.
//   - ThrottleBackoffError: RetryAt (already plan-scheduled by the
//     client's short loop, D8) is honored as-is, still capped by a
//     fresh plan's horizon; zero RetryAt is a give-up.
//   - ErrAllModelsQuotaBlocked: no server reset is attached, so the
//     schedule is the quota plan's first step (D5 base); past the
//     horizon → give-up.
func (p *EpisodeParker) providerWaitSchedule(err error, now time.Time) (time.Time, bool) {
	var quotaErr *llm.QuotaResetError
	if errors.As(err, &quotaErr) {
		resetAt := quotaErr.ResetAt
		if resetAt.IsZero() && quotaErr.RetryAfter > 0 {
			resetAt = now.Add(quotaErr.RetryAfter)
		}
		if resetAt.IsZero() || !resetAt.After(now) {
			return time.Time{}, true
		}
		plan := llm.DefaultBackoffPlan(llm.FailureQuota, now, p.policy)
		if resetAt.After(plan.GiveUpAt) {
			return time.Time{}, true
		}
		return resetAt, false
	}

	if throttleErr, ok := llm.AsThrottleBackoffError(err); ok {
		if throttleErr.RetryAt.IsZero() {
			return time.Time{}, true
		}
		plan := llm.DefaultBackoffPlan(llm.FailureThrottle, now, p.policy)
		if throttleErr.RetryAt.After(plan.GiveUpAt) {
			return time.Time{}, true
		}
		return throttleErr.RetryAt, false
	}

	if errors.Is(err, llm.ErrAllModelsQuotaBlocked) {
		plan := llm.DefaultBackoffPlan(llm.FailureQuota, now, p.policy)
		if plan.ShouldGiveUp(now) {
			return time.Time{}, true
		}
		return plan.NextAttempt(now, 0, time.Time{}), false
	}

	return time.Time{}, true
}

// park snapshots the episode payload and hands it to the shared parker.
// Returns false when parking is disabled, the payload cannot be encoded,
// or the shared parker refuses the record (zero/past resume time — the
// caller then propagates the original error).
func (p *EpisodeParker) park(employeeID, sessionID string, class llm.FailureClass, resumeAt time.Time, payload goalTurnPayload) bool {
	if p == nil || p.turns == nil {
		return false
	}
	now := time.Now().UTC()
	raw, err := json.Marshal(payload)
	if err != nil {
		p.logger.Warn("goal episode park: payload marshal failed — giving up",
			"employee_id", employeeID, "phase", payload.Phase, "error", err)
		return false
	}
	rec := agent.ParkedTurnRecord{
		ConversationID: sessionID,
		SessionID:      sessionID,
		AgentID:        employeeID,
		Class:          class,
		ResumeAt:       resumeAt,
		TurnPayload:    raw,
	}
	// H4 (bughunt 2026-09-04): episode-level dedup. While an episode is
	// parked, every scheduler tick re-fires the same trigger, hits the
	// same provider wait, and would park a fresh record — N ticks during
	// a 24h quota window ≈ N duplicate full executions at reset. The
	// parker's own queue is identity-agnostic (chat turns are
	// intentionally NOT deduped: two different user messages are two
	// turns), so episode identity is enforced HERE, in employee scope,
	// keyed on employee+phase+trigger-identity (FiredAt excluded —
	// scheduler ticks re-fire with fresh timestamps).
	dedupKey := employeeID + ":" + payload.Phase + ":" + triggerKey(payload.Trigger)
	p.dedupMu.Lock()
	if due, exists := p.parkedEpisodes[dedupKey]; exists && due.After(now) {
		p.dedupMu.Unlock()
		p.logger.Debug("goal episode already parked — dedup suppressed duplicate park",
			"employee_id", employeeID,
			"phase", payload.Phase,
			"resume_at", due.Format(time.RFC3339),
		)
		// The episode IS parked and scheduled; this tick's failure is
		// absorbed the same as a fresh park would (parked = no error).
		return true
	}
	p.parkedEpisodes[dedupKey] = resumeAt
	p.dedupMu.Unlock()
	parked := p.turns.Park(rec)
	if !parked {
		// Park refused → the record is not queued; clear the dedup claim
		// so a later tick can park fresh.
		p.dedupMu.Lock()
		delete(p.parkedEpisodes, dedupKey)
		p.dedupMu.Unlock()
		return false
	}
	// Park observability (leaf 04, D9): the parked episode surfaces on the
	// existing agent.quota_wait topic through the shared parker's event bus
	// — the goal loop needs no bus wiring of its own.
	p.turns.EmitParkEvent(rec, "", "")
	return true
}

// clearParkDedup removes an episode's dedup claim (drain time). Employee
// and phase come from the draining record; the trigger identity is
// normalized exactly as at park time (H4).
func (p *EpisodeParker) clearParkDedup(employeeID, phase string, trigger *TriggerEvent) {
	if p == nil {
		return
	}
	key := employeeID + ":" + phase + ":" + triggerKey(trigger)
	p.dedupMu.Lock()
	delete(p.parkedEpisodes, key)
	p.dedupMu.Unlock()
}

// maybeParkProviderWait classifies err as a provider wait and, on a
// schedule within the horizon, parks the episode (phase snapshot) and
// returns (true, nil) — no error surfaces. Give-up conditions return
// (false, err) so the ORIGINAL error propagates byte-identically to the
// pre-existing failure handling. A non-provider-wait error returns
// (false, err) unchanged.
//
// parker is set once at wiring time (WithEpisodeParker) and only read
// here, so no loop-mutex handoff is needed (CLAUDE.md mutex-scope rule).
func (l *GoalLoop) maybeParkProviderWait(err error, payload goalTurnPayload) (bool, error) {
	parker := l.parker
	if parker == nil {
		return false, err
	}
	now := time.Now().UTC()

	var class llm.FailureClass
	_, throttleShaped := llm.AsThrottleBackoffError(err)
	switch {
	case llm.IsQuotaResetError(err) || errors.Is(err, llm.ErrAllModelsQuotaBlocked):
		class = llm.FailureQuota
	case throttleShaped:
		class = llm.FailureThrottle
	default:
		return false, err // not a provider wait
	}

	resumeAt, giveUp := parker.providerWaitSchedule(err, now)
	if giveUp {
		return false, err
	}
	if !parker.park(l.employeeID, l.employeeID, class, resumeAt, payload) {
		return false, err
	}
	parker.logger.Info("goal episode parked on provider wait",
		"employee_id", l.employeeID,
		"class", class,
		"phase", payload.Phase,
		"resume_at", resumeAt.Format(time.RFC3339),
	)
	return true, nil
}

// ResumeGoalEpisode re-enters a parked episode's phase. Daemon wiring
// registers this — via the Manager's registered GoalLoop for the parked
// employee — as the shared TurnParker's resume callback, so ONE callback
// serves every loop and there is exactly one TurnParker per daemon.
func (l *GoalLoop) ResumeGoalEpisode(ctx context.Context, rec agent.ParkedTurnRecord) {
	if l == nil || l.parker == nil {
		return
	}
	var p goalTurnPayload
	if err := json.Unmarshal(rec.TurnPayload, &p); err != nil {
		l.logger.Error("parked goal episode payload undecodable — dropping",
			"employee_id", l.employeeID, "class", rec.Class, "error", err)
		return
	}
	l.logger.Info("resuming parked goal episode",
		"employee_id", l.employeeID,
		"class", rec.Class,
		"phase", p.Phase,
	)
	// H4 (bughunt 2026-09-04): the episode is draining — clear the dedup
	// claim recorded at park time so a post-resume provider wait parks a
	// FRESH record instead of being suppressed by the stale one.
	l.parker.clearParkDedup(l.employeeID, p.Phase, p.Trigger)
	// Resume observability (leaf 04, D9): symmetric to the park event, via
	// the shared parker's event bus on agent.quota_wait.
	l.parker.turns.EmitResumeEvent(rec)

	// H6 (bughunt 2026-09-04): resume must honor the operator's pause —
	// the normal Trigger path rejects paused employees (manager.go Trigger
	// → GetBot → Enabled check, "employee is paused"), so the resume
	// callback must not bypass that gate. A paused employee's episode is
	// re-parked at the repoll cadence instead of dropped (pause is not
	// cancel); it resumes when the operator lifts the pause.
	if l.statusFn != nil && strings.EqualFold(l.statusFn(), "paused") {
		repoll := time.Now().UTC().Add(pausedRepollInterval)
		rec.ResumeAt = repoll
		if l.parker.turns.Park(rec) {
			// Re-arm the H4 dedup claim for the repoll window so scheduler
			// ticks during the pause cannot park duplicates.
			l.parker.dedupMu.Lock()
			l.parker.parkedEpisodes[l.employeeID+":"+p.Phase+":"+triggerKey(p.Trigger)] = repoll
			l.parker.dedupMu.Unlock()
			l.logger.Info("resumed goal episode deferred: employee is paused — re-parked",
				"employee_id", l.employeeID,
				"phase", p.Phase,
				"repoll_at", repoll.Format(time.RFC3339),
			)
			return
		}
		// The parker refused (e.g. zero/past resume time); fall through
		// and execute rather than silently losing the episode.
		l.logger.Warn("resumed goal episode: paused re-park refused — executing anyway",
			"employee_id", l.employeeID)
	}

	switch p.Phase {
	case "assess":
		// H5 (bughunt 2026-09-04): tier-2 parks during Assess with phase
		// "assess", but tier-2's Assess only PROPOSES — decideTier2
		// (Assess → Plan → pending_approval) is the completion of the
		// interrupted propose step. The old resume called l.Assess and
		// DISCARDED the candidates, so tier-2 parked episodes silently
		// produced nothing. decideTier2 carries the plan cap and
		// approval-routing logic; calling it here re-enters the exact
		// phase that was interrupted. Tier-1/3 phases unchanged (their
		// decide* functions already run the full assess+execute cycle).
		if err := l.decideTier2(ctx, normalizedTrigger(p.Trigger), l.logger); err != nil {
			l.logger.Warn("resumed goal episode tier2 assess failed",
				"employee_id", l.employeeID, "error", err)
		}
	case "tier1":
		if err := l.decideTier1(ctx, normalizedTrigger(p.Trigger), l.logger); err != nil {
			l.logger.Warn("resumed goal episode tier1 failed",
				"employee_id", l.employeeID, "error", err)
		}
	case "tier3":
		if err := l.decideTier3(ctx, normalizedTrigger(p.Trigger), l.logger); err != nil {
			l.logger.Warn("resumed goal episode tier3 failed",
				"employee_id", l.employeeID, "error", err)
		}
	case "approve":
		if p.Plan == nil {
			l.logger.Warn("parked goal episode missing plan — dropping",
				"employee_id", l.employeeID)
			return
		}
		l.ApproveAndExecute(ctx, *p.Plan)
	case "reflect":
		if p.Plan == nil || p.Result == nil {
			l.logger.Warn("parked goal episode reflect missing plan/result — dropping",
				"employee_id", l.employeeID)
			return
		}
		// Re-running Reflect re-advances the consecutive-failure/success
		// counters by one step for this turn (accepted drift); the health
		// verdict and its persistence converge on the resumed call. A
		// fresh provider wait here simply re-parks.
		if _, err := l.Reflect(ctx, *p.Plan, p.Result); err != nil {
			l.logger.Warn("resumed goal episode reflect failed",
				"employee_id", l.employeeID, "error", err)
		}
	default:
		l.logger.Warn("parked goal episode has unknown phase — dropping",
			"employee_id", l.employeeID, "phase", p.Phase)
	}
}

// normalizedTrigger restores a decoded trigger, backfilling the fired
// timestamp lost to a zero-value JSON round-trip.
func normalizedTrigger(t *TriggerEvent) TriggerEvent {
	if t == nil {
		return TriggerEvent{Source: "parked_resume", FiredAt: time.Now().UTC()}
	}
	if t.FiredAt.IsZero() {
		t.FiredAt = time.Now().UTC()
	}
	return *t
}

// triggerKey is the dedup component for the parked-episode key (H4): the
// semantic identity of the trigger, IGNORING FiredAt. Scheduler ticks
// re-fire the same assess trigger with fresh timestamps; without this
// normalization every tick would look like a distinct episode and N
// ticks during a quota window would park N duplicate executions.
func triggerKey(t *TriggerEvent) string {
	if t == nil {
		return "nil"
	}
	return t.Source + "|" + t.Topic
}
