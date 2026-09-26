package validator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// badlyFormattedBlock is deliberately un-gofmt-ed Go: wrong indentation and
// alignment that gofmt will change.
const badlyFormattedBlock = `package main

import "fmt"

func main() {
        x := []int{1, 2, 3}
        for _, v := range x {
                    fmt.Println(v)
        }
}
`

// badlyFormattedOutput wraps the bad block with non-Go prose that must stay
// byte-identical through the rewrite.
const badlyFormattedOutput = "Here is the fix you asked for:\n\n```go\n" +
	badlyFormattedBlock + "```\n\nLet me know if anything else comes up.\n"

func TestGoLintFilter_Name(t *testing.T) {
	if got := NewGoLintFilter("", "").Name(); got != "lint_go" {
		t.Fatalf("Name = %q, want lint_go", got)
	}
}

func TestGoLintFilter_ContainsGoCode(t *testing.T) {
	tests := []struct {
		output string
		want   bool
	}{
		{"```go\npackage x\n```", true},
		{"see package main and func main below", true},
		{"just some prose about packages", false},
		{"", false},
		{"```python\nprint(1)\n```", false},
	}
	for _, tc := range tests {
		if got := containsGoCode(tc.output); got != tc.want {
			t.Fatalf("containsGoCode(%q) = %v, want %v", tc.output, got, tc.want)
		}
	}
}

func TestGoLintFilter_ExtractGoBlocks(t *testing.T) {
	// Two fenced blocks plus trailing prose; offsets must slice exactly.
	out := "intro\n```go\npackage a\nfunc a() {}\n```\nmid\n```go\npackage b\nfunc b() {}\n```\ntail\n"
	blocks := extractGoBlocks(out)
	if len(blocks) != 2 {
		t.Fatalf("len(blocks) = %d, want 2: %+v", len(blocks), blocks)
	}
	for i, want := range []string{"package a\nfunc a() {}\n", "package b\nfunc b() {}\n"} {
		if blocks[i].code != want {
			t.Fatalf("block %d code = %q, want %q", i, blocks[i].code, want)
		}
		if out[blocks[i].start:blocks[i].end] != want {
			t.Fatalf("block %d offsets do not slice the original", i)
		}
	}
	// Bare Go source without fences.
	bare := "package main\n\nfunc main() {}\n"
	blocks = extractGoBlocks(bare)
	if len(blocks) != 1 || blocks[0].code != bare {
		t.Fatalf("bare source not detected as one whole block: %+v", blocks)
	}
}

func TestGoLintFilter_ProcessTable(t *testing.T) {
	if _, err := lookGofmt(); err != nil {
		t.Skipf("gofmt not available: %v", err)
	}
	f := NewGoLintFilter("", "")
	ctx := context.Background()

	tests := []struct {
		name    string
		step    *task.TaskStep
		output  string
		want    FilterOutcome
		wantSub string
	}{
		{
			name:   "badly formatted block rewrites",
			step:   &task.TaskStep{Result: "go code"},
			output: badlyFormattedOutput,
			want:   FilterRewrite,
		},
		{
			name:   "already formatted block passes",
			step:   &task.TaskStep{Result: "go code"},
			output: "pre\n```go\npackage main\n\nfunc main() {}\n```\npost\n",
			want:   FilterPass,
		},
		{
			name:   "no go content passes without processing",
			step:   &task.TaskStep{Result: "go code"},
			output: "an ordinary prose answer about the weather\n",
			want:   FilterPass,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := f.Process(ctx, tc.step, tc.output)
			if !res.Valid() {
				t.Fatalf("result invalid: %+v", res)
			}
			if res.Outcome != tc.want {
				t.Fatalf("Outcome = %v, want %v (reason=%q)", res.Outcome, tc.want, res.Reason)
			}
			if tc.wantSub != "" && !strings.Contains(res.Reason, tc.wantSub) {
				t.Fatalf("Reason %q missing %q", res.Reason, tc.wantSub)
			}
		})
	}
}

func TestGoLintFilter_RewriteSubstitution(t *testing.T) {
	if _, err := lookGofmt(); err != nil {
		t.Skipf("gofmt not available: %v", err)
	}
	f := NewGoLintFilter("", "")
	ctx := context.Background()
	step := &task.TaskStep{Result: "go code"}

	res := f.Process(ctx, step, badlyFormattedOutput)
	if res.Outcome != FilterRewrite {
		t.Fatalf("Outcome = %v, want rewrite (reason=%q)", res.Outcome, res.Reason)
	}

	// The prose around the fence is byte-identical; only the block changed.
	prefix := "Here is the fix you asked for:\n\n```go\n"
	suffix := "```\n\nLet me know if anything else comes up.\n"
	if !strings.HasPrefix(res.Output, prefix) || !strings.HasSuffix(res.Output, suffix) {
		t.Fatalf("rewrite did not preserve surrounding prose: %q", res.Output)
	}
	block := strings.TrimSuffix(strings.TrimPrefix(res.Output, prefix), suffix)
	if !strings.Contains(block, "\tfmt.Println(v)") && !gofmtUsesTabs(block) {
		t.Fatalf("block was not gofmt-ed: %q", block)
	}
	if !strings.HasPrefix(block, "package main") {
		t.Fatalf("block content mangled: %q", block)
	}
}

// gofmtUsesTabs reports whether the block's body lines are tab-indented
// (gofmt's canonical style) regardless of editor renderings.
func gofmtUsesTabs(block string) bool {
	for _, line := range strings.Split(block, "\n") {
		if strings.Contains(line, "fmt.Println") {
			return strings.HasPrefix(line, "\t")
		}
	}
	return false
}

func TestGoLintFilter_Idempotent(t *testing.T) {
	if _, err := lookGofmt(); err != nil {
		t.Skipf("gofmt not available: %v", err)
	}
	f := NewGoLintFilter("", "")
	ctx := context.Background()
	step := &task.TaskStep{Result: "go code"}

	first := f.Process(ctx, step, badlyFormattedOutput)
	if first.Outcome != FilterRewrite {
		t.Fatalf("first pass = %v, want rewrite", first.Outcome)
	}
	// Feeding the rewrite back MUST pass: gofmt on gofmt-clean code is a
	// no-op, so the second run never rewrites.
	second := f.Process(ctx, step, first.Output)
	if second.Outcome != FilterPass {
		t.Fatalf("second pass = %v, want pass (reason=%q output=%q)", second.Outcome, second.Reason, second.Output)
	}
}

func TestGoLintFilter_MissingBinaryFails(t *testing.T) {
	f := NewGoLintFilter("/nonexistent/gofmt- surely-missing", "go")
	ctx := context.Background()
	step := &task.TaskStep{Result: "go code"}

	res := f.Process(ctx, step, badlyFormattedOutput)
	if res.Outcome != FilterAdvisory {
		t.Fatalf("Outcome = %v, want advisory (result=%+v)", res.Outcome, res)
	}
	if !strings.HasPrefix(res.Reason, "lint_go: ") {
		t.Fatalf("Reason %q missing lint_go: prefix", res.Reason)
	}
}

func TestGoLintFilter_TimeoutRespected(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script sleep bin test is unix-only")
	}
	dir := t.TempDir()
	// A fake gofmt that ONLY sleeps: with a 1s context deadline the filter
	// must kill the subprocess and fail with "lint_go: ..." well under 4s,
	// proving the context timeout is the bound (not the 10s default).
	sleepBin := filepath.Join(dir, "slowfmt")
	script := "#!/bin/sh\nsleep 30\n"
	if err := os.WriteFile(sleepBin, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake bin: %v", err)
	}

	f := NewGoLintFilter(sleepBin, "go")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	step := &task.TaskStep{Result: "go code"}

	start := time.Now()
	res := f.Process(ctx, step, badlyFormattedOutput)
	elapsed := time.Since(start)

	if res.Outcome != FilterAdvisory {
		t.Fatalf("Outcome = %v, want advisory (result=%+v)", res.Outcome, res)
	}
	if !strings.HasPrefix(res.Reason, "lint_go: ") {
		t.Fatalf("Reason %q missing lint_go: prefix", res.Reason)
	}
	if elapsed >= 4*time.Second {
		t.Fatalf("filter took %v, context deadline not respected", elapsed)
	}
	if elapsed < 500*time.Millisecond {
		t.Fatalf("filter returned after %v; the fake bin never ran", elapsed)
	}
}

// lookGofmt resolves gofmt from PATH (with a GOROOT/bin fallback) so the
// machine-dependent tests can skip cleanly when the Go toolchain is absent.
func lookGofmt() (string, error) {
	if p, err := exec.LookPath("gofmt"); err == nil {
		return p, nil
	}
	if root := goRootEnv(); root != "" {
		p := filepath.Join(root, "bin", "gofmt")
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", fmt.Errorf("gofmt not found on PATH or GOROOT/bin")
}

// goRootEnv returns the GOROOT of the running test binary.
func goRootEnv() string {
	out, err := exec.Command("go", "env", "GOROOT").Output()
	if err != nil {
		return os.Getenv("GOROOT")
	}
	return strings.TrimSpace(string(out))
}
