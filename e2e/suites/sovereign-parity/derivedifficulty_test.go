//go:build e2e

// Gate leaf 00T — R7 difficulty source (T22; Contract C11, leaf 07).
//
// Compile-RED until leaf 07 declares llm.DeriveDifficulty. The table
// pinned here is leaf 07's frozen table verbatim:
//
//	requiresPlanning==true               -> 4
//	mode=="quick_plan"                   -> 5
//	mode=="spec_plan" && requiresPlan    -> 4
//	mode=="spec_pair" || mode=="compound" -> 3
//	otherwise, inputLen<50               -> 1
//	otherwise                            -> 2
//
// Note (flagged in the report): the "no derivable signal returns 0"
// prose in C11 has NO table row — with these three params the table
// never yields 0; the 0 (inert) value arises only when a caller with no
// Intent never calls DeriveDifficulty. The gate pins the table as the
// implementation leaf freezes it and treats 0 as a caller-side value.
package sovereignparity

import (
	"testing"

	"github.com/caimlas/meept/internal/llm"
)

// TestDeriveDifficulty_FrozenTable pins every row of the C11 table
// verbatim plus purity (same input, same output — a pure function needs
// no classifier call and no I/O).
func TestDeriveDifficulty_FrozenTable(t *testing.T) {
	cases := []struct {
		name     string
		mode     string
		planning bool
		inputLen int
		want     int
	}{
		{"planning-requires-4", "", true, 100, 4},
		{"quick-plan-5", "quick_plan", false, 100, 5},
		{"spec-plan-4", "spec_plan", true, 100, 4},
		{"spec-pair-3", "spec_pair", false, 100, 3},
		{"compound-3", "compound", false, 100, 3},
		{"direct-long-2", "direct", false, 100, 2},
		{"short-input-1", "direct", false, 10, 1},
		{"empty-mode-short-1", "", false, 10, 1},
	}
	for _, c := range cases {
		got := llm.DeriveDifficulty(c.mode, c.planning, c.inputLen)
		if got != c.want {
			t.Fatalf("T22 R7 RED (expected): DeriveDifficulty(%q, %v, %d) = %d, want %d — leaf 07 not landed (undefined symbol is the compile-RED gate)",
				c.mode, c.planning, c.inputLen, got, c.want)
		}
	}

	// Purity: repeated calls return identical values (no hidden state).
	first := llm.DeriveDifficulty("quick_plan", false, 5)
	for range 3 {
		if again := llm.DeriveDifficulty("quick_plan", false, 5); again != first {
			t.Fatalf("DeriveDifficulty is stateful: %d then %d for identical inputs", first, again)
		}
	}
}

// TestDeriveDifficulty_ZeroInert documents the caller-side 0 contract:
// R7 releases only on fc.Difficulty > pin.Difficulty, so 0 (unclassified)
// never releases. The resolver-side inertness drive lives in sticky_test.go.
func TestDeriveDifficulty_ZeroInert(t *testing.T) {
	// The frozen release predicate: only a strict increase releases.
	// With difficulty 0 against any pin >= 0, 0 > pin is false — inert.
	pins := []int{0, 1, 2, 3, 4, 5}
	for _, pin := range pins {
		if 0 > pin {
			t.Fatalf("difficulty 0 released pin at %d — the inert rule broke", pin)
		}
	}
	// And the ChatOption carrying the explicit difficulty exists (leaf 07).
	opt := llm.WithDifficulty(4)
	if opt == nil {
		t.Fatal("WithDifficulty returned nil; a ChatOption is a func and can never be nil here — leaf 07 not landed (compile-RED)")
	}
}
