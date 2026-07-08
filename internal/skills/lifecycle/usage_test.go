package lifecycle

import (
	"context"
	"path/filepath"
	"testing"
)

// TestUsageTrackerRecordInjection verifies that RecordInjection increments
// the inject_count and returns the correct count via GetStats.
func TestUsageTrackerRecordInjection(t *testing.T) {
	tracker := newTestTracker(t)
	defer func() { _ = tracker.Close() }()

	for i := 0; i < 10; i++ {
		if err := tracker.RecordInjection("test-skill"); err != nil {
			t.Fatalf("RecordInjection[%d] failed: %v", i, err)
		}
	}

	stats, err := tracker.GetStats("test-skill")
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.InjectCount != 10 {
		t.Errorf("InjectCount = %d, want 10", stats.InjectCount)
	}
}

// TestUsageTrackerRecordOutcome verifies the behavioral acceptance criterion:
// after 10 RecordInjection + 8 RecordOutcome(Positive) + 2
// RecordOutcome(Negative), GetStats returns InjectCount=10, PositiveCount=8,
// NegativeCount=2, Effectiveness=0.8.
func TestUsageTrackerRecordOutcome(t *testing.T) {
	tracker := newTestTracker(t)
	defer func() { _ = tracker.Close() }()

	// 10 injections.
	for i := 0; i < 10; i++ {
		if err := tracker.RecordInjection("test-skill"); err != nil {
			t.Fatalf("RecordInjection[%d] failed: %v", i, err)
		}
	}

	// 8 positive outcomes.
	for i := 0; i < 8; i++ {
		if err := tracker.RecordOutcome("test-skill", OutcomePositive, "sess-1"); err != nil {
			t.Fatalf("RecordOutcome(Positive)[%d] failed: %v", i, err)
		}
	}

	// 2 negative outcomes.
	for i := 0; i < 2; i++ {
		if err := tracker.RecordOutcome("test-skill", OutcomeNegative, "sess-1"); err != nil {
			t.Fatalf("RecordOutcome(Negative)[%d] failed: %v", i, err)
		}
	}

	stats, err := tracker.GetStats("test-skill")
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.InjectCount != 10 {
		t.Errorf("InjectCount = %d, want 10", stats.InjectCount)
	}
	if stats.PositiveCount != 8 {
		t.Errorf("PositiveCount = %d, want 8", stats.PositiveCount)
	}
	if stats.NegativeCount != 2 {
		t.Errorf("NegativeCount = %d, want 2", stats.NegativeCount)
	}
	if stats.Effectiveness < 0.79 || stats.Effectiveness > 0.81 {
		t.Errorf("Effectiveness = %f, want ~0.8", stats.Effectiveness)
	}
}

// TestUsageTrackerUnknownSkill verifies that RecordOutcome on an unknown skill
// does NOT panic (upserts a row with inject_count=0).
func TestUsageTrackerUnknownSkill(t *testing.T) {
	tracker := newTestTracker(t)
	defer func() { _ = tracker.Close() }()

	// RecordOutcome on an unknown skill should not panic.
	err := tracker.RecordOutcome("unknown-skill", OutcomePositive, "sess-1")
	if err != nil {
		t.Fatalf("RecordOutcome on unknown skill failed: %v", err)
	}

	stats, err := tracker.GetStats("unknown-skill")
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.InjectCount != 0 {
		t.Errorf("InjectCount = %d, want 0 (outcomes on unknown skill should not increment inject_count)", stats.InjectCount)
	}
	if stats.PositiveCount != 1 {
		t.Errorf("PositiveCount = %d, want 1", stats.PositiveCount)
	}
}

// TestUsageTrackerGetAllStats verifies that GetAllStats returns all tracked skills.
func TestUsageTrackerGetAllStats(t *testing.T) {
	tracker := newTestTracker(t)
	defer func() { _ = tracker.Close() }()

	_ = tracker.RecordInjection("skill-a")
	_ = tracker.RecordInjection("skill-b")
	_ = tracker.RecordInjection("skill-b")

	all, err := tracker.GetAllStats()
	if err != nil {
		t.Fatalf("GetAllStats failed: %v", err)
	}

	if len(all) != 2 {
		t.Fatalf("expected 2 skills, got %d", len(all))
	}

	if all["skill-a"] == nil || all["skill-a"].InjectCount != 1 {
		t.Errorf("skill-a stats wrong: %+v", all["skill-a"])
	}
	if all["skill-b"] == nil || all["skill-b"].InjectCount != 2 {
		t.Errorf("skill-b stats wrong: %+v", all["skill-b"])
	}
}

// TestUsageTrackerGetLowPerformers verifies that GetLowPerformers returns
// only skills with inject_count >= minInjections AND effectiveness < threshold.
func TestUsageTrackerGetLowPerformers(t *testing.T) {
	tracker := newTestTracker(t)
	defer func() { _ = tracker.Close() }()

	// skill-low: 10 injections, all negative -> effectiveness 0.0
	for i := 0; i < 10; i++ {
		_ = tracker.RecordInjection("skill-low")
		_ = tracker.RecordOutcome("skill-low", OutcomeNegative, "s")
	}

	// skill-high: 10 injections, all positive -> effectiveness 1.0
	for i := 0; i < 10; i++ {
		_ = tracker.RecordInjection("skill-high")
		_ = tracker.RecordOutcome("skill-high", OutcomePositive, "s")
	}

	// skill-few: 2 injections, all negative -> effectiveness 0.0 (below minInjections)
	for i := 0; i < 2; i++ {
		_ = tracker.RecordInjection("skill-few")
		_ = tracker.RecordOutcome("skill-few", OutcomeNegative, "s")
	}

	low, err := tracker.GetLowPerformers(0.5, 5)
	if err != nil {
		t.Fatalf("GetLowPerformers failed: %v", err)
	}

	if len(low) != 1 {
		t.Fatalf("expected 1 low performer, got %d", len(low))
	}

	if low[0].SkillName != "skill-low" {
		t.Errorf("expected skill-low, got %s", low[0].SkillName)
	}
}

// TestOutcomeString verifies string representations.
func TestOutcomeString(t *testing.T) {
	tests := []struct {
		outcome Outcome
		want    string
	}{
		{OutcomePositive, "positive"},
		{OutcomeNegative, "negative"},
		{OutcomeNeutral, "neutral"},
	}

	for _, tc := range tests {
		if got := tc.outcome.String(); got != tc.want {
			t.Errorf("Outcome(%d).String() = %q, want %q", tc.outcome, got, tc.want)
		}
	}
}

// TestParseOutcome verifies string-to-Outcome conversion.
func TestParseOutcome(t *testing.T) {
	tests := []struct {
		input string
		want  Outcome
	}{
		{"positive", OutcomePositive},
		{"negative", OutcomeNegative},
		{"neutral", OutcomeNeutral},
		{"unknown", OutcomeNeutral},
		{"", OutcomeNeutral},
	}

	for _, tc := range tests {
		if got := ParseOutcome(tc.input); got != tc.want {
			t.Errorf("ParseOutcome(%q) = %d, want %d", tc.input, got, tc.want)
		}
	}
}

// TestUsageTrackerImpl_RecordsAndReturnsLowMatchQueries verifies the Pass D
// data source: RecordLowMatchQuery upserts accumulate count, and
// GetLowMatchQueries returns them ranked by descending count, filtered by
// maxScore. This test is mandated by plan Task 5.1 Step 1.
func TestUsageTrackerImpl_RecordsAndReturnsLowMatchQueries(t *testing.T) {
	tracker := newTestTracker(t)
	defer func() { _ = tracker.Close() }()

	ctx := context.Background()
	if err := tracker.RecordLowMatchQuery(ctx, "debug goroutine race condition", 0.42); err != nil {
		t.Fatalf("RecordLowMatchQuery: %v", err)
	}
	// Repeat the same query — count should increment.
	if err := tracker.RecordLowMatchQuery(ctx, "debug goroutine race condition", 0.42); err != nil {
		t.Fatalf("second: %v", err)
	}
	// Different query with lower best_score.
	if err := tracker.RecordLowMatchQuery(ctx, "deploy to kubernetes", 0.35); err != nil {
		t.Fatalf("third: %v", err)
	}

	got, err := tracker.GetLowMatchQueries(ctx, 0.5, 10)
	if err != nil {
		t.Fatalf("GetLowMatchQueries: %v", err)
	}
	if len(got) != 2 {
		t.Errorf("expected 2 distinct queries, got %d", len(got))
	}
	// First entry should be the duplicate (higher count).
	if got[0].Count != 2 {
		t.Errorf("expected count 2 on top entry, got %d", got[0].Count)
	}
	// Verify ordering: count DESC means the count-2 entry comes first.
	if got[0].Query != "debug goroutine race condition" {
		t.Errorf("expected top query to be the duplicated one, got %q", got[0].Query)
	}
	// Verify the second entry.
	if got[1].Count != 1 {
		t.Errorf("expected count 1 on second entry, got %d", got[1].Count)
	}
	if got[1].Query != "deploy to kubernetes" {
		t.Errorf("expected second query to be %q, got %q", "deploy to kubernetes", got[1].Query)
	}
	// Verify maxScore filter excludes queries at or above threshold.
	high, err := tracker.GetLowMatchQueries(ctx, 0.3, 10)
	if err != nil {
		t.Fatalf("GetLowMatchQueries(0.3): %v", err)
	}
	if len(high) != 0 {
		t.Errorf("expected 0 queries with maxScore=0.3 (both are >= 0.35), got %d", len(high))
	}
}

// newTestTracker creates a UsageTrackerImpl using a temp directory.
func newTestTracker(t *testing.T) *UsageTrackerImpl {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "skills.db")
	tracker, err := NewUsageTracker(dbPath, nil)
	if err != nil {
		t.Fatalf("NewUsageTracker failed: %v", err)
	}
	return tracker
}
