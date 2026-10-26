//go:build e2e

package migrateeq

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/metrics"
	"github.com/caimlas/meept/internal/migrate"
	"github.com/caimlas/meept/internal/plan"
	"github.com/caimlas/meept/internal/shadow"
	"github.com/caimlas/meept/internal/task"
)

// TestMetricsStoreFreshVsLegacySchemaEqual proves the metrics store's
// fresh-created schema is byte-identical to a migrated-up legacy schema.
func TestMetricsStoreFreshVsLegacySchemaEqual(t *testing.T) {
	freshPath := filepath.Join(t.TempDir(), "fresh.db")
	freshStore, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  freshPath,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore(fresh): %v", err)
	}
	freshSchema := dumpSchema(t, openRaw(t, freshPath))
	if err := freshStore.Close(); err != nil {
		t.Fatalf("close fresh: %v", err)
	}

	// Legacy: create the store fresh (stamps version), then strip the
	// registry-migrated objects and reset user_version to 0 — the exact
	// shape a pre-registry database arrives in.
	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyStore, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  legacyPath,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore(legacy seed): %v", err)
	}
	legacyRaw := openRaw(t, legacyPath)
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_llm_calls_session_ts`,
		`DROP INDEX IF EXISTS idx_dispatch_log_hash`,
		`DROP INDEX IF EXISTS idx_dispatch_log_outcome`,
		`ALTER TABLE llm_calls DROP COLUMN session_id`,
		`ALTER TABLE llm_calls DROP COLUMN reasoning_tokens`,
		`ALTER TABLE llm_calls DROP COLUMN cache_creation_tokens`,
		`ALTER TABLE dispatch_log DROP COLUMN input_hash`,
		`ALTER TABLE dispatch_log DROP COLUMN model`,
		`ALTER TABLE dispatch_log DROP COLUMN margin`,
		`ALTER TABLE dispatch_log DROP COLUMN turn_no`,
		`ALTER TABLE dispatch_log DROP COLUMN outcome`,
		`ALTER TABLE dispatch_log DROP COLUMN corrected_agent`,
		// model_performance.agent_id participates in the UNIQUE index,
		// so DROP COLUMN is rejected; a pre-registry database simply has
		// the old-shape table, which is recreated here.
		`DROP TABLE model_performance`,
		// Oldest known shape (pre-registry): no agent_id column and no
		// UNIQUE constraint (the UNIQUE gained agent_id only in the
		// current CREATE script; an ALTER can never change a
		// constraint, pre-registry code and the registry alike —
		// constraint migrations require a table-rebuild step).
		`CREATE TABLE model_performance (
			id              INTEGER PRIMARY KEY AUTOINCREMENT,
			model_id        TEXT NOT NULL,
			provider        TEXT NOT NULL DEFAULT '',
			total_requests  INTEGER NOT NULL DEFAULT 0,
			total_errors    INTEGER NOT NULL DEFAULT 0,
			avg_latency_ms  REAL NOT NULL DEFAULT 0,
			avg_tokens_in   REAL NOT NULL DEFAULT 0,
			avg_tokens_out  REAL NOT NULL DEFAULT 0,
			period_start    TEXT NOT NULL,
			period_end      TEXT NOT NULL,
			updated_at      TEXT NOT NULL DEFAULT (strftime('%Y-%m-%dT%H:%M:%SZ', 'now'))
		)`,
		`PRAGMA user_version = 0`,
	} {
		if _, err := legacyRaw.Exec(stmt); err != nil {
			t.Fatalf("legacy strip %q: %v", stmt, err)
		}
	}
	if err := legacyStore.Close(); err != nil {
		t.Fatalf("close legacy seed: %v", err)
	}

	// Reopen: the registry must migrate the legacy shape up.
	upgraded, err := metrics.NewStore(&metrics.StoreConfig{
		DatabasePath:  legacyPath,
		FlushInterval: time.Hour,
	})
	if err != nil {
		t.Fatalf("NewStore(upgrade): %v", err)
	}
	upgradedSchema := dumpSchema(t, openRaw(t, legacyPath))
	if err := upgraded.Close(); err != nil {
		t.Fatalf("close upgraded: %v", err)
	}

	if freshSchema != upgradedSchema {
		t.Fatalf("fresh vs migrated metrics schema differ:\n--- fresh ---\n%s\n--- migrated ---\n%s", freshSchema, upgradedSchema)
	}
}

// TestTaskStoreFreshVsLegacySchemaEqual covers the task store, whose
// database file is shared between Store (tasks) and StepStore
// (task_steps) under ONE contiguous registry history.
func TestTaskStoreFreshVsLegacySchemaEqual(t *testing.T) {
	freshPath := filepath.Join(t.TempDir(), "fresh.db")
	freshStore, err := task.NewStore(freshPath, nil)
	if err != nil {
		t.Fatalf("NewStore(fresh): %v", err)
	}
	freshSchema := dumpSchema(t, openRaw(t, freshPath))
	if err := freshStore.Close(); err != nil {
		t.Fatalf("close fresh: %v", err)
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyStore, err := task.NewStore(legacyPath, nil)
	if err != nil {
		t.Fatalf("NewStore(legacy seed): %v", err)
	}
	legacyRaw := openRaw(t, legacyPath)
	for _, stmt := range []string{
		// Only columns the CREATE scripts do NOT carry — the rest of
		// the guarded steps exist for databases older than the
		// current CREATE scripts, which this seed simulates by
		// dropping what the scripts would not recreate.
		`ALTER TABLE tasks DROP COLUMN goal_id`,
		`ALTER TABLE task_steps DROP COLUMN session_id`,
		`ALTER TABLE task_steps DROP COLUMN suggested_next_hint`,
		`PRAGMA user_version = 0`,
	} {
		if _, err := legacyRaw.Exec(stmt); err != nil {
			t.Fatalf("legacy strip %q: %v", stmt, err)
		}
	}
	if err := legacyStore.Close(); err != nil {
		t.Fatalf("close legacy seed: %v", err)
	}

	upgraded, err := task.NewStore(legacyPath, nil)
	if err != nil {
		t.Fatalf("NewStore(upgrade): %v", err)
	}
	upgradedSchema := dumpSchema(t, openRaw(t, legacyPath))
	if err := upgraded.Close(); err != nil {
		t.Fatalf("close upgraded: %v", err)
	}

	if freshSchema != upgradedSchema {
		t.Fatalf("fresh vs migrated task schema differ:\n--- fresh ---\n%s\n--- migrated ---\n%s", freshSchema, upgradedSchema)
	}
}

// TestPlanStoreFreshVsLegacySchemaEqual covers the plan store.
func TestPlanStoreFreshVsLegacySchemaEqual(t *testing.T) {
	freshPath := filepath.Join(t.TempDir(), "fresh.db")
	freshStore, err := plan.NewSQLiteStore(freshPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore(fresh): %v", err)
	}
	freshSchema := dumpSchema(t, openRaw(t, freshPath))
	if err := freshStore.Close(); err != nil {
		t.Fatalf("close fresh: %v", err)
	}

	legacyPath := filepath.Join(t.TempDir(), "legacy.db")
	legacyStore, err := plan.NewSQLiteStore(legacyPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore(legacy seed): %v", err)
	}
	legacyRaw := openRaw(t, legacyPath)
	for _, stmt := range []string{
		`ALTER TABLE plan_phases DROP COLUMN produces`,
		`ALTER TABLE plan_phases DROP COLUMN consumes`,
		`ALTER TABLE plan_phases DROP COLUMN task_id`,
		`ALTER TABLE plans DROP COLUMN synthesis_attempts`,
		`ALTER TABLE plans DROP COLUMN synthesis_lease_until`,
		`ALTER TABLE plans DROP COLUMN goal_id`,
		`PRAGMA user_version = 0`,
	} {
		if _, err := legacyRaw.Exec(stmt); err != nil {
			t.Fatalf("legacy strip %q: %v", stmt, err)
		}
	}
	if err := legacyStore.Close(); err != nil {
		t.Fatalf("close legacy seed: %v", err)
	}

	upgraded, err := plan.NewSQLiteStore(legacyPath, nil)
	if err != nil {
		t.Fatalf("NewSQLiteStore(upgrade): %v", err)
	}
	upgradedSchema := dumpSchema(t, openRaw(t, legacyPath))
	if err := upgraded.Close(); err != nil {
		t.Fatalf("close upgraded: %v", err)
	}

	if freshSchema != upgradedSchema {
		t.Fatalf("fresh vs migrated plan schema differ:\n--- fresh ---\n%s\n--- migrated ---\n%s", freshSchema, upgradedSchema)
	}
}

// TestShadowStoresFreshVsLegacySchemaEqual covers all three shadow stores
// (training, examples, adapters), which previously ran the schema_version
// ladder instead of the registry.
func TestShadowStoresFreshVsLegacySchemaEqual(t *testing.T) {
	freshDir := t.TempDir()
	freshTraining, err := shadow.NewSQLiteTrainingStore(filepath.Join(freshDir, "training.db"))
	if err != nil {
		t.Fatalf("NewSQLiteTrainingStore(fresh): %v", err)
	}
	freshExamples, err := shadow.NewSQLiteExamplesStore(filepath.Join(freshDir, "examples.db"))
	if err != nil {
		t.Fatalf("NewSQLiteExamplesStore(fresh): %v", err)
	}
	freshAdapters, err := shadow.NewSQLiteAdaptersStore(filepath.Join(freshDir, "adapters.db"))
	if err != nil {
		t.Fatalf("NewSQLiteAdaptersStore(fresh): %v", err)
	}
	freshTrainingSchema := dumpSchema(t, openRaw(t, filepath.Join(freshDir, "training.db")))
	freshExamplesSchema := dumpSchema(t, openRaw(t, filepath.Join(freshDir, "examples.db")))
	freshAdaptersSchema := dumpSchema(t, openRaw(t, filepath.Join(freshDir, "adapters.db")))
	if err := freshTraining.Close(); err != nil {
		t.Fatalf("close fresh training: %v", err)
	}
	if err := freshExamples.Close(); err != nil {
		t.Fatalf("close fresh examples: %v", err)
	}
	if err := freshAdapters.Close(); err != nil {
		t.Fatalf("close fresh adapters: %v", err)
	}

	legacyDir := t.TempDir()
	seedTraining, err := shadow.NewSQLiteTrainingStore(filepath.Join(legacyDir, "training.db"))
	if err != nil {
		t.Fatalf("NewSQLiteTrainingStore(seed): %v", err)
	}
	seedExamples, err := shadow.NewSQLiteExamplesStore(filepath.Join(legacyDir, "examples.db"))
	if err != nil {
		t.Fatalf("NewSQLiteExamplesStore(seed): %v", err)
	}
	seedAdapters, err := shadow.NewSQLiteAdaptersStore(filepath.Join(legacyDir, "adapters.db"))
	if err != nil {
		t.Fatalf("NewSQLiteAdaptersStore(seed): %v", err)
	}

	// Pre-registry shape: registry-migrated columns dropped and the
	// legacy schema_version bookkeeping removed (the ladder stamped it;
	// a database migrated by the OLD code path carries version rows, not
	// user_version).
	legacyTraining := openRaw(t, filepath.Join(legacyDir, "training.db"))
	for _, stmt := range []string{
		`DROP INDEX IF EXISTS idx_shadow_records_exported`,
		`ALTER TABLE shadow_records DROP COLUMN metadata_json`,
		`ALTER TABLE shadow_records DROP COLUMN exported_at`,
		`ALTER TABLE preference_pairs DROP COLUMN domain`,
		`ALTER TABLE preference_pairs DROP COLUMN task_type`,
		`ALTER TABLE preference_pairs DROP COLUMN routing_path`,
		`PRAGMA user_version = 0`,
	} {
		if _, err := legacyTraining.Exec(stmt); err != nil {
			t.Fatalf("legacy training strip %q: %v", stmt, err)
		}
	}
	legacyExamples := openRaw(t, filepath.Join(legacyDir, "examples.db"))
	for _, stmt := range []string{
		`ALTER TABLE fewshot_examples DROP COLUMN tags`,
		`ALTER TABLE fewshot_examples DROP COLUMN last_used_at`,
		`PRAGMA user_version = 0`,
	} {
		if _, err := legacyExamples.Exec(stmt); err != nil {
			t.Fatalf("legacy examples strip %q: %v", stmt, err)
		}
	}
	legacyAdapters := openRaw(t, filepath.Join(legacyDir, "adapters.db"))
	if _, err := legacyAdapters.Exec(`PRAGMA user_version = 0`); err != nil {
		t.Fatalf("legacy adapters strip: %v", err)
	}

	if err := seedTraining.Close(); err != nil {
		t.Fatalf("close seed training: %v", err)
	}
	if err := seedExamples.Close(); err != nil {
		t.Fatalf("close seed examples: %v", err)
	}
	if err := seedAdapters.Close(); err != nil {
		t.Fatalf("close seed adapters: %v", err)
	}

	upTraining, err := shadow.NewSQLiteTrainingStore(filepath.Join(legacyDir, "training.db"))
	if err != nil {
		t.Fatalf("NewSQLiteTrainingStore(upgrade): %v", err)
	}
	defer func() { _ = upTraining.Close() }()
	upExamples, err := shadow.NewSQLiteExamplesStore(filepath.Join(legacyDir, "examples.db"))
	if err != nil {
		t.Fatalf("NewSQLiteExamplesStore(upgrade): %v", err)
	}
	defer func() { _ = upExamples.Close() }()
	upAdapters, err := shadow.NewSQLiteAdaptersStore(filepath.Join(legacyDir, "adapters.db"))
	if err != nil {
		t.Fatalf("NewSQLiteAdaptersStore(upgrade): %v", err)
	}
	defer func() { _ = upAdapters.Close() }()

	if got := dumpSchema(t, openRaw(t, filepath.Join(legacyDir, "training.db"))); got != freshTrainingSchema {
		t.Fatalf("fresh vs migrated training schema differ:\n--- fresh ---\n%s\n--- migrated ---\n%s", freshTrainingSchema, got)
	}
	if got := dumpSchema(t, openRaw(t, filepath.Join(legacyDir, "examples.db"))); got != freshExamplesSchema {
		t.Fatalf("fresh vs migrated examples schema differ:\n--- fresh ---\n%s\n--- migrated ---\n%s", freshExamplesSchema, got)
	}
	if got := dumpSchema(t, openRaw(t, filepath.Join(legacyDir, "adapters.db"))); got != freshAdaptersSchema {
		t.Fatalf("fresh vs migrated adapters schema differ:\n--- fresh ---\n%s\n--- migrated ---\n%s", freshAdaptersSchema, got)
	}
}

// TestDownLevelBinaryRefusedByWiredStore proves the fail-loud contract
// through a wired store's dependency: stamp user_version beyond the
// registry's target, then reopen — the constructor must fail, never
// silently proceed.
func TestDownLevelBinaryRefusedByWiredStore(t *testing.T) {
	path := filepath.Join(t.TempDir(), "downlevel.db")
	store, err := task.NewStore(path, nil)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	raw := openRaw(t, path)
	if got := userVersion(t, raw); got == 0 {
		t.Fatalf("fresh store did not stamp user_version (got 0)")
	}
	if _, err := raw.Exec("PRAGMA user_version = 999"); err != nil {
		t.Fatalf("stamp future version: %v", err)
	}
	if err := store.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	_, err = task.NewStore(path, nil)
	if err == nil {
		t.Fatal("down-level open must fail, got nil")
	}
	if !errors.Is(err, migrate.ErrDownLevel) {
		t.Fatalf("error does not wrap ErrDownLevel: %v", err)
	}
}
