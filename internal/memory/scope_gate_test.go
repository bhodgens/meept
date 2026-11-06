package memory

// Gate tests for the 01-scoping branch (docs/plans/20261030-memory-second-brain/01-scoping/00T-tests.md).
//
// These pins are RED by design: Scope, WithScopeFilter, WithMultiUserEnabled
// and the Memory.Scope field DO NOT EXIST yet. They must fail ONLY for
// missing-symbol reasons; once leaf 01-scope-schema lands they assert exact
// contract behavior (C1, C2 from ../master.md, verbatim shapes).
//
// Pin deviation (gate-author decision, reported to orchestrator): Flag 3
// requires the predicate to distinguish multiuser-disabled from
// multiuser-enabled-empty-userID, but C2's WithScopeFilter(maxScope, projectID,
// userID) has no channel for the multiuser-enabled bit. The bit is pinned
// ADDITIVELY as a separate SearchOption, WithMultiUserEnabled(bool), so C2
// callers compile unchanged; leaf 03's resolveScopeInputs returns the bit as a
// 4th value and the loop passes both options. If the orchestrator prefers a
// 4th WithScopeFilter parameter instead, THIS FILE is the only pin to update.
//
// Flag 3 (userID empty-match fails open), from master.md dated addenda
// 2026-11-03, VERBATIM:
//
//	C2's predicate "scope==user AND userID matches (empty userID matches in
//	single-user)" fails OPEN if multiuser is enabled but identity resolution
//	returns "" (a resolution bug, not a config choice): every user-scope row
//	becomes recallable by every agent. Pin the INVERSE: when multiuser is
//	ENABLED (cfg multiuser.enabled=true) and the resolved userID is EMPTY,
//	user-scope rows are NOT recallable (fail closed). Empty-userID-matches
//	applies ONLY when multiuser is disabled. Both leaves touch this predicate:
//	01 implements it, 03's resolveScopeInputs must carry the multiuser-enabled
//	bit alongside userID so the predicate can distinguish the two cases. Add a
//	table-test cell for (multiuser=true, userID="") => user rows hidden.

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"

	"github.com/jmoiron/sqlx"
)

const (
	scopeGateProjectID = "proj-alpha"
	scopeGateUserID    = "user-u1"
)

// ---------------------------------------------------------------------------
// S1 / C1: Scope enum, Memory.Scope field, metadata stamping, backfill
// ---------------------------------------------------------------------------

// TestScopeEnumValues pins the C1 enum vocabulary exactly: the storage and
// metadata values are the string literals below, and the legacy backfill value
// is "global".
func TestScopeEnumValues(t *testing.T) {
	if string(ScopeProject) != "project" {
		t.Errorf("ScopeProject = %q, want %q", string(ScopeProject), "project")
	}
	if string(ScopeUser) != "user" {
		t.Errorf("ScopeUser = %q, want %q", string(ScopeUser), "user")
	}
	if string(ScopeGlobal) != "global" {
		t.Errorf("ScopeGlobal = %q, want %q", string(ScopeGlobal), "global")
	}
}

// TestScopeMetadataStamping pins the write-side C1 contract: Manager.Store
// stamps metadata["scope"] from mem.Scope (portable truth), the Memory.Scope
// field roundtrips on read, and an EMPTY Scope stores as "global" so unscoped
// writers keep legacy (everywhere-visible) behavior.
func TestScopeMetadataStamping(t *testing.T) {
	m := mustNewManager(t)
	ctx := context.Background()

	pid, err := m.Store(ctx, Memory{
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
		Content:  "scoped write pin: qubit gate calibration",
		Scope:    ScopeProject,
		Metadata: map[string]any{"project_id": scopeGateProjectID},
	})
	if err != nil {
		t.Fatalf("Store scoped: %v", err)
	}

	gid, err := m.Store(ctx, Memory{
		Type:     MemoryTypeEpisodic,
		Category: "conversation",
		Content:  "unscoped write pin: default backfills to global",
	})
	if err != nil {
		t.Fatalf("Store unscoped: %v", err)
	}

	res, err := m.Episodic().GetByID(ctx, pid)
	if err != nil || res == nil {
		t.Fatalf("GetByID scoped row: res=%v err=%v", res, err)
	}
	if res.Memory.Scope != ScopeProject {
		t.Errorf("scoped row Memory.Scope = %q, want %q", string(res.Memory.Scope), string(ScopeProject))
	}
	if res.Memory.Metadata["scope"] != "project" {
		t.Errorf("scoped row metadata[scope] = %v, want %q", res.Memory.Metadata["scope"], "project")
	}

	res, err = m.Episodic().GetByID(ctx, gid)
	if err != nil || res == nil {
		t.Fatalf("GetByID unscoped row: res=%v err=%v", res, err)
	}
	if res.Memory.Scope != ScopeGlobal {
		t.Errorf("unscoped row Memory.Scope = %q, want %q (legacy backfill)", string(res.Memory.Scope), string(ScopeGlobal))
	}
	if res.Memory.Metadata["scope"] != "global" {
		t.Errorf("unscoped row metadata[scope] = %v, want %q", res.Memory.Metadata["scope"], "global")
	}
}

// TestScopeColumnLegacyBackfill pins the S1/T2 migration contract: a LEGACY
// episodic db (no scope column, pre-scoping rows) gains the
// `scope TEXT NOT NULL DEFAULT 'global'` column on open, every existing row
// backfills to 'global' (no memory disappears from recall), the migration is
// idempotent across open-close-open, and PRAGMA integrity_check passes.
func TestScopeColumnLegacyBackfill(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "episodic_memories.db")

	// Build the legacy fixture: the pre-scoping episodic schema (no scope
	// column) with two rows written before the branch existed.
	legacy, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("open legacy db: %v", err)
	}
	legacySchema := `
CREATE TABLE IF NOT EXISTS episodic_memories (
    id               TEXT PRIMARY KEY,
    content          TEXT NOT NULL,
    category         TEXT NOT NULL DEFAULT 'conversation',
    metadata_json    TEXT NOT NULL DEFAULT '{}',
    created_at       TEXT NOT NULL,
    last_accessed_at TEXT NOT NULL DEFAULT '',
    version          INTEGER DEFAULT 1,
    parent_id        TEXT REFERENCES episodic_memories(id),
    is_current       INTEGER DEFAULT 1
)`
	if _, err := legacy.Exec(legacySchema); err != nil {
		t.Fatalf("create legacy table: %v", err)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, row := range []struct{ id, content string }{
		{"legacy-row-1", "pre-scoping fact one"},
		{"legacy-row-2", "pre-scoping fact two"},
	} {
		if _, err := legacy.Exec(
			`INSERT INTO episodic_memories (id, content, category, metadata_json, created_at)
			 VALUES (?, ?, 'conversation', '{}', ?)`, row.id, row.content, now); err != nil {
			t.Fatalf("insert legacy row %s: %v", row.id, err)
		}
	}
	if err := legacy.Close(); err != nil {
		t.Fatalf("close legacy db: %v", err)
	}

	// Open through the real store: Initialize must add the column + backfill.
	em, err := NewEpisodicMemory(EpisodicConfig{DataDir: dir, Logger: testLogger()})
	if err != nil {
		t.Fatalf("NewEpisodicMemory: %v", err)
	}
	if err := em.Initialize(ctx); err != nil {
		t.Fatalf("Initialize on legacy db: %v", err)
	}

	db := em.sharedStore().GetDB()

	// Column exists.
	if !scopeColumnExists(t, db) {
		t.Fatal("episodic_memories.scope column missing after migration")
	}

	// Every legacy row backfilled to 'global'.
	var total, nonGlobal int
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memories`).Scan(&total); err != nil {
		t.Fatalf("count rows: %v", err)
	}
	if err := db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM episodic_memories WHERE scope IS NULL OR scope != 'global'`).Scan(&nonGlobal); err != nil {
		t.Fatalf("count non-global rows: %v", err)
	}
	if total != 2 {
		t.Errorf("legacy row count = %d, want 2", total)
	}
	if nonGlobal != 0 {
		t.Errorf("%d legacy rows did not backfill to scope='global', want 0", nonGlobal)
	}

	// FTS index intact after migration (content table column only; the FTS
	// index table is NEVER altered).
	var ftsCount int
	if err := db.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_fts`).Scan(&ftsCount); err != nil {
		t.Fatalf("count fts rows: %v", err)
	}
	if ftsCount != 2 {
		t.Errorf("episodic_fts row count = %d, want 2 (index must survive the migration)", ftsCount)
	}

	if err := em.Close(); err != nil {
		t.Fatalf("close after migration: %v", err)
	}

	// Open-close-open idempotence: second open adds nothing, errors nothing.
	em2, err := NewEpisodicMemory(EpisodicConfig{DataDir: dir, Logger: testLogger()})
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if err := em2.Initialize(ctx); err != nil {
		t.Fatalf("re-Initialize (migration must be idempotent): %v", err)
	}
	defer func() { _ = em2.Close() }()

	db2 := em2.sharedStore().GetDB()
	if !scopeColumnExists(t, db2) {
		t.Fatal("scope column missing after reopen")
	}
	var total2 int
	if err := db2.QueryRowContext(ctx, `SELECT COUNT(*) FROM episodic_memories`).Scan(&total2); err != nil {
		t.Fatalf("count rows after reopen: %v", err)
	}
	if total2 != 2 {
		t.Errorf("row count after reopen = %d, want 2 (no duplication)", total2)
	}
	var integrity string
	if err := db2.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil {
		t.Fatalf("integrity_check: %v", err)
	}
	if integrity != "ok" {
		t.Errorf("PRAGMA integrity_check = %q, want %q", integrity, "ok")
	}
}

func scopeColumnExists(t *testing.T, db *sqlx.DB) bool {
	t.Helper()
	rows, err := db.Query(`PRAGMA table_info(episodic_memories)`)
	if err != nil {
		t.Fatalf("PRAGMA table_info: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, ctype string
		var notnull int
		var dflt sql.NullString
		var pk int
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dflt, &pk); err != nil {
			t.Fatalf("scan table_info: %v", err)
		}
		if name == "scope" {
			return true
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate table_info: %v", err)
	}
	return false
}

// ---------------------------------------------------------------------------
// S2 / C2: WithScopeFilter predicate matrix (+ Flag 3 fail-closed cell)
// ---------------------------------------------------------------------------

// scopeMatrixRow is one seeded memory row: its unique content marker, its
// scope, and its owning identity metadata (the keys the predicate matches
// against: project_id / user_id).
type scopeMatrixRow struct {
	marker   string
	scope    Scope
	metadata map[string]any
}

func seedScopeMatrixRows(t *testing.T, m *Manager) {
	t.Helper()
	ctx := context.Background()
	rows := []scopeMatrixRow{
		{marker: "zonkey", scope: ScopeGlobal, metadata: nil},
		{marker: "yelbok", scope: ScopeProject, metadata: map[string]any{"project_id": scopeGateProjectID}},
		{marker: "xenial", scope: ScopeUser, metadata: map[string]any{"user_id": scopeGateUserID}},
	}
	for _, r := range rows {
		if _, err := m.Store(ctx, Memory{
			Type:     MemoryTypeEpisodic,
			Category: "conversation",
			Content:  r.marker + " scope matrix fixture",
			Scope:    r.scope,
			Metadata: r.metadata,
		}); err != nil {
			t.Fatalf("store %s row: %v", r.marker, err)
		}
	}
}

func searchScopeMarkers(t *testing.T, m *Manager, opts ...SearchOption) map[string]bool {
	t.Helper()
	ctx := context.Background()
	results, err := m.Search(ctx, MemoryQuery{
		Query:    "zonkey yelbok xenial scope matrix fixture",
		Limit:    10,
		MatchAny: true,
	}, opts...)
	if err != nil {
		t.Fatalf("Search: %v", err)
	}
	got := make(map[string]bool)
	for _, r := range results {
		for _, marker := range []string{"zonkey", "yelbok", "xenial"} {
			if containsToken(r.Memory.Content, marker) {
				got[marker] = true
			}
		}
	}
	return got
}

func containsToken(content, token string) bool {
	return len(content) >= len(token) && (func() bool {
		for i := 0; i+len(token) <= len(content); i++ {
			if content[i:i+len(token)] == token {
				return true
			}
		}
		return false
	})()
}

func markerSet(markers ...string) map[string]bool {
	out := make(map[string]bool, len(markers))
	for _, mk := range markers {
		out[mk] = true
	}
	return out
}

func sameMarkerSet(got, want map[string]bool) bool {
	if len(got) != len(want) {
		return false
	}
	for mk := range want {
		if !got[mk] {
			return false
		}
	}
	return true
}

// TestScopeFilterMatrix pins the C2 recall predicate exactly. Rows: G=global,
// P=project (bound to scopeGateProjectID), U=user (bound to scopeGateUserID).
// Recallable iff: scope==global; OR scope==project AND projectID matches;
// OR scope==user AND userID matches (empty filter userID matches ONLY when
// multiuser is disabled). No option = today's unfiltered behavior.
//
// Flag 3 cell (multiuser=true, userID="") => user rows HIDDEN, fail closed.
func TestScopeFilterMatrix(t *testing.T) {
	m := mustNewManager(t)
	seedScopeMatrixRows(t, m)

	all := markerSet("zonkey", "yelbok", "xenial")
	noUser := markerSet("zonkey", "yelbok")
	globalOnly := markerSet("zonkey")

	cells := []struct {
		name      string
		useFilter bool
		max       Scope
		projectID string
		userID    string
		multiUser bool
		want      map[string]bool
	}{
		{
			name:      "no option is unfiltered (legacy behavior)",
			useFilter: false,
			want:      all,
		},
		{
			name:      "max global sees only global rows",
			useFilter: true,
			max:       ScopeGlobal,
			want:      globalOnly,
		},
		{
			name:      "project match: global + matching project",
			useFilter: true,
			max:       ScopeProject,
			projectID: scopeGateProjectID,
			want:      noUser,
		},
		{
			name:      "project miss: global only (other project isolated)",
			useFilter: true,
			max:       ScopeProject,
			projectID: "proj-other",
			want:      globalOnly,
		},
		{
			name:      "unbound session (empty projectID): nothing project-scoped",
			useFilter: true,
			max:       ScopeProject,
			projectID: "",
			want:      globalOnly,
		},
		{
			name:      "single-user empty userID matches user rows (C2 empty-match)",
			useFilter: true,
			max:       ScopeUser,
			userID:    "",
			multiUser: false,
			want:      all,
		},
		{
			name:      "single-user matching userID sees user rows",
			useFilter: true,
			max:       ScopeUser,
			userID:    scopeGateUserID,
			multiUser: false,
			want:      all,
		},
		{
			name:      "single-user mismatched userID hides user rows",
			useFilter: true,
			max:       ScopeUser,
			userID:    "user-someone-else",
			multiUser: false,
			want:      noUser,
		},
		{
			// FLAG 3 fail-closed cell: multiuser ENABLED + resolved userID
			// EMPTY => user-scope rows NOT recallable. This must be
			// distinguishable from the single-user empty-match cell above.
			name:      "multiuser enabled empty userID fails CLOSED (Flag 3)",
			useFilter: true,
			max:       ScopeUser,
			userID:    "",
			multiUser: true,
			want:      noUser,
		},
		{
			name:      "multiuser matching userID sees user rows",
			useFilter: true,
			max:       ScopeUser,
			userID:    scopeGateUserID,
			multiUser: true,
			want:      all,
		},
		{
			name:      "multiuser mismatched userID hides user rows",
			useFilter: true,
			max:       ScopeUser,
			userID:    "user-someone-else",
			multiUser: true,
			want:      noUser,
		},
	}

	for _, tc := range cells {
		t.Run(tc.name, func(t *testing.T) {
			var got map[string]bool
			if !tc.useFilter {
				got = searchScopeMarkers(t, m)
			} else {
				got = searchScopeMarkers(t, m,
					WithScopeFilter(tc.max, tc.projectID, tc.userID),
					WithMultiUserEnabled(tc.multiUser),
				)
			}
			if !sameMarkerSet(got, tc.want) {
				t.Errorf("filter(max=%q, project=%q, user=%q, multiuser=%v) recalled %v, want %v",
					string(tc.max), tc.projectID, tc.userID, tc.multiUser, got, tc.want)
			}
		})
	}
}

// TestGetRelevantContextAcceptsScopeFilter pins that the recall entry point
// used by the agent loop (GetRelevantContext) accepts the additive variadic
// option and applies the same predicate: a global-max filter must never
// return project- or user-scoped rows.
func TestGetRelevantContextAcceptsScopeFilter(t *testing.T) {
	m := mustNewManager(t)
	seedScopeMatrixRows(t, m)

	results, err := m.GetRelevantContext(context.Background(),
		"zonkey yelbok xenial scope matrix fixture", 10,
		WithScopeFilter(ScopeGlobal, "", ""))
	if err != nil {
		t.Fatalf("GetRelevantContext with scope filter: %v", err)
	}
	for _, r := range results {
		if containsToken(r.Memory.Content, "yelbok") || containsToken(r.Memory.Content, "xenial") {
			t.Errorf("GetRelevantContext leaked non-global row under ScopeGlobal filter: %q", r.Memory.Content)
		}
	}
}
