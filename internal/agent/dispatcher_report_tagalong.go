package agent

import "strings"

// Report tag-along arbitration (2026-09-18 tool-boundary-hardening leaf 04).
//
// The 2026-09-10 chatLike collapse (dispatcher.go) absorbs chat/platform/recall
// tag-alongs when exactly one actionable intent remains. Report was excluded
// (bughunt 2026-09-12 F42) because "write a summary and a report" is two
// deliverables. But the READBACK flavor of report — "then tell me the full
// path", "and show me the result" — asks for the FIRST action's output to be
// surfaced, not for a second deliverable. In e2e T1 (2026-09-18) that flavor
// still went compound (intents=4 type=parallel) and routed a one-file task
// into a planner pair session.
//
// classifyReportTagAlong is the positive-signal detector separating the two
// flavors; the collapse arm in classifyMultiIntent consumes it for the
// actionable==2 case.

// reportTagAlongObjects lists output-reference nouns: a report clause
// containing one of these asks to surface the first action's OUTPUT.
// Each entry earns its keep from the motivating prompt noted beside it.
var reportTagAlongObjects = []string{
	"path",     // T1 e2e 2026-09-18: "…, then tell me the full path"
	"paths",    // plural of the T1 readback ("list the paths")
	"result",   // T1 variant: "and show me the result"
	"results",  // plural: "then show me the results"
	"output",   // "then show me the output"
	"content",  // "then show me the file's content"
	"contents", // plural variant of the content readback
	"it",       // T1 variant: "then tell me where it is" (demonstrative)
	"that",     // "then show me that" (demonstrative)
	"them",     // "then list them" (demonstrative over plural artifacts)
	"file's",   // possessive artifact reference: "tell me the file's path"
	"its",      // possessive: "then show me its content"
}

// reportTagAlongWorkVerbs are OWN-WORK verbs: when the report clause carries
// one, it is producing something itself, not reading back — F42 2026-09-12:
// "write a summary and a report" must stay compound. Each entry cites the
// prompt it earns its keep from.
var reportTagAlongWorkVerbs = []string{
	"write",     // F42: "…, and write a report about the findings"
	"create",    // F42 shape: "…, then create a report on the rollout"
	"make",      // "…, and make a summary of the meeting"
	"generate",  // "…, then generate a report on the test suite"
	"produce",   // "…, and produce a summary document"
	"summarize", // "…, then summarize the findings in a doc"
}

// classifyReportTagAlong reports whether the REPORT clause asks to surface
// the FIRST action's output (a readback) rather than produce an independent
// deliverable. Matching is case-insensitive substring based — deliberately
// narrow, no general NLP parsing.
//
// Positive signals (any ⇒ true): an object noun from reportTagAlongObjects,
// or a demonstrative pronoun (it/that/them), or a possessive artifact
// reference ("the file('s) … path", "its content").
//
// Negative signals (any ⇒ false): the clause carries its OWN work verb from
// reportTagAlongWorkVerbs ("write a report" — F42), or it introduces a NEW
// topic noun absent from the action clause with no output-reference signal
// ("then tell me the weather").
func classifyReportTagAlong(actionClause, reportClause string) bool {
	report := strings.ToLower(reportClause)
	action := strings.ToLower(actionClause)

	hasOutputRef := false
	for _, obj := range reportTagAlongObjects {
		if strings.Contains(report, obj) {
			hasOutputRef = true
			break
		}
	}

	// Negative signal 1: the clause's own work verb means it is a second
	// deliverable (F42), even if the word "report" or a stray reference
	// appears in it.
	for _, verb := range reportTagAlongWorkVerbs {
		if strings.Contains(report, verb) {
			return false
		}
	}

	if hasOutputRef {
		return true
	}

	// Negative signal 2: new topic. Every word the report clause shares
	// with the action clause is excluded; if what remains still names a
	// topic noun (a non-stopword of >= 4 chars) that the action never
	// mentioned, the clause is a fresh request, not a readback ("fix the
	// flaky test, then tell me the weather" — the weather is new).
	stop := newTopicStopwords()
	for _, f := range strings.FieldsFunc(report, func(r rune) bool {
		return !(r >= 'a' && r <= 'z')
	}) {
		if len(f) < 4 || stop[f] || strings.Contains(action, f) {
			continue
		}
		return false
	}
	// No output reference and no new-topic noun: a bare connective tail
	// ("then, please") carries no readback signal either; do not collapse.
	return false
}

// newTopicStopwords lists function words that are never topic nouns. Kept
// minimal; the length >= 4 filter already discards most of them.
func newTopicStopwords() map[string]bool {
	return map[string]bool{
		"tell": true, "show": true, "give": true, "list": true, "print": true,
		"then": true, "also": true, "with": true, "what": true, "where": true,
		"when": true, "please": true, "full": true, "after": true, "this": true,
		"that": true, "them": true, "there": true, "here": true, "back": true,
		"know": true, "about": true, "into": true, "your": true,
	}
}
