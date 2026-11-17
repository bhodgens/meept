package llm

// The pre-dispatch selection chain (Contract C1, sovereign-router-parity
// leaf 01). This file is the SINGLE integration surface every later
// filter leaf extends: a leaf adds ONE Filter implementation and ONE
// registration line in registerSelectionFilters — never a resolver edit.
//
// Registration order is FIXED and is part of the contract (master.md
// "The selection chain"):
//
//	1. postureFilter   (R3, leaf 05) — a sensitive turn never reaches a
//	   non-cleared model
//	2. qualityFilter   (R1, leaf 01) — quality floor for the classified
//	   difficulty
//	3. admissionFilter (R2, leaf 02) — context fit
//
// There is no position 4 (drift-audit ruling #4: advisory budget is NOT a
// chain filter). Order matters: posture is a hard compliance boundary and
// must be evaluated even when the quality filter would skip, so the skip
// reason recorded in the routing log names the BINDING constraint. This
// order is now fully populated: leaf 05 registered postureFilter at
// position 1, leaf 02 registered admissionFilter at position 3. A leaf
// that adds a filter appends it to this list and updates this contract in
// the same commit.

import "time"

// Candidate is one model under selection-filter consideration.
type Candidate struct {
	Model      *ModelConfig
	Alias      string
	Capability string
	EstInput   int
	EstOutput  int
}

// FilterCtx carries the per-request facts a filter judges against. It is
// request-scoped and read-only to filters: no filter may mutate it or
// touch shared resolver state (runSelectionFilters is called with the
// Resolver lock held on the alias path and WITHOUT it on the skill path).
type FilterCtx struct {
	SessionID  string
	Capability string
	EstInput   int
	EstOutput  int
	Sensitive  bool
	// PromptText carries the outbound user+system prompt text for the
	// position-1 posture filter (leaf 05, R3). It rides FilterCtx — the
	// chain's existing request-scoped carrier — rather than a resolver
	// field, because sensitivity is a PER-REQUEST fact, not resolver
	// state. Empty means the caller supplied no text: the posture gate
	// fail-opens (treats the turn as non-sensitive, never errors).
	// Callers build it with PosturePromptText; tool/assistant text is
	// deliberately excluded (already scanned at the tool boundary).
	PromptText string
	Difficulty int // 0 = unclassified = difficulty-derived behavior inert
	Now        time.Time
}

// Verdict is one filter's decision for a candidate.
type Verdict int

const (
	// VerdictAllow lets the candidate through.
	VerdictAllow Verdict = iota
	// VerdictSkip skips this candidate; the filter's reason is recorded.
	VerdictSkip
	// VerdictFloor is terminal: NO candidate may serve this turn.
	VerdictFloor
)

// Filter is the one interface every pre-dispatch selection filter
// implements (C1). Name identifies the filter in logs/reasons; Reason is
// returned alongside a non-Allow verdict.
type Filter interface {
	Name() string
	Filter(c *Candidate, fc *FilterCtx) Verdict
	Reason() string
}

// qualityFilter is the R1 floor filter (position 2). Inert until a model
// declares quality: with an all-zero candidate Quality the floor is never
// consulted and the verdict is Allow (flag-off byte-identical).
type qualityFilter struct{}

func (qualityFilter) Name() string { return "quality" }

// Filter applies QualityFloorFor(fc.Difficulty, fc.Capability) to the
// candidate. Difficulty 0 (unclassified) or a capability outside the five
// quality classes yields floor 0, which every score passes — inert.
func (qualityFilter) Filter(c *Candidate, fc *FilterCtx) Verdict {
	if c == nil || c.Model == nil || fc == nil || c.Model.Quality.IsZero() {
		return VerdictAllow
	}
	floor := QualityFloorFor(fc.Difficulty, fc.Capability)
	if floor <= 0 {
		return VerdictAllow
	}
	if c.Model.Quality.QualityFor(QualityClass(fc.Capability)) < floor {
		return VerdictSkip
	}
	return VerdictAllow
}

// Reason is the skip reason recorded when the quality filter rejects a
// candidate (mirrors Sovereign Router's "quality_below_required").
func (qualityFilter) Reason() string { return "quality_below_required" }

// registerSelectionFilters installs the chain in the FROZEN order.
// Called once from NewResolver; never mutates after construction, so
// runSelectionFilters needs no lock. Leaves append their registration
// lines at their contract positions and update the master contract in the
// same commit.
func (r *Resolver) registerSelectionFilters() {
	r.selectionFilters = []Filter{
		postureFilter{},   // Position 1 (R3, leaf 05) — hard compliance boundary; MUST stay first: it is evaluated even when a later filter would skip the same candidate, so the routing log names the binding constraint (master C1).
		qualityFilter{},   // Position 2 (R1)
		admissionFilter{}, // Position 3 (R2, leaf 02) — context fit
	}
}

// runSelectionFilters iterates r.selectionFilters in registration order
// and returns the FIRST non-Allow verdict with that filter's Reason().
// All-Allow returns (VerdictAllow, ""). Callers hold no contract on the
// Resolver lock (the alias path holds r.mu, the skill path does not), so
// this must not lock and must not touch shared mutable state — the
// filters slice is written once in NewResolver and never again.
func (r *Resolver) runSelectionFilters(c *Candidate, fc *FilterCtx) (Verdict, string) {
	for _, f := range r.selectionFilters {
		if v := f.Filter(c, fc); v != VerdictAllow {
			return v, f.Reason()
		}
	}
	return VerdictAllow, ""
}

// RunSelectionFiltersForGate is the test seam for the chain's first-non-
// Allow semantics (gate contract test TestContract_C1_SelectionChain): it
// runs the GIVEN filters over the candidate — not the registered chain —
// so the gate can pin the iteration contract without depending on which
// leaves have registered production filters. Production callers use
// runSelectionFilters; this exists for the e2e suite only.
func (r *Resolver) RunSelectionFiltersForGate(c *Candidate, fc *FilterCtx, filters []Filter) (Verdict, string) {
	for _, f := range filters {
		if v := f.Filter(c, fc); v != VerdictAllow {
			return v, f.Reason()
		}
	}
	return VerdictAllow, ""
}
