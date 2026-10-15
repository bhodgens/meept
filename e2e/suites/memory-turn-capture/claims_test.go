//go:build e2e

// Claim-store reader for the memory-turn-capture suite: opens the daemon's
// real episodic sqlite DB (read-only, WAL + busy timeout — same DSN discipline
// as the harness tasksdb reader) and decodes claim rows + status metadata.
package memoryturncapture

import (
	"database/sql"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/memory"

	_ "modernc.org/sqlite" // pure-Go sqlite driver (module dep of the daemon)
)

// claimRow is one claim memory row (decoded metadata included).
type claimRow struct {
	ID      string         `json:"id"`
	Content string         `json:"content"`
	Status  string         // metadata.status
	Meta    map[string]any `json:"-"`
}

// claimRecord is the decoded memory row shape the episodic store persists.
type claimRecord struct {
	ID       string         `json:"id"`
	Content  string         `json:"content"`
	Category string         `json:"category"`
	Metadata map[string]any `json:"metadata"`
}

// queryClaims opens the episodic DB read-only and returns claim-category rows
// whose content contains marker (LIKE-escaped single-word markers only — the
// suite uses slug-style markers with no SQL wildcards).
func queryClaims(t *testing.T, dbPath, marker string) []claimRow {
	t.Helper()
	db, err := sql.Open("sqlite", dbPath+"?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&mode=ro")
	if err != nil {
		t.Fatalf("open episodic db: %v", err)
	}
	db.SetMaxOpenConns(1)
	// The DB may not exist yet (nothing stored): treat as zero rows.
	if err := db.Ping(); err != nil {
		db.Close()
		return nil
	}
	defer db.Close()

	rows, err := db.Query(
		`SELECT id, content, category, metadata_json FROM episodic_memories WHERE category = 'claim' ORDER BY rowid DESC LIMIT 200`)
	if err != nil {
		// Table not migrated yet = nothing stored.
		if strings.Contains(err.Error(), "no such table") {
			return nil
		}
		t.Fatalf("query claims: %v", err)
	}
	defer rows.Close()

	var out []claimRow
	for rows.Next() {
		var rec claimRecord
		var metaJSON sql.NullString
		if err := rows.Scan(&rec.ID, &rec.Content, &rec.Category, &metaJSON); err != nil {
			t.Fatalf("scan claim row: %v", err)
		}
		if metaJSON.Valid && metaJSON.String != "" {
			_ = json.Unmarshal([]byte(metaJSON.String), &rec.Metadata)
		}
		if marker != "" && !strings.Contains(rec.Content, marker) {
			continue
		}
		status, _ := rec.Metadata["status"].(string)
		out = append(out, claimRow{
			ID:      rec.ID,
			Content: rec.Content,
			Status:  status,
			Meta:    rec.Metadata,
		})
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate claims: %v", err)
	}
	return out
}

// waitAutoClaim polls until a claim whose content contains marker lands with
// metadata status=auto (the L1/ambient write contract).
func waitAutoClaim(t *testing.T, dbPath, marker string, timeout time.Duration) claimRow {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last []claimRow
	for time.Now().Before(deadline) {
		last = queryClaims(t, dbPath, marker)
		for _, r := range last {
			if strings.Contains(r.Content, marker) && r.Status == "auto" {
				return r
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("no status=auto claim containing %q landed within %s; claim rows seen: %+v",
		marker, timeout, last)
	return claimRow{}
}

// waitAutoClaimCount polls until at least n status=auto claims whose content
// contains marker have landed (the ambient + L1 double-write contract).
func waitAutoClaimCount(t *testing.T, dbPath, marker string, n int, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last []claimRow
	for time.Now().Before(deadline) {
		last = queryClaims(t, dbPath, marker)
		count := 0
		for _, r := range last {
			if strings.Contains(r.Content, marker) && r.Status == string(memory.ClaimStatusAuto) {
				count++
			}
		}
		if count >= n {
			return
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("only %d status=auto claims containing %q landed within %s, want >= %d; claim rows seen: %+v",
		countAuto(last, marker), marker, timeout, n, last)
}

func countAuto(rows []claimRow, marker string) int {
	count := 0
	for _, r := range rows {
		if strings.Contains(r.Content, marker) && r.Status == string(memory.ClaimStatusAuto) {
			count++
		}
	}
	return count
}
