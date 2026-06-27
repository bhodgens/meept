package config

import (
	"testing"
	"time"
)

func TestDefaultConfigSyncConfig(t *testing.T) {
	cfg := DefaultConfigSyncConfig()

	if cfg.Enabled {
		t.Error("enabled should be false by default")
	}
	if cfg.PullSchedule != 5*time.Minute {
		t.Errorf("pull_schedule: got %v, want %v", cfg.PullSchedule, 5*time.Minute)
	}
	if cfg.RepoURL != "" {
		t.Errorf("repo_url should be empty by default, got %q", cfg.RepoURL)
	}
}

func TestConfigSyncConfig_Validate_Disabled(t *testing.T) {
	cfg := ConfigSyncConfig{
		Enabled: false,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("disabled config should not require validation, got: %v", err)
	}
}

func TestConfigSyncConfig_Validate_NoRepo(t *testing.T) {
	cfg := ConfigSyncConfig{
		Enabled:      true,
		PullSchedule: 5 * time.Minute,
	}
	if err := cfg.Validate(); err != ErrConfigSyncInvalid {
		t.Errorf("expected ErrConfigSyncInvalid for missing repo_url, got %v", err)
	}
}

func TestConfigSyncConfig_Validate_ZeroSchedule(t *testing.T) {
	cfg := ConfigSyncConfig{
		Enabled:      true,
		RepoURL:      "https://example.com/config.git",
		PullSchedule: 0,
	}
	if err := cfg.Validate(); err != ErrConfigSyncInvalid {
		t.Errorf("expected ErrConfigSyncInvalid for zero schedule, got %v", err)
	}
}

func TestConfigSyncConfig_Validate_Success(t *testing.T) {
	cfg := ConfigSyncConfig{
		Enabled:      true,
		RepoURL:      "https://example.com/config.git",
		PullSchedule: 5 * time.Minute,
	}
	if err := cfg.Validate(); err != nil {
		t.Errorf("expected no error for valid config, got: %v", err)
	}
}

func TestConfigSyncConfig_IsValidated(t *testing.T) {
	valid := ConfigSyncConfig{
		Enabled:      true,
		RepoURL:      "https://example.com/config.git",
		PullSchedule: 5 * time.Minute,
	}
	if !valid.IsValidated() {
		t.Error("expected valid config to report IsValidated=true")
	}

	invalid := ConfigSyncConfig{
		Enabled:      true,
		PullSchedule: 5 * time.Minute,
	}
	if invalid.IsValidated() {
		t.Error("expected partial config to report IsValidated=false")
	}

	disabled := ConfigSyncConfig{
		Enabled: false,
	}
	if disabled.IsValidated() {
		t.Error("expected disabled config to report IsValidated=false")
	}
}
