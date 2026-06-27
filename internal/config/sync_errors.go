package config

import "fmt"

// ConfigSyncError wraps contextual information for sync-phase errors.
type ConfigSyncError struct {
	Op   string // "pull", "merge", "reload", "checkout"
	Path string // config file or repo path
	Err  error
}

func (e *ConfigSyncError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("config sync (%s) %s: %s", e.Op, e.Path, e.Err)
	}
	return fmt.Sprintf("config sync (%s): %s", e.Op, e.Err)
}

func (e *ConfigSyncError) Unwrap() error {
	return e.Err
}

// Sentinel errors for the config sync subsystem.
var (
	ErrGitConflict     = fmt.Errorf("config sync: git conflict (manual resolution required)")
	ErrConfigInvalid   = fmt.Errorf("config sync: invalid JSON5 syntax")
	ErrReloadFailed    = fmt.Errorf("config sync: hot-reload failed")
	ErrRepoUnreachable = fmt.Errorf("config sync: git repo unreachable")
	ErrCheckoutDirty   = fmt.Errorf("config sync: working tree has uncommitted changes")
)
