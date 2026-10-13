package memory

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/memory/conflict"
)

// _rfc3339UTC mirrors the RFC3339 stamping convention used by StoreClaim.
const _rfc3339UTC = time.RFC3339

// Conflict wiring (plan forest 20261012-memory-capability-forest, tree 02
// leaf L2): adapts the Manager to the dependency-free conflict.Detector and
// applies the frozen consumer rule per Decision. The conflict package only
// DECIDES — every write happens here, through the Manager's existing claim
// and supersede paths.

// conflictActionMeta is the claim-metadata key marking claims created by a
// conflict decision ("merge" or "supersede"). Read by ListMergeClaims (the
// memory.listConflicts RPC behind `meept memory conflicts`).
const conflictActionMeta = "conflict_action"

// conflictTargetsMeta records the superseded target claim IDs on the
// successor claim created by a merge/supersede decision.
const conflictTargetsMeta = "conflict_targets"

// ConflictWiring is the optional conflict-detection configuration handed to
// the DistillWorker. Nil (or Enabled=false) bypasses detection entirely: no
// recall, no LLM call (SHARED-CONVENTIONS §2.3).
type ConflictWiring struct {
	// Enabled turns conflict detection on for L1 extraction batches.
	Enabled bool
	// TopK caps recalled existing claims per candidate (default 5).
	TopK int
	// MaxBatch caps candidates per conflict LLM call (default 20).
	MaxBatch int
	// LLM performs the conflict-detection completion (the memory_model slot
	// chatter, adapted to conflict.LLM by the daemon wiring).
	LLM conflict.LLM
	// Logger receives per-decision and fail-open lines. May be nil.
	Logger *slog.Logger
}

// managerRecallStore adapts *Manager to conflict.RecallStore: claims are
// recalled through SearchSemantic (vector ranking when embeddings are
// available, keyword fallback otherwise) filtered to claim-type memories,
// and EmbeddingsAvailable reports whether the vector searcher is configured.
type managerRecallStore struct {
	m *Manager
}

var _ conflict.RecallStore = (*managerRecallStore)(nil)

// Search recalls up to limit existing claims similar to query. Recall uses
// the episodic store's FTS index directly with MatchAny semantics: an AND
// binding of the candidate's terms only matches verbatim overlap and misses
// near-conflicts ("Fridays" vs "Friday afternoons"), which is exactly the
// case conflict detection exists for.
func (r managerRecallStore) Search(ctx context.Context, query string, limit int) ([]conflict.RecallResult, error) {
	m := r.m
	m.mu.RLock()
	initialized := m.initialized
	epi := m.episodic
	m.mu.RUnlock()
	if !initialized {
		return nil, errors.New("memory manager not initialized")
	}
	if epi == nil {
		return nil, nil
	}
	results, err := epi.SearchOptions(ctx, SearchParams{
		Query:    query,
		Limit:    limit,
		MatchAny: true,
	})
	if err != nil {
		return nil, fmt.Errorf("conflict recall: %w", err)
	}
	out := make([]conflict.RecallResult, 0, len(results))
	for _, res := range results {
		if res.Memory.Type != MemoryTypeClaim {
			continue
		}
		// Superseded claims are historical lineage, not live facts — never
		// recall them as merge/supersede targets.
		if vt, ok := res.Memory.Metadata["valid_to"].(string); ok && vt != "" {
			continue
		}
		out = append(out, conflict.RecallResult{MemoryID: res.Memory.ID, Content: res.Memory.Content})
	}
	return out, nil
}

// EmbeddingsAvailable reports whether semantic search ranks by embeddings
// (vector searcher configured). When false the detector re-ranks the
// keyword-recall pool by token overlap.
func (r managerRecallStore) EmbeddingsAvailable() bool {
	return r.m.GetVectorSearcher() != nil
}

// NewRecallStore returns the Manager adapted to the conflict package's
// RecallStore seam (exported for the daemon wiring and tests).
func (m *Manager) NewRecallStore() conflict.RecallStore {
	return managerRecallStore{m: m}
}

// conflictCandidate is one extracted claim candidate positioned for the
// detector: Idx is the index in the extraction batch, carried back on the
// Decision so the caller can join decisions to candidates.
type conflictCandidate struct {
	Idx  int
	Cand conflict.Candidate
}

// detectConflicts runs the detector over the extraction candidates and
// returns one decision per candidate, aligned by index with cands.
//
// Fail-open contract (SHARED-CONVENTIONS §2.3): any error here returns
// all-store decisions so the extraction path stores every candidate plainly
// — extraction availability never depends on the conflict LLM.
func detectConflicts(ctx context.Context, w *ConflictWiring, recall conflict.RecallStore, cands []conflictCandidate) []conflict.Decision {
	allStore := make([]conflict.Decision, len(cands))
	for i := range allStore {
		allStore[i] = conflict.Decision{CandidateIdx: i, Action: conflict.ActionStore}
	}
	if w == nil || !w.Enabled || len(cands) == 0 {
		return allStore
	}
	input := make([]conflict.Candidate, len(cands))
	for i, c := range cands {
		input[i] = c.Cand
	}
	detector := conflict.NewDetector(conflict.DetectorConfig{
		LLM:                w.LLM,
		Store:              recall,
		Logger:             w.Logger,
		MaxBatch:           w.MaxBatch,
		RecallPerCandidate: w.TopK,
	})
	decisions := detector.DetectBatch(ctx, input)
	if len(decisions) != len(cands) {
		// Defensive: the detector always returns one decision per candidate,
		// but a length mismatch must never misalign the consumer rule.
		return allStore
	}
	return decisions
}

// applyConflictDecision applies the frozen consumer rule for one decision.
// It returns the ID of the claim created for the candidate ("" for skip) —
// store/merge/supersede all store the candidate (merge stores the merged
// content), skip drops it.
func (w *DistillWorker) applyConflictDecision(ctx context.Context, cand conflictCandidate, dec conflict.Decision, claim Claim) (string, error) {
	switch dec.Action {
	case conflict.ActionSkip:
		w.logger.Info("conflict decision: skip",
			"candidate_idx", cand.Idx, "candidate_id", cand.Cand.ID,
			"target_ids", dec.TargetIDs, "reason", dec.Reason)
		return "", nil

	case conflict.ActionStore:
		id, err := w.manager.StoreClaim(ctx, claim)
		if err != nil {
			return "", fmt.Errorf("conflict store claim: %w", err)
		}
		w.logger.Info("conflict decision: store",
			"candidate_idx", cand.Idx, "candidate_id", cand.Cand.ID,
			"claim_id", id)
		return id, nil

	case conflict.ActionMerge, conflict.ActionSupersede:
		text := claim.Text
		if dec.Action == conflict.ActionMerge {
			merged := strings.TrimSpace(dec.MergedContent)
			if merged == "" {
				// A merge without merged content is unusable — keep the
				// candidate text rather than storing an empty claim.
				w.logger.Warn("conflict merge without merged_content; storing candidate text",
					"candidate_idx", cand.Idx)
			} else {
				text = merged
			}
		}
		meta := map[string]any{
			conflictActionMeta: string(dec.Action),
		}
		if len(dec.TargetIDs) > 0 {
			meta[conflictTargetsMeta] = append([]string(nil), dec.TargetIDs...)
		}
		newID, err := w.manager.storeClaimWithMetadata(ctx, claim, text, meta)
		if err != nil {
			return "", err
		}
		superseded := 0
		for _, target := range dec.TargetIDs {
			if _, _, err := w.manager.MarkSuperseded(ctx, target, newID); err != nil {
				// MarkSuperseded enforces auto-cannot-supersede-confirmed;
				// that refusal is expected behavior, not a failure of the
				// extraction. Log and keep the successor claim.
				w.logger.Warn("conflict supersede of target refused",
					"target_id", target, "new_claim_id", newID, "error", err)
				continue
			}
			w.calibrateSupersede(ctx, target, text, claim.Confidence)
			superseded++
		}
		w.logger.Info(fmt.Sprintf("conflict decision: %s", dec.Action),
			"candidate_idx", cand.Idx, "candidate_id", cand.Cand.ID,
			"claim_id", newID, "target_ids", dec.TargetIDs,
			"superseded", superseded, "reason", dec.Reason)
		return newID, nil

	default:
		// Unknown action: fail open to a plain store.
		w.logger.Warn("conflict decision: unknown action; storing",
			"candidate_idx", cand.Idx, "action", string(dec.Action))
		id, err := w.manager.StoreClaim(ctx, claim)
		if err != nil {
			return "", fmt.Errorf("conflict store claim (unknown action fallback): %w", err)
		}
		return id, nil
	}
}

// calibrateSupersede writes one supersede verdict row to the calibration log
// (claim_verdicts.jsonl) for a conflict-driven supersede. Best-effort: the
// Manager's calibration logger is nil-safe; a nil manager or logger skips.
func (w *DistillWorker) calibrateSupersede(ctx context.Context, targetID, text string, confidence float64) {
	if w.manager == nil {
		return
	}
	w.manager.logCalibrationReason(ctx, targetID, "supersede", &Memory{
		Type:     MemoryTypeClaim,
		Content:  text,
		Metadata: map[string]any{"confidence": confidence},
	}, "conflict detection merge/supersede")
}

// storeClaimWithMetadata stores a claim with extra conflict-provenance
// metadata merged on top of the standard claim metadata. StoreClaim does not
// accept extra metadata, so the memory is built the same way and stored with
// the additional keys.
func (m *Manager) storeClaimWithMetadata(ctx context.Context, c Claim, text string, extra map[string]any) (string, error) {
	if c.Status == "" {
		c.Status = ClaimStatusConfirmed
	}
	meta := map[string]any{
		"status":     string(c.Status),
		"confidence": c.Confidence,
	}
	if c.Source != "" {
		meta["source"] = c.Source
	}
	if len(c.Premises) > 0 {
		meta["premises"] = c.Premises
	}
	if len(c.Tags) > 0 {
		meta["tags"] = c.Tags
	}
	if !c.ObservedAt.IsZero() {
		meta["observed_at"] = c.ObservedAt.Format(_rfc3339UTC)
	}
	if c.ValidFrom != nil {
		meta["valid_from"] = c.ValidFrom.Format(_rfc3339UTC)
	}
	if c.ValidTo != nil {
		meta["valid_to"] = c.ValidTo.Format(_rfc3339UTC)
	}
	if c.Rev != 0 {
		meta["rev"] = c.Rev
	}
	for k, v := range extra {
		meta[k] = v
	}
	id, err := m.Store(ctx, Memory{
		Type:     MemoryTypeClaim,
		Category: "claim",
		Content:  text,
		Metadata: meta,
	})
	if err != nil {
		return "", fmt.Errorf("conflict store claim with metadata: %w", err)
	}
	return id, nil
}

// ListMergeClaims returns claims stamped conflict_action=merge (created by a
// conflict merge decision), newest first, optionally filtered by createdAfter.
// Backs the memory.listConflicts RPC / `meept memory conflicts` CLI. Follows
// the ListAutoClaims pattern: broad claim search, metadata filtered
// post-query.
func (m *Manager) ListMergeClaims(ctx context.Context, createdAfter time.Time, limit int) ([]MemoryResult, error) {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return nil, errors.New("memory manager not initialized")
	}
	if limit <= 0 {
		limit = 20
	}
	results, err := m.Search(ctx, MemoryQuery{
		Type:  MemoryTypeClaim,
		Limit: limit * 4,
	})
	if err != nil {
		return nil, fmt.Errorf("search merge claims: %w", err)
	}
	var out []MemoryResult
	for _, r := range results {
		if r.Memory.Type != MemoryTypeClaim {
			continue
		}
		if asString(r.Memory.Metadata[conflictActionMeta]) != string(conflict.ActionMerge) {
			continue
		}
		if !createdAfter.IsZero() && r.Memory.CreatedAt.Before(createdAfter) {
			continue
		}
		out = append(out, r)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}
