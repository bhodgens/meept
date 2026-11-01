// Package trajectorydiff provides a graded sequence comparator for
// recorded tool-call trajectories: normalize golden and actual call lists
// (tool name + argument shape, variable-value scrubbing, sorted-key
// canonical JSON), score them under a configurable match mode with a pass
// threshold, and return diagnostic step diffs.
//
// The comparator is a pure function: no I/O, no wall clock, no
// randomness. Float comparisons use a small epsilon.
package trajectorydiff

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/caimlas/meept/internal/eval"
	"github.com/caimlas/meept/internal/session"
)

// NormalizedCall is one recorded tool call, normalized for comparison:
// tool name + argument SHAPE (per-tool argument rules applied; JSON
// object keys sorted; list order preserved; variable values such as
// generated IDs, timestamps, and paths under the temp workdir replaced
// per Rule). Equality of two NormalizedCall values is deterministic.
type NormalizedCall struct {
	Name       string         `json:"name"`
	ArgsShape  map[string]any `json:"args_shape"`  // sorted-key canonical JSON
	ArgsRaw    string         `json:"args_raw"`    // original arguments, verbatim
	InstanceID string         `json:"instance_id"` // stable per-position ID (name#ordinal)
}

// MatchMode selects how golden and actual sequences are scored.
type MatchMode string

// Match modes.
const (
	MatchExact    MatchMode = "exact"     // same length, position-wise equal
	MatchInOrder  MatchMode = "in_order"  // golden is an ordered subsequence of actual
	MatchAnyOrder MatchMode = "any_order" // golden multiset contained in actual multiset
)

// ArgRule selects how one tool's arguments are compared.
type ArgRule string

// Argument rules.
const (
	ArgRuleExact  ArgRule = "exact"  // JSON-value equality
	ArgRuleSubset ArgRule = "subset" // expected keys/values contained in actual object
	ArgRuleIgnore ArgRule = "ignore" // arguments excluded from comparison
)

// Options configures Compare. Zero value is valid: Mode defaults to
// MatchInOrder and Threshold defaults to 1.0.
type Options struct {
	Mode          MatchMode
	Threshold     float64
	ArgRules      map[string]ArgRule // per-tool-name argument rule override
	IgnoreCallSet map[string]bool    // tool names excluded entirely
}

// StepDiff is one diagnostic entry: a golden/actual pair (Matched), a
// missing golden step (Actual nil), or an extra actual step (Golden nil).
type StepDiff struct {
	Golden  *NormalizedCall // nil when the step is extra (in actual only)
	Actual  *NormalizedCall // nil when the step is missing (in golden only)
	Matched bool
}

// Result is the Compare verdict with diagnostics.
type Result struct {
	Passed    bool
	Score     float64
	Mode      MatchMode
	Steps     []StepDiff
	FirstDiff int    // 1-based golden index of first mismatch; 0 when Passed
	Summary   string // one-line human-readable verdict
}

// epsilon is the tolerance for threshold comparison.
const epsilon = 1e-9

// varKeySubstrings are the case-insensitive substrings that mark a JSON
// key as carrying a variable value (generated IDs, timestamps, paths,
// tokens, session identifiers).
var varKeySubstrings = []string{"id", "uuid", "timestamp", "time", "path", "token", "session"}

// varPlaceholder replaces scrubbed variable values.
const varPlaceholder = "<var>"

// isVariableKey reports whether a JSON object key matches the
// variable-value rule (case-insensitive substring match).
func isVariableKey(key string) bool {
	lower := strings.ToLower(key)
	for _, sub := range varKeySubstrings {
		if strings.Contains(lower, sub) {
			return true
		}
	}
	return false
}

// Normalize parses one recorded tool call's arguments JSON into its
// comparable shape. Values under variable-valued keys (case-insensitive
// substring match on id|uuid|timestamp|time|path|token|session) are
// scrubbed to "<var>"; nested objects recurse; lists keep order and
// element shapes. Invalid JSON is an error.
func Normalize(name string, argumentsJSON string) (NormalizedCall, error) {
	if strings.TrimSpace(argumentsJSON) == "" {
		argumentsJSON = "{}"
	}
	var shape map[string]any
	if err := json.Unmarshal([]byte(argumentsJSON), &shape); err != nil {
		return NormalizedCall{}, fmt.Errorf("trajectorydiff: normalize %q: parse arguments: %w", name, err)
	}
	return NormalizedCall{
		Name:      name,
		ArgsShape: scrubObject(shape),
		ArgsRaw:   argumentsJSON,
		// Standalone normalization has exactly one call: ordinal 0.
		// NormalizeSessionCalls overwrites this with the true
		// per-sequence ordinal.
		InstanceID: fmt.Sprintf("%s#%d", name, 0),
	}, nil
}

// scrubObject applies the variable-key scrubbing rule recursively. An
// object key matching the rule replaces its value with the placeholder;
// nested objects recurse; lists keep order and element shapes.
func scrubObject(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if isVariableKey(k) {
			out[k] = varPlaceholder
			continue
		}
		out[k] = scrubValue(v)
	}
	return out
}

// scrubValue recurses into lists and (non-scrubbed) objects.
func scrubValue(v any) any {
	switch t := v.(type) {
	case map[string]any:
		return scrubObject(t)
	case []any:
		out := make([]any, len(t))
		for i, el := range t {
			out[i] = scrubValue(el)
		}
		return out
	default:
		return v
	}
}

// NormalizeSessionCalls maps recorded session tool calls (as returned by
// GetToolCalls with ORDER BY seq) to comparable shape, preserving input
// order. InstanceID is name#ordinal where ordinal is the 0-based call
// index for that tool name in the sequence.
func NormalizeSessionCalls(calls []session.ToolCall) ([]NormalizedCall, error) {
	out := make([]NormalizedCall, 0, len(calls))
	ordinal := map[string]int{}
	for i, tc := range calls {
		nc, err := Normalize(tc.ToolName, tc.Arguments)
		if err != nil {
			return nil, fmt.Errorf("trajectorydiff: call %d: %w", i, err)
		}
		ord := ordinal[nc.Name]
		ordinal[nc.Name] = ord + 1
		nc.InstanceID = fmt.Sprintf("%s#%d", nc.Name, ord)
		out = append(out, nc)
	}
	return out, nil
}

// StepsFromGolden maps normalized calls to judge steps for reuse by
// internal/eval Judge(): every step is clean (Failed=false, Err="").
func StepsFromGolden(calls []NormalizedCall) []eval.Step {
	steps := make([]eval.Step, 0, len(calls))
	for _, nc := range calls {
		steps = append(steps, eval.Step{Name: nc.Name})
	}
	return steps
}

// Compare scores the actual tool-call sequence against the golden one
// under opts and returns the verdict plus per-step diagnostics. Compare
// never mutates its inputs and never fails: an imperfect match is a low
// Score, not an error.
func Compare(golden, actual []NormalizedCall, opts Options) Result {
	mode := opts.Mode
	if mode == "" {
		mode = MatchInOrder
	}
	threshold := opts.Threshold
	if opts.Threshold == 0 {
		threshold = 1.0
	}

	golden = applyIgnoreCallSet(golden, opts.IgnoreCallSet)
	actual = applyIgnoreCallSet(actual, opts.IgnoreCallSet)

	var score float64
	var steps []StepDiff
	var firstDiff int

	switch mode {
	case MatchExact:
		score, steps, firstDiff = compareExact(golden, actual, opts.ArgRules)
	case MatchAnyOrder:
		score, steps, firstDiff = compareAnyOrder(golden, actual, opts.ArgRules)
	case MatchInOrder:
		fallthrough
	default:
		mode = MatchInOrder
		score, steps, firstDiff = compareInOrder(golden, actual, opts.ArgRules)
	}

	passed := score+epsilon >= threshold
	if passed {
		firstDiff = 0
	}

	summary := buildSummary(mode, score, threshold, len(golden), passed, firstDiff)

	return Result{
		Passed:    passed,
		Score:     score,
		Mode:      mode,
		Steps:     steps,
		FirstDiff: firstDiff,
		Summary:   summary,
	}
}

// buildSummary renders the one-line verdict.
func buildSummary(mode MatchMode, score, threshold float64, goldenLen int, passed bool, firstDiff int) string {
	matchedApprox := int(score*float64(goldenLen) + epsilon)
	summary := fmt.Sprintf("%s: score %.3f threshold %.3f: %d/%d golden steps matched",
		mode, score, threshold, matchedApprox, goldenLen)
	if passed {
		return summary + " (PASS)"
	}
	if firstDiff > 0 {
		return summary + fmt.Sprintf(" (FAIL first diff at golden step %d)", firstDiff)
	}
	return summary + " (FAIL)"
}

// applyIgnoreCallSet removes calls whose tool name is excluded entirely,
// from either side.
func applyIgnoreCallSet(calls []NormalizedCall, ignore map[string]bool) []NormalizedCall {
	if len(ignore) == 0 {
		return calls
	}
	out := make([]NormalizedCall, 0, len(calls))
	for _, nc := range calls {
		if ignore[nc.Name] {
			continue
		}
		out = append(out, nc)
	}
	return out
}

// argsEqual compares one golden call against one actual call under the
// tool's argument rule (default exact). Call names are compared by the
// caller; arguments only here.
func argsEqual(g, a NormalizedCall, rules map[string]ArgRule) bool {
	switch rules[g.Name] {
	case ArgRuleIgnore:
		return true
	case ArgRuleSubset:
		return subsetOf(g.ArgsShape, a.ArgsShape)
	default: // ArgRuleExact and unknown rules
		return equalValues(g.ArgsShape, a.ArgsShape)
	}
}

// subsetOf reports whether every expected key/value in expected is
// present (JSON-value equal) in actual.
func subsetOf(expected, actual map[string]any) bool {
	for k, v := range expected {
		av, ok := actual[k]
		if !ok || !equalValues(v, av) {
			return false
		}
	}
	return true
}

// equalValues is deterministic JSON-value equality via canonical
// (sorted-key) JSON bytes; encoding/json emits map keys in sorted order.
func equalValues(a, b any) bool {
	ab, errA := json.Marshal(a)
	bb, errB := json.Marshal(b)
	if errA != nil || errB != nil {
		return false
	}
	return string(ab) == string(bb)
}

// callEq reports full NormalizedCall equality under the tool's rule.
func callEq(g, a NormalizedCall, rules map[string]ArgRule) bool {
	return g.Name == a.Name && argsEqual(g, a, rules)
}

// compareExact requires same length and position-wise equality. Score is
// 1.0 iff fully equal (same length, all positions matched); otherwise the
// length-ratio penalty matched/max(len(golden), len(actual)) - extra
// actual steps penalize exact mode, unlike in_order/any_order.
func compareExact(golden, actual []NormalizedCall, rules map[string]ArgRule) (float64, []StepDiff, int) {
	steps := make([]StepDiff, 0, max(len(golden), len(actual)))
	matched := 0
	firstDiff := 0
	for i := 0; i < max(len(golden), len(actual)); i++ {
		switch {
		case i < len(golden) && i < len(actual):
			ok := callEq(golden[i], actual[i], rules)
			g, a := golden[i], actual[i]
			steps = append(steps, StepDiff{Golden: &g, Actual: &a, Matched: ok})
			if ok {
				matched++
			} else if firstDiff == 0 {
				firstDiff = i + 1
			}
		case i < len(golden):
			g := golden[i]
			steps = append(steps, StepDiff{Golden: &g, Matched: false})
			if firstDiff == 0 {
				firstDiff = i + 1
			}
		default:
			a := actual[i]
			steps = append(steps, StepDiff{Actual: &a, Matched: false})
		}
	}
	if len(golden) == 0 {
		if len(actual) == 0 {
			return 1.0, steps, 0
		}
		return 0.0, steps, 0
	}
	denominator := max(len(golden), len(actual))
	score := float64(matched) / float64(denominator)
	if matched == len(golden) && len(actual) == len(golden) {
		score = 1.0
	}
	return score, steps, firstDiff
}

// compareInOrder scores |LCS(golden, actual)| / len(golden) using the
// classic O(n*m) DP over NormalizedCall equality (Name plus argument
// shape under the tool's rule). Extra actual steps are score-neutral;
// unmatched golden steps lower the score. Diagnostics preserve sequence
// order: each golden step (matched or missing) plus extra actual steps
// interleaved in actual order.
func compareInOrder(golden, actual []NormalizedCall, rules map[string]ArgRule) (float64, []StepDiff, int) {
	n, m := len(golden), len(actual)
	// dp[i][j] = LCS length of golden[i:] and actual[j:]
	dp := make([][]int, n+1)
	for i := range dp {
		dp[i] = make([]int, m+1)
	}
	for i := n - 1; i >= 0; i-- {
		for j := m - 1; j >= 0; j-- {
			if callEq(golden[i], actual[j], rules) {
				dp[i][j] = dp[i+1][j+1] + 1
			} else if dp[i+1][j] >= dp[i][j+1] {
				dp[i][j] = dp[i+1][j]
			} else {
				dp[i][j] = dp[i][j+1]
			}
		}
	}

	steps := make([]StepDiff, 0, n+(m-n)+1)
	firstDiff := 0
	if n == 0 {
		for j := 0; j < m; j++ {
			a := actual[j]
			steps = append(steps, StepDiff{Actual: &a, Matched: false})
		}
		return 1.0, steps, 0
	}

	// Walk the LCS table emitting stable, order-preserving diagnostics.
	// At a mismatch, prefer advancing whichever side keeps the LCS
	// reachable; the table's tie order makes golden-side drops surface
	// deterministically.
	i, j := 0, 0
	for i < n && j < m {
		switch {
		case callEq(golden[i], actual[j], rules):
			g, a := golden[i], actual[j]
			steps = append(steps, StepDiff{Golden: &g, Actual: &a, Matched: true})
			i++
			j++
		case dp[i+1][j] >= dp[i][j+1]:
			g := golden[i]
			steps = append(steps, StepDiff{Golden: &g, Matched: false})
			if firstDiff == 0 {
				firstDiff = i + 1
			}
			i++
		default:
			a := actual[j]
			steps = append(steps, StepDiff{Actual: &a, Matched: false})
			j++
		}
	}
	for ; i < n; i++ {
		g := golden[i]
		steps = append(steps, StepDiff{Golden: &g, Matched: false})
		if firstDiff == 0 {
			firstDiff = i + 1
		}
	}
	for ; j < m; j++ {
		a := actual[j]
		steps = append(steps, StepDiff{Actual: &a, Matched: false})
	}

	return float64(dp[0][0]) / float64(n), steps, firstDiff
}

// compareAnyOrder scores matched-multiset/len(golden): each golden call
// matches one unconsumed actual call (consumption gives multiset
// semantics: two golden A calls need two actual A calls). Diagnostics
// preserve golden order, with leftover actual steps appended as extras.
func compareAnyOrder(golden, actual []NormalizedCall, rules map[string]ArgRule) (float64, []StepDiff, int) {
	consumed := make([]bool, len(actual))
	matchOf := make([]int, len(golden))
	for i := range matchOf {
		matchOf[i] = -1
	}
	for i := range golden {
		for j := range actual {
			if consumed[j] {
				continue
			}
			if callEq(golden[i], actual[j], rules) {
				matchOf[i] = j
				consumed[j] = true
				break
			}
		}
	}

	steps := make([]StepDiff, 0, max(len(golden), len(actual)))
	firstDiff := 0
	matched := 0
	for i := range golden {
		g := golden[i]
		if matchOf[i] >= 0 {
			a := actual[matchOf[i]]
			steps = append(steps, StepDiff{Golden: &g, Actual: &a, Matched: true})
			matched++
		} else {
			steps = append(steps, StepDiff{Golden: &g, Matched: false})
			if firstDiff == 0 {
				firstDiff = i + 1
			}
		}
	}
	for j := range actual {
		if !consumed[j] {
			a := actual[j]
			steps = append(steps, StepDiff{Actual: &a, Matched: false})
		}
	}

	if len(golden) == 0 {
		return 1.0, steps, 0
	}
	return float64(matched) / float64(len(golden)), steps, firstDiff
}
