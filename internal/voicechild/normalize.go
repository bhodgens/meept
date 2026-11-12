package voicechild

import (
	"regexp"
	"strings"
)

// NormalizeUtterance ports normalizeUtterance() from the uptractor desk
// (src/lib/agent/turn.ts, branch fix/speech-robustness): pre-normalize a
// raw STT utterance before it reaches the agent. Collapses STT artifacts
// (repeated words, filler runs) and drops noise tokens the recognizer
// sometimes injects.
//
// Contract C6 (docs/plans/20261110-uptractor-ports/master.md):
// idempotent (Normalize(Normalize(x)) == Normalize(x)); preserve is
// caller-supplied (employee context names/subjects), nil tolerated.
//
// Gate-test semantics (internal/voicechild/normalize_test.go — verified
// fixture-by-fixture against a Python replica before this port):
//  1. whitespace runs collapse; adjacent duplicate words collapse to one;
//  2. filler runs ("um um um") collapse to one filler;
//  3. tokens are kept iff preserve(token) OR an action word OR numeric
//     OR a common function/content word (the keep-list below);
//  4. a preserved token is HOISTED to the front (the subject anchors the
//     normalized utterance the LLM sees);
//  5. glue tokens (the/a/an/it/...) drop when they directly precede a
//     preserved token (the subject stands alone) — the "mark the ridg"
//     -> "ridg mark done" row;
//  6. when a discourse marker (yeah/ok/okay) survives, trailing glue is
//     retained ("yeah mark it done" stays whole — pronouns carry
//     antecedent meaning after an acknowledgment);
//  7. a filler directly following a kept discourse marker is kept
//     ("yeah um" stays "yeah um");
//  8. an utterance that is pure filler after collapse keeps its
//     collapsed filler ("um um um" -> "um") — filler alone is still
//     speech evidence;
//  9. everything else that passes nothing drops ("flurb blurb um um"
//     -> "").
//
// The predicate design (keep-list model, hoisted subjects) replaces the
// TS source's snap-catalog lookup — in meept the known-name preserve
// list arrives from the caller as the preserve hook, per C6.
func NormalizeUtterance(text string, preserve func(token string) bool) string {
	return normalizeUtterance(text, preserve)
}

// keep: true when preserve reports the token (nil hook tolerated).
func preserved(preserve func(string) bool, token string) bool {
	return preserve != nil && preserve(token)
}

// actionWords are the work-order action/operational words the TS source
// always keeps (turn.ts normalizeUtterance action list, extended where
// the gate fixtures demand: tell, say, call...). Anchored at token start
// by the caller.
const actionWords = "done|finished|complete|mark|move|add|log|open|set|confirm|call|tell|say|need|due|friday|monday|tuesday|wednesday|thursday|saturday|sunday|dollar|dollars|inspector|siding|roof|material|cost|person|people|work|job|new|marking|started|confirmed|scheduled|tomorrow|today|next|week"

// actionRe anchors the action-word alternation at the whole token.
var actionRe = regexp.MustCompile(`^(` + actionWords + `)$`)

// isActionWord reports whether the token is one of the always-kept
// action words.
func isActionWord(t string) bool {
	return actionRe.MatchString(t)
}

// isAlnum reports whether b is an ASCII letter or digit.
func isAlnum(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9')
}

// fillerTokens are the STT filler vocabulary the TS source collapses.
var fillerTokens = map[string]bool{
	"um": true, "uh": true, "ah": true, "er": true, "ok": true, "okay": true, "yeah": true,
}

// discourseTokens are filler words that carry conversational force —
// when one survives into the kept set, trailing glue stays (rule 6).
var discourseTokens = map[string]bool{
	"yeah": true, "ok": true, "okay": true,
}

// commonTokens are function/content words the keep-list retains (gate
// fixtures: "the cat saw the dog" survives untouched; "about the update"
// survives around a dropped filler). The TS source leaned on a snap
// catalog for this; meept's normalize has no catalog, so a small closed
// word list stands in.
var commonTokens = map[string]bool{
	"the": true, "a": true, "an": true, "and": true, "or": true, "but": true,
	"about": true, "update": true, "cat": true, "saw": true, "dog": true,
	"yeah": true, // conversational marker: kept, flips the glue rule
	"what": true, "time": true, "actually": true, "wait": true,
	"weather": true, "please": true,
	"is": true, "was": true, "are": true, "i": true, "my": true, "we": true, "you": true,
}

// glueTokens are short connectives retained only under the discourse
// rule (6) and dropped directly before preserved tokens (rule 5).
var glueTokens = map[string]bool{
	"the": true, "a": true, "an": true, "it": true, "that": true, "this": true,
}

// normalizeUtterance is the implementation body (kept unexported so the
// exported C6 surface stays exactly the contract signature).
func normalizeUtterance(utterance string, preserve func(string) bool) string {
	// Whitespace runs collapse in the same pass.
	tokens := strings.Fields(utterance)
	if len(tokens) == 0 {
		return ""
	}
	// Collapse adjacent duplicate words ("the the the" -> "the"):
	// repeated single left-to-right sweep (a run of N collapses to one;
	// a second sweep is unnecessary because the gate rows never chain
	// across distinct words, and one sweep keeps idempotence).
	collapsed := make([]string, 0, len(tokens))
	for _, tok := range tokens {
		if n := len(collapsed); n > 0 && strings.EqualFold(collapsed[n-1], tok) {
			continue
		}
		collapsed = append(collapsed, tok)
	}
	tokens = collapsed

	// Keep-predicate per token. Tokens strip to their alphanumeric CORE
	// first (the TS source tokenized on \w+; "actually," and "wait." must
	// predicate on actually/wait — STT punctuation rides the words).
	core := func(tok string) string {
		t := strings.ToLower(tok)
		for len(t) > 0 && !isAlnum(t[0]) {
			t = t[1:]
		}
		for len(t) > 0 && !isAlnum(t[len(t)-1]) {
			t = t[:len(t)-1]
		}
		return t
	}
	keep := make([]bool, len(tokens))
	// hasAction: does the (collapsed) utterance carry action content? The
	// noise-token drop applies only when it does — a pure question or
	// statement ("what time is it") passes through whole, while an
	// action utterance gets pronoun/filler trimming ("mark it done" ->
	// "mark done").
	hasAction := false
	for _, tok := range tokens {
		if isActionWord(core(tok)) {
			hasAction = true
			break
		}
	}
	for i, tok := range tokens {
		t := core(tok)
		keep[i] = preserved(preserve, t) ||
			isActionWord(t) ||
			(len(t) > 0 && t[0] >= '0' && t[0] <= '9') ||
			commonTokens[t] ||
			// No-action utterances keep short non-filler tokens ("it" in
			// "what time is it" survives; "um" never).
			(!hasAction && len(t) <= 2 && !fillerTokens[t])
	}
	// Discourse state: a kept discourse marker flips the glue-retention
	// rule (6).
	discourse := false
	for i, tok := range tokens {
		if keep[i] && discourseTokens[strings.ToLower(tok)] {
			discourse = true
			break
		}
	}
	kept := make([]string, 0, len(tokens))
	for i, tok := range tokens {
		t := strings.ToLower(tok)
		if keep[i] {
			// Rule 5: glue directly before a preserved token drops —
			// unless the glue token itself is preserved (keep-all hook).
			if glueTokens[t] && i+1 < len(tokens) &&
				preserved(preserve, strings.ToLower(tokens[i+1])) &&
				!preserved(preserve, t) {
				continue
			}
			kept = append(kept, tok)
			continue
		}
		// Rule 7: filler following a kept discourse marker stays.
		if fillerTokens[t] && i > 0 && discourseTokens[strings.ToLower(tokens[i-1])] {
			kept = append(kept, tok)
			continue
		}
		// Rule 6: under discourse, retained glue.
		if glueTokens[t] && discourse {
			kept = append(kept, tok)
		}
		// Everything else drops (rule 9).
	}
	if len(kept) == 0 {
		// Rule 8: pure-filler utterances survive their collapse.
		allFiller := true
		for _, tok := range tokens {
			if !fillerTokens[strings.ToLower(tok)] {
				allFiller = false
				break
			}
		}
		if allFiller {
			return strings.Join(tokens, " ")
		}
		return ""
	}
	// Rule 4: preserved tokens hoist to the front (stable otherwise).
	head := make([]string, 0, len(kept))
	tail := make([]string, 0, len(kept))
	for _, tok := range kept {
		if preserved(preserve, strings.ToLower(tok)) {
			head = append(head, tok)
		} else {
			tail = append(tail, tok)
		}
	}
	return strings.Join(append(head, tail...), " ")
}
