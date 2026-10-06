package validator

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"unicode"

	"github.com/caimlas/meept/internal/task"
)

// englishStopwords is the embedded English stopword list (a Go string
// constant per the leaf contract; ~200 words). Detection is pure stdlib +
// these tables: NO network, NO model call.
const englishStopwords = `the be to of and a in that have i it for not on with he as you do at this
but his by from they we say her she or an will my one all would there their what so up out if about
who get which go me when make can like time no just him know take people into year your good some
could them see other than then now look only come its over think also back after use two how our
work first well way even new want because any these give day most us is are was were been am has
had said each may find where much before right too means old same tell set three state never
become between high really something those always show large both hold must little follow around
often through another world still own under last end house part might next place made against
every great small point man women child school study group problem number case fact thing system
program question hand head side order line government during without again while why general
several difference within along since however upon per among across toward beyond yet`

// germanCues is the supplementary Latin-script cue list that lets the
// hit-rate path detect a non-English language (a Latin-script German
// sentence shares no function words with the English list, so without a
// second table its hit-rate could never reach the 0.5 floor and
// lang=de failures would be unobservable). Format mirrors englishStopwords.
const germanCues = `der die das des dem den ein eine einen einem eines einer und ist sind war wird
werden wurde wurden nicht auch noch schon nur aber als wie bei mit von vom zum zur im am an auf
aus nach über unter für um durch dass wenn weil da doch ja nein man sich ihn ihr ihre seinem ihrer
habe haben hat hatte sein uns euch sie er wir es diese dieser dieses diesen hier dort dann wann wo
warum viele mehr wenig sehr heute morgen gestern immer nie oft manchmal jetzt bald darauf deshalb
jedoch wobei obwohl damit sollen kann können muss darf möchte wollen zwischen gegen ohne innerhalb
mittlerweile ebenfalls ebenso sowie sowohl weder sondern eher`

// stopwordSet / germanCueSet are the parsed word tables.
var (
	stopwordSet  = parseWordList(englishStopwords)
	germanCueSet = parseWordList(germanCues)
)

// extraLangTables holds user-supplied word tables for Latin-script
// languages beyond the built-in en/de pairs, keyed by language code.
// Loaded once per process from $MEEPT_HOME/validator/lang/<code>.txt
// (one word per line, # comments allowed) by LoadLanguageWordTables —
// wired at daemon boot BEFORE the filter chain is built. A maintainer
// adding French output support drops a ~200-word function-word list
// (articles, pronouns, common verbs) into validator/lang/fr.txt and sets
// output_filters.expected_language = "fr": no Go, no rebuild.
//
// Thread-safety: written once before the chain exists, read-only after —
// guarded by loadMu for tests that load per-case.
var (
	extraLangTables map[string]map[string]struct{}
	loadMu          sync.Mutex
)

// LoadLanguageWordTables reads every <code>.txt under
// $MEEPT_HOME/validator/lang into the detection tables. Missing dir is
// the normal case (no user tables); per-file errors are collected and
// returned so the daemon can Warn without skipping the other files.
// Safe to call multiple times: later calls REPLACE earlier tables.
// File I/O happens BEFORE the lock; only the final table swap is
// synchronized (mutexio: never hold a mutex across I/O).
func LoadLanguageWordTables(dir string) error {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			// Missing dir is the normal "no user tables" case. CLEAR the
			// tables: the contract says a later call REPLACES earlier
			// tables, and a missing dir is the empty set. Returning
			// without clearing left the previous tables live forever —
			// every "reset for other tests" cleanup in this package was
			// silently a no-op, so a table loaded by one test decided the
			// outcome of the next (bughunt wave DISCLOSURE 6 class).
			replaceExtraLangTables(nil)
			return nil
		}
		return fmt.Errorf("language word tables: %w", err)
	}
	tables := make(map[string]map[string]struct{})
	var errs []error
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".txt") {
			continue
		}
		code := strings.TrimSuffix(e.Name(), ".txt")
		// Key the table by the SAME normalized code the filter compares
		// against, so a file named "FR.txt" or "en-US.txt" is reachable
		// from the configured expected_language rather than silently
		// loaded into a bucket nothing looks in.
		norm, ok := NormalizeLanguageCode(code)
		if !ok {
			errs = append(errs, fmt.Errorf("%s: %q is not a language code (expected a 2-3 letter ISO-639 code, e.g. fr.txt)", e.Name(), code))
			continue
		}
		if norm == "en" || norm == "de" {
			continue // built-ins are code-defined; user files can't shadow them
		}
		raw, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			errs = append(errs, fmt.Errorf("read %s: %w", e.Name(), err))
			continue
		}
		words := parseWordList(string(raw))
		if len(words) < 20 {
			errs = append(errs, fmt.Errorf("%s: %d words — need >= 20 for a usable hit-rate table (one word per line)", e.Name(), len(words)))
			continue
		}
		tables[norm] = words
	}
	replaceExtraLangTables(tables)
	return errors.Join(errs...)
}

// replaceExtraLangTables installs tables as the package-global word
// tables. nil clears them. The swap is the ONLY writer, and it is the
// only place loadMu is taken for a write.
func replaceExtraLangTables(tables map[string]map[string]struct{}) {
	loadMu.Lock()
	defer loadMu.Unlock()
	extraLangTables = tables
}

// lookupExtraLangSet returns the user table for code, or nil.
func lookupExtraLangSet(code string) map[string]struct{} {
	loadMu.Lock()
	defer loadMu.Unlock()
	return extraLangTables[code]
}

// tokenizeWords is the SINGLE source of truth for splitting text into
// detection tokens: non-letters separate, an inner apostrophe stays part
// of the word ("don't" is one token, a bare apostrophe is not a word).
// The word-table loader and the detector MUST both go through it —
// they used to disagree (loader split on whitespace, detector on
// non-letters), so the natural `de, la, les, …` CSV form loaded 20+
// tokens that could never match anything (bughunt wave M6/L12).
func tokenizeWords(text string) iter.Seq[string] {
	return strings.FieldsFuncSeq(text, func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	})
}

// normalizeWord is the single normalization applied to every token on both
// sides of the comparison: trim the surrounding apostrophes a tokenizer
// can leave at an edge ("'le'" -> "le") and lowercase it.
func normalizeWord(field string) string {
	return strings.ToLower(strings.Trim(field, "'"))
}

// parseWordList builds a word set from a word table. `#` comments are
// stripped at the LINE level BEFORE tokenizing: a per-token strip leaked
// the words of a trailing comment into the table ("# french function
// words" contributed "french"/"function"/"words", bughunt wave L12),
// because only a token that STARTED with `#` was dropped.
func parseWordList(list string) map[string]struct{} {
	set := make(map[string]struct{}, 256)
	for line := range strings.Lines(list) {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		for field := range tokenizeWords(line) {
			word := normalizeWord(field)
			if word == "" {
				continue
			}
			set[word] = struct{}{}
		}
	}
	return set
}

// scriptCodeByRune classifies a rune into a language code via unicode
// script membership. Latin runes return "" - Latin-script text needs the
// word-table pass to disambiguate.
func scriptCodeByRune(r rune) string {
	switch {
	case unicode.Is(unicode.Han, r):
		return "zh"
	case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
		return "ja"
	case unicode.Is(unicode.Hangul, r):
		return "ko"
	case unicode.Is(unicode.Cyrillic, r):
		return "ru"
	case unicode.Is(unicode.Arabic, r):
		return "ar"
	case unicode.Is(unicode.Greek, r):
		return "el"
	case unicode.Is(unicode.Hebrew, r):
		return "he"
	case unicode.Is(unicode.Devanagari, r):
		return "hi"
	case unicode.Is(unicode.Thai, r):
		return "th"
	}
	return ""
}

// detectedLanguage returns the dominant language of text and a confidence
// in [0,1]. Non-Latin scripts are detected per-rune and are authoritative
// (kana presence implies Japanese; otherwise the majority script wins).
// Latin-script text is scored by word-table hit-rate: English stopwords vs
// German cues; the winner at rate >= 0.5 is the detection, with English
// winning ties.
func detectedLanguage(text string) (string, float64) {
	scriptRunes := map[string]int{}
	latinWords := 0
	englishHits := 0
	germanHits := 0
	// Per-code hit counts for user-supplied tables, materialized lazily:
	// the common case (no user tables) never allocates it.
	extraHits := map[string]int{}
	// One snapshot of the user tables for the whole call: the loop below
	// does not take loadMu per word, and the sets are read-only after
	// load. (The previous code guarded on the package-level map header
	// OUTSIDE loadMu inside this loop — an unsynchronized read racing
	// LoadLanguageWordTables, bughunt wave M7. The snapshot alone is the
	// guard: it returns nil when no tables are loaded.)
	extras := snapshotExtraTables()

	for field := range tokenizeWords(text) {
		latin := true
		for _, r := range field {
			if code := scriptCodeByRune(r); code != "" {
				latin = false
				scriptRunes[code]++
			}
		}
		if !latin {
			continue
		}
		latinWords++
		word := normalizeWord(field)
		if _, ok := stopwordSet[word]; ok {
			englishHits++
		}
		if _, ok := germanCueSet[word]; ok {
			germanHits++
		}
		for code, set := range extras {
			if _, ok := set[word]; ok {
				extraHits[code]++
			}
		}
	}

	total := 0
	for _, n := range scriptRunes {
		total += n
	}
	if total > 0 {
		// Kana occurs ONLY in Japanese: any real presence settles it.
		if scriptRunes["ja"] >= 3 {
			return "ja", 1.0
		}
		best, bestCount := "", 0
		for code, n := range scriptRunes {
			if n > bestCount {
				best, bestCount = code, n
			}
		}
		if bestCount*2 >= total {
			return best, 1.0
		}
	}

	if latinWords == 0 {
		return "", 0
	}
	englishRate := float64(englishHits) / float64(latinWords)
	germanRate := float64(germanHits) / float64(latinWords)
	bestCode, bestRate := "en", englishRate
	if germanRate > bestRate {
		bestCode, bestRate = "de", germanRate
	}
	for code, hits := range extraHits {
		rate := float64(hits) / float64(latinWords)
		if rate > bestRate {
			bestCode, bestRate = code, rate
		}
	}
	if bestRate >= 0.5 {
		return bestCode, bestRate
	}
	return "", max64(englishRate, germanRate)
}

// snapshotExtraTables copies the extra-table map header (code -> set) so
// iteration does not hold loadMu. The sets themselves are read-only after
// load; the snapshot is of the OUTER map only.
func snapshotExtraTables() map[string]map[string]struct{} {
	loadMu.Lock()
	defer loadMu.Unlock()
	if extraLangTables == nil {
		return nil
	}
	out := make(map[string]map[string]struct{}, len(extraLangTables))
	maps.Copy(out, extraLangTables)
	return out
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// DefaultLanguageCode is the language code assumed when none is
// configured — the frozen pre-existing default for an empty
// expected_language.
const DefaultLanguageCode = "en"

// builtinLangCodes is every language code the detector can name with NO
// user data: the per-rune script codes plus the two bundled Latin-script
// word tables. A code outside this set is still perfectly usable — it
// just needs a word table at $MEEPT_HOME/validator/lang/<code>.txt (the
// data-driven tier). This is the vocabulary the config layer validates
// against; TestBuiltinLangCodesCoverScriptCodes pins it against the
// codes scriptCodeByRune actually returns.
var builtinLangCodes = []string{
	"ar", "de", "el", "en", "he", "hi", "ja", "ko", "ru", "th", "zh",
}

// NormalizeLanguageCode reduces a configured language code to the single
// token detection actually compares: lowercased, region/script subtags
// dropped ("en-US" -> "en", "EN_us" -> "en"), and a shape check that
// rejects anything that is not an ISO-639 primary subtag (2-3 ASCII
// letters). It reports false for "" and for typos like "english".
//
// Why a primary-subtag shape and not a full BCP-47 parse: detection
// returns a bare primary code (the bundled tables' literals and the
// word-table filename stem), so a region qualifier can never match it.
// Accepting "en-US" as "en" is the deliberate call (bughunt wave M5):
// a well-formed tag whose PRIMARY language is real is the user's
// intent spelled with a region, and treating it as "no words match"
// would reject every output including correct English — a self-inflicted
// denial of service on the chat path. A code whose primary subtag is not
// real ("xx", "english") is NOT repaired here: it fails the vocabulary
// check at config load, where the mistake is visible, instead of
// resolving to some other language here.
func NormalizeLanguageCode(raw string) (string, bool) {
	code := strings.ToLower(strings.TrimSpace(raw))
	if i := strings.IndexAny(code, "-_"); i >= 0 {
		// An empty subtag on either side ("en-", "-en") is a malformed
		// tag, not a base language: reject rather than silently repair.
		if i == 0 || i == len(code)-1 {
			return "", false
		}
		code = code[:i]
	}
	if len(code) < 2 || len(code) > 3 {
		return "", false
	}
	for i := range len(code) {
		if code[i] < 'a' || code[i] > 'z' {
			return "", false
		}
	}
	return code, true
}

// KnownLanguageCode reports whether raw names a language detection can
// resolve with no user data: it normalizes to a primary subtag AND that
// subtag is one of the bundled script codes or word tables. It is the
// predicate the config layer uses to reject an unknown code at load.
func KnownLanguageCode(raw string) bool {
	code, ok := NormalizeLanguageCode(raw)
	if !ok {
		return false
	}
	return slices.Contains(builtinLangCodes, code)
}

// HasLanguageWordTable reports whether a user word table for raw is
// present in dir — the second way a code becomes usable (the
// data-driven Latin-script tier). Callers pass
// $MEEPT_HOME/validator/lang. It shares NormalizeLanguageCode with the
// loader, so the code a table satisfies is exactly the code the filter
// compares against.
func HasLanguageWordTable(raw, dir string) bool {
	code, ok := NormalizeLanguageCode(raw)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(dir, code+".txt"))
	return err == nil && !info.IsDir()
}

// LanguageFilter is a fail-only language detector: whole-output
// wrong-language responses are rejected; the filter NEVER rewrites.
// Detection is heuristic by design (master Contract 5 note): script-range
// scan plus bundled word-table hit-rates, stdlib-only.
type LanguageFilter struct {
	expected string
	// undetectable records the configured code when detection cannot
	// name it. Non-empty means Process declines to filter (it passes and
	// reports why) instead of rejecting every output — see
	// NewLanguageFilter and the M5 note there.
	undetectable string
}

// NewLanguageFilter creates a language output filter expecting the given
// language code. The code is normalized (see NormalizeLanguageCode), so
// "EN" and "en-US" both mean English and a filter built from either
// reports the same Name() as one built from "en".
//
// An EMPTY code keeps the frozen default "en".
//
// A code detection cannot name — "xx", a two-letter shape naming no
// language — falls back to the default with a Warn rather than being
// carried into the comparison. This is the second half of the M5 fix and
// it is the part that actually stops the denial of service: an
// unmatchable expectation rejects EVERY output, including output in the
// language the operator asked for, and burns the retry budget to
// rejected_exhausted on the chat path. A filter that cannot detect its
// own language can only do harm, so it declines to filter.
//
// It never fails OPEN in the dangerous direction either: the fallback is
// to English, the historical default, NOT to "pass everything". The
// loud failure for a config-supplied code remains the validation error at
// config load (OutputFiltersConfig.Validate); this Warn covers the
// direct-constructor and inline `language_<code>` paths. The fallback is
// reported through Process as "language detection is not available for
// <code>", so the condition is visible in the filter accounting rather
// than silent.
func NewLanguageFilter(expected string) *LanguageFilter {
	if expected == "" {
		return &LanguageFilter{expected: DefaultLanguageCode}
	}
	code, ok := NormalizeLanguageCode(expected)
	if !ok {
		slog.Warn("output filter expected language is not a language code; assuming English",
			"expected_language", expected,
			"assumed", DefaultLanguageCode,
		)
		return &LanguageFilter{expected: DefaultLanguageCode}
	}
	f := &LanguageFilter{expected: code}
	if !isDetectableCode(code) {
		// Well-formed but undetectable (e.g. "xx", or "fr" with no word
		// table). Degrade to "do not filter": a code no detector can
		// produce would reject every output.
		slog.Warn("output filter expected language cannot be detected; not filtering on it",
			"expected_language", code,
			"word_table_hint", "ship a word list at $MEEPT_HOME/validator/lang/"+code+".txt",
		)
		f.undetectable = code
	}
	return f
}

// isDetectableCode reports whether detection can ever return code: one of
// the bundled per-rune script codes or Latin-script tables, OR a code a
// user word table has been loaded for.
//
// It reads the SAME package state (loadMu-guarded) the detector's scoring
// reads, so the answer cannot drift from what detection actually does.
// The answer is taken ONCE, at construction: the daemon loads word tables
// before it builds the filter chain (internal/daemon/filter_wiring.go),
// which is the documented order, so a filter never changes behavior
// underneath the step pipeline after it is wired. Name() deliberately
// does NOT depend on this — a filter's identity is its configured code.
func isDetectableCode(code string) bool {
	if slices.Contains(builtinLangCodes, code) {
		return true
	}
	return lookupExtraLangSet(code) != nil
}

// Name implements OutputFilter: "language_en" (or "language_<code>").
func (f *LanguageFilter) Name() string { return "language_" + f.expected }

// Applies implements OutputFilter: the filter is declared by chain
// membership, so any non-nil step carries prose output it applies to.
func (f *LanguageFilter) Applies(step *task.TaskStep) bool { return step != nil }

// Process implements OutputFilter. Empty output passes; when the detected
// language differs from the expected one with confidence >= 0.5 the filter
// fails with Reason "lang=<code> confidence=<f> expected=<code>"; anything
// else passes. A filter built from a code detection cannot name does not
// filter at all: it passes with a reason naming the condition, because
// the alternative (comparing against a code nothing produces) rejects
// every output. Pure function of the input: idempotent.
func (f *LanguageFilter) Process(_ context.Context, _ *task.TaskStep, output string) FilterResult {
	if f.undetectable != "" {
		return FilterResult{Outcome: FilterPass, Filter: f.Name(),
			Reason: fmt.Sprintf("no language detection for %s; not filtering", f.undetectable)}
	}
	if strings.TrimSpace(output) == "" {
		return FilterResult{Outcome: FilterPass, Filter: f.Name()}
	}
	detected, confidence := detectedLanguage(output)
	if detected != "" && detected != f.expected && confidence >= 0.5 {
		return FilterResult{Outcome: FilterFail, Filter: f.Name(),
			Reason: fmt.Sprintf("lang=%s confidence=%.2f expected=%s", detected, confidence, f.expected)}
	}
	return FilterResult{Outcome: FilterPass, Filter: f.Name()}
}
