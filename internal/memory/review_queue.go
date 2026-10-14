package memory

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"
)

// VerdictAuditor receives one audit record per claim verdict (promote/reject).
// Implemented in internal/daemon wiring by an adapter over *auditlog.Store —
// the memory package must not import internal/auditlog (leaf T03-L1, mirroring
// the internal/employee injection pattern). A returned error propagates to the
// verdict caller: the audit entry is durable-then-acknowledged.
type VerdictAuditor interface {
	RecordVerdict(claimID, verdict, reasonHash string) error
}

// MaxVerdictReasonBytes caps the verdict reason stored in claim metadata —
// same cap as the usefulness-vote reason (memory_vote convention).
const MaxVerdictReasonBytes = 512

// ErrVerdictReasonTooLong is returned when a verdict reason exceeds
// MaxVerdictReasonBytes.
var ErrVerdictReasonTooLong = errors.New("verdict reason exceeds 512 bytes")

// SetVerdictAuditor wires the auditlog adapter that receives one entry per
// verdict. Nil-safe: a nil auditor disables verdict auditing (best-effort —
// auditing is never required for a verdict to proceed).
func (m *Manager) SetVerdictAuditor(a VerdictAuditor) {
	if a == nil {
		return
	}
	m.mu.Lock()
	m.verdictAuditor = a
	m.mu.Unlock()
}

// RecordVerdict delivers the verdict to the wired auditor. Returns an error
// when an auditor is wired and rejects the entry (durable-then-ack: the
// verdict transition is aborted before the status write so the audit chain
// never misses a verdict that was acknowledged).
func (m *Manager) RecordVerdict(claimID, verdict, reasonHash string) error {
	m.mu.RLock()
	a := m.verdictAuditor
	m.mu.RUnlock()
	if a == nil {
		return nil
	}
	if err := a.RecordVerdict(claimID, verdict, reasonHash); err != nil {
		return fmt.Errorf("record verdict audit: %w", err)
	}
	return nil
}

// ReasonHash returns the first 16 hex chars of sha256(reason). An empty
// reason hashes to the sha256 of the empty string (stable sentinel).
func ReasonHash(reason string) string {
	sum := sha256.Sum256([]byte(reason))
	return hex.EncodeToString(sum[:])[:16]
}

// ListAutoClaimsFiltered returns status=auto claims matching the filter
// options, plus the TOTAL count of matching claims (before offset/limit) so
// callers can paginate. Ordering follows Search (relevance, then created_at
// descending).
//
// MinConfidence/MaxConfidence are inclusive bounds over the claim's
// extraction-time confidence. OlderThan keeps only claims created before
// now-OlderThan (stale claims first for review). Limit <= 0 defaults to 20;
// Offset < 0 is treated as 0.
func (m *Manager) ListAutoClaimsFiltered(ctx context.Context, opts ListAutoClaimsOptions) (items []MemoryResult, total int, err error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return nil, 0, errors.New("memory manager not initialized")
	}

	limit := opts.Limit
	if limit <= 0 {
		limit = 20
	}
	offset := max(opts.Offset, 0)

	results, err := m.Search(ctx, MemoryQuery{
		Type:  MemoryTypeClaim,
		Limit: maxSearchLimit,
	})
	if err != nil {
		return nil, 0, fmt.Errorf("search auto claims: %w", err)
	}

	var olderThanCutoff time.Time
	if opts.OlderThan != nil {
		olderThanCutoff = time.Now().Add(-*opts.OlderThan)
	}

	matched := make([]MemoryResult, 0, len(results))
	for _, r := range results {
		if r.Memory.Type != MemoryTypeClaim {
			continue
		}
		if asString(r.Memory.Metadata["status"]) != string(ClaimStatusAuto) {
			continue
		}
		conf := confFloat(r.Memory.Metadata["confidence"])
		if opts.MinConfidence != nil && (conf < 0 || conf < *opts.MinConfidence) {
			continue
		}
		if opts.MaxConfidence != nil && (conf < 0 || conf > *opts.MaxConfidence) {
			continue
		}
		if !olderThanCutoff.IsZero() && !r.Memory.CreatedAt.Before(olderThanCutoff) {
			continue
		}
		matched = append(matched, r)
	}

	total = len(matched)
	if offset >= total {
		return nil, total, nil
	}
	end := min(offset+limit, total)
	return matched[offset:end], total, nil
}

// maxSearchLimit is the broad-search window used by the filtered review
// query. Generous but bounded: claims beyond this window are invisible to the
// review queue until older ones are verdict-ed or purged.
const maxSearchLimit = 10000
