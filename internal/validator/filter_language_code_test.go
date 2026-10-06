package validator

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/caimlas/meept/internal/task"
)

// Pins for the expected_language contract (bughunt wave M5).
//
// The defect: langOrDefault substituted "en" ONLY for the empty string,
// so every other value passed through verbatim into the comparison
// against a detector that only ever returns a bare lowercase primary
// code. `expected_language: "EN"`, `"english"`, or `"en-US"` therefore
// matched nothing and rejected EVERY prose output — including output in
// the language the operator asked for — burning filter retries to
// rejected_exhausted. The only signal was a lang= reason string.
//
// The decision pinned here, and the reasoning:
//
//   - "EN" is ACCEPTED as English. Case is not meaning.
//   - "en-US" is ACCEPTED as English. Detection returns a bare primary
//     subtag, so a region qualifier can never match literally; the
//     operator's intent ("English") is unambiguous, and rejecting it
//     would reject every correct output. Accept-as-base is the only
//     reading that cannot harm.
//   - "xx" is NOT accepted. A two-letter shape that names no language
//     detection can produce is a typo, and there is no safe base to
//     guess. It fails at config load (OutputFiltersConfig.Validate),
//     where the operator sees it, and here it is reported unknown
//     rather than silently aliased to anything.
//   - A malformed code never becomes "matches nothing": the filter
//     normalizes, WARNS, and falls back to English, so no spelling can
//     turn the filter into a reject-everything device.

// TestNormalizeLanguageCode pins the normalizer directly: the accepted
// spellings and the rejected ones.
func TestNormalizeLanguageCode(t *testing.T) {
	accepted := map[string]string{
		"en":         "en",
		"EN":         "en",
		"En":         "en",
		"en-US":      "en",
		"en_US":      "en",
		"EN-us":      "en",
		"  de":       "de",
		"zh-Hans-CN": "zh",
		"fr":         "fr",
		"pt-BR":      "pt",
	}
	for raw, want := range accepted {
		got, ok := NormalizeLanguageCode(raw)
		if !ok || got != want {
			t.Errorf("NormalizeLanguageCode(%q) = (%q, %v), want (%q, true)", raw, got, ok, want)
		}
	}

	// Rejected: not a language code at all. "english" is the audit's
	// example — a real word, not a code.
	for _, raw := range []string{"", "english", "x", "abcd", "123", "e1", "e n", "!!!", "en-", "-en"} {
		if got, ok := NormalizeLanguageCode(raw); ok {
			t.Errorf("NormalizeLanguageCode(%q) = (%q, true), want rejection", raw, got)
		}
	}
}

// TestKnownLanguageCode pins the vocabulary the config layer validates
// against: every code detection can name with no user data, and nothing
// else.
func TestKnownLanguageCode(t *testing.T) {
	for _, code := range []string{"en", "de", "zh", "ja", "ko", "ru", "ar", "el", "he", "hi", "th", "EN", "en-US", "zh-CN"} {
		if !KnownLanguageCode(code) {
			t.Errorf("KnownLanguageCode(%q) = false; the bundled script codes must be known", code)
		}
	}
	for _, code := range []string{"", "xx", "english", "fr", "es", "zz"} {
		if KnownLanguageCode(code) {
			t.Errorf("KnownLanguageCode(%q) = true; only bundled detections are known without user data", code)
		}
	}
}

// TestBuiltinLangCodesCoverScriptCodes is the anti-drift pin for
// builtinLangCodes: every code scriptCodeByRune can return must be in the
// vocabulary the config layer accepts. A new script code added to the
// detector without being listed here would make a legitimate
// expected_language fail at startup.
func TestBuiltinLangCodesCoverScriptCodes(t *testing.T) {
	runes := []rune{
		0x4E2D, // zh
		0x3042, // ja
		0xAC00, // ko
		0x0410, // ru
		0x0627, // ar
		0x03B1, // el
		0x05D0, // he
		0x0905, // hi
		0x0E01, // th
	}
	for _, r := range runes {
		code := scriptCodeByRune(r)
		if code == "" {
			t.Fatalf("scriptCodeByRune(%#U) returned no code; the test rune is wrong", r)
		}
		if !slicesContains(builtinLangCodes, code) {
			t.Errorf("scriptCodeByRune(%#U) = %q, missing from builtinLangCodes %v", r, code, builtinLangCodes)
		}
	}
	// The two bundled Latin-script tables are part of the vocabulary too.
	for _, code := range []string{"en", "de"} {
		if !slicesContains(builtinLangCodes, code) {
			t.Errorf("bundled word-table code %q missing from builtinLangCodes %v", code, builtinLangCodes)
		}
	}
}

func slicesContains(hay []string, needle string) bool {
	return slices.Contains(hay, needle)
}

// TestLanguageFilter_ExpectedLanguageSpelling is pin (3), the end-to-end
// contract for "EN", "en-US" and "xx".
//
// "EN" and "en-US" behave EXACTLY like "en": they pass English prose,
// reject German, and report Name() == "language_en". "xx" — a code
// nothing can detect — is never passed through verbatim either: the
// filter falls back to English with a Warn (so it cannot reject
// everything), and the loud failure for a config-supplied "xx" is the
// validation error at config load, pinned in internal/config.
func TestLanguageFilter_ExpectedLanguageSpelling(t *testing.T) {
	ctx := context.Background()
	step := &task.TaskStep{}

	t.Run("EN is english", func(t *testing.T) {
		f := NewLanguageFilter("EN")
		if got := f.Name(); got != "language_en" {
			t.Fatalf("Name() = %q, want language_en", got)
		}
		if res := f.Process(ctx, step, englishFixture); res.Outcome != FilterPass {
			t.Fatalf("english prose under expected=EN: %+v, want pass", res)
		}
		if res := f.Process(ctx, step, germanFixture); res.Outcome != FilterFail {
			t.Fatalf("german prose under expected=EN: %+v, want fail", res)
		}
	})

	t.Run("en-US is english", func(t *testing.T) {
		f := NewLanguageFilter("en-US")
		if got := f.Name(); got != "language_en" {
			t.Fatalf("Name() = %q, want language_en", got)
		}
		if res := f.Process(ctx, step, englishFixture); res.Outcome != FilterPass {
			t.Fatalf("english prose under expected=en-US: %+v, want pass (region-qualified code must not reject correct output)", res)
		}
		if res := f.Process(ctx, step, germanFixture); res.Outcome != FilterFail {
			t.Fatalf("german prose under expected=en-US: %+v, want fail", res)
		}
	})

	t.Run("xx never becomes matches-nothing", func(t *testing.T) {
		// The audit's failure mode verbatim: "xx" reached the comparison
		// verbatim and rejected EVERY output. "xx" is well-FORMED (two
		// letters) but names no detectable language, so it must not
		// become the filter's identity at all: the filter declines to
		// filter, passes, and says why.
		f := NewLanguageFilter("xx")
		if got := f.Name(); got != "language_xx" {
			t.Fatalf("Name() = %q, want language_xx (a filter is named for its configured code, degraded or not)", got)
		}
		for name, output := range map[string]string{
			"english":  englishFixture,
			"german":   germanFixture,
			"chinese":  chineseFixture,
			"japanese": japaneseFixture,
		} {
			res := f.Process(ctx, step, output)
			if res.Outcome != FilterPass {
				t.Errorf("%s prose under expected=xx: %+v, want NOT a blanket rejection", name, res)
				continue
			}
			// Visible, not silent: the operator can see why nothing is
			// being filtered.
			if !strings.Contains(res.Reason, "no language detection for xx") {
				t.Errorf("%s prose: Reason = %q, want it to name the missing detection", name, res.Reason)
			}
		}
	})

	t.Run("english is still detected by a readable code", func(t *testing.T) {
		// The guard on the guard: a detectable code must NOT take the
		// no-filter path, or the M5 fix would have silently disabled the
		// language filter for every operator.
		f := NewLanguageFilter("en")
		if res := f.Process(ctx, step, englishFixture); res.Outcome != FilterPass || res.Reason != "" {
			t.Fatalf("english under expected=en: %+v, want a clean pass with no reason", res)
		}
		if res := f.Process(ctx, step, germanFixture); res.Outcome != FilterFail {
			t.Fatalf("german under expected=en: %+v, want fail (the filter must still filter)", res)
		}
	})

	t.Run("empty keeps the frozen default", func(t *testing.T) {
		f := NewLanguageFilter("")
		if got := f.Name(); got != "language_en" {
			t.Fatalf("Name() = %q, want language_en", got)
		}
		if res := f.Process(ctx, step, germanFixture); res.Outcome != FilterFail {
			t.Fatalf("german prose under default: %+v, want fail (default behavior must be unchanged)", res)
		}
	})
}

// TestBuiltinFilter_ExpectedLanguageSpellingThroughRegistry pins the
// wiring path the daemon actually uses: the `language_en` compat alias
// resolving to a configured expected_language.
func TestBuiltinFilter_ExpectedLanguageSpellingThroughRegistry(t *testing.T) {
	for _, raw := range []string{"en", "EN", "en-US", "en_us"} {
		f, err := NewBuiltinFilter("language_en", BuiltinConfig{ExpectedLang: raw})
		if err != nil {
			t.Fatalf("language_en with ExpectedLang=%q: %v", raw, err)
		}
		if got := f.Name(); got != "language_en" {
			t.Errorf("language_en with ExpectedLang=%q: Name() = %q, want language_en", raw, got)
		}
	}
	// An inline code normalizes the same way.
	f, err := NewBuiltinFilter("language_DE", BuiltinConfig{ExpectedLang: "en"})
	if err != nil {
		t.Fatalf("language_DE: %v", err)
	}
	if got := f.Name(); got != "language_de" {
		t.Errorf("language_DE Name() = %q, want language_de", got)
	}
}

// TestHasLanguageWordTable pins the config layer's second accept path: a
// code with no built-in detection is still valid when the operator
// shipped the word table that makes it detectable.
func TestHasLanguageWordTable(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fr.txt"), []byte(frenchCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, "es.txt"), 0o755); err != nil { // a DIRECTORY named es.txt
		t.Fatal(err)
	}
	for _, code := range []string{"fr", "FR", "fr-CA", "fr_CA"} {
		if !HasLanguageWordTable(code, dir) {
			t.Errorf("HasLanguageWordTable(%q) = false, want true", code)
		}
	}
	for _, code := range []string{"xx", "es", "english", ""} {
		if HasLanguageWordTable(code, dir) {
			t.Errorf("HasLanguageWordTable(%q) = true, want false", code)
		}
	}
}

// TestDetectedLanguage_ConcurrentLoadAndDetect is pin (4): the M7 race.
// The detector walked the package-level table map outside loadMu on
// every word of every call; a concurrent LoadLanguageWordTables (daemon
// boot, or a test loading per-case) tripped -race on the map header.
// Run under -race, a survivor here is a failure.
func TestDetectedLanguage_ConcurrentLoadAndDetect(t *testing.T) {
	t.Cleanup(func() { _ = LoadLanguageWordTables(filepath.Join(t.TempDir(), "absent")) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fr.txt"), []byte(frenchCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	// Detector side.
	for range 4 {
		wg.Go(func() {
			for range 50 {
				detectedLanguage("le fichier est dans la table et il a ete fait pour notre equipe")
				NewLanguageFilter("fr").Process(context.Background(), &task.TaskStep{}, "le fichier est dans la table")
			}
		})
	}
	// Loader side: repeated swaps of the package-global table map.
	wg.Go(func() {
		for range 50 {
			_ = LoadLanguageWordTables(dir)
		}
	})
	wg.Wait()

	if lookupExtraLangSet("fr") == nil {
		t.Fatal("fr table missing after concurrent load; the test did not exercise the swap")
	}
}

// TestLanguageFilter_NoPerWordSnapshot confirms the snapshot is taken
// ONCE per detection call, not once per word: a text with many words and
// a loaded table must still detect correctly (the pre-M7 code took a
// fresh snapshot inside the loop, which was both a race and O(words x
// languages) copying).
func TestLanguageFilter_NoPerWordSnapshot(t *testing.T) {
	t.Cleanup(func() { _ = LoadLanguageWordTables(filepath.Join(t.TempDir(), "absent")) })

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fr.txt"), []byte(frenchCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadLanguageWordTables(dir); err != nil {
		t.Fatalf("load: %v", err)
	}

	// Long prose: if the snapshot were taken per word and raced, the
	// detector could observe a half-swapped map and miss the language.
	prose := strings.Repeat("le fichier est dans la table avec les donnees du client et il a ete fait ", 20)
	code, conf := detectedLanguage(prose)
	if code != "fr" || conf < 0.5 {
		t.Fatalf("detectedLanguage(long french prose) = (%q, %.2f), want (fr, >=0.50)", code, conf)
	}
}

func TestLanguageFilter_UndetectableCodeDoesNotFilter(t *testing.T) {
	// The `language_fr` path with NO word table loaded: the code is valid
	// and stays the filter's identity, but detection cannot name "fr", so
	// filtering it would reject every output. It must pass and say why.
	f := NewLanguageFilter("fr")
	if got := f.Name(); got != "language_fr" {
		t.Fatalf("Name() = %q, want language_fr", got)
	}
	res := f.Process(context.Background(), &task.TaskStep{}, englishFixture)
	if res.Outcome != FilterPass {
		t.Fatalf("english prose with no fr table: %+v, want pass (undetectable code must not reject)", res)
	}
	if !strings.Contains(res.Reason, "no language detection for fr") {
		t.Fatalf("Reason = %q, want it to name the missing detection", res.Reason)
	}
}

// TestLanguageFilter_UserTableLoadedBeforeConstruction is the wiring-order
// contract: the daemon loads word tables BEFORE building the chain, so a
// filter for a table-backed language DOES filter.
func TestLanguageFilter_UserTableLoadedBeforeConstruction(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "fr.txt"), []byte(frenchCSV), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := LoadLanguageWordTables(dir); err != nil {
		t.Fatalf("load: %v", err)
	}
	t.Cleanup(func() { _ = LoadLanguageWordTables(filepath.Join(t.TempDir(), "absent")) })

	f := NewLanguageFilter("fr")
	prose := "le fichier est dans la table avec les donnees du client et il a ete fait pour notre equipe"
	if res := f.Process(context.Background(), &task.TaskStep{}, prose); res.Outcome != FilterPass || res.Reason != "" {
		t.Fatalf("french prose under a loaded fr table: %+v, want a clean pass", res)
	}
	if res := f.Process(context.Background(), &task.TaskStep{}, englishFixture); res.Outcome != FilterFail {
		t.Fatalf("english prose under a loaded fr table: %+v, want fail (the table-backed filter must filter)", res)
	}
}
