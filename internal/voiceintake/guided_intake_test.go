package voiceintake

// Gate tests for guided intake (leaf 05, master "Guided intake" matrix
// row). Package internal/voiceintake is NEW — internal/intake is TAKEN by
// the GitHub intake-factory and must never be touched.
//
// Contracts (leaf 05, quoted):
//
//	ResolveSubject(query string, candidates []string, threshold
//	float64) (outcome ResolveOutcome, match string, matches []string)
//	with ResolveOutcome in {ResolveExact, ResolveFuzzySingle,
//	ResolveAmbiguous, ResolveNoMatch}; threshold 0.60.
//
// Test rows per master T8: ambiguous fuzzy (two candidates >= threshold)
// => refuse result listing both; single match => proceed; save blocked
// until explicit confirm flag.
//
// RED until leaf 05 (package internal/voiceintake has no non-test files;
// every symbol below is undefined).

import (
	"testing"
)

// TestIntakeResolveAmbiguousListsBoth pins the refusal rule: two
// candidates above threshold must NEVER auto-pick — the outcome is
// ResolveAmbiguous and BOTH candidates ride the matches list so the agent
// asks the caller.
func TestIntakeResolveAmbiguousListsBoth(t *testing.T) {
	outcome, match, matches := ResolveSubject("meadow ridg e",
		[]string{"Meadow Ridge", "Meadow Brook"}, 0.60)
	if outcome != ResolveAmbiguous {
		t.Fatalf("outcome = %d, want ResolveAmbiguous (refuse, never auto-pick)", outcome)
	}
	if match != "" {
		t.Errorf("ambiguous resolve returned a pick %q — auto-picking is forbidden", match)
	}
	if len(matches) != 2 {
		t.Fatalf("matches = %v, want both candidates listed", matches)
	}
	seen := map[string]bool{}
	for _, m := range matches {
		seen[m] = true
	}
	if !seen["Meadow Ridge"] || !seen["Meadow Brook"] {
		t.Errorf("matches = %v, want [Meadow Ridge Meadow Brook]", matches)
	}
}

// TestIntakeResolveSingleMatchProceeds pins the proceed path: one
// candidate above threshold resolves as ResolveFuzzySingle with that
// candidate as match (the "did you mean X?" propose-confirmation case).
func TestIntakeResolveSingleMatchProceeds(t *testing.T) {
	outcome, match, matches := ResolveSubject("meadow rich",
		[]string{"Meadow Ridge", "Dakota Drywall"}, 0.60)
	if outcome != ResolveFuzzySingle {
		t.Fatalf("outcome = %d, want ResolveFuzzySingle", outcome)
	}
	if match != "Meadow Ridge" {
		t.Errorf("match = %q, want Meadow Ridge", match)
	}
	if len(matches) != 1 || matches[0] != "Meadow Ridge" {
		t.Errorf("matches = %v, want exactly [Meadow Ridge]", matches)
	}
}

// TestIntakeResolveExact pins exact match: a query equal (case-/
// spacing-insensitive) to one candidate resolves ResolveExact with the
// canonical candidate name — never routed through fuzzy scoring.
func TestIntakeResolveExact(t *testing.T) {
	outcome, match, matches := ResolveSubject("Oak Street",
		[]string{"Oak Street", "Oak Court"}, 0.60)
	if outcome != ResolveExact {
		t.Fatalf("outcome = %d, want ResolveExact", outcome)
	}
	if match != "Oak Street" {
		t.Errorf("match = %q, want Oak Street", match)
	}
	if len(matches) != 0 {
		t.Errorf("exact resolve should carry no fuzzy candidate list, got %v", matches)
	}
}

// TestIntakeResolveNoMatch pins the below-threshold case:
// ResolveNoMatch with no pick and no candidate list (the offer
// create-new path).
func TestIntakeResolveNoMatch(t *testing.T) {
	// Canonical no-match vector: "summer planes" matches nothing above
	// 0.60 (audit R5 wording: planes, not plans).
	outcome, match, matches := ResolveSubject("summer planes",
		[]string{"Meadow Ridge", "Oak Street"}, 0.60)
	if outcome != ResolveNoMatch {
		t.Fatalf("outcome = %d, want ResolveNoMatch", outcome)
	}
	if match != "" || len(matches) != 0 {
		t.Errorf("no-match resolve carried pick %q / list %v, want empty", match, matches)
	}
}

// TestIntakeResolveEmpty pins degenerate inputs: empty query or empty
// candidate list is ResolveNoMatch, never a panic.
func TestIntakeResolveEmpty(t *testing.T) {
	if o, _, _ := ResolveSubject("", []string{"Meadow Ridge"}, 0.60); o != ResolveNoMatch {
		t.Errorf("empty query outcome = %d, want ResolveNoMatch", o)
	}
	if o, _, _ := ResolveSubject("Meadow Ridge", nil, 0.60); o != ResolveNoMatch {
		t.Errorf("no candidates outcome = %d, want ResolveNoMatch", o)
	}
}

// captureSaver records intake_save payloads so the confirm-gate tests can
// assert what would have been filed.
type captureSaver struct {
	saves []string // kind:subject in Append order
}

func (c *captureSaver) save(kind, subject string) error {
	c.saves = append(c.saves, kind+":"+subject)
	return nil
}

// TestIntakeSaveBlockedWithoutConfirm pins the hard gate: intake_save
// called with NO prior explicit confirmation turn must be REFUSED at the
// tool layer (not just the prompt layer) and must file nothing.
func TestIntakeSaveBlockedWithoutConfirm(t *testing.T) {
	saver := &captureSaver{}
	rec := IntakeRecord{Kind: "message", Fields: map[string]string{"text": "call the customer"}}
	if err := SaveIntake(saver, rec, false); err == nil {
		t.Fatal("SaveIntake with confirm=false must be refused at the tool layer")
	}
	if len(saver.saves) != 0 {
		t.Fatalf("refused save filed rows anyway: %v", saver.saves)
	}
}

// TestIntakeSaveProceedsAfterConfirm pins the happy path: the explicit
// confirm flag (set only by a confirmation turn) lets the save file the
// record with the spec's kind.
func TestIntakeSaveProceedsAfterConfirm(t *testing.T) {
	saver := &captureSaver{}
	rec := IntakeRecord{Kind: "message", Fields: map[string]string{"text": "call the customer"}}
	if err := SaveIntake(saver, rec, true); err != nil {
		t.Fatalf("SaveIntake(confirm=true): %v", err)
	}
	if len(saver.saves) != 1 || saver.saves[0] != "message:call the customer" {
		t.Fatalf("saves = %v, want exactly [message:call the customer]", saver.saves)
	}
}

// TestIntakeSaveRequiresKind pins schema validation: an intake record
// without a Kind is refused even with the confirm flag (the kind becomes
// the ledger subject vocabulary and must be present).
func TestIntakeSaveRequiresKind(t *testing.T) {
	saver := &captureSaver{}
	rec := IntakeRecord{Fields: map[string]string{"text": "x"}}
	if err := SaveIntake(saver, rec, true); err == nil {
		t.Fatal("SaveIntake without Kind must be refused")
	}
	if len(saver.saves) != 0 {
		t.Errorf("invalid record filed: %v", saver.saves)
	}
}
