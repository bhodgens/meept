package config

import (
	"fmt"
	"time"
)

// BackupConfig holds configuration for the git backup scheduler.
type BackupConfig struct {
	Enabled       bool          `json:"enabled"        toml:"enabled"`
	RepoURL       string        `json:"repo_url"       toml:"repo_url"`
	CheckoutDir   string        `json:"checkout_dir"   toml:"checkout_dir"`
	Schedule      time.Duration `json:"schedule"       toml:"schedule"`
	RetentionDays int           `json:"retention_days" toml:"retention_days"`
	NodeID        string        `json:"node_id"        toml:"node_id"`
}

// DefaultBackupConfig returns sensible defaults.
func DefaultBackupConfig() BackupConfig {
	return BackupConfig{
		Enabled:       false,
		Schedule:      24 * time.Hour,
		RetentionDays: 12,
	}
}

// ErrBackupInvalid is returned when BackupConfig validation fails.
var ErrBackupInvalid = fmt.Errorf("backup config is invalid")

// Validate checks that BackupConfig is valid when enabled.
func (c *BackupConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.RepoURL == "" {
		return ErrBackupInvalid
	}

	if c.Schedule <= 0 {
		return ErrBackupInvalid
	}

	if c.RetentionDays <= 0 {
		return ErrBackupInvalid
	}

	return nil
}

// IsValidated reports whether c is enabled and fully configured.
func (c *BackupConfig) IsValidated() bool {
	return c.Enabled && c.RepoURL != "" && c.Schedule > 0 && c.RetentionDays > 0
}
