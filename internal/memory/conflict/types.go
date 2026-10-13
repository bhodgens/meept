// Package conflict implements batch LLM conflict detection over candidate
// memories before they are stored: for each candidate it recalls similar
// existing memories and asks an LLM to decide one of four actions — store
// (no conflict), merge (both statements are true aspects of one fact),
// supersede (the candidate replaces a stale target), or skip (exact
// duplicate). Any failure fails open: every candidate is stored, preserving
// pre-detector behavior.
//
// The package is intentionally dependency-free: it does NOT import
// internal/memory (or any other internal package). Callers adapt their own
// stores to the RecallStore interface and their LLM client to the LLM
// interface.
package conflict

// SPEC (SHARED-CONVENTIONS.md §2.3):
//
//	package conflict
//	type Action string // "store" | "merge" | "supersede" | "skip"
//	type Decision struct {
//	    CandidateIdx int
//	    Action       Action
//	    TargetIDs    []string
//	    MergedContent string
//	    Reason       string
//	}
//
// Fail-open default: any detector error → all actions become `store`
// (preserves current behavior; matches TAM `l1-dedup.ts` fallback).

// Action is the conflict resolution action for one candidate.
type Action string

const (
	// ActionStore keeps the candidate as a new memory (default / fail-open).
	ActionStore Action = "store"
	// ActionMerge folds the candidate into the named targets: both
	// statements are true aspects of one fact.
	ActionMerge Action = "merge"
	// ActionSupersede replaces the named (stale) targets with the candidate.
	ActionSupersede Action = "supersede"
	// ActionSkip drops the candidate (exact duplicate of a target).
	ActionSkip Action = "skip"
)

// Decision is the LLM's verdict for a single candidate. Actions outside the
// four defined values, or a JSON entry with an out-of-range candidate index,
// are rejected and the decision becomes store.
type Decision struct {
	CandidateIdx  int
	Action        Action
	TargetIDs     []string
	MergedContent string
	Reason        string
}

// Candidate is a new memory about to be stored, presented to the detector.
type Candidate struct {
	// ID is the caller's identifier for this candidate (used for logging;
	// decisions are keyed by CandidateIdx, not by ID).
	ID string
	// Content is the canonical candidate text to compare against recalled
	// memories.
	Content string
	// Metadata is optional caller context echoed into the prompt (may be nil).
	Metadata map[string]string
}
