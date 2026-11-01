// Package pii implements the detection-and-swap half of the unmasking
// proxy: regex-first detection of personally identifiable information
// (PII) with a pluggable Recognizer seam, and reversible
// pseudonymization via [PII:<uuid-v4>] tokens stored in a durable,
// session-scoped SwapStore.
//
// Value discipline (mirrors internal/secrets/broker.go): raw PII values
// and the swap map are in-process only - they never reach logs, env
// entries, audit rows, or traces. Error strings wrap indices and
// identifiers, never text.
package pii

import (
	"regexp"
	"sort"
	"strings"
)

// PII classes detected by the default recognizer set (research D1).
const (
	ClassEmailAddress = "EMAIL_ADDRESS"
	ClassPhoneNumber  = "PHONE_NUMBER"
	ClassSSN          = "SSN"
	ClassCreditCard   = "CREDIT_CARD"
	ClassIPv4         = "IPV4"
	ClassIBAN         = "IBAN"
)

// Recognizer produces PII spans for a piece of text. It is the seam a
// future NER backend (local spaCy sidecar, RemoteRecognizer-style)
// implements without touching call sites. Built-in regex recognizers
// satisfy it too.
type Recognizer interface {
	Detect(text string) []Span
}

// Span is one detected PII occurrence. Value is in-process only: it
// never crosses a log, env, audit, or trace boundary.
type Span struct {
	Start int
	End   int
	Class string
	Value string
}

// Config controls detector construction. It maps 1:1 to the future
// PiiConfig schema leaf 02 adds (the config schema itself is out of
// scope for this leaf).
type Config struct {
	// Enabled gates all detection. A disabled Detector returns no spans.
	Enabled bool
	// Classes restricts detection to the listed classes; empty means all
	// default classes.
	Classes []string
}

// Detector finds PII spans in text using an ordered recognizer set.
type Detector struct {
	recognizers []Recognizer
}

// luhnValid reports whether the digit string passes the Luhn checksum.
func luhnValid(digits string) bool {
	sum := 0
	double := false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if double {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		double = !double
	}
	return sum%10 == 0
}

// allDigits extracts ASCII digits from s.
func allDigits(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] >= '0' && s[i] <= '9' {
			b.WriteByte(s[i])
		}
	}
	return b.String()
}

// digitRunAdjacent reports whether the span's digit edges touch a longer
// digit run (a 16-digit reference number must not yield a phone span).
// The pattern boundaries below only guard against non-digit characters;
// this check closes the digit-digit gap a regex cannot express.
func digitRunAdjacent(text string, sp *Span) bool {
	if sp.Start > 0 && text[sp.Start-1] >= '0' && text[sp.Start-1] <= '9' {
		return true
	}
	return sp.End < len(text) && text[sp.End] >= '0' && text[sp.End] <= '9'
}

// ibanValid checks mod-97 validity per ISO 13616 (rearrange, convert
// letters to numbers, compute the remainder).
func ibanValid(iban string) bool {
	rearranged := iban[4:] + iban[:4]
	rem := 0
	for i := 0; i < len(rearranged); i++ {
		c := rearranged[i]
		switch {
		case c >= '0' && c <= '9':
			rem = (rem*10 + int(c-'0')) % 97
		case c >= 'A' && c <= 'Z':
			v := int(c-'A') + 10
			rem = (rem*100 + v) % 97
		default:
			return false
		}
	}
	return rem == 1
}

// defaultPatterns pairs a class with a pre-validated pattern; validators
// run on the matched text before a span is emitted.
type defaultPatterns struct {
	class    string
	pattern  *regexp.Regexp
	validate func(text string, sp *Span) bool
	digitRun bool // apply the digit-run adjacency guard
}

var defaultRecognizers = []defaultPatterns{
	{
		class:   ClassEmailAddress,
		pattern: regexp.MustCompile(`[A-Za-z0-9._%+\-]+@[A-Za-z0-9.\-]+\.[A-Za-z]{2,}`),
	},
	{
		class:   ClassIBAN,
		pattern: regexp.MustCompile(`\b[A-Z]{2}[0-9]{2}(?:[ ]?[A-Z0-9]{4}){2,7}(?:[ ]?[A-Z0-9]{1,3})?\b`),
		validate: func(_ string, sp *Span) bool {
			// Mod-97 per ISO 13616 runs over the compact form: grouping
			// spaces are stripped before validation.
			return ibanValid(strings.ReplaceAll(sp.Value, " ", ""))
		},
		digitRun: true,
	},
	{
		class:   ClassCreditCard,
		pattern: regexp.MustCompile(`\b(?:[0-9]{4}[ -]?){3}[0-9]{4}\b`),
		validate: func(_ string, sp *Span) bool {
			// Luhn plus a plausibility floor: the all-zero run (and any
			// single-repeated-digit run) passes the mod-10 arithmetic but
			// is not a card. Luhn cuts false positives; this cuts the
			// degenerate remainder.
			digits := allDigits(sp.Value)
			if !luhnValid(digits) {
				return false
			}
			for i := 1; i < len(digits); i++ {
				if digits[i] != digits[0] {
					return true
				}
			}
			return false
		},
		digitRun: true,
	},
	{
		class:   ClassSSN,
		pattern: regexp.MustCompile(`\b[0-9]{3}-[0-9]{2}-[0-9]{4}\b`),
	},
	{
		class:   ClassPhoneNumber,
		pattern: regexp.MustCompile(`(?:\+\d{1,3}[ .\-]?)?(?:\(\d{1,4}\)[ .\-]?)?\d{3}[ .\-]?\d{3,4}(?:[ .\-]?\d{2,4})?`),
		validate: func(text string, sp *Span) bool {
			digits := allDigits(sp.Value)
			return len(digits) >= 7 && len(digits) <= 15
		},
		digitRun: true,
	},
	{
		class: ClassIPv4,
		pattern: regexp.MustCompile(
			`\b(?:(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\.){3}` +
				`(?:25[0-5]|2[0-4][0-9]|1[0-9][0-9]|[1-9]?[0-9])\b`),
	},
}

// recognizerAdapter runs one default pattern and validates its matches.
type recognizerAdapter struct {
	class    string
	pattern  *regexp.Regexp
	validate func(text string, sp *Span) bool
	digitRun bool
}

func (a recognizerAdapter) Detect(text string) []Span {
	var spans []Span
	for _, loc := range a.pattern.FindAllStringIndex(text, -1) {
		sp := Span{Start: loc[0], End: loc[1], Class: a.class, Value: text[loc[0]:loc[1]]}
		if a.digitRun && digitRunAdjacent(text, &sp) {
			continue
		}
		if a.validate != nil && !a.validate(text, &sp) {
			continue
		}
		spans = append(spans, sp)
	}
	return spans
}

// NewDetector builds a Detector with the default recognizer set filtered
// by cfg.Classes.
func NewDetector(cfg Config) *Detector {
	allowed := make(map[string]bool, len(cfg.Classes))
	for _, c := range cfg.Classes {
		allowed[c] = true
	}
	useAll := len(cfg.Classes) == 0

	d := &Detector{}
	if !cfg.Enabled {
		return d
	}
	for i := range defaultRecognizers {
		dr := &defaultRecognizers[i]
		if !useAll && !allowed[dr.class] {
			continue
		}
		d.recognizers = append(d.recognizers, recognizerAdapter{
			class:    dr.class,
			pattern:  dr.pattern,
			validate: dr.validate,
			digitRun: dr.digitRun,
		})
	}
	return d
}

// AddRecognizer appends a pluggable recognizer (the NER seam).
func (d *Detector) AddRecognizer(r Recognizer) {
	d.recognizers = append(d.recognizers, r)
}

// Detect returns ordered, non-overlapping spans: conflicts resolve
// longest-first with earlier-start tie-break, overlapping spans dropped.
func (d *Detector) Detect(text string) []Span {
	if text == "" || len(d.recognizers) == 0 {
		return nil
	}
	var all []Span
	for _, r := range d.recognizers {
		all = append(all, r.Detect(text)...)
	}
	return resolveSpans(all)
}

// resolveSpans sorts longest-first, drops overlapping spans, and returns
// start-ordered non-overlapping results.
func resolveSpans(spans []Span) []Span {
	if len(spans) == 0 {
		return nil
	}
	sort.SliceStable(spans, func(i, j int) bool {
		li, lj := spans[i].End-spans[i].Start, spans[j].End-spans[j].Start
		if li != lj {
			return li > lj
		}
		return spans[i].Start < spans[j].Start
	})
	kept := spans[:0]
	for i, sp := range spans {
		overlap := false
		for _, k := range kept {
			if sp.Start < k.End && k.Start < sp.End {
				overlap = true
				break
			}
		}
		if !overlap {
			kept = append(kept, spans[i])
		}
	}
	sort.SliceStable(kept, func(i, j int) bool { return kept[i].Start < kept[j].Start })
	return kept
}
