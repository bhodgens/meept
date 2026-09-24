//go:build e2e

// Suite audit-chain: the hash-chained audit log over a real sandbox
// sqlite file — verifiable chain from real writes, tamper detection to
// the exact seq, canonical-JSON determinism, and the anchor JSONL job.
package auditchain

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auditlog"
	_ "modernc.org/sqlite"
)

// openSandboxAuditStore opens a real audit-log store under a per-test
// sandbox dir (never ~/.meept) and also returns the db file path.
func openSandboxAuditStore(t *testing.T) (*auditlog.Store, string, string) {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "audit.db")
	store, err := auditlog.OpenStore(dbPath, nil)
	if err != nil {
		t.Fatalf("open audit store: %v", err)
	}
	t.Cleanup(func() { _ = store.Close() })
	return store, dbPath, dir
}

// tamperPayloadAtSeq opens a second raw sqlite handle to the audit DB and
// rewrites one record's payload, bypassing the append path.
func tamperPayloadAtSeq(t *testing.T, dsn string, seq int, payload string) {
	t.Helper()
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open tamper handle: %v", err)
	}
	defer db.Close()
	if _, err := db.Exec(`UPDATE audit_log_chain SET payload = ? WHERE seq = ?`, payload, seq); err != nil {
		t.Fatalf("tamper update seq %d: %v", seq, err)
	}
}

// TestAuditChain_RealWritesVerifyAndTamperPointsAtSeq covers
// audit-chain-01: real appends produce a verifiable hash chain; a direct
// DB tamper is reported at the exact seq it touched.
func TestAuditChain_RealWritesVerifyAndTamperPointsAtSeq(t *testing.T) {
	ctx := context.Background()
	store, dbPath, _ := openSandboxAuditStore(t)

	base := time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	var head auditlog.Record
	for i, payload := range []map[string]any{
		{"action": "plan.approve", "plan_id": "plan-1", "approver": "user"},
		{"action": "tool.shell", "cmd": "go test ./...", "exit": float64(0)},
		{"action": "goal.health", "goal_id": "goal-9", "health": "at_risk"},
	} {
		rec, err := store.Append(ctx, auditlog.Record{
			Type:       "e2e_event",
			EmployeeID: "emp-e2e",
			Payload:    payload,
			At:         base.Add(time.Duration(i) * time.Second),
		})
		if err != nil {
			t.Fatalf("append %d: %v", i, err)
		}
		if rec.Seq != uint64(i+1) {
			t.Fatalf("seq = %d, want %d", rec.Seq, i+1)
		}
		head = rec
	}

	// Untampered chain verifies clean.
	res, err := auditlog.VerifyChain(ctx, store.ChainDB())
	if err != nil {
		t.Fatalf("verify: %v", err)
	}
	if !res.OK {
		t.Fatalf("chain should verify: %+v", res)
	}
	if res.Records != 3 {
		t.Fatalf("records = %d, want 3", res.Records)
	}
	if res.Head != head.RecordHash {
		t.Fatalf("head = %q, want %q", res.Head, head.RecordHash)
	}

	// Tamper: rewrite seq 2's payload directly in sqlite. Verification
	// must fail exactly at seq 2.
	tamperPayloadAtSeq(t, dbPath, 2, `{"action":"forged"}`)

	res, err = auditlog.VerifyChain(ctx, store.ChainDB())
	if err != nil {
		t.Fatalf("verify after tamper: %v", err)
	}
	if res.OK {
		t.Fatal("tampered chain must not verify")
	}
	if res.BrokenAt != 2 {
		t.Fatalf("BrokenAt = %d, want 2 (the exact tampered seq); reason: %s", res.BrokenAt, res.Reason)
	}
}

// TestAuditChain_CanonicalJSONDeterministic covers audit-chain-02: the
// canonical encoder is deterministic under key order and iteration-order
// perturbation.
func TestAuditChain_CanonicalJSONDeterministic(t *testing.T) {
	a := map[string]any{
		"seq":     uint64(7),
		"type":    "tool.shell",
		"payload": map[string]any{"z": 1.5, "a": "x", "m": map[string]any{"q": true, "b": 2}},
	}
	b := map[string]any{
		"payload": map[string]any{"m": map[string]any{"b": 2, "q": true}, "a": "x", "z": 1.5},
		"type":    "tool.shell",
		"seq":     uint64(7),
	}

	ra, err := auditlog.CanonicalJSON(a)
	if err != nil {
		t.Fatalf("canonical a: %v", err)
	}
	rb, err := auditlog.CanonicalJSON(b)
	if err != nil {
		t.Fatalf("canonical b: %v", err)
	}
	if string(ra) != string(rb) {
		t.Fatalf("canonical forms differ under key order:\n%s\n%s", ra, rb)
	}

	// Determinism across repeated calls (map-iteration order must not leak).
	for i := 0; i < 20; i++ {
		again, err := auditlog.CanonicalJSON(a)
		if err != nil {
			t.Fatalf("canonical iter %d: %v", i, err)
		}
		if string(again) != string(ra) {
			t.Fatalf("canonical form unstable at iter %d", i)
		}
	}

	// HashRecord is stable and hex-64 for an identical record.
	rec := auditlog.Record{
		Seq: 7, Type: "tool.shell", EmployeeID: "emp",
		Payload: map[string]any{"k": 1.5},
		At:      time.Date(2026, 9, 24, 12, 0, 0, 123456789, time.UTC),
	}
	h1, err := auditlog.HashRecord(rec)
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	h2, err := auditlog.HashRecord(rec)
	if err != nil {
		t.Fatalf("hash 2: %v", err)
	}
	if h1 != h2 || len(h1) != 64 {
		t.Fatalf("hash not stable/hex-64: %q vs %q", h1, h2)
	}

	// A changed payload changes the hash (tamper evidence actually binds).
	rec.Payload = map[string]any{"k": 2.5}
	h3, err := auditlog.HashRecord(rec)
	if err != nil {
		t.Fatalf("hash 3: %v", err)
	}
	if h3 == h1 {
		t.Fatal("hash did not change when the payload changed")
	}
}

// TestAuditChain_AnchorJobAppendsVerifiableDigests covers audit-chain-03:
// the anchor job's ExportOnce appends chain-head digests to the anchors
// JSONL, each matching the live head at export time, and later appends
// grow the file rather than replacing it.
func TestAuditChain_AnchorJobAppendsVerifiableDigests(t *testing.T) {
	ctx := context.Background()
	store, _, dir := openSandboxAuditStore(t)
	anchorDir := filepath.Join(dir, "employee-data")

	// Anchor an EMPTY chain first: file is created but carries no line.
	job := auditlog.NewAnchorJob(store, anchorDir, time.Hour, nil)
	path, err := job.ExportOnce(ctx)
	if err != nil {
		t.Fatalf("export empty: %v", err)
	}
	if filepath.Base(path) != "audit-anchors.jsonl" {
		t.Fatalf("anchor path = %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read anchors: %v", err)
	}
	if len(strings.TrimSpace(string(data))) != 0 {
		t.Fatalf("empty chain must anchor no lines, got: %s", data)
	}

	// Append records, export, append more, export again.
	wantHeads := make([]string, 0, 2)
	for round := 0; round < 2; round++ {
		rec, err := store.Append(ctx, auditlog.Record{
			Type: "e2e_anchor_round", EmployeeID: "emp-e2e",
			Payload: map[string]any{"round": float64(round)},
		})
		if err != nil {
			t.Fatalf("append round %d: %v", round, err)
		}
		wantHeads = append(wantHeads, rec.RecordHash)
		if _, err := job.ExportOnce(ctx); err != nil {
			t.Fatalf("export round %d: %v", round, err)
		}
	}

	data, err = os.ReadFile(path)
	if err != nil {
		t.Fatalf("read anchors final: %v", err)
	}
	lines := nonEmptyLines(string(data))
	if len(lines) != 2 {
		t.Fatalf("want 2 anchor lines, got %d:\n%s", len(lines), data)
	}
	for i, line := range lines {
		var entry struct {
			ExportedAt string `json:"exported_at"`
			Seq        uint64 `json:"seq"`
			ChainHead  string `json:"chain_head"`
		}
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("anchor line %d not canonical JSON: %v\nline: %s", i, err, line)
		}
		if entry.ChainHead != wantHeads[i] {
			t.Fatalf("anchor %d chain_head = %q, want live head %q", i, entry.ChainHead, wantHeads[i])
		}
		wantSeq := uint64(i + 1)
		if entry.Seq != wantSeq {
			t.Fatalf("anchor %d seq = %d, want %d", i, entry.Seq, wantSeq)
		}
		if entry.ExportedAt == "" {
			t.Fatalf("anchor %d missing exported_at", i)
		}
	}
}

// nonEmptyLines returns the whitespace-trimmed non-empty lines of s.
func nonEmptyLines(s string) []string {
	var out []string
	for _, line := range strings.Split(s, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			out = append(out, line)
		}
	}
	return out
}
