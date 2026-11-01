package contextalloc

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// seamProvider returns a fixed tier list for the seam tests; distinct
// from tier_test.go's stubProvider so both suites stay independent.
type seamProvider struct {
	tiers []TierBudget
	calls int
}

func (s *seamProvider) TierBudgets(ctx context.Context, query string, windowTokens int) []TierBudget {
	s.calls++
	return s.tiers
}

// TestLoopSeamNilAllocatorDisabled pins T2's nil rule: a nil *Allocator
// disables the seam entirely -- ok=false, no provider call.
func TestLoopSeamNilAllocatorDisabled(t *testing.T) {
	prov := &seamProvider{tiers: []TierBudget{{Tier: TierWorking}}}
	s := NewLoopSeam(nil, prov, nil)

	assert.False(t, s.Enabled())
	items, budgets, ok := s.MemoryContext(context.Background(), "q", 100000)
	assert.False(t, ok)
	assert.Nil(t, items)
	assert.Nil(t, budgets)
	assert.Equal(t, 0, prov.calls, "disabled seam must not call the provider")
}

// TestLoopSeamTypedNilProviderDisabled pins the typed-nil guard: a
// non-nil *Allocator with a typed-nil provider (*stubProvider)(nil) is
// ALSO disabled (isAbsentProvider reflect rule).
func TestLoopSeamTypedNilProviderDisabled(t *testing.T) {
	a, err := New(DefaultConfig())
	require.NoError(t, err)
	var typedNil *seamProvider
	s := NewLoopSeam(a, typedNil, nil)

	assert.False(t, s.Enabled())
	_, _, ok := s.MemoryContext(context.Background(), "q", 100000)
	assert.False(t, ok)
}

// TestLoopSeamNilReceiverSafe pins that every seam method tolerates a nil
// receiver (loop fields are populated by setters; the zero value is nil).
func TestLoopSeamNilReceiverSafe(t *testing.T) {
	var s *LoopSeam
	assert.False(t, s.Enabled())
	assert.Nil(t, s.Allocator())
	assert.Nil(t, s.Tokenizer())
	items, budgets, ok := s.MemoryContext(context.Background(), "q", 1000)
	assert.False(t, ok)
	assert.Nil(t, items)
	assert.Nil(t, budgets)
}

// TestLoopSeamEnabledBudgetsPerTier pins the enabled path: an allocator
// with a real provider yields per-tier budgets for the three named tiers
// and calls recall through the provider's handles.
func TestLoopSeamEnabledBudgetsPerTier(t *testing.T) {
	a, err := New(DefaultConfig())
	require.NoError(t, err)
	recalled := map[TierName]int{}
	prov := &seamProvider{tiers: []TierBudget{
		{Tier: TierWorking, Recall: func(ctx context.Context, q string, limit int) []string {
			recalled[TierWorking] = limit
			return []string{"working item"}
		}},
		{Tier: TierEpisodic},
		{Tier: TierSemantic},
	}}
	s := NewLoopSeam(a, prov, nil)

	assert.True(t, s.Enabled())
	items, budgets, ok := s.MemoryContext(context.Background(), "query words", 200000)
	require.True(t, ok)

	assert.Equal(t, 2, prov.calls,
		"one provider pass in Allocate's tier split + one recall pass")
	// Three named tiers budgeted. Episodic/semantic carry no ceiling, so
	// the allocator's per-tier minimum (MinTierTokens=256) is their
	// floor; working takes the rest of the Memory section.
	assert.Len(t, budgets, 3)
	assert.Greater(t, budgets[TierWorking], MinTierTokens)
	assert.GreaterOrEqual(t, budgets[TierEpisodic], MinTierTokens)
	assert.GreaterOrEqual(t, budgets[TierSemantic], MinTierTokens)
	// Working recall happened through the handle; episodic/semantic have
	// no recall and yield nil items, not missing keys.
	assert.Equal(t, []string{"working item"}, items[TierWorking])
	assert.Nil(t, items[TierEpisodic])
	assert.Nil(t, items[TierSemantic])
	assert.Greater(t, recalled[TierWorking], 0)
}

// TestLoopSeamWindowFeedsAllocation pins that MemoryContext's
// windowTokens drives the allocation (T4 hook): a 200k window produces
// the documented reserve math, and the per-tier budgets are drawn from
// it.
func TestLoopSeamWindowFeedsAllocation(t *testing.T) {
	a, err := New(DefaultConfig())
	require.NoError(t, err)
	prov := &seamProvider{tiers: []TierBudget{{Tier: TierWorking}}}
	s := NewLoopSeam(a, prov, nil)

	_, budgets, ok := s.MemoryContext(context.Background(), "q", 200000)
	require.True(t, ok)

	alloc := a.Allocate(context.Background(), 200000, Request{Query: "q"})
	assert.Equal(t, alloc.MemoryWorking, budgets[TierWorking],
		"seam budgets must equal the allocator's section math for the same window")
	// 200000 - max(0.09*200000, 4096) = 200000 - 18000 = 182000 usable.
	assert.Equal(t, 182000, alloc.WindowTokens)
	assert.Equal(t, 18000, alloc.OutputReserve)
}

// TestLoopSeamUnknownTierIgnored pins that a provider returning an
// unknown tier name still yields a usable seam response for the known
// tiers.
func TestLoopSeamUnknownTierIgnored(t *testing.T) {
	a, err := New(DefaultConfig())
	require.NoError(t, err)
	prov := &seamProvider{tiers: []TierBudget{
		{Tier: TierName("wiki")},
		{Tier: TierEpisodic},
	}}
	s := NewLoopSeam(a, prov, nil)

	items, budgets, ok := s.MemoryContext(context.Background(), "q", 100000)
	require.True(t, ok)
	assert.Len(t, budgets, 1)
	assert.Contains(t, budgets, TierEpisodic)
	assert.Contains(t, items, TierEpisodic)
}

// TestLoopSeamTokenizerPassThrough pins the tokenizer accessor used by
// the doc-budget path.
func TestLoopSeamTokenizerPassThrough(t *testing.T) {
	a, err := New(DefaultConfig())
	require.NoError(t, err)
	tk := &llm.HeuristicTokenizer{}
	s := NewLoopSeam(a, &seamProvider{}, tk)
	assert.Same(t, tk, s.Tokenizer())

	s2 := NewLoopSeam(a, &seamProvider{}, nil)
	assert.Nil(t, s2.Tokenizer())
}
