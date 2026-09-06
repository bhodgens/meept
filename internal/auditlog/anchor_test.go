package auditlog

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestExportDigest_EmptyChainWritesNothing(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_ = Migrate(ctx, db)
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()
	dir := t.TempDir()
	path, err := s.ExportDigest(ctx, dir)
	if err != nil {
		t.Fatalf("ExportDigest: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if len(strings.TrimSpace(string(data))) != 0 {
		t.Fatalf("empty chain must write no line, got %q", data)
	}
}

func TestExportDigest_AppendsVerifiableLines(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_ = Migrate(ctx, db)
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()
	_, err = s.Append(ctx, Record{Type: "audit_finding", EmployeeID: "e",
		Payload: map[string]any{"i": 1}})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	dir := t.TempDir()
	p1, err := s.ExportDigest(ctx, dir)
	if err != nil {
		t.Fatalf("ExportDigest 1: %v", err)
	}
	p2, err := s.ExportDigest(ctx, dir)
	if err != nil {
		t.Fatalf("ExportDigest 2: %v", err)
	}
	if p1 != p2 {
		t.Fatalf("exports must append to the same file: %s vs %s", p1, p2)
	}
	data, err := os.ReadFile(p2)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("want 2 lines, got %d: %q", len(lines), data)
	}
	var line2 struct {
		ExportedAt string `json:"exported_at"`
		Seq        uint64 `json:"seq"`
		ChainHead  string `json:"chain_head"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &line2); err != nil {
		t.Fatalf("line 2 not JSON: %v", err)
	}
	if line2.Seq != 1 {
		t.Fatalf("seq: %+v", line2)
	}
	head, ok, err := s.Head(ctx)
	if err != nil || !ok || line2.ChainHead != head.RecordHash {
		t.Fatalf("chain_head mismatch: %+v err=%v head=%+v", line2, err, head)
	}
	// Line format sanity: no whitespace between tokens (canonical-ish JSON).
	if strings.Contains(lines[1], ": ") || strings.Contains(lines[1], ", \"") {
		t.Fatalf("line has insignificant whitespace: %q", lines[1])
	}
}

func TestAnchorJob_RunExportsAndStopsOnCancel(t *testing.T) {
	db := openTestDB(t)
	ctx := context.Background()
	_ = Migrate(ctx, db)
	s, err := OpenStoreFromDB(db, nil)
	if err != nil {
		t.Fatalf("OpenStoreFromDB: %v", err)
	}
	defer s.Close()
	_, err = s.Append(ctx, Record{Type: "t", Payload: map[string]any{}})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}

	dir := t.TempDir()
	job := NewAnchorJob(s, dir, 25*time.Millisecond, nil)
	runCtx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { job.Run(runCtx); close(done) }()

	// Wait for at least one export.
	deadline := time.Now().Add(2 * time.Second)
	anchorPath := filepath.Join(dir, "anchors", "audit-anchors.jsonl")
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(anchorPath); err == nil && len(data) > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not exit on cancel")
	}
	data, err := os.ReadFile(anchorPath)
	if err != nil || len(data) == 0 {
		t.Fatalf("no anchor written: err=%v data=%q", err, data)
	}
}

func TestAnchorJob_NilIntervalUsesDefault(t *testing.T) {
	// ExportOnce must work regardless of interval; constructor validation:
	job := NewAnchorJob(nil, t.TempDir(), 0, nil)
	if job == nil {
		t.Fatal("constructor must not return nil for zero interval (uses default)")
	}
}
