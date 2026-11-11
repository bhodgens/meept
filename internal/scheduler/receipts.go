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
// history only; a stuck row survives until recovery resolves it. The
// count bound still applies (bounded memory), keeping newest stuck rows.
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
	if len(kept) > MaxDeliveryHistoryRecords {
		pruned = true
		kept = kept[len(kept)-MaxDeliveryHistoryRecords:]
	}
	h.Records = kept
	if pruned {
		h.EarlierRecordsPruned = true
	}
}

// recordDelivery persists a delivery receipt on the job's JobConfig: the
// receipt is appended to the bounded DeliveryHistory and mirrored into
// LastDelivery. The job must exist in the store.
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
	last := r
	cfg.LastDelivery = &last
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
// only at-least-once path. Re-delivery reuses the SAME MessageID, which the
// chat.request handler treats idempotently via turn_id dedupe.
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
			if deliveredIDs[r.MessageID] {
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
			r.Status = DeliveryDelivered
			r.DeliveredAt = time.Now().UTC()
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
