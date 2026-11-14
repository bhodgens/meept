package scheduler

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// Delivery receipt + session inbox bookkeeping (leaf 05-schedule/02-delivery-inbox).
//
// A scheduled agent job that targets a session (AgentConfig.TargetSessionID)
// is no longer fire-and-forget: every delivery is tracked by a durable
// ScheduleDeliveryReceipt whose status moves pending -> in_flight ->
// delivered. The tick claim store (claims.go) remains the at-least-once
// backstop for ticks; recovery of interrupted DELIVERIES is driven by
// RECEIPT rows (a claimed tick that crashed before the session ack will not
// re-fire, so recoverInFlightDeliveries re-delivers stuck receipts with the
// same MessageID — idempotent via the chat.request turn_id dedupe).

// DeliveryStatus is the lifecycle state of a scheduled-message delivery.
type DeliveryStatus string

const (
	// DeliveryPending: the occurrence is due but dispatch has not started.
	DeliveryPending DeliveryStatus = "pending"
	// DeliveryInFlight: dispatched (or about to be) but not yet acked by the
	// target session. A crash here is recoverable via re-delivery.
	DeliveryInFlight DeliveryStatus = "in_flight"
	// DeliveryDelivered: the publish to chat.request succeeded.
	DeliveryDelivered DeliveryStatus = "delivered"
)

// ScheduleDeliveryReceipt records one delivery attempt of a scheduled
// message to a target session's inbox.
type ScheduleDeliveryReceipt struct {
	ScheduleID      string         `json:"schedule_id"`
	ScheduledAt     time.Time      `json:"scheduled_at"`
	OccurrenceAt    time.Time      `json:"occurrence_at,omitzero"`
	DeliveredAt     time.Time      `json:"delivered_at,omitzero"`
	MessageID       string         `json:"message_id"`
	TargetSessionID string         `json:"target_session_id"`
	Prompt          string         `json:"prompt,omitempty"`
	Status          DeliveryStatus `json:"status"`

	// RecoveryNote is the audit trail for in-place reconciliation: when
	// recoverInFlightDeliveries flips a stuck row (or skips a re-delivery
	// because the MessageID is already delivered), the reason is recorded
	// here. Empty for normal dispatch-path deliveries.
	RecoveryNote string `json:"recovery_note,omitempty"`
}

// MaxDeliveryHistoryRecords bounds the per-job delivery history length.
const MaxDeliveryHistoryRecords = 200

// DeliveryHistoryRetentionDays bounds the per-job delivery history age.
const DeliveryHistoryRetentionDays = 30

// DeliveryHistory is a bounded, most-recent-first-kept history of delivery
// receipts for one schedule.
type DeliveryHistory struct {
	Records              []ScheduleDeliveryReceipt `json:"records"`
	EarlierRecordsPruned bool                      `json:"earlier_records_pruned"`

	// LastEvictedStuckIDs carries the MessageIDs of any pending/in_flight
	// rows evicted by the most recent Append as a last-resort count-bound
	// eviction (bughunt 2026-11-12 F4). The caller (recordDelivery) Warns
	// naming each so the lost occurrence is at least observable. Empty
	// when the last Append evicted only terminal rows (or nothing).
	LastEvictedStuckIDs []string `json:"last_evicted_stuck_ids,omitempty"`
}

// NewDeliveryHistory returns an empty delivery history.
func NewDeliveryHistory() *DeliveryHistory {
	return &DeliveryHistory{Records: make([]ScheduleDeliveryReceipt, 0, 16)}
}

// Append adds a receipt and enforces the retention bounds: records older
// than DeliveryHistoryRetentionDays (measured on DeliveredAt, falling back
// to ScheduledAt) and records beyond the MaxDeliveryHistoryRecords most
// recent are dropped. Any drop sets EarlierRecordsPruned.
//
// Non-terminal statuses (pending/in_flight) are EXEMPT from the age prune
// (bughunt 2026-11-10 B8): recoverInFlightDeliveries re-delivers FROM
// pending/in_flight rows, so pruning a stuck receipt older than retention
// permanently lost the tick it represents. Age-retention is for delivered
// history only; a stuck row survives until recovery resolves it.
//
// The count bound evicts TERMINAL rows (delivered/failed) first. A stuck
// row is evicted only as a last resort — when the bound still overflows
// with nothing terminal left — and that case is flagged so the caller can
// Warn naming the MessageID (bughunt 2026-11-12 F4): silently evicting a
// pending/in_flight row loses the occurrence it represents.
func (h *DeliveryHistory) Append(r ScheduleDeliveryReceipt) {
	h.Records = append(h.Records, r)

	cutoff := time.Now().UTC().AddDate(0, 0, -DeliveryHistoryRetentionDays)
	kept := make([]ScheduleDeliveryReceipt, 0, len(h.Records))
	pruned := false
	for _, rec := range h.Records {
		ts := rec.DeliveredAt
		if ts.IsZero() {
			ts = rec.ScheduledAt
		}
		stuck := rec.Status == DeliveryPending || rec.Status == DeliveryInFlight
		if ts.Before(cutoff) && !stuck {
			pruned = true
			continue
		}
		kept = append(kept, rec)
	}

	// Count-bound overflow: evict OLDEST TERMINAL rows first; stuck rows
	// are last-resort evictions recorded for a Warn. Overflow beyond the
	// terminal pool is impossible in practice (Append adds one row per
	// call), but the defensive slice keeps the bound exact.
	evictedStuck := []string{}
	if len(kept) > MaxDeliveryHistoryRecords {
		pruned = true
		overflow := len(kept) - MaxDeliveryHistoryRecords

		// Pass 1: evict oldest terminal rows.
		evicted := 0
		keptTerminalPass := make([]ScheduleDeliveryReceipt, 0, len(kept))
		for _, rec := range kept {
			stuck := rec.Status == DeliveryPending || rec.Status == DeliveryInFlight
			if !stuck && evicted < overflow {
				evicted++
				continue
			}
			keptTerminalPass = append(keptTerminalPass, rec)
		}

		// Pass 2 (last resort): evict oldest stuck rows, naming each.
		if len(keptTerminalPass) > MaxDeliveryHistoryRecords {
			stillOver := len(keptTerminalPass) - MaxDeliveryHistoryRecords
			evicted = 0
			final := make([]ScheduleDeliveryReceipt, 0, len(keptTerminalPass))
			for _, rec := range keptTerminalPass {
				stuck := rec.Status == DeliveryPending || rec.Status == DeliveryInFlight
				if stuck && evicted < stillOver {
					evicted++
					evictedStuck = append(evictedStuck, rec.MessageID)
					continue
				}
				final = append(final, rec)
			}
			keptTerminalPass = final
		}
		kept = keptTerminalPass
	}

	h.Records = kept
	h.LastEvictedStuckIDs = evictedStuck
	if pruned {
		h.EarlierRecordsPruned = true
	}
}

// recordDelivery persists a delivery receipt on the job's JobConfig: the
// receipt is appended to the bounded DeliveryHistory and mirrored into
// LastDelivery. The job must exist in the store.
//
// When the count bound forced a last-resort eviction of a pending/in_flight
// row (bughunt 2026-11-12 F4), every evicted MessageID is Warn'd — the
// occurrence it represented is no longer recoverable.
func (s *Scheduler) recordDelivery(jobID string, r ScheduleDeliveryReceipt) error {
	cfg, ok := s.store.Get(jobID)
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	if r.ScheduledAt.IsZero() {
		r.ScheduledAt = time.Now().UTC()
	}
	if cfg.DeliveryHistory == nil {
		cfg.DeliveryHistory = NewDeliveryHistory()
	}
	cfg.DeliveryHistory.Append(r)
	for _, mid := range cfg.DeliveryHistory.LastEvictedStuckIDs {
		s.logger.Warn("scheduler: delivery history cap evicted a stuck receipt — its occurrence is no longer recoverable",
			"job_id", jobID,
			"message_id", mid,
		)
	}
	last := r
	cfg.LastDelivery = &last
	return s.store.Update(cfg)
}

// hasDeliveredReceiptForMessageID reports whether any DELIVERED receipt in
// the job's history (or the LastDelivery mirror) carries the given
// MessageID. Recovery consults this BEFORE re-publishing (bughunt
// 2026-11-12 F2): the chat.request handler registers the turn with no
// prior-completion check, so a recovered MessageID whose turn already
// completed would re-execute the turn — the dedupe-that-isn't. Only a
// delivered status counts; a stuck row for the same MessageID does not.
func hasDeliveredReceiptForMessageID(cfg JobConfig, messageID string) bool {
	if messageID == "" {
		return false
	}
	if cfg.DeliveryHistory != nil {
		for _, r := range cfg.DeliveryHistory.Records {
			if r.MessageID == messageID && r.Status == DeliveryDelivered {
				return true
			}
		}
	}
	return cfg.LastDelivery != nil &&
		cfg.LastDelivery.MessageID == messageID &&
		cfg.LastDelivery.Status == DeliveryDelivered
}

// reconcileStuckReceipt flips a stuck receipt row to delivered IN PLACE in
// the persisted history (bughunt 2026-11-12 F1): recordDelivery only
// APPENDS, so a recovery re-delivery previously left the original
// pending/in_flight row stuck forever and every daemon start re-delivered
// the same MessageID. The new delivered row is appended alongside (audit
// trail of the recovery delivery); the original row carries a
// RecoveryNote and is terminal, so the next recovery pass no longer sees
// it stuck.
func (s *Scheduler) reconcileStuckReceipt(jobID string, stuck ScheduleDeliveryReceipt, note string) error {
	cfg, ok := s.store.Get(jobID)
	if !ok {
		return fmt.Errorf("job not found: %s", jobID)
	}
	reconciled := false
	if cfg.DeliveryHistory != nil {
		for i := range cfg.DeliveryHistory.Records {
			rec := cfg.DeliveryHistory.Records[i]
			if rec.MessageID == stuck.MessageID &&
				(rec.Status == DeliveryPending || rec.Status == DeliveryInFlight) {
				rec.Status = DeliveryDelivered
				rec.DeliveredAt = stuck.DeliveredAt
				rec.RecoveryNote = note
				cfg.DeliveryHistory.Records[i] = rec
				reconciled = true
			}
		}
	}
	// The LastDelivery mirror can be the stuck row itself (absent from
	// history only in the B2 edge). Reconcile it too.
	if cfg.LastDelivery != nil &&
		cfg.LastDelivery.MessageID == stuck.MessageID &&
		(cfg.LastDelivery.Status == DeliveryPending || cfg.LastDelivery.Status == DeliveryInFlight) {
		m := *cfg.LastDelivery
		m.Status = DeliveryDelivered
		m.DeliveredAt = stuck.DeliveredAt
		m.RecoveryNote = note
		cfg.LastDelivery = &m
		reconciled = true
	}
	if !reconciled {
		return nil // nothing stuck to reconcile; caller still appends its row
	}
	return s.store.Update(cfg)
}

// Shutdown is a cleanup alias for Stop: the delivery-inbox gate test uses it
// in a t.Cleanup to release the claim store after Start.
func (s *Scheduler) Shutdown(ctx context.Context) error {
	return s.Stop(ctx)
}

// receiptAttemptTime is the ordering key for recovery: when a receipt was
// attempted. DeliveredAt (for already-delivered rows, never recovered) falls
// back to OccurrenceAt, then ScheduledAt.
func receiptAttemptTime(r ScheduleDeliveryReceipt) time.Time {
	switch {
	case !r.DeliveredAt.IsZero():
		return r.DeliveredAt
	case !r.OccurrenceAt.IsZero():
		return r.OccurrenceAt
	default:
		return r.ScheduledAt
	}
}

// recoverInFlightDeliveries re-delivers receipts left pending/in_flight by a
// previous run (crash between claim and session ack). Recovery scans
// RECEIPTS, not ticks: the claimed tick will not re-fire, so this is the
// only at-least-once path. Re-delivery reuses the SAME MessageID.
//
// Bughunt 2026-11-12 F2 (dedupe-that-isn't): BEFORE re-publishing, the
// MessageID is checked against DELIVERED history. The chat.request handler
// (internal/agent) registers a turn with no prior-completion check, so a
// recovered MessageID whose turn already completed would re-execute it.
// A MessageID that already has a delivered receipt is skipped entirely —
// the stuck rows are reconciled to delivered in place with an audit note
// (also fixing F1's every-restart re-delivery) instead of re-sent.
//
// M-5 / C-S1 (audit scheduler F5): walks the FULL DeliveryHistory per job,
// not cfg.LastDelivery only — a stuck earlier receipt hidden behind a newer
// delivered mirror must still be recovered. Stuck receipts are re-delivered
// in attempt-time order, deduped by MessageID within one recovery run
// (pending and in_flight rows for the same occurrence count once);
// delivered receipts are never touched.
func (s *Scheduler) recoverInFlightDeliveries() error {
	for _, cfg := range s.store.List() {
		// Collect every stuck receipt for this job, oldest attempt first.
		var stuck []ScheduleDeliveryReceipt
		if cfg.DeliveryHistory != nil {
			for _, r := range cfg.DeliveryHistory.Records {
				if r.Status == DeliveryPending || r.Status == DeliveryInFlight {
					stuck = append(stuck, r)
				}
			}
		}
		// The LastDelivery mirror is normally also a history row; include
		// it only if it's stuck and absent from history (dedup by MessageID
		// below makes double-counting impossible).
		if cfg.LastDelivery != nil {
			ls := *cfg.LastDelivery
			if ls.Status == DeliveryPending || ls.Status == DeliveryInFlight {
				seen := false
				for _, r := range stuck {
					if r.MessageID == ls.MessageID {
						seen = true
						break
					}
				}
				if !seen {
					stuck = append(stuck, ls)
				}
			}
		}
		if len(stuck) == 0 {
			continue
		}
		sort.SliceStable(stuck, func(i, j int) bool {
			return receiptAttemptTime(stuck[i]).Before(receiptAttemptTime(stuck[j]))
		})

		// F2 guard: any stuck MessageID that already has a DELIVERED
		// receipt in this job's history is a completed turn — re-publishing
		// it would re-execute the turn (chat.request has no
		// prior-completion dedupe). Reconcile the stuck rows in place and
		// skip before any publish.
		skipDelivered := make(map[string]bool, len(stuck))
		for _, r := range stuck {
			if hasDeliveredReceiptForMessageID(cfg, r.MessageID) {
				skipDelivered[r.MessageID] = true
			}
		}
		if len(skipDelivered) > 0 {
			note := "recovery skipped: message already delivered (turn completed; chat.request re-publish would re-execute it)"
			if err := s.reconcileStuckReceiptsByIDs(cfg.ID, skipDelivered, note, stuck); err != nil {
				s.logger.Warn("scheduler: failed to persist already-delivered reconciliation",
					"job_id", cfg.ID,
					"error", err,
				)
				// Leave the rows stuck; the guard will re-skip next pass.
			}
			for mid := range skipDelivered {
				s.logger.Info("scheduler: recovery skipped already-delivered message",
					"job_id", cfg.ID,
					"message_id", mid,
				)
			}
		}

		if cfg.AgentConfig == nil || cfg.AgentConfig.TargetSessionID == "" {
			for _, r := range stuck {
				s.logger.Warn("scheduler: skipping receipt recovery for job without a target session",
					"job_id", cfg.ID,
					"message_id", r.MessageID,
				)
			}
			continue
		}

		s.mu.RLock()
		job, ok := s.jobs[cfg.ID]
		s.mu.RUnlock()
		if !ok {
			continue
		}
		agentJob, ok := job.(*AgentJob)
		if !ok {
			continue
		}

		deliveredIDs := make(map[string]bool, len(stuck))
		// Re-read the persisted history as we go: a successful re-delivery
		// flips the row to delivered, so a later stuck row sharing the same
		// MessageID (pending+in_flight pair for one occurrence) must not be
		// sent twice in this run.
		for _, r := range stuck {
			if deliveredIDs[r.MessageID] || skipDelivered[r.MessageID] {
				continue
			}

			s.logger.Info("scheduler: recovering in-flight delivery",
				"job_id", cfg.ID,
				"message_id", r.MessageID,
				"target_session_id", r.TargetSessionID,
			)

			if err := agentJob.deliver(context.Background(), r.MessageID); err != nil {
				// Leave the receipt in_flight/pending so the next Start retries.
				s.logger.Warn("scheduler: in-flight delivery recovery failed; receipt stays re-dispatchable",
					"job_id", cfg.ID,
					"message_id", r.MessageID,
					"error", err,
				)
				continue
			}

			deliveredIDs[r.MessageID] = true
			now := time.Now().UTC()
			// F1: reconcile the original stuck row(s) to delivered IN PLACE
			// with an audit note so the next recovery pass no longer sees
			// them stuck (recordDelivery below only APPENDS — without this
			// every daemon start re-delivered the same MessageID).
			reconcileNote := "recovery re-delivery succeeded; original stuck receipt reconciled in place"
			if err := s.reconcileStuckReceipt(cfg.ID, ScheduleDeliveryReceipt{
				MessageID:   r.MessageID,
				DeliveredAt: now,
			}, reconcileNote); err != nil {
				s.logger.Warn("scheduler: failed to persist stuck-row reconciliation",
					"job_id", cfg.ID,
					"message_id", r.MessageID,
					"error", err,
				)
			}
			r.Status = DeliveryDelivered
			r.DeliveredAt = now
			r.RecoveryNote = reconcileNote
			if err := s.recordDelivery(cfg.ID, r); err != nil {
				s.logger.Warn("scheduler: failed to persist recovered-delivery receipt",
					"job_id", cfg.ID,
					"error", err,
				)
			}
		}
	}
	return nil
}

// reconcileStuckReceiptsByIDs flips the stuck rows whose MessageID is in
// ids to delivered, stamping note with the time each stuck row was
// attempted (attempt-time is the honest "delivered" timestamp for a skipped
// re-delivery). Used by the F2 skip path where NO publish happens but the
// stuck rows must stop being seen as stuck by the next recovery pass.
func (s *Scheduler) reconcileStuckReceiptsByIDs(jobID string, ids map[string]bool, note string, stuck []ScheduleDeliveryReceipt) error {
	var lastErr error
	for _, r := range stuck {
		if !ids[r.MessageID] {
			continue
		}
		deliveredAt := receiptAttemptTime(r)
		if err := s.reconcileStuckReceipt(jobID, ScheduleDeliveryReceipt{
			MessageID:   r.MessageID,
			DeliveredAt: deliveredAt,
		}, note); err != nil {
			lastErr = err
		}
	}
	return lastErr
}

// recoverClaimedTicksWithoutReceipt re-delivers claimed ticks that have NO
// delivery receipt (bughunt 2026-11-10 B2). A crash in the window between
// ClaimTick (claims.go, scheduler_claims.db) and the first receipt write
// (AgentJob.Execute -> recordDelivery, jobs.json) permanently lost the tick:
// recoverInFlightDeliveries scans receipts only, and ProcessMissedTicks
// skips already-claimed ticks. This sweep walks the claim store, matches
// each claim against the job's DeliveryHistory (a delivered receipt for
// the tick's occurrence means the delivery actually completed — the crash
// was AFTER delivery), and re-delivers the rest exactly once per pass
// (at-least-once; downstream turn_id dedupe absorbs duplicates).
//
// R2 (regression-review 2026-11-11): the at-least-once guarantee honestly
// applies ONLY to receipt-writing (session-pinned) jobs. A legacy
// fire-and-forget agent job (AgentConfig.TargetSessionID == "",
// jobs.go Execute) writes NO receipts and mints a FRESH turn id per
// delivery, so hasReceiptForTick is permanently false for every claim —
// sweeping it re-delivered every historical claim on EVERY daemon Start
// with a new, dedupe-proof turn id. Receipt-less job kinds are excluded
// from this sweep (logged); their claim rows simply age out through
// pruneExpiredClaims.
func (s *Scheduler) recoverClaimedTicksWithoutReceipt() error {
	if s.claims == nil {
		return nil
	}
	cutoff := time.Now().UTC().AddDate(0, 0, -s.claimRetentionDays)
	claims, err := s.claims.ListClaimsSince(cutoff)
	if err != nil {
		return err
	}
	for _, c := range claims {
		cfg, ok := s.store.Get(c.JobID)
		if !ok {
			continue // job deleted since the claim; nothing to recover
		}
		if !jobKindWritesReceipts(cfg) {
			// Fire-and-forget (or non-agent) job: no receipts are ever
			// written, so no-receipt says nothing about delivery — the
			// sweep cannot distinguish a lost tick from a delivered one
			// and would re-deliver with an un-dedupable fresh turn id on
			// every Start. Skip with a log (debug: this fires for every
			// legacy claim on every boot).
			s.logger.Debug("scheduler: skipping claim recovery for receipt-less job kind",
				"job_id", c.JobID,
				"tick", c.Tick,
				"reason", "job delivery mode records no receipts (fire-and-forget); no at-least-once recovery possible",
			)
			continue
		}
		if hasReceiptForTick(cfg, c.Tick) {
			continue // receipt exists: recoverInFlightDeliveries owns it
		}
		s.mu.RLock()
		job, known := s.jobs[c.JobID]
		s.mu.RUnlock()
		if !known {
			continue
		}
		s.logger.Info("scheduler: recovering claimed tick with no delivery receipt",
			"job_id", c.JobID,
			"tick", c.Tick,
		)
		// PreClaimed: the tick is already claimed — re-claiming would
		// dedupe-skip. Exactly one pass per recovery; delivery failure
		// leaves no receipt, so the NEXT Start retries (at-least-once).
		s.dispatchMissed(job, c.Tick, 0)
	}
	return nil
}

// jobKindWritesReceipts reports whether the job config's delivery mode
// records durable delivery receipts. Only session-pinned agent jobs
// (AgentConfig.TargetSessionID set) write them — AgentJob.Execute (jobs.go)
// skips every recordDelivery call for fire-and-forget dispatch and the
// non-agent job kinds (shell/reminder/optimization/security/learning) have
// no delivery-receipt path at all. Recovery sweeps consult this so the
// at-least-once claim stays honest: a job that never writes receipts has no
// no-receipt signal for the sweep to act on.
func jobKindWritesReceipts(cfg JobConfig) bool {
	return cfg.AgentConfig != nil && cfg.AgentConfig.TargetSessionID != ""
}

// hasReceiptForTick reports whether the job's delivery history carries a
// receipt for the given tick — matched on OccurrenceAt, falling back to
// ScheduledAt (the two timestamps AgentJob.Execute stamps). Any status
// counts: a pending/in_flight receipt means the crash happened AFTER the
// first write, and recoverInFlightDeliveries re-delivers those with
// turn_id dedupe; delivered means it completed.
func hasReceiptForTick(cfg JobConfig, tick time.Time) bool {
	if cfg.DeliveryHistory != nil {
		for _, r := range cfg.DeliveryHistory.Records {
			if receiptMatchesTick(r, tick) {
				return true
			}
		}
	}
	if cfg.LastDelivery != nil && receiptMatchesTick(*cfg.LastDelivery, tick) {
		return true
	}
	return false
}

func receiptMatchesTick(r ScheduleDeliveryReceipt, tick time.Time) bool {
	if !r.OccurrenceAt.IsZero() && receiptTimesEqual(r.OccurrenceAt, tick) {
		return true
	}
	return receiptTimesEqual(r.ScheduledAt, tick)
}

// receiptTimesEqual compares two timestamps at the claim store's stored
// precision (milliseconds — claimTickTimeFormat carries exactly 9 digits
// but wall-clock writes round-trip through RFC3339Nano; a sub-ms drift
// would otherwise miss the match). Zero times never match.
func receiptTimesEqual(a, b time.Time) bool {
	if a.IsZero() || b.IsZero() {
		return false
	}
	return a.UnixMilli() == b.UnixMilli()
}
