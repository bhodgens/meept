package queue

import (
	"database/sql"
	"encoding/json"
	"io"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite" //nolint:revive // blank import for side effects
)

// newInteractiveTestStore creates a Store on a fresh file-backed DB and runs
// the full migration path, mirroring production boot.
func newInteractiveTestStore(t *testing.T) *Store {
	t.Helper()
	store, err := NewStore(filepath.Join(t.TempDir(), "queue.db"), discardLogger())
	if err != nil {
		t.Fatalf("NewStore failed: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// interactiveSeedJob builds a pending job with explicit id, priority,
// interactivity and creation time so ordering assertions are deterministic.
func interactiveSeedJob(id string, agentID string, pri Priority, interactive bool, createdAt time.Time) *Job {
	return &Job{
		ID:          id,
		AgentID:     agentID,
		Type:        JobTypeOneOff,
		Priority:    pri,
		State:       StatePending,
		Payload:     json.RawMessage(`{}`),
		MaxRetries:  3,
		Interactive: interactive,
		CreatedAt:   createdAt,
		UpdatedAt:   createdAt,
	}
}

func seedJobs(t *testing.T, store *Store, jobs ...*Job) {
	t.Helper()
	for _, job := range jobs {
		if err := store.Insert(job); err != nil {
			t.Fatalf("seed insert %s: %v", job.ID, err)
		}
	}
}

func claimIDs(t *testing.T, store *Store, n int, agentID string) []string {
	t.Helper()
	var got []string
	for i := 0; i < n; i++ {
		var (
			job *Job
			err error
		)
		if agentID == "" {
			job, err = store.ClaimNext("worker-1", nil)
		} else {
			job, err = store.ClaimNextForAgent("worker-1", nil, agentID)
		}
		if err != nil {
			if i == 0 {
				t.Fatalf("first claim failed: %v", err)
			}
			t.Fatalf("claim %d failed: %v", i+1, err)
		}
		got = append(got, job.ID)
	}
	return got
}

// TestFreshDB_HasInteractiveColumnAndClaimIndex pins Task 1's fresh-schema
// requirements: the column exists and the claim-covering index is created.
func TestFreshDB_HasInteractiveColumnAndClaimIndex(t *testing.T) {
	store := newInteractiveTestStore(t)
	db := store.DB()

	var colName string
	err := db.QueryRow(`SELECT name FROM pragma_table_info('jobs') WHERE name = 'interactive'`).Scan(&colName)
	if err != nil {
		t.Fatalf("interactive column missing on fresh jobs table: %v", err)
	}

	var idxName string
	err = db.QueryRow(`SELECT name FROM sqlite_master WHERE type = 'index' AND name = 'idx_jobs_claim'`).Scan(&idxName)
	if err != nil {
		t.Fatalf("idx_jobs_claim missing on fresh schema: %v", err)
	}
}

// TestMigration_PreInteractiveDatabase upgrades a database created with the
// PRE-interactive jobs shape (the shape shipped before tree 04 leaf 02) and
// asserts the column is added with existing rows backfilled to 0.
func TestMigration_PreInteractiveDatabase(t *testing.T) {
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	// The pre-interactive jobs table: today's base shape WITHOUT interactive.
	oldSchema := `
	CREATE TABLE jobs (
		id            TEXT PRIMARY KEY,
		task_id       TEXT,
		agent_id      TEXT,
		type          TEXT NOT NULL,
		priority      INTEGER DEFAULT 2,
		state         TEXT DEFAULT 'pending',
		payload       TEXT NOT NULL,
		required_caps TEXT DEFAULT '[]',
		max_retries   INTEGER DEFAULT 3,
		retry_count   INTEGER DEFAULT 0,
		claimed_by    TEXT,
		result        TEXT,
		error         TEXT,
		created_at    TEXT NOT NULL,
		updated_at    TEXT NOT NULL,
		due_at        TEXT,
		next_retry_at TEXT
	);`
	if _, err := db.Exec(oldSchema); err != nil {
		t.Fatalf("old schema failed: %v", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := db.Exec(`INSERT INTO jobs (id, type, priority, state, payload, created_at, updated_at)
		VALUES ('legacy-1', 'one_off', 3, 'pending', '{}', ?, ?)`, now, now); err != nil {
		t.Fatalf("legacy row insert failed: %v", err)
	}

	// Run the store's migration path against the old database.
	store := &Store{db: db, logger: discardLogger()}
	if err := store.migrate(); err != nil {
		t.Fatalf("migrate on pre-interactive db failed: %v", err)
	}

	var colName string
	if err := db.QueryRow(`SELECT name FROM pragma_table_info('jobs') WHERE name = 'interactive'`).Scan(&colName); err != nil {
		t.Fatalf("interactive column not added by migration: %v", err)
	}

	// Existing row backfilled to 0 and readable through the normal scan path.
	job, err := store.GetByID("legacy-1")
	if err != nil {
		t.Fatalf("GetByID on migrated legacy row: %v", err)
	}
	if job.Interactive {
		t.Errorf("legacy row Interactive = true, want false (backfilled 0)")
	}

	// Migration is idempotent.
	if err := store.migrate(); err != nil {
		t.Fatalf("second migrate should be a no-op: %v", err)
	}
}

// TestJob_InteractiveRoundTrip proves Interactive survives insert + scan.
func TestJob_InteractiveRoundTrip(t *testing.T) {
	store := newInteractiveTestStore(t)

	flagged := interactiveSeedJob("rt-true", "", PriorityNormal, true, time.Now().UTC())
	if err := store.Insert(flagged); err != nil {
		t.Fatalf("insert flagged: %v", err)
	}
	plain := interactiveSeedJob("rt-false", "", PriorityNormal, false, time.Now().UTC())
	if err := store.Insert(plain); err != nil {
		t.Fatalf("insert plain: %v", err)
	}

	got, err := store.GetByID("rt-true")
	if err != nil {
		t.Fatalf("get flagged: %v", err)
	}
	if !got.Interactive {
		t.Errorf("flagged job round-tripped as Interactive=false")
	}

	got, err = store.GetByID("rt-false")
	if err != nil {
		t.Fatalf("get plain: %v", err)
	}
	if got.Interactive {
		t.Errorf("plain job round-tripped as Interactive=true")
	}
}

// TestClaimOrdering_InteractiveFirst is the leaf Task 2 table: (a) interactive
// beats older higher-priority background; (b) among interactive, priority then
// FIFO; (c) background-only order is regression-identical; (d) both claim
// entry points honor the ordering.
func TestClaimOrdering_InteractiveFirst(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name string
		seed func() []*Job
		// claimPath selects the claim entry point under test.
		claimPath string
		agentID   string
		wantOrder []string
	}{
		{
			name: "interactive beats older higher-priority background",
			seed: func() []*Job {
				return []*Job{
					interactiveSeedJob("bg-high", "", PriorityHigh, false, base),
					interactiveSeedJob("ia-new", "", PriorityNormal, true, base.Add(time.Second)),
				}
			},
			claimPath: "ClaimNext",
			wantOrder: []string{"ia-new", "bg-high"},
		},
		{
			name: "two interactive: priority then FIFO",
			seed: func() []*Job {
				return []*Job{
					interactiveSeedJob("ia-low", "", PriorityLow, true, base),
					interactiveSeedJob("ia-high", "", PriorityHigh, true, base.Add(time.Second)),
				}
			},
			claimPath: "ClaimNext",
			wantOrder: []string{"ia-high", "ia-low"},
		},
		{
			name: "background-only order regression-identical",
			seed: func() []*Job {
				return []*Job{
					interactiveSeedJob("bg-high", "", PriorityHigh, false, base.Add(2 * time.Second)),
					interactiveSeedJob("bg-norm-1", "", PriorityNormal, false, base),
					interactiveSeedJob("bg-norm-2", "", PriorityNormal, false, base.Add(time.Second)),
				}
			},
			claimPath: "ClaimNext",
			wantOrder: []string{"bg-high", "bg-norm-1", "bg-norm-2"},
		},
		{
			name: "ClaimNextForAgent honors interactive first",
			seed: func() []*Job {
				return []*Job{
					interactiveSeedJob("bg-high", "", PriorityHigh, false, base),
					interactiveSeedJob("ia-new", "coder", PriorityNormal, true, base.Add(time.Second)),
				}
			},
			claimPath: "ClaimNextForAgent",
			agentID:   "coder",
			wantOrder: []string{"ia-new", "bg-high"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := newInteractiveTestStore(t)
			seedJobs(t, store, tt.seed()...)

			got := claimIDs(t, store, len(tt.wantOrder), tt.agentID)
			for i, want := range tt.wantOrder {
				if got[i] != want {
					t.Fatalf("claim order: got %v, want %v (mismatch at %d: got %s want %s)",
						got, tt.wantOrder, i, got[i], want)
				}
			}
			_ = tt.claimPath // both paths exercised via claimIDs' agentID switch
		})
	}
}

// TestClaimOrdering_AffinityRegression pins the store.go:379 affinity CASE:
// among SAME-interactivity jobs, agent affinity still precedes priority.
// It also pins the documented cross-agent semantics: an interactive job NOT
// targeted at the claiming agent still beats the agent's own targeted
// background job (interactive leads affinity, per D11 user-first intent).
func TestClaimOrdering_AffinityRegression(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	t.Run("affinity beats priority among background jobs", func(t *testing.T) {
		store := newInteractiveTestStore(t)
		seedJobs(t, store,
			interactiveSeedJob("bg-unassigned-high", "", PriorityHigh, false, base),
			interactiveSeedJob("bg-coder-normal", "coder", PriorityNormal, false, base.Add(time.Second)),
		)

		got := claimIDs(t, store, 2, "coder")
		want := []string{"bg-coder-normal", "bg-unassigned-high"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("affinity regression: got %v, want %v", got, want)
			}
		}
	})

	t.Run("interactive leads affinity across agents", func(t *testing.T) {
		store := newInteractiveTestStore(t)
		seedJobs(t, store,
			// coder's own targeted background job, highest priority, oldest.
			interactiveSeedJob("bg-coder-urgent", "coder", PriorityUrgent, false, base),
			// An interactive job from another session, unassigned, low priority.
			interactiveSeedJob("ia-any", "", PriorityLow, true, base.Add(time.Second)),
		)

		got := claimIDs(t, store, 2, "coder")
		want := []string{"ia-any", "bg-coder-urgent"}
		for i := range want {
			if got[i] != want[i] {
				t.Fatalf("interactive should lead affinity: got %v, want %v", got, want)
			}
		}
	})
}

// TestListPending_InteractiveOrdering covers the requeue/peek variants the
// leaf calls out: ListByState (the slow-path claim source) and ListByAgentID
// must surface interactive-first pending ordering.
func TestListPending_InteractiveOrdering(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	store := newInteractiveTestStore(t)
	seedJobs(t, store,
		interactiveSeedJob("bg-high", "", PriorityHigh, false, base),
		interactiveSeedJob("ia-new", "", PriorityNormal, true, base.Add(time.Second)),
		interactiveSeedJob("ia-coder", "coder", PriorityLow, true, base.Add(2*time.Second)),
	)

	byState, err := store.ListByState(StatePending, 10)
	if err != nil {
		t.Fatalf("ListByState: %v", err)
	}
	wantState := []string{"ia-new", "ia-coder", "bg-high"}
	for i := range wantState {
		if byState[i].ID != wantState[i] {
			t.Fatalf("ListByState order: got %v, want %v", ids(byState), wantState)
		}
	}

	byAgent, err := store.ListByAgentID("coder", 10)
	if err != nil {
		t.Fatalf("ListByAgentID: %v", err)
	}
	if len(byAgent) != 1 || byAgent[0].ID != "ia-coder" {
		t.Fatalf("ListByAgentID: got %v, want [ia-coder]", ids(byAgent))
	}
}

func ids(jobs []*Job) []string {
	out := make([]string, len(jobs))
	for i, j := range jobs {
		out[i] = j.ID
	}
	return out
}

// TestGetStats_InteractivePending adds the nice-to-have interactive split to
// queue statistics.
func TestGetStats_InteractivePending(t *testing.T) {
	base := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)

	store := newInteractiveTestStore(t)
	seedJobs(t, store,
		interactiveSeedJob("bg-1", "", PriorityNormal, false, base),
		interactiveSeedJob("ia-1", "", PriorityNormal, true, base.Add(time.Second)),
	)

	stats, err := store.GetStats()
	if err != nil {
		t.Fatalf("GetStats: %v", err)
	}
	if stats.ByInteractive[true] != 1 || stats.ByInteractive[false] != 1 {
		t.Errorf("ByInteractive = %v, want map[true:1 false:1]", stats.ByInteractive)
	}
}
