package validator

import (
	"bytes"
	"context"
	"log/slog"
	"os/exec"
	"strings"

	"github.com/caimlas/meept/internal/task"
)

// lintResult captures a subprocess outcome for the script linters.
type lintResult struct {
	err     error
	stdout  string
	stderr  string
	timeout bool
}

// runLintCmd runs a context-bound lint subprocess and captures stdout /
// stderr / timeout classification in one place. The ctx argument lets a
// canceled deadline be distinguished from a real tool failure.
func runLintCmd(ctx context.Context, cmd *exec.Cmd) lintResult {
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	res := lintResult{err: err, stdout: stdout.String(), stderr: stderr.String()}
	if err != nil && ctx != nil && ctx.Err() != nil {
		res.timeout = true
	}
	return res
}

// extractFencedBlocks finds fenced code blocks whose fence matches either
// given marker, returning byte offsets so a rewrite can substitute back.
// Kept parallel to extractGoBlocks.
func extractFencedBlocks(output, fenceBegin, fenceAlt string) []fencedBlock {
	return extractFencedMulti(output, []string{fenceBegin, fenceAlt})
}

// fencedBlock is a code block plus the fence marker it was found under
// (the marker decides the checker: js vs ts).
type fencedBlock struct {
	codeBlock
	fence string
}

// extractFencedMulti finds fenced code blocks under any of the given
// fence markers, scanning left to right with correct offsets. Unterminated
// fences are skipped (same rule as extractGoBlocks).
func extractFencedMulti(output string, fenceBegins []string) []fencedBlock {
	var blocks []fencedBlock
	offset := 0
	for {
		bestIdx, bestFence := -1, ""
		for _, fb := range fenceBegins {
			idx := strings.Index(output[offset:], fb)
			if idx >= 0 && (bestIdx < 0 || offset+idx < bestIdx) {
				bestIdx = offset + idx
				bestFence = fb
			}
		}
		if bestIdx < 0 {
			break
		}
		codeStart := bestIdx + len(bestFence)
		if codeStart < len(output) && output[codeStart] == '\n' {
			codeStart++
		} else if codeStart < len(output) && output[codeStart] == '\r' && codeStart+1 < len(output) && output[codeStart+1] == '\n' {
			codeStart += 2
		}
		tail := output[codeStart:]
		endRel := strings.Index(tail, goFenceEnd)
		if endRel < 0 {
			break // unterminated fence
		}
		codeEnd := codeStart + endRel
		// Keep the author's exact bytes inside the fence: checkers disagree
		// on EOF-newline requirements and rewriting here would desync the
		// offsets we report in failure reasons.
		blocks = append(blocks, fencedBlock{
			codeBlock: codeBlock{start: codeStart, end: codeEnd, code: output[codeStart:codeEnd]},
			fence:     bestFence,
		})
		offset = codeEnd + len(goFenceEnd)
	}
	return blocks
}

// slogWarnTempCleanup logs a temp-dir cleanup failure with the standard
// stage keys (shared shape with filter_lint.go's inline logging).
func slogWarnTempCleanup(filter string, err error) {
	slog.Warn("output filter temp cleanup failed",
		"stage", "output_filter", "filter", filter, "err", err)
}

// slogWarnToolSkip logs a tool-skip decision (host lacks the checker) with
// the standard stage keys.
func slogWarnToolSkip(filter, tool, msg string) {
	slog.Warn("output filter tool skipped",
		"stage", "output_filter", "filter", filter, "tool", tool, "msg", msg)
}

// compile-time interface checks for the script filters.
var (
	_ OutputFilter = (*PythonLintFilter)(nil)
	_ OutputFilter = (*JSLintFilter)(nil)
	_              = task.TaskStep{}
)
