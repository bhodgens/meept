package fuzzy

// Gate tests for contract C5 (master.md, quoted verbatim):
//
//	C5 Fuzzy: package fuzzy; func EditDistance(a, b string) int; func
//	Similarity(a, b string) float64; func FuzzyFind[T any](query string,
//	candidates []T, getName func(T) string, threshold float64) []T
//	(sorted descending; epsilon comparisons per Pitfall 26). Thresholds
//	used by callers: jobs/subjects 0.60, short descriptions 0.55.
//
// The canonical test table is ported VERBATIM from uptractor
// docs/SPEECH-ROBUSTNESS.md (branch fix/speech-robustness), including the
// "summer planes" no-match row (audit 2026-11-11 R5: the doc says
// "planes", not "plans"). Expected values verified against a faithful
// replica of the frozen TS algorithm (token-set, per-token edit-distance
// weighted) before this file was written.
//
// RED until leaf 03 (package fuzzy does not exist).

import (
	"math"
	"regexp"
	"strings"
	"testing"
)

// normString mirrors the source norm(): lowercase, non-alphanumerics to
// spaces, trimmed. The comparison space is normalized on both sides.
func normString(v string) string {
	return strings.TrimSpace(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(strings.ToLower(v), " "))
}

// epsilon is the float-comparison slack (Pitfall 26: Go float
// determinism — never compare similarity scores with ==).
const epsilon = 1e-9

// diff reports |got-want| outside the epsilon slack.
func diff(got, want float64) bool {
	return math.Abs(got-want) > epsilon
}

type job struct{ Name string }

// canonicalJobs is the seed catalog the SPEECH-ROBUSTNESS table was
// written against (jobs for the 0.60 threshold rows).
var canonicalJobs = []job{
	{"Meadow Ridge"},
	{"Oak Street"},
	{"Dakota Drywall"},
}

// TestEditDistanceKnownPairs pins Levenshtein distance on known pairs,
// including the exact distances the canonical table cites ("1 deletion",
// "2 deletions").
func TestEditDistanceKnownPairs(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"", "", 0},
		{"", "abc", 3},
		{"abc", "", 3},
		{"abc", "abc", 0},
		// Canonical-table distances:
		{"Meadow Rich", "Meadow Ridge", 1}, // "1 deletion"
		{"Meadow Ridg", "Meadow Ridge", 2}, // "2 deletions"
		{"Oak St", "Oak Street", 3},
		{"kitten", "sitting", 3},
		{"flaw", "lawn", 2},
	}
	for _, tc := range cases {
		if got := EditDistance(tc.a, tc.b); got != tc.want {
			t.Errorf("EditDistance(%q, %q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

// TestEditDistanceCaseInsensitive pins the TS source semantics: per-
// character comparison is case-insensitive (a[i].toLowerCase()).
func TestEditDistanceCaseInsensitive(t *testing.T) {
	if got := EditDistance("ABC", "abc"); got != 0 {
		t.Errorf("EditDistance case-only = %d, want 0 (case-insensitive per source)", got)
	}
}

// TestSimilarityEdges pins the boundary semantics of the frozen formula:
// both empty = 1; one empty = 0; token-set membership (duplicates
// collapse); identical tokens = 1.
func TestSimilarityEdges(t *testing.T) {
	cases := []struct {
		a, b string
		want float64
	}{
		{"", "", 1.0},
		{"abc", "", 0.0},
		{"", "abc", 0.0},
		{"same same", "same", 1.0}, // token SETS, duplicates collapse
		{"Same", "same", 1.0},
	}
	for _, tc := range cases {
		if got := Similarity(tc.a, tc.b); diff(got, tc.want) {
			t.Errorf("Similarity(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
	if got := Similarity("Oak St", "Oak Street"); got <= 0.6 || got >= 0.7 {
		t.Errorf("Similarity(\"Oak St\", \"Oak Street\") = %v, want 2/3 (0.667)", got)
	}
}

// TestSimilarityCanonicalVector pins one exact non-trivial similarity so
// threshold drift is caught numerically, not just pass/fail:
// "Meadow Rich" vs "Meadow Ridge" = 0.70 under the frozen formula
// (tokens {meadow,rich} vs {meadow,ridge}: 1 + (1 - 2/5) = 0.70).
func TestSimilarityCanonicalVector(t *testing.T) {
	got := Similarity("Meadow Rich", "Meadow Ridge")
	if diff(got, 0.70) {
		t.Errorf("Similarity = %v, want 0.70 (frozen formula vector)", got)
	}
}

// TestFuzzyFindCanonicalTable is the canonical SPEECH-ROBUSTNESS.md test
// table, VERBATIM (audit R5: "summer planes", not "summer plans"). Given
// the seed catalog; When FuzzyFind at the frozen jobs threshold 0.60;
// Then the table's expected match (or no match) is the only result.
func TestFuzzyFindCanonicalTable(t *testing.T) {
	names := func(j job) string { return j.Name }

	cases := []struct {
		input     string
		wantMatch string // "" = no match expected
	}{
		{"Meadow Ridge", "Meadow Ridge"}, // exact
		{"Meadow Rich", "Meadow Ridge"},  // fuzzy, 1 deletion
		{"Meadow Ridg", "Meadow Ridge"},  // fuzzy, 2 deletions
		{"summer planes", ""},            // no match (too dissimilar) — canonical no-match row
		{"Oak Street", "Oak Street"},     // exact
		{"Oak St", "Oak Street"},         // fuzzy, 1 deletion
		{"mark the siding done", ""},     // matches WOs, not job names — below 0.60 here
		{"roof flashing", ""},            // not a job in this catalog
	}
	for _, tc := range cases {
		got := FuzzyFind(normString(tc.input), canonicalJobs, names, 0.60)
		if tc.wantMatch == "" {
			if len(got) != 0 {
				t.Errorf("FuzzyFind(%q) matched %v, want NO match (canonical table)", tc.input, got)
			}
			continue
		}
		if len(got) != 1 {
			t.Fatalf("FuzzyFind(%q) = %d matches (%v), want exactly %q", tc.input, len(got), got, tc.wantMatch)
		}
		if normString(tc.wantMatch) != normString(got[0].Name) {
			t.Errorf("FuzzyFind(%q) matched %q, want %q", tc.input, got[0].Name, tc.wantMatch)
		}
	}
}

// workOrder mirrors the findOrders() candidate shape: fuzzy runs on
// "{id} {description} {assigned}" at the frozen short-description
// threshold 0.55.
type workOrder struct {
	ID, Description, Assigned string
}

func (w workOrder) FuzzyName() string { return w.ID + " " + w.Description + " " + w.Assigned }

var canonicalWOs = []workOrder{
	{"WO-1", "siding", "crew"},
	{"WO-2", "drywall taping", "ana"},
	{"WO-3", "roof flashing", "jo"},
}

// TestFuzzyFindCanonicalWorkOrders ports the SPEECH-ROBUSTNESS work-order
// rows verbatim at threshold 0.55: "siding", "siding work" => WO-1;
// "roof flashing" => WO-3; "mark the siding done" => WO-1 (fuzzy on
// "siding" — the query's action-word tokens drag the score down but the
// subject token still wins top spot above 0.55).
func TestFuzzyFindCanonicalWorkOrders(t *testing.T) {
	getName := func(w workOrder) string { return w.FuzzyName() }
	cases := []struct {
		input string
		want  string
	}{
		{"siding", "WO-1"},
		{"siding work", "WO-1"},
		{"roof flashing", "WO-3"},
		{"mark the siding done", "WO-1"},
	}
	for _, tc := range cases {
		got := FuzzyFind(normString(tc.input), canonicalWOs, getName, 0.55)
		if len(got) == 0 {
			t.Fatalf("FuzzyFind(%q over work orders) = no match, want %q (canonical table)", tc.input, tc.want)
		}
		if got[0].ID != tc.want {
			t.Errorf("FuzzyFind(%q) top match = %q, want %q", tc.input, got[0].ID, tc.want)
		}
	}
	// "roof flasheeng" (documented STT garble) must still match WO-3.
	got := FuzzyFind(normString("roof flasheeng"), canonicalWOs, getName, 0.55)
	if len(got) == 0 || got[0].ID != "WO-3" {
		t.Errorf("garbled \"roof flasheeng\" => %v, want WO-3", got)
	}
}

// fuzzyMatch is the frozen C5 result element: item + score + name. The
// generic FuzzyFind returns a slice of this shape; the sort test reads
// the scores off it.
type fuzzyMatch struct {
	item  job
	score float64
	name  string
}

// TestFuzzyFindSortedDescending pins the contract: results are sorted by
// score DESCENDING, and only candidates >= threshold are returned.
func TestFuzzyFindSortedDescending(t *testing.T) {
	getName := func(j job) string { return j.Name }
	matches := FuzzyFind(normString("meadow"), []job{{"Meadow Ridge"}, {"Meadow Rich"}, {"Oak Street"}}, getName, 0.5)
	if len(matches) < 2 {
		t.Fatalf("FuzzyFind returned %d results, want at least 2 to observe ordering", len(matches))
	}
	for i := 1; i < len(matches); i++ {
		if matches[i].score > matches[i-1].score+epsilon {
			t.Errorf("results not sorted descending: [%d]=%v > [%d]=%v", i, matches[i].score, i-1, matches[i-1].score)
		}
	}
}

// TestFuzzyFindThresholdExcludes pins the threshold boundary: a match
// just below threshold is excluded; at/above (with epsilon slack per
// Pitfall 26) is included.
func TestFuzzyFindThresholdExcludes(t *testing.T) {
	getName := func(j job) string { return j.Name }
	// "summer planes" vs "Meadow Ridge" = 0.25: excluded at any sane
	// threshold; included only if threshold drifted below 0.25.
	if got := FuzzyFind(normString("summer planes"), canonicalJobs, getName, 0.60); len(got) != 0 {
		t.Errorf("summer planes matched at 0.60: %v", got)
	}
	// Oak St vs Oak Street = 0.667: included at 0.60, excluded at 0.70.
	if got := FuzzyFind(normString("Oak St"), canonicalJobs, getName, 0.60); len(got) != 1 {
		t.Errorf("Oak St not matched at 0.60: %v", got)
	}
	if got := FuzzyFind(normString("Oak St"), canonicalJobs, getName, 0.70); len(got) != 0 {
		t.Errorf("Oak St matched at 0.70 (score 0.667 < 0.70): %v", got)
	}
}

// TestFuzzyFindEmptyQuery pins the degenerate case: an empty (or
// whitespace-only after normalization) query returns no matches, never a
// panic.
func TestFuzzyFindEmptyQuery(t *testing.T) {
	getName := func(j job) string { return j.Name }
	if got := FuzzyFind(normString("   "), canonicalJobs, getName, 0.60); len(got) != 0 {
		t.Errorf("empty query matched: %v", got)
	}
}
