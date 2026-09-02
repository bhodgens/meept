package agent

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"time"

	"github.com/caimlas/meept/internal/llm"
)

// QuotaParkedTurn captures a chat turn interrupted by a provider quota wall
// and is waiting for the quota window to lift before being retried. Mirrors
// ParkedTurn (budget_resume.go) with quota-specific resume metadata.
//
// Since tree 03 leaf 01 the scheduling machinery lives on the class-agnostic
// TurnParker (parked_turn.go); QuotaResumeWatcher is a thin delegating
// wrapper over one TurnParker, byte-identical for class=FailureQuota.
type QuotaParkedTurn struct {
	SessionID      string            // original session ID (for persistence + push)
	ConversationID string            // resolved conversation ID (for the agent loop)
	Message        string            // user message text
	Parts          []llm.ContentPart // multimodal parts, if any
	AgentID        string            // agent override, if any
	SourceClient   string            // originating client identifier
	ProviderID     string            // provider that hit the quota
	CredentialKey  string            // credential fingerprint of the blocked pool
	UnblockAt      time.Time         // earliest time the quota window lifts
	ParkedAt       time.Time         // when the turn was parked
}

// DefaultQuotaResumePollInterval is the re-check cadence for parked turns.
// Mirrors config llm.quota_retry.defer_check_interval's 10m default; the
// field is exported on the watcher for tests and config wiring. It is an
// alias of the TurnParker default (parked_turn.go) — one source of truth.
const DefaultQuotaResumePollInterval = DefaultTurnParkerPollInterval

// quotaTurnPayload is the class=FailureQuota TurnPayload encoding stored on
// a ParkedTurnRecord (SHARED-CONVENTIONS §4.5: "JSON of the quota watcher's
// stored fields"). The keys the resume router reads are message, parts,
// conversation_id, provider_id, credential_key (provider_id/credential_key
// REQUIRED — the re-park path at handler.go reads them); source_client and
// parked_at ride along so a parked turn's full stored state round-trips
// byte-identically through the generalized queue. SessionID/AgentID/
// UnblockAt are record fields, not payload keys.
type quotaTurnPayload struct {
	Message        string            `json:"message"`
	Parts          []llm.ContentPart `json:"parts,omitempty"`
	ConversationID string            `json:"conversation_id"`
	ProviderID     string            `json:"provider_id"`
	CredentialKey  string            `json:"credential_key"`
	SourceClient   string            `json:"source_client,omitempty"`
	ParkedAt       time.Time         `json:"parked_at,omitempty"`
}

// quotaTurnToRecord encodes a QuotaParkedTurn as a Class=quota
// ParkedTurnRecord for the generalized parker.
func quotaTurnToRecord(turn QuotaParkedTurn) (ParkedTurnRecord, error) {
	raw, err := json.Marshal(quotaTurnPayload{
		Message:        turn.Message,
		Parts:          turn.Parts,
		ConversationID: turn.ConversationID,
		ProviderID:     turn.ProviderID,
		CredentialKey:  turn.CredentialKey,
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
		Class:          llm.FailureQuota,
		ResumeAt:       turn.UnblockAt,
		TurnPayload:    raw,
	}, nil
}

// recordToQuotaTurn decodes a Class=quota ParkedTurnRecord back into the
// QuotaParkedTurn the resume callback expects.
func recordToQuotaTurn(rec ParkedTurnRecord) (QuotaParkedTurn, error) {
	var p quotaTurnPayload
	if err := json.Unmarshal(rec.TurnPayload, &p); err != nil {
		return QuotaParkedTurn{}, err
	}
	return QuotaParkedTurn{
		SessionID:      rec.SessionID,
		ConversationID: p.ConversationID,
		Message:        p.Message,
		Parts:          p.Parts,
		AgentID:        rec.AgentID,
		ProviderID:     p.ProviderID,
		CredentialKey:  p.CredentialKey,
		SourceClient:   p.SourceClient,
		UnblockAt:      rec.ResumeAt,
		ParkedAt:       p.ParkedAt,
	}, nil
}

// QuotaResumeWatcher parks chat turns interrupted by provider quota errors
// and retries them once the quota window lifts. It polls on a fixed
// interval; when the earliest unblock time for parked turns has passed, it
// drains the queue (oldest first) by invoking the resume callback supplied
// by the ChatHandler (quota-reset-resilience plan leaf 06: task deferral +
// auto-resume).
//
// MaxWait soft-stop: turns whose remaining wait exceeds MaxWait are NOT
// parked (the caller surfaces the error instead), matching contract 7's
// "schedule requeue at min(unblockAt, now+MaxWait)".
//
// The watcher is best-effort: turns parked when the daemon shuts down are
// dropped (logged at warn) and are not persisted across restarts. Quota
// blocks are in-memory, so a restart re-probes providers anyway.
//
// Since tree 03 leaf 01 this is a delegating wrapper over a TurnParker:
// the watcher refuses over-MaxWait waits (quota contract) before the
// parker's generic min(ResumeAt, now+MaxWait) soft-stop can reschedule,
// and owns the quota-facing log lines. Persistence: none — the underlying
// TurnParker is memory-only, mirroring the original watcher.
type QuotaResumeWatcher struct {
	logger     *slog.Logger
	maxWait    time.Duration
	resumeFunc func(ctx context.Context, turn QuotaParkedTurn)

	turns *TurnParker // generalized machinery; quota decisions pre-checked here
}

// NewQuotaResumeWatcher creates a watcher. resumeFunc is invoked (in a
// background goroutine) for each parked turn once its unblock time passes;
// it is responsible for re-running the turn and pushing the result to the
// session. A nil resumeFunc disables auto-resume (Park becomes a no-op).
// maxWait <= 0 falls back to llm.DefaultQuotaMaxWait.
func NewQuotaResumeWatcher(logger *slog.Logger, resumeFunc func(ctx context.Context, turn QuotaParkedTurn), maxWait time.Duration) *QuotaResumeWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	if maxWait <= 0 {
		maxWait = llm.DefaultQuotaMaxWait
	}
	w := &QuotaResumeWatcher{
		logger:     logger,
		maxWait:    maxWait,
		resumeFunc: resumeFunc,
	}
	// The inner parker runs silently: this wrapper owns the quota-facing
	// log lines so observable output stays byte-identical.
	var adapted func(context.Context, ParkedTurnRecord)
	if resumeFunc != nil {
		adapted = func(ctx context.Context, rec ParkedTurnRecord) {
			turn, err := recordToQuotaTurn(rec)
			if err != nil {
				w.logger.Error("parked quota turn payload undecodable — dropping",
					"session_id", rec.SessionID,
					"err", err,
				)
				return
			}
			resumeFunc(ctx, turn)
		}
	}
	w.turns = NewTurnParker(slog.New(slog.NewTextHandler(io.Discard, nil)), adapted, maxWait)
	return w
}

// SetPollInterval overrides the re-check cadence (config plumbing seam).
// Must be called before Start.
func (w *QuotaResumeWatcher) SetPollInterval(d time.Duration) {
	if w == nil || d <= 0 {
		return
	}
	w.turns.SetPollInterval(d)
}

// Start begins the background polling loop. Safe to call once; subsequent
// calls are no-ops.
func (w *QuotaResumeWatcher) Start(ctx context.Context) {
	if w == nil || w.resumeFunc == nil {
		return
	}
	w.turns.mu.Lock()
	already := w.turns.cancel != nil
	w.turns.mu.Unlock()
	if already {
		return // already started
	}
	w.turns.Start(ctx)
	w.logger.Info("quota resume watcher started",
		"poll_interval", w.turns.pollInterval,
		"max_wait", w.maxWait,
	)
}

// Stop halts the polling loop and waits for it to exit. Turns that have not
// yet resumed are logged and dropped.
func (w *QuotaResumeWatcher) Stop() {
	if w == nil {
		return
	}
	// Mirror the original watcher exactly: the dropped count is read under
	// the parker's mutex atomically with cancel, then the poller is joined.
	w.turns.mu.Lock()
	if w.turns.cancel != nil {
		w.turns.cancel()
		w.turns.cancel = nil
	}
	remaining := len(w.turns.parked)
	w.turns.mu.Unlock()

	w.turns.wg.Wait()
	if remaining > 0 {
		w.logger.Warn("quota resume watcher stopped with parked turns dropped", "dropped", remaining)
	}
}

// Park enqueues a turn for later retry once the quota window lifts.
// Returns false (and does nothing) when:
//   - auto-resume is not configured, or
//   - the wait exceeds MaxWait (soft-stop: caller surfaces the error), or
//   - the unblock time is unknown/zero (cannot schedule a resume).
func (w *QuotaResumeWatcher) Park(turn QuotaParkedTurn) bool {
	if w == nil || w.resumeFunc == nil {
		return false
	}
	if turn.UnblockAt.IsZero() {
		return false
	}
	wait := time.Until(turn.UnblockAt)
	if wait <= 0 {
		return false // already unblocked; caller should retry directly
	}
	if wait > w.maxWait {
		w.logger.Info("quota wait exceeds max_wait — not parking",
			"provider", turn.ProviderID,
			"wait", wait,
			"max_wait", w.maxWait,
		)
		return false
	}
	if turn.ParkedAt.IsZero() {
		turn.ParkedAt = time.Now()
	}
	rec, err := quotaTurnToRecord(turn)
	if err != nil {
		w.logger.Error("quota park payload encode failed — not parking",
			"session_id", turn.SessionID,
			"err", err,
		)
		return false
	}
	if !w.turns.Park(rec) {
		// Unreachable when the pre-checks above pass (the parker only
		// refuses zero/past resume times); stay safe regardless.
		return false
	}
	w.logger.Info("parked turn pending quota reset",
		"session_id", turn.SessionID,
		"provider", turn.ProviderID,
		"unblock_at", turn.UnblockAt.Format(time.RFC3339),
		"queued", w.turns.Pending(),
	)
	return true
}

// Pending returns the number of currently parked turns.
func (w *QuotaResumeWatcher) Pending() int {
	if w == nil {
		return 0
	}
	return w.turns.Pending()
}

// drainDue resumes every parked turn whose unblock time has passed,
// oldest-first, sequentially: a slow resume delays later resumes rather
// than reordering them. Called from the polling loop; its cadence bounds
// the delay. Turns whose window has not lifted stay parked.
func (w *QuotaResumeWatcher) drainDue(ctx context.Context) {
	if w == nil {
		return
	}
	w.turns.drainDue(ctx)
}
