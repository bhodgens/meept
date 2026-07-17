package memory

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func TestReportStore_EnsureReportFile(t *testing.T) {
	// Create temp database.
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	// Create table.
	_, err = db.Exec(`
		CREATE TABLE halo_run_artifacts (
			id TEXT PRIMARY KEY,
			run_id TEXT,
			artifact_type TEXT,
			path TEXT,
			size_bytes INTEGER,
			created_at INTEGER
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	rs := NewReportStore(dbPath)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	reportContent := "# HALO Report\n\nAnalysis findings here."
	artifact, err := rs.EnsureReportFile(tx, "test-run-123", reportContent)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if artifact == nil {
		t.Fatal("expected artifact, got nil")
	}
	if artifact.ArtifactType != ReportArtifactType {
		t.Errorf("expected %q, got %q", ReportArtifactType, artifact.ArtifactType)
	}
	if artifact.SizeBytes == 0 {
		t.Error("expected non-zero size")
	}
	if artifact.RunID != "test-run-123" {
		t.Errorf("expected run_id 'test-run-123', got %q", artifact.RunID)
	}

	// Verify file exists.
	if _, err := os.Stat(artifact.Path); err != nil {
		t.Error("report file not created")
	}

	// Verify file content.
	data, err := os.ReadFile(artifact.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != reportContent {
		t.Errorf("content mismatch: got %q, want %q", string(data), reportContent)
	}
}

func TestReportStore_EnsureReportFile_EmptyContent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE halo_run_artifacts (
			id TEXT PRIMARY KEY,
			run_id TEXT,
			artifact_type TEXT,
			path TEXT,
			size_bytes INTEGER,
			created_at INTEGER
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	rs := NewReportStore(dbPath)
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}

	artifact, err := rs.EnsureReportFile(tx, "test-run-456", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}

	if artifact != nil {
		t.Errorf("expected nil for empty content, got %#v", artifact)
	}
}

func TestReportStore_EnsureReportFile_Idempotent(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE halo_run_artifacts (
			id TEXT PRIMARY KEY,
			run_id TEXT,
			artifact_type TEXT,
			path TEXT,
			size_bytes INTEGER,
			created_at INTEGER
		)
	`)
	if err != nil {
		t.Fatal(err)
	}

	rs := NewReportStore(dbPath)

	// First call.
	tx1, _ := db.Begin()
	artifact1, err := rs.EnsureReportFile(tx1, "test-run-789", "# Report v1")
	if err != nil {
		t.Fatal(err)
	}
	tx1.Commit()

	// Second call (should update, not insert new).
	tx2, _ := db.Begin()
	artifact2, err := rs.EnsureReportFile(tx2, "test-run-789", "# Report v2")
	if err != nil {
		t.Fatal(err)
	}
	tx2.Commit()

	// Should have same ID (update, not insert).
	if artifact1.ID != artifact2.ID {
		t.Errorf("expected same ID on update, got %s vs %s", artifact1.ID, artifact2.ID)
	}

	// Verify file content is updated.
	data, err := os.ReadFile(artifact2.Path)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "# Report v2" {
		t.Errorf("expected updated content, got %q", string(data))
	}
}

func TestReportStore_OutputDirForRun(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "test.db")
	rs := NewReportStore(dbPath)

	outputDir := rs.OutputDirForRun("test-run-abc")
	expected := filepath.Join(dir, "halo-runs", "test-run-abc")
	if outputDir != expected {
		t.Errorf("expected %q, got %q", expected, outputDir)
	}
}

func TestReportStore_GenerateID(t *testing.T) {
	rs := &ReportStore{}
	id1 := rs.generateID()
	id2 := rs.generateID()

	if id1 == id2 {
		t.Error("expected unique IDs")
	}
	if len(id1) != 16 {
		t.Errorf("expected 16-char ID, got %d", len(id1))
	}
}
