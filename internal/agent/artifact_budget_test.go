package agent

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// noticeRe pins the documented truncation-notice shape:
// "[... CLAUDE.md truncated: NNNN/TTTT tokens ...]" (original/budgeted).
var noticeRe = regexp.MustCompile(`\[... CLAUDE\.md truncated: \d+/\d+ tokens \.\.\.\]`)

// noticeCountsRe extracts the original and budgeted counts from the notice.
var noticeCountsRe = regexp.MustCompile(`truncated: (\d+)/(\d+) tokens`)

// --- Test fixtures -------------------------------------------------------

// lastN returns the final n bytes of s (ASCII-safe suffix view for test
// failure messages).
func lastN(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

// writeBudgetFixture writes a temp project dir containing CLAUDE.md and/or
// README.md, and returns the dir path.
func writeBudgetFixture(t *testing.T, claudeMD, readme string) string {
	t.Helper()
	dir := t.TempDir()
	if claudeMD != "" {
		if err := os.WriteFile(filepath.Join(dir, "CLAUDE.md"), []byte(claudeMD), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if readme != "" {
		if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte(readme), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

// bigDoc returns deterministic ASCII doc content of roughly wantTokens tokens
// under the 3-chars-per-token heuristic (one char per token is conservative
// even for real tokenizers).
func bigDoc(tokens int) string {
	var b strings.Builder
	line := "x"
	for len(line) < 60 {
		line += " word"
	}
	for b.Len() < tokens*3 {
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// --- T2: budgeted doc builder --------------------------------------------

func TestBuildFullArtifactContextBudgeted(t *testing.T) {
	const budget = 2000

	t.Run("oversize CLAUDE.md is truncated to budget", func(t *testing.T) {
		dir := writeBudgetFixture(t, bigDoc(1_000_000), "")
		am := NewArtifactManager(nil)

		section, truncated := am.BuildFullArtifactContextBudgeted("", dir, budget, nil)
		if !truncated {
			t.Fatal("truncated = false; want true for a 1M-token doc under a 2000-token budget")
		}

		tk := newDocBudgetTokenizer("")
		got := tk.CountTokens(section)
		if got > budget {
			t.Fatalf("section = %d tokens; want <= %d budget", got, budget)
		}
		// Notice must be the exact ASCII shape with original/budgeted
		// counts: "[... CLAUDE.md truncated: NNNN/TTTT tokens ...]".
		if !strings.Contains(section, docBudgetTruncationNotice[:25]) {
			t.Fatalf("truncation notice prefix missing from section: got %q", lastN(section, 120))
		}
		if !noticeRe.MatchString(section) {
			t.Fatalf("notice does not match the documented NNNN/TTTT shape: %q", lastN(section, 120))
		}
	})

	t.Run("small file is byte-identical to unbounded content", func(t *testing.T) {
		small := "# CLAUDE.md (small)\n\nBuild with: go build ./...\n"
		dir := writeBudgetFixture(t, small, "")
		am := NewArtifactManager(nil)

		budgeted, truncated := am.BuildFullArtifactContextBudgeted("", dir, budget, nil)
		if truncated {
			t.Fatal("truncated = true for a small doc; want false")
		}

		unbounded := am.BuildFullArtifactContext("", dir)
		if budgeted != unbounded {
			t.Fatalf("budgeted output differs from unbounded baseline for an under-budget doc:\n got  %q\n want %q",
				budgeted, unbounded)
		}
	})

	t.Run("notice carries original and budgeted token counts", func(t *testing.T) {
		dir := writeBudgetFixture(t, bigDoc(50_000), "")
		am := NewArtifactManager(nil)

		section, truncated := am.BuildFullArtifactContextBudgeted("", dir, 500, nil)
		if !truncated {
			t.Fatal("expected truncation")
		}
		// The notice must match the documented NNNN/TTTT shape and its
		// original count must reflect the ~50000-token fixture (heuristic
		// 3 bytes/token, minus nothing: the notice reports the doc's own
		// original size against the trim budget).
		m := noticeRe.FindString(section)
		if m == "" {
			t.Fatalf("no truncation notice found in section: %q", lastN(section, 120))
		}
		parts := noticeCountsRe.FindStringSubmatch(m)
		if parts == nil {
			t.Fatalf("notice %q does not carry NNNN/TTTT counts", m)
		}
		// parts[1] = original, parts[2] = budget applied to this doc's trim.
		if parts[1] != "50014" { // 150042 bytes / 3, exact heuristic count
			t.Errorf("notice original count = %s; want 50014 (doc's exact heuristic size)", parts[1])
		}
		if parts[2] != "490" { // section budget 500 minus 10 tokens of header
			t.Errorf("notice budget count = %s; want 490 (500 budget minus header)", parts[2])
		}
	})

	t.Run("no artifacts yields empty section", func(t *testing.T) {
		dir := t.TempDir()
		am := NewArtifactManager(nil)
		section, truncated := am.BuildFullArtifactContextBudgeted("", dir, budget, nil)
		if section != "" || truncated {
			t.Fatalf("empty project: section=%q truncated=%v; want \"\", false", section, truncated)
		}
	})
}

// --- T3: tokenizer choice --------------------------------------------------

func TestDocBudgetTokenizer(t *testing.T) {
	doc := bigDoc(5_000)

	t.Run("nil tokenizer falls back to heuristic", func(t *testing.T) {
		tk := newDocBudgetTokenizer("")
		if _, ok := tk.(*llmHeuristicTokenizerAdapter); !ok {
			t.Fatalf("nil tokenizer must produce the heuristic tokenizer; got %T", tk)
		}
		want := (len(doc) + 2) / 3 // 3-chars-per-token heuristic, ceiling
		if got := tk.CountTokens(doc); got != want {
			t.Fatalf("heuristic count = %d; want %d", got, want)
		}
	})

	t.Run("model tokenizer is used when provided", func(t *testing.T) {
		tk := newDocBudgetTokenizer("gpt-4o")
		if _, ok := tk.(*llmHeuristicTokenizerAdapter); ok {
			t.Fatal("a known model must not degrade to the heuristic tokenizer")
		}
		if tk == nil {
			t.Fatal("tokenizer is nil for a known model")
		}
	})

	t.Run("same tokenizer counts budget and content (deterministic)", func(t *testing.T) {
		dir := writeBudgetFixture(t, doc, "")
		am := NewArtifactManager(nil)

		const budget = 300
		section, truncated := am.BuildFullArtifactContextBudgeted("", dir, budget, nil)
		if !truncated {
			t.Fatal("expected truncation")
		}
		tkB := newDocBudgetTokenizer("")
		if got := tkB.CountTokens(section); got > budget {
			t.Fatalf("section = %d tokens; want <= %d", got, budget)
		}
		// Determinism: same input, same output.
		section2, _ := am.BuildFullArtifactContextBudgeted("", dir, budget, nil)
		if section != section2 {
			t.Fatal("budgeted output is not deterministic across identical calls")
		}
	})
}

// --- T5: default budget ------------------------------------------------------

func TestDocBudgetDefault(t *testing.T) {
	doc := bigDoc(20_000)
	dir := writeBudgetFixture(t, doc, "")
	am := NewArtifactManager(nil)

	t.Run("zero uses documented default 2000", func(t *testing.T) {
		section, truncated := am.BuildFullArtifactContextBudgeted("", dir, 0, nil)
		if !truncated {
			t.Fatal("expected truncation at the default budget")
		}
		tk := newDocBudgetTokenizer("")
		if got := tk.CountTokens(section); got > DefaultDocBudgetTokens {
			t.Fatalf("section = %d tokens; want <= default %d", got, DefaultDocBudgetTokens)
		}
	})

	t.Run("negative treated as default too", func(t *testing.T) {
		section, truncated := am.BuildFullArtifactContextBudgeted("", dir, -42, nil)
		if !truncated {
			t.Fatal("expected truncation at the default budget")
		}
		tk := newDocBudgetTokenizer("")
		if got := tk.CountTokens(section); got > DefaultDocBudgetTokens {
			t.Fatalf("section = %d tokens; want <= default %d", got, DefaultDocBudgetTokens)
		}
	})
}

// --- budget honored across the full multi-section output ---------------------

func TestBuildFullArtifactContextBudgetedMultiSection(t *testing.T) {
	readme := bigDoc(10_000)
	dir := writeBudgetFixture(t, bigDoc(8_000), readme)
	am := NewArtifactManager(nil)

	const budget = 400
	section, truncated := am.BuildFullArtifactContextBudgeted("", dir, budget, nil)
	if !truncated {
		t.Fatal("expected truncation for multi-oversize docs")
	}
	tk := newDocBudgetTokenizer("")
	if got := tk.CountTokens(section); got > budget {
		t.Fatalf("combined section = %d tokens; want <= %d", got, budget)
	}
}
