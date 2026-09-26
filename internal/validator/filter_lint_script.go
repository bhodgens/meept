package validator

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/caimlas/meept/internal/task"
)

// lintPythonTimeout bounds the py_compile subprocess (master Contract 5
// pattern, same envelope as lint_go).
const lintPythonTimeout = lintTimeout

// Python fence markers (`python` and the common `py` abbreviation).
const (
	pyFenceBegin = "```python"
	pyFenceAlt   = "```py"
)

// PythonLintFilter typechecks Python snippets via `python -m py_compile`
// (stdlib, no third-party tooling required on the daemon host). py_compile
// is a pure SYNTAX check: no imports run, no code executes. Verdicts:
// clean -> pass; syntax error -> fail (Python has no deterministic
// autofix, so lint_python never rewrites); tool failure -> fail with
// "lint_python: <err>". Idempotent: a pure check.
type PythonLintFilter struct {
	pythonBin string
}

// NewPythonLintFilter creates a lint_python output filter. Empty binary
// name falls back to PATH lookup ("python3").
func NewPythonLintFilter(pythonBin string) *PythonLintFilter {
	if pythonBin == "" {
		pythonBin = "python3"
	}
	return &PythonLintFilter{pythonBin: pythonBin}
}

// Name implements OutputFilter.
func (f *PythonLintFilter) Name() string { return "lint_python" }

// Applies implements OutputFilter: declared by chain membership; the
// content gate re-checks inside Process (Applies only sees the step).
func (f *PythonLintFilter) Applies(step *task.TaskStep) bool {
	return step != nil
}

// Process implements OutputFilter. No Python content -> pass. Blocks are
// extracted, written to a temp dir, and py_compile'd; any syntax error
// fails with the compiler's message.
func (f *PythonLintFilter) Process(ctx context.Context, _ *task.TaskStep, output string) FilterResult {
	const self = "lint_python"
	if !containsPythonCode(output) {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}
	blocks := extractFencedBlocks(output, pyFenceBegin, pyFenceAlt)
	if len(blocks) == 0 {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}

	dir, err := os.MkdirTemp("", "lintpy-")
	if err != nil {
		return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintPythonReason(err)}
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slogWarnTempCleanup(self, rmErr)
		}
	}()

	runCtx, cancel := context.WithTimeout(ctx, lintPythonTimeout)
	defer cancel()

	for i, block := range blocks {
		name := fmt.Sprintf("snippet_%d.py", i)
		path := filepath.Join(dir, name)
		if writeErr := os.WriteFile(path, []byte(block.code), 0o600); writeErr != nil {
			return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintPythonReason(writeErr)}
		}
		cmd := newLintCmd(runCtx, f.pythonBin, "-m", "py_compile", path)
		res := runLintCmd(runCtx, cmd)
		if res.err != nil {
			if res.timeout {
				return FilterResult{Outcome: FilterFail, Filter: self,
					Reason: fmt.Sprintf("lint_python: timeout after %s", lintPythonTimeout)}
			}
			detail := strings.TrimSpace(res.stderr)
			if detail == "" {
				detail = strings.TrimSpace(res.stdout)
			}
			return FilterResult{Outcome: FilterFail, Filter: self,
				Reason: fmt.Sprintf("lint_python: %s: %s", name, firstMeaningfulLine(detail))}
		}
	}
	return FilterResult{Outcome: FilterPass, Filter: self}
}

// containsPythonCode reports whether output carries Python content (the
// Applies-adjacent content heuristic: ```python / ```py fence, or a bare
// def/class + colon-block co-occurrence).
func containsPythonCode(output string) bool {
	if strings.Contains(output, pyFenceBegin) || strings.Contains(output, pyFenceAlt) {
		return true
	}
	t := strings.TrimSpace(output)
	hasDef := strings.Contains(t, "\ndef ") || strings.HasPrefix(t, "def ") ||
		strings.Contains(t, "\nclass ") || strings.HasPrefix(t, "class ")
	hasColonBlock := strings.Contains(t, ":\n") || strings.Contains(t, ":\r\n")
	return hasDef && hasColonBlock
}

// lintPythonReason formats a tool failure as "lint_python: <err>".
func lintPythonReason(err error) string {
	return fmt.Sprintf("lint_python: %v", err)
}

// firstMeaningfulLine returns the first non-empty line of checker output
// (py_compile and friends report the syntax error there), bounded.
func firstMeaningfulLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			if len(line) > 200 {
				line = line[:200]
			}
			return line
		}
	}
	return "unknown checker failure"
}

// compile-time interface check.
var _ OutputFilter = (*PythonLintFilter)(nil)

// jsTSLintFences lists the fence markers carrying JavaScript or
// TypeScript code.
var jsTSLintFences = []string{"```javascript", "```js", "```typescript", "```ts"}

// JSLintFilter typechecks JavaScript/TypeScript blocks. JS is checked with
// `node --check` (pure syntax parse, no execution); TS with `tsc
// --noEmit` when a tsc binary is resolvable. A missing tsc is a logged
// skip-shaped pass, NOT a failure: node/tsc are far less universal on
// daemon hosts than gofmt, and lint_go's fail-on-missing-binary rule is
// deliberately not mirrored here. Syntax errors fail with the checker's
// message; neither checker has a deterministic autofix, so this filter
// never rewrites.
type JSLintFilter struct {
	nodeBin string
	tscBin  string
}

// NewJSLintFilter creates a lint_js output filter. Empty binary names
// fall back to PATH lookup.
func NewJSLintFilter(nodeBin, tscBin string) *JSLintFilter {
	if nodeBin == "" {
		nodeBin = "node"
	}
	if tscBin == "" {
		tscBin = "tsc"
	}
	return &JSLintFilter{nodeBin: nodeBin, tscBin: tscBin}
}

// Name implements OutputFilter.
func (f *JSLintFilter) Name() string { return "lint_js" }

// Applies implements OutputFilter: declared by chain membership.
func (f *JSLintFilter) Applies(step *task.TaskStep) bool {
	return step != nil
}

// Process implements OutputFilter. No JS/TS content -> pass. Fenced blocks
// route by fence language: .js through node --check, .ts through tsc
// --noEmit (missing tsc passes with a logged warning). Bare non-fenced JS
// is deliberately not detected heuristically: JS syntax is a subset of too
// many other languages' for a co-occurrence heuristic to be safe.
func (f *JSLintFilter) Process(ctx context.Context, _ *task.TaskStep, output string) FilterResult {
	const self = "lint_js"
	if !containsJSTSCode(output) {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}
	blocks := extractFencedMulti(output, jsTSLintFences)
	if len(blocks) == 0 {
		return FilterResult{Outcome: FilterPass, Filter: self}
	}

	dir, err := os.MkdirTemp("", "lintjs-")
	if err != nil {
		return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintJSReason(err)}
	}
	defer func() {
		if rmErr := os.RemoveAll(dir); rmErr != nil {
			slogWarnTempCleanup(self, rmErr)
		}
	}()

	runCtx, cancel := context.WithTimeout(ctx, lintTimeout)
	defer cancel()

	tscBin := f.lookupTSC()

	var tsFiles []string
	for i, block := range blocks {
		isTS := block.fence == "```typescript" || block.fence == "```ts"
		ext := ".js"
		checker := "node"
		if isTS {
			ext = ".ts"
			checker = "tsc"
		}
		name := fmt.Sprintf("snippet_%d%s", i, ext)
		path := filepath.Join(dir, name)
		if writeErr := os.WriteFile(path, []byte(block.code), 0o600); writeErr != nil {
			return FilterResult{Outcome: FilterFail, Filter: self, Reason: lintJSReason(writeErr)}
		}
		if isTS {
			if tscBin == "" {
				slogWarnToolSkip(self, "tsc", "not found; TS block passed unchecked")
				continue
			}
			tsFiles = append(tsFiles, name)
			continue
		}
		// Prose-in-a-js-fence guard: the model sometimes labels narration
		// (a sentence about the file it just wrote) with a ```js fence.
		// node --check then reports a syntax error at line 1 and the whole
		// turn dies on a prose answer (run 33: T1/T3 replies replaced by
		// "the turn failed: lint_js: ... snippet_0.js:1"). A block whose
		// first line reads as natural-language narration is NOT code this
		// filter can judge — skip it, matching the filter's existing
		// stance that bare (unfenced) prose-adjacent JS is not detected.
		if looksLikeProse(block.code) {
			slogWarnToolSkip(self, "node", "js-fenced block is prose; skipped")
			continue
		}
		cmd := newLintCmd(runCtx, f.nodeBin, "--check", path)
		if res := runLintCmd(runCtx, cmd); res.err != nil {
			return FilterResult{Outcome: FilterFail, Filter: self,
				Reason: jsFailReason(checker, name, res, "node --check", lintTimeout)}
		}
	}

	if len(tsFiles) > 0 {
		// TS5108: tsc 6+/7 removed moduleResolution=node10; omit the option
		// and let the checker default apply. tsc 5.x's default (node10 for
		// module commonjs) is what these snippet checks want anyway.
		args := append([]string{"--noEmit", "--skipLibCheck", "--target", "es2020",
			"--module", "commonjs"}, tsFiles...)
		cmd := newLintCmd(runCtx, tscBin, args...)
		cmd.Dir = dir
		if res := runLintCmd(runCtx, cmd); res.err != nil {
			return FilterResult{Outcome: FilterFail, Filter: self,
				Reason: jsFailReason("tsc", strings.Join(tsFiles, ","), res, "tsc --noEmit", lintTimeout)}
		}
	}
	return FilterResult{Outcome: FilterPass, Filter: self}
}

// jsFailReason formats a checker failure: "lint_js: <checker>: <file>:
// <first stderr line>". Checker stderr carries the line/column.
func jsFailReason(checker, name string, res lintResult, label string, timeout time.Duration) string {
	if res.timeout {
		return fmt.Sprintf("lint_js: %s timeout after %s", label, timeout)
	}
	detail := strings.TrimSpace(res.stderr)
	if detail == "" {
		detail = strings.TrimSpace(res.stdout)
	}
	if detail == "" {
		detail = "unknown failure"
	}
	return fmt.Sprintf("lint_js: %s: %s: %s", checker, name, firstMeaningfulLine(detail))
}

func lintJSReason(err error) string {
	return fmt.Sprintf("lint_js: %v", err)
}

// containsJSTSCode reports whether output carries JS/TS content.

// looksLikeProse reports whether a fenced block's first non-empty line reads
// as natural-language narration rather than JavaScript. Conservative by
// design: it returns true ONLY when the line starts with a lowercase
// English word followed by other lowercase words (a sentence opening) and
// contains no JS statement tokens. Real code lines start with keywords
// (const/let/function/import), identifiers, punctuation, or indentation —
// none of which match. A false negative (prose that slips through) costs
// one node --check failure verdict, the pre-existing behavior; a false
// positive (real code skipped) costs an unchecked block, which the filter
// already treats as acceptable for missing toolchains.
func looksLikeProse(code string) bool {
	for _, line := range strings.Split(code, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		words := strings.Fields(line)
		if len(words) < 4 {
			return false
		}
		// Code identifiers carry INTERIOR uppercase (camelCase/PascalCase)
		// or ALL CAPS; narration is sentence-case ("The", "the"). A word
		// with both cases beyond a single leading capital is code:
		// "myVar" -> code, "The" -> narration, "THE" -> code.
		raw := words[0]
		body := raw
		if len(body) > 1 {
			body = body[1:]
		}
		low := strings.ToLower(body)
		up := strings.ToUpper(body)
		mixed := low != body && up != body // has both cases -> camelCase
		if mixed {
			return false
		}
		first := strings.ToLower(raw)
		// Sentence openings: "the file ...", "The file ...", "this snippet
		// ...". Case-insensitive on purpose: models capitalize narration
		// (run 34 T1: "The file is at ..." slipped past the lowercase
		// check and node --check killed the turn). A real code line
		// starting with an identifier is caught by the token checks below.
		jsStatementStarts := []string{
			"const", "let", "var", "function", "class", "import", "export",
			"if", "for", "while", "switch", "try", "return", "await", "async",
			"require", "console", "throw", "new", "delete", "typeof",
		}
		for _, kw := range jsStatementStarts {
			if first == kw {
				return false
			}
		}
		// Statement-shaped characters: braces, semicolons, quotes, arrows,
		// assignments, or a comment marker — treat as code.
		for _, ch := range []string{"{", "}", ";", "=", "(", ")", "//", "/*", "=>", "\"", "`", "'"} {
			if strings.Contains(line, ch) {
				return false
			}
		}
		// All-lowercase multi-word line with no code tokens: narration.
		return true
	}
	return false
}
func containsJSTSCode(output string) bool {
	for _, fence := range jsTSLintFences {
		if strings.Contains(output, fence) {
			return true
		}
	}
	return false
}

// lookupTSC resolves the tsc binary: PATH first, then the well-known nvm
// version directories (daemon hosts often install tsc through a version
// manager that the service PATH does not carry). Empty result = absent.
func (f *JSLintFilter) lookupTSC() string {
	if p, err := exec.LookPath(f.tscBin); err == nil {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	versionsDir := filepath.Join(home, ".nvm", "versions", "node")
	entries, readErr := os.ReadDir(versionsDir)
	if readErr != nil {
		return ""
	}
	// Newest version wins (entries are version-named; lexical max is a
	// good approximation and only affects the checker binary version).
	best := ""
	for _, e := range entries {
		cand := filepath.Join(versionsDir, e.Name(), "bin", "tsc")
		if st, statErr := os.Stat(cand); statErr == nil && !st.IsDir() {
			if e.Name() > best {
				best = cand
			}
		}
	}
	return best
}

// compile-time interface check.
var _ OutputFilter = (*JSLintFilter)(nil)
