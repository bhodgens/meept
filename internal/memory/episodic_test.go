package memory

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/migrate"
)

// mustNewEpisodicMemory wraps NewEpisodicMemory with a t.Fatalf on error,
// for terser test setup.
func mustNewEpisodicMemory(t *testing.T, cfg EpisodicConfig) *EpisodicMemory {
	t.Helper()
	mem, err := NewEpisodicMemory(cfg)
	if err != nil {
		t.Fatalf("NewEpisodicMemory: %v", err)
	}
	return mem
}

func TestEpisodicMemoryInitialize(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Second initialization should be idempotent
	err = mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Second initialization failed: %v", err)
	}
}

func TestEpisodicMemoryStore(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store a memory
	id, err := mem.Store(ctx, "Hello, world!", "conversation", map[string]any{
		"user": "test",
	})
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	if id == "" {
		t.Error("Expected non-empty ID")
	}

	// Verify count
	count, err := mem.Count(ctx)
	if err != nil {
		t.Fatalf("Failed to count: %v", err)
	}
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}
}

func TestEpisodicMemorySearch(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store some memories
	_, err = mem.Store(ctx, "The quick brown fox jumps over the lazy dog", "conversation", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	_, err = mem.Store(ctx, "Python is a programming language", "code", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	_, err = mem.Store(ctx, "Go is another programming language", "code", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Search for "programming"
	results, err := mem.Search(ctx, "programming", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results for 'programming', got %d", len(results))
	}

	// Search for "fox"
	results, err = mem.Search(ctx, "fox", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result for 'fox', got %d", len(results))
	}
}

func TestEpisodicMemoryGetRecent(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories (created_at is RFC3339Nano — ns separation, no sleeps)
	for i := range 5 {
		_, err = mem.Store(ctx, "Memory "+string(rune('A'+i)), "conversation", nil)
		if err != nil {
			t.Fatalf("Failed to store: %v", err)
		}
	}

	// Get recent
	results, err := mem.GetRecent(ctx, 3)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(results) != 3 {
		t.Errorf("Expected 3 results, got %d", len(results))
	}

	// Most recent should be first
	if results[0].Memory.Content != "Memory E" {
		t.Errorf("Expected 'Memory E' first, got %q", results[0].Memory.Content)
	}
}

func TestEpisodicMemoryGetByCategory(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories with different categories
	_, _ = mem.Store(ctx, "Conversation 1", "conversation", nil)
	_, _ = mem.Store(ctx, "Code snippet 1", "code", nil)
	_, _ = mem.Store(ctx, "Conversation 2", "conversation", nil)

	// Get by category
	results, err := mem.GetByCategory(ctx, "conversation", 10)
	if err != nil {
		t.Fatalf("GetByCategory failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 conversation results, got %d", len(results))
	}

	for _, r := range results {
		if r.Memory.Category != "conversation" {
			t.Errorf("Expected category 'conversation', got %q", r.Memory.Category)
		}
	}
}

// TestGetNewestByCategoryAndMetadataBeyondWindow pins the keyed marker
// lookup (bughunt 2026-10-14): a row whose metadata matches must be found
// even when hundreds of newer same-category rows push it far past any
// newest-N scan window. The old GetByCategory(200) scan lost the L0
// progress marker for exactly this shape.
func TestGetNewestByCategoryAndMetadataBeyondWindow(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})
	if err := mem.Initialize(ctx); err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	for i := range 250 {
		if _, err := mem.Store(ctx, "filler", "l0_progress", map[string]any{
			"session_id":  "other",
			"l0_progress": "filler",
		}); err != nil {
			t.Fatalf("store filler %d: %v", i, err)
		}
	}
	if _, err := mem.Store(ctx, "target marker", "l0_progress", map[string]any{
		"session_id":  "target",
		"l0_progress": "msg-99",
	}); err != nil {
		t.Fatalf("store target: %v", err)
	}

	res, ok, err := mem.GetNewestByCategoryAndMetadata(ctx, "l0_progress", "session_id", "target")
	if err != nil {
		t.Fatalf("keyed lookup: %v", err)
	}
	if !ok {
		t.Fatal("keyed lookup missed the target row beyond the scan window")
	}
	if got, _ := res.Memory.Metadata["l0_progress"].(string); got != "msg-99" {
		t.Fatalf("l0_progress = %q, want msg-99", got)
	}

	// And a miss is a clean ok=false.
	if _, ok, err := mem.GetNewestByCategoryAndMetadata(ctx, "l0_progress", "session_id", "absent"); err != nil || ok {
		t.Fatalf("absent key: ok=%v err=%v, want ok=false", ok, err)
	}
}

func TestEpisodicMemoryDelete(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store a memory
	id, err := mem.Store(ctx, "To be deleted", "conversation", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Verify it exists
	count, _ := mem.Count(ctx)
	if count != 1 {
		t.Fatalf("Expected count 1, got %d", count)
	}

	// Delete
	err = mem.Delete(ctx, id)
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	// Verify it's gone
	count, _ = mem.Count(ctx)
	if count != 0 {
		t.Errorf("Expected count 0 after delete, got %d", count)
	}
}

func TestEpisodicMemoryDeleteByIDs(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	id1, _ := mem.Store(ctx, "Memory 1", "conversation", nil)
	id2, _ := mem.Store(ctx, "Memory 2", "conversation", nil)
	id3, _ := mem.Store(ctx, "Memory 3", "conversation", nil)

	// Delete multiple
	deleted, err := mem.DeleteByIDs(ctx, []string{id1, id3})
	if err != nil {
		t.Fatalf("Failed to delete: %v", err)
	}

	if deleted != 2 {
		t.Errorf("Expected 2 deleted, got %d", deleted)
	}

	// Verify only one remains
	count, _ := mem.Count(ctx)
	if count != 1 {
		t.Errorf("Expected count 1, got %d", count)
	}

	// Verify the correct one remains
	results, _ := mem.GetRecent(ctx, 10)
	if len(results) != 1 || results[0].Memory.ID != id2 {
		t.Error("Wrong memory remained after deletion")
	}
}

func TestEpisodicMemoryTimestamps(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Empty store should return ErrNotFound
	oldest, err := mem.GetOldestTimestamp(ctx)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetOldestTimestamp expected ErrNotFound, got: %v", err)
	}
	if oldest != nil {
		t.Error("Expected nil oldest timestamp for empty store")
	}

	newest, err := mem.GetNewestTimestamp(ctx)
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("GetNewestTimestamp expected ErrNotFound, got: %v", err)
	}
	if newest != nil {
		t.Error("Expected nil newest timestamp for empty store")
	}

	// Store memories. Two immediate Stores can land on the same coarse wall
	// reading (macOS time.Now can return identical ns readings for
	// back-to-back calls), so oldest==newest is possible by construction;
	// only strictly-descending order is a bug.
	_, _ = mem.Store(ctx, "First", "conversation", nil)
	_, _ = mem.Store(ctx, "Second", "conversation", nil)

	oldest, err = mem.GetOldestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetOldestTimestamp failed: %v", err)
	}
	if oldest == nil {
		t.Fatal("Expected non-nil oldest timestamp")
	}

	newest, err = mem.GetNewestTimestamp(ctx)
	if err != nil {
		t.Fatalf("GetNewestTimestamp failed: %v", err)
	}
	if newest == nil {
		t.Fatal("Expected non-nil newest timestamp")
	}

	if oldest.After(*newest) {
		t.Errorf("oldest %v must not be after newest %v", oldest, newest)
	}
}

func TestEpisodicMemoryNotInitialized(t *testing.T) {
	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: "/tmp/test",
	})

	ctx := context.Background()

	// All operations should fail before initialization
	_, err := mem.Store(ctx, "test", "test", nil)
	if err == nil {
		t.Error("Store should fail before initialization")
	}

	_, err = mem.Search(ctx, "test", 10)
	if err == nil {
		t.Error("Search should fail before initialization")
	}

	_, err = mem.GetRecent(ctx, 10)
	if err == nil {
		t.Error("GetRecent should fail before initialization")
	}
}

func TestEpisodicMemoryEmptySearch(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories
	_, _ = mem.Store(ctx, "Hello world", "conversation", nil)
	_, _ = mem.Store(ctx, "Goodbye world", "conversation", nil)

	// Empty search should return recent
	results, err := mem.Search(ctx, "", 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results for empty search, got %d", len(results))
	}

	// Search with only special characters should also return recent
	results, err = mem.Search(ctx, `"" * :`, 10)
	if err != nil {
		t.Fatalf("Search failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results for special-char-only search, got %d", len(results))
	}
}

func TestEpisodicMemoryGetOldMemories(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store memories (RFC3339Nano created_at: ns separation guaranteed)
	_, _ = mem.Store(ctx, "Memory 1", "conversation", nil)
	_, _ = mem.Store(ctx, "Memory 2", "conversation", nil)

	// Get memories older than now (should get all)
	cutoff := time.Now().Add(time.Hour)
	results, err := mem.GetOldMemories(ctx, cutoff, 10)
	if err != nil {
		t.Fatalf("GetOldMemories failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 old memories, got %d", len(results))
	}

	// Boundary check without timing luck: read Memory 2's actual created_at
	// and cut strictly between the two records. Memory 1 (older created_at)
	// must match; Memory 2 must not. (The previous version slept 50ms, cut
	// at now-20ms, and asserted NOTHING about the result — a tautology.)
	all, err := mem.GetRecent(ctx, 2)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("GetRecent = %d memories, want 2", len(all))
	}
	// Identify the two records BY CONTENT — do not assume GetRecent's
	// ordering direction (it flipped between a macOS dev machine and the
	// CI runner, which silently inverted this boundary).
	var newer, older time.Time
	for _, r := range all {
		switch r.Memory.Content {
		case "Memory 1":
			older = r.Memory.CreatedAt
		case "Memory 2":
			newer = r.Memory.CreatedAt
		}
	}
	if older.Equal(newer) {
		// Timestamps are fixed-width since #196, so two back-to-back
		// Stores collide only when the clock genuinely reads the same
		// nanosecond (coarse-clock VMs), making a strict boundary
		// untestable at that granularity.
		t.Skip("both memories share one timestamp; boundary granularity not testable")
	}
	// Strict < cutoff with cutoff = newer.CreatedAt: Memory 1 (older) must
	// match, Memory 2 (the boundary itself) must not.
	results, err = mem.GetOldMemories(ctx, newer, 10)
	if err != nil {
		t.Fatalf("GetOldMemories (boundary) failed: %v", err)
	}
	if len(results) != 1 || results[0].Memory.Content != "Memory 1" {
		t.Errorf("boundary cutoff %v: got %d memories (%v), want exactly ['Memory 1']",
			newer, len(results), results)
	}
}

func TestEpisodicMemoryMetadata(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store with metadata
	metadata := map[string]any{
		"user":    "testuser",
		"channel": "general",
		"count":   42,
	}
	_, err = mem.Store(ctx, "Test with metadata", "conversation", metadata)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	// Retrieve and verify metadata
	results, err := mem.GetRecent(ctx, 1)
	if err != nil {
		t.Fatalf("GetRecent failed: %v", err)
	}

	if len(results) != 1 {
		t.Fatalf("Expected 1 result, got %d", len(results))
	}

	meta := results[0].Memory.Metadata
	if meta == nil {
		t.Fatal("Expected non-nil metadata")
	}

	if meta["user"] != "testuser" {
		t.Errorf("Expected user 'testuser', got %v", meta["user"])
	}

	if meta["channel"] != "general" {
		t.Errorf("Expected channel 'general', got %v", meta["channel"])
	}
}

func TestEpisodicMemoryStorePopulatesVersioningColumns(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store first memory (root) - should get default version=1, is_current=1
	rootID, err := mem.Store(ctx, "Original content", "conversation", map[string]any{
		"version":    1,
		"is_current": 1,
	})
	if err != nil {
		t.Fatalf("Failed to store root: %v", err)
	}

	// Store second memory (version 2) with parent_id
	v2ID, err := mem.Store(ctx, "Updated content v2", "conversation", map[string]any{
		"parent_id":  rootID,
		"version":    2,
		"is_current": 1,
	})
	if err != nil {
		t.Fatalf("Failed to store v2: %v", err)
	}

	// Verify the SQL columns are populated by querying directly
	db := mem.store.GetDB()

	// Check root memory SQL columns
	var rootVersion int
	var rootParentID sql.NullString
	var rootIsCurrent int
	err = db.QueryRow("SELECT version, parent_id, is_current FROM episodic_memories WHERE id = ?", rootID).
		Scan(&rootVersion, &rootParentID, &rootIsCurrent)
	if err != nil {
		t.Fatalf("Failed to query root: %v", err)
	}
	if rootVersion != 1 {
		t.Errorf("Expected root version=1, got %d", rootVersion)
	}
	if rootParentID.Valid && rootParentID.String != "" {
		t.Errorf("Expected root parent_id to be NULL, got %q", rootParentID.String)
	}
	if rootIsCurrent != 1 {
		t.Errorf("Expected root is_current=1, got %d", rootIsCurrent)
	}

	// Check v2 memory SQL columns
	var v2Version int
	var v2ParentID sql.NullString
	var v2IsCurrent int
	err = db.QueryRow("SELECT version, parent_id, is_current FROM episodic_memories WHERE id = ?", v2ID).
		Scan(&v2Version, &v2ParentID, &v2IsCurrent)
	if err != nil {
		t.Fatalf("Failed to query v2: %v", err)
	}
	if v2Version != 2 {
		t.Errorf("Expected v2 version=2, got %d", v2Version)
	}
	if !v2ParentID.Valid || v2ParentID.String != rootID {
		t.Errorf("Expected v2 parent_id=%q, got valid=%v value=%q", rootID, v2ParentID.Valid, v2ParentID.String)
	}
	if v2IsCurrent != 1 {
		t.Errorf("Expected v2 is_current=1, got %d", v2IsCurrent)
	}
}

func TestEpisodicMemoryStoreDefaultVersioning(t *testing.T) {
	tmpDir := t.TempDir()
	ctx := context.Background()

	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(tmpDir, "episodic"),
	})

	err := mem.Initialize(ctx)
	if err != nil {
		t.Fatalf("Failed to initialize: %v", err)
	}
	defer mem.Close()

	// Store with nil metadata - should get default version=1, is_current=1
	id, err := mem.Store(ctx, "Simple memory", "conversation", nil)
	if err != nil {
		t.Fatalf("Failed to store: %v", err)
	}

	db := mem.store.GetDB()

	var version int
	var isCurrent int
	err = db.QueryRow("SELECT version, is_current FROM episodic_memories WHERE id = ?", id).
		Scan(&version, &isCurrent)
	if err != nil {
		t.Fatalf("Failed to query: %v", err)
	}
	if version != 1 {
		t.Errorf("Expected default version=1, got %d", version)
	}
	if isCurrent != 1 {
		t.Errorf("Expected default is_current=1, got %d", isCurrent)
	}
}

// ---------------------------------------------------------------------------
// Issue #196 pins: fixed-width RFC3339 timestamp writer + legacy-row padding
// migration. See isoTimeLayout / IsoTime in timeformat.go.
// ---------------------------------------------------------------------------

// legacyRFC3339Nano emulates the pre-fix writer: time.Format(time.RFC3339Nano)
// trims trailing fraction zeros, producing variable-width strings.
func legacyRFC3339Nano(t time.Time) string { return t.UTC().Format(time.RFC3339Nano) }

// TestIsoTimeLexicalSort pins the #196 fix contract:
// (a) fixed-width output, lexically ordered at 1ns granularity — including
// the exact trailing-zero collision shapes from the RCA;
// (b) legacy rows written by the OLD variable-width writer are repaired by
// the migration, after which GetOldMemories and ORDER BY agree with time
// semantics on the collision pair;
// (c) IsoTime roundtrips exactly through time.Parse(time.RFC3339Nano);
// (d) the padding migration is idempotent.
func TestIsoTimeLexicalSort(t *testing.T) {
	ctx := context.Background()

	// --- (a) fixed width + lexical order on the RCA collision shapes ---
	// base = ...01:08:12.1833 exactly (trailing zeros .183300): the shape
	// RFC3339Nano trims to '...12.1833Z' (24 chars).
	base := time.Date(2026, 11, 28, 1, 8, 12, 183300000, time.UTC)
	collide := base.Add(88 * time.Microsecond) // .183388 -> '...183388Z' (26 chars)
	oneNs := base.Add(time.Nanosecond)         // .183300001

	if got := len(IsoTime(base)); got != isoTimeLen {
		t.Fatalf("IsoTime width = %d, want fixed %d", got, isoTimeLen)
	}
	for _, pair := range [][2]time.Time{{base, collide}, {base, oneNs}, {oneNs, collide}} {
		lo, hi := IsoTime(pair[0]), IsoTime(pair[1])
		if len(lo) != len(hi) {
			t.Fatalf("IsoTime variable width: %q (%d) vs %q (%d)", lo, len(lo), hi, len(hi))
		}
		if !(lo < hi) {
			t.Errorf("lexical order broken: %q !< %q", lo, hi)
		}
	}
	// Prove the pin exercises the real bug shape: the OLD writer INVERTS —
	// the older instant ('...1833Z') sorts lexically ABOVE the later one
	// because 'Z' (0x5A) > any digit at the first differing character.
	if !(legacyRFC3339Nano(base) > legacyRFC3339Nano(oneNs)) {
		t.Errorf("sanity: legacy RFC3339Nano shapes should invert (older %q should sort ABOVE newer %q), got in order",
			legacyRFC3339Nano(base), legacyRFC3339Nano(oneNs))
	}

	// --- (c) roundtrip through the reader layout (readers stay RFC3339Nano) ---
	for _, x := range []time.Time{base, collide, oneNs, time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)} {
		back, err := time.Parse(time.RFC3339Nano, IsoTime(x))
		if err != nil {
			t.Fatalf("IsoTime(%v) unparseable as RFC3339Nano: %v", x, err)
		}
		if !back.Equal(x) {
			t.Errorf("roundtrip changed value: %v -> %q -> %v", x, IsoTime(x), back)
		}
	}

	// --- (b) legacy rows + migration repair ---
	// Build the store's DB by hand with the OLD writer shapes (the exact
	// collision pair from the RCA), then let Initialize run the #196
	// migration step, then assert query semantics.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "episodic_memories.db")
	seed, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open seed db: %v", err)
	}
	if _, err := seed.Exec(createEpisodicTableSQL); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	olderRaw := legacyRFC3339Nano(base)    // '...12.1833Z'   (24 chars, TRIMMED)
	newerRaw := legacyRFC3339Nano(collide) // '...12.183388Z' (26 chars)
	if olderRaw == IsoTime(base) {
		t.Fatal("pin precondition: legacy shape must differ from padded shape")
	}
	for _, row := range []struct{ id, content, ts string }{
		{"legacy-older", "Memory 1", olderRaw},
		{"legacy-newer", "Memory 2", newerRaw},
	} {
		if _, err := seed.Exec(
			`INSERT INTO episodic_memories (id, content, category, metadata_json, created_at, last_accessed_at)
			 VALUES (?, ?, 'conversation', '{}', ?, '')`,
			row.id, row.content, row.ts); err != nil {
			t.Fatalf("insert legacy row %s: %v", row.id, err)
		}
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed db: %v", err)
	}

	// Pre-migration: the RCA inversion is live in SQL. 'legacy-older' has
	// the SHORTER string that sorts ABOVE the longer one.
	{
		check, err := sql.Open("sqlite", dbPath)
		if err != nil {
			t.Fatalf("reopen pre-migration: %v", err)
		}
		var inverted int
		if err := check.QueryRowContext(ctx,
			`SELECT COUNT(*) FROM episodic_memories
			 WHERE id = 'legacy-older' AND created_at >= ?`, IsoTime(collide)).Scan(&inverted); err != nil {
			t.Fatalf("pre-migration probe: %v", err)
		}
		if inverted != 1 {
			t.Errorf("pre-migration sanity: legacy trimmed row should lexically sort ABOVE the padded cutoff (the #196 bug), got %d", inverted)
		}
		check.Close()
	}

	mem := mustNewEpisodicMemory(t, EpisodicConfig{DataDir: dir})
	if err := mem.Initialize(ctx); err != nil {
		t.Fatalf("Initialize on legacy db: %v", err)
	}
	defer mem.Close()

	db := mem.store.GetDB()

	// Migration rewrote both representations, and rewrote VALUES not
	// instants: exactly IsoTime(original instant) stored now.
	for id, want := range map[string]string{
		"legacy-older": IsoTime(base),
		"legacy-newer": IsoTime(collide),
	} {
		var got string
		if err := db.QueryRowContext(ctx,
			`SELECT created_at FROM episodic_memories WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("read %s: %v", id, err)
		}
		if got != want {
			t.Errorf("migrated %s created_at = %q, want %q", id, got, want)
		}
	}

	// The RCA failure mode, now fixed: strict cutoff at the newer instant
	// must return the OLDER row.
	results, err := mem.GetOldMemories(ctx, collide, 10)
	if err != nil {
		t.Fatalf("GetOldMemories: %v", err)
	}
	if len(results) != 1 || results[0].Memory.Content != "Memory 1" {
		t.Fatalf("boundary cutoff after migration: got %d results %v, want exactly [Memory 1]",
			len(results), results)
	}
	if !results[0].Memory.CreatedAt.Equal(base) {
		t.Errorf("migrated row parsed back as %v, want %v", results[0].Memory.CreatedAt, base)
	}

	// ORDER BY created_at ASC now agrees with time semantics.
	ordRows, err := db.QueryContext(ctx,
		`SELECT id FROM episodic_memories ORDER BY created_at ASC`)
	if err != nil {
		t.Fatalf("order by query: %v", err)
	}
	defer ordRows.Close()
	var order []string
	for ordRows.Next() {
		var id string
		if err := ordRows.Scan(&id); err != nil {
			t.Fatalf("scan order row: %v", err)
		}
		order = append(order, id)
	}
	if len(order) != 2 || order[0] != "legacy-older" || order[1] != "legacy-newer" {
		t.Errorf("ORDER BY created_at ASC = %v, want [legacy-older legacy-newer]", order)
	}

	// GetByTimeRange inclusive bounds agree too.
	rng, err := mem.GetByTimeRange(ctx, base, collide, 10)
	if err != nil {
		t.Fatalf("GetByTimeRange: %v", err)
	}
	if len(rng) != 2 {
		t.Errorf("GetByTimeRange inclusive bounds returned %d rows, want 2", len(rng))
	}

	// --- (d) migration idempotent: rerun the SQL and the whole open path ---
	var pendingBefore int
	if err := db.QueryRowContext(ctx, fmt.Sprintf(
		`SELECT COUNT(*) FROM episodic_memories WHERE %s`,
		migrate.LegacyTimestampWhere("created_at"))).Scan(&pendingBefore); err != nil {
		t.Fatalf("pending probe: %v", err)
	}
	if pendingBefore != 0 {
		t.Errorf("%d rows still match the legacy-shape predicate after migration", pendingBefore)
	}
	changed, err := migrate.BackfillFixedWidthTimestamps(ctx, db.DB, "episodic_memories", "created_at", "last_accessed_at")
	if err != nil {
		t.Fatalf("second padding pass: %v", err)
	}
	if changed != 0 {
		t.Errorf("second padding pass rewrote %d rows, want 0 (idempotence)", changed)
	}
	// Full store lifecycle re-open (fresh Initialize against the migrated
	// db) must also be a no-op on representations.
	if err := mem.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	mem2 := mustNewEpisodicMemory(t, EpisodicConfig{DataDir: dir})
	if err := mem2.Initialize(ctx); err != nil {
		t.Fatalf("re-Initialize: %v", err)
	}
	defer mem2.Close()
	for id, want := range map[string]string{
		"legacy-older": IsoTime(base),
		"legacy-newer": IsoTime(collide),
	} {
		var got string
		if err := mem2.store.GetDB().QueryRowContext(ctx,
			`SELECT created_at FROM episodic_memories WHERE id = ?`, id).Scan(&got); err != nil {
			t.Fatalf("re-read %s: %v", id, err)
		}
		if got != want {
			t.Errorf("after re-open %s created_at = %q, want unchanged %q", id, got, want)
		}
	}
}

// TestFreshStoreWritesFixedWidthTimestamps pins that the WRITER itself
// (not just the migration) emits fixed width for fresh rows, so the
// migration never has to run against same-binary data.
func TestFreshStoreWritesFixedWidthTimestamps(t *testing.T) {
	ctx := context.Background()
	mem := mustNewEpisodicMemory(t, EpisodicConfig{
		DataDir: filepath.Join(t.TempDir(), "episodic"),
	})
	if err := mem.Initialize(ctx); err != nil {
		t.Fatalf("Initialize: %v", err)
	}
	defer mem.Close()

	id, err := mem.Store(ctx, "fresh row", "conversation", nil)
	if err != nil {
		t.Fatalf("Store: %v", err)
	}
	var created, accessed string
	if err := mem.store.GetDB().QueryRowContext(ctx,
		`SELECT created_at, last_accessed_at FROM episodic_memories WHERE id = ?`, id).
		Scan(&created, &accessed); err != nil {
		t.Fatalf("read row: %v", err)
	}
	for label, v := range map[string]string{"created_at": created, "last_accessed_at": accessed} {
		if len(v) != isoTimeLen {
			t.Errorf("fresh %s = %q (len %d), want fixed-width %d", label, v, len(v), isoTimeLen)
		}
	}

	// Registry target: episodic carries the padding step (version 7).
	reg, rerr := mem.store.migrationRegistry()
	if rerr != nil {
		t.Fatalf("migrationRegistry: %v", rerr)
	}
	if reg.Target() < 7 {
		t.Errorf("episodic registry target = %d, want >= 7 (padding step present)", reg.Target())
	}
}

// TestIsoTimeWidthMatchesMigrate pins the writer width against the shared
// SQL-side constant the padding migration uses: the Go writer and the SQL
// shape predicate can never silently diverge (a layout edit that changes
// the width would otherwise disable padding for new legacy rows).
func TestIsoTimeWidthMatchesMigrate(t *testing.T) {
	if got := isoTimeLen; got != migrate.FixedWidthTimeLen {
		t.Errorf("isoTimeLen = %d, migrate.FixedWidthTimeLen = %d — writer and "+
			"padding predicate layouts diverged (#196)", got, migrate.FixedWidthTimeLen)
	}
	if isoTimeLayout != "2006-01-02T15:04:05.000000000Z07:00" {
		t.Errorf("isoTimeLayout changed: %q — update migrate/timepad.go shape offsets in lockstep", isoTimeLayout)
	}
}
