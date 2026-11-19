package activity

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func openTestExecutionStore(t *testing.T) *ExecutionStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "execution_runs.db")
	s, err := OpenExecutionStore(path, nil)
	if err != nil {
		t.Fatalf("OpenExecutionStore: %v", err)
	}
	t.Cleanup(func() {
		if err := s.Close(); err != nil {
			t.Errorf("close: %v", err)
		}
	})
	return s
}

func TestExecutionStore_AppendListRoundtrip(t *testing.T) {
	s := openTestExecutionStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	run := ExecutionRun{
		Tier: "bot", AgentID: "ci-bot", Status: "completed",
		Output: "green", TokensUsed: 42, DurationMS: 1500,
		StopReason: "end_turn", TriggerRef: "cron:ci", SessionID: "s1",
		CreatedAt: now,
	}
	if err := s.Append(ctx, run); err != nil {
		t.Fatalf("Append: %v", err)
	}
	runs, total, err := s.List(ctx, ExecutionQuery{})
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if total != 1 || len(runs) != 1 {
		t.Fatalf("total=%d len=%d, want 1/1", total, len(runs))
	}
	got := runs[0]
	if got.Tier != "bot" || got.AgentID != "ci-bot" || got.Status != "completed" ||
		got.TokensUsed != 42 || got.DurationMS != 1500 || got.StopReason != "end_turn" ||
		got.TriggerRef != "cron:ci" || got.SessionID != "s1" || got.Output != "green" {
		t.Errorf("roundtrip mismatch: %+v", got)
	}
	if !got.CreatedAt.Equal(now) {
		t.Errorf("CreatedAt = %v, want %v (RFC3339 second precision)", got.CreatedAt, now)
	}
}

func TestExecutionStore_ListFiltersAndOrder(t *testing.T) {
	s := openTestExecutionStore(t)
	ctx := context.Background()
	base := time.Now().UTC().Truncate(time.Second).Add(-3 * time.Minute)

	for i, id := range []string{"a", "b", "a"} {
		if err := s.Append(ctx, ExecutionRun{
			Tier: "employee", AgentID: id, Status: "completed",
			TokensUsed: i + 1, CreatedAt: base.Add(time.Duration(i) * time.Minute),
		}); err != nil {
			t.Fatalf("Append %d: %v", i, err)
		}
	}

	// Newest first, unfiltered.
	runs, total, err := s.List(ctx, ExecutionQuery{})
	if err != nil || total != 3 {
		t.Fatalf("List: %v total=%d", err, total)
	}
	if runs[0].TokensUsed != 3 {
		t.Errorf("newest-first order broken: first tokens=%d, want 3", runs[0].TokensUsed)
	}

	// agent_id filter: total counts matching rows before limit.
	runs, total, err = s.List(ctx, ExecutionQuery{AgentID: "a", Limit: 1})
	if err != nil {
		t.Fatalf("filtered List: %v", err)
	}
	if total != 2 || len(runs) != 1 {
		t.Fatalf("filter total=%d len=%d, want 2/1", total, len(runs))
	}

	// since filter excludes the oldest row.
	runs, total, err = s.List(ctx, ExecutionQuery{Since: base.Add(30 * time.Second)})
	if err != nil || total != 2 || len(runs) != 2 {
		t.Fatalf("since filter: %v total=%d len=%d, want 2/2", err, total, len(runs))
	}
}

func TestExecutionStore_HistogramBuckets(t *testing.T) {
	s := openTestExecutionStore(t)
	ctx := context.Background()
	now := time.Now().UTC().Truncate(time.Second)

	// One run 5 minutes ago (cell 7: last 7.5-minute window), one 50
	// minutes ago (cell 1), one 2 hours ago (outside the hour, ignored).
	for _, tc := range []struct {
		ago    time.Duration
		tokens int
	}{
		{5 * time.Minute, 100},
		{50 * time.Minute, 7},
		{2 * time.Hour, 999},
	} {
		if err := s.Append(ctx, ExecutionRun{
			Tier: "bot", AgentID: "h", Status: "completed",
			TokensUsed: tc.tokens, CreatedAt: now.Add(-tc.ago),
		}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}

	cells, err := s.Histogram(ctx, "h", now)
	if err != nil {
		t.Fatalf("Histogram: %v", err)
	}
	if cells[7] != 100 {
		t.Errorf("cell 7 = %d, want 100 (run 5m ago)", cells[7])
	}
	if cells[1] != 7 {
		t.Errorf("cell 1 = %d, want 7 (run 50m ago)", cells[1])
	}
	var sum int
	for _, c := range cells {
		sum += c
	}
	if sum != 107 {
		t.Errorf("sum = %d, want 107 (2h-old run must be excluded)", sum)
	}
}

func TestExecutionStore_NilSafe(t *testing.T) {
	var s *ExecutionStore
	if _, _, err := s.List(context.Background(), ExecutionQuery{}); err == nil {
		t.Error("nil store List must error, not panic")
	}
	if err := s.Append(context.Background(), ExecutionRun{}); err == nil {
		t.Error("nil store Append must error, not panic")
	}
	if err := s.Close(); err != nil {
		t.Errorf("nil store Close: %v", err)
	}
}
