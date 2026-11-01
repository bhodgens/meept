package contextalloc

import (
	"context"
	"reflect"
	"testing"
)

// stubProvider returns a fixed tier list; used to exercise the tier seam.
type stubProvider struct {
	tiers []TierBudget
	calls int
}

func (s *stubProvider) TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget {
	s.calls++
	return s.tiers
}

// nilableProvider is a concrete type whose pointer can be typed-nil.
type nilableProvider struct{}

func (n *nilableProvider) TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget {
	return nil
}

// tierTestConfig gives the Memory section enough room (17000 tokens) for the
// ceiling examples below to fit without proportional shrinking: window
// 100000, reserve 0.15 -> usable 85000, Memory fraction 0.20 -> 17000.
func tierTestConfig(provider TierBudgetProvider) Config {
	return Config{
		OutputReserveFraction: 0.15,
		Fractions: SectionFractions{
			SystemStable: 0.30,
			DynamicDocs:  0.10,
			Memory:       0.20,
			Tools:        0.15,
			History:      0.25,
		},
		TierProvider: provider,
	}
}

func TestTierSplitCeilingHints(t *testing.T) {
	p := &stubProvider{tiers: []TierBudget{
		{Tier: TierWorking, Ceiling: 6000},
		{Tier: TierEpisodic, Ceiling: 0},
	}}
	a, err := New(tierTestConfig(p))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if memTotal(alloc) <= 0 {
		t.Fatalf("Memory section must be positive, got %d", memTotal(alloc))
	}
	sum := alloc.MemoryWorking + alloc.MemoryEpisodic + alloc.MemorySemantic
	if sum != memTotal(alloc) {
		t.Fatalf("tier split sum %d != Memory %d", sum, memTotal(alloc))
	}
	if alloc.MemoryWorking < 6000 {
		t.Fatalf("ceiling hint 6000 not honored as floor: MemoryWorking = %d", alloc.MemoryWorking)
	}
	if p.calls != 1 {
		t.Fatalf("provider consulted %d times per Allocate, want 1", p.calls)
	}
}

func TestTierSplitEqualWithoutCeilings(t *testing.T) {
	p := &stubProvider{tiers: []TierBudget{
		{Tier: TierWorking},
		{Tier: TierEpisodic},
	}}
	a, err := New(tierTestConfig(p))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if alloc.MemoryWorking != alloc.MemoryEpisodic {
		t.Fatalf("equal split expected with no ceilings: working=%d episodic=%d",
			alloc.MemoryWorking, alloc.MemoryEpisodic)
	}
	sum := alloc.MemoryWorking + alloc.MemoryEpisodic + alloc.MemorySemantic
	if sum != memTotal(alloc) {
		t.Fatalf("tier split sum %d != Memory %d", sum, memTotal(alloc))
	}
}

func TestTierSplitNilProviderUndivided(t *testing.T) {
	a, err := New(tierTestConfig(nil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if alloc.MemoryWorking != memTotal(alloc) {
		t.Fatalf("nil provider: MemoryWorking %d != Memory %d", alloc.MemoryWorking, memTotal(alloc))
	}
	if alloc.MemoryEpisodic != 0 || alloc.MemorySemantic != 0 {
		t.Fatalf("nil provider must leave memory undivided, got episodic=%d semantic=%d",
			alloc.MemoryEpisodic, alloc.MemorySemantic)
	}
}

func TestTierSplitTypedNilProviderAbsent(t *testing.T) {
	// Typed-nil interface value must be treated as absent (C1 seam rule,
	// typed-nil guard convention): Memory stays undivided, no panic.
	typedNil := (*nilableProvider)(nil)
	a, err := New(tierTestConfig(typedNil))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if alloc.MemoryWorking != memTotal(alloc) || alloc.MemoryEpisodic != 0 || alloc.MemorySemantic != 0 {
		t.Fatalf("typed-nil provider must behave as absent, got %+v", alloc)
	}
}

func TestTierSplitSliceOrderInsignificant(t *testing.T) {
	forward := &stubProvider{tiers: []TierBudget{
		{Tier: TierWorking, Ceiling: 6000},
		{Tier: TierEpisodic, Ceiling: 0},
		{Tier: TierSemantic, Ceiling: 2000},
	}}
	reversed := &stubProvider{tiers: []TierBudget{
		{Tier: TierSemantic, Ceiling: 2000},
		{Tier: TierEpisodic, Ceiling: 0},
		{Tier: TierWorking, Ceiling: 6000},
	}}
	af, err := New(tierTestConfig(forward))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ar, err := New(tierTestConfig(reversed))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	a1 := af.Allocate(context.Background(), 100000, Request{})
	a2 := ar.Allocate(context.Background(), 100000, Request{})
	if a1.MemoryWorking != a2.MemoryWorking ||
		a1.MemoryEpisodic != a2.MemoryEpisodic ||
		a1.MemorySemantic != a2.MemorySemantic {
		t.Fatalf("slice order changed the split: %+v vs %+v", a1, a2)
	}
}

func TestTierSplitUnknownAndDuplicateNames(t *testing.T) {
	// Unknown tier names are ignored; duplicate names keep the first entry.
	// Either way the per-tier split still sums to Memory exactly.
	p := &stubProvider{tiers: []TierBudget{
		{Tier: TierName("bogus"), Ceiling: 9999},
		{Tier: TierEpisodic, Ceiling: 3000},
		{Tier: TierEpisodic, Ceiling: 1111},
	}}
	a, err := New(tierTestConfig(p))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	sum := alloc.MemoryWorking + alloc.MemoryEpisodic + alloc.MemorySemantic
	if sum != memTotal(alloc) {
		t.Fatalf("tier split sum %d != Memory %d", sum, memTotal(alloc))
	}
	if alloc.MemoryEpisodic < 3000 {
		t.Fatalf("first duplicate entry must win its ceiling: episodic=%d", alloc.MemoryEpisodic)
	}
	if alloc.MemoryWorking != 0 {
		t.Fatalf("unknown tier name must be ignored entirely, working=%d", alloc.MemoryWorking)
	}
}

func TestTierSplitZeroMemory(t *testing.T) {
	cfg := tierTestConfig(&stubProvider{tiers: []TierBudget{
		{Tier: TierWorking, Ceiling: 6000},
	}})
	cfg.Fractions = SectionFractions{SystemStable: 1.0}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if memTotal(alloc) != 0 || alloc.MemoryWorking != 0 || alloc.MemoryEpisodic != 0 || alloc.MemorySemantic != 0 {
		t.Fatalf("zero Memory section must split to zeros, got %+v", alloc)
	}
}

func TestTierNameConstants(t *testing.T) {
	if TierWorking != "working" || TierEpisodic != "episodic" || TierSemantic != "semantic" {
		t.Fatalf("tier constants drifted: %q %q %q", TierWorking, TierEpisodic, TierSemantic)
	}
	// Compile-time shape checks for the C1 contract.
	var _ RecallFunc = func(ctx context.Context, query string, limit int) []string { return nil }
	var _ TierBudgetProvider = (*stubProvider)(nil)
	if !reflect.TypeOf(TierWorking).Comparable() {
		t.Fatal("TierName must be comparable")
	}
}
