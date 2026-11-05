package session

import (
	"errors"
	"testing"
)

func TestFormatCatalog(t *testing.T) {
	// G1: Unit: version constants + catalog chain validation
	migrations := []SessionFormatMigration{
		{Name: "v0 to v1", FromVersion: 0, ToVersion: 1},
	}
	catalog, err := NewSessionFormatCatalog(migrations)
	if err != nil {
		t.Fatalf("NewSessionFormatCatalog failed: %v", err)
	}

	// Test ErrSessionFormatNewer
	_, err = catalog.Plan(2)
	if !errors.Is(err, ErrSessionFormatNewer) {
		t.Errorf("expected ErrSessionFormatNewer, got %v", err)
	}

	// Test ErrSessionFormatGap
	invalidMigrations := []SessionFormatMigration{
		{Name: "v0 to v1", FromVersion: 0, ToVersion: 1},
		{Name: "v2 to v3", FromVersion: 2, ToVersion: 3},
	}
	_, err = NewSessionFormatCatalog(invalidMigrations)
	if err == nil {
		t.Error("expected error from invalid migrations (gap), got nil")
	}
}
