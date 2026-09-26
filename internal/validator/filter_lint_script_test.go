package validator

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// requiresTool skips the test when the binary is not resolvable: the
// script linters degrade to pass on hosts missing their toolchain, and the
// unit tests mirror that contract rather than failing on tool-less hosts.
func requiresTool(t *testing.T, bin string) {
	t.Helper()
	if _, err := exec.LookPath(bin); err != nil {
		t.Skipf("%s not on PATH; skipping (host lacks toolchain, filter degrades to pass)", bin)
	}
}

func TestPythonLintFilter_NameAndApplies(t *testing.T) {
	f := NewPythonLintFilter("")
	if f.Name() != "lint_python" {
		t.Fatalf("Name = %q, want lint_python", f.Name())
	}
	if !f.Applies(&task.TaskStep{}) {
		t.Fatal("Applies = false for a non-nil step; filter is declared by chain membership")
	}
	if f.Applies(nil) {
		t.Fatal("Applies = true for nil step")
	}
}

func TestPythonLintFilter_CleanBlockPasses(t *testing.T) {
	f := NewPythonLintFilter("python3")
	res := f.Process(context.Background(), &task.TaskStep{},
		"Here is the helper:\n\n```python\ndef add(a, b):\n    return a + b\n```\n")
	if res.Outcome != FilterPass {
		t.Fatalf("Outcome = %v, want pass (clean block); reason: %s", res.Outcome, res.Reason)
	}
}

func TestPythonLintFilter_SyntaxErrorFails(t *testing.T) {
	f := NewPythonLintFilter("python3")
	res := f.Process(context.Background(), &task.TaskStep{},
		"```python\ndef broken(:\n    pass\n```\n")
	if res.Outcome != FilterAdvisory {
		t.Fatalf("Outcome = %v, want advisory (syntax error); reason: %s", res.Outcome, res.Reason)
	}
	if !strings.HasPrefix(res.Reason, "lint_python: snippet_0.py: ") {
		t.Fatalf("Reason = %q, want the lint_python: <file>: <detail> shape", res.Reason)
	}
}

func TestPythonLintFilter_NonPythonPasses(t *testing.T) {
	f := NewPythonLintFilter("python3")
	res := f.Process(context.Background(), &task.TaskStep{},
		"Plain text answer with no code blocks at all.")
	if res.Outcome != FilterPass {
		t.Fatalf("Outcome = %v, want pass (no python content)", res.Outcome)
	}
}

func TestPythonLintFilter_Idempotent(t *testing.T) {
	f := NewPythonLintFilter("python3")
	output := "```python\nx = 1\n```\n"
	// A pure check: identical input always yields the identical verdict.
	r1 := f.Process(context.Background(), &task.TaskStep{}, output)
	r2 := f.Process(context.Background(), &task.TaskStep{}, output)
	if r1.Outcome != r2.Outcome || r1.Reason != r2.Reason {
		t.Fatalf("not idempotent: %+v vs %+v", r1, r2)
	}
}

func TestPythonLintFilter_MissingBinaryFails(t *testing.T) {
	f := NewPythonLintFilter("/nonexistent/python-for-filter-test")
	res := f.Process(context.Background(), &task.TaskStep{},
		"```python\nx = 1\n```\n")
	if res.Outcome != FilterAdvisory {
		t.Fatalf("Outcome = %v, want advisory (missing binary is a hard failure, unlike lint_js)", res.Outcome)
	}
	if !strings.HasPrefix(res.Reason, "lint_python: ") {
		t.Fatalf("Reason = %q, want lint_python: prefix", res.Reason)
	}
}

func TestJSLintFilter_NameAndApplies(t *testing.T) {
	f := NewJSLintFilter("", "")
	if f.Name() != "lint_js" {
		t.Fatalf("Name = %q, want lint_js", f.Name())
	}
	if !f.Applies(&task.TaskStep{}) || f.Applies(nil) {
		t.Fatal("Applies must be declared-by-membership (non-nil true, nil false)")
	}
}

func TestJSLintFilter_CleanJSBlockPasses(t *testing.T) {
	requiresTool(t, "node")
	f := NewJSLintFilter("", "")
	res := f.Process(context.Background(), &task.TaskStep{},
		"```js\nfunction add(a, b) {\n  return a + b;\n}\n```\n")
	if res.Outcome != FilterPass {
		t.Fatalf("Outcome = %v, want pass (clean block); reason: %s", res.Outcome, res.Reason)
	}
}

func TestJSLintFilter_SyntaxErrorFails(t *testing.T) {
	requiresTool(t, "node")
	f := NewJSLintFilter("", "")
	res := f.Process(context.Background(), &task.TaskStep{},
		"```js\nfunction broken( {\n```\n")
	if res.Outcome != FilterAdvisory {
		t.Fatalf("Outcome = %v, want advisory (syntax error); reason: %s", res.Outcome, res.Reason)
	}
	if !strings.HasPrefix(res.Reason, "lint_js: node: snippet_0.js: ") {
		t.Fatalf("Reason = %q, want the lint_js: node: <file>: <detail> shape", res.Reason)
	}
}

func TestJSLintFilter_CleanTSBlockPasses(t *testing.T) {
	f := NewJSLintFilter("", "")
	if f.lookupTSC() == "" {
		t.Skip("tsc not resolvable; skipping TS positive case (filter degrades to logged skip)")
	}
	res := f.Process(context.Background(), &task.TaskStep{},
		"```ts\nfunction greet(name: string): string {\n  return `hi ${name}`;\n}\n```\n")
	if res.Outcome != FilterPass {
		t.Fatalf("Outcome = %v, want pass (clean TS); reason: %s", res.Outcome, res.Reason)
	}
}

func TestJSLintFilter_TypeErrorFailsWhenTSCAvailable(t *testing.T) {
	f := NewJSLintFilter("", "")
	if f.lookupTSC() == "" {
		t.Skip("tsc not resolvable; TS type errors cannot be checked on this host")
	}
	res := f.Process(context.Background(), &task.TaskStep{},
		"```ts\nconst n: number = \"not a number\";\n```\n")
	if res.Outcome != FilterAdvisory {
		t.Fatalf("Outcome = %v, want advisory (type error); reason: %s", res.Outcome, res.Reason)
	}
}

func TestJSLintFilter_NonJSPasses(t *testing.T) {
	f := NewJSLintFilter("", "")
	res := f.Process(context.Background(), &task.TaskStep{}, "no code here")
	if res.Outcome != FilterPass {
		t.Fatalf("Outcome = %v, want pass (no js content)", res.Outcome)
	}
}

func TestJSLintFilter_MissingTSCSkipsNotFails(t *testing.T) {
	requiresTool(t, "node")
	f := NewJSLintFilter("", "")
	if f.lookupTSC() != "" {
		t.Skip("tsc IS resolvable; the skip contract cannot be exercised on this host")
	}
	// A TS block with a host missing tsc must PASS (logged skip), never
	// fail the step for the host's missing tooling.
	res := f.Process(context.Background(), &task.TaskStep{},
		"```ts\nconst n: number = 1;\n```\n")
	if res.Outcome != FilterPass {
		t.Fatalf("Outcome = %v, want pass (missing tsc = logged skip, not a content failure)", res.Outcome)
	}
}

func TestExtractFencedMulti_OffsetsAndFences(t *testing.T) {
	output := "before\n```python\nx = 1\n```\nmid\n```js\nlet y = 2;\n```\nafter"
	blocks := extractFencedMulti(output, []string{"```python", "```js"})
	if len(blocks) != 2 {
		t.Fatalf("blocks = %d, want 2", len(blocks))
	}
	if blocks[0].fence != "```python" || blocks[1].fence != "```js" {
		t.Fatalf("fences = %q, %q; want python, js", blocks[0].fence, blocks[1].fence)
	}
	// Offsets must slice the original output back to the exact code bytes.
	for _, b := range blocks {
		if got := output[b.start:b.end]; got != b.code {
			t.Fatalf("offset desync: output[%d:%d] = %q, want %q", b.start, b.end, got, b.code)
		}
	}
	if !strings.Contains(blocks[0].code, "x = 1") || !strings.Contains(blocks[1].code, "let y = 2;") {
		t.Fatalf("block codes wrong: %q / %q", blocks[0].code, blocks[1].code)
	}
}

func TestContainsPythonCode_Heuristics(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"```python\nx = 1\n```", true},
		{"```py\nx = 1\n```", true},
		{"def f():\n    return 1", true},
		{"class A:\n    pass", true},
		{"```go\nfunc f() {}\n```", false},
		{"plain prose with a colon: nothing", false},
	}
	for _, c := range cases {
		if got := containsPythonCode(c.in); got != c.want {
			t.Errorf("containsPythonCode(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestBuiltinRegistry_IncludesScriptLinters(t *testing.T) {
	for _, name := range []string{"lint_python", "lint_js"} {
		if _, err := NewBuiltinFilter(name, BuiltinConfig{}); err != nil {
			t.Errorf("NewBuiltinFilter(%q) errored: %v", name, err)
		}
	}
	// The known-set error text must list the new names too.
	_, err := NewBuiltinFilter("nope", BuiltinConfig{})
	if err == nil || !strings.Contains(err.Error(), "lint_python") || !strings.Contains(err.Error(), "lint_js") {
		t.Fatalf("unknown-name error = %v, want it to list lint_python and lint_js", err)
	}
}

func TestDefaultFilters_HostAdaptive(t *testing.T) {
	got := DefaultFilters()
	// The always-safe trio is unconditional.
	for _, want := range []string{"json_format", "language_en", "lint_go"} {
		found := false
		for _, g := range got {
			if g == want {
				found = true
			}
		}
		if !found {
			t.Errorf("DefaultFilters() = %v; missing %q", got, want)
		}
	}
	// Toolchain-dependent entries match what is actually resolvable.
	_, pyErr := exec.LookPath("python3")
	hasPy := pyErr == nil
	_, nodeErr := exec.LookPath("node")
	hasNode := nodeErr == nil
	hasPyInSet, hasNodeInSet := false, false
	for _, g := range got {
		if g == "lint_python" {
			hasPyInSet = true
		}
		if g == "lint_js" {
			hasNodeInSet = true
		}
	}
	if hasPy != hasPyInSet {
		t.Errorf("lint_python in set = %v, but python3 on PATH = %v", hasPyInSet, hasPy)
	}
	if hasNode != hasNodeInSet {
		t.Errorf("lint_js in set = %v, but node on PATH = %v", hasNodeInSet, hasNode)
	}
}

func TestToolOnPath(t *testing.T) {
	if toolOnPath("") {
		t.Fatal("toolOnPath(\"\") = true; empty must be false")
	}
	if !toolOnPath("sh") && !toolOnPath("go") {
		t.Skip("neither sh nor go resolvable; environment too bare for this check")
	}
	if toolOnPath("/nonexistent/binary-xyz") {
		t.Fatal("toolOnPath resolvable a nonexistent binary")
	}
	_ = os.Environ() // keep the os import meaningful if LookPath is stubbed later
}
