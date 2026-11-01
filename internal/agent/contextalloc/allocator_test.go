package contextalloc

import (
	"context"
	"strings"
	"testing"
)

// validFractions returns the documented default fractions (sum = 1).
func validFractions() SectionFractions {
	return SectionFractions{
		SystemStable: 0.30,
		DynamicDocs:  0.10,
		Memory:       0.20,
		Tools:        0.15,
		History:      0.25,
	}
}

func validConfig() Config {
	return Config{
		OutputReserveFraction: 0.15,
		Fractions:             validFractions(),
		MinSections: MinSections{
			SystemStable: 256,
			DynamicDocs:  256,
			Memory:       256,
			Tools:        256,
			History:      256,
		},
	}
}

// memTotal is the aggregate Memory section (C2 exposes only the three
// per-tier fields; they always sum to the section).
func memTotal(a Allocation) int {
	return a.MemoryWorking + a.MemoryEpisodic + a.MemorySemantic
}

// --- T2: New validation ---

func TestNewRejectsFractionSumOff(t *testing.T) {
	cfg := validConfig()
	cfg.Fractions = SectionFractions{SystemStable: 0.30, DynamicDocs: 0.10, Memory: 0.20, Tools: 0.15, History: 0.15} // 0.90
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for fractions summing to 0.9")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "fraction") {
		t.Fatalf("error must mention fractions, got: %v", err)
	}
}

func TestNewAcceptsSumWithinEpsilon(t *testing.T) {
	cfg := validConfig()
	cfg.Fractions.History += 1e-10 // within the 1e-9 epsilon
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("sum within 1e-9 must be accepted, got: %v", err)
	}
	if a == nil {
		t.Fatal("New returned nil allocator with nil error")
	}
}

func TestNewRejectsNegativeFraction(t *testing.T) {
	cfg := validConfig()
	cfg.Fractions.Memory = -0.01
	_, err := New(cfg)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "fraction") {
		t.Fatalf("expected fraction error, got: %v", err)
	}
}

func TestNewRejectsNegativeFloor(t *testing.T) {
	cfg := validConfig()
	cfg.MinSections.History = -1
	_, err := New(cfg)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "floor") {
		t.Fatalf("expected floor error, got: %v", err)
	}
}

func TestNewRejectsFloorAboveSection(t *testing.T) {
	// A floor must not exceed the section's maximum plausible value; with a
	// zero History fraction the section can never exceed zero.
	cfg := validConfig()
	cfg.Fractions = SectionFractions{SystemStable: 1.0}
	cfg.MinSections.History = 1
	_, err := New(cfg)
	if err == nil {
		t.Fatal("expected error for History floor with a zero History fraction")
	}
}

func TestNewRejectsNegativeReserve(t *testing.T) {
	cfg := validConfig()
	cfg.OutputReserveFraction = -0.05
	_, err := New(cfg)
	if err == nil || !strings.Contains(strings.ToLower(err.Error()), "reserve") {
		t.Fatalf("expected reserve error, got: %v", err)
	}
}

func TestNewRejectsReserveAtOrAboveOne(t *testing.T) {
	for _, r := range []float64{1.0, 1.5} {
		cfg := validConfig()
		cfg.OutputReserveFraction = r
		if _, err := New(cfg); err == nil {
			t.Fatalf("reserve %v must be rejected", r)
		}
	}
}

// --- T3: exact sum + determinism ---

func TestAllocateSumsExactly(t *testing.T) {
	windows := []int{
		100000, 200000, 8192, 4096, 32768, 65536, 128000,
		99991, 123457, 100001, 3, 7, 1000, 12345, 777777,
	}
	fracts := []SectionFractions{
		validFractions(),
		{SystemStable: 0.333333, DynamicDocs: 0.111111, Memory: 0.111112, Tools: 0.222222, History: 0.222222},
		{SystemStable: 0.10, DynamicDocs: 0.20, Memory: 0.30, Tools: 0.05, History: 0.35},
	}
	for _, w := range windows {
		for fi, f := range fracts {
			cfg := Config{OutputReserveFraction: 0.15, Fractions: f}
			a, err := New(cfg)
			if err != nil {
				t.Fatalf("window %d frac %d: New: %v", w, fi, err)
			}
			alloc := a.Allocate(context.Background(), w, Request{})
			sum := alloc.SystemStable + alloc.SystemDynamicDocs + memTotal(alloc) +
				alloc.Tools + alloc.History
			if sum != alloc.WindowTokens {
				t.Fatalf("window %d frac %d: section sum %d != usable %d (alloc %+v)",
					w, fi, sum, alloc.WindowTokens, alloc)
			}
			wantUsable := w - alloc.OutputReserve
			if wantUsable < 0 {
				wantUsable = 0
			}
			if alloc.WindowTokens != wantUsable {
				t.Fatalf("window %d: usable %d, want %d (reserve %d)",
					w, alloc.WindowTokens, wantUsable, alloc.OutputReserve)
			}
		}
	}
}

func TestAllocateDeterministic(t *testing.T) {
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	req := Request{Query: "alpha", ToolCount: 7, DocBytes: 2048, HistoryTokens: 4096}
	first := a.Allocate(context.Background(), 100000, req)
	for i := 0; i < 50; i++ {
		again := a.Allocate(context.Background(), 100000, req)
		if first != again {
			t.Fatalf("Allocate is not deterministic:\nfirst: %+v\nagain: %+v", first, again)
		}
	}
}

func TestAllocateLargestRemainderProperty(t *testing.T) {
	// Every section must equal floor(frac*usable) or floor+1: no section may
	// drift more than one token from its exact share.
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	usable := 85000 // 100000 - 15000 reserve
	alloc := a.Allocate(context.Background(), 100000, Request{})
	shares := map[string][2]int{
		"SystemStable":      {int(0.30 * float64(usable)), alloc.SystemStable},
		"SystemDynamicDocs": {int(0.10 * float64(usable)), alloc.SystemDynamicDocs},
		"Memory":            {int(0.20 * float64(usable)), memTotal(alloc)},
		"Tools":             {int(0.15 * float64(usable)), alloc.Tools},
		"History":           {int(0.25 * float64(usable)), alloc.History},
	}
	for name, sh := range shares {
		want, got := sh[0], sh[1]
		if got != want && got != want+1 {
			t.Fatalf("%s: got %d, want floor share %d or %d", name, got, want, want+1)
		}
	}
}

func TestAllocateTinyWindowsNonNegative(t *testing.T) {
	// The absolute output-reserve floor (R-A2) exceeds tiny windows
	// entirely; usable floors at zero and every section stays non-negative.
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, w := range []int{0, 1, 100, 4096, 5000} {
		alloc := a.Allocate(context.Background(), w, Request{})
		fields := []struct {
			name string
			v    int
		}{
			{"OutputReserve", alloc.OutputReserve},
			{"SystemStable", alloc.SystemStable},
			{"SystemDynamicDocs", alloc.SystemDynamicDocs},
			{"Memory", memTotal(alloc)},
			{"Tools", alloc.Tools},
			{"History", alloc.History},
			{"IterationBudget", alloc.IterationBudget},
			{"ConversationBudget", alloc.ConversationBudget},
		}
		for _, f := range fields {
			if f.v < 0 {
				t.Fatalf("window %d: %s = %d is negative", w, f.name, f.v)
			}
		}
		sum := alloc.SystemStable + alloc.SystemDynamicDocs + memTotal(alloc) + alloc.Tools + alloc.History
		if sum != alloc.WindowTokens {
			t.Fatalf("window %d: section sum %d != usable %d", w, sum, alloc.WindowTokens)
		}
	}
}

// --- T4: floors ---

func TestAllocateFloorsHonoredWhenFit(t *testing.T) {
	cfg := Config{
		OutputReserveFraction: 0.15,
		Fractions:             validFractions(),
		MinSections:           MinSections{History: 5000},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	// window 100000, reserve 15000 -> usable 85000; proportional History
	// share is 21250, far above its floor; floors must not disturb it.
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if alloc.History != 21250 {
		t.Fatalf("History = %d, want proportional 21250", alloc.History)
	}
}

func TestAllocateFloorsExceedUsableShrinkProportionally(t *testing.T) {
	// Floor History=50000 but usable is only ~40000 (window 47059, reserve
	// 0.15). Floors must shrink proportionally, never go negative, and
	// History must never exceed the usable tokens.
	cfg := Config{
		OutputReserveFraction: 0.15,
		Fractions:             validFractions(),
		MinSections:           MinSections{History: 50000},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 47059, Request{})
	if alloc.History > alloc.WindowTokens {
		t.Fatalf("History %d exceeds usable %d", alloc.History, alloc.WindowTokens)
	}
	if alloc.History < 0 {
		t.Fatalf("History is negative: %d", alloc.History)
	}
	sum := alloc.SystemStable + alloc.SystemDynamicDocs + memTotal(alloc) + alloc.Tools + alloc.History
	if sum != alloc.WindowTokens {
		t.Fatalf("sections must still sum exactly after floor shrink: %d != %d",
			sum, alloc.WindowTokens)
	}
	if alloc.History <= alloc.Tools {
		t.Fatalf("proportional shrink lost History's share primacy: history=%d tools=%d",
			alloc.History, alloc.Tools)
	}
}

func TestAllocateFloorsNeverNegative(t *testing.T) {
	cfg := Config{
		OutputReserveFraction: 0.80,
		Fractions:             validFractions(),
		MinSections:           MinSections{History: 50000, Tools: 1000},
	}
	a, err := New(cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 40000, Request{})
	fields := []struct {
		name string
		v    int
	}{
		{"SystemStable", alloc.SystemStable},
		{"SystemDynamicDocs", alloc.SystemDynamicDocs},
		{"Memory", memTotal(alloc)},
		{"Tools", alloc.Tools},
		{"History", alloc.History},
	}
	for _, f := range fields {
		if f.v < 0 {
			t.Fatalf("%s negative under floor pressure: %d", f.name, f.v)
		}
	}
	sum := alloc.SystemStable + alloc.SystemDynamicDocs + memTotal(alloc) + alloc.Tools + alloc.History
	if sum != alloc.WindowTokens {
		t.Fatalf("sections must sum exactly under floor pressure: %d != %d", sum, alloc.WindowTokens)
	}
}

// --- Reserve semantics (R-A2/R-A3/R-A10) ---

func TestOutputReserveAbsoluteFloor(t *testing.T) {
	// R-A2: the reserve is the LARGER of the configured fraction and the
	// absolute floor of 4096 tokens; it never shrinks proportionally.
	a, err := New(Config{OutputReserveFraction: 0.01, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if alloc.OutputReserve != 4096 {
		t.Fatalf("reserve = %d, want absolute floor 4096", alloc.OutputReserve)
	}
	// Sections distribute exactly the post-reserve window (R-A3's ~3%
	// tokenizer margin is documented as folded into the reserve default).
	sum := alloc.SystemStable + alloc.SystemDynamicDocs + memTotal(alloc) + alloc.Tools + alloc.History
	if sum != 95904 {
		t.Fatalf("sections must distribute window - reserve: sum %d, want 95904", sum)
	}
}

func TestOutputReserveFractionWinsWhenLarger(t *testing.T) {
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if alloc.OutputReserve != 15000 {
		t.Fatalf("reserve = %d, want fraction value 15000", alloc.OutputReserve)
	}
}

func TestSmallWindowReserveStaysAbsolute(t *testing.T) {
	// R-A2/R-A10: on a 8192-token window the fraction (0.15*8192 = 1228) is
	// below the absolute floor; the reserve must stay 4096.
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 8192, Request{})
	if alloc.OutputReserve != 4096 {
		t.Fatalf("small-window reserve = %d, want absolute floor 4096 (R-A2)", alloc.OutputReserve)
	}
	if alloc.WindowTokens != 4096 {
		t.Fatalf("usable = %d, want 4096", alloc.WindowTokens)
	}
}

// --- T6: derived budgets ---

func TestDerivedBudgetsNonzeroAndMonotonic(t *testing.T) {
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	small := a.Allocate(context.Background(), 100000, Request{})
	large := a.Allocate(context.Background(), 200000, Request{})
	if small.IterationBudget <= 0 || small.ConversationBudget <= 0 {
		t.Fatalf("derived budgets must be nonzero: %+v", small)
	}
	if large.IterationBudget <= small.IterationBudget {
		t.Fatalf("IterationBudget must increase with window: %d vs %d",
			large.IterationBudget, small.IterationBudget)
	}
	if large.ConversationBudget <= small.ConversationBudget {
		t.Fatalf("ConversationBudget must increase with window: %d vs %d",
			large.ConversationBudget, small.ConversationBudget)
	}
}

func TestIterationBudgetCappedAtHistoryShare(t *testing.T) {
	// T6: IterationBudget = min(History share, prior-art ratio rule). The
	// prior-art rule (context_firewall.go DerivedIterationBudget, 0.30 ratio
	// of the resolved window) is capped here at the History section share.
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	if alloc.IterationBudget > alloc.History {
		t.Fatalf("IterationBudget %d exceeds History share %d", alloc.IterationBudget, alloc.History)
	}
	if alloc.IterationBudget != alloc.History {
		// prior-art rule: 0.30 * 100000 = 30000 > History 21250 -> History wins
		t.Fatalf("IterationBudget = %d, want History share 21250 (min rule)", alloc.IterationBudget)
	}
}

func TestConversationBudgetTracksFraction(t *testing.T) {
	// T6: ConversationBudget follows the prior-art conversation ratio
	// (context_firewall.go DerivedConversationBudget, 0.50 of the resolved
	// window), capped at the post-reserve window.
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	alloc := a.Allocate(context.Background(), 100000, Request{})
	want := int(0.50 * float64(100000))
	if alloc.ConversationBudget != want {
		t.Fatalf("ConversationBudget = %d, want window*0.50 = %d", alloc.ConversationBudget, want)
	}
}

func TestZeroWindowAndNegativeWindow(t *testing.T) {
	a, err := New(Config{OutputReserveFraction: 0.15, Fractions: validFractions()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	for _, w := range []int{0, -100} {
		alloc := a.Allocate(context.Background(), w, Request{})
		if alloc.WindowTokens != 0 {
			t.Fatalf("window %d: usable must floor at 0, got %d", w, alloc.WindowTokens)
		}
		if alloc.OutputReserve < 0 {
			t.Fatalf("window %d: negative reserve %d", w, alloc.OutputReserve)
		}
	}
}
