package voiceintake

// Subject resolution for guided intake (leaf 05, contract per
// 05-guided-intake.md Interface Contracts). The intake conversation names
// subjects in spoken words; internal/util/fuzzy (C5, leaf 03) turns a
// garbled transcript into a confident match or a "did you mean?" list.
// This file is the thin intake-layer policy over that primitive:
//
//	exact match (case/spacing-insensitive)      => ResolveExact (canonical name)
//	single fuzzy candidate >= threshold         => ResolveFuzzySingle (propose confirmation)
//	two or more fuzzy candidates >= threshold   => ResolveAmbiguous (REFUSE + list, never auto-pick)
//	nothing >= threshold                        => ResolveNoMatch (offer create-new)
//
// The frozen intake threshold is 0.60 (the C5 registries/subjects value);
// callers pass it explicitly so the signature stays threshold-honest.
//
// Uptractor-vocabulary note: this package is a GENERIC port. Nothing here
// knows about jobs or work orders; the candidate list is whatever registry
// the wiring hands in (contacts, ledger subjects).

import (
	"strings"

	"github.com/caimlas/meept/internal/util/fuzzy"
)

// ResolveOutcome classifies one ResolveSubject call.
type ResolveOutcome int

const (
	// ResolveExact: the query equals one candidate (case- and
	// spacing-insensitive). The match is the CANONICAL candidate name.
	ResolveExact ResolveOutcome = iota
	// ResolveFuzzySingle: exactly one candidate scored >= threshold.
	// Propose confirmation ("did you mean X?") — the caller must confirm
	// before the record files under that subject.
	ResolveFuzzySingle
	// ResolveAmbiguous: two or more candidates scored >= threshold.
	// REFUSE and list every candidate (matches) so the agent asks the
	// caller. Auto-picking is forbidden by contract.
	ResolveAmbiguous
	// ResolveNoMatch: nothing scored >= threshold (or a degenerate empty
	// query/candidate list). Offer the create-new path.
	ResolveNoMatch
)

// ResolveSubject resolves a spoken query against a candidate registry
// through fuzzy (C5) with the four-outcome policy above. threshold is the
// caller's frozen value (intake uses 0.60).
//
// Return contract (pinned by guided_intake_test.go):
//   - ResolveExact: match = canonical candidate, matches = nil.
//   - ResolveFuzzySingle: match = that candidate, matches = exactly
//     [match] (the propose-confirmation single list).
//   - ResolveAmbiguous: match = "" (NEVER a pick), matches = every
//     candidate >= threshold in FuzzyFind order (score desc, deterministic
//     tie-break).
//   - ResolveNoMatch: match = "", matches = nil.
func ResolveSubject(query string, candidates []string, threshold float64) (ResolveOutcome, string, []string) {
	if strings.TrimSpace(query) == "" || len(candidates) == 0 {
		return ResolveNoMatch, "", nil
	}

	// Exact match first (leaf contract: never routed through fuzzy
	// scoring). Case- and spacing-insensitive; the canonical candidate
	// name is what returns.
	for _, c := range candidates {
		if sameSpokenName(query, c) {
			return ResolveExact, c, nil
		}
	}

	hits := fuzzy.FuzzyFind(query, candidates, func(c string) string { return c }, threshold)
	switch len(hits) {
	case 0:
		return ResolveNoMatch, "", nil
	case 1:
		return ResolveFuzzySingle, hits[0].Name, []string{hits[0].Name}
	default:
		names := make([]string, 0, len(hits))
		for _, h := range hits {
			names = append(names, h.Name)
		}
		return ResolveAmbiguous, "", names
	}
}

// sameSpokenName reports whether a and b are the same spoken name:
// lowercased, whitespace-collapsed, trimmed (the fuzzy package's
// normalization shape, mirrored locally so internal/voiceintake does not
// depend on the voicechild normalize pipeline). "Oak  street" matches
// "Oak Street"; "oak st" does not.
func sameSpokenName(a, b string) bool {
	return normalizeName(a) != "" && normalizeName(a) == normalizeName(b)
}

// normalizeName lowercases and collapses non-alphanumeric runs to single
// spaces (fuzzy.normalize's exported-shape equivalent; C5 keeps the
// unexported one, so the two-char policy is re-stated here).
func normalizeName(s string) string {
	var b strings.Builder
	prevSpace := true
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			prevSpace = false
		} else if !prevSpace {
			b.WriteByte(' ')
			prevSpace = true
		}
	}
	return strings.TrimRight(b.String(), " ")
}
