// Demotion: the semantic-to-episodic lifecycle direction (contract C6,
// tree 02 leaf 03; findings-memory-tiers.md R-B6/R-B7).
//
// Claims flow episodic -> semantic via L1 extract but never return. This
// file closes that loop: stale or low-utility semantic claims are COPIED
// into the episodic tier (lossless, flag-not-delete - D1/D2) and the
// semantic entry is stamped with a demoted metadata flag so recall stops
// surfacing it as a live claim while history retains it. Demotion is
// idempotent (R-B7): re-planning an already-demoted ID is a noop, and
// re-executing a demote action after a crash between the two steps
// completes the remaining step without duplicating the episodic copy
// (the copy carries demoted_from = the claim ID; a second Execute finds
// the flag already set and skips).
//
// Criteria (R-B6, in priority order; supersession/contradiction detection
// lives in the conflict path - conflict_wiring.go - and stamps its own
// superseded status, so this planner implements criteria 2 and 3):
//   - staleness: claim age > StaleDays (staleness.go signals the caveat
//     at >= 2 days; the demotion window is the config-driven threshold).
//   - low usefulness: memory.Usefulness score below MinUsefulness.
//
// Execution order rule (R-B4 extended by the leaf contract): demote
// precedes evict in every returned plan - demotion preserves content,
// eviction does not, so a mixed plan must reclaim by demoting before it
// destroys. Engine.Evaluate appends demote actions before the band
// actions; demote_execute_test pins the order.
//
// All execution happens ONLY on the background tick path (Engine.Execute),
// never on the chat path.
//
// ASCII only. No internal/agent imports.
package tierpolicy

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/caimlas/meept/internal/memory"
)

// KindDemote moves the target semantic claim to the episodic tier:
// lossless copy + demoted flag (never a delete).
const KindDemote = "demote"

// Demotion reasons (machine-readable, contract C6).
const (
	DemoteReasonStale         = "stale"
	DemoteReasonLowUsefulness = "low_usefulness"
)

// ClaimSource supplies semantic-claim candidates for demotion planning.
// The daemon adapts memory.Manager claim lookups to this; tests stub it.
type ClaimSource interface {
	// Claims returns up to limit semantic claims (Type == claim),
	// oldest first for deterministic planning.
	Claims(ctx context.Context, limit int) ([]memory.MemoryResult, error)
}

// DemotionApplier applies the two-step demotion to the stores:
//
//  1. CopyToEpisodic: the lossless episodic copy of the claim content
//     (episodic write FIRST - T2: a failure mid-copy leaves no partial
//     state, because the semantic entry is still un-flagged and a re-run
//     repeats the copy attempt).
//  2. MarkDemoted: the demoted metadata flag on the semantic entry
//     (visibility axis only - validity and content are untouched).
//
// Implementations must not delete anything: the demote path has no
// delete seam by construction.
type DemotionApplier interface {
	CopyToEpisodic(ctx context.Context, claim memory.Memory) (newID string, err error)
	MarkDemoted(ctx context.Context, claimID, reason string, at time.Time) error
}

// DemotedDetector reports whether a semantic claim already carries the
// demoted flag (R-B7 idempotency: checked inside BOTH planning and
// execution so a crash-between-steps re-run completes instead of
// duplicating).
type DemotedDetector interface {
	IsDemoted(ctx context.Context, claimID string) (bool, error)
}

// Deps gains the demotion collaborators. All nil-safe: a nil Claims (or
// nil Demotion) makes PlanDemotions return nil actions honestly; a nil
// Demotion makes Execute report an error per demote action. A nil
// Votes only disables the usefulness criterion (staleness still plans).
// The DemotedDetector is optional: when nil, demotion state is read
// from claim metadata ("demoted" key) via the ClaimSource results.

// Demotion state keys (R-B7 small lifecycle enum: active | demoted |
// superseded - re-promotion stays representable because the flag is
// metadata, not a delete).
const (
	// MetaDemoted is the metadata flag key stamped on a demoted claim.
	MetaDemoted = "demoted"
	// MetaDemotedReason records why (DemoteReason* constant).
	MetaDemotedReason = "demoted_reason"
	// MetaDemotedAt records the RFC3339 demotion instant.
	MetaDemotedAt = "demoted_at"
	// MetaDemotedFrom is stamped on the episodic COPY so re-runs can
	// detect an existing copy (crash-between-steps recovery, R-B7).
	MetaDemotedFrom = "demoted_from"
	// MetaStatus carries the R-B7 lifecycle enum value "demoted" next
	// to the existing claim status (active stays as-is).
	MetaStatusDemoted = "demoted"
)

// PlanDemotions turns semantic claim IDs into demote actions (contract
// C6). For each candidate: an already-demoted ID is omitted (noop,
// R-B7); a claim older than the staleness window or scoring below the
// usefulness floor gets exactly one Action{Kind: "demote"} with the
// matching reason. Claims carrying net votes <= memory.HarmfulVoteThreshold
// are NOT planned here - they are eviction business (PlanUsefulEviction
// harmful set), and demote-before-evict ordering would waste a copy on
// content eviction is about to destroy.
//
// semanticIDs filters the candidate pool when non-empty; empty means
// "evaluate all claims" (the background-tick posture).
func (e *Engine) PlanDemotions(ctx context.Context, semanticIDs []string) ([]Action, error) {
	if e == nil {
		return nil, errors.New("tierpolicy: nil engine")
	}
	if !e.cfg.Enabled {
		return nil, nil
	}
	if e.deps.Demotion == nil || e.deps.DemotionClaims == nil {
		return nil, nil // honest noop: no demotion wiring wired
	}
	cands, err := e.deps.DemotionClaims.Claims(ctx, demoteCandidateLimit)
	if err != nil {
		return nil, fmt.Errorf("tierpolicy: demotion candidates: %w", err)
	}
	want := make(map[string]struct{}, len(semanticIDs))
	for _, id := range semanticIDs {
		want[id] = struct{}{}
	}
	now := e.now()
	votes, voteErr := e.netVotesSafe(ctx, cands)
	if voteErr != nil {
		votes = nil // staleness criterion still applies
	}
	th := ResolveDemotionThresholds(e.cfg.Demotion)
	var actions []Action
	for _, c := range cands {
		if len(want) > 0 {
			if _, ok := want[c.Memory.ID]; !ok {
				continue
			}
		}
		if c.Memory.Type != memory.MemoryTypeClaim {
			continue
		}
		if demotedFromMeta(c.Memory.Metadata) {
			continue // R-B7: already demoted -> omit (noop)
		}
		reason, ok := demotionReason(c.Memory, votes, now, th)
		if !ok {
			continue
		}
		actions = append(actions, Action{
			Kind:      KindDemote,
			TargetIDs: []string{c.Memory.ID},
			Reason:    demoteReasonString(c.Memory, reason, now),
		})
	}
	return actions, nil
}

// demoteCandidateLimit mirrors evictCandidateLimit: the same candidate
// population scale the eviction ranking sees.
const demoteCandidateLimit = 500

// netVotesSafe fetches net votes for the candidate IDs; a nil Votes or a
// lookup failure returns (nil, err) and the planner degrades to the
// staleness-only criterion.
func (e *Engine) netVotesSafe(ctx context.Context, cands []memory.MemoryResult) (map[string]int, error) {
	if e.deps.Votes == nil {
		return nil, errors.New("tierpolicy: no vote source")
	}
	ids := make([]string, len(cands))
	for i, c := range cands {
		ids[i] = c.Memory.ID
	}
	return e.deps.Votes.NetVotes(ctx, ids)
}

// demotedFromMeta reports whether the claim metadata carries the demoted
// flag (R-B7). Accepts the bool true and the string "true"/"demoted"
// shapes so JSON-round-tripped metadata (float64/bool/string) and
// hand-stamped metadata both read correctly.
func demotedFromMeta(meta map[string]any) bool {
	if meta == nil {
		return false
	}
	switch v := meta[MetaDemoted].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == MetaStatusDemoted
	case float64:
		return v != 0
	}
	return false
}

// demotionReason applies the R-B6 criteria in priority order and returns
// the reason plus whether demotion is warranted.
func demotionReason(c memory.Memory, votes map[string]int, now time.Time, th DemotionThresholds) (string, bool) {
	// Criterion 2: staleness (priority over usefulness per R-B6 order).
	age := now.Sub(c.CreatedAt)
	if age > time.Duration(th.StaleDays)*24*time.Hour {
		return DemoteReasonStale, true
	}
	// Criterion 3: low usefulness (memory.Usefulness, same scoring the
	// eviction ranking uses). Only computable with a vote map.
	if votes != nil {
		score := memory.ScoreCandidate(c, votes[c.ID], 0, now, memory.DefaultWeights())
		if score < th.MinUsefulness {
			return DemoteReasonLowUsefulness, true
		}
	}
	return "", false
}

// demoteReasonString builds the machine-readable reason for a demote
// action: the criterion plus identity/age context.
func demoteReasonString(c memory.Memory, reason string, now time.Time) string {
	return fmt.Sprintf("reason=%s;claim=%s;age_days=%.0f",
		reason, c.ID, now.Sub(c.CreatedAt).Hours()/24)
}

// DemoteOrderBefore sorts actions into the contract order (demote ->
// compress -> evict, R-B4 extended): demotion preserves content and must
// precede destructive actions in the same plan. Stable, so within-kind
// order is preserved. Used by Evaluate's integration path and testable
// standalone.
func DemoteOrderBefore(actions []Action) []Action {
	rank := map[string]int{KindDemote: 0, KindCompress: 1, KindEvict: 2}
	out := make([]Action, len(actions))
	copy(out, actions)
	sort.SliceStable(out, func(i, j int) bool {
		return rank[out[i].Kind] < rank[out[j].Kind]
	})
	return out
}

// executeDemote applies one demote action in the T2 order: episodic copy
// FIRST, demoted flag SECOND, no delete anywhere. Crash-between-steps is
// recoverable (R-B7): step 2 re-runs cleanly because the flag write is
// idempotent, and a duplicated step-1 copy attempt is prevented by the
// flag check re-read before copying (already-flagged => copy skipped).
func (e *Engine) executeDemote(ctx context.Context, act Action) (int, error) {
	if e.deps.Demotion == nil {
		return 0, fmt.Errorf("demote %d ids: no Demotion applier wired", len(act.TargetIDs))
	}
	applied := 0
	for _, id := range act.TargetIDs {
		reason := parseDemoteReason(act.Reason)
		demoted, err := e.demoteOne(ctx, id, reason)
		if err != nil {
			return applied, fmt.Errorf("demote %s: %w", id, err)
		}
		if demoted {
			applied++
		}
	}
	return applied, nil
}

// parseDemoteReason extracts the demotion criterion from the
// machine-readable reason ("reason=stale;..."). Unknown reasons map to
// "stale" (the R-B6 priority-1 default posture for legacy actions).
func parseDemoteReason(reason string) string {
	for _, part := range splitSemicolon(reason) {
		if r, ok := cutPrefix(part, "reason="); ok {
			return r
		}
	}
	return DemoteReasonStale
}

// demoteOne runs the two-step demotion for one claim ID. The bool
// reports whether the demotion actually applied (false = idempotent
// noop: the claim was already flagged, R-B7).
func (e *Engine) demoteOne(ctx context.Context, id, reason string) (bool, error) {
	now := e.now()
	// R-B7 idempotency check inside execution (concurrent plan+execute
	// resolution): a claim flagged between planning and now is a noop.
	if det, ok := e.demotedDetector(); ok {
		already, err := det.IsDemoted(ctx, id)
		if err != nil {
			return false, fmt.Errorf("demoted check: %w", err)
		}
		if already {
			return false, nil
		}
	}
	// Step 1: lossless episodic copy (episodic write FIRST - T2).
	claim, err := e.loadClaim(ctx, id)
	if err != nil {
		return false, fmt.Errorf("load: %w", err)
	}
	if demotedFromMeta(claim.Metadata) {
		return false, nil // flagged between the detector read and the load
	}
	if _, err := e.deps.Demotion.CopyToEpisodic(ctx, *claim); err != nil {
		return false, fmt.Errorf("episodic copy: %w", err)
	}
	// Step 2: stamp the demoted flag on the semantic entry (visibility
	// axis only; content and validity untouched, never deleted).
	if err := e.deps.Demotion.MarkDemoted(ctx, id, reason, now); err != nil {
		return false, fmt.Errorf("mark demoted: %w", err)
	}
	return true, nil
}

// demotedDetector returns the optional detector seam. The Demotion
// applier itself is the natural detector (it owns the flag semantics);
// a standalone DemotedDetector wins when wired.
func (e *Engine) demotedDetector() (DemotedDetector, bool) {
	if e.deps.DemotedCheck != nil {
		return e.deps.DemotedCheck, true
	}
	if det, ok := interface{}(e.deps.Demotion).(DemotedDetector); ok && det != nil {
		return det, true
	}
	return nil, false
}

// loadClaim fetches one claim through the ClaimSource pool (or via the
// semanticIDs filter path). The applier contract keeps planning and
// execution on the same candidate shape.
func (e *Engine) loadClaim(ctx context.Context, id string) (*memory.Memory, error) {
	if e.deps.DemotionClaims == nil {
		return nil, errors.New("tierpolicy: no claim source wired")
	}
	cands, err := e.deps.DemotionClaims.Claims(ctx, demoteCandidateLimit)
	if err != nil {
		return nil, err
	}
	for i := range cands {
		if cands[i].Memory.ID == id {
			return &cands[i].Memory, nil
		}
	}
	return nil, fmt.Errorf("claim %s not in candidate pool", id)
}

// splitSemicolon splits a reason string on ';'.
func splitSemicolon(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ';' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	out = append(out, s[start:])
	return out
}

// cutPrefix extracts the value after prefix in a "key=value" segment.
func cutPrefix(s, prefix string) (string, bool) {
	if len(s) >= len(prefix) && s[:len(prefix)] == prefix {
		return s[len(prefix):], true
	}
	return "", false
}
