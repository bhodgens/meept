package validator

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// Pins for the loader/detector tokenization unification (bughunt wave
// M6 + L12). The defect these lock down: the word-table loader split on
// WHITESPACE while the detector tokenized on NON-LETTERS, so the natural
// `de, la, les, …` CSV a maintainer would write produced tokens that
// loaded cleanly and then never matched anything. The same split made
// `#` comment stripping per-token, so the words of a comment leaked into
// the table.

// frenchCSV is a natural comma-separated French function-word list: the
// shape a maintainer writes by hand.
const frenchCSV = `# french function words
le, la, les, un, une, des, du, de, au, aux, et, ou, mais, donc, or, ni, car, je, tu,
il, elle, nous, vous, ils, elles, ce, cette, ces, mon, ton, son, ma, ta, sa, mes,
tes, ses, notre, votre, leurs, qui, que, quoi, dont, est, sont, etait, etaient, a, ai,
as, ont, avons, avez, sera, seront, etre, avoir, fait, faire, plus, moins, tres,
dans, sur, avec, pour, par, bien, tout, tous, toute, toutes, autre, autres, aussi`

// TestLoadLanguageWordTables_CommaSeparatedCSVMatchesFrench is pin (1):
// the natural `le, la, les, …` CSV form MUST match French output. Before
// the tokenization unification the loader stored every entry with its
// comma attached ("le," "la," …), so the table cleared the 20-word floor,
// loaded without error, and then matched NOTHING — the language became
// undetectable while looking healthy. The pin asserts the real end-to-end
// consequence: French prose is named "fr" and a French-expecting filter
// passes it.
func TestLoadLanguageWordTables_CommaSeparatedCSVMatchesFrench(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fr.txt"), []byte(frenchCSV+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadLanguageWordTables(dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { _ = LoadLanguageWordTables(filepath.Join(t.TempDir(), "absent")) })

	prose := "le fichier est dans la table avec les donnees du client et il a ete fait " +
		"par notre equipe pour vous avec plus de soins et de tres bons resultats dans la verification."
	code, conf := detectedLanguage(prose)
	if code != "fr" || conf < 0.5 {
		t.Fatalf("detectedLanguage(french prose, comma-separated table) = (%q, %.2f), want (fr, >=0.50)", code, conf)
	}
	if r := NewLanguageFilter("fr").Process(t.Context(), nil, prose); r.Outcome != FilterPass {
		t.Fatalf("french output vs language_fr: %+v, want pass (CSV-form table never matched)", r)
	}
}

// TestParseWordList_CommaSeparatedEntriesAreWholeWords proves the loader
// strips punctuation: "de," and "de" are the same entry, and the stored
// key is the bare word the detector compares against.
func TestParseWordList_CommaSeparatedEntriesAreWholeWords(t *testing.T) {
	set := parseWordList(`de, la, les.`)
	for _, want := range []string{"de", "la", "les"} {
		if _, ok := set[want]; !ok {
			t.Fatalf("tokenized CSV missing %q; table = %v", want, sortedKeys(set))
		}
	}
	for _, bad := range []string{"de,", "les."} {
		if _, ok := set[bad]; ok {
			t.Fatalf("table kept punctuated token %q; entries must be bare words", bad)
		}
	}
}

// TestParseWordList_CommentWordsDoNotLeak is pin (2): `#` comments are
// stripped at the LINE level, so a comment's words never enter the
// table. The repo's own e2e fixture (# french function words) is the
// regression: before the fix "french", "function" and "words" were all
// stored as French cues, which both inflated the table and could win a
// hit-rate against real text.
func TestParseWordList_CommentWordsDoNotLeak(t *testing.T) {
	set := parseWordList("# french function words\nle la les\n# another comment line here\nun une\n")
	for _, leak := range []string{"french", "function", "words", "comment", "line", "here", "another"} {
		if _, ok := set[leak]; ok {
			t.Fatalf("comment word %q leaked into the table; table = %v", leak, sortedKeys(set))
		}
	}
	for _, want := range []string{"le", "la", "les", "un", "une"} {
		if _, ok := set[want]; !ok {
			t.Fatalf("real entry %q missing; table = %v", want, sortedKeys(set))
		}
	}
}

// TestParseWordList_TrailingCommentOnAContentLine is pin (2) again for
// the shape that actually broke: a comment sharing a line with content.
// Per-token stripping dropped only the token that started with "#", so
// every word after it became a table entry.
func TestParseWordList_TrailingCommentOnAContentLine(t *testing.T) {
	set := parseWordList("le la les # the rest of this line is prose\n")
	for _, leak := range []string{"the", "rest", "of", "this", "prose", "line", "is"} {
		if _, ok := set[leak]; ok {
			t.Fatalf("trailing-comment word %q leaked into the table; table = %v", leak, sortedKeys(set))
		}
	}
	if len(set) != 3 {
		t.Fatalf("table has %d entries (%v), want exactly the 3 content words", len(set), sortedKeys(set))
	}
}

// stripLineComments mirrors the loader's documented pre-step: `#` starts a
// comment that runs to the end of its LINE. It is deliberately NOT shared
// with the detector — detection reads prose, where "#" is ordinary text —
// so this helper exists to express the loader's input, not to be the
// implementation under test.
func stripLineComments(text string) string {
	var b strings.Builder
	for line := range strings.Lines(text) {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		b.WriteString(line)
	}
	return b.String()
}

// TestLoaderAndDetectorShareTokenization is pin (5): every word the
// loader stores MUST be a word the detector actually emits. This is the
// M6 contract stated so a future divergence fails here first.
//
// The old defect is exactly this, inverted: the loader stored entries the
// detector could never produce ("le," with its comma, "de;" with its
// semicolon), so the two paths disagreed on what a word IS. A test that
// only compared counts would pass on two different vocabularies; this
// one asserts set membership of the detector's tokens.
func TestLoaderAndDetectorShareTokenization(t *testing.T) {
	inputs := []string{
		`de, la, les, un, une`,
		"le la les # trailing comment words here",
		"don't stop; it's fine — really?",
		`mixed CASE, UPPER and lower`,
		"'quoted' 'words' — dashes…",
		"l'un de ces deux",
		"#only a comment",
		"   ",
		"a, b, c, d, e, f, g, h, i, j, k, l",
		"one-per-line\nle\nla\nles",
		"tab\tseparated\twords",
	}
	for _, in := range inputs {
		t.Run(in, func(t *testing.T) {
			// The words the detector can produce from the text the loader
			// is allowed to see (comments removed, same shared tokenizer).
			emitted := map[string]struct{}{}
			for field := range tokenizeWords(stripLineComments(in)) {
				emitted[normalizeWord(field)] = struct{}{}
			}
			for _, key := range sortedKeys(parseWordList(in)) {
				if _, ok := emitted[key]; !ok {
					t.Fatalf("loader stored %q, which the detector can never emit — the loader and detector disagree on tokenization (detector tokens: %v)",
						key, sortedKeys(emitted))
				}
			}
		})
	}
}

// TestLoaderAndDetectorAgreeOnCounts closes the loophole above: for input
// with no comment (nothing for the loader to strip), the loader's set must
// be EXACTLY the detector's token set — same members, same cardinality.
func TestLoaderAndDetectorAgreeOnCounts(t *testing.T) {
	for _, in := range []string{
		`de, la, les, un, une`,
		"don't stop; it's fine — really?",
		"'quoted' 'words' — dashes…",
		"l'un de ces deux",
	} {
		t.Run(in, func(t *testing.T) {
			loaded := parseWordList(in)
			want := map[string]struct{}{}
			for field := range tokenizeWords(in) {
				want[normalizeWord(field)] = struct{}{}
			}
			if len(loaded) != len(want) {
				t.Fatalf("loader stored %d words %v, detector emitted %d %v", len(loaded), sortedKeys(loaded), len(want), sortedKeys(want))
			}
			for w := range want {
				if _, ok := loaded[w]; !ok {
					t.Fatalf("detector token %q missing from the loaded table %v", w, sortedKeys(loaded))
				}
			}
		})
	}
}

// TestTokenizeWords_DetectorShape pins the tokenizer contract the loader
// now shares: non-letters separate, an inner apostrophe does not, and an
// edge apostrophe is trimmed by normalization.
func TestTokenizeWords_DetectorShape(t *testing.T) {
	fields := slices.Collect(tokenizeWords("don't — 'quoted' l'un"))
	want := []string{"don't", "'quoted'", "l'un"}
	if !slices.Equal(fields, want) {
		t.Fatalf("tokenizeWords = %q, want %q", fields, want)
	}
	if got := normalizeWord("'quoted'"); got != "quoted" {
		t.Fatalf("normalizeWord(%q) = %q, want quoted", "'quoted'", got)
	}
	if got := normalizeWord("DE"); got != "de" {
		t.Fatalf("normalizeWord(DE) = %q, want de", got)
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}
