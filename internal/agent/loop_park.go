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
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/tools"
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
	// TurnID rides the payload (C-K2 compile carrier, tree 04 leaf 01;
	// delivery parity lands with leaf 02): the chat.submit turn identity so
	// the throttle resume can emit the final turn.terminal under the acked
	// id like the quota path does (F15). Empty on legacy rows (omitted key),
	// which the resume treats as silent.
	TurnID   string    `json:"turn_id,omitempty"`
	ParkedAt time.Time `json:"parked_at,omitzero"`
	// Autonomous mirrors ParkedTurnRecord.Autonomous (F14 follow-up): the
	// parked turn ran with the AUTONOMOUS marker, so the resume must re-apply
	// it. It lives HERE, not only on the record, because the SQLite park store
	// persists turn_payload byte-for-byte and nothing else of a non-column
	// field — a step job parked before a daemon restart must still resume
	// autonomously, or its file_write/file_edit stages again (e2e run 8).
	Autonomous bool `json:"autonomous,omitempty"`
}

// throttleParkedTurn is the decoded form of a class=FailureThrottle record.
type throttleParkedTurn struct {
	Message        string
	Parts          []llm.ContentPart
	ConversationID string
	ProviderID     string
	ModelID        string
	SourceClient   string
	// TurnID carries the chat.submit turn identity (C-K2 compile carrier;
	// leaf 02 wires it into the park site + delivery parity).
	TurnID     string
	ParkedAt   time.Time
	Autonomous bool
	SessionID  string
	AgentID    string
	Attempt    int
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
		TurnID:         turn.TurnID,
		ParkedAt:       turn.ParkedAt,
		Autonomous:     turn.Autonomous,
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
		Autonomous:     turn.Autonomous,
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
		TurnID:         p.TurnID,
		ParkedAt:       p.ParkedAt,
		Autonomous:     p.Autonomous,
		SessionID:      rec.SessionID,
		AgentID:        rec.AgentID,
		Attempt:        rec.Attempt,
	}, nil
}

// parkedAutonomous reports whether a parked throttle turn ran with the
// AUTONOMOUS marker and its RESUME must re-apply it (F14 follow-up). Both
// carriers are honored: ParkedTurnRecord.Autonomous is the in-process copy,
// turn.Autonomous is the class payload's copy — the ONLY part the SQLite park
// store persists, so a record re-armed after a daemon restart carries the
// marker solely there. Either set means autonomous; neither means interactive.
func parkedAutonomous(rec ParkedTurnRecord, turn throttleParkedTurn) bool {
	return rec.Autonomous || turn.Autonomous
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

// resumedTurnKey carries the resumed-turn marker (H3, bughunt 2026-09-03):
// a resumed parked turn re-enters RunOnceWithParts on a conversation that
// already holds the original user message; the marker suppresses ONLY the
// history re-add so the model context does not see the turn duplicated
// (user: X → assistant: "" → user: X).
type resumedTurnKey struct{}

// WithResumedTurn marks ctx as a RESUMED parked turn (throttle and quota
// resume paths; fresh turns use a plain context).
func WithResumedTurn(ctx context.Context) context.Context {
	return context.WithValue(ctx, resumedTurnKey{}, true)
}

// ResumedTurnFromContext reports whether ctx marks a resumed parked turn.
// False for fresh turns and nil contexts.
func ResumedTurnFromContext(ctx context.Context) bool {
	if ctx == nil {
		return false
	}
	v, _ := ctx.Value(resumedTurnKey{}).(bool)
	return v
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
//
// F8 (bughunt 2026-11-09, statemachine-remediation tree 04 leaf 01): one
// slot per LOOP is NOT per-turn safe — two overlapping RunOnceWithParts
// calls on the same loop race on it (turn B's entry overwrites A's stash;
// A throttles and parks B's dispatch, and A's deferred clear can blank B's
// payload). The stash is now ALSO carried on the turn's context via
// WithThrottleDispatch (keyed by the turn's ctx, immune to overlap); the
// loop slot remains as a fallback for park sites that run on a context
// this loop did not enter (defensive; no production path hits it today).
type throttleTurnContext struct {
	message        string
	parts          []llm.ContentPart
	conversationID string
	// parked is the F6 explicit per-turn marker (tree 04 leaf 02): set by
	// parkThrottledTurn on a SUCCESSFUL park, read by RunOnceWithParts'
	// parked-turn detection. The OPTIONAL state machine (nil / rejected
	// transition => StateQuotaWait never observable) must not be the only
	// signal that a turn parked: without the marker a parked turn fell
	// through to the post-turn success pipeline and appended a blank
	// assistant message (H2's defect class).
	//
	// Threading: written and read on the TURN's goroutine — park happens
	// inside reasoningCycle (synchronous under RunOnceWithParts), the read
	// happens in RunOnceWithParts after the cycle returns. No lock needed.
	parked bool
}

// markThrottleParked sets the F6 parked marker on the turn ctx's dispatch
// stash. No-op when the ctx did not pass through RunOnceWithParts.
func markThrottleParked(ctx context.Context) {
	if d := throttleDispatchFromContext(ctx); d != nil {
		d.parked = true
	}
}

// turnParkedInContext reports whether THIS turn parked (F6 marker). False
// for contexts that never entered RunOnceWithParts.
func turnParkedInContext(ctx context.Context) bool {
	d := throttleDispatchFromContext(ctx)
	return d != nil && d.parked
}

// chatTurnIDKey carries the chat.submit turn identity on the turn context
// (C-K2, tree 04 leaf 02) so a mid-turn throttle park can stamp the TurnID
// onto the resume payload — the throttle resume then emits the final
// turn.terminal under the acked id exactly like the quota path (F15).
type chatTurnIDKey struct{}

// WithChatTurnID returns ctx carrying the turn's chat.submit identity.
// Empty turnIDs are not stamped (legacy turns stay payload-silent).
func WithChatTurnID(ctx context.Context, turnID string) context.Context {
	if ctx == nil || turnID == "" {
		return ctx
	}
	return context.WithValue(ctx, chatTurnIDKey{}, turnID)
}

// ChatTurnIDFromContext reads the turn identity stamped by the handler
// seam; "" when absent.
func ChatTurnIDFromContext(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	s, _ := ctx.Value(chatTurnIDKey{}).(string)
	return s
}

// throttleDispatchKey carries the per-turn dispatch on the turn context.
type throttleDispatchKey struct{}

// WithThrottleDispatch returns ctx carrying the turn's original dispatch
// (RunOnceWithParts entry). SetTurnParker park sites read it first when
// building the resume payload (F8).
func WithThrottleDispatch(ctx context.Context, d *throttleTurnContext) context.Context {
	if ctx == nil || d == nil {
		return ctx
	}
	return context.WithValue(ctx, throttleDispatchKey{}, d)
}

// throttleDispatchFromContext reads the turn-context dispatch (F8); nil
// when the ctx did not pass through RunOnceWithParts.
func throttleDispatchFromContext(ctx context.Context) *throttleTurnContext {
	if ctx == nil {
		return nil
	}
	d, _ := ctx.Value(throttleDispatchKey{}).(*throttleTurnContext)
	return d
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
//
// ctx is the TURN's context: its AUTONOMOUS marker (daemon step jobs) is
// recorded in the payload so the resumed turn re-enters autonomously. This is
// the carrier that survives the SQLite park store.
//
// F8 (tree 04 leaf 01): the dispatch is read from the TURN CONTEXT first
// (WithThrottleDispatch, stamped by RunOnceWithParts with the exact
// message/parts/conversation the turn was entered with). Overlapping turns
// on one loop no longer cross-park each other's dispatches — the old shared
// l.throttleTurnCtx slot let turn B overwrite turn A's stash mid-flight, so
// A's park carried B's payload and A's deferred clear could blank B's. The
// loop slot remains only as a fallback for park sites entered without the
// RunOnceWithParts context plumbing.
func (l *AgentLoop) captureThrottlePayload(ctx context.Context, conversationID, providerID, modelID string, parkedAt time.Time) json.RawMessage {
	stash := throttleDispatchFromContext(ctx)
	if stash == nil {
		l.throttleMu.Lock()
		stash = l.throttleTurnCtx
		l.throttleMu.Unlock()
	}
	if stash == nil || stash.message == "" {
		return nil
	}
	rec, err := throttleTurnToRecord(throttleParkedTurn{
		Message:        stash.message,
		Parts:          stash.parts,
		ConversationID: stash.conversationID,
		ProviderID:     providerID,
		ModelID:        modelID,
		// C-K2 (tree 04 leaf 02): the chat.submit turn identity rides the
		// payload (stamped on the turn ctx at the handler seam, preserved
		// across re-parks by resumeThrottledTurn) so the throttle resume can
		// emit the final turn.terminal under the acked id like the quota
		// path does. "" on legacy/unbound entries = payload-silent.
		TurnID:     ChatTurnIDFromContext(ctx),
		ParkedAt:   parkedAt,
		Autonomous: tools.AutonomousFromContext(ctx),
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
		// Give-up observability (leaf 04): the abandonment surfaces on the
		// existing agent.quota_wait topic so TUI/GUI can show it (D9).
		l.turnParker.emitGiveUpEvent(l.agentID, terr.ModelID, terr.ProviderID, wait)
		return false, &llm.ThrottleGiveUpError{
			ProviderID: terr.ProviderID,
			ModelID:    terr.ModelID,
			Waited:     wait,
		}
	}

	// Capture the original dispatch for the resume path (Task 3). Session
	// identity is read under the loop mutex. H2 (bughunt 2026-09-08): the
	// task path (RunWithTask) is the only writer of currentSessionID —
	// interactive turns leave it empty again (the cbf0b775 accounting
	// repurpose moved to the dedicated turnAccountingSessionID field), so
	// the AUDIT FIX H11 contract below holds unconditionally.
	l.mu.RLock()
	sessionID := l.currentSessionID
	l.mu.RUnlock()
	// AUDIT FIX H11 (bughunt 2026-09-03): the old fallback stamped the
	// CONVERSATION id into SessionID (and both record fields), so the WS
	// filter (server.go session_id match) dropped park events for
	// TUI/CLI-originated turns — the client subscribed on its session id,
	// never on conv-*. With no session binding, leave SessionID empty:
	// events with neither id broadcast to all connections (documented
	// backward-compat path), which is correct for an unbound loop.
	conversationID := sessionID
	if conversationID == "" {
		// F8: prefer the turn's own context-carried dispatch; the shared
		// loop slot (fallback) can hold ANOTHER overlapping turn's stash.
		if stash := throttleDispatchFromContext(ctx); stash != nil {
			conversationID = stash.conversationID
		} else {
			l.throttleMu.Lock()
			if l.throttleTurnCtx != nil {
				conversationID = l.throttleTurnCtx.conversationID
			}
			l.throttleMu.Unlock()
		}
	}
	payload := l.captureThrottlePayload(ctx, conversationID, terr.ProviderID, terr.ModelID, now)

	rec := ParkedTurnRecord{
		ConversationID: conversationID,
		SessionID:      sessionID,
		AgentID:        l.agentID,
		Class:          llm.FailureThrottle,
		ResumeAt:       resumeAt,
		Attempt:        attempt,
		TurnPayload:    payload,
		// F14 follow-up: carry the turn's autonomy onto the record (and its
		// payload copy) so the resume re-applies the marker. A step job's turn
		// runs under tools.ContextWithAutonomous; without this the resume
		// re-enters interactive and its file_write/file_edit stage a change
		// nothing accepts (e2e run 8).
		Autonomous: tools.AutonomousFromContext(ctx),
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
	// F6 (tree 04 leaf 02): stamp the explicit per-turn parked marker on
	// the turn ctx's dispatch stash BEFORE anything else. RunOnceWithParts'
	// parked-turn detection reads this marker and no longer REQUIRES the
	// optional state machine: with stateMachine nil (or a rejected
	// transition — safeTransition only logs), the old CurrentState()==
	// StateQuotaWait read silently missed the park and the parked turn fell
	// through to the post-turn success pipeline, appending a blank assistant
	// message (H2's defect class). The state-machine read stays as a
	// secondary signal for park paths without ctx plumbing.
	markThrottleParked(ctx)
	// Park observability (leaf 04): the parked record surfaces on the
	// existing agent.quota_wait topic with its class + resume time (D9).
	l.turnParker.emitParkEvent(rec, terr.ModelID, terr.ProviderID)

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

// isTransportTimeout reports whether err is a transport-level timeout:
// context deadline/cancellation or a net.Error with Timeout() — the same
// classification the resolver uses (VerdictForFailure) to arm the
// endpoint-level cooldown this file's parking waits on.
func isTransportTimeout(err error) bool {
	if err == nil {
		return false
	}
	return llm.VerdictForFailure(err).Reason == "transport_timeout"
}

// parkEndpointBlockedTurn parks a turn whose provider call hit a
// timeout-class failure while the resolver holds an endpoint-level timeout
// cooldown (tree 02 leaf 04 D10 + tree 03 leaf 02 D4/D8 composition): the
// failed endpoint is blocked and the lazy-clear probe (first request after
// expiry; failure re-arms with doubling; success clears) is the ONE
// reconnecting prober — every other agent's turn waits, parked on the SAME
// TurnParker machinery and quota_wait state as a throttled turn (D9: no new
// state, no new error type, no new topic).
//
// The ThrottleBackoffError is CONSTRUCTED here (never wrapped from a
// producer — do not touch errors.go) with RetryAt = the live endpoint-block
// deadline (llm.EndpointBlockUntil), so the existing parkThrottledTurn
// scheduling composes unchanged: plan.NextAttempt honors the later of the
// block expiry and the throttle base step, and the D8 MaxWait give-up keeps
// applying.
//
// Identity resolution: servedModel carries the failed call's EndpointKey
// identity when known; when it is nil (e.g. the turn's FIRST resolve failed
// with ErrAllEndpointsBlocked — a peer armed the block moments ago) the
// alias's members are consulted and the EARLIEST-expiring live block wins —
// that is when the alias becomes prober-eligible again. When the resolver
// holds NO live block for the served model or any alias member, the turn is
// NOT parked here — callers keep their existing behavior.
func (l *AgentLoop) parkEndpointBlockedTurn(ctx context.Context, aliasName string, servedModel *llm.ModelConfig) (bool, *llm.ThrottleGiveUpError) {
	if l == nil || l.turnParker == nil || l.resolver == nil {
		return false, nil
	}
	identity := servedModel
	blockUntil := l.resolver.EndpointBlockUntil(servedModel)
	if blockUntil.IsZero() {
		// No live block on the served model: fall back to the alias
		// members (ErrAllEndpointsBlocked entry — any member's endpoint
		// may hold the block; wait for the earliest expiry).
		models, ok := l.resolver.GetAllModelsForAlias(aliasName)
		if !ok {
			return false, nil
		}
		for _, m := range models {
			if m == nil {
				continue
			}
			if until := l.resolver.EndpointBlockUntil(m); !until.IsZero() && (blockUntil.IsZero() || until.Before(blockUntil)) {
				blockUntil = until
				identity = m
			}
		}
		if blockUntil.IsZero() {
			// Nothing in the alias is blocked: the endpoint is
			// prober-eligible (or the failure never armed one).
			return false, nil
		}
	}
	return l.parkThrottledTurn(ctx, &llm.ThrottleBackoffError{
		ProviderID: identity.ProviderID,
		ModelID:    identity.ModelID,
		RetryAt:    blockUntil,
		Attempt:    ThrottleParkAttemptFromContext(ctx),
	})
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
	// Resume observability (leaf 04): the resumed record surfaces on the
	// existing agent.quota_wait topic with its class + waited duration (D9).
	// Guarded: the resume callback normally runs on a wired parker, but the
	// emit must never be able to panic the resume path.
	// D-M3 (bughunt 2026-09-04): the payload's ParkedAt is the TRUE park
	// time; rec.ResumeAt may have been rewritten by the parker's MaxWait
	// soft-stop, which would understate Waited.
	if l.turnParker != nil {
		l.turnParker.emitResumeEvent(rec, turn.ParkedAt)
	}

	// Attempt growth across park generations: the re-run's park math starts
	// from this generation's attempt+1. H3 (bughunt 2026-09-03): the marker
	// also suppresses the user-message history re-add inside
	// RunOnceWithParts — the parked attempt already added it.
	resumeCtx := WithThrottleParkAttempt(WithResumedTurn(ctx), rec.Attempt+1)
	// C-K2 (tree 04 leaf 02): the park generation's TurnID rides the resume
	// ctx so a re-park inside this resume re-encodes the SAME turn identity
	// into the next payload (re-park preserves the acked id, mirroring the
	// quota path's `TurnID: turn.TurnID // F15` carry).
	if turn.TurnID != "" {
		resumeCtx = WithChatTurnID(resumeCtx, turn.TurnID)
	}
	// F14 follow-up: re-apply the AUTONOMOUS marker a parked step job's turn
	// ran under. `ctx` here is the PARKER's context (no turn markers), so
	// without this the resumed turn re-enters interactive, file_write/
	// file_edit STAGE a pending change no human will accept, and the step
	// still reports success (e2e run 8, 2026-09-11). EITHER carrier counts:
	// rec.Autonomous is the in-process copy, the payload copy (turn.Autonomous)
	// is what survives the SQLite park store's restart re-arm.
	if parkedAutonomous(rec, turn) {
		resumeCtx = tools.ContextWithAutonomous(resumeCtx)
	}
	reply, err := l.RunOnceWithParts(resumeCtx, turn.Message, turn.Parts, turn.ConversationID)
	if err != nil {
		// D9 park-not-fail parity (quota resume, handler.go): a classified
		// provider wait that surfaces DURING the resume must re-park, not
		// terminalize. RunOnceWithParts parks ThrottleBackoffError waits
		// internally (returning the parked shape), but a QuotaResetError (or
		// any other classified provider wait the loop passes through) reaches
		// THIS seam as an error — the quota resume at handler.go:2751 re-parks
		// its QuotaResetError, and the throttle resume mirrors that posture
		// through the SAME parkThrottledTurn machinery (grown attempt carried
		// in resumeCtx). A ThrottleGiveUpError or any unclassified failure
		// still ends the resume below — logged, no second retry path.
		if quotaErr, ok := errors.AsType[*llm.QuotaResetError](err); ok {
			unblockAt := quotaErr.ResetAt
			if unblockAt.IsZero() && quotaErr.RetryAfter > 0 {
				unblockAt = time.Now().Add(quotaErr.RetryAfter)
			}
			if !unblockAt.IsZero() && l.TurnParker() != nil {
				rec2 := rec
				rec2.ResumeAt = unblockAt
				rec2.Attempt = rec.Attempt + 1
				rec2.TurnPayload = rec.TurnPayload
				if p := l.TurnParker(); p.Park(rec2) {
					l.logger.Info("resumed throttle turn hit a fresh quota error — re-parked",
						"session_id", rec.SessionID,
						"provider", quotaErr.ProviderID,
						"attempt", rec.Attempt+1,
					)
					return
				}
				l.logger.Warn("throttle resume quota re-park refused — failing the resume",
					"session_id", rec.SessionID,
					"error", err,
				)
			}
		}
		// A ThrottleBackoffError already re-parked inside the loop (grown
		// attempt, no error surfaced); a ThrottleGiveUpError or any other
		// failure ends the resume here — logged, no second retry path.
		l.logger.Error("resumed throttle turn failed",
			"session_id", rec.SessionID,
			"error", err,
		)
		// M-13 parity with the quota resume's failure branch: an abandoned
		// resume (give-up / hard failure) resolves the awaiting client's
		// turn honestly instead of leaving it hanging. Legacy rows (no
		// TurnID) and unrouted handlers stay silent.
		if handler := l.throttleResumeDeliveryHandler(); handler != nil && turn.TurnID != "" {
			handler.sendResponse("throttle-resume-"+rec.SessionID, ChatResponse{
				ConversationID: rec.SessionID,
				Error:          "Auto-resume failed: " + err.Error(),
			})
			failTraceID, failTurnSpanID := handler.turnRegistry.TraceForTurn(turn.TurnID)
			handler.publishTurnTerminal(TurnTerminalEvent{
				ConversationID: turn.ConversationID,
				SessionID:      rec.SessionID,
				TurnID:         turn.TurnID,
				HandlerCase:    "throttle_resume",
				Status:         "failed",
				Reply:          "I encountered an error: Auto-resume failed: " + err.Error(),
				Error:          err.Error(),
				TraceID:        failTraceID,
				TurnSpanID:     failTurnSpanID,
			})
		}
		return
	}
	if strings.TrimSpace(reply) == "" {
		// Parked again inside the resume (re-throttle re-park: the loop
		// returns the parked shape ("", nil) — the next generation's
		// payload already carries the TurnID via resumeCtx) or an empty
		// completion. Nothing to deliver; a terminal here would falsely
		// close a turn that is still parked.
		l.logger.Info("resumed throttle turn parked again or produced no reply — deferring delivery",
			"session_id", rec.SessionID,
			"conversation_id", turn.ConversationID,
		)
		return
	}
	l.logger.Info("resumed throttle turn completed",
		"session_id", rec.SessionID,
		"conversation_id", turn.ConversationID,
	)

	// M-13 (C-K2 delivery parity, tree 04 leaf 02): a resumed throttle
	// turn's answer must reach the client and history exactly like the
	// quota resume (handler.resumeQuotaParkedTurn): persistExchange →
	// sendResponse (chat.response + chat_message push) → turn.terminal
	// under the parked TurnID. SAME helpers, SAME topics, SAME event shape
	// — no new payload invented here.
	handler := l.throttleResumeDeliveryHandler()
	if handler == nil {
		// Bare loop-side parker wiring (no chat handler routed): the turn
		// completed in the loop but there is no delivery seam to push
		// through. Loud warn — this is the silent-answer defect class.
		l.logger.Warn("throttle resume completed with no delivery handler — client not notified",
			"session_id", rec.SessionID,
			"conversation_id", turn.ConversationID,
			"turn_id", turn.TurnID,
		)
		return
	}
	if turn.TurnID == "" {
		// Legacy row (parked before C-K2, no turn_id in the payload): the
		// resume ran and the loop-side history append happened; client
		// delivery + turn.terminal are SKIPPED (there is no acked turn to
		// resolve, and a push without a turn identity would race the
		// eventual legacy RPC reply). Warn once so operators can see rows
		// draining out of the old schema.
		l.logger.Warn("throttle resume completed on a legacy row (no TurnID) — client delivery skipped",
			"session_id", rec.SessionID,
			"conversation_id", turn.ConversationID,
		)
		return
	}
	// Routing key: the park record's SessionID (H11: interactive parks leave
	// it empty — never stamp the conversation id into it). The handler's own
	// persistID convention applies here: prefer the session id, fall back to
	// the conversation id, so GUI submits (which carry only conversation_id)
	// still persist + push the resumed answer (Scopes-2 audit precedent,
	// handler.go:1198).
	deliveryID := rec.SessionID
	if deliveryID == "" {
		deliveryID = turn.ConversationID
	}
	// Persist the exchange (quota parity: the session id keys the store row).
	handler.persistExchange(deliveryID, turn.Message, turn.Parts, reply, turn.AgentID)

	// Push the result back to the session via the bus (chat.response +
	// chat_message; the push routes on SessionID like the quota resume).
	handler.sendResponse("throttle-resume-"+deliveryID, ChatResponse{
		ConversationID: deliveryID,
		SessionID:      deliveryID, // for WS push routing
		Reply:          reply,
	})

	// Final turn.terminal for the submitted turn (F15 shape, HandlerCase
	// names the lane): the client's awaiter resolves only if the terminal
	// event fires under the id the ack returned.
	resumeTraceID, resumeTurnSpanID := handler.turnRegistry.TraceForTurn(turn.TurnID)
	handler.publishTurnTerminal(TurnTerminalEvent{
		ConversationID: turn.ConversationID,
		SessionID:      rec.SessionID,
		TurnID:         turn.TurnID,
		HandlerCase:    "throttle_resume",
		Status:         "completed",
		Reply:          reply,
		TraceID:        resumeTraceID,
		TurnSpanID:     resumeTurnSpanID,
	})
	l.logger.Info("resumed throttle turn delivered",
		"session_id", rec.SessionID,
		"conversation_id", turn.ConversationID,
		"turn_id", turn.TurnID,
		"reply_length", len(reply),
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
	// C-K2 delivery parity: remember the handler so resumeThrottledTurn can
	// deliver a successful resume (persistExchange + client push +
	// turn.terminal) through the SAME helpers the quota resume uses. A nil
	// handler (bare loop-side parker wiring) keeps the resume silent.
	if h != nil {
		l.mu.Lock()
		l.throttleResumeHandler = h
		l.mu.Unlock()
	}
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

// throttleResumeDeliveryHandler reads the C-K2 delivery handler (nil when
// the parker was wired without a chat handler — resume stays loop-side only).
func (l *AgentLoop) throttleResumeDeliveryHandler() *ChatHandler {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.throttleResumeHandler
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

// LoopAutonomousMarker reports whether the LOOP-level autonomous latch
// (SetAutonomous / l.autonomous, loop.go) is set.
//
// It exists for the daemon's wiring pin: since F14 the production job path
// (AgentJobProcessor.Process) marks the TURN's context, never the loop, and
// that latch must stay false for every production path — it is a one-way
// marker with no reset, so setting it on the process-wide interactive loop
// silently disables the pending-change preview for every later chat turn.
// Read-only; guarded by the loop mutex (SetAutonomous writes under it).
//
// SetAutonomous itself is retained as the escape hatch for a caller that
// genuinely owns a headless-only loop; it is NOT part of the queued-job path.
func (l *AgentLoop) LoopAutonomousMarker() bool {
	if l == nil {
		return false
	}
	l.mu.RLock()
	defer l.mu.RUnlock()
	return l.autonomous
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
