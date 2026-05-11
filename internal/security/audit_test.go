package security

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

func TestAuditLogQueryHistory(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Generate some decisions to log
	_ = engine.Check("file_read", "file_read", nil, "conv1")
	_ = engine.Check("file_write", "file_write", map[string]string{"path": "/tmp/test.txt"}, "conv1")
	_ = engine.Check("file_delete", "file_delete", map[string]string{"path": "/tmp/test.txt"}, "conv2")

	// Create audit log accessor
	audit := NewAuditLog(engine.db)

	// Query all entries
	entries, err := audit.QueryHistory(QueryFilters{Limit: 10})
	if err != nil {
		t.Fatalf("QueryHistory failed: %v", err)
	}
	if len(entries) < 3 {
		t.Errorf("Expected at least 3 entries, got %d", len(entries))
	}

	// Query by action
	entries, err = audit.QueryHistory(QueryFilters{Action: "file_read", Limit: 10})
	if err != nil {
		t.Fatalf("QueryHistory by action failed: %v", err)
	}
	if len(entries) < 1 {
		t.Errorf("Expected at least 1 file_read entry, got %d", len(entries))
	}
	for _, e := range entries {
		if e.Action != "file_read" {
			t.Errorf("Expected action file_read, got %s", e.Action)
		}
	}
}

func TestAuditLogGetStats(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Generate some decisions
	_ = engine.Check("file_read", "file_read", nil, "")
	_ = engine.Check("file_write", "file_write", nil, "")
	_ = engine.Check("file_delete", "file_delete", nil, "")

	audit := NewAuditLog(engine.db)
	stats, err := audit.GetStats()
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}

	if stats.TotalDecisions < 3 {
		t.Errorf("Expected at least 3 total decisions, got %d", stats.TotalDecisions)
	}
	if stats.TotalAllows < 2 {
		t.Errorf("Expected at least 2 allows, got %d", stats.TotalAllows)
	}
}

func TestAuditLogGetRecentEntries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	engine, err := NewEngine(dbPath, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Generate some decisions
	for range 5 {
		_ = engine.Check("file_read", "file_read", nil, "")
	}

	audit := NewAuditLog(engine.db)
	entries, err := audit.GetRecentEntries(3)
	if err != nil {
		t.Fatalf("GetRecentEntries failed: %v", err)
	}

	if len(entries) != 3 {
		t.Errorf("Expected 3 entries, got %d", len(entries))
	}
}

func TestAuditLogGetDeniedEntries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh:     true,
		RequireConfirmationCritical: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Generate allow and deny decisions
	_ = engine.Check("file_read", "file_read", nil, "")
	_ = engine.Check("shell_execute", "shell", map[string]string{"command": "rm -rf /"}, "")

	audit := NewAuditLog(engine.db)
	entries, err := audit.GetDeniedEntries(10)
	if err != nil {
		t.Fatalf("GetDeniedEntries failed: %v", err)
	}

	if len(entries) < 1 {
		t.Errorf("Expected at least 1 denied entry, got %d", len(entries))
	}

	for _, e := range entries {
		if e.Decision != "deny" {
			t.Errorf("Expected decision 'deny', got '%s'", e.Decision)
		}
	}
}

func TestAuditLogCountEntries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	engine, err := NewEngine(dbPath, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	audit := NewAuditLog(engine.db)

	// Count before any decisions
	initialCount, err := audit.CountEntries()
	if err != nil {
		t.Fatalf("CountEntries failed: %v", err)
	}

	// Generate some decisions
	_ = engine.Check("file_read", "file_read", nil, "")
	_ = engine.Check("file_write", "file_write", nil, "")

	// Count after decisions
	newCount, err := audit.CountEntries()
	if err != nil {
		t.Fatalf("CountEntries failed: %v", err)
	}

	if newCount != initialCount+2 {
		t.Errorf("Expected count to increase by 2, got %d -> %d", initialCount, newCount)
	}
}

func TestAuditLogPurgeOldEntries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	engine, err := NewEngine(dbPath, nil, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Generate some decisions
	for range 5 {
		_ = engine.Check("file_read", "file_read", nil, "")
	}

	audit := NewAuditLog(engine.db)

	// Purge entries older than 1 hour (should delete nothing since entries are fresh)
	deleted, err := audit.PurgeOldEntries(1 * time.Hour)
	if err != nil {
		t.Fatalf("PurgeOldEntries failed: %v", err)
	}
	if deleted != 0 {
		t.Errorf("Expected 0 deleted entries, got %d", deleted)
	}

	// Verify entries still exist
	count, err := audit.CountEntries()
	if err != nil {
		t.Fatalf("CountEntries failed: %v", err)
	}
	if count < 5 {
		t.Errorf("Expected at least 5 entries, got %d", count)
	}
}

func TestAuditLogCountEntriesByDecision(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "security.db")

	cfg := &config.SecurityConfig{
		RequireConfirmationHigh: true,
	}

	engine, err := NewEngine(dbPath, cfg, nil)
	if err != nil {
		t.Fatalf("NewEngine failed: %v", err)
	}
	defer engine.Close()

	// Generate various decisions
	_ = engine.Check("file_read", "file_read", nil, "")
	_ = engine.Check("file_delete", "file_delete", nil, "")
	_ = engine.Check("shell_execute", "shell", map[string]string{"command": "rm -rf /"}, "")

	audit := NewAuditLog(engine.db)
	counts, err := audit.CountEntriesByDecision()
	if err != nil {
		t.Fatalf("CountEntriesByDecision failed: %v", err)
	}

	if counts["allow"] < 1 {
		t.Errorf("Expected at least 1 allow, got %d", counts["allow"])
	}
}
