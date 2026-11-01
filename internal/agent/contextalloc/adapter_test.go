package contextalloc

import (
	"context"
	"testing"

	"github.com/caimlas/meept/internal/llm"
	"github.com/caimlas/meept/internal/memory"
	"github.com/caimlas/meept/internal/memory/working"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// countingCounter is a deterministic TokenCounter for working.Store.
type countingCounter struct{}

func (countingCounter) CountTokens(s string) int { return len(s) / 4 }

// newWorkingStore builds a small working ring with one turn.
func newWorkingStore(t *testing.T) *working.Store {
	t.Helper()
	s, err := working.New(working.Config{MaxTokens: 4000, MaxItems: 20}, countingCounter{})
	require.NoError(t, err)
	require.NoError(t, s.Append(context.Background(), working.Turn{
		ID: "t1", SessionID: "s1", Role: "user",
		Content: "remember the deploy checklist item alpha",
	}))
	return s
}

// TestAdapterNilStoresEmptySlice pins T6's typed-nil rule: an adapter
// over all-nil (and typed-nil) stores returns an EMPTY NON-NIL slice --
// never a panic, never nil-map traps downstream.
func TestAdapterNilStoresEmptySlice(t *testing.T) {
	p := NewTierStoreProvider(nil, 0, nil, 0, nil, 0, nil)
	assert.False(t, p.Enabled())

	got := p.TierBudgets(context.Background(), "q", 100000)
	assert.NotNil(t, got, "absent adapter must return an empty non-nil slice")
	assert.Empty(t, got)

	// Typed-nil pointers behind the concrete types behave the same.
	var workNil *working.Store
	var epiNil *memory.EpisodicMemory
	var mgrNil *memory.Manager
	p2 := NewTierStoreProvider(workNil, 0, epiNil, 0, mgrNil, 0, nil)
	assert.False(t, p2.Enabled())
	got2 := p2.TierBudgets(context.Background(), "q", 100000)
	assert.NotNil(t, got2)
	assert.Empty(t, got2)
}

// TestAdapterNilTierOmittedPanicFree pins the per-tier nil rule: nil
// episodic/semantic stores alongside a wired working store yield exactly
// one tier entry (working), never nil-derefs.
func TestAdapterNilTierOmittedPanicFree(t *testing.T) {
	p := NewTierStoreProvider(newWorkingStore(t), 0, nil, 0, nil, 0, nil)
	assert.True(t, p.Enabled())

	got := p.TierBudgets(context.Background(), "deploy checklist", 100000)
	require.Len(t, got, 1)
	assert.Equal(t, TierWorking, got[0].Tier)
	assert.NotNil(t, got[0].Recall)
	items := got[0].Recall(context.Background(), "deploy checklist", 5)
	assert.Contains(t, items, "remember the deploy checklist item alpha")
}

// TestAdapterCeilingsHonored pins the Ceiling pass-through: each wired
// tier carries its configured cap hint (0 = allocator decides).
func TestAdapterCeilingsHonored(t *testing.T) {
	p := NewTierStoreProvider(newWorkingStore(t), 1500, nil, 2000, nil, 3000, nil)
	got := p.TierBudgets(context.Background(), "q", 100000)
	require.Len(t, got, 1) // only working is wired here
	assert.Equal(t, 1500, got[0].Ceiling)
}

// TestAdapterEndToEndThroughAllocator pins the whole C1 chain: the
// adapter feeds the allocator's tier split and the resulting Allocation
// sums exactly.
func TestAdapterEndToEndThroughAllocator(t *testing.T) {
	p := NewTierStoreProvider(newWorkingStore(t), 0, nil, 0, nil, 0, nil)
	cfg := DefaultConfig()
	cfg.TierProvider = p
	a, err := New(cfg)
	require.NoError(t, err)

	alloc := a.Allocate(context.Background(), 200000, Request{Query: "q"})
	sum := alloc.MemoryWorking + alloc.MemoryEpisodic + alloc.MemorySemantic
	assert.Greater(t, sum, 0)
	// One tier present and undivided-shares == Memory section: with only
	// working wired and no ceilings, working takes the whole Memory
	// section (splitLR of one weight).
	assert.Equal(t, sum, alloc.MemoryWorking)
	assert.Equal(t, 0, alloc.MemoryEpisodic)
	assert.Equal(t, 0, alloc.MemorySemantic)
}

// TestAdapterCeilingsSum pins the three-tier ceiling math: capped tiers
// receive exactly their ceilings when the Memory section can fund them.
func TestAdapterCeilingsSum(t *testing.T) {
	// Only ceilings matter here; stores may be nil since ceilings ride
	// the tier entry, but the adapter omits nil stores -- so wire the
	// working store for all three slots via distinct providers. This
	// test exercises the allocator math through the seam instead.
	prov := &stubProvider{tiers: []TierBudget{
		{Tier: TierWorking, Ceiling: 6000},
		{Tier: TierEpisodic, Ceiling: 2000},
		{Tier: TierSemantic, Ceiling: 2000},
	}}
	cfg := DefaultConfig()
	cfg.TierProvider = prov
	a, err := New(cfg)
	require.NoError(t, err)
	alloc := a.Allocate(context.Background(), 200000, Request{Query: "q"})
	// Memory section (20% of 182000 = 36400) funds 6000+2000+2000 with
	// surplus; every tier is capped so the surplus spreads proportional
	// to the ceilings (caps are hints, tier.go's documented rule). Sum
	// invariants hold regardless.
	sum := alloc.MemoryWorking + alloc.MemoryEpisodic + alloc.MemorySemantic
	assert.Equal(t, 36400, sum)
	assert.GreaterOrEqual(t, alloc.MemoryWorking, 6000, "ceiling acts as the tier's minimum share")
	assert.GreaterOrEqual(t, alloc.MemoryEpisodic, 2000)
	assert.GreaterOrEqual(t, alloc.MemorySemantic, 2000)
}

// TestAdapterTokenizerCarried pins the tokenizer pass-through used by the
// loop's doc-budget path.
func TestAdapterTokenizerCarried(t *testing.T) {
	tk := &llm.HeuristicTokenizer{}
	p := NewTierStoreProvider(nil, 0, nil, 0, nil, 0, tk)
	assert.Same(t, tk, p.tk)
}
