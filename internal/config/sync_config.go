package config

import (
	"fmt"
	"time"
)

// PeerSyncConfig holds configuration for peer-to-peer backup synchronization.
// This is separate from SyncConfig (memory sync/hydration) in schema.go.
type PeerSyncConfig struct {
	// Enabled turns on synchronous pull from backup repo
	Enabled bool `json:"enabled" toml:"enabled"`
	// Peers lists known peer node IDs to sync with
	Peers []string `json:"peers" toml:"peers"`
	// PullSchedule is the interval between automatic pull cycles (0 = disabled)
	PullSchedule time.Duration `json:"pull_schedule" toml:"pull_schedule"`
	// MaxMergeMinutes is the maximum time allowed for a merge operation (default 10m)
	MaxMergeMinutes int `json:"max_merge_minutes" toml:"max_merge_minutes"`
	// RepoURL is the git repo URL for backup synchronization (inherited from backup.repo_url)
	RepoURL string `json:"repo_url" toml:"repo_url"`
}

// DefaultPeerSyncConfig returns sensible defaults.
func DefaultPeerSyncConfig() PeerSyncConfig {
	return PeerSyncConfig{
		Enabled:         false,
		Peers:           []string{},
		PullSchedule:    time.Hour,
		MaxMergeMinutes: 10,
	}
}

// Validate checks that PeerSyncConfig is valid when enabled.
func (c *PeerSyncConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if len(c.Peers) == 0 {
		return fmt.Errorf("sync: enabled without peers; peers list is required")
	}

	if c.PullSchedule <= 0 {
		return fmt.Errorf("sync: enabled without a positive pull_schedule")
	}

	if c.MaxMergeMinutes <= 0 {
		c.MaxMergeMinutes = 10 // default
	}

	return nil
}

// IsValidated reports whether c is enabled and fully configured.
func (c *PeerSyncConfig) IsValidated() bool {
	return c.Enabled && len(c.Peers) > 0 && c.PullSchedule > 0
}

// ConfigSyncConfig holds configuration for config-file synchronization via git.
// This is separate from PeerSyncConfig (backup sync) — it pulls config files
// from a shared repo and merges them into the local config directory.
type ConfigSyncConfig struct {
	// Enabled turns on periodic config pulling from a shared git repo
	Enabled bool `json:"enabled" toml:"enabled"`
	// RepoURL is the git repo URL containing shared configs
	RepoURL string `json:"repo_url" toml:"repo_url"`
	// PullSchedule is the interval between config pulls (0 = disabled)
	PullSchedule time.Duration `json:"pull_schedule" toml:"pull_schedule"`
	// ConflictMode resolves conflicts: "local-wins", "remote-wins", "manual"
	ConflictMode string `json:"conflict_mode" toml:"conflict_mode"`
}

// DefaultConfigSyncConfig returns sensible defaults.
func DefaultConfigSyncConfig() ConfigSyncConfig {
	return ConfigSyncConfig{
		Enabled:      false,
		PullSchedule: 5 * time.Minute,
		ConflictMode: "local-wins",
	}
}

var ErrConfigSyncInvalid = fmt.Errorf("config sync config is invalid")

// Validate checks that ConfigSyncConfig is valid when enabled.
func (c *ConfigSyncConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.RepoURL == "" {
		return ErrConfigSyncInvalid
	}

	if c.PullSchedule <= 0 {
		return ErrConfigSyncInvalid
	}

	return nil
}

// IsValidated reports whether c is enabled and fully configured.
func (c *ConfigSyncConfig) IsValidated() bool {
	return c.Enabled && c.RepoURL != "" && c.PullSchedule > 0
}
