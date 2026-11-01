package trajectorydiff

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/eval"
	"github.com/caimlas/meept/internal/session"
)

func mustNorm(t *testing.T, name string, argsJSON string) NormalizedCall {
	t.Helper()
	nc, err := Normalize(name, argsJSON)
	if err != nil {
		t.Fatalf("Normalize(%q, %s): unexpected error: %v", name, argsJSON, err)
	}
	return nc
}

func mustNormSeq(t *testing.T, name string, argsJSON string, seq int) session.ToolCall {
	t.Helper()
	return session.ToolCall{
		ID:        int64(seq + 1),
		MessageID: 1,
		ToolName:  name,
		Arguments: argsJSON,
		Seq:       seq,
	}
}

// canonicalJSON renders ArgsShape as sorted-key JSON without HTML
// escaping, so "<var>" stays literal.
func canonicalJSON(t *testing.T, nc NormalizedCall) string {
	t.Helper()
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(nc.ArgsShape); err != nil {
		t.Fatalf("encode ArgsShape: %v", err)
	}
	return strings.TrimSuffix(sb.String(), "\n")
}

// --- T1: Normalize ---

func TestNormalizeScrubVariableKeys(t *testing.T) {
	nc := mustNorm(t, "read_file", `{"path":"/tmp/x1","op":"read"}`)
	got := canonicalJSON(t, nc)
	want := `{"op":"read","path":"<var>"}`
	if got != want {
		t.Fatalf("canonical ArgsShape = %s, want %s", got, want)
	}
	if nc.Name != "read_file" {
		t.Fatalf("Name = %q, want %q", nc.Name, "read_file")
	}
	if nc.ArgsRaw != `{"path":"/tmp/x1","op":"read"}` {
		t.Fatalf("ArgsRaw = %q, want verbatim input", nc.ArgsRaw)
	}
	if nc.InstanceID == "" {
		t.Fatal("InstanceID must be non-empty")
	}
}

func TestNormalizeSortedKeyCanonicalForm(t *testing.T) {
	nc := mustNorm(t, "tool_a", `{"zeta":1,"alpha":2,"nested":{"b":3,"a":4}}`)
	// sorted at both top level and nested level
	want := `{"alpha":2,"nested":{"a":4,"b":3},"zeta":1}`
	if got := canonicalJSON(t, nc); got != want {
		t.Fatalf("canonical ArgsShape = %s, want %s", got, want)
	}
}

func TestNormalizeInvalidJSONErrors(t *testing.T) {
	if _, err := Normalize("tool_a", `{not json`); err == nil {
		t.Fatal("want error for invalid JSON, got nil")
	}
}

func TestNormalizeVariableKeyPatternsCaseInsensitive(t *testing.T) {
	cases := []struct{ key, want string }{
		{"id", "<var>"},
		{"ID", "<var>"},
		{"Uuid", "<var>"},
		{"TIMESTAMP", "<var>"},
		{"created_time", "<var>"},
		{"path", "<var>"},
		{"api_token", "<var>"},
		{"session_id", "<var>"},
		{"ordinary", "keep"},
		{"identity_card", "<var>"}, // substring match
	}
	args := map[string]string{}
	for _, c := range cases {
		args[c.key] = "keep"
	}
	b, err := json.Marshal(args)
	if err != nil {
		t.Fatalf("marshal fixture: %v", err)
	}
	nc := mustNorm(t, "tool_a", string(b))
	for _, c := range cases {
		if got, ok := nc.ArgsShape[c.key].(string); ok && got != c.want {
			t.Errorf("key %q = %q, want %q", c.key, got, c.want)
		} else if !ok && c.want == "<var>" {
			t.Errorf("key %q = %v, want %q", c.key, nc.ArgsShape[c.key], c.want)
		}
	}
}

func TestNormalizeListsKeepOrderAndShape(t *testing.T) {
	nc := mustNorm(t, "tool_a", `{"files":["/a/p1","/b/p2"],"count":2}`)
	files, ok := nc.ArgsShape["files"].([]any)
	if !ok {
		t.Fatalf("files = %T, want []any", nc.ArgsShape["files"])
	}
	if len(files) != 2 {
		t.Fatalf("len(files) = %d, want 2", len(files))
	}
	// order and element shapes are preserved; "files" is not a variable
	// key, so element values pass through unsanitized
	if files[0] != "/a/p1" || files[1] != "/b/p2" {
		t.Fatalf("files = %v, want original order/values", files)
	}
	// elements keep their shapes; nested object elements have their
	// variable keys scrubbed recursively while list order is preserved
	nc2 := mustNorm(t, "tool_a", `{"items":[{"path":"/a/p1","op":"read"},{"path":"/b/p2","op":"write"}]}`)
	items := nc2.ArgsShape["items"].([]any)
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	first, ok := items[0].(map[string]any)
	if !ok {
		t.Fatalf("items[0] = %T, want map", items[0])
	}
	if first["path"] != "<var>" || first["op"] != "read" {
		t.Fatalf("items[0] = %v, want scrubbed path, kept op", first)
	}
	if nc.ArgsShape["count"] != float64(2) {
		t.Fatalf("count = %v, want 2", nc.ArgsShape["count"])
	}
}

// --- T2: NormalizeSessionCalls + StepsFromGolden ---

func TestNormalizeSessionCallsPreservesSeqOrder(t *testing.T) {
	// seq-ordered input (0,1,2) as GetToolCalls (ORDER BY seq) supplies
	calls := []session.ToolCall{
		mustNormSeq(t, "tool_a", `{}`, 0),
		mustNormSeq(t, "tool_c", `{}`, 1),
		mustNormSeq(t, "tool_a", `{}`, 2),
	}
	got, err := NormalizeSessionCalls(calls)
	if err != nil {
		t.Fatalf("NormalizeSessionCalls: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("len = %d, want 3", len(got))
	}
	wantNames := []string{"tool_a", "tool_c", "tool_a"}
	for i, w := range wantNames {
		if got[i].Name != w {
			t.Errorf("pos %d Name = %q, want %q", i, got[i].Name, w)
		}
	}
	// instance ids are stable per position: name#ordinal, ordinal
	// counting occurrences of that name in sequence order
	if got[0].InstanceID != "tool_a#0" {
		t.Errorf("InstanceID[0] = %q, want %q", got[0].InstanceID, "tool_a#0")
	}
	if got[1].InstanceID != "tool_c#0" {
		t.Errorf("InstanceID[1] = %q, want %q", got[1].InstanceID, "tool_c#0")
	}
	if got[2].InstanceID != "tool_a#1" {
		t.Errorf("InstanceID[2] = %q, want %q", got[2].InstanceID, "tool_a#1")
	}
}

func TestNormalizeSessionCallsInvalidArgErrors(t *testing.T) {
	calls := []session.ToolCall{mustNormSeq(t, "tool_a", `{bad`, 0)}
	if _, err := NormalizeSessionCalls(calls); err == nil {
		t.Fatal("want error for invalid arguments JSON, got nil")
	}
}

func TestStepsFromGoldenMapsCleanSteps(t *testing.T) {
	calls := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{"x":1}`),
	}
	steps := StepsFromGolden(calls)
	if len(steps) != 2 {
		t.Fatalf("len(steps) = %d, want 2", len(steps))
	}
	for i, s := range steps {
		if s.Name != calls[i].Name {
			t.Errorf("steps[%d].Name = %q, want %q", i, s.Name, calls[i].Name)
		}
		if s.Failed {
			t.Errorf("steps[%d].Failed = true, want false", i)
		}
		if s.Err != "" {
			t.Errorf("steps[%d].Err = %q, want empty", i, s.Err)
		}
	}
	var _ []eval.Step = steps
}

// --- T3: Compare EXACT ---

func TestCompareExactModeEqual(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "read_file", `{"op":"read"}`),
		mustNorm(t, "write_file", `{"op":"write"}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "read_file", `{"op":"read"}`),
		mustNorm(t, "write_file", `{"op":"write"}`),
	}
	r := Compare(g, a, Options{Mode: MatchExact})
	if !r.Passed || r.Score != 1.0 || r.FirstDiff != 0 {
		t.Fatalf("got %+v, want Passed Score=1.0 FirstDiff=0", r)
	}
}

func TestCompareExactModeMismatchFails(t *testing.T) {
	g := []NormalizedCall{mustNorm(t, "read_file", `{"op":"read"}`)}
	a := []NormalizedCall{mustNorm(t, "write_file", `{"op":"read"}`)}
	r := Compare(g, a, Options{Mode: MatchExact})
	if r.Passed || r.FirstDiff != 1 {
		t.Fatalf("got %+v, want failed with FirstDiff=1", r)
	}
}

func TestCompareExactLengthMismatchFails(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
	}
	a := []NormalizedCall{mustNorm(t, "tool_a", `{}`)}
	r := Compare(g, a, Options{Mode: MatchExact})
	if r.Passed {
		t.Fatalf("got %+v, want failed", r)
	}
	if r.FirstDiff != 2 {
		t.Fatalf("FirstDiff = %d, want 2 (first missing golden position)", r.FirstDiff)
	}
}

// --- T4: in_order + any_order with threshold/epsilon ---

func TestCompareInOrderWithExtrasPasses(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
		mustNorm(t, "tool_c", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "tool_x", `{}`),
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
		mustNorm(t, "tool_c", `{}`),
		mustNorm(t, "tool_y", `{}`),
	}
	r := Compare(g, a, Options{Mode: MatchInOrder, Threshold: 1.0})
	if !r.Passed {
		t.Fatalf("got %+v, want Passed", r)
	}
	if r.Score != 1.0 {
		t.Fatalf("Score = %v, want 1.0", r.Score)
	}
}

func TestCompareInOrderPartialScore(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
		mustNorm(t, "tool_c", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
	}
	r := Compare(g, a, Options{Mode: MatchInOrder, Threshold: 1.0})
	if r.Passed {
		t.Fatalf("got %+v, want failed", r)
	}
	if diff := r.Score - 2.0/3.0; diff > 1e-9 || diff < -1e-9 {
		t.Fatalf("Score = %v, want ~=0.667", r.Score)
	}
}

func TestCompareInOrderReorderIsPartial(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "tool_b", `{}`),
		mustNorm(t, "tool_a", `{}`),
	}
	r := Compare(g, a, Options{Mode: MatchInOrder, Threshold: 1.0})
	if r.Passed {
		t.Fatalf("got %+v, want failed (reorder is not an ordered subsequence)", r)
	}
	if r.Score != 0.5 {
		t.Fatalf("Score = %v, want 0.5 (LCS=1)", r.Score)
	}
}

func TestCompareAnyOrderMultisetSemantics(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_a", `{}`),
	}
	a := []NormalizedCall{mustNorm(t, "tool_a", `{}`)}
	r := Compare(g, a, Options{Mode: MatchAnyOrder, Threshold: 1.0})
	if r.Passed {
		t.Fatalf("got %+v, want failed (two golden A need two actual A)", r)
	}
	if r.Score != 0.5 {
		t.Fatalf("Score = %v, want 0.5", r.Score)
	}
}

func TestCompareAnyOrderWithExtrasPasses(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_b", `{}`),
		mustNorm(t, "tool_a", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_x", `{}`),
		mustNorm(t, "tool_b", `{}`),
	}
	r := Compare(g, a, Options{Mode: MatchAnyOrder, Threshold: 1.0})
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("got %+v, want Passed Score=1.0", r)
	}
}

func TestCompareThresholdBoundaryPasses(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
		mustNorm(t, "tool_c", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
	}
	// Score exactly 2/3; threshold exactly 2/3 -> passes (Score >= Threshold)
	threshold := 2.0 / 3.0
	r := Compare(g, a, Options{Mode: MatchInOrder, Threshold: threshold})
	if !r.Passed {
		t.Fatalf("got %+v, want Passed at Score == Threshold (epsilon)", r)
	}
}

func TestCompareEmptyGoldenPasses(t *testing.T) {
	a := []NormalizedCall{mustNorm(t, "tool_a", `{}`)}
	r := Compare(nil, a, Options{Mode: MatchInOrder})
	if !r.Passed {
		t.Fatalf("got %+v, want Passed for empty golden", r)
	}
}

// --- T5: Options plumbing ---

func TestCompareDefaultsModeAndThreshold(t *testing.T) {
	// zero Options: Mode defaults in_order, Threshold defaults 1.0
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "tool_x", `{}`),
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
		mustNorm(t, "tool_y", `{}`),
	}
	r := Compare(g, a, Options{})
	if r.Mode != MatchInOrder {
		t.Fatalf("Mode = %q, want %q", r.Mode, MatchInOrder)
	}
	if !r.Passed || r.Score != 1.0 {
		t.Fatalf("got %+v, want Passed Score=1.0 (default threshold 1.0)", r)
	}
}

func TestCompareArgRuleIgnore(t *testing.T) {
	g := []NormalizedCall{mustNorm(t, "web_search", `{"search_path":"/tmp/a","q":"x"}`)}
	a := []NormalizedCall{mustNorm(t, "web_search", `{"search_path":"/tmp/b","q":"x"}`)}
	r := Compare(g, a, Options{Mode: MatchExact, ArgRules: map[string]ArgRule{"web_search": ArgRuleIgnore}})
	if !r.Passed || r.FirstDiff != 0 {
		t.Fatalf("got %+v, want Passed (search_path never affects verdict)", r)
	}
	// sanity: a non-variable-argument difference must still fail under
	// exact without the ignore rule (search_path itself is already
	// scrubbed by the normalizer as a variable key)
	g2 := []NormalizedCall{mustNorm(t, "web_search", `{"search_path":"/tmp/a","q":"x"}`)}
	a2 := []NormalizedCall{mustNorm(t, "web_search", `{"search_path":"/tmp/a","q":"DIFFERENT"}`)}
	r2 := Compare(g2, a2, Options{Mode: MatchExact})
	if r2.Passed {
		t.Fatal("without ignore rule, differing non-variable args must fail")
	}
}

func TestCompareArgRuleSubset(t *testing.T) {
	g := []NormalizedCall{mustNorm(t, "tool_a", `{"required":"yes"}`)}
	a := []NormalizedCall{mustNorm(t, "tool_a", `{"required":"yes","extra":42}`)}
	r := Compare(g, a, Options{Mode: MatchExact, ArgRules: map[string]ArgRule{"tool_a": ArgRuleSubset}})
	if !r.Passed {
		t.Fatalf("got %+v, want Passed (expected keys/values contained in actual)", r)
	}
	// missing required key fails subset
	a2 := []NormalizedCall{mustNorm(t, "tool_a", `{"extra":42}`)}
	r2 := Compare(g, a2, Options{Mode: MatchExact, ArgRules: map[string]ArgRule{"tool_a": ArgRuleSubset}})
	if r2.Passed {
		t.Fatal("subset with missing required key must fail")
	}
}

func TestCompareArgRuleExactDefault(t *testing.T) {
	g := []NormalizedCall{mustNorm(t, "tool_a", `{"a":1}`)}
	a := []NormalizedCall{mustNorm(t, "tool_a", `{"a":1,"b":2}`)}
	r := Compare(g, a, Options{Mode: MatchExact, ArgRules: map[string]ArgRule{"tool_a": ArgRuleExact}})
	if r.Passed {
		t.Fatal("exact rule with extra actual key must fail")
	}
}

func TestCompareIgnoreCallSet(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "memory_write", `{"k":"v"}`),
		mustNorm(t, "tool_a", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "memory_write", `{"k":"DIFFERENT"}`),
		mustNorm(t, "tool_a", `{}`),
	}
	r := Compare(g, a, Options{Mode: MatchExact, IgnoreCallSet: map[string]bool{"memory_write": true}})
	if !r.Passed || r.FirstDiff != 0 {
		t.Fatalf("got %+v, want Passed (memory_write excluded from both sides)", r)
	}
}

func TestCompareResultSummaryAndSteps(t *testing.T) {
	g := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_b", `{}`),
	}
	a := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_x", `{}`),
	}
	r := Compare(g, a, Options{Mode: MatchExact})
	if r.Summary == "" {
		t.Fatal("Summary must be non-empty")
	}
	if len(r.Steps) == 0 {
		t.Fatal("Steps diagnostics must be non-empty on failure")
	}
	// stable order: golden index 2 mismatched
	if r.Steps[1].Matched {
		t.Errorf("Steps[1].Matched = true, want false")
	}
	if r.Steps[1].Golden == nil {
		t.Errorf("Steps[1].Golden = nil, want the golden call")
	}
}

func TestCompareExtraActualStepDiagnostic(t *testing.T) {
	g := []NormalizedCall{mustNorm(t, "tool_a", `{}`)}
	a := []NormalizedCall{
		mustNorm(t, "tool_a", `{}`),
		mustNorm(t, "tool_extra", `{}`),
	}
	r := Compare(g, a, Options{Mode: MatchExact})
	if r.Passed {
		t.Fatalf("exact mode with extra actual step must fail: %+v", r)
	}
	// find the extra-step diagnostic: Actual set, Golden nil
	found := false
	for _, s := range r.Steps {
		if s.Golden == nil && s.Actual != nil && s.Actual.Name == "tool_extra" {
			found = true
		}
	}
	if !found {
		t.Fatalf("want a Golden=nil/Actual=tool_extra StepDiff, got %+v", r.Steps)
	}
}

func TestNormalizeErrorWrapsPackagePrefix(t *testing.T) {
	_, err := Normalize("tool_a", `{bad`)
	if err == nil {
		t.Fatal("want error")
	}
	if !contains(err.Error(), "trajectorydiff:") {
		t.Fatalf("error %q must carry trajectorydiff: prefix", err.Error())
	}
}

func contains(haystack, needle string) bool {
	return len(haystack) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(haystack); i++ {
			if haystack[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	})()
}

var _ = fmt.Sprintf // placeholder to keep fmt import when cases shrink
