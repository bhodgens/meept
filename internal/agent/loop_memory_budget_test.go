package agent

import (
	"strings"
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// memoryItem is a single "- " bullet line as produced by RunWithTask's
// context assembly ("\\n"-terminated), before budgeting.
func memoryItem(id string, words int) string {
	var b strings.Builder
	b.WriteString("- memory-" + id + ": ")
	for i := 0; i < words; i++ {
		b.WriteString("word ")
	}
	return b.String()
}

// fitBudgetCase asserts the shared post-conditions of the whole-item budget
// path: kept items verbatim in order, dropped items fully absent, and the
// estimated token count within budget.
func fitBudgetCase(t *testing.T, got string, budgetTokens int, kept []string, dropped []string) {
	t.Helper()

	for _, item := range kept {
		if !strings.Contains(got, item) {
			t.Errorf("kept item missing from context:\n  want: %q\n  got: %q", item, got)
		}
	}
	for _, item := range dropped {
		if strings.Contains(got, item) {
			t.Errorf("dropped item present in context:\n  not-want: %q\n  got: %q", item, got)
		}
		// A whole-item drop must never leave a partial fragment: the item's
		// unique prefix marker must not appear at all.
		marker := "- memory-" + item[9:10] + ":"
		if strings.Contains(got, marker) {
			t.Errorf("partial fragment of dropped item present (marker %q in context)", marker)
		}
	}
	if est := llm.EstimateTokenCountHeuristic(strings.TrimSuffix(got, memoryBudgetTruncatedMarker)); est > budgetTokens {
		t.Errorf("estimated tokens %d exceed budget %d", est, budgetTokens)
	}
}

// joinBudgeted rejoins the structured items returned by
// EnforceMemoryItemBudget into the context string.
func joinBudgeted(items []string) string {
	return strings.Join(items, "\n")
}

// TestMemoryBudgetDropsWholeItems verifies that when the assembled memory
// context exceeds MaxMemoryContextTokens, whole lowest-ranked items are
// dropped in reverse relevance order until the estimate fits, and items that
// fit are kept verbatim (no mid-string proportional cut).
func TestMemoryBudgetDropsWholeItems(t *testing.T) {
	// 5 items; items 1-3 fit and item 4 pushes over the budget.
	// Each item is 512 chars (~171 tokens); 5 items + separators ~853 tokens.
	// Use a budget that fits exactly 3 whole items.
	const itemWords = 100 // "- memory-X: " + 100 words = 512 chars ≈ 171 tokens each
	items := []string{
		memoryItem("a", itemWords),
		memoryItem("b", itemWords),
		memoryItem("c", itemWords),
		memoryItem("d", itemWords),
		memoryItem("e", itemWords),
	}
	joined := strings.Join(items, "\n")

	// Budget: 3 items + headroom for the truncated marker (~29 chars ≈ 10
	// tokens), but less than 4 items would need.
	budget := llm.EstimateTokenCountHeuristic(items[0]+items[1]+items[2]) + 9
	if budget >= llm.EstimateTokenCountHeuristic(joined) {
		t.Fatalf("test setup: budget %d does not pressure the full context", budget)
	}

	got := joinBudgeted(EnforceMemoryItemBudget(strings.Split(joined, "\n"), budget))
	fitBudgetCase(t, got, budget,
		[]string{items[0], items[1], items[2]},
		[]string{items[3], items[4]},
	)

	// The observability suffix must be appended when anything was dropped.
	if !strings.Contains(got, "...[memory truncated due to token budget]...") {
		t.Errorf("truncated-marker suffix missing after dropping items:\n%s", got)
	}

	// Kept items must stay in relevance order (1 before 2 before 3).
	i1 := strings.Index(got, items[0])
	i2 := strings.Index(got, items[1])
	i3 := strings.Index(got, items[2])
	if i1 < 0 || i2 < 0 || i3 < 0 || !(i1 < i2 && i2 < i3) {
		t.Errorf("kept items not in relevance order: idx(%d,%d,%d)", i1, i2, i3)
	}
}

// TestMemoryBudgetKeepsEverythingWhenFitting verifies the no-drop path: a
// context within budget is returned unchanged.
func TestMemoryBudgetKeepsEverythingWhenFitting(t *testing.T) {
	items := []string{
		memoryItem("a", 20),
		memoryItem("b", 20),
		memoryItem("c", 20),
	}
	joined := strings.Join(items, "\n")
	budget := llm.EstimateTokenCountHeuristic(joined) + 5

	got := joinBudgeted(EnforceMemoryItemBudget(strings.Split(joined, "\n"), budget))
	if got != joined {
		t.Errorf("fitting context was modified:\n  want: %q\n  got: %q", joined, got)
	}
	if strings.Contains(got, "...[memory truncated due to token budget]...") {
		t.Error("truncated marker present although nothing was dropped")
	}
}

// TestSplitMemoryItemsMultiLine verifies that multi-line memory items stay
// glued together: a continuation line inside an item (even one starting with
// "- ") must not split the item, or a whole-item drop would orphan content.
func TestSplitMemoryItemsMultiLine(t *testing.T) {
	context := strings.Join([]string{
		"- memory-a: first item, line two of the same item",
		"- memory-b: second item",
		"\n\n...[memory truncated due to token budget]...",
	}, "\n")

	got := SplitMemoryItems(context)
	want := []string{
		"- memory-a: first item, line two of the same item",
		"- memory-b: second item\n\n\n...[memory truncated due to token budget]...",
	}
	if len(got) != len(want) {
		t.Fatalf("split into %d items, want %d: %#v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("item %d:\n  want: %q\n  got:  %q", i, want[i], got[i])
		}
	}
}

// TestSplitMemoryItemsSingle verifies the degenerate single-item shape.
func TestSplitMemoryItemsSingle(t *testing.T) {
	one := "- memory-only: just one item"
	if got := SplitMemoryItems(one); len(got) != 1 || got[0] != one {
		t.Errorf("single-item split wrong: %#v", got)
	}
	if got := SplitMemoryItems(""); len(got) != 0 && !(len(got) == 1 && got[0] == "") {
		t.Errorf("empty context should split to at most one empty item, got %#v", got)
	}
}

// TestMemoryBudgetOversizedSingleItemTruncates verifies the last-resort path:
// when a SINGLE item alone exceeds the budget, its existing 300-rune
// per-item truncation (formatMemoryForPrompt) fires, not a whole-item drop
// (which would leave an empty context).
func TestMemoryBudgetOversizedSingleItemTruncates(t *testing.T) {
	// A single item alone exceeding the budget: whole-item drop would empty
	// the context, so the item must be KEPT (its existing 300-rune per-item
	// truncation, applied upstream by formatMemoryForPrompt, is the item-
	// shaped last resort). Assert the enforcement keeps a single oversized
	// item rather than dropping it, and appends the observability marker.
	big := memoryItem("big", 5000) // ~25000 chars ≈ 8334 tokens
	budget := 100

	got := joinBudgeted(EnforceMemoryItemBudget([]string{big}, budget))
	if !strings.HasPrefix(got, big) {
		t.Errorf("single oversized item was altered or dropped (len got %d, want %d)", len(got), len(big))
	}
	if !strings.HasSuffix(got, memoryBudgetTruncatedMarker) {
		t.Error("truncated-marker suffix missing after keeping oversized item over budget")
	}
}

// TestMemoryBudgetOversizedFirstItemKeepsOnlyIt pins the MULTI-item
// degenerate path (bughunt M6): when the FIRST (highest-ranked) item alone
// exceeds the budget, ONLY that single item is kept — never the whole list,
// which would bypass the budget while still appending the truncation marker.
func TestMemoryBudgetOversizedFirstItemKeepsOnlyIt(t *testing.T) {
	big := memoryItem("big", 5000) // ~25000 chars ≈ 8334 tokens, alone over budget
	small1 := memoryItem("s1", 20)
	small2 := memoryItem("s2", 20)
	budget := 100

	got := EnforceMemoryItemBudget([]string{big, small1, small2}, budget)

	// Bounded: exactly the oversized item plus the marker — not all three.
	if len(got) != 2 {
		t.Fatalf("result item count = %d, want 2 (oversized item + marker): %#v", len(got), got)
	}
	if got[0] != big {
		t.Errorf("top-ranked oversized item not kept verbatim:\n got %q", got[0])
	}
	if got[1] != memoryBudgetTruncatedMarker {
		t.Errorf("truncated marker missing or not last: %q", got[1])
	}
	joined := joinBudgeted(got)
	if strings.Contains(joined, small1) || strings.Contains(joined, small2) {
		t.Errorf("items that did not fit are present — budget bypassed:\n%s", joined)
	}
	if n := strings.Count(joined, "- memory-"); n != 1 {
		t.Errorf("kept %d memory items, want exactly 1", n)
	}
}
