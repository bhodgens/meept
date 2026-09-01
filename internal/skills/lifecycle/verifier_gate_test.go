package lifecycle

import (
	"log/slog"
	"testing"
)

// TestArchiveUsageGate drives the archive usage gate table-first: archive
// proposals are judged on usage statistics ONLY — never the content rubric.
//
// Gate polarity (leaf 04, orchestrator-corrected): the gate re-verifies
// passCPrune's SELECTION predicate (GetLowPerformers: inject_count >=
// ArchiveMinInjections AND effectiveness < MinEffectiveness). An archive
// passes only when the skill is STILL a low performer; a skill whose
// effectiveness recovered above the threshold is rejected — it improved.
func TestArchiveUsageGate(t *testing.T) {
	cfg := defaultEvolverConfig()
	verifier := NewVerifier(nil, slog.Default())

	tests := []struct {
		name               string
		stats              *UsageStats
		wantAction         VerifyAction
		wantReasonContains string // empty: no reason content asserted
	}{
		{
			// Still a low performer: exactly what passCPrune selected.
			// This is the canonical archive-accepted case.
			name:       "pass when still a low performer",
			stats:      &UsageStats{SkillName: "still-weak", InjectCount: 25, PositiveCount: 2, Effectiveness: 0.1},
			wantAction: ActionAccept,
		},
		{
			name:       "pass exactly at injection threshold, effectiveness below prune threshold",
			stats:      &UsageStats{SkillName: "boundary-inject", InjectCount: ArchiveMinInjections, Effectiveness: cfg.MinEffectiveness - 0.01},
			wantAction: ActionAccept,
		},
		{
			// Recovered since selection: effectiveness crossed the prune
			// threshold upward — archiving it would discard a skill that
			// improved. This is the gate's whole reason to exist.
			name:               "reject when effectiveness recovered above prune threshold",
			stats:              &UsageStats{SkillName: "recovered", InjectCount: 25, PositiveCount: 20, Effectiveness: 0.8},
			wantAction:         ActionReject,
			wantReasonContains: "recovered",
		},
		{
			// Selection used effectiveness strictly < threshold; the gate
			// matches: exactly-at-threshold counts as recovered (>=).
			name:               "reject exactly at prune threshold (inclusive recovery boundary)",
			stats:              &UsageStats{SkillName: "boundary-recover", InjectCount: ArchiveMinInjections, Effectiveness: cfg.MinEffectiveness},
			wantAction:         ActionReject,
			wantReasonContains: "recovered",
		},
		{
			name:               "reject when injections below minimum (not enough usage history)",
			stats:              &UsageStats{SkillName: "few-shots", InjectCount: 9, PositiveCount: 9, Effectiveness: 0.1},
			wantAction:         ActionReject,
			wantReasonContains: "injection",
		},
		{
			// Nil stats degrade to zero stats: an unverifiable archive is a
			// rejected archive.
			name:               "reject on zero stats (never used skill)",
			stats:              &UsageStats{SkillName: "never-used"},
			wantAction:         ActionReject,
			wantReasonContains: "injection",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Archive proposals carry empty CandidateContent (evolver.go) —
			// there is no content for the rubric to judge.
			req := VerifyRequest{
				Action:           "archive_skill",
				SkillName:        tc.stats.SkillName,
				CandidateContent: "",
				CurrentContent:   "---\nname: " + tc.stats.SkillName + "\n---\nold body",
				EvidenceSummary:  "low performer",
				UsageGate:        &UsageGateInput{Stats: tc.stats, MinEffectiveness: cfg.MinEffectiveness},
			}

			res, err := verifier.Verify(t.Context(), req)
			if err != nil {
				t.Fatalf("Verify failed: %v", err)
			}

			if res.Action != tc.wantAction {
				t.Fatalf("action = %q, want %q (reasons: %v)", res.Action, tc.wantAction, res.Reasons)
			}
			if res.Gate != GateUsage {
				t.Fatalf("gate = %q, want %q", res.Gate, GateUsage)
			}
			if tc.wantReasonContains != "" && len(res.Reasons) > 0 {
				found := false
				for _, r := range res.Reasons {
					if contains(r, tc.wantReasonContains) {
						found = true
						break
					}
				}
				if !found {
					t.Fatalf("reasons %v do not mention %q", res.Reasons, tc.wantReasonContains)
				}
			}
			// The content rubric must never score archive proposals.
			if res.Dimensions != (Dimensions{}) {
				t.Fatalf("archive verdict has content dimensions: %+v", res.Dimensions)
			}
		})
	}
}

// TestVerifierGate pins the gate vocabulary on the content path: refine and
// create verdicts carry GateContent.
func TestVerifierGate(t *testing.T) {
	verifier := NewVerifier(nil, slog.Default())

	cases := []struct {
		name   string
		action string
	}{
		{"refine verdict carries content gate", "improve_skill"},
		{"create verdict carries content gate", "create_skill"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := VerifyRequest{
				Action:           tc.action,
				SkillName:        "some-skill",
				CandidateContent: "---\nname: some-skill\ndescription: d\n---\nnew body",
				CurrentContent:   "---\nname: some-skill\ndescription: d\n---\nold body",
				EvidenceSummary:  "test",
			}
			res, err := verifier.Verify(t.Context(), req)
			if err != nil {
				t.Fatalf("Verify failed: %v", err)
			}
			if res.Gate != GateContent {
				t.Fatalf("gate = %q, want %q", res.Gate, GateContent)
			}
		})
	}
}

// contains is a tiny helper avoiding strings import in table tests.
func contains(s, sub string) bool {
	return len(sub) == 0 || (len(s) >= len(sub) && indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}

// TestVerifyUsage_NilInputFailClosed pins the fail-closed contract: an
// archive proposal arriving with no usage payload (UsageGate nil) rejects —
// never a silent content-rubric rubber-stamp.
func TestVerifyUsage_NilInputFailClosed(t *testing.T) {
	res := verifyUsage(nil)
	if res.Action != ActionReject {
		t.Fatalf("nil input must reject, got %s", res.Action)
	}
	if res.Gate != GateUsage {
		t.Fatalf("gate = %q, want usage", res.Gate)
	}
	if len(res.Reasons) == 0 {
		t.Fatal("nil input must carry a rejection reason")
	}
}
