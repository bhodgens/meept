package memory

import (
	"database/sql"
	"github.com/caimlas/meept/pkg/id"
	"os"
	"path/filepath"
	"time"
)

// ReportArtifact represents a persisted analysis report on disk.
// Modeled after HALO's halo_run_artifacts table (report.ts:8-15).
type ReportArtifact struct {
	ID           string    `json:"id"`
	RunID        string    `json:"run_id"`
	ArtifactType string    `json:"artifact_type"` // always "report_markdown"
	Path         string    `json:"path"`
	SizeBytes    int64     `json:"size_bytes"`
	CreatedAt    time.Time `json:"created_at"`
}

const ReportArtifactType = "report_markdown"

// ReportStore manages on-disk report artifacts with SQLite tracking.
type ReportStore struct {
	dbPath    string
	outputDir string
}

// NewReportStore creates a store for the given database path.
func NewReportStore(dbPath string) *ReportStore {
	dir := filepath.Dir(dbPath)
	return &ReportStore{
		dbPath:    dbPath,
		outputDir: filepath.Join(dir, "halo-runs"),
	}
}

// EnsureReportFile materializes a report as markdown and tracks in SQLite.
// Returns null when report is empty. Rewrites if missing or stale.
// Modeled after HALO's ensureHaloReportFile (report.ts:30-54).
func (rs *ReportStore) EnsureReportFile(tx *sql.Tx, runID string, content string) (*ReportArtifact, error) {
	if content == "" {
		return nil, nil
	}

	outputDir := filepath.Join(rs.outputDir, runID)
	reportPath := filepath.Join(outputDir, "report.md")

	// Check staleness - rewrite if file doesn't exist or is older than now.
	existing, err := os.Stat(reportPath)
	stale := err != nil || existing.ModTime().Before(time.Now())

	if stale {
		// Create directory.
		if err := os.MkdirAll(outputDir, 0o755); err != nil {
			return nil, err
		}

		// Write report atomically via .tmp -> rename.
		tmpPath := reportPath + ".tmp"
		if err := os.WriteFile(tmpPath, []byte(content), 0o644); err != nil {
			return nil, err
		}
		if err := os.Rename(tmpPath, reportPath); err != nil {
			os.Remove(tmpPath)
			return nil, err
		}
	}

	// Get file stats.
	stat, err := os.Stat(reportPath)
	if err != nil {
		return nil, err
	}

	// Upsert artifact in database.
	artifact, err := rs.upsertArtifact(tx, runID, reportPath, stat.Size())
	if err != nil {
		return nil, err
	}

	return artifact, nil
}

// upsertArtifact inserts or updates a report artifact record.
func (rs *ReportStore) upsertArtifact(tx *sql.Tx, runID, path string, sizeBytes int64) (*ReportArtifact, error) {
	// Check for existing.
	var existingID string
	err := tx.QueryRow(
		`SELECT id FROM halo_run_artifacts WHERE run_id = ? AND artifact_type = ? LIMIT 1`,
		runID, ReportArtifactType,
	).Scan(&existingID)

	if err == nil {
		// Update existing.
		_, err = tx.Exec(
			`UPDATE halo_run_artifacts SET path = ?, size_bytes = ? WHERE id = ?`,
			path, sizeBytes, existingID,
		)
		if err != nil {
			return nil, err
		}
		return &ReportArtifact{
			ID:           existingID,
			RunID:        runID,
			ArtifactType: ReportArtifactType,
			Path:         path,
			SizeBytes:    sizeBytes,
		}, nil
	}

	if err != sql.ErrNoRows {
		return nil, err
	}

	// Insert new.
	id := rs.generateID()
	_, err = tx.Exec(
		`INSERT INTO halo_run_artifacts (id, run_id, artifact_type, path, size_bytes, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		id, runID, ReportArtifactType, path, sizeBytes, time.Now().Unix(),
	)
	if err != nil {
		return nil, err
	}

	return &ReportArtifact{
		ID:           id,
		RunID:        runID,
		ArtifactType: ReportArtifactType,
		Path:         path,
		SizeBytes:    sizeBytes,
		CreatedAt:    time.Now(),
	}, nil
}

// generateID creates a cryptographically secure random ID via pkg/id.
func (rs *ReportStore) generateID() string {
	return id.Generate("")
}

// OutputDirForRun returns the output directory for a run ID.
// Exported for use by report renderers.
func (rs *ReportStore) OutputDirForRun(runID string) string {
	return filepath.Join(rs.outputDir, runID)
}
