// Claim demotion flag (tree 02 leaf 03, contract C6; findings R-B7):
// additive extensions over the EXISTING claim metadata-update path.
//
// The flag-not-delete rule (D1/D2): demotion stamps metadata on the
// claim row IN PLACE (stampMetadataInPlace precedent from the verdict
// path) so the claim ID stays stable, history retains the content, and
// recall-side consumers can filter demoted IDs from the default
// retrieval surface while episodic search still reaches the content.
//
// The demotion lifecycle enum (R-B7: active | demoted | superseded)
// hangs off the existing "status" claim metadata key: "demoted" is the
// third value next to auto/confirmed/promoted/rejected. The dedicated
// "demoted" bool + "demoted_reason"/"demoted_at" keys carry the tier
// semantics (the status enum alone cannot say WHY a claim left the
// semantic surface).
//
// ASCII only. No changes to existing claim structs; additive only.
package memory

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Claim demotion metadata keys (tierpolicy mirrors these as constants;
// the string values are the single source of truth here).
const (
	// MetaClaimDemoted is the demotion flag: JSON true after demotion.
	MetaClaimDemoted = "demoted"
	// MetaClaimDemotedReason records why (staleness | low_usefulness |
	// superseded).
	MetaClaimDemotedReason = "demoted_reason"
	// MetaClaimDemotedAt records the RFC3339 demotion instant.
	MetaClaimDemotedAt = "demoted_at"
	// MetaClaimDemotedTo records the destination tier ("episodic").
	MetaClaimDemotedTo = "demoted_to"
	// ClaimStatusDemoted is the R-B7 lifecycle value stamped next to
	// the existing status (ClaimStatus-typed for reuse).
	ClaimStatusDemoted ClaimStatus = "demoted"
)

// ClaimIsDemoted reports whether a claim memory carries the demotion
// flag. Metadata-shape tolerant: hand-stamped (bool) and
// JSON-round-tripped (bool/float64/string) shapes all read correctly.
// Nil-safe on the metadata map.
func ClaimIsDemoted(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	switch v := meta[MetaClaimDemoted].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == string(ClaimStatusDemoted)
	case float64:
		return v != 0
	}
	return false
}

// DemoteClaim stamps the demotion flag on a claim row IN PLACE via the
// same metadata-write path the verdict flow uses (stampMetadataInPlace:
// no new version row, claim ID stable, supersede lineage intact).
//
// Idempotent (R-B7): an already-demoted claim is a successful noop (the
// caller's demote counter does not increment on a semantic noop because
// tierpolicy checks the flag before calling; this double-check is the
// transaction-side belt of that braces).
//
// Order contract with tierpolicy's executeDemote: the episodic COPY is
// written FIRST by the caller; this flag write is step 2. A crash
// between the steps leaves the claim un-flagged, and a re-run repeats
// the copy attempt - the copy is deduped by the demoted_from marker on
// the episodic side (DemotionApplier implementations).
//
// Not exported to the chat path: callers are the tierpolicy applier
// adapter (background tick) and admin surfaces. No delete anywhere.
func (m *Manager) DemoteClaim(ctx context.Context, claimID, reason string, at time.Time) error {
	m.mu.RLock()
	initialized := m.initialized
	m.mu.RUnlock()
	if !initialized {
		return errors.New("memory manager not initialized")
	}
	mem, err := m.GetByID(ctx, claimID)
	if err != nil {
		return fmt.Errorf("load claim %s: %w", claimID, err)
	}
	if mem.Type != MemoryTypeClaim {
		return fmt.Errorf("memory %s is type %s, not claim", claimID, mem.Type)
	}
	if mem.Metadata == nil {
		mem.Metadata = make(map[string]any)
	}
	if ClaimIsDemoted(mem.Metadata) {
		return nil // R-B7 idempotent noop
	}
	mem.Metadata[MetaClaimDemoted] = true
	mem.Metadata[MetaClaimDemotedReason] = reason
	mem.Metadata[MetaClaimDemotedAt] = at.UTC().Format(time.RFC3339)
	mem.Metadata[MetaClaimDemotedTo] = string(MemoryTypeEpisodic)
	// Status moves to the R-B7 "demoted" lifecycle value in place - the
	// same in-place discipline as the verdict path (a versioned write
	// would mint a new row ID and orphan evidence/supersede edges).
	mem.Metadata["status"] = string(ClaimStatusDemoted)
	if err := m.stampMetadataInPlace(ctx, mem); err != nil {
		return fmt.Errorf("stamp demotion on claim %s: %w", claimID, err)
	}
	return nil
}

// FilterDemotedClaims drops demoted claims from a result slice - the
// visibility-axis filter recall-side consumers apply so demoted claims
// stop surfacing as semantic claims while the rows (and their episodic
// copies) remain in history. Invalid/missing metadata reads as
// not-demoted (fail-open, same posture as claimInForce).
func FilterDemotedClaims(results []MemoryResult) []MemoryResult {
	out := results[:0:0]
	for _, r := range results {
		if ClaimIsDemoted(r.Memory.Metadata) {
			continue
		}
		out = append(out, r)
	}
	return out
}
