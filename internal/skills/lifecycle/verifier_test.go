package lifecycle

import (
	"strings"
	"testing"
)

// TestDecideAcceptAllHigh validates that when all four dimensions are at or
// above 0.75, the decide function accepts the proposal.
func TestDecideAcceptAllHigh(t *testing.T) {
	dims := Dimensions{
		GroundedInEvidence:        0.85,
		PreservesExistingValue:    0.90,
		SpecificityAndReusability: 0.75,
		SafeToPublish:             0.80,
	}
	result := decide(dims, nil, defaultMinScore)

	if result.Action != ActionAccept {
		t.Errorf("expected ActionAccept, got %q (score=%.4f)", result.Action, result.Score)
	}
	expectedScore := (0.85 + 0.90 + 0.75 + 0.80) / 4.0
	if result.Score != expectedScore {
		t.Errorf("expected score %.4f, got %.4f", expectedScore, result.Score)
	}
	if len(result.Reasons) != 0 {
		t.Errorf("expected no rejection reasons, got %d: %v", len(result.Reasons), result.Reasons)
	}
}

// TestDecideRejectAnyBelowFloor validates that when any single dimension falls
// below the per-dimension floor (0.5), the proposal is rejected regardless of
// the overall average.
func TestDecideRejectAnyBelowFloor(t *testing.T) {
	dims := Dimensions{
		GroundedInEvidence:        0.40, // below floor
		PreservesExistingValue:    0.90,
		SpecificityAndReusability: 0.90,
		SafeToPublish:             0.90,
	}
	result := decide(dims, nil, defaultMinScore)

	if result.Action != ActionReject {
		t.Errorf("expected ActionReject due to dim below floor, got %q", result.Action)
	}

	// Check that the reason mentions the offending dimension.
	foundReason := false
	for _, r := range result.Reasons {
		if strings.Contains(r, "grounded_in_evidence") && strings.Contains(r, "below floor") {
			foundReason = true
		}
	}
	if !foundReason {
		t.Errorf("expected a reason mentioning grounded_in_evidence below floor, got %v", result.Reasons)
	}
}

// TestDecideRejectAverageBelowMinScore validates that even when all dimensions
// are at or above the floor, a proposal whose overall average falls below
// minScore is rejected.
func TestDecideRejectAverageBelowMinScore(t *testing.T) {
	// All dims >= 0.5 (floor), but average is 0.6 which is < default 0.75.
	dims := Dimensions{
		GroundedInEvidence:        0.60,
		PreservesExistingValue:    0.60,
		SpecificityAndReusability: 0.60,
		SafeToPublish:             0.60,
	}
	result := decide(dims, nil, defaultMinScore)

	if result.Action != ActionReject {
		t.Errorf("expected ActionReject (avg 0.60 < minScore 0.75), got %q", result.Action)
	}
	if result.Score != 0.60 {
		t.Errorf("expected score 0.60, got %.4f", result.Score)
	}
}

// TestDecideAcceptWithLowerMinScore validates that the same dimension scores
// that are rejected under the default threshold (0.75) are accepted when the
// threshold is lowered.
func TestDecideAcceptWithLowerMinScore(t *testing.T) {
	dims := Dimensions{
		GroundedInEvidence:        0.60,
		PreservesExistingValue:    0.60,
		SpecificityAndReusability: 0.60,
		SafeToPublish:             0.60,
	}
	// minScore = 0.5, all dims >= 0.5 floor and avg 0.60 >= 0.50
	result := decide(dims, nil, 0.50)

	if result.Action != ActionAccept {
		t.Errorf("expected ActionAccept with lowered threshold 0.50, got %q", result.Action)
	}
}

// TestDecideAcceptBoundary checks the exact boundary: average exactly equals
// minScore.
func TestDecideAcceptBoundary(t *testing.T) {
	// average = 0.75 exactly
	dims := Dimensions{
		GroundedInEvidence:        0.75,
		PreservesExistingValue:    0.75,
		SpecificityAndReusability: 0.75,
		SafeToPublish:             0.75,
	}
	result := decide(dims, nil, defaultMinScore)

	if result.Action != ActionAccept {
		t.Errorf("expected ActionAccept at boundary (avg == minScore), got %q", result.Action)
	}
}

// TestDecideRejectFloorBoundary checks that a dimension exactly at the floor
// (0.5) does NOT trigger rejection — rejection is strictly < floor.
func TestDecideRejectFloorBoundary(t *testing.T) {
	dims := Dimensions{
		GroundedInEvidence:        0.50, // exactly at floor — should NOT trigger floor rejection
		PreservesExistingValue:    0.80,
		SpecificityAndReusability: 0.80,
		SafeToPublish:             0.80,
	}
	// avg = 0.725, which is < 0.75 so still rejected by minScore
	result := decide(dims, nil, defaultMinScore)
	if result.Action != ActionReject {
		t.Errorf("expected ActionReject (avg 0.725 < 0.75), got %q", result.Action)
	}
	// Now lower minScore to 0.70 — should accept since no dim is *strictly* below 0.5.
	result2 := decide(dims, nil, 0.70)
	if result2.Action != ActionAccept {
		t.Errorf("expected ActionAccept (floor not breached, avg >= 0.70), got %q", result2.Action)
	}
}

// TestVerifierHeuristicNilClient verifies that when the LLM client is nil,
// Verify uses the heuristic fallback (all dims 0.5) and rejects under the
// default threshold.
func TestVerifierHeuristicNilClient(t *testing.T) {
	v := NewVerifier(nil, nil) // nil client, nil logger (defaults to slog.Default())

	req := VerifyRequest{
		Action:           "improve_skill",
		SkillName:        "test-skill",
		CandidateContent: "# test skill\n\nupdated content",
		CurrentContent:   "# test skill\n\nold content",
		EvidenceSummary:  "10 injections, 0.5 effectiveness",
	}

	result, err := v.Verify(t.Context(), req)
	if err != nil {
		t.Fatalf("unexpected error from heuristic Verify: %v", err)
	}

	if result.Action != ActionReject {
		t.Errorf("expected ActionReject from heuristic (0.5 avg < 0.75 default), got %q", result.Action)
	}
	if result.Dimensions.GroundedInEvidence != 0.5 {
		t.Errorf("expected heuristic GroundedInEvidence=0.5, got %.2f", result.Dimensions.GroundedInEvidence)
	}
	if result.Dimensions.PreservesExistingValue != 0.5 {
		t.Errorf("expected heuristic PreservesExistingValue=0.5, got %.2f", result.Dimensions.PreservesExistingValue)
	}
	if result.Dimensions.SpecificityAndReusability != 0.5 {
		t.Errorf("expected heuristic SpecificityAndReusability=0.5, got %.2f", result.Dimensions.SpecificityAndReusability)
	}
	if result.Dimensions.SafeToPublish != 0.5 {
		t.Errorf("expected heuristic SafeToPublish=0.5, got %.2f", result.Dimensions.SafeToPublish)
	}
	if result.Score != 0.5 {
		t.Errorf("expected heuristic score=0.5, got %.4f", result.Score)
	}
}

// TestVerifierHeuristicLowMinScore verifies that the heuristic fallback can
// accept when the minScore threshold is lowered to 0.5 (matching the 0.5
// heuristic scores).
func TestVerifierHeuristicLowMinScore(t *testing.T) {
	v := NewVerifier(nil, nil, WithMinScore(0.5))

	req := VerifyRequest{
		Action:           "create_skill",
		CandidateContent: "# new skill",
		EvidenceSummary:  "pattern promoted from learned data",
	}

	result, err := v.Verify(t.Context(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if result.Action != ActionAccept {
		t.Errorf("expected ActionAccept (heuristic 0.5 == minScore 0.5), got %q", result.Action)
	}
}

// TestVerifierWithMinScoreOption verifies that the WithMinScore option
// correctly sets the threshold on the Verifier.
func TestVerifierWithMinScoreOption(t *testing.T) {
	v := NewVerifier(nil, nil, WithMinScore(0.60))
	if v.minScore != 0.60 {
		t.Errorf("expected minScore=0.60, got %.2f", v.minScore)
	}
}

// TestVerifierDefaultMinScore verifies the default threshold is 0.75.
func TestVerifierDefaultMinScore(t *testing.T) {
	v := NewVerifier(nil, nil)
	if v.minScore != defaultMinScore {
		t.Errorf("expected default minScore=%.2f, got %.2f", defaultMinScore, v.minScore)
	}
}

// TestParseVerifierResponseValidJSON tests that a well-formed LLM JSON
// response is parsed correctly into Dimensions.
func TestParseVerifierResponseValidJSON(t *testing.T) {
	content := "Here is my evaluation:\n" +
		"```json\n" +
		"{\n" +
		"  \"grounded_in_evidence\": 0.85,\n" +
		"  \"preserves_existing_value\": 0.90,\n" +
		"  \"specificity_and_reusability\": 0.70,\n" +
		"  \"safe_to_publish\": 0.95,\n" +
		"  \"reasons\": [\"strong evidence\", \"retains structure\"]\n" +
		"}\n" +
		"```"
	dims, reasons, err := parseVerifierResponse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dims.GroundedInEvidence != 0.85 {
		t.Errorf("expected GroundedInEvidence=0.85, got %.2f", dims.GroundedInEvidence)
	}
	if dims.PreservesExistingValue != 0.90 {
		t.Errorf("expected PreservesExistingValue=0.90, got %.2f", dims.PreservesExistingValue)
	}
	if dims.SpecificityAndReusability != 0.70 {
		t.Errorf("expected SpecificityAndReusability=0.70, got %.2f", dims.SpecificityAndReusability)
	}
	if dims.SafeToPublish != 0.95 {
		t.Errorf("expected SafeToPublish=0.95, got %.2f", dims.SafeToPublish)
	}
	if len(reasons) != 2 {
		t.Errorf("expected 2 reasons, got %d", len(reasons))
	}
}

// TestParseVerifierResponseClampsOutOfRange verifies that scores outside [0,1]
// are clamped.
func TestParseVerifierResponseClampsOutOfRange(t *testing.T) {
	content := `{
		"grounded_in_evidence": 1.5,
		"preserves_existing_value": -0.3,
		"specificity_and_reusability": 0.7,
		"safe_to_publish": 0.8
	}`
	dims, _, err := parseVerifierResponse(content)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if dims.GroundedInEvidence != 1.0 {
		t.Errorf("expected clamp to 1.0, got %.2f", dims.GroundedInEvidence)
	}
	if dims.PreservesExistingValue != 0.0 {
		t.Errorf("expected clamp to 0.0, got %.2f", dims.PreservesExistingValue)
	}
}

// TestParseVerifierResponseInvalidJSON ensures that invalid JSON returns an error.
func TestParseVerifierResponseInvalidJSON(t *testing.T) {
	content := `this is not JSON at all`
	_, _, err := parseVerifierResponse(content)
	if err == nil {
		t.Fatal("expected error for invalid JSON, got nil")
	}
}

// TestVerifierVerifyRequestFields ensures that VerifyRequest fields are
// properly populated and accessible.
func TestVerifierVerifyRequestFields(t *testing.T) {
	req := VerifyRequest{
		Action:           "archive_skill",
		SkillName:        "low-performer",
		CandidateContent: "",
		CurrentContent:   "# old content",
		EvidenceSummary:  "15 injections, 0.1 effectiveness",
	}
	if req.Action != "archive_skill" {
		t.Errorf("expected Action=archive_skill, got %q", req.Action)
	}
	if req.SkillName != "low-performer" {
		t.Errorf("expected SkillName=low-performer, got %q", req.SkillName)
	}
}
