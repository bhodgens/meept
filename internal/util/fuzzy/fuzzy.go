package fuzzy

// Ported from the uptractor desk (src/lib/agent/fuzzy.ts, branch
// fix/speech-robustness) by leaf 03: fuzzy matching for voice-transcribed
// names against a catalog. The browser SpeechRecognition API frequently
// garbles proper nouns — "Meadow Ridge" comes back as "Meadow Rich" — and
// these helpers turn a garbled transcript into a confident match, or a
// "did you mean?" suggestion.
//
// Contract C5 (docs/plans/20261110-uptractor-ports/master.md): stdlib
// only; FuzzyFind results sorted descending with a deterministic
// tie-break; epsilon float comparisons per coding-conventions Pitfall 26
// (Go float determinism — similarity scores are never compared with ==).
// Caller thresholds are frozen: registries/subjects 0.60, short
// descriptions 0.55.

import (
	"sort"
	"strings"
)

// epsilon is the float slack every threshold comparison gets (Pitfall 26).
const implEpsilon = 1e-9

// normalize lowercases and collapses non-alphanumeric runs to single
// spaces (the TS .toLowerCase().replace(/[^a-z0-9]+/g, ' ').trim()
// pipeline). Exported as NormalizeToken so scorers that pre-tokenize
// (the cross-call echo roster) can drop dead tokens — tokens that
// normalize to empty ("...", "--") — with the SAME rule this package
// applies internally, instead of re-implementing it (fork-prevention).
func normalize(s string) string {
	var b strings.Builder
	prevSpace := true // trims the head
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

// editDistance is the Levenshtein distance between a and b (stdlib only,
// two-row DP, case-insensitive per the TS source's per-character
// .toLowerCase() comparison).
func editDistance(a, b string) int {
	ar, br := []rune(strings.ToLower(a)), []rune(strings.ToLower(b))
	if len(ar) == 0 {
		return len(br)
	}
	if len(br) == 0 {
		return len(ar)
	}
	prev := make([]int, len(br)+1)
	cur := make([]int, len(br)+1)
	for j := 0; j <= len(br); j++ {
		prev[j] = j
	}
	for i := 1; i <= len(ar); i++ {
		cur[0] = i
		for j := 1; j <= len(br); j++ {
			cost := 1
			if ar[i-1] == br[j-1] {
				cost = 0
			}
			m := prev[j] + 1 // deletion
			if v := cur[j-1] + 1; v < m {
				m = v // insertion
			}
			if v := prev[j-1] + cost; v < m {
				m = v // substitution
			}
			cur[j] = m
		}
		prev, cur = cur, prev
	}
	return prev[len(br)]
}

// similarity scores one query token against one candidate token: 1 -
// editDistance/maxLen (the frozen TS per-token measure).
func similarity(queryToken, candidateToken string) float64 {
	maxLen := len([]rune(queryToken))
	if lr := len([]rune(candidateToken)); lr > maxLen {
		maxLen = lr
	}
	if maxLen == 0 {
		return 1
	}
	return 1 - float64(editDistance(queryToken, candidateToken))/float64(maxLen)
}

// Named is the optional adapter interface: a candidate can carry its own
// matchable name (the workOrder.FuzzyName() shape the gate uses). A
// candidate NOT implementing it matches through the caller's getName.
type Named interface {
	FuzzyName() string
}

// Match is one FuzzyFind result: the candidate item, its similarity score,
// and the name it was matched on (the frozen C5 result element: item +
// score + name). Go forbids embedding a (pointer to a) type parameter, so
// the item rides the Item field; callers read got[0].Item.ID. (The
// gate's original `got[0].ID` promotion shape is unreachable in pure Go
// generics without embedding — the test-compilation gap belongs to leaf
// 03, not this file's contract.)
type Match[T any] struct {
	Item  T
	Score float64
	Name  string
}

// FuzzyFind returns every candidate whose similarity to query meets the
// threshold (with epsilon slack per Pitfall 26), sorted by score
// DESCENDING. Ties break deterministically by name ascending, then by
// candidate order — equal-score results always come back in the same
// sequence. An empty (or whitespace-only after normalization) query
// returns no matches.
//
// Frozen formula: per-query-token best scores (1 - editDistance/maxLen
// against the best candidate token), averaged over the TOP TWO query
// tokens (a single-token query averages its one score).
func FuzzyFind[T any](query string, candidates []T, getName func(T) string, threshold float64) []Match[T] {
	q := normalize(query)
	if q == "" {
		return nil
	}
	qTokens := strings.Fields(q)
	var out []Match[T]
	for _, c := range candidates {
		name := getName(c)
		cTokens := strings.Fields(normalize(name))
		if len(cTokens) == 0 {
			continue
		}
		// Per-query-token best score against any candidate token.
		best := make([]float64, len(qTokens))
		for i, qt := range qTokens {
			for _, ct := range cTokens {
				if s := similarity(qt, ct); s > best[i] {
					best[i] = s
				}
			}
		}
		// Mean over the TOP TWO query tokens (frozen; descending).
		sort.Float64s(best)
		var total float64
		var n int
		if len(best) == 1 {
			total, n = best[0], 1
		} else {
			total, n = best[len(best)-1]+best[len(best)-2], 2
		}
		score := total / float64(n)
		if score+implEpsilon >= threshold {
			out = append(out, Match[T]{Item: c, Score: score, Name: name})
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Name < out[j].Name
	})
	return out
}

// ContainsRatio scores how much of `query` is recognizably contained in
// `text`: for every query token, the best per-token similarity against
// any text token (the frozen FuzzyFind measure, 1 - editDistance/maxLen),
// averaged over ALL query tokens (multiset — a repeated query token is
// scored twice). 1.0 means every query token appears (near-)verbatim in
// text; 0 means nothing overlaps.
//
// Unlike Similarity (which averages only the TOP TWO query tokens and so
// confidently matches unrelated sentences that share two words), a
// containment ratio over the WHOLE query is the echo-provenance shape:
// agent-echo transcripts land near 1.0 (whisper re-hears the TTS almost
// word-for-word, garbled tails still score high because EVERY token gets
// its best fuzzy match), while a human sentence that merely shares a few
// function words with the agent's speech stays low.
//
// BARGE-IN CONTRACT (2026-11-11): this scorer is the text-provenance half
// of the echo gate in internal/voicechild — energy barge-in trips are
// NEVER allowed on bare loudness; a recent final authorizes a trip only
// when its text does NOT ContainsRatio-match the agent's own recent
// speech (see voicechild textIsAgentEcho). Stdlib-only per contract C5.
//
// Empty query or empty text score 0 (no overlap evidence, never a match).
func ContainsRatio(query, text string) float64 {
	q := strings.Fields(normalize(query))
	if len(q) == 0 {
		return 0
	}
	t := strings.Fields(normalize(text))
	if len(t) == 0 {
		return 0
	}
	var sum float64
	for _, qt := range q {
		best := 0.0
		for _, tt := range t {
			if s := similarity(qt, tt); s > best {
				best = s
			}
		}
		sum += best
	}
	return sum / float64(len(q))
}

// EditDistance is the exported contract name (C5): Levenshtein distance.
func EditDistance(a, b string) int { return editDistance(a, b) }

// NormalizeToken returns normalize(s): the canonical lower/alphanumeric
// form every similarity comparison in this package runs on. A token
// whose normalized form is EMPTY ("...", "--", "!!!") scores 1.0 against
// any other empty-normalizing token (Similarity's maxLen==0 branch) —
// pure noise. Pre-tokenizing scorers (the cross-call echo roster) use
// this to DROP such dead tokens at store/query time so they never count
// toward coverage.
func NormalizeToken(s string) string { return normalize(s) }

// Similarity is the exported contract name (C5): token-aware similarity
// with the frozen top-two query-token vector.
func Similarity(a, b string) float64 {
	ta := strings.Fields(normalize(a))
	tb := strings.Fields(normalize(b))
	if len(ta) == 0 && len(tb) == 0 {
		return 1
	}
	if len(ta) == 0 || len(tb) == 0 {
		return 0
	}
	best := make([]float64, len(ta))
	for i, qt := range ta {
		for _, ct := range tb {
			if s := similarity(qt, ct); s > best[i] {
				best[i] = s
			}
		}
	}
	sort.Float64s(best)
	var total float64
	var n int
	if len(best) == 1 {
		total, n = best[0], 1
	} else {
		total, n = best[len(best)-1]+best[len(best)-2], 2
	}
	return total / float64(n)
}
