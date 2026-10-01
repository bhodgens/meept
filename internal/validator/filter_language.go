package validator

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
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
			return nil // no user tables — normal
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
		if code == "" || code == "en" || code == "de" {
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
		tables[code] = words
	}
	loadMu.Lock()
	extraLangTables = tables
	loadMu.Unlock()
	return errors.Join(errs...)
}

// lookupExtraLangSet returns the user table for code, or nil.
func lookupExtraLangSet(code string) map[string]struct{} {
	loadMu.Lock()
	defer loadMu.Unlock()
	return extraLangTables[code]
}

func parseWordList(list string) map[string]struct{} {
	set := make(map[string]struct{}, 256)
	for w := range strings.FieldsSeq(list) {
		w = strings.TrimSpace(w)
		if w == "" || strings.HasPrefix(w, "#") {
			continue
		}
		set[strings.ToLower(w)] = struct{}{}
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

	for _, field := range strings.FieldsFunc(text, func(r rune) bool {
		return !unicode.IsLetter(r) && r != '\''
	}) {
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
		word := strings.ToLower(strings.Trim(field, "'"))
		if _, ok := stopwordSet[word]; ok {
			englishHits++
		}
		if _, ok := germanCueSet[word]; ok {
			germanHits++
		}
		// User tables: one lookup per loaded language per word. The map
		// is usually empty; snap it once per call, not per word.
		if extraLangTables != nil {
			for code, set := range snapshotExtraTables() {
				if _, ok := set[word]; ok {
					extraHits[code]++
				}
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
	for code, set := range extraLangTables {
		out[code] = set
	}
	return out
}

func max64(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}

// LanguageFilter is a fail-only language detector: whole-output
// wrong-language responses are rejected; the filter NEVER rewrites.
// Detection is heuristic by design (master Contract 5 note): script-range
// scan plus bundled word-table hit-rates, stdlib-only.
type LanguageFilter struct {
	expected string
}

// NewLanguageFilter creates a language output filter expecting the given
// language code ("en" when empty).
func NewLanguageFilter(expected string) *LanguageFilter {
	if expected == "" {
		expected = "en"
	}
	return &LanguageFilter{expected: expected}
}

// Name implements OutputFilter: "language_en" (or "language_<code>").
func (f *LanguageFilter) Name() string { return "language_" + f.expected }

// Applies implements OutputFilter: the filter is declared by chain
// membership, so any non-nil step carries prose output it applies to.
func (f *LanguageFilter) Applies(step *task.TaskStep) bool { return step != nil }

// Process implements OutputFilter. Empty output passes; when the detected
// language differs from the expected one with confidence >= 0.5 the filter
// fails with Reason "lang=<code> confidence=<f> expected=<code>"; anything
// else passes. Pure function of the input: idempotent.
func (f *LanguageFilter) Process(_ context.Context, _ *task.TaskStep, output string) FilterResult {
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
