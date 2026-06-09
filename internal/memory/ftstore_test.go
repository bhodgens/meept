package memory

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mustNewTestFTSStore creates an initialized SQLiteFTSStore for testing.
func mustNewTestFTSStore(t *testing.T, tmpDir string) *SQLiteFTSStore {
	t.Helper()
	cfg := FTSConfig{
		TableName:     "test_items",
		FTS5Table:     "test_fts",
		CategoryField: "category",
		DataDir:       filepath.Join(tmpDir, "fts"),
		Schema: []string{
			`CREATE TABLE IF NOT EXISTS test_items (
				id            TEXT PRIMARY KEY,
				content       TEXT NOT NULL,
				category      TEXT NOT NULL DEFAULT 'general',
				metadata_json TEXT NOT NULL DEFAULT '{}',
				created_at    TEXT NOT NULL
			)`,
			`CREATE VIRTUAL TABLE IF NOT EXISTS test_fts
			USING fts5(content, category, content='test_items', content_rowid='rowid')`,
		},
		Triggers: []string{
			`CREATE TRIGGER IF NOT EXISTS test_fts_ai AFTER INSERT ON test_items BEGIN
				INSERT INTO test_fts(rowid, content, category)
				VALUES (new.rowid, new.content, new.category);
			END`,
			`CREATE TRIGGER IF NOT EXISTS test_fts_ad AFTER DELETE ON test_items BEGIN
				INSERT INTO test_fts(test_fts, rowid, content, category)
				VALUES ('delete', old.rowid, old.content, old.category);
			END`,
			`CREATE TRIGGER IF NOT EXISTS test_fts_au AFTER UPDATE ON test_items BEGIN
				INSERT INTO test_fts(test_fts, rowid, content, category)
				VALUES ('delete', old.rowid, old.content, old.category);
				INSERT INTO test_fts(rowid, content, category)
				VALUES (new.rowid, new.content, new.category);
			END`,
		},
	}

	store, err := NewSQLiteFTSStore(cfg, slog.Default())
	require.NoError(t, err, "NewSQLiteFTSStore")

	ctx := context.Background()
	require.NoError(t, store.Initialize(ctx), "Initialize")

	return store
}

// insertTestItem inserts a row directly for testing purposes.
func insertTestItem(t *testing.T, store *SQLiteFTSStore, id, content, category string) {
	t.Helper()
	ctx := context.Background()
	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	err := store.Store(ctx,
		`INSERT INTO test_items (id, content, category, metadata_json, created_at)
		 VALUES (?, ?, ?, '{}', ?)`,
		id, content, category, nowISO,
	)
	require.NoError(t, err, "insert test item %s", id)
}

// ---------------------------------------------------------------------------
// Tests: Construction and Initialization
// ---------------------------------------------------------------------------

func TestNewSQLiteFTSStore_NilLogger(t *testing.T) {
	cfg := FTSConfig{
		TableName: "dummy",
		DataDir:   t.TempDir(),
	}
	store, err := NewSQLiteFTSStore(cfg, nil)
	require.NoError(t, err, "NewSQLiteFTSStore with nil logger")
	require.NotNil(t, store)
}

func TestSQLiteFTSStore_Initialize(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	assert.True(t, store.Initialized(), "expected store to be initialized")
}

func TestSQLiteFTSStore_Initialize_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	ctx := context.Background()
	require.NoError(t, store.Initialize(ctx), "second Initialize should be idempotent")
}

func TestSQLiteFTSStore_HasFTS5(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	// HasFTS5 and HasFTS5Public should return consistent values.
	hasPrivate := store.HasFTS5()
	hasPublic := store.HasFTS5Public()
	assert.Equal(t, hasPrivate, hasPublic, "HasFTS5 and HasFTS5Public should match")
	t.Logf("HasFTS5=%v (FTS5 availability depends on SQLite build)", hasPrivate)
}

// ---------------------------------------------------------------------------
// Tests: Store / Delete / DeleteByIDs
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_Store(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	insertTestItem(t, store, "id-1", "hello world", "general")

	count, err := store.Count(context.Background(), "test_items")
	require.NoError(t, err, "Count")
	assert.Equal(t, 1, count)
}

func TestSQLiteFTSStore_Delete(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	insertTestItem(t, store, "id-1", "hello world", "general")

	err := store.Delete(context.Background(), "DELETE FROM test_items WHERE id = ?", "id-1")
	require.NoError(t, err, "Delete")

	count, _ := store.Count(context.Background(), "test_items")
	if count != 0 {
		t.Errorf("expected count 0 after delete, got %d", count)
	}
}

func TestSQLiteFTSStore_DeleteByIDs(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	insertTestItem(t, store, "id-1", "item 1", "general")
	insertTestItem(t, store, "id-2", "item 2", "general")
	insertTestItem(t, store, "id-3", "item 3", "general")

	deleted, err := store.DeleteByIDs(context.Background(), "test_items", []string{"id-1", "id-3"})
	require.NoError(t, err, "DeleteByIDs")
	assert.Equal(t, 2, deleted)

	count, _ := store.Count(context.Background(), "test_items")
	assert.Equal(t, 1, count)
}

func TestSQLiteFTSStore_DeleteByIDs_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	deleted, err := store.DeleteByIDs(context.Background(), "test_items", []string{})
	require.NoError(t, err, "DeleteByIDs empty")
	if deleted != 0 {
		t.Errorf("expected 0 deleted for empty list, got %d", deleted)
	}
}

// ---------------------------------------------------------------------------
// Tests: Count / Timestamps
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_Count(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	insertTestItem(t, store, "id-1", "a", "general")
	insertTestItem(t, store, "id-2", "b", "code")

	count, err := store.Count(context.Background(), "test_items")
	require.NoError(t, err, "Count")
	assert.Equal(t, 2, count)
}

func TestSQLiteFTSStore_GetOldestTimestamp_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	ts, err := store.GetOldestTimestamp(context.Background(), "test_items")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, ts, "expected nil for empty store")
}

func TestSQLiteFTSStore_GetNewestTimestamp_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	ts, err := store.GetNewestTimestamp(context.Background(), "test_items")
	require.ErrorIs(t, err, ErrNotFound)
	assert.Nil(t, ts, "expected nil for empty store")
}

func TestSQLiteFTSStore_Timestamps_WithData(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	insertTestItem(t, store, "id-1", "first", "general")
	time.Sleep(50 * time.Millisecond)
	insertTestItem(t, store, "id-2", "second", "general")

	oldest, err := store.GetOldestTimestamp(ctx, "test_items")
	require.NoError(t, err, "GetOldestTimestamp")
	require.NotNil(t, oldest, "expected non-nil oldest timestamp")

	newest, err := store.GetNewestTimestamp(ctx, "test_items")
	require.NoError(t, err, "GetNewestTimestamp")
	require.NotNil(t, newest, "expected non-nil newest timestamp")

	if !oldest.Before(*newest) {
		t.Errorf("oldest %v should be before newest %v", oldest, newest)
	}
}

// ---------------------------------------------------------------------------
// Tests: FindDuplicateGroups
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_FindDuplicateGroups(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	longContent := "This is a long piece of content that exceeds the minimum threshold for duplicate detection in our tests"
	insertTestItem(t, store, "id-1", longContent, "general")
	insertTestItem(t, store, "id-2", longContent, "general")
	insertTestItem(t, store, "id-3", longContent, "code")
	insertTestItem(t, store, "id-4", "Unique content that is also long enough to be considered", "general")

	groups, err := store.FindDuplicateGroups(context.Background(), "test_items", 50)
	require.NoError(t, err, "FindDuplicateGroups")

	require.Len(t, groups, 1)
	if len(groups[0]) != 3 {
		t.Errorf("expected 3 IDs in duplicate group, got %d", len(groups[0]))
	}
}

func TestSQLiteFTSStore_FindDuplicateGroups_NoDuplicates(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	insertTestItem(t, store, "id-1", "Alpha bravo charlie delta echo", "general")
	insertTestItem(t, store, "id-2", "Foxtrot golf hotel india juliet", "code")

	groups, err := store.FindDuplicateGroups(context.Background(), "test_items", 10)
	require.NoError(t, err, "FindDuplicateGroups")
	if len(groups) != 0 {
		t.Errorf("expected 0 groups for unique content, got %d", len(groups))
	}
}

// ---------------------------------------------------------------------------
// Tests: ScanResults (shared row scanning)
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_ScanResults_NoRank(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	insertTestItem(t, store, "id-1", "hello world", "general")

	db := store.GetDB()

	rows, err := db.QueryContext(ctx,
		"SELECT id, content, category, metadata_json, created_at FROM test_items")
	require.NoError(t, err, "Query")
	defer rows.Close()

	results, err := store.ScanResults(rows, false, ScanRowConfig{
		MemoryType: MemoryTypeEpisodic,
		SourceFmt:  "episodic",
	})
	require.NoError(t, err, "ScanResults")

	require.Len(t, results, 1)

	r := results[0]
	if r.Memory.ID != "id-1" {
		t.Errorf("expected ID 'id-1', got %q", r.Memory.ID)
	}
	if r.Memory.Content != "hello world" {
		t.Errorf("expected content 'hello world', got %q", r.Memory.Content)
	}
	if r.Memory.Type != MemoryTypeEpisodic {
		t.Errorf("expected type episodic, got %q", r.Memory.Type)
	}
	if r.Memory.Category != "general" {
		t.Errorf("expected category 'general', got %q", r.Memory.Category)
	}
	if r.Source != "episodic" {
		t.Errorf("expected source 'episodic', got %q", r.Source)
	}
	if r.Memory.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}

func TestSQLiteFTSStore_ScanResults_WithRank(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	if !store.HasFTS5() {
		t.Skip("FTS5 not available in this SQLite build, skipping rank scan test")
	}

	insertTestItem(t, store, "id-1", "python programming language", "code")
	insertTestItem(t, store, "id-2", "go programming basics", "code")

	db := store.GetDB()

	rows, err := db.QueryContext(ctx, `
		SELECT m.id, m.content, m.category, m.metadata_json, m.created_at, f.rank
		FROM test_fts f
		JOIN test_items m ON m.rowid = f.rowid
		WHERE test_fts MATCH ?
		ORDER BY f.rank
	`, "programming", 10)
	require.NoError(t, err, "FTS query")
	defer rows.Close()

	results, err := store.ScanResults(rows, true, ScanRowConfig{
		MemoryType: MemoryTypeTask,
		SourceFmt:  "task:%s",
	})
	require.NoError(t, err, "ScanResults")

	require.Len(t, results, 2)

	for _, r := range results {
		if r.Memory.Type != MemoryTypeTask {
			t.Errorf("expected type task, got %q", r.Memory.Type)
		}
		// Source should be "task:code" since category is "code"
		if r.Source != "task:code" {
			t.Errorf("expected source 'task:code', got %q", r.Source)
		}
		if r.Memory.CreatedAt.IsZero() {
			t.Error("expected non-zero CreatedAt")
		}
	}
}

func TestSQLiteFTSStore_ScanResults_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	db := store.GetDB()

	rows, err := db.QueryContext(ctx,
		"SELECT id, content, category, metadata_json, created_at FROM test_items WHERE id = 'nonexistent'")
	require.NoError(t, err, "Query")
	defer rows.Close()

	results, err := store.ScanResults(rows, false, ScanRowConfig{
		MemoryType: MemoryTypeEpisodic,
		SourceFmt:  "episodic",
	})
	require.NoError(t, err, "ScanResults")
	if len(results) != 0 {
		t.Errorf("expected 0 results, got %d", len(results))
	}
}

// ---------------------------------------------------------------------------
// Tests: Guard clauses (not initialized)
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_OperationsBeforeInit(t *testing.T) {
	cfg := FTSConfig{
		TableName: "uninit_test",
		DataDir:   t.TempDir(),
		Schema: []string{
			"CREATE TABLE IF NOT EXISTS uninit_test (id TEXT PRIMARY KEY)",
		},
	}
	store, err := NewSQLiteFTSStore(cfg, slog.Default())
	require.NoError(t, err, "NewSQLiteFTSStore")

	ctx := context.Background()

	// Store should fail
	err = store.Store(ctx, "INSERT INTO uninit_test (id) VALUES (?)", "x")
	assert.Error(t, err, "Store should fail before initialization")

	// Delete should fail
	err = store.Delete(ctx, "DELETE FROM uninit_test WHERE id = ?", "x")
	assert.Error(t, err, "Delete should fail before initialization")

	// DeleteByIDs should fail
	_, err = store.DeleteByIDs(ctx, "uninit_test", []string{"x"})
	assert.Error(t, err, "DeleteByIDs should fail before initialization")

	// Count should fail
	_, err = store.Count(ctx, "uninit_test")
	assert.Error(t, err, "Count should fail before initialization")

	// GetOldestTimestamp should fail
	_, err = store.GetOldestTimestamp(ctx, "uninit_test")
	assert.Error(t, err, "GetOldestTimestamp should fail before initialization")

	// GetNewestTimestamp should fail
	_, err = store.GetNewestTimestamp(ctx, "uninit_test")
	assert.Error(t, err, "GetNewestTimestamp should fail before initialization")

	// FindDuplicateGroups should fail
	_, err = store.FindDuplicateGroups(ctx, "uninit_test", 50)
	assert.Error(t, err, "FindDuplicateGroups should fail before initialization")

	// HasFTS5 should return false
	assert.False(t, store.HasFTS5(), "HasFTS5 should be false before initialization")

	// Initialized should return false
	assert.False(t, store.Initialized(), "Initialized should be false before initialization")
}

// ---------------------------------------------------------------------------
// Tests: Close
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_Close(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)

	require.NoError(t, store.Close(), "Close: %v")

	// Close should be idempotent
	require.NoError(t, store.Close(), "second Close: %v")

	// After close, Initialized should be false
	assert.False(t, store.Initialized(), "Initialized should be false after close")
}

// ---------------------------------------------------------------------------
// Tests: io.Closer compliance
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_ImplementsCloser(t *testing.T) {
	// Compile-time check already in ftstore.go via `var _ io.Closer`.
	// This test verifies runtime behavior.
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)

	closer := interface{ Close() error }(store)
	require.NoError(t, closer.Close(), "Close via io.Closer: %v")
}

// ---------------------------------------------------------------------------
// Tests: Utility functions
// ---------------------------------------------------------------------------

func TestJoinStrings(t *testing.T) {
	tests := []struct {
		input []string
		sep   string
		want  string
	}{
		{[]string{}, ",", ""},
		{[]string{"a"}, ",", "a"},
		{[]string{"a", "b"}, ",", "a,b"},
		{[]string{"a", "b", "c"}, "|", "a|b|c"},
	}
	for _, tt := range tests {
		got := strings.Join(tt.input, tt.sep)
		if got != tt.want {
			t.Errorf("strings.Join(%v, %q) = %q, want %q", tt.input, tt.sep, got, tt.want)
		}
	}
}

func TestStringsSplit(t *testing.T) {
	tests := []struct {
		input string
		sep   string
		want  []string
	}{
		{"", ",", []string{""}},
		{"a", ",", []string{"a"}},
		{"a,b,c", ",", []string{"a", "b", "c"}},
		{"a,,b", ",", []string{"a", "", "b"}},
	}
	for _, tt := range tests {
		got := strings.Split(tt.input, tt.sep)
		if len(got) != len(tt.want) {
			t.Errorf("strings.Split(%q, %q) = %v, want %v", tt.input, tt.sep, got, tt.want)
			continue
		}
		for i := range got {
			if got[i] != tt.want[i] {
				t.Errorf("strings.Split(%q, %q)[%d] = %q, want %q", tt.input, tt.sep, i, got[i], tt.want[i])
			}
		}
	}
}

func TestGenerateUUID(t *testing.T) {
	id := generateUUID()
	if id == "" {
		t.Error("generateUUID returned empty string")
	}
	// UUID v4 format: 8-4-4-4-12
	if len(strings.Split(id, "-")) != 5 {
		t.Errorf("UUID should have 5 dash-separated segments, got %q", id)
	}
}

// ---------------------------------------------------------------------------
// Tests: GetDB
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_GetDB(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()

	db := store.GetDB()
	require.NotNil(t, db, "GetDB should return non-nil after initialization")
}

// ---------------------------------------------------------------------------
// Tests: No FTS5 schema fallback
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_NoFTS5Schema(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := FTSConfig{
		TableName:     "nofts_test",
		FTS5Table:     "nofts_fts",
		CategoryField: "category",
		DataDir:       filepath.Join(tmpDir, "nofts"),
		Schema: []string{
			`CREATE TABLE IF NOT EXISTS nofts_test (
				id      TEXT PRIMARY KEY,
				content TEXT NOT NULL
			)`,
		},
	}
	store, err := NewSQLiteFTSStore(cfg, slog.Default())
	require.NoError(t, err, "NewSQLiteFTSStore")

	ctx := context.Background()
	require.NoError(t, store.Initialize(ctx), "Initialize: %v")
	defer store.Close()

	assert.False(t, store.HasFTS5(), "expected HasFTS5 to be false when no FTS5 schema provided")
}

// ---------------------------------------------------------------------------
// Tests: ScanRowConfig with SQL mock-like verification via real DB
// ---------------------------------------------------------------------------

func TestScanRowConfig_SourceFormatting(t *testing.T) {
	// Verify that SourceFmt with %s is formatted with the category value.
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	insertTestItem(t, store, "id-1", "test content", "code")

	db := store.GetDB()

	rows, err := db.QueryContext(ctx,
		"SELECT id, content, category, metadata_json, created_at FROM test_items")
	require.NoError(t, err, "Query")
	defer rows.Close()

	results, err := store.ScanResults(rows, false, ScanRowConfig{
		MemoryType: MemoryTypeTask,
		SourceFmt:  "task:%s",
	})
	require.NoError(t, err, "ScanResults")

	require.Len(t, results, 1)

	if results[0].Source != "task:code" {
		t.Errorf("expected source 'task:code', got %q", results[0].Source)
	}
}

func TestScanRowConfig_StaticSource(t *testing.T) {
	// Verify that SourceFmt without %s is used as-is.
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	insertTestItem(t, store, "id-1", "test content", "code")

	db := store.GetDB()

	rows, err := db.QueryContext(ctx,
		"SELECT id, content, category, metadata_json, created_at FROM test_items")
	require.NoError(t, err, "Query")
	defer rows.Close()

	results, err := store.ScanResults(rows, false, ScanRowConfig{
		MemoryType: MemoryTypeEpisodic,
		SourceFmt:  "episodic",
	})
	require.NoError(t, err, "ScanResults")

	require.Len(t, results, 1)

	if results[0].Source != "episodic" {
		t.Errorf("expected source 'episodic', got %q", results[0].Source)
	}
}

// ---------------------------------------------------------------------------
// Tests: Metadata round-trip through ScanResults
// ---------------------------------------------------------------------------

func TestSQLiteFTSStore_ScanResults_Metadata(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	// Insert with metadata
	metaJSON := `{"user":"test","count":42}`
	db := store.GetDB()

	nowISO := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := db.ExecContext(ctx,
		`INSERT INTO test_items (id, content, category, metadata_json, created_at)
		 VALUES (?, ?, ?, ?, ?)`,
		"id-meta", "metadata test", "general", metaJSON, nowISO)
	require.NoError(t, err, "insert")

	rows, err := db.QueryContext(ctx,
		"SELECT id, content, category, metadata_json, created_at FROM test_items WHERE id = ?", "id-meta")
	require.NoError(t, err, "Query")
	defer rows.Close()

	results, err := store.ScanResults(rows, false, ScanRowConfig{
		MemoryType: MemoryTypeEpisodic,
		SourceFmt:  "episodic",
	})
	require.NoError(t, err, "ScanResults")

	require.Len(t, results, 1)

	meta := results[0].Memory.Metadata
	require.NotNil(t, meta, "expected non-nil metadata")
	if meta["user"] != "test" {
		t.Errorf("expected user 'test', got %v", meta["user"])
	}
}

// Compile-time check that SQLiteFTSStore is used via sql.Rows
var _ = (*sql.Rows)(nil)

// TestScanResults_WithRank_Synthetic tests the rank-scanning code path even
// without FTS5 by providing a synthetic rank column.
func TestScanResults_WithRank_Synthetic(t *testing.T) {
	tmpDir := t.TempDir()
	store := mustNewTestFTSStore(t, tmpDir)
	defer store.Close()
	ctx := context.Background()

	insertTestItem(t, store, "id-1", "alpha bravo charlie", "code")

	db := store.GetDB()

	// Use a subquery to provide a synthetic rank column
	rows, err := db.QueryContext(ctx, `
		SELECT id, content, category, metadata_json, created_at, -1.0 AS rank
		FROM test_items
		WHERE id = ?
	`, "id-1")
	require.NoError(t, err, "Query")
	defer rows.Close()

	results, err := store.ScanResults(rows, true, ScanRowConfig{
		MemoryType: MemoryTypeTask,
		SourceFmt:  "task:%s",
	})
	require.NoError(t, err, "ScanResults")

	require.Len(t, results, 1)

	r := results[0]
	if r.Memory.ID != "id-1" {
		t.Errorf("expected ID 'id-1', got %q", r.Memory.ID)
	}
	if r.Memory.Type != MemoryTypeTask {
		t.Errorf("expected type task, got %q", r.Memory.Type)
	}
	if r.Source != "task:code" {
		t.Errorf("expected source 'task:code', got %q", r.Source)
	}
	if r.Memory.CreatedAt.IsZero() {
		t.Error("expected non-zero CreatedAt")
	}
}
