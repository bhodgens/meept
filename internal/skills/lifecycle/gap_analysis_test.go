package lifecycle

import (
	"context"
	"testing"
)

func TestPassDFillGap_ProposesSkillForRepeatedQueries(t *testing.T) {
	analyzer := NewGapAnalyzer()
	gaps := []LowMatchQuery{
		{Query: "deploy helm chart", Count: 12, BestScore: 0.3},
		{Query: "deploy helm chart", Count: 12, BestScore: 0.3},      // dedup at the analyzer level
		{Query: "single occurrence noise", Count: 1, BestScore: 0.1}, // below threshold, skipped
	}
	proposals := analyzer.Propose(context.Background(), gaps)
	if len(proposals) != 1 {
		t.Fatalf("expected 1 proposal (deduped, singletons skipped), got %d", len(proposals))
	}
	if proposals[0].Action != ProposalFillGap {
		t.Errorf("expected ProposalFillGap, got %v", proposals[0].Action)
	}
	if proposals[0].SkillName != "deploy-helm-chart" {
		t.Errorf("expected slugified name 'deploy-helm-chart', got %q", proposals[0].SkillName)
	}
}

func TestPassDFillGap_SlugifiesNames(t *testing.T) {
	cases := map[string]string{
		"Deploy Helm Chart": "deploy-helm-chart",
		"k8s rollout undo":  "k8s-rollout-undo",
		"--weird--input--":  "weird-input",
		"":                  "unnamed-skill",
	}
	for input, want := range cases {
		got := slugifyQuery(input)
		if got != want {
			t.Errorf("slugifyQuery(%q) = %q, want %q", input, got, want)
		}
	}
}
