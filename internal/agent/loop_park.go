package agent

// Throttle parking (llm-resilience-forest tree 03 leaf 02, DECISIONS.md D4/D8):
// when the LLM call path surfaces a ThrottleBackoffError, the loop parks the
// turn on the shared TurnParker (parked_turn.go, leaf 01) instead of failing
// it. The resume time comes from the class BackoffPlan — plan.NextAttempt(now,
// err.Attempt, err.RetryAt) — so the exponential schedule grows with the
// attempt count and the server's Retry-At is honored when later. When the
// wait would exceed the parker's MaxWait, the D8 ThrottleGiveUpError surfaces
// and nothing is parked.
//
// Resume: the parker's callback routes by record class (quota → the chat
// handler's quota resume path; throttle → resumeThrottledTurn, which re-runs
// the ORIGINAL turn payload through RunOnceWithParts). A resumed turn that
// throttles again re-parks through the normal branch with a GROWN attempt:
// the resume path carries rec.Attempt+1 into the park math via
// WithThrottleParkAttempt (context-carried, stateless).
//
// D4 guard: throttle is load-shedding, NOT model death — this path never
// rotates models and never calls RecordAliasFailure.

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// throttledTurnPayload is the class=FailureThrottle TurnPayload encoding
// stored on a ParkedTurnRecord (SHARED-CONVENTIONS §4.5 freezes the throttle
// payload keys in leaf 02). Mirrors quotaTurnPayload (quota_resume.go) plus
// model_id/parked_at so the resume router can re-run the turn without
// reconstructing history.
type throttledTurnPayload struct {
	Message        string            `json:"message"`
	Parts          []llm.ContentPart `json:"parts,omitempty"`
	ConversationID string            `json:"conversation_id"`
	ProviderID     string            `json:"provider_id,omitempty"`
	ModelID        string            `json:"model_id,omitempty"`
	SourceClient   string            `json:"source_client,omitempty"`
	ParkedAt       time.Time         `json:"parked_at,omitempty"`
}

// throttleParkedTurn is the decoded form of a class=FailureThrottle record.
type throttleParkedTurn struct {
	Message        string
	Parts          []llm.ContentPart
	ConversationID string
	ProviderID     string
	ModelID        string
	SourceClient   string
	ParkedAt       time.Time
	SessionID      string
	AgentID        string
	Attempt        int
}

// throttleTurnToRecord encodes a throttleParkedTurn as a Class=throttle
// ParkedTurnRecord for the generalized parker.
func throttleTurnToRecord(turn throttleParkedTurn) (ParkedTurnRecord, error) {
	raw, err := json.Marshal(throttledTurnPayload{
		Message:        turn.Message,
		Parts:          turn.Parts,
		ConversationID: turn.ConversationID,
		ProviderID:     turn.ProviderID,
		ModelID:        turn.ModelID,
		SourceClient:   turn.SourceClient,
		ParkedAt:       turn.ParkedAt,
	})
	if err != nil {
		return ParkedTurnRecord{}, err
	}
	return ParkedTurnRecord{
		ConversationID: turn.ConversationID,
		SessionID:      turn.SessionID,
		AgentID:        turn.AgentID,
		Class:          llm.FailureThrottle,
		ResumeAt:       time.Time{}, // set by the caller from the plan
		Attempt:        turn.Attempt,
		TurnPayload:    raw,
	}, nil
}

// recordToThrottleTurn decodes a Class=throttle ParkedTurnRecord back into
// the throttleParkedTurn the resume path re-runs.
func recordToThrottleTurn(rec ParkedTurnRecord) (throttleParkedTurn, error) {
	var p throttledTurnPayload
	if err := json.Unmarshal(rec.TurnPayload, &p); err != nil {
		return throttleParkedTurn{}, err
	}
	return throttleParkedTurn{
		Message:        p.Message,
		Parts:          p.Parts,
		ConversationID: p.ConversationID,
		ProviderID:     p.ProviderID,
		ModelID:        p.ModelID,
		SourceClient:   p.SourceClient,
		ParkedAt:       p.ParkedAt,
		SessionID:      rec.SessionID,
		AgentID:        rec.AgentID,
		Attempt:        rec.Attempt,
	}, nil
}

// throttleParkAttemptKey carries the previous park generation's attempt
// through the resume path so a re-parked turn's schedule GROWS across park
// generations (stateless: context-carried, no ledger to clean up).
type throttleParkAttemptKey struct{}

// WithThrottleParkAttempt returns a ctx carrying the previous park
// generation's attempt count (resume path only; zero-value ctx = fresh turn).
func WithThrottleParkAttempt(ctx context.Context, attempt int) context.Context {
	return context.WithValue(ctx, throttleParkAttemptKey{}, attempt)
}

// ThrottleParkAttemptFromContext returns the previous park generation's
// attempt count, or 0 for a fresh turn.
func ThrottleParkAttemptFromContext(ctx context.Context) int {
	if ctx == nil {
		return 0
	}
	if n, ok := ctx.Value(throttleParkAttemptKey{}).(int); ok && n > 0 {
		return n
	}
	return 0
}

// throttleTurnContext is the per-turn dispatch stash: the ORIGINAL chat
// request RunOnceWithParts was entered with, captured so a mid-turn throttle
// can park a resume payload that re-enters the same path (Task 3). Guarded by
// throttleMu; a re-entrant resume overwrites it for the resumed turn.
type throttleTurnContext struct {
	message        string
	parts          []llm.ContentPart
	conversationID string
}

func (l *AgentLoop) setThrottleTurnContext(message string, parts []llm.ContentPart, conversationID string) {
	l.throttleMu.Lock()
	l.throttleTurnCtx = &throttleTurnContext{message: message, parts: parts, conversationID: conversationID}
	l.throttleMu.Unlock()
}

func (l *AgentLoop) clearThrottleTurnContext() {
	l.throttleMu.Lock()
	l.throttleTurnCtx = nil
	l.throttleMu.Unlock()
}

// captureThrottlePayload encodes the stash + parker fields into a payload for
// the ParkedTurnRecord. Returns nil when no dispatch is stashed (task/skill
// entry paths) or the encode fails — parking then proceeds without a payload
// (the resume drops it; leaf 03 owns task-path payload routing).
func (l *AgentLoop) captureThrottlePayload(conversationID, providerID, modelID string, parkedAt time.Time) json.RawMessage {
	l.throttleMu.Lock()
	stash := l.throttleTurnCtx
	l.throttleMu.Unlock()
	if stash == nil || stash.message == "" {
		return nil
	}
	rec, err := throttleTurnToRecord(throttleParkedTurn{
		Message:        stash.message,
		Parts:          stash.parts,
		ConversationID: stash.conversationID,
		ProviderID:     providerID,
		ModelID:        modelID,
		ParkedAt:       parkedAt,
	})
	if err != nil {
		l.logger.Warn("throttle park payload encode failed — parking without payload",
			"conversation_id", conversationID,
			"error", err,
		)
		return nil
	}
	return rec.TurnPayload
}

// parkThrottledTurn is the throttle branch body: builds a ParkedTurnRecord
// from the loop state + the ThrottleBackoffError, parks it on the loop's
// TurnParker, and transitions the agent to StateQuotaWait with reason
// "throttle_wait". Returns (true, nil) when parked, (false, nil) when there
// is no parker wired or the park is refused (callers keep the pass-through),
// and (false, giveUp) when the wait exceeds MaxWait — giveUp is the D8
// ThrottleGiveUpError.
//
// Scheduling: ResumeAt = plan.NextAttempt(now, attempt, err.RetryAt) from
// llm.DefaultBackoffPlan(llm.FailureThrottle, now, cfg). cfg is the
// process-wide llm.failure_policy install (SetFailurePolicyDefaults, set by
// the daemon; see backoff.go). The plan is composed, never recomputed.
// attempt = max(err.Attempt, previous park generation from ctx) so the
// schedule grows across park generations.
func (l *AgentLoop) parkThrottledTurn(ctx context.Context, terr *llm.ThrottleBackoffError) (bool, *llm.ThrottleGiveUpError) {
	if l == nil || l.turnParker == nil {
		return false, nil
	}
	now := l.clockNow()

	// Compose the schedule from the failure policy (D8): exponential step
	// from the throttle base, server RetryAt honored when later, horizon cap.
	plan := llm.DefaultBackoffPlan(llm.FailureThrottle, now, currentFailurePolicyDefaults())
	attempt := terr.Attempt
	if boost := ThrottleParkAttemptFromContext(ctx); boost > attempt {
		attempt = boost
	}
	resumeAt := plan.NextAttempt(now, attempt, terr.RetryAt)

	// MaxWait refusal (quota-mirror): a wait beyond the cap abandons the
	// turn with the D8 surface instead of parking (the parker itself would
	// only soft-stop to now+MaxWait, hiding the true wait from the user).
	l.turnParker.mu.Lock()
	maxWait := l.turnParker.maxWait
	l.turnParker.mu.Unlock()
	if wait := resumeAt.Sub(now); maxWait > 0 && wait > maxWait {
		l.logger.Warn("throttle wait exceeds max_wait — turn abandoned",
			"provider", terr.ProviderID,
			"model", terr.ModelID,
			"wait", wait,
			"max_wait", maxWait,
			"attempt", attempt,
		)
		return false, &llm.ThrottleGiveUpError{
			ProviderID: terr.ProviderID,
			ModelID:    terr.ModelID,
			Waited:     wait,
		}
	}

	// Capture the original dispatch for the resume path (Task 3). Session
	// identity is read under the loop mutex.
	l.mu.RLock()
	sessionID := l.currentSessionID
	l.mu.RUnlock()
	if sessionID == "" {
		l.throttleMu.Lock()
		if l.throttleTurnCtx != nil {
			sessionID = l.throttleTurnCtx.conversationID
		}
		l.throttleMu.Unlock()
	}
	payload := l.captureThrottlePayload(sessionID, terr.ProviderID, terr.ModelID, now)

	rec := ParkedTurnRecord{
		ConversationID: sessionID,
		SessionID:      sessionID,
		AgentID:        l.agentID,
		Class:          llm.FailureThrottle,
		ResumeAt:       resumeAt,
		Attempt:        attempt,
		TurnPayload:    payload,
	}
	if !l.turnParker.Park(rec) {
		l.logger.Warn("throttle park refused — surfacing the throttle error",
			"provider", terr.ProviderID,
			"model", terr.ModelID,
			"retry_at", terr.RetryAt,
			"attempt", attempt,
		)
		return false, nil
	}

	// Parked state: the quota branch's StateQuotaWait carries the parked
	// semantics (leaf 04 finalizes the surface); the reason distinguishes
	// the throttle class.
	l.safeTransition(StateQuotaWait, "throttle_wait", map[string]any{
		"provider":  terr.ProviderID,
		"model":     terr.ModelID,
		"resume_at": resumeAt.Format(time.RFC3339),
		"attempt":   attempt,
	})
	l.logger.Info("turn parked pending provider throttle window",
		"class", llm.FailureThrottle,
		"session_id", sessionID,
		"provider", terr.ProviderID,
		"resume_at", resumeAt.Format(time.RFC3339),
		"attempt", attempt,
	)
	return true, nil
}

// resumeThrottledTurn re-runs a parked throttle turn through its original
// chat entry (RunOnceWithParts) — the TurnParker resume callback for
// class=FailureThrottle. The previous attempt count rides the context so a
// re-throttled resume parks again with a grown schedule. Resume success and
// failure follow the loop's normal error handling — no second path.
func (l *AgentLoop) resumeThrottledTurn(ctx context.Context, rec ParkedTurnRecord) {
	if l == nil {
		return
	}
	turn, err := recordToThrottleTurn(rec)
	if err != nil {
		l.logger.Error("parked throttle turn payload undecodable — dropping",
			"session_id", rec.SessionID,
			"error", err,
		)
		return
	}
	if turn.Message == "" {
		// Deferred payload (parked without a captured message): nothing to
		// re-run — drop with a warning rather than replay an empty turn.
		l.logger.Warn("parked throttle turn has no payload — dropping",
			"session_id", rec.SessionID,
		)
		return
	}
	l.logger.Info("resuming parked turn after throttle window",
		"class", llm.FailureThrottle,
		"session_id", rec.SessionID,
		"conversation_id", turn.ConversationID,
		"attempt", rec.Attempt,
	)

	// Attempt growth across park generations: the re-run's park math starts
	// from this generation's attempt+1.
	resumeCtx := WithThrottleParkAttempt(ctx, rec.Attempt+1)
	_, err = l.RunOnceWithParts(resumeCtx, turn.Message, turn.Parts, turn.ConversationID)
	if err != nil {
		// A ThrottleBackoffError already re-parked inside the loop (grown
		// attempt, no error surfaced); a ThrottleGiveUpError or any other
		// failure ends the resume here — logged, no second retry path.
		l.logger.Error("resumed throttle turn failed",
			"session_id", rec.SessionID,
			"error", err,
		)
		return
	}
	l.logger.Info("resumed throttle turn completed",
		"session_id", rec.SessionID,
		"conversation_id", turn.ConversationID,
	)
}

// clockNow resolves the loop's injected clock (SetClock) or the wall clock.
func (l *AgentLoop) clockNow() time.Time {
	l.mu.RLock()
	fn := l.nowFunc
	l.mu.RUnlock()
	if fn != nil {
		return fn()
	}
	return time.Now()
}

// SetTurnParker wires the class-agnostic parker onto the loop (daemon wiring
// + tests). Nil-guarded per repo setter convention: a nil parker is refused,
// keeping the pass-through behavior. Thread-safe under l.mu.
func (l *AgentLoop) SetTurnParker(parker *TurnParker) {
	if l == nil || parker == nil {
		return
	}
	l.mu.Lock()
	l.turnParker = parker
	l.mu.Unlock()
}

// SetThrottleParker wires a throttle-resume parker onto the loop, sharing the
// SAME underlying TurnParker machinery as the quota watcher when the daemon
// passes the chat handler's generalized parker (leaf 03 merges the queues).
// The parker's resume callback is REPLACED with a class-dispatching router:
// quota records delegate to the handler's existing quota resume path;
// throttle records re-enter the loop via resumeThrottledTurn. Nil-guarded;
// replaces any previously wired parker (idempotent for repeated wiring).
func (l *AgentLoop) SetThrottleParker(h *ChatHandler, parker *TurnParker) {
	if l == nil || parker == nil {
		return
	}
	l.SetTurnParker(parker)
	parker.SetResumeFunc(func(ctx context.Context, rec ParkedTurnRecord) {
		if rec.Class == llm.FailureThrottle {
			l.resumeThrottledTurn(ctx, rec)
			return
		}
		if h != nil {
			h.resumeRouterDefault(ctx, rec)
			return
		}
	})
}

// TurnParker exposes the wired parker (nil when parking is disabled).
func (l *AgentLoop) TurnParker() *TurnParker {
	if l == nil {
		return nil
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.turnParker
}

// SetClock injects the clock used for parking schedule decisions. Nil-guarded
// per repo setter convention. Test seam; the daemon never sets it (wall
// clock).
func (l *AgentLoop) SetClock(now func() time.Time) {
	if l == nil || now == nil {
		return
	}
	l.mu.Lock()
	l.nowFunc = now
	l.mu.Unlock()
}

// ThrottleMaxWait reports the MaxWait the loop's parker applies for a
// throttled turn (handler-side pre-checks and tests). Zero when no parker is
// wired.
func (l *AgentLoop) ThrottleMaxWait() time.Duration {
	p := l.TurnParker()
	if p == nil {
		return 0
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.maxWait
}

// ensure sync stays referenced even if helpers above change (compile-time
// placeholder; removed alongside the field moves if it ever goes unused).
var _ sync.Locker
