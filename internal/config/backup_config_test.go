package config

import (
	"testing"
	"time"
)

func TestDefaultBackupConfig(t *testing.T) {
	cfg := DefaultBackupConfig()

	if cfg.Enabled {
		t.Error("enabled should be false by default")
	}
	if cfg.Schedule != 24*time.Hour {
		t.Errorf("schedule: got %v, want %v", cfg.Schedule, 24*time.Hour)
	}
	if cfg.RetentionDays != 12 {
		t.Errorf("retention_days: got %d, want %d", cfg.RetentionDays, 12)
	}
	if cfg.RepoURL != "" {
		t.Errorf("repo_url should be empty by default, got %q", cfg.RepoURL)
	}
}

func TestBackupConfig_Validate_Disabled(t *testing.T) {
	cfg := BackupConfig{
		Enabled: false,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("disabled config should not require validation, got: %v", err)
	}
}

func TestBackupConfig_Validate_NoRepo(t *testing.T) {
	cfg := BackupConfig{
		Enabled:   true,
		Schedule:  time.Hour,
		RetentionDays: 7,
	}
	if err := cfg.Validate(); err != ErrBackupInvalid {
		t.Errorf("expected ErrBackupInvalid for missing repo_url, got %v", err)
	}
}

func TestBackupConfig_Validate_ZeroSchedule(t *testing.T) {
	cfg := BackupConfig{
		Enabled:    true,
		RepoURL:    "https://example.com/backup.git",
		Schedule:   0,
		RetentionDays: 7,
	}
	if err := cfg.Validate(); err != ErrBackupInvalid {
		t.Errorf("expected ErrBackupInvalid for zero schedule, got %v", err)
	}
}

func TestBackupConfig_Validate_ZeroRetention(t *testing.T) {
	cfg := BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      time.Hour,
		RetentionDays: 0,
	}
	if err := cfg.Validate(); err != ErrBackupInvalid {
		t.Errorf("expected ErrBackupInvalid for zero retention, got %v", err)
	}
}

func TestBackupConfig_Validate_Success(t *testing.T) {
	cfg := BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      24 * time.Hour,
		RetentionDays: 12,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error for valid config, got: %v", err)
	}
}

func TestBackupConfig_IsValidated(t *testing.T) {
	valid := BackupConfig{
		Enabled:       true,
		RepoURL:       "https://example.com/backup.git",
		Schedule:      time.Hour,
		RetentionDays: 7,
	}
	if !valid.IsValidated() {
		t.Error("expected valid config to report IsValidated=true")
	}

	invalid := BackupConfig{
		Enabled: true,
	}
	if invalid.IsValidated() {
		t.Error("expected partial config to report IsValidated=false")
	}

	disabled := BackupConfig{
		Enabled: false,
	}
	if disabled.IsValidated() {
		t.Error("expected disabled config to report IsValidated=false")
	}
}

func TestConfig_ValidateAll(t *testing.T) {
	// Test that default config passes validation
	cfg := DefaultConfig()
	if err := cfg.ValidateAll(); err != nil {
		t.Errorf("Default config should be valid: %v", err)
	}

	// Test that backup config errors are wrapped
	cfg2 := DefaultConfig()
	cfg2.Backup.Enabled = true
	// Missing RepoURL should fail
	if err := cfg2.ValidateAll(); err == nil {
		t.Error("Expected error for invalid backup config, got nil")
	} else if !contains(err.Error(), "backup config") {
		t.Errorf("Expected 'backup config' in error message, got: %v", err)
	}

	// Test that peer sync config errors are wrapped
	cfg3 := DefaultConfig()
	cfg3.PeerSync.Enabled = true
	// Missing Peers should fail
	if err := cfg3.ValidateAll(); err == nil {
		t.Error("Expected error for invalid peer sync config, got nil")
	} else if !contains(err.Error(), "peer sync config") {
		t.Errorf("Expected 'peer sync config' in error message, got: %v", err)
	}

	// Test that config sync config errors are wrapped
	cfg4 := DefaultConfig()
	cfg4.ConfigSync.Enabled = true
	// Missing RepoURL should fail
	if err := cfg4.ValidateAll(); err == nil {
		t.Error("Expected error for invalid config sync config, got nil")
	} else if !contains(err.Error(), "config sync config") {
		t.Errorf("Expected 'config sync config' in error message, got: %v", err)
	}
}

func contains(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
