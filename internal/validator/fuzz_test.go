package validator

import (
	"strings"
	"testing"
)

// checkBlocks validates the slicing invariant shared by every fenced-block
// extractor: start <= end <= len(output) and the reported code is exactly
// output[start:end].
func checkBlocks(t *testing.T, output string, starts, ends []int, codes []string) {
	t.Helper()
	for i := range ends {
		if i >= len(starts) || i >= len(codes) {
			t.Fatalf("block %d: ragged result (starts=%d ends=%d codes=%d)", i, len(starts), len(ends), len(codes))
		}
		if starts[i] < 0 || ends[i] > len(output) || starts[i] > ends[i] {
			t.Fatalf("block %d: offsets out of range start=%d end=%d len=%d", i, starts[i], ends[i], len(output))
		}
		if output[starts[i]:ends[i]] != codes[i] {
			t.Fatalf("block %d: code %q != output[start:end] %q", i, codes[i], output[starts[i]:ends[i]])
		}
	}
}

// FuzzExtractFencedBlocks exercises the fenced-block extraction used by the
// JS/TS/Python lint filters with arbitrary markdown. This is exactly where
// the lint_js prose bug lived. Invariants:
//   - never panics;
//   - every returned block's offsets slice cleanly: 0 <= start <= end <= len;
//   - the reported code is byte-identical to output[start:end];
//   - unterminated fences never produce a block.
func FuzzExtractFencedBlocks(f *testing.F) {
	seeds := []string{
		"",
		"plain prose, no fences",
		"```js\nconsole.log('hi');\n```",
		"```ts\nconst x: number = 1;\n```",
		// The FULL production marker names (L15b) — jsTSLintFences in
		// filter_lint_script.go carries ```javascript/```typescript
		// alongside the ```js/```ts shorthands, and the corpus previously
		// only fuzzed the shorthands.
		"```javascript\nconsole.log('hi');\n```",
		"```typescript\nconst x: number = 1;\n```",
		"```javascript\nconsole.log('a');\n```\nprose between\n```typescript\nconst b: number = 2;\n```",
		"```python\nprint('hi')\n```",
		"```py\nx = 1\n```",
		"```js\nconsole.log('a');\n```\nprose between\n```js\nconsole.log('b');\n```",
		"```js\nunterminated fence",
		"```js\r\ncrlf block\r\n```",
		"``````",
		"```js```inline```ts```",
		"unicode \u4e2d\u6587 ```js\n\u00e9\u00e9\n``` \U0001F600",
		"```js\n" + strings.Repeat("x;\n", 5000) + "```",
		"```python\n```\n```py\n```\n```js\n``` nested fences",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	fences := []string{"```js", "```ts", "```py", "```python"}
	f.Fuzz(func(t *testing.T, output string) {
		for _, fence := range fences {
			blocks := extractFencedBlocks(output, fence, fence)
			for _, b := range blocks {
				checkBlocks(t, output, []int{b.start}, []int{b.end}, []string{b.code})
			}
		}
		// Multi-fence path with the production marker sets: the JS/TS set
		// is jsTSLintFences verbatim (filter_lint_script.go) — including
		// the full ```javascript/```typescript names — plus the Python
		// pair. Multi-fence pass exercises extractFencedMulti directly the
		// way JSLintFilter invokes it.
		jsTsBlocks := extractFencedMulti(output, []string{"```javascript", "```js", "```typescript", "```ts"})
		for _, b := range jsTsBlocks {
			checkBlocks(t, output, []int{b.start}, []int{b.end}, []string{b.code})
		}
		pyBlocks := extractFencedMulti(output, []string{"```python", "```py"})
		for _, b := range pyBlocks {
			checkBlocks(t, output, []int{b.start}, []int{b.end}, []string{b.code})
		}
	})
}

// FuzzExtractGoBlocks exercises the Go fence extractor used by filter_lint's
// gofmt rewrite path. Invariants: never panics; start <= end <= len; code
// equals output[start:end]; a bare-Go whole-file block spans the exact input.
func FuzzExtractGoBlocks(f *testing.F) {
	seeds := []string{
		"",
		"no code here",
		"```go\npackage main\n\nfunc main() {}\n```",
		"```go\nfunc f() {}\n```",
		"```go\nunterminated",
		"```go\r\npackage main\r\n```",
		"package main\n\nfunc main() {}",
		"prose\n```go\nvar x = 1\n```\ntrailer",
		"```go\n```go\nnested\n```",
		"```go\nunicode \u4e2d\u6587 var \u00e9 = 1\n```",
		"```go\n" + strings.Repeat("x := 1\n", 5000) + "```",
		"func only, no package keyword",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, output string) {
		blocks := extractGoBlocks(output)
		for _, b := range blocks {
			checkBlocks(t, output, []int{b.start}, []int{b.end}, []string{b.code})
		}
	})
}
