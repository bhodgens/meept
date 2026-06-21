package memory

import (
	"context"
	"testing"
	"time"
)

func TestClaimStatusTrustWeight(t *testing.T) {
	cases := []struct {
		status     ClaimStatus
		autoWeight float64
		want       float64
	}{
		{ClaimStatusConfirmed, 0.5, 1.0},
		{ClaimStatusPromoted, 0.5, 1.0},
		{ClaimStatusAuto, 0.5, 0.5},
		{ClaimStatusAuto, 0.0, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, 1.5, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, -0.1, DefaultAutoClaimTrustWeight},
		{ClaimStatusAuto, 0.8, 0.8},
		{ClaimStatusRejected, 0.5, 0.0},
		{ClaimStatus("bogus"), 0.5, 0.0},
	}
	for _, c := range cases {
		got := c.status.TrustWeight(c.autoWeight)
		if got != c.want {
			t.Errorf("status=%q autoWeight=%v: got %v, want %v", c.status, c.autoWeight, got, c.want)
		}
	}
}

func TestEffectiveAutoTrustWeight(t *testing.T) {
	if EffectiveAutoTrustWeight(0) != DefaultAutoClaimTrustWeight {
		t.Error("zero value should yield default")
	}
	if EffectiveAutoTrustWeight(0.9) != 0.9 {
		t.Error("explicit value should pass through")
	}
	if EffectiveAutoTrustWeight(1.5) != DefaultAutoClaimTrustWeight {
		t.Error("out-of-range should yield default")
	}
}

func TestIsEpistemicType(t *testing.T) {
	for _, mt := range []MemoryType{MemoryTypeClaim, MemoryTypeDecision, MemoryTypePrediction, MemoryTypeQuestion} {
		if !IsEpistemicType(mt) {
			t.Errorf("%q should be epistemic", mt)
		}
	}
	if IsEpistemicType(MemoryTypeEpisodic) {
		t.Error("episodic should not be epistemic")
	}
}

func TestStoreClaimRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StoreClaim(context.Background(), Claim{Text: "x", Status: ClaimStatusConfirmed}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestStoreDecisionRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StoreDecision(context.Background(), Decision{Call: "x"}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestStorePredictionRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StorePrediction(context.Background(), Prediction{Forecast: "x", Horizon: time.Now()}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestStoreQuestionRequiresManager(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.StoreQuestion(context.Background(), Question{Text: "x"}); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestPromoteRejectUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if err := m.PromoteClaim(context.Background(), "x"); err == nil {
		t.Error("PromoteClaim should fail on uninitialized manager")
	}
	if err := m.RejectClaim(context.Background(), "x"); err == nil {
		t.Error("RejectClaim should fail on uninitialized manager")
	}
	if _, err := m.ListAutoClaims(context.Background(), time.Now(), 10); err == nil {
		t.Error("ListAutoClaims should fail on uninitialized manager")
	}
	if _, _, err := m.ListPendingReviews(context.Background(), time.Now()); err == nil {
		t.Error("ListPendingReviews should fail on uninitialized manager")
	}
	if _, err := m.FindCanonicalFor(context.Background(), "topic"); err == nil {
		t.Error("FindCanonicalFor should fail on uninitialized manager")
	}
}

func TestOutcomeOverlapScore(t *testing.T) {
	if got := outcomeOverlapScore("same", "same"); got != 1.0 {
		t.Errorf("identical: got %v, want 1.0", got)
	}
	if got := outcomeOverlapScore("a", "b"); got != 0.0 {
		t.Errorf("disjoint: got %v, want 0.0", got)
	}
}

func TestMarkSupersededUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, _, err := m.MarkSuperseded(context.Background(), "a", "b"); err == nil {
		t.Error("expected error from uninitialized manager")
	}
}

func TestMarkResolvedUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, err := m.MarkResolved(context.Background(), "x", "outcome"); err == nil {
		t.Error("expected error")
	}
}

func TestRecordReviewUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if _, _, err := m.RecordReview(context.Background(), "x", "actual"); err == nil {
		t.Error("expected error")
	}
}

func TestManagerSetEpistemicDetector(t *testing.T) {
	m := NewManager(ManagerConfig{})
	// Setter should accept nil without panic (defense in depth).
	m.SetEpistemicDetector(nil)
	// Setter should accept a real detector.
	d := NewEpistemicDetector(EpistemicDetectorConfig{})
	m.SetEpistemicDetector(d)
}
