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
