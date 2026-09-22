package validator

import (
	"context"
	"fmt"
	"strings"
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

func parseWordList(list string) map[string]struct{} {
	set := make(map[string]struct{}, 256)
	for _, w := range strings.Fields(list) {
		set[w] = struct{}{}
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
	switch {
	case englishRate >= germanRate && englishRate >= 0.5:
		return "en", englishRate
	case germanRate > englishRate && germanRate >= 0.5:
		return "de", germanRate
	default:
		return "", max64(englishRate, germanRate)
	}
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
