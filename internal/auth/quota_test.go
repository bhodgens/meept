package auth

import (
	"context"
	"testing"
)

func TestNoopQuotaAllowsEverything(t *testing.T) {
	var q QuotaEvaluator = NoopQuota{}

	for _, cost := range []int{0, 1, 1000, -1} {
		ok, err := q.Allow(context.Background(), &Identity{UserID: "user-1"}, cost)
		if err != nil {
			t.Fatalf("Allow(cost=%d): %v", cost, err)
		}
		if !ok {
			t.Fatalf("Allow(cost=%d) = false, want true (no-op allows everything)", cost)
		}
	}
}

func TestNoopQuotaNilIdentity(t *testing.T) {
	q := NoopQuota{}

	ok, err := q.Allow(context.Background(), nil, 42)
	if err != nil {
		t.Fatalf("Allow(nil identity): %v", err)
	}
	if !ok {
		t.Fatal("Allow(nil identity) = false, want true")
	}
}
