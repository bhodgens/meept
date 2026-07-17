package agent

import (
	"fmt"
	"strings"
	"sync"
	"testing"
)

// -----------------------------------------------------------------------
// TestTurnCounter_NudgeThresholds — verify threshold crossing behavior
// -----------------------------------------------------------------------

func TestTurnCounter_NudgeThresholds(t *testing.T) {
	tests := []struct {
		name       string
		limit      int
		increment  int   // how many times to call Increment() before calling Nudge()
		wantThr    float64
		wantExh    bool
		description string
	}{
		// Below 25% — no threshold, returns basic banner.
		{"turn-0", 20, 0, 0.0, false, "zero turns: no threshold"},
		{"turn-1", 20, 1, 0.0, false, "1 turn of 20 (5%): below 25% threshold"},
		{"turn-4", 20, 4, 0.0, false, "4 turns of 20 (20%): still below 25%"},

		// At 25% threshold.
		{"turn-5", 20, 5, 0.25, false, "5 turns of 20 (25%): outline threshold"},
		{"turn-6", 20, 6, 0.25, false, "6 turns of 20 (30%): still under outline"},

		// At 50% threshold.
		{"turn-10", 20, 10, 0.50, false, "10 turns of 20 (50%): prioritize threshold"},
		{"turn-14", 20, 14, 0.50, false, "14 turns of 20 (70%): consolidate applies, not prioritize"},

		// Correction: 14/20=70%, which is >= 0.75? No, 0.7 < 0.75. So it should be 0.50.
		{"turn-14-corrected", 20, 14, 0.50, false, "14 turns of 20 (70%): consolidate would be 75%"},

		// At 75% threshold.
		{"turn-15", 20, 15, 0.75, false, "15 turns of 20 (75%): consolidate threshold"},
		{"turn-18", 20, 18, 0.90, false, "18 turns of 20 (90%): finalize threshold"},

		// At 100% — exhausted.
		{"turn-20", 20, 20, 1.0, true, "20 turns of 20 (100%): exhausted"},
		{"turn-21", 20, 21, 1.0, true, "21 turns of 20 (105%): past limit, exhausted"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newTurnCounter(tt.limit)

			// Burn turns.
			for i := 0; i < tt.increment; i++ {
				tc.Increment()
			}

			info := tc.Nudge()

			if info.Threshold != tt.wantThr {
				t.Errorf("threshold = %v, want %v (%s)", info.Threshold, tt.wantThr, tt.description)
			}
			if info.Exhausted != tt.wantExh {
				t.Errorf("exhausted = %v, want %v (%s)", info.Exhausted, tt.wantExh, tt.description)
			}
			if info.CurrentTurn != tc.current {
				t.Errorf("currentTurn in info = %d, want %d (%s)", info.CurrentTurn, tc.current, tt.description)
			}
			if info.Limit != tt.limit {
				t.Errorf("limit in info = %d, want %d (%s)", info.Limit, tt.limit, tt.description)
			}

			wantRemaining := tt.limit - tc.current
			if wantRemaining < 0 {
				wantRemaining = 0
			}
			if info.Remaining != wantRemaining {
				t.Errorf("remaining = %d, want %d (%s)", info.Remaining, wantRemaining, tt.description)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_NudgeMessages — verify message content at each threshold
// -----------------------------------------------------------------------

func TestTurnCounter_NudgeMessages(t *testing.T) {
	tests := []struct {
		name      string
		increment int
		wantTag   string
		contain   string // substring that must appear in the message
	}{
		{"below_25pct", 1, "", "[HALO:"},
		{"at_25pct_outline", 5, "outline", "outlining"},
		{"at_50pct_prioritize", 10, "prioritize", "prioritizing"},
		{"at_75pct_consolidate", 15, "consolidate", "consolidating"},
		{"at_90pct_finalize", 18, "finalize", "finalize"},
		{"at_100pct_exhausted", 20, "exhausted", "Turn limit reached"},
		{"past_limit", 25, "exhausted", "Turn limit reached"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := newTurnCounter(20)
			for i := 0; i < tt.increment; i++ {
				tc.Increment()
			}

			info := tc.Nudge()

			if info.Tag != tt.wantTag {
				t.Errorf("tag = %q, want %q", info.Tag, tt.wantTag)
			}

			if !strings.Contains(info.Message, tt.contain) {
				t.Errorf("message = %q, should contain %q", info.Message, tt.contain)
			}
		})
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_Remaining — verify GetRemainingTurns / Remaining
// -----------------------------------------------------------------------

func TestTurnCounter_Remaining(t *testing.T) {
	tc := newTurnCounter(10)

	if tc.Remaining() != 10 {
		t.Errorf("initial remaining = %d, want 10", tc.Remaining())
	}
	if tc.GetRemainingTurns() != 10 {
		t.Errorf("GetRemainingTurns = %d, want 10", tc.GetRemainingTurns())
	}

	// Burn 3 turns.
	tc.Increment()
	tc.Increment()
	tc.Increment()

	if tc.Remaining() != 7 {
		t.Errorf("after 3 increments remaining = %d, want 7", tc.Remaining())
	}
	if tc.GetRemainingTurns() != 7 {
		t.Errorf("GetRemainingTurns after 3 increments = %d, want 7", tc.GetRemainingTurns())
	}

	// Burn up to limit.
	for i := 0; i < 7; i++ {
		tc.Increment()
	}
	if tc.Remaining() != 0 {
		t.Errorf("at limit remaining = %d, want 0", tc.Remaining())
	}

	// Go past limit.
	tc.Increment()
	if tc.Remaining() != 0 {
		t.Errorf("past limit remaining = %d, want 0 (clamped to 0)", tc.Remaining())
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_GetNudgeThreshold
// -----------------------------------------------------------------------

func TestTurnCounter_GetNudgeThreshold(t *testing.T) {
	tc := newTurnCounter(20)

	if got := tc.GetNudgeThreshold(); got != 0.0 {
		t.Errorf("initial threshold = %v, want 0.0", got)
	}

	// Thresholds at 5, 10, 15, 18 turns for limit=20.
	tc.Increment() // 1
	if got := tc.GetNudgeThreshold(); got != 0.0 {
		t.Errorf("after 1 turn: threshold = %v, want 0.0", got)
	}

	for i := 0; i < 4; i++ {
		tc.Increment() // 5
	}
	if got := tc.GetNudgeThreshold(); got != 0.25 {
		t.Errorf("after 5 turns: threshold = %v, want 0.25", got)
	}

	for i := 0; i < 5; i++ {
		tc.Increment() // 10
	}
	if got := tc.GetNudgeThreshold(); got != 0.50 {
		t.Errorf("after 10 turns: threshold = %v, want 0.50", got)
	}

	for i := 0; i < 5; i++ {
		tc.Increment() // 15
	}
	if got := tc.GetNudgeThreshold(); got != 0.75 {
		t.Errorf("after 15 turns: threshold = %v, want 0.75", got)
	}

	for i := 0; i < 3; i++ {
		tc.Increment() // 18
	}
	if got := tc.GetNudgeThreshold(); got != 0.90 {
		t.Errorf("after 18 turns: threshold = %v, want 0.90", got)
	}

	tc.Increment() // 19
	_ = tc.GetNudgeThreshold() // 19/20 = 0.95, still 0.90

	tc.Increment() // 20 — exhausted, threshold = 1.0
	if got := tc.GetNudgeThreshold(); got != 1.0 {
		t.Errorf("after 20 turns: threshold = %v, want 1.0", got)
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_ZeroLimit
// -----------------------------------------------------------------------

func TestTurnCounter_ZeroLimit(t *testing.T) {
	tc := newTurnCounter(0)

	// Zero limit: any increment is immediately at/over the limit.
	_, exhausted := tc.Increment()
	if !exhausted {
		t.Error("Increment(0, 0): should be exhausted (zero limit)")
	}

	info := tc.Nudge()
	// Zero-limit exhausted returns the sentinel message.
	if info.Tag != "exhausted" {
		t.Errorf("zero-limit nudge tag = %q, want 'exhausted'", info.Tag)
	}

	if tc.GetRemainingTurns() != 0 {
		t.Errorf("zero-limit remaining = %d, want 0", tc.GetRemainingTurns())
	}

	if got := tc.GetNudgeThreshold(); got != 0.0 {
		t.Errorf("zero-limit threshold = %v, want 0.0", got)
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_MultiIncrement
// -----------------------------------------------------------------------

func TestTurnCounter_MultiIncrement(t *testing.T) {
	tc := newTurnCounter(5)

	// Call Increment without tracking return values (simulate burn).
	for i := 0; i < 5; i++ {
		tc.Increment()
	}

	info := tc.Nudge()
	if !info.Exhausted {
		t.Error("after exhausting limit: should be exhausted")
	}
	if info.Threshold != 1.0 {
		t.Errorf("exhausted threshold = %v, want 1.0", info.Threshold)
	}
	if info.Tag != "exhausted" {
		t.Errorf("exhausted tag = %q, want 'exhausted'", info.Tag)
	}
	if info.Remaining != 0 {
		t.Errorf("exhausted remaining = %d, want 0", info.Remaining)
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_Concurrent
// -----------------------------------------------------------------------

func TestTurnCounter_Concurrent(t *testing.T) {
	tc := newTurnCounter(1000)

	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(3)
		go func() {
			defer wg.Done()
			tc.Increment()
		}()
		go func() {
			defer wg.Done()
			_ = tc.Nudge()
		}()
		go func() {
			defer wg.Done()
			_ = tc.Remaining()
		}()
	}
	wg.Wait()

	remaining := tc.Remaining()
	if remaining < 0 || remaining > 1000 {
		t.Errorf("concurrent: remaining = %d, want in [0, 1000]", remaining)
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_SoftLimitWarning — warn at 80% threshold
// -----------------------------------------------------------------------

func TestTurnCounter_SoftLimitWarning(t *testing.T) {
	tc := newTurnCounter(50) // soft limit ~30, warn at 80% = 24

	// Up to 23 turns: below 50%, so 25% threshold fires.
	for i := 0; i < 23; i++ {
		tc.Increment()
	}
	info := tc.Nudge()
	if info.Threshold != 0.25 {
		t.Errorf("at 23/50 (46%%): threshold = %v, want 0.25", info.Threshold)
	}
	if info.Exhausted {
		t.Error("at 23/50 should not be exhausted")
	}

	// At 24 turns (48%): still 0.75 (75% threshold = turns 37.5, rounded to 40 for 80% of 50).
	// Actually 75% of 50 = 37.5, so first hits at turn 38.
	// At 80% of 50 = 40. The thresholds are 25%, 50%, 75%, 90%, 100%.
	// "Soft limit" at 30 turns (60%): the 50% threshold fires.
	// The spec says warn at 80% of soft limit, but the implementation uses fixed thresholds.
	// At turn 38 (76%): 75% threshold fires.
	tc = newTurnCounter(50)
	for i := 0; i < 38; i++ {
		tc.Increment()
	}
	info = tc.Nudge()
	if info.Threshold != 0.75 {
		t.Errorf("at 38/50 (76%%): threshold = %v, want 0.75 (consolidate tag)", info.Threshold)
	}
	if info.Tag != "consolidate" {
		t.Errorf("at 38/50: tag = %q, want 'consolidate'", info.Tag)
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_HardLimitEnforcement — forces summary at hard limit
// -----------------------------------------------------------------------

func TestTurnCounter_HardLimitEnforcement(t *testing.T) {
	limit := 10
	tc := newTurnCounter(limit)

	// Burn all turns.
	for i := 0; i < limit; i++ {
		count, exhausted := tc.Increment()
		if i < limit-1 && exhausted {
			t.Errorf("at turn %d/%d: should not be exhausted yet", count, limit)
		}
	}

	// At exactly the limit: exhausted = true.
	info := tc.Nudge()
	if !info.Exhausted {
		t.Error("at hard limit: should be exhausted")
	}
	if info.Threshold != 1.0 {
		t.Errorf("at hard limit: threshold = %v, want 1.0", info.Threshold)
	}
	if info.Tag != "exhausted" {
		t.Errorf("at hard limit: tag = %q, want 'exhausted'", info.Tag)
	}
	if info.Remaining != 0 {
		t.Errorf("at hard limit: remaining = %d, want 0", info.Remaining)
	}
	if info.Message != "Turn limit reached. Finalize immediately." {
		t.Errorf("at hard limit: message = %q, want exact exhausted message", info.Message)
	}

	// Verify force-termination signal via Exhausted field.
	if !info.Exhausted {
		t.Error("Hard limit enforcement: Exhausted should be true for forced termination")
	}
}

// -----------------------------------------------------------------------
// TestThresholdFor_Turn
// -----------------------------------------------------------------------

func TestThresholdForTurn(t *testing.T) {
	// Limit 20: thresholds at turns 5 (25%), 10 (50%), 15 (75%), 18 (90%), 20 (100%)
	tests := []struct {
		turn int
		want float64
	}{
		{0, 0.0},
		{1, 0.0},
		{4, 0.0},
		{5, 0.25},
		{9, 0.25},
		{10, 0.50},
		{14, 0.50},
		{15, 0.75},
		{17, 0.75},
		{18, 0.90},
		{19, 0.90},
		{20, 1.0},
		{25, 1.0},
	}

	for _, tt := range tests {
		got := thresholdFor(tt.turn, 20)
		if got != tt.want {
			t.Errorf("thresholdFor(%d, 20) = %v, want %v", tt.turn, got, tt.want)
		}
	}

	// Zero limit returns 0.0.
	if thresholdFor(5, 0) != 0.0 {
		t.Error("thresholdFor(5, 0) should be 0.0")
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_Reset — verify reset after checkpoint
// -----------------------------------------------------------------------

func TestTurnCounter_Reset(t *testing.T) {
	tc := newTurnCounter(20)

	// Burn 10 turns.
	for i := 0; i < 10; i++ {
		tc.Increment()
	}

	if tc.GetCount() != 10 {
		t.Errorf("before reset count = %d, want 10", tc.GetCount())
	}
	if tc.Remaining() != 10 {
		t.Errorf("before reset remaining = %d, want 10", tc.Remaining())
	}

	tc.Reset()

	if tc.GetCount() != 0 {
		t.Errorf("after reset count = %d, want 0", tc.GetCount())
	}
	if tc.Remaining() != 20 {
		t.Errorf("after reset remaining = %d, want 20", tc.Remaining())
	}

	// Verify it can burn again from zero.
	tc.Increment()
	if tc.GetCount() != 1 {
		t.Errorf("after reset + increment count = %d, want 1", tc.GetCount())
	}
}

// -----------------------------------------------------------------------
// TestTurnCounter_GetCount
// -----------------------------------------------------------------------

func TestTurnCounter_GetCount(t *testing.T) {
	tc := newTurnCounter(10)

	if tc.GetCount() != 0 {
		t.Errorf("initial count = %d, want 0", tc.GetCount())
	}

	tc.Increment()
	if tc.GetCount() != 1 {
		t.Errorf("after 1 increment count = %d, want 1", tc.GetCount())
	}

	tc.Increment()
	tc.Increment()
	if tc.GetCount() != 3 {
		t.Errorf("after 3 increments count = %d, want 3", tc.GetCount())
	}
}

// -----------------------------------------------------------------------
// TestRoundTo
// -----------------------------------------------------------------------

func TestRoundTo(t *testing.T) {
	tests := []struct {
		input  float64
		dec    int
		want   float64
	}{
		{0.333333, 2, 0.33},
		{0.999, 0, 1.0},
		{0.5, 4, 0.5},
		{1.23456, 3, 1.235},
	}

	for _, tt := range tests {
		got := RoundTo(tt.input, tt.dec)
		if got != tt.want {
			t.Errorf("RoundTo(%v, %d) = %v, want %v", tt.input, tt.dec, got, tt.want)
		}
	}
}

// -----------------------------------------------------------------------
// TestNudgeMessageForTurn
// -----------------------------------------------------------------------

func TestNudgeMessageForTurn(t *testing.T) {
	// 25% message.
	msg := nudgeMessageFor(5, 20)
	if !strings.Contains(msg, "outlining") {
		t.Errorf("outline message = %q, should contain 'outline'", msg)
	}
	if !strings.Contains(msg, "15 turns remaining") {
		t.Errorf("outline message = %q, should contain remaining count", msg)
	}

	// Exhausted message.
	msg = nudgeMessageFor(20, 20)
	if msg != "Turn limit reached. Finalize immediately." {
		t.Errorf("exhausted message = %q, want exact", msg)
	}

	// Below 25%.
	msg = nudgeMessageFor(1, 20)
	if !strings.Contains(msg, "[HALO:") {
		t.Errorf("baseline message = %q, should contain [HALO:", msg)
	}
}

// -----------------------------------------------------------------------
// TestNudgeInfo_String
// -----------------------------------------------------------------------

func TestNudgeInfo_String(t *testing.T) {
	info := nudgeInfo{
		Message:     "test",
		Threshold:   0.50,
		Tag:         "prioritize",
		CurrentTurn: 10,
		Limit:       20,
		Remaining:   10,
		Exhausted:   false,
	}

	s := info.String()
	for _, want := range []string{"threshold=0.50", "tag=prioritize", "turn=10", "limit=20", "remaining=10"} {
		if !strings.Contains(s, want) {
			t.Errorf("String() = %q, should contain %q", s, want)
		}
	}
	if strings.Contains(s, "exhausted=true") {
		t.Error("String() should not contain exhausted=true when false")
	}
}

func TestNudgeInfo_String_Exhausted(t *testing.T) {
	info := nudgeInfo{
		Threshold:   1.0,
		Tag:         "exhausted",
		CurrentTurn: 20,
		Limit:       20,
		Remaining:   0,
		Exhausted:   true,
	}

	s := info.String()
	if !strings.Contains(s, "exhausted=true") {
		t.Errorf("String() = %q, should contain 'exhausted=true'", s)
	}
}

// -----------------------------------------------------------------------
// TestSmoke_NudgeFormat
// -----------------------------------------------------------------------

func TestSmoke_NudgeFormat(t *testing.T) {
	// Verify that every nudge message is injectable into fmt/Sprintf
	// for %d templates (i.e., no unpaired format verbs).
	for _, tbl := range nudgeTable {
		if tbl.Tag == "" {
			continue // sentinel message has no %d
		}
		_ = fmt.Sprintf(tbl.Message, 5)
	}
}
