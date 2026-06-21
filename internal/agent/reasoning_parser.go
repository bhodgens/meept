// Package agent provides the reasoning-effort natural-language parser.
//
// This file implements ParseReasoningDirective per spec §7 of the LLM
// Reasoning Effort design. It scans user input for directives that adjust
// the per-request reasoning/thinking tier (e.g. "use high reasoning",
// "[/reasoning xhigh]", "think hard", "use 8000 thinking tokens") and
// returns a ReasoningDirective describing the parsed configuration.
package agent

import (
	"errors"
	"regexp"
	"strconv"
	"strings"

	"github.com/caimlas/meept/internal/llm"
)

// ReasoningDirective captures a parsed reasoning-effort directive from user
// text. Returned by ParseReasoningDirective.
type ReasoningDirective struct {
	// Config is the parsed ReasoningConfig. Nil when Ambiguous is true and
	// the parser couldn't determine a tier (caller decides fallback).
	Config *llm.ReasoningConfig

	// Scope is one of "session" (default), "next-turn", or "task".
	Scope string

	// Ambiguous is true when the user wrote "use reasoning" / "enable
	// reasoning" without specifying a tier. In that case Config is nil and
	// the caller is expected to either pick a sensible default or surface a
	// clarifying question.
	Ambiguous bool

	// ReasoningReq is the substring of the user input that matched the
	// directive. Useful for logging and clarification prompts.
	ReasoningReq string
}

// All tier names recognized by the parser, in alphabetical order for the
// regex alternation. Order doesn't affect matching — the regex captures
// whichever tier word actually appears.
const parserTierAlternation = `none|low|medium|high|xhigh|max`

// Compiled matchers. Each returns the captured tier (when applicable) and
// the matched substring. Patterns are case-insensitive throughout.
var (
	// Tier word + "reasoning"/"thinking" in either order.
	//   "use high reasoning"            -> high
	//   "set reasoning effort to xhigh" -> xhigh
	//   "reasoning: max"                -> max
	//   "medium thinking"               -> medium
	// We capture the tier word from whichever side it appears. The middle
	// alternation accepts optional "effort" + "to" connectors so phrases
	// like "set reasoning effort to xhigh" match cleanly.
	reasoningTierRe = regexp.MustCompile(
		`(?i)\b(` + parserTierAlternation + `)\s+(?:reasoning|thinking)\b` +
			`|\breasoning(?:_effort)?\s*(?:effort\s*)?[:=]?\s*(?:to\s+)?(` + parserTierAlternation + `)\b` +
			`|\bthinking\s*(?:effort\s*)?[:=]?\s*(?:to\s+)?(` + parserTierAlternation + `)\b`,
	)

	// Slash directive: [/reasoning high], [/reasoning low]
	slashDirectiveRe = regexp.MustCompile(
		`(?i)\[/reasoning\s+(` + parserTierAlternation + `)\]`,
	)

	// Disable: "stop thinking", "no reasoning", "disable thinking/reasoning"
	disableRe = regexp.MustCompile(
		`(?i)\b(?:stop\s+thinking|no\s+reasoning|disable\s+(?:thinking|reasoning)|reasoning\s+off|thinking\s+off)\b`,
	)

	// Token hints. Two shapes:
	//   "use 8000 thinking tokens" / "use 8000 reasoning tokens"
	//   "reasoning budget: 4000" / "thinking budget: 4000"
	tokenHintRe = regexp.MustCompile(
		`(?i)(\d+)\s+(?:thinking|reasoning)\s+tokens?|(?:(?:reasoning|thinking)\s+budget)\s*[:=]?\s*(\d+)`,
	)

	// Ambiguous: "use reasoning" / "enable reasoning" without any tier.
	// Must be matched AFTER tier-specific patterns fail, otherwise it
	// shadows "use high reasoning".
	ambiguousRe = regexp.MustCompile(`(?i)\b(?:use|enable|turn\s+on)\s+(?:extended\s+)?(?:reasoning|thinking)\b`)

	// Alias phrases. Longest first so "deep think" doesn't get shadowed by
	// "think" alone. Each maps to a specific tier.
	// Ordered list; first match wins.
	aliasPhrases = []struct {
		re    *regexp.Regexp
		tier  string
		alias string
	}{
		// xhigh aliases
		{
			re:    regexp.MustCompile(`(?i)\b(?:deep\s+think|deep\s+reasoning|reason\s+maximally|maximum\s+reasoning)\b`),
			tier:  llm.ReasoningXHigh,
			alias: "deep think/deep reasoning",
		},
		// high aliases
		{
			re:    regexp.MustCompile(`(?i)\b(?:think\s+hard|think\s+deeply|extended\s+thinking|reason\s+hard|heavy\s+reasoning)\b`),
			tier:  llm.ReasoningHigh,
			alias: "think hard/extended thinking",
		},
		// low aliases. "quick" alone is ambiguous (common English word), so
		// we only match "quick" when it's followed by reasoning/thinking
		// context.
		{
			re:    regexp.MustCompile(`(?i)\b(?:minimal\s+thinking|quick\s+(?:reasoning|thinking)|light\s+reasoning|brief\s+thinking)\b`),
			tier:  llm.ReasoningLow,
			alias: "minimal thinking/quick reasoning",
		},
	}

	// Scope detection (run after the directive itself is identified so we
	// have the full text available).
	scopeTaskRe     = regexp.MustCompile(`(?i)\bfor\s+this\s+task\b|\bonce\s+for\s+the\s+task\b`)
	scopeNextTurnRe = regexp.MustCompile(`(?i)\b(?:for\s+next\s+turn|just\s+once|for\s+this\s+turn|one\s+time)\b`)
)

// ParseReasoningDirective scans text for reasoning-effort directives per
// spec §7.1. Returns (nil, nil) when no directive is found.
//
// The first matching pattern wins; the parser does not accumulate multiple
// directives in a single call. Recognized forms (case-insensitive):
//
//   - Tier word + "reasoning"/"thinking": "use high reasoning"
//   - "reasoning_effort: X": "reasoning_effort: low"
//   - "[/reasoning X]" slash directive
//   - Aliases: "think hard" -> high, "deep think" -> xhigh, etc.
//   - Token hints: "use 8000 thinking tokens" -> BudgetTokens=8000
//   - Disable phrases: "stop thinking", "no reasoning"
//
// Ambiguous matches ("use reasoning" with no tier) set Ambiguous=true and
// leave Config nil.
//
// Scope detection looks for "for this task" (scope=task), "for next turn"
// (scope=next-turn); otherwise scope=session.
func ParseReasoningDirective(text string) (*ReasoningDirective, error) {
	if strings.TrimSpace(text) == "" {
		return nil, nil
	}

	// Order matters: more-specific patterns first. Token hints are checked
	// before tier-only matches because "use 8000 thinking tokens" contains
	// "thinking" which the tier regex could partially absorb.

	// 1. Slash directive ([/reasoning high])
	if m := slashDirectiveRe.FindStringSubmatch(text); m != nil {
		tier := strings.ToLower(m[1])
		return &ReasoningDirective{
			Config:       &llm.ReasoningConfig{Effort: tier},
			Scope:        detectScope(text),
			ReasoningReq: m[0],
		}, nil
	}

	// 2. Disable phrases ("stop thinking", "no reasoning")
	if m := disableRe.FindStringSubmatch(text); m != nil {
		return &ReasoningDirective{
			Config:       &llm.ReasoningConfig{Effort: llm.ReasoningNone},
			Scope:        detectScope(text),
			ReasoningReq: m[0],
		}, nil
	}

	// 3. Token hints ("use 8000 thinking tokens", "reasoning budget: 4000")
	if m := tokenHintRe.FindStringSubmatch(text); m != nil {
		var raw string
		if m[1] != "" {
			raw = m[1]
		} else {
			raw = m[2]
		}
		n, err := strconv.Atoi(raw)
		if err != nil {
			return nil, errors.New("reasoning: failed to parse token hint count: " + raw)
		}
		// When a token hint is present without an explicit tier word, we
		// still need an effort tier to drive the wire-format translation.
		// Default to "medium" — the spec's middle tier — which pairs
		// naturally with a user-supplied budget.
		effort := llm.ReasoningMedium
		// If a tier word also appears in the text, honor it.
		if tier := findTierWord(text); tier != "" {
			effort = tier
		}
		budget := n
		return &ReasoningDirective{
			Config: &llm.ReasoningConfig{
				Effort:       effort,
				BudgetTokens: &budget,
			},
			Scope:        detectScope(text),
			ReasoningReq: m[0],
		}, nil
	}

	// 4. Alias phrases ("think hard", "deep think", ...)
	for _, alias := range aliasPhrases {
		if m := alias.re.FindStringSubmatch(text); m != nil {
			return &ReasoningDirective{
				Config: &llm.ReasoningConfig{
					Effort:  alias.tier,
					Enabled: boolPtr(true),
				},
				Scope:        detectScope(text),
				ReasoningReq: m[0],
			}, nil
		}
	}

	// 5. Tier word + reasoning/thinking ("use high reasoning", "reasoning: max")
	if tier := findTierWord(text); tier != "" {
		// Confirm the tier word actually co-occurs with reasoning/thinking
		// context, otherwise we'd hijack ordinary sentences ("low" alone
		// isn't a directive). findTierWord already enforces this.
		return &ReasoningDirective{
			Config:       &llm.ReasoningConfig{Effort: tier},
			Scope:        detectScope(text),
			ReasoningReq: reasoningTierRe.FindString(text),
		}, nil
	}

	// 6. Ambiguous ("use reasoning" with no tier)
	if m := ambiguousRe.FindStringSubmatch(text); m != nil {
		return &ReasoningDirective{
			Config:       nil,
			Scope:        detectScope(text),
			Ambiguous:    true,
			ReasoningReq: m[0],
		}, nil
	}

	return nil, nil
}

// findTierWord returns the lowercased tier word from reasoningTierRe when the
// text contains a tier+reasoning/thinking co-occurrence. Empty otherwise.
func findTierWord(text string) string {
	m := reasoningTierRe.FindStringSubmatch(text)
	if m == nil {
		return ""
	}
	// reasoningTierRe has three alternative capture groups (one per branch
	// of the alternation). Exactly one will be populated.
	for _, g := range m[1:] {
		if g != "" {
			return strings.ToLower(g)
		}
	}
	return ""
}

// detectScope scans for scope keywords and returns the scope identifier. The
// default is "session".
func detectScope(text string) string {
	if scopeTaskRe.MatchString(text) {
		return "task"
	}
	if scopeNextTurnRe.MatchString(text) {
		return "next-turn"
	}
	return "session"
}

// boolPtr returns a pointer to b. Small helper to keep ReasoningConfig
// construction concise.
func boolPtr(b bool) *bool { return &b }
