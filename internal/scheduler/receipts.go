package scheduler

import (
	"context"
	"fmt"
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
		if ts.Before(cutoff) {
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

// recoverInFlightDeliveries re-delivers receipts left pending/in_flight by a
// previous run (crash between claim and session ack). Recovery scans
// RECEIPTS, not ticks: the claimed tick will not re-fire, so this is the
// only at-least-once path. Re-delivery reuses the SAME MessageID, which the
// chat.request handler treats idempotently via turn_id dedupe.
func (s *Scheduler) recoverInFlightDeliveries() error {
	for _, cfg := range s.store.List() {
		if cfg.LastDelivery == nil {
			continue
		}
		stuck := *cfg.LastDelivery
		if stuck.Status != DeliveryPending && stuck.Status != DeliveryInFlight {
			continue
		}
		if cfg.AgentConfig == nil || cfg.AgentConfig.TargetSessionID == "" {
			s.logger.Warn("scheduler: skipping receipt recovery for job without a target session",
				"job_id", cfg.ID,
				"message_id", stuck.MessageID,
			)
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

		s.logger.Info("scheduler: recovering in-flight delivery",
			"job_id", cfg.ID,
			"message_id", stuck.MessageID,
			"target_session_id", stuck.TargetSessionID,
		)

		if err := agentJob.deliver(context.Background(), stuck.MessageID); err != nil {
			// Leave the receipt in_flight so the next Start retries.
			s.logger.Warn("scheduler: in-flight delivery recovery failed; receipt stays re-dispatchable",
				"job_id", cfg.ID,
				"message_id", stuck.MessageID,
				"error", err,
			)
			continue
		}

		stuck.Status = DeliveryDelivered
		stuck.DeliveredAt = time.Now().UTC()
		if err := s.recordDelivery(cfg.ID, stuck); err != nil {
			s.logger.Warn("scheduler: failed to persist recovered-delivery receipt",
				"job_id", cfg.ID,
				"error", err,
			)
		}
	}
	return nil
}
