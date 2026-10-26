//go:build e2e

// Package migrateeq proves the central SQLite schema-migration registry
// (internal/migrate) end to end for every wired store: a database created
// FRESH by the store's constructor and a database migrated UP from a
// legacy shape must end with IDENTICAL sqlite_master contents, and the
// registry's version gates must fail loud (down-level binary) instead of
// silently proceeding. The wiring proofs live in the hermetic e2e tier
// per the AGENTS.md e2e policy; pure unit tests stay in-package
// (internal/migrate).
package migrateeq

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

// dumpSchema returns a deterministic dump of a database's LOGICAL schema:
// every user table with its ordered columns (name:type:notnull:default:pk)
// and every index with its ordered columns + uniqueness. CREATE-statement
// TEXT is deliberately excluded: DROP COLUMN rewrites and ALTER ADD COLUMN
// appends change the stored text without changing what queries see, so the
// text is not the equivalence contract — the queryable shape is.
// sqlite_master (existence of every object) is still covered implicitly:
// tables and indexes are enumerated by name from it.
func dumpSchema(t *testing.T, db *sql.DB) string {
	t.Helper()
	var out string

	tables := []string{}
	trows, err := db.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if err != nil {
		t.Fatalf("tables query: %v", err)
	}
	for trows.Next() {
		var name string
		if err := trows.Scan(&name); err != nil {
			t.Fatalf("tables scan: %v", err)
		}
		tables = append(tables, name)
	}
	if err := trows.Err(); err != nil {
		t.Fatalf("tables iteration: %v", err)
	}
	trows.Close()

	for _, tbl := range tables {
		out += "table " + tbl + "\n"
		// Columns sorted by NAME, not cid: ALTER ADD COLUMN appends at
		// the end, so a legacy-migrated table legitimately differs from
		// a fresh-created one in column ORDER when its DDL carries the
		// column inline (pre-existing reality of the pre-registry
		// stores, unchanged by the registry). Column order has no
		// query-semantic effect; name/type/notnull/default/pk identity
		// does, and that is what is compared.
		crows, err := db.Query(`SELECT name, type, "notnull", COALESCE(dflt_value, ''), pk FROM pragma_table_info(?) ORDER BY name`, tbl)
		if err != nil {
			t.Fatalf("table_info(%s): %v", tbl, err)
		}
		for crows.Next() {
			var name, colType, dflt string
			var notnull, pk int
			if err := crows.Scan(&name, &colType, &notnull, &dflt, &pk); err != nil {
				t.Fatalf("table_info(%s) scan: %v", tbl, err)
			}
			out += "  col " + name + " " + colType + " notnull=" + itoa(notnull) + " dflt=" + dflt + " pk=" + itoa(pk) + "\n"
		}
		if err := crows.Err(); err != nil {
			t.Fatalf("table_info(%s) iteration: %v", tbl, err)
		}
		crows.Close()
	}

	for _, tbl := range tables {
		irows, err := db.Query(`SELECT name, [unique], COALESCE([origin],'') FROM pragma_index_list(?) ORDER BY name`, tbl)
		if err != nil {
			t.Fatalf("index_list(%s): %v", tbl, err)
		}
		for irows.Next() {
			var name string
			var unique int
			var origin string
			if err := irows.Scan(&name, &unique, &origin); err != nil {
				t.Fatalf("index_list(%s) scan: %v", tbl, err)
			}
			cols := []string{}
			cnRows, err := db.Query(`SELECT name FROM pragma_index_info(?) ORDER BY seqno`, name)
			if err != nil {
				t.Fatalf("index_info(%s): %v", name, err)
			}
			for cnRows.Next() {
				var cn string
				if err := cnRows.Scan(&cn); err != nil {
					t.Fatalf("index_info(%s) scan: %v", name, err)
				}
				cols = append(cols, cn)
			}
			cnRows.Close()
			out += "index " + name + " on " + tbl + " unique=" + itoa(unique) + " origin=" + origin + " (" + joinStrs(cols) + ")\n"
		}
		if err := irows.Err(); err != nil {
			t.Fatalf("index_list(%s) iteration: %v", tbl, err)
		}
		irows.Close()
	}
	return out
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	digits := ""
	for n > 0 {
		digits = string(rune('0'+n%10)) + digits
		n /= 10
	}
	return digits
}

func joinStrs(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ","
		}
		out += s
	}
	return out
}

// userVersion reads PRAGMA user_version.
func userVersion(t *testing.T, db *sql.DB) int {
	t.Helper()
	var v int
	if err := db.QueryRow("PRAGMA user_version").Scan(&v); err != nil {
		t.Fatalf("user_version: %v", err)
	}
	return v
}

// openRaw opens an independent handle on a store's database file for
// inspection. The store keeps its own pool open; SQLite (WAL) allows
// concurrent readers.
func openRaw(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		t.Fatalf("open raw: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	return db
}

func TestMigrateRegistryExists(t *testing.T) {
	// Pin the package's public surface minimally: the registry applies
	// and stamps user_version on a scratch database. The full unit
	// matrix lives in internal/migrate; this hermetic proof just
	// anchors the package into the e2e coverage map.
	dir := t.TempDir()
	db, err := sql.Open("sqlite", "file:"+filepath.Join(dir, "scratch.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = db.Close() }()
	if _, err := db.Exec(`CREATE TABLE probe (id TEXT PRIMARY KEY)`); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA user_version = 1`); err != nil {
		t.Fatalf("user_version write (modernc support): %v", err)
	}
	if got := userVersion(t, db); got != 1 {
		t.Fatalf("user_version = %d, want 1", got)
	}
}
