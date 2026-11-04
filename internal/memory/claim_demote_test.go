package memory

import (
	"context"
	"testing"
	"time"
)

// TestClaimDemoteFlagRoundTrip: DemoteClaim stamps the flag via the REAL
// store path (SQLite fixture), the flag survives a re-read, and a second
// DemoteClaim is an idempotent noop (R-B7).
func TestClaimDemoteFlagRoundTrip(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	id, err := m.StoreClaim(ctx, Claim{Text: "the deploy runs at noon", Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("StoreClaim: %v", err)
	}

	at := time.Date(2026, 11, 1, 12, 0, 0, 0, time.UTC)
	if err := m.DemoteClaim(ctx, id, "stale", at); err != nil {
		t.Fatalf("DemoteClaim: %v", err)
	}

	mem, err := m.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after demote: %v", err)
	}
	if !ClaimIsDemoted(mem.Metadata) {
		t.Fatalf("claim must carry the demoted flag, metadata: %v", mem.Metadata)
	}
	if mem.ID != id {
		t.Fatalf("in-place stamp must keep the claim ID stable: %s -> %s", id, mem.ID)
	}
	if got := mem.Metadata[MetaClaimDemotedReason]; got != "stale" {
		t.Fatalf("reason must be recorded, got %v", got)
	}
	if got := mem.Metadata[MetaClaimDemotedTo]; got != string(MemoryTypeEpisodic) {
		t.Fatalf("destination tier must be recorded, got %v", got)
	}
	if mem.Content != "the deploy runs at noon" {
		t.Fatalf("flag-not-delete: content must be untouched, got %q", mem.Content)
	}

	// Idempotent re-run: noop, no error.
	if err := m.DemoteClaim(ctx, id, "stale", time.Now()); err != nil {
		t.Fatalf("re-demote must be a noop: %v", err)
	}
	mem2, err := m.GetByID(ctx, id)
	if err != nil {
		t.Fatalf("GetByID after re-demote: %v", err)
	}
	if got := mem2.Metadata[MetaClaimDemotedAt]; got != mem.Metadata[MetaClaimDemotedAt] {
		t.Fatalf("re-demote must not overwrite the original demotion stamp: %v vs %v", got, mem.Metadata[MetaClaimDemotedAt])
	}
}

// TestClaimDemoteRejectsNonClaim: the demotion flag path is claim-only.
func TestClaimDemoteRejectsNonClaim(t *testing.T) {
	m := newTestManager(t)
	ctx := context.Background()

	id, err := m.Store(ctx, Memory{Type: MemoryTypeEpisodic, Category: "conversation", Content: "plain"})
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	if err := m.DemoteClaim(ctx, id, "stale", time.Now()); err == nil {
		t.Fatal("demoting a non-claim must error")
	}
}

// TestClaimDemoteUninitialized: an uninitialized manager errors honestly.
func TestClaimDemoteUninitialized(t *testing.T) {
	m := NewManager(ManagerConfig{})
	if err := m.DemoteClaim(context.Background(), "any", "stale", time.Now()); err == nil {
		t.Fatal("uninitialized manager must error")
	}
}

// TestFilterDemotedClaims: recall-side visibility filter drops demoted
// claims, keeps everything else, and fails open on nil metadata.
func TestFilterDemotedClaims(t *testing.T) {
	results := []MemoryResult{
		{Memory: Memory{ID: "keep-1", Metadata: map[string]any{"status": "auto"}}},
		{Memory: Memory{ID: "demoted-1", Metadata: map[string]any{MetaClaimDemoted: true}}},
		{Memory: Memory{ID: "keep-nil"}},
		{Memory: Memory{ID: "demoted-json", Metadata: map[string]any{MetaClaimDemoted: "true"}}},
	}
	out := FilterDemotedClaims(results)
	if len(out) != 2 {
		t.Fatalf("want 2 survivors, got %d: %+v", len(out), out)
	}
	if out[0].Memory.ID != "keep-1" || out[1].Memory.ID != "keep-nil" {
		t.Fatalf("wrong survivors: %+v", out)
	}
	// Nil input is safe.
	if got := FilterDemotedClaims(nil); len(got) != 0 {
		t.Fatalf("nil input must yield empty, got %+v", got)
	}
}

// TestClaimIsDemotedShapes: the flag reader tolerates every metadata
// shape the store can produce.
func TestClaimIsDemotedShapes(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{true, true},
		{"true", true},
		{"demoted", true},
		{float64(1), true},
		{false, false},
		{"false", false},
		{"", false},
		{nil, false},
		{42, false},
	}
	for _, tc := range cases {
		if got := ClaimIsDemoted(map[string]any{MetaClaimDemoted: tc.v}); got != tc.want {
			t.Fatalf("ClaimIsDemoted(%#v) = %v, want %v", tc.v, got, tc.want)
		}
	}
	if ClaimIsDemoted(nil) {
		t.Fatal("nil metadata must read as not-demoted")
	}
}
