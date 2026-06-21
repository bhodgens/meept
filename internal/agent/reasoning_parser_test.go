// Package agent provides tests for reasoning_parser.go.
//
// These tests exercise the spec §7.1 recognition table: tier words, slash
// directives, aliases, token hints, disable phrases, and ambiguous forms.
// Each test case is self-contained and order-independent.
package agent

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestParseReasoningDirective_TierWords covers direct tier+reasoning/thinking
// patterns and the reasoning_effort:X shorthand.
func TestParseReasoningDirective_TierWords(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name      string
		input     string
		wantTier  string
		wantScope string
	}{
		{"use high reasoning", "please use high reasoning for this", llm.ReasoningHigh, "session"},
		{"set reasoning effort to xhigh", "set reasoning effort to xhigh", llm.ReasoningXHigh, "session"},
		{"reasoning: max", "reasoning: max please", llm.ReasoningMax, "session"},
		{"reasoning_effort: low", "reasoning_effort: low", llm.ReasoningLow, "session"},
		{"medium thinking", "use medium thinking", llm.ReasoningMedium, "session"},
		{"none reasoning", "switch to none reasoning now", llm.ReasoningNone, "session"},
		{"thinking=max", "thinking=max for now", llm.ReasoningMax, "session"},
		{"case insensitive", "Use High Reasoning Please", llm.ReasoningHigh, "session"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd == nil {
				t.Fatalf("expected directive, got nil for input %q", tc.input)
			}
			if rd.Config == nil {
				t.Fatalf("expected non-nil Config")
			}
			if rd.Config.Effort != tc.wantTier {
				t.Errorf("Effort: got %q want %q", rd.Config.Effort, tc.wantTier)
			}
			if rd.Scope != tc.wantScope {
				t.Errorf("Scope: got %q want %q", rd.Scope, tc.wantScope)
			}
			if rd.ReasoningReq == "" {
				t.Errorf("ReasoningReq should be populated")
			}
		})
	}
}

// TestParseReasoningDirective_SlashDirective covers the [/reasoning X] form.
func TestParseReasoningDirective_SlashDirective(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input    string
		wantTier string
	}{
		{"[/reasoning high]", llm.ReasoningHigh},
		{"[/reasoning low] please", llm.ReasoningLow},
		{"[/reasoning xhigh]", llm.ReasoningXHigh},
		{"[/REASONING MAX]", llm.ReasoningMax},
		{"ok [/reasoning medium] now", llm.ReasoningMedium},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd == nil || rd.Config == nil {
				t.Fatalf("expected directive with Config, got %#v", rd)
			}
			if rd.Config.Effort != tc.wantTier {
				t.Errorf("Effort: got %q want %q", rd.Config.Effort, tc.wantTier)
			}
		})
	}
}

// TestParseReasoningDirective_Aliases covers phrase-level aliases per §7.1.
func TestParseReasoningDirective_Aliases(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input    string
		wantTier string
	}{
		{"think hard about this", llm.ReasoningHigh},
		{"please think deeply", llm.ReasoningHigh},
		{"extended thinking please", llm.ReasoningHigh},
		{"use extended thinking for this task", llm.ReasoningHigh},
		{"deep think on this one", llm.ReasoningXHigh},
		{"deep reasoning please", llm.ReasoningXHigh},
		{"reason maximally here", llm.ReasoningXHigh},
		{"maximum reasoning please", llm.ReasoningXHigh},
		{"minimal thinking is fine", llm.ReasoningLow},
		{"quick reasoning mode", llm.ReasoningLow},
		{"quick thinking please", llm.ReasoningLow},
		{"light reasoning please", llm.ReasoningLow},
		{"brief thinking please", llm.ReasoningLow},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.input, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd == nil || rd.Config == nil {
				t.Fatalf("expected directive with Config, got %#v", rd)
			}
			if rd.Config.Effort != tc.wantTier {
				t.Errorf("Effort: got %q want %q", rd.Config.Effort, tc.wantTier)
			}
		})
	}
}

// TestParseReasoningDirective_TokenHints covers budget-token directives.
func TestParseReasoningDirective_TokenHints(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name       string
		input      string
		wantBudget int
		wantTier   string
	}{
		{"8000 thinking tokens", "use 8000 thinking tokens", 8000, llm.ReasoningMedium},
		{"4000 reasoning tokens", "use 4000 reasoning tokens", 4000, llm.ReasoningMedium},
		{"reasoning budget 16000", "reasoning budget: 16000", 16000, llm.ReasoningMedium},
		{"thinking budget 4000", "thinking budget: 4000", 4000, llm.ReasoningMedium},
		{"token hint with tier", "use high reasoning with 32000 thinking tokens", 32000, llm.ReasoningHigh},
		{"thinking budget=8000", "thinking budget=8000", 8000, llm.ReasoningMedium},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd == nil || rd.Config == nil {
				t.Fatalf("expected directive with Config, got %#v", rd)
			}
			if rd.Config.BudgetTokens == nil {
				t.Fatalf("expected BudgetTokens to be set")
			}
			if *rd.Config.BudgetTokens != tc.wantBudget {
				t.Errorf("BudgetTokens: got %d want %d", *rd.Config.BudgetTokens, tc.wantBudget)
			}
			if rd.Config.Effort != tc.wantTier {
				t.Errorf("Effort: got %q want %q", rd.Config.Effort, tc.wantTier)
			}
		})
	}
}

// TestParseReasoningDirective_Disable covers "stop thinking" / "no reasoning"
// and the other disable phrases.
func TestParseReasoningDirective_Disable(t *testing.T) {
	t.Parallel()
	cases := []string{
		"stop thinking now",
		"please use no reasoning",
		"disable thinking",
		"disable reasoning for this turn",
		"reasoning off",
		"thinking off for now",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd == nil || rd.Config == nil {
				t.Fatalf("expected directive with Config, got %#v", rd)
			}
			if rd.Config.Effort != llm.ReasoningNone {
				t.Errorf("Effort: got %q want %q", rd.Config.Effort, llm.ReasoningNone)
			}
		})
	}
}

// TestParseReasoningDirective_Ambiguous covers "use reasoning" with no tier.
func TestParseReasoningDirective_Ambiguous(t *testing.T) {
	t.Parallel()
	cases := []string{
		"use reasoning for this",
		"enable reasoning please",
		"turn on thinking",
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd == nil {
				t.Fatalf("expected directive, got nil for %q", input)
			}
			if !rd.Ambiguous {
				t.Errorf("expected Ambiguous=true for %q", input)
			}
			if rd.Config != nil {
				t.Errorf("expected Config=nil for ambiguous directive, got %#v", rd.Config)
			}
		})
	}
}

// TestParseReasoningDirective_Scope covers scope detection.
func TestParseReasoningDirective_Scope(t *testing.T) {
	t.Parallel()
	cases := []struct {
		input       string
		wantScope   string
		description string
	}{
		{"use high reasoning for this task", "task", "explicit task"},
		{"use high reasoning for next turn", "next-turn", "explicit next turn"},
		{"use high reasoning just once", "next-turn", "just once"},
		{"use high reasoning for this turn", "next-turn", "for this turn"},
		{"use high reasoning one time", "next-turn", "one time"},
		{"use high reasoning", "session", "default session"},
		{"[/reasoning high] for this task", "task", "slash with task"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.description, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(tc.input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd == nil {
				t.Fatalf("expected directive, got nil")
			}
			if rd.Scope != tc.wantScope {
				t.Errorf("Scope: got %q want %q (input %q)", rd.Scope, tc.wantScope, tc.input)
			}
		})
	}
}

// TestParseReasoningDirective_Negative covers inputs that should NOT match.
func TestParseReasoningDirective_Negative(t *testing.T) {
	t.Parallel()
	cases := []string{
		"",
		"   ",
		"hello world",
		"what's the weather like",
		"please refactor this function",
		"write me a poem about cats",
		"low battery warning",          // "low" alone isn't a directive
		"high performance computing",   // "high" alone isn't a directive
		"max throughput please",        // "max" alone isn't a directive
		"the thinking person's guide",  // "thinking" without tier
		"reasoning is important",       // "reasoning" without tier
		"how long does it take",        // sounds like a budget phrase but no
		"quick brown fox",              // "quick" alone is ambiguous
		"use the reasoning endpoint",   // "use reasoning-ish" but "endpoint"
	}
	for _, input := range cases {
		input := input
		t.Run(input, func(t *testing.T) {
			t.Parallel()
			rd, err := ParseReasoningDirective(input)
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if rd != nil {
				t.Errorf("expected nil directive for %q, got %#v", input, rd)
			}
		})
	}
}

// TestParseReasoningDirective_EmptyInput verifies empty/whitespace input is
// safe.
func TestParseReasoningDirective_EmptyInput(t *testing.T) {
	t.Parallel()
	rd, err := ParseReasoningDirective("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rd != nil {
		t.Errorf("expected nil for empty input, got %#v", rd)
	}
}

// TestParseReasoningDirective_ReasoningReqPopulated verifies that the matched
// substring is returned for logging / clarification prompts.
func TestParseReasoningDirective_ReasoningReqPopulated(t *testing.T) {
	t.Parallel()
	rd, err := ParseReasoningDirective("please use high reasoning now")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rd == nil {
		t.Fatalf("expected directive")
	}
	if rd.ReasoningReq == "" {
		t.Errorf("expected ReasoningReq to be non-empty")
	}
}
