package validator

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// lintTimeout bounds both gofmt and go vet subprocesses (master Contract 5).
const lintTimeout = 10 * time.Second

// lintWaitDelay force-unblocks Run() after the cancel signal: a killed
// /bin/sh wrapper's child can keep piped stdio open past the kill, which
// would otherwise block cmd.Wait() until that child exits.
const lintWaitDelay = 500 * time.Millisecond

// goFenceBegin/goFenceEnd delimit fenced Go code blocks in step output.
const (
	goFenceBegin = "```go"
	goFenceEnd   = "```"
)

// codeBlock is an extracted Go snippet with its byte offsets in the
// original output, so a gofmt rewrite can be substituted back.
type codeBlock struct {
	start int // byte offset of the first code byte
	end   int // byte offset one past the last code byte
	code  string
}

// extractGoBlocks finds the Go code in output: fenced ```go blocks, or -
// when none are fenced - the whole output if it looks like a bare Go
// source file (package+func co-occurrence heuristic from the leaf).
func extractGoBlocks(output string) []codeBlock {
	var blocks []codeBlock
	rest := output
	offset := 0
	for {
		begin := strings.Index(rest, goFenceBegin)
		if begin < 0 {
			break
		}
		abs := offset + begin
		codeStart := abs + len(goFenceBegin)
		// Skip the newline (or first byte) after the fence marker.
		if codeStart < len(output) && output[codeStart] == '\n' {
			codeStart++
		} else if codeStart < len(output) && output[codeStart] == '\r' && codeStart+1 < len(output) && output[codeStart+1] == '\n' {
			codeStart += 2
		}
		tail := output[codeStart:]
		endRel := strings.Index(tail, goFenceEnd)
		if endRel < 0 {
			break // unterminated fence: take nothing further
		}
		codeEnd := codeStart + endRel
		// The block is the exact bytes between the fences, INCLUDING any
		// trailing newline: gofmt requires the final newline, so trimming
		// it here would make clean blocks flag forever (never idempotent).
		code := output[codeStart:codeEnd]
		blocks = append(blocks, codeBlock{start: codeStart, end: codeEnd, code: code})
		offset = codeEnd + len(goFenceEnd)
		rest = output[offset:]
	}
	if len(blocks) == 0 && looksLikeBareGo(output) {
		blocks = append(blocks, codeBlock{start: 0, end: len(output), code: output})
	}
	return blocks
}

// looksLikeBareGo reports whether output is plausibly a bare Go source
// file: a package clause plus at least one func declaration.
func looksLikeBareGo(output string) bool {
	trimmed := strings.TrimSpace(output)
	if !strings.HasPrefix(trimmed, "package ") && !strings.HasPrefix(trimmed, "//") {
		return false
	}
	return strings.Contains(trimmed, "\nfunc ") ||
		strings.HasPrefix(trimmed[min(len(trimmed), 8):], "func ") ||
		strings.Contains(trimmed, "package ") && strings.Contains(trimmed, "func ")
}

// containsGoCode reports whether output carries Go content (the Applies
// heuristic: ```go fence or package+func co-occurrence).
func containsGoCode(output string) bool {
	if strings.Contains(output, goFenceBegin) {
		return true
	}
	return strings.Contains(output, "package ") && strings.Contains(output, "func ")
}

// GoLintFilter runs gofmt over Go code blocks in step output, rewriting
// the output with the reformatted blocks. go vet runs advisory-only: its
// findings are logged, never used to fail the chain.
type GoLintFilter struct {
	gofmtBin string
	goBin    string
}

// NewGoLintFilter creates a lint_go output filter. Empty binary names
// fall back to PATH lookup.
func NewGoLintFilter(gofmtBin, goBin string) *GoLintFilter {
	if gofmtBin == "" {
		gofmtBin = "gofmt"
	}
	if goBin == "" {
		goBin = "go"
	}
	return &GoLintFilter{gofmtBin: gofmtBin, goBin: goBin}
}

// Name implements OutputFilter.
func (f *GoLintFilter) Name() string { return "lint_go" }

// Applies implements OutputFilter: the filter is declared by chain
// membership; the output-content gate re-checks inside Process, because
// Applies only sees the step. step.Result is used as the declaration-time
// proxy so Go-free steps skip the stage entirely.
func (f *GoLintFilter) Applies(step *task.TaskStep) bool {
	if step == nil {
		return false
	}
	if containsGoCode(step.Result) {
		return true
	}
	// The Process output may diverge from step.Result (earlier chain
	// rewrites); stay applicable and let Process gate on the actual bytes.
	return step.Result == ""
}

// Process implements OutputFilter. No Go content -> pass. gofmt -l on the
// extracted blocks decides the verdict: clean -> pass; files needing
// format -> gofmt -w and rewrite with blocks substituted back; tool
// failure/timeout -> fail with "lint_go: <err>". Idempotent: gofmt-clean
// code produces a no-op second run.
func (f *GoLintFilter) Process(ctx context.Context, _ *task.TaskStep, output string) FilterResult {
	const self = "lint_go"
	if !containsGoCode(output) {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}
	blocks := extractGoBlocks(output)
	if len(blocks) == 0 {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}

	dir, err := os.MkdirTemp("", "lintgo-")
	if err != nil {
		return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintGoReason(err)}
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slog.Warn("output filter temp cleanup failed",
				"stage", "output_filter", "filter", self, "err", rmErr)
		}
	}()

	// A minimal go.mod so the advisory `go vet ./...` has package context
	// (gofmt itself is module-independent).
	if modErr := os.WriteFile(filepath.Join(dir, "go.mod"),
		[]byte("module lintgo/temp\n\ngo 1.21\n"), 0o600); modErr != nil {
		return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintGoReason(modErr)}
	}

	runCtx, cancel := context.WithTimeout(ctx, lintTimeout)
	defer cancel()

	names := make([]string, len(blocks))
	for i, block := range blocks {
		name := fmt.Sprintf("snippet_%d.go", i)
		if writeErr := os.WriteFile(filepath.Join(dir, name), []byte(block.code), 0o600); writeErr != nil {
			return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintGoReason(writeErr)}
		}
		names[i] = name
	}

	listed, err := f.runGofmtList(runCtx, dir)
	if err != nil {
		return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintGoReason(err)}
	}
	if len(listed) == 0 {
		f.runGoVetAdvisory(runCtx, dir)
		return FilterResult{Outcome: FilterPass, Filter: self}
	}

	formatted := make(map[string]string, len(listed))
	for _, name := range listed {
		if err := f.runGofmtWrite(runCtx, filepath.Join(dir, name)); err != nil {
			return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintGoReason(err)}
		}
		content, readErr := os.ReadFile(filepath.Join(dir, name))
		if readErr != nil {
			return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintGoReason(readErr)}
		}
		formatted[name] = string(content)
	}

	rewritten, subErr := substituteBlocks(output, blocks, names, formatted)
	if subErr != nil {
		return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintGoReason(subErr)}
	}
	f.runGoVetAdvisory(runCtx, dir)
	return FilterResult{Outcome: FilterRewrite, Filter: self, Output: rewritten}
}

// substituteBlocks replaces each original block with its gofmt-ed content,
// walking blocks in reverse so earlier offsets stay valid. names[i] is the
// temp-file name for blocks[i]; formatted holds the reformatted sources.
func substituteBlocks(output string, blocks []codeBlock, names []string, formatted map[string]string) (string, error) {
	out := output
	for i := len(blocks) - 1; i >= 0; i-- {
		newCode, ok := formatted[names[i]]
		if !ok {
			continue // file was already gofmt-clean
		}
		block := blocks[i]
		if block.start < 0 || block.end > len(out) || block.start > block.end {
			return "", fmt.Errorf("code block %d offsets out of range", i)
		}
		out = out[:block.start] + newCode + out[block.end:]
	}
	return out, nil
}

// newLintCmd builds a context-bound exec.Cmd with a force-bounded wait:
// the default CommandContext kill does not unblock Run() while piped
// stdio is still open (a /bin/sh wrapper's child can hold the pipe for the
// whole sleep), so a WaitDelay forces the wait to return right after the
// cancel signal.
func newLintCmd(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.WaitDelay = lintWaitDelay
	return cmd
}

// runGofmtList runs `gofmt -l <dir>` and returns the file names (relative
// to dir) that need formatting.
func (f *GoLintFilter) runGofmtList(ctx context.Context, dir string) ([]string, error) {
	cmd := newLintCmd(ctx, f.gofmtBin, "-l", dir)
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return nil, fmt.Errorf("gofmt -l timed out after %s: %w", lintTimeout, ctx.Err())
		}
		return nil, fmt.Errorf("gofmt -l: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	if ctx.Err() != nil {
		return nil, fmt.Errorf("gofmt -l timed out after %s: %w", lintTimeout, ctx.Err())
	}
	var listed []string
	for _, line := range strings.Split(stdout.String(), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			listed = append(listed, filepath.Base(line))
		}
	}
	return listed, nil
}

// runGofmtWrite runs `gofmt -w <file>` in place.
func (f *GoLintFilter) runGofmtWrite(ctx context.Context, path string) error {
	cmd := newLintCmd(ctx, f.gofmtBin, "-w", path)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("gofmt -w timed out after %s: %w", lintTimeout, ctx.Err())
		}
		return fmt.Errorf("gofmt -w: %v: %s", err, strings.TrimSpace(stderr.String()))
	}
	return nil
}

// runGoVetAdvisory runs `go vet` over the extracted snippets. Per master
// Contract 5, vet is ADVISORY: findings and toolchain failures are logged
// and never fail the chain (the tri-state FilterResult has no warnings
// channel, so structured logging is the observability surface).
func (f *GoLintFilter) runGoVetAdvisory(ctx context.Context, dir string) {
	cmd := newLintCmd(ctx, f.goBin, "vet", "./...")
	cmd.Dir = dir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	switch {
	case err != nil && ctx.Err() != nil:
		slog.Warn("output filter advisory",
			"stage", "output_filter", "filter", "lint_go",
			"msg", "go vet timed out (advisory)", "err", ctx.Err())
	case err != nil:
		slog.Warn("output filter advisory",
			"stage", "output_filter", "filter", "lint_go",
			"msg", "go vet failed (advisory)", "err", err,
			"stderr", strings.TrimSpace(stderr.String()))
	default:
		slog.Info("output filter advisory",
			"stage", "output_filter", "filter", "lint_go",
			"msg", "go vet clean")
	}
}

// lintGoReason renders a lint_go failure reason per the leaf contract:
// "lint_go: <err>".
func lintGoReason(err error) string {
	return fmt.Sprintf("lint_go: %v", err)
}
