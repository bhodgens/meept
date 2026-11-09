package sqlite

import (
	"fmt"
	"strings"
	"unicode"
)

// sanitizeTerm removes characters that are invalid in FTS5 queries.
func sanitizeTerm(term string) string {
	// Remove quotes and FTS5 special characters
	var sb strings.Builder
	for _, r := range term {
		if r == '"' || r == '\'' || r == '*' || r == ':' || r == '(' || r == ')' {
			continue
		}
		if unicode.IsControl(r) {
			continue
		}
		sb.WriteRune(r)
	}
	return strings.TrimSpace(sb.String())
}

// SanitizeQueryOpPrefixes converts a raw user query into a safe FTS5 MATCH
// expression joining tokens with the given boolean operator, with each token
// as a PREFIX query ("term"*). FTS5's default unicode61 tokenizer does not
// stem, so an exact "friday" never matches stored "Fridays" — prefix
// matching recovers the near-miss recall a semantic-search fallback owes its
// callers (bughunt 2026-10-14: SearchSemantic with MatchAny still returned 0
// hits for "deploy friday afternoons" over stored "user deploys on Fridays").
//
// The query-builder half of this file (FTS5Query chainable API: NewFTS5Query,
// Term/Terms/Column(s)/And/Or/Build/BuildPhrase/BuildPrefix, Highlight,
// Snippet, Rank, CreateFTS5Table, CreateFTS5Triggers) was removed in the
// wave-1 deadcode sweep: FTS stores hand-roll their SQL and only the
// Sanitize*/BM25/NormalizeRank helpers below have live callers
// (memory/task.go, episodic.go, ftstore.go, knowledge/search.go).
func SanitizeQueryOpPrefixes(raw, operator string) string {
	quotedTerms := make([]string, 0, 8)
	op := " AND "
	if operator == "OR" {
		op = " OR "
	}
	for t := range strings.FieldsSeq(raw) {
		if sanitized := sanitizeTerm(t); sanitized != "" {
			// The star goes OUTSIDE the quotes: "term"* is the FTS5
			// prefix-query form; "term*" is a literal string containing a
			// star and matches nothing.
			quotedTerms = append(quotedTerms, fmt.Sprintf("%q*", sanitized))
		}
	}
	if len(quotedTerms) == 0 {
		return ""
	}
	return strings.Join(quotedTerms, op)
}

// SanitizeQuery converts a raw user query into a safe FTS5 MATCH expression.
// Each whitespace-separated token is quoted for safety.
func SanitizeQuery(raw string) string {
	return SanitizeQueryOp(raw, "AND")
}

// SanitizeQueryOp converts a raw user query into a safe FTS5 MATCH
// expression joining tokens with the given boolean operator ("AND" or
// "OR"). Each whitespace-separated token is quoted for safety.
func SanitizeQueryOp(raw, operator string) string {
	quotedTerms := make([]string, 0, 8)
	for t := range strings.FieldsSeq(raw) {
		if sanitized := sanitizeTerm(t); sanitized != "" {
			quotedTerms = append(quotedTerms, fmt.Sprintf("%q", sanitized))
		}
	}
	if len(quotedTerms) == 0 {
		return ""
	}
	return strings.Join(quotedTerms, " "+operator+" ")
}

// BM25 returns SQL for FTS5 BM25 ranking function.
// tableName: the FTS5 table name
// weights: optional column weights (higher = more important)
func BM25(tableName string, weights ...float64) string {
	if len(weights) == 0 {
		return fmt.Sprintf("bm25(%s)", tableName)
	}

	weightStrs := make([]string, len(weights))
	for i, w := range weights {
		weightStrs[i] = fmt.Sprintf("%.2f", w)
	}

	return fmt.Sprintf("bm25(%s, %s)", tableName, strings.Join(weightStrs, ", "))
}

// NormalizeRank converts an FTS5 rank value to a [0.0, 1.0) relevance
// score. FTS5 rank values are negative (more negative = better match), so
// the score is normalized to RISE with match strength:
//
//	score = |rank| / (1 + |rank|)
//
// rank 0 (no signal) → 0.0; rank -1 → 0.5; rank -9 → 0.9. Monotonicity is
// the whole point (M13, bughunt 2026-09-04): the previous 1/(1+|rank|)
// map DECREASED as matches improved, so any positive min_relevance floor
// filtered the strongest matches first — the 7ee95a53 0.3→0.1 default
// change moved the cliff, it didn't remove it.
func NormalizeRank(rank float64) float64 {
	if rank >= 0 {
		return 0.0
	}
	a := -rank
	return a / (1.0 + a)
}
