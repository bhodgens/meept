package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/pkg/models"
)

// isTimeWindowedBudget reports whether a budget limit clears on its own over
// time (hourly/daily token or cost windows). Per-task and per-session limits
// are cumulative and never clear on a timer, so they are not auto-resumed.
func isTimeWindowedBudget(reason llm.BudgetLimit) bool {
	switch reason {
	case llm.BudgetLimitHourlyTokens, llm.BudgetLimitDailyTokens,
		llm.BudgetLimitHourlyCost, llm.BudgetLimitDailyCost:
		return true
	default:
		return false
	}
}

// ParkedTurn captures a chat turn that was interrupted by budget exhaustion
// and is waiting for the budget window to clear before being retried.
type ParkedTurn struct {
	SessionID      string            // original session ID (for persistence + push)
	ConversationID string            // resolved conversation ID (for the agent loop)
	Message        string            // user message text
	Parts          []llm.ContentPart // multimodal parts, if any
	AgentID        string            // agent override, if any
	SourceClient   string            // originating client identifier
	// Limit is the budget limit that fired the park (hourly_token, ...).
	// Additive (tree 04 leaf 04): carried into the persisted payload and
	// the park event so surfaces can say WHICH window is being waited on.
	// Zero on legacy parks (unknown reason), which stay silent on the field.
	Limit llm.BudgetLimit
	// TurnID is the chat.submit turn identity (week bughunt 2026-09-17
	// F15): recorded at park time so the resume can emit the FINAL
	// turn.terminal event under the id the client's ack returned —
	// without it the awaiter never resolves. Empty on legacy parked
	// turns (the blocking `chat` path), which stay silent on resume.
	TurnID   string
	ParkedAt time.Time // when the turn was parked
}

// budgetParkResumeHorizon bounds how long a persisted budget park may wait
// across restarts before the store prunes it at Load (F5 give-up horizon —
// "every park class has a bounded give-up horizon"). Budget waits are
// window-based (hourly/daily), so a park older than a full quota-max-wait
// day is well past any time-windowed limit; keeping it would replay a
// stale turn the user has moved on from. The store's Load prunes rows past
// resume_at, which is park time + this horizon.
const budgetParkResumeHorizon = llm.DefaultQuotaMaxWait

// budgetTurnPayload is the persistence encoding of a ParkedTurn (F5). The
// "budget" discriminator key is REQUIRED on decode: the quota/throttle
// payload shapes (quotaTurnPayload / throttledTurnPayload) lack it, and the
// strict decode rejects their keys (provider_id, credential_key, model_id)
// as unknown — a foreign row that somehow reached this watcher's Load is
// dropped, never re-executed through the budget router (C-K1 defense in
// depth, mirroring the quota side's strict decode).
type budgetTurnPayload struct {
	Message        string            `json:"message"`
	Parts          []llm.ContentPart `json:"parts,omitempty"`
	ConversationID string            `json:"conversation_id"`
	SourceClient   string            `json:"source_client,omitempty"`
	AgentID        string            `json:"agent_id,omitempty"`
	TurnID         string            `json:"turn_id,omitempty"`
	ParkedAt       time.Time         `json:"parked_at,omitzero"`
	Reason         llm.BudgetLimit   `json:"reason,omitempty"`
	// Kind is the discriminator marker, always "budget" on rows written by
	// this watcher. A budget row decoded by the QUOTA watcher would fail
	// the quota side's DisallowUnknownFields on this key — the partition is
	// mutual.
	Kind string `json:"budget"`
}

// budgetTurnToRecord encodes a ParkedTurn as a ParkedTurnRecord for the
// persistence store. Budget parks carry no provider-failure class (they
// wait on a LOCAL window, not a provider wall), so Class stays
// llm.FailureNone — the parkClassString round-trip ("none") and the store's
// class parser keep it distinguishable from quota ("quota") and throttle
// ("throttle") rows even without the owner column.
func budgetTurnToRecord(turn ParkedTurn, owner string) (ParkedTurnRecord, error) {
	raw, err := json.Marshal(budgetTurnPayload{
		Message:        turn.Message,
		Parts:          turn.Parts,
		ConversationID: turn.ConversationID,
		SourceClient:   turn.SourceClient,
		AgentID:        turn.AgentID,
		TurnID:         turn.TurnID,
		ParkedAt:       turn.ParkedAt,
		Reason:         turn.Limit,
		Kind:           ParkClassBudget,
	})
	if err != nil {
		return ParkedTurnRecord{}, err
	}
	return ParkedTurnRecord{
		ConversationID: turn.ConversationID,
		SessionID:      turn.SessionID,
		AgentID:        turn.AgentID,
		Class:          llm.FailureNone,
		ResumeAt:       turn.ParkedAt.Add(budgetParkResumeHorizon),
		TurnPayload:    raw,
		Owner:          owner,
	}, nil
}

// recordToBudgetTurn decodes a persisted ParkedTurnRecord back into the
// ParkedTurn the resume callback expects. Strict decode (see
// budgetTurnPayload): unknown keys, malformed JSON, a missing/foreign
// discriminator, or an empty payload all return an error, and the caller
// drops the record.
func recordToBudgetTurn(rec ParkedTurnRecord) (ParkedTurn, error) {
	raw := []byte(rec.TurnPayload)
	if len(bytes.TrimSpace(raw)) == 0 {
		return ParkedTurn{}, fmt.Errorf("budget payload empty")
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	var p budgetTurnPayload
	if err := dec.Decode(&p); err != nil {
		return ParkedTurn{}, fmt.Errorf("budget payload strict decode: %w", err)
	}
	if dec.More() {
		return ParkedTurn{}, fmt.Errorf("budget payload strict decode: trailing data after JSON object")
	}
	if p.Kind != ParkClassBudget {
		return ParkedTurn{}, fmt.Errorf("budget payload discriminator = %q, want %q", p.Kind, ParkClassBudget)
	}
	return ParkedTurn{
		SessionID:      rec.SessionID,
		ConversationID: p.ConversationID,
		Message:        p.Message,
		Parts:          p.Parts,
		AgentID:        p.AgentID,
		SourceClient:   p.SourceClient,
		Limit:          p.Reason,
		TurnID:         p.TurnID,
		ParkedAt:       p.ParkedAt,
	}, nil
}

// BudgetResumeWatcher parks chat turns interrupted by budget exhaustion and
// retries them once the budget window clears. It polls the budget on a fixed
// interval; when the budget is no longer exceeded, it drains the parked queue
// (oldest first) by invoking the resume callback supplied by the ChatHandler.
//
// Persistence (F5, tree 04 leaf 04): when a ParkPersistence is wired via
// SetParkPersistence, every accepted Park mirrors its record to the store
// (kind-scoped, stamped with the SetPersistenceOwner identity — daemon
// wiring: owner "budget"), Stop keeps the rows for the next Start, and
// Start re-arms this watcher's OWN rows (expired ones are pruned at Load).
// Each resume deletes the row BEFORE invoking the callback (at-most-once,
// same contract as TurnParker). Without a wired store the watcher stays
// best-effort in memory: turns parked when Stop runs are dropped (logged at
// warn).
//
// Events (F5 observability): when a park event bus is wired via
// SetParkEventBus, park and resume publish on the EXISTING agent.quota_wait
// topic with class "budget" (ParkTurnEvent schema, no new topic, no payload
// reshape), so the TUI/GUI wait labels see budget-parked turns like every
// other park class.
type BudgetResumeWatcher struct {
	budget       *llm.Budget
	logger       *slog.Logger
	pollInterval time.Duration

	// Wiring-time fields (installed once before Start, like the TurnParker's
	// parkEventBus/parkPersistence — read without the mutex afterwards).
	parkEventBus     parkEventBus
	parkPersistence  ParkPersistence
	parkKind         ParkKind
	persistenceOwner string
	// backend, when set via SetParkBackend (tree 20261110 leaf 02,
	// [parking] queue_backend), REPLACES parkPersistence for all storage
	// traffic (C-A1 contract) — the sqlite store stays empty.
	backend parkBackend

	mu         sync.Mutex
	parked     []ParkedTurn
	parkKeys   []string // persistence keys, index-aligned with parked ("" without persistence)
	resumeFunc func(ctx context.Context, turn ParkedTurn)

	cancel context.CancelFunc
	wg     sync.WaitGroup
}

// NewBudgetResumeWatcher creates a watcher. resumeFunc is invoked (in a
// background goroutine) for each parked turn once the budget clears; it is
// responsible for re-running the turn and pushing the result to the session.
// A nil budget or nil resumeFunc disables auto-resume (Park becomes a no-op).
func NewBudgetResumeWatcher(budget *llm.Budget, logger *slog.Logger, resumeFunc func(ctx context.Context, turn ParkedTurn)) *BudgetResumeWatcher {
	if logger == nil {
		logger = slog.Default()
	}
	interval := 30 * time.Second
	return &BudgetResumeWatcher{
		budget:       budget,
		logger:       logger,
		pollInterval: interval,
		resumeFunc:   resumeFunc,
	}
}

// Start begins the background polling loop. Safe to call once; subsequent
// calls are no-ops. With persistence wired (SetParkPersistence), the first
// Start also re-arms turns persisted by the previous run under this
// watcher's owner: survivors are queued, and if the budget already cleared
// they resume immediately (before the first tick).
func (w *BudgetResumeWatcher) Start(ctx context.Context) {
	if w == nil || w.budget == nil || w.resumeFunc == nil {
		return
	}
	w.mu.Lock()
	if w.cancel != nil {
		w.mu.Unlock()
		return // already started
	}
	runCtx, cancel := context.WithCancel(ctx)
	w.cancel = cancel
	w.mu.Unlock()

	w.reArm(ctx)

	w.wg.Go(func() {
		ticker := time.NewTicker(w.pollInterval)
		defer ticker.Stop()
		for {
			select {
			case <-runCtx.Done():
				return
			case <-ticker.C:
				w.drainIfClear(runCtx)
			}
		}
	})
	w.logger.Info("budget resume watcher started", "poll_interval", w.pollInterval)
}

// Stop halts the polling loop and waits for it to exit. With persistence
// wired, parked turns STAY in the store and re-arm on the next Start (F5:
// Stop no longer silently drops them — the rows survive, logged at Info).
// Without persistence they are logged and dropped (legacy warn).
func (w *BudgetResumeWatcher) Stop() {
	if w == nil {
		return
	}
	w.mu.Lock()
	if w.cancel != nil {
		w.cancel()
		w.cancel = nil
	}
	remaining := len(w.parked)
	persisted := w.parkPersistence != nil
	w.mu.Unlock()

	w.wg.Wait()
	if remaining > 0 {
		if persisted {
			w.logger.Info("budget resume watcher stopped — parked turns persisted for next start",
				"parked", remaining,
				"kind", string(w.parkKind),
			)
		} else {
			w.logger.Warn("budget resume watcher stopped with parked turns dropped", "dropped", remaining)
		}
	}
}

// Park enqueues a turn for later retry. Returns false (and does nothing) if
// auto-resume is not configured. With persistence wired, the accepted turn
// is mirrored to the store (best-effort: a save failure logs and the turn
// stays memory-only); with an event bus wired, the park publishes on the
// agent.quota_wait topic with class "budget".
func (w *BudgetResumeWatcher) Park(turn ParkedTurn) bool {
	if w == nil || w.budget == nil || w.resumeFunc == nil {
		return false
	}
	if turn.ParkedAt.IsZero() {
		turn.ParkedAt = time.Now()
	}
	// Persist outside the mutex (store I/O), then append the turn and its
	// key as ONE indexed pair under the mutex — drainIfClear snapshots both
	// slices together, so they must never disagree on length or alignment.
	// A save failure leaves key=="" and the turn stays memory-only for this
	// boot (unpersist no-ops on an empty key).
	key := w.persistPark(turn)
	w.mu.Lock()
	w.parked = append(w.parked, turn)
	w.parkKeys = append(w.parkKeys, key)
	n := len(w.parked)
	w.mu.Unlock()
	w.emitBudgetParkEvent(turn)
	w.logger.Info("parked turn pending budget clearance",
		"session_id", turn.SessionID,
		"queued", n,
	)
	return true
}

// Pending returns the number of currently parked turns.
func (w *BudgetResumeWatcher) Pending() int {
	if w == nil {
		return 0
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.parked)
}

// --- C-K1 wiring seams (F5, tree 04 leaf 04) --------------------------------

// SetParkPersistence wires durable budget-park storage. Wiring-time setter:
// call once before Start (re-arm happens in Start), so the fields are read
// without the mutex afterwards. Nil store or watcher keeps the memory-only
// behaviour. kind scopes the rows (daemon wiring: ParkKindChat; an empty
// kind defaults to it — the budget parker only ever parks chat turns).
func (w *BudgetResumeWatcher) SetParkPersistence(store ParkPersistence, kind ParkKind) {
	if w == nil || store == nil {
		return
	}
	if kind == "" {
		kind = ParkKindChat
	}
	w.parkPersistence = store
	w.parkKind = kind
}

// SetParkBackend installs the C-A1 backend seam (tree 20261110 leaf 02)
// on the budget watcher's own persistence path: with a backend set it
// REPLACES parkPersistence for Save/Delete/Load (C-A1 contract — the
// sqlite store stays empty and only derives keys). Wiring-time setter:
// call once before Start. Nil-guarded per repo convention.
func (w *BudgetResumeWatcher) SetParkBackend(b parkBackend) {
	if w == nil || b == nil {
		return
	}
	w.backend = b
}

// SetPersistenceOwner installs the C-K1 parker identity for budget parks
// (daemon wiring: ParkOwnerBudget = "budget"). Every persisted row is
// stamped with it and the restart re-arm loads ONLY this owner's rows, so
// the quota watcher / throttle parker (which share ParkKindChat) can never
// re-arm budget turns — and vice versa. Empty owner keeps the legacy
// unfiltered Load. Wiring-time setter: call once before Start.
func (w *BudgetResumeWatcher) SetPersistenceOwner(owner string) {
	if w == nil {
		return
	}
	w.persistenceOwner = owner
}

// SetParkEventBus installs the bus the budget park/resume events publish to
// on the EXISTING agent.quota_wait topic with class "budget" (F5 — no new
// topic, no payload reshape: the WS "agent.quota" prefix keeps classifying
// them as agent_progress). Nil disables publishing (the default; parking
// stays silent). Wiring-time setter: call once before Start.
func (w *BudgetResumeWatcher) SetParkEventBus(b parkEventBus) {
	if w == nil || b == nil {
		return
	}
	w.parkEventBus = b
}

// --- persistence internals ---------------------------------------------------

// budgetParkKey derives the persistence key for a freshly parked turn —
// same shape as the TurnParker's chat key (class prefix + session +
// park-time nanoseconds; two different user messages are two turns, never
// deduped), with the budget wire class as the prefix.
func (w *BudgetResumeWatcher) budgetParkKey(turn ParkedTurn) string {
	return ParkClassBudget + "|" + turn.SessionID + "|" + turn.ParkedAt.UTC().Format(chatPersistenceKeyTimeFormat)
}

// persistPark mirrors an accepted park to the store. Best-effort: a failure
// is logged and the turn stays parked in memory only. Returns the key the
// row was saved under ("" when nothing was written, so unpersist no-ops).
func (w *BudgetResumeWatcher) persistPark(turn ParkedTurn) string {
	if w.backend == nil && w.parkPersistence == nil {
		return ""
	}
	rec, err := budgetTurnToRecord(turn, w.persistenceOwner)
	if err != nil {
		w.logger.Warn("budget park persistence: encode failed — turn parked in memory only",
			"session_id", turn.SessionID,
			"error", err,
		)
		return ""
	}
	key := w.budgetParkKey(turn)
	ctx, cancel := context.WithTimeout(context.Background(), parkPersistTimeout)
	defer cancel()
	if w.backend != nil {
		// C-A1 seam (tree 20261110 leaf 02): the backend REPLACES the
		// store path — the sqlite row is never written in backend mode.
		// The queue backend implements keyedParkBackend, so the budget
		// park key survives the Save boundary; a plain-only backend
		// falls back to Save (idempotent-resurrection contract, same
		// posture as the TurnParker's persistPark).
		var err error
		if kb, ok := w.backend.(keyedParkBackend); ok {
			err = kb.SaveKeyed(ctx, w.parkKind, key, rec)
		} else {
			err = w.backend.Save(ctx, rec)
		}
		if err != nil {
			w.logger.Warn("budget park backend: save failed — turn parked in memory only",
				"kind", string(w.parkKind),
				"session_id", turn.SessionID,
				"error", err,
			)
		}
		return key
	}
	if err := w.parkPersistence.Save(ctx, w.parkKind, key, rec); err != nil {
		w.logger.Warn("budget park persistence: save failed — turn parked in memory only",
			"kind", string(w.parkKind),
			"session_id", turn.SessionID,
			"error", err,
		)
	}
	return key
}

// unpersistPark removes a turn's row. Called on resume BEFORE the callback
// (at-most-once resume — the same contract as the TurnParker). Best-effort.
func (w *BudgetResumeWatcher) unpersistPark(key string) {
	if (w.backend == nil && w.parkPersistence == nil) || key == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), parkPersistTimeout)
	defer cancel()
	if w.backend != nil {
		if err := w.backend.Delete(ctx, w.parkKind, key); err != nil {
			w.logger.Warn("budget park backend: delete failed — turn may re-arm on next boot",
				"kind", string(w.parkKind),
				"error", err,
			)
		}
		return
	}
	if err := w.parkPersistence.Delete(ctx, w.parkKind, key); err != nil {
		w.logger.Warn("budget park persistence: delete failed — turn may re-arm on next boot",
			"kind", string(w.parkKind),
			"error", err,
		)
	}
}

// reArm loads this watcher's persisted turns (owner-scoped when the store
// supports it, else kind-scoped) and queues the survivors. Called from
// Start. Load-best-effort: a store failure logs and leaves the queue empty;
// expired rows are pruned by the store itself.
func (w *BudgetResumeWatcher) reArm(ctx context.Context) {
	if w.backend == nil && w.parkPersistence == nil {
		return
	}
	now := time.Now()
	var records []ParkedTurnRecord
	var keys []string
	var err error
	switch {
	case w.backend != nil:
		// C-A1 seam (tree 20261110 leaf 02): loads go to the backend
		// ONLY. A keyed backend (the queue backend) keeps the budget
		// park keys so drain deletes resolve exactly the re-armed rows.
		if kb, ok := w.backend.(keyedParkBackend); ok {
			records, keys, err = kb.LoadOwnedKeyed(ctx, w.parkKind, w.persistenceOwner, now)
		} else {
			records, err = w.backend.LoadOwned(ctx, w.parkKind, w.persistenceOwner)
		}
	case w.persistenceOwner != "":
		if osp, ok := w.parkPersistence.(OwnerScopedPersistence); ok {
			records, keys, err = osp.LoadOwned(ctx, w.parkKind, w.persistenceOwner, now)
		} else {
			records, keys, err = w.parkPersistence.Load(ctx, w.parkKind, now)
		}
	default:
		records, keys, err = w.parkPersistence.Load(ctx, w.parkKind, now)
	}
	if err != nil {
		w.logger.Warn("budget park persistence: re-arm load failed — turns left for next boot",
			"kind", string(w.parkKind),
			"error", err,
		)
		return
	}
	if len(records) == 0 {
		return
	}
	w.mu.Lock()
	rearmed := 0
	for i, rec := range records {
		turn, derr := recordToBudgetTurn(rec)
		if derr != nil {
			// Foreign or malformed payload on this watcher's rows: drop
			// (never re-execute through the wrong router), and remove the
			// row so it cannot be re-armed again.
			var key string
			if i < len(keys) {
				key = keys[i]
			}
			w.mu.Unlock()
			w.logger.Error("budget park persistence: re-armed row undecodable — dropping",
				"session_id", rec.SessionID,
				"error", derr,
			)
			w.unpersistPark(key)
			w.mu.Lock()
			continue
		}
		w.parked = append(w.parked, turn)
		var key string
		if i < len(keys) {
			key = keys[i]
		}
		w.parkKeys = append(w.parkKeys, key)
		rearmed++
	}
	n := len(w.parked)
	w.mu.Unlock()
	if rearmed > 0 {
		w.logger.Info("budget park persistence: re-armed turns from previous run",
			"kind", string(w.parkKind),
			"count", rearmed,
			"queued", n,
		)
	}
}

// --- park/resume events (F5) -------------------------------------------------

// publishBudgetEvent marshals ev and publishes it on the agent.quota_wait
// topic when a park event bus is wired. Best-effort like every parker side
// channel: a marshal failure is logged at warn and never affects parking.
func (w *BudgetResumeWatcher) publishBudgetEvent(ev ParkTurnEvent) {
	if w == nil || w.parkEventBus == nil {
		return
	}
	msg, err := models.NewBusMessage(models.MessageTypeEvent, ev.AgentID, ev)
	if err != nil {
		w.logger.Warn("budget park event marshal failed — event dropped",
			"reason", ev.Reason,
			"error", err,
		)
		return
	}
	msg.Topic = "agent.quota_wait"
	w.parkEventBus.Publish("agent.quota_wait", msg)
}

// emitBudgetParkEvent publishes the budget park event: reason
// "budget_wait", class "budget", to=quota_wait (the label both surfaces
// already render), unblock_at carrying the bounded give-up horizon so the
// wait label shows an honest expiry rather than an open-ended promise.
func (w *BudgetResumeWatcher) emitBudgetParkEvent(turn ParkedTurn) {
	w.publishBudgetEvent(ParkTurnEvent{
		AgentID:   turn.AgentID,
		To:        "quota_wait",
		Reason:    ReasonBudgetWait,
		Class:     ParkClassBudget,
		UnblockAt: turn.ParkedAt.Add(budgetParkResumeHorizon).Format(time.RFC3339),
		ResumeAt:  turn.ParkedAt.Add(budgetParkResumeHorizon).Format(time.RFC3339),
		SessionID: turn.SessionID,
		Timestamp: time.Now(),
	})
}

// emitBudgetResumeEvent publishes the budget resume event: reason
// "budget_resumed", class "budget", to=running, waited measuring the real
// park -> resume interval.
func (w *BudgetResumeWatcher) emitBudgetResumeEvent(turn ParkedTurn) {
	parkedAt := turn.ParkedAt
	if parkedAt.IsZero() {
		parkedAt = time.Now()
	}
	w.publishBudgetEvent(ParkTurnEvent{
		AgentID:   turn.AgentID,
		To:        "running",
		Reason:    ReasonBudgetResumed,
		Class:     ParkClassBudget,
		Waited:    time.Since(parkedAt).String(),
		SessionID: turn.SessionID,
		Timestamp: time.Now(),
	})
}

// drainIfClear checks the budget and, if it is no longer exceeded, resumes all
// parked turns oldest-first. Each resume runs in its own goroutine so a slow
// turn does not block the polling loop.
func (w *BudgetResumeWatcher) drainIfClear(ctx context.Context) {
	// Read the budget pointer under mu: tests (and hot-swaps) may replace
	// w.budget concurrently with this read on the polling goroutine.
	w.mu.Lock()
	budget := w.budget
	resume := w.resumeFunc
	w.mu.Unlock()
	if budget == nil || budget.CheckBudget().Exceeded {
		return // still over budget; wait for next tick
	}

	w.mu.Lock()
	pending := w.parked
	pendingKeys := w.parkKeys
	w.parked = nil
	w.parkKeys = nil
	w.mu.Unlock()

	if len(pending) == 0 {
		return
	}

	w.logger.Info("budget cleared — resuming parked turns", "count", len(pending))
	for i, turn := range pending {
		// At-most-once resume (F5, same contract as the TurnParker): the
		// persisted row goes BEFORE the callback, so a crash mid-resume
		// can never re-arm the turn a second time.
		var key string
		if i < len(pendingKeys) {
			key = pendingKeys[i]
		}
		w.unpersistPark(key)
		w.emitBudgetResumeEvent(turn)
		w.wg.Go(func() {
			resume(ctx, turn)
		})
	}
}
