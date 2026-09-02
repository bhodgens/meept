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
	"time"

	"github.com/caimlas/meept/internal/agent"
	"github.com/caimlas/meept/internal/bot"
	"github.com/caimlas/meept/internal/llm"
)

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
}

// NewEpisodeParker wires a GoalLoop-facing parker over the shared
// TurnParker. turns may be nil (parking disabled; Park is a no-op and
// provider waits propagate as before).
func NewEpisodeParker(turns *agent.TurnParker, policy llm.FailurePolicyConfig, logger *slog.Logger) *EpisodeParker {
	if logger == nil {
		logger = slog.Default()
	}
	return &EpisodeParker{turns: turns, policy: policy, logger: logger}
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
	raw, err := json.Marshal(payload)
	if err != nil {
		p.logger.Warn("goal episode park: payload marshal failed — giving up",
			"employee_id", employeeID, "phase", payload.Phase, "error", err)
		return false
	}
	return p.turns.Park(agent.ParkedTurnRecord{
		ConversationID: sessionID,
		SessionID:      sessionID,
		AgentID:        employeeID,
		Class:          class,
		ResumeAt:       resumeAt,
		TurnPayload:    raw,
	})
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
	switch p.Phase {
	case "assess":
		if _, err := l.Assess(ctx, normalizedTrigger(p.Trigger)); err != nil {
			l.logger.Warn("resumed goal episode assess failed",
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
