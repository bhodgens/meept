package shadow

import (
	"context"
	"log/slog"
	"math"
	"strings"
	"testing"
)

// newScorerForTrapTests builds a scorer with default heuristic weights.
func newScorerForTrapTests(t *testing.T) *Scorer {
	t.Helper()
	cfg := &QualityConfig{
		Method:               MethodHeuristic,
		HighQualityThreshold: 0.85,
		TrainableThreshold:   0.6,
		HeuristicWeights: HeuristicWeights{
			Relevance:    0.30,
			Completeness: 0.25,
			Correctness:  0.35,
			Style:        0.10,
		},
	}
	return NewScorer(cfg, WithScorerLogger(slog.Default()))
}

// longWellFormattedWrongAnswer is a verbose, structured, confident answer to
// "What is the capital of France?" that never states the correct answer.
const longWellFormattedWrongAnswer = `# About France

France is a large country in Western Europe with a rich history and culture.
It is bordered by Belgium, Luxembourg, Germany, Switzerland, Italy, and Spain.
The country is famous for its cuisine, art, architecture, and philosophy.

## Key Facts

1. France has a population of around 68 million people.
2. The official language is French, spoken across all regions.
3. France is a founding member of the European Union.
4. The currency is the euro, adopted in 2002.
5. France operates under a semi-presidential system of government.

## More Context

The French Republic traces its modern form to the revolution of 1789.
Over the centuries it has produced renowned writers, scientists, and artists.
Tourism is a major industry, with millions visiting the Louvre, Versailles,
and the chateaux of the Loire Valley every single year without exception.
Culinary traditions such as baguettes, croissants, and coq au vin are known
worldwide, and French wine regions like Bordeaux and Burgundy are legendary.`

func TestScorer_LengthTrap(t *testing.T) {
	scorer := newScorerForTrapTests(t)
	ctx := context.Background()

	tests := []struct {
		name            string
		record          *ShadowRecord
		wantCorrectness float64
		wantScoreAtMost float64
		wantIsHighQual  bool
	}{
		{
			name: "long_wrong_never_beats_short_pass",
			record: &ShadowRecord{
				Messages:       []Message{{Role: RoleUser, Content: "What is the capital of France?"}},
				StudentContent: longWellFormattedWrongAnswer,
				Domain:         DomainGeneral,
				EvalPassed:     boolRef(false),
			},
			wantCorrectness: 0.0,
			wantScoreAtMost: 0.85, // never high quality
			wantIsHighQual:  false,
		},
		{
			name: "short_pass_outranks_long_wrong",
			record: &ShadowRecord{
				Messages:       []Message{{Role: RoleUser, Content: "What is the capital of France?"}},
				StudentContent: "The capital of France is Paris, a city of about two million people known for its history, art, and architecture across many centuries of European development.",
				Domain:         DomainGeneral,
				EvalPassed:     boolRef(true),
			},
			wantCorrectness: 1.0,
			wantScoreAtMost: 1.0,
			wantIsHighQual:  true, // oracle pass + relevant answer is legitimately high quality
		},
	}

	var longWrong, shortPass *ScoreResult
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result, err := scorer.Score(ctx, tc.record)
			if err != nil {
				t.Fatalf("Score failed: %v", err)
			}
			gotCorrectness := result.Dimensions["correctness"]
			if gotCorrectness != tc.wantCorrectness {
				t.Errorf("correctness = %v, want %v (oracle must dominate)", gotCorrectness, tc.wantCorrectness)
			}
			if result.Score > tc.wantScoreAtMost+1e-9 {
				t.Errorf("score = %v, want <= %v", result.Score, tc.wantScoreAtMost)
			}
			if result.IsHighQuality != tc.wantIsHighQual {
				t.Errorf("IsHighQuality = %v, want %v", result.IsHighQuality, tc.wantIsHighQual)
			}
			switch tc.name {
			case "long_wrong_never_beats_short_pass":
				longWrong = result
			case "short_pass_outranks_long_wrong":
				shortPass = result
			}
		})
	}

	if longWrong == nil || shortPass == nil {
		t.Fatal("expected both subtests to run")
	}
	if shortPass.Score <= longWrong.Score {
		t.Errorf("short oracle-pass score %v must beat long wrong score %v", shortPass.Score, longWrong.Score)
	}
}

func TestScorer_CompletenessCap(t *testing.T) {
	scorer := newScorerForTrapTests(t)
	ctx := context.Background()

	longPad := &ShadowRecord{
		Messages:       []Message{{Role: RoleUser, Content: "Tell me about X."}},
		StudentContent: longWellFormattedWrongAnswer,
		Domain:         DomainGeneral,
	}
	result, err := scorer.Score(ctx, longPad)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}

	// Weights: completeness 0.25. A long structured response scores 1.0
	// completeness => raw contribution 0.25... verify the cap only bites
	// when the contribution would exceed 0.3.
	if c := result.Dimensions["completeness"]; c*0.25 > maxCompletenessContribution {
		// Cap should have reduced the score by the excess.
		recomputed := result.Dimensions["relevance"]*0.30 +
			c*0.25 +
			result.Dimensions["correctness"]*0.35 +
			result.Dimensions["style"]*0.10
		excess := c*0.25 - maxCompletenessContribution
		if math.Abs(result.Score-(recomputed-excess)) > 1e-9 {
			t.Errorf("score %v does not reflect completeness cap", result.Score)
		}
	}

	// Even a pathological response padded to 40x with structure cannot
	// push its score near 1.0 via completeness alone.
	huge := &ShadowRecord{
		Messages:       []Message{{Role: RoleUser, Content: "Tell me about X."}},
		StudentContent: longWellFormattedWrongAnswer + strings.Repeat(longWellFormattedWrongAnswer, 40),
		Domain:         DomainGeneral,
	}
	hugeResult, err := scorer.Score(ctx, huge)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}
	if hugeResult.Score >= 1.0 {
		t.Errorf("padded response scored %v; length must not buy a perfect score", hugeResult.Score)
	}
	base, err := scorer.Score(ctx, longPad)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}
	if hugeResult.Score > base.Score+1e-9 {
		t.Errorf("more padding increased score (%v -> %v); length trap not capped",
			base.Score, hugeResult.Score)
	}
}

func TestScorer_EvalPassedNil_UsesHeuristicCorrectness(t *testing.T) {
	scorer := newScorerForTrapTests(t)

	withOracle := &ShadowRecord{
		Messages:       []Message{{Role: RoleUser, Content: "What is the capital of France?"}},
		StudentContent: "Paris",
		Domain:         DomainGeneral,
		EvalPassed:     boolRef(true),
	}
	withoutOracle := &ShadowRecord{
		Messages:       []Message{{Role: RoleUser, Content: "What is the capital of France?"}},
		StudentContent: "Paris",
		Domain:         DomainGeneral,
	}

	resWith, err := scorer.Score(context.Background(), withOracle)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}
	resWithout, err := scorer.Score(context.Background(), withoutOracle)
	if err != nil {
		t.Fatalf("Score failed: %v", err)
	}

	if resWith.Dimensions["correctness"] != 1.0 {
		t.Errorf("oracle-passed record correctness = %v, want 1.0", resWith.Dimensions["correctness"])
	}
	if resWithout.Dimensions["correctness"] == 1.0 {
		t.Errorf("heuristic short answer unexpectedly got perfect correctness")
	}
	// Oracle attach must raise the score relative to heuristic-only.
	if resWith.Score <= resWithout.Score {
		t.Errorf("oracle-pass score %v should exceed heuristic-only score %v", resWith.Score, resWithout.Score)
	}
}

// boolRef returns a pointer to b (test helper; no package-wide equivalent).
func boolRef(b bool) *bool {
	return &b
}
