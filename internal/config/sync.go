package config

import (
	"context"
	"log/slog"
	"path/filepath"
	"sync"
	"time"
)

// ReloadFunc is called when a config file changes and needs to be reloaded.
// The commitHash identifies the git commit that introduced the change.
// Hooks are responsible for re-reading the file from disk if they need the
// new contents; the registry does not parse configs (they may be any format:
// JSON5, TOML, etc.) and therefore cannot hand the hook parsed old/new pairs.
type ReloadFunc func(commitHash string) error

// ConfigSyncer manages periodic config pulling from a git repo, merging,
// and triggering reload hooks when configs change.
type ConfigSyncer struct {
	cfg   ConfigSyncConfig

	nodeID   string
	logger   *slog.Logger

	// Git checkout operations
	checkout *GitCheckout

	// Config merge operations
	merger *Merger

	// Reload hook registry
	hooks *ReloadRegistry

	// Shared config base dir (typically ~/.meept)
	baseDir string

	// State
	lastCommitHash    string
	lastAppliedCommit string // empty until first successful merge; forces merge on first pull
	mu                sync.Mutex

	// Background ticker control
	ticker      *time.Ticker
	tickerStop  chan struct{}
	stopping    chan struct{}
	stopped     sync.WaitGroup
}

// NewConfigSyncer creates a ConfigSyncer.
// baseDir is the target directory for merged configs (e.g. ~/.meept).
func NewConfigSyncer(cfg ConfigSyncConfig, nodeID, baseDir string, logger *slog.Logger) (*ConfigSyncer, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	// Derive checkout dir from repo URL
	// Use a standard location: ~/.meept/.config-sync/<sanitized URL>
	checkoutPath := filepath.Join(baseDir, ".config-sync")
	sanitizedName := sanitizeRepoName(cfg.RepoURL)
	checkoutPath = filepath.Join(checkoutPath, sanitizedName)

	checkout, err := NewGitCheckout(cfg.RepoURL, checkoutPath, logger)
	if err != nil {
		return nil, err
	}

	merger := NewMerger(baseDir, checkoutPath, nodeID, logger)
	hooks := NewReloadRegistry()

	return &ConfigSyncer{
		cfg:       cfg,
		nodeID:    nodeID,
		logger:    logger,
		checkout:  checkout,
		merger:    merger,
		hooks:     hooks,
		baseDir:   baseDir,
		tickerStop: make(chan struct{}),
		stopping:   make(chan struct{}),
	}, nil
}

// Start begins the periodic pull loop. It returns immediately;
// call Stop() to shut down.
func (s *ConfigSyncer) Start(ctx context.Context) {
	s.stopped.Add(1)
	go s.run(ctx)
}

func (s *ConfigSyncer) run(ctx context.Context) {
	defer s.stopped.Done()

	// Do an initial pull immediately
	if err := s.pullOnce(ctx); err != nil {
		s.logger.Warn("config sync: initial pull failed", "error", err)
	}

	// Set up ticker for periodic pulls
	s.ticker = time.NewTicker(s.cfg.PullSchedule)
	defer s.ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-s.stopping:
			return
		case <-s.ticker.C:
			if err := s.pullOnce(ctx); err != nil {
				s.logger.Warn("config sync: periodic pull failed", "error", err)
			}
		}
	}
}

// pullOnce fetches from the git repo, merges configs, and triggers reload hooks.
// On the very first pull (when lastAppliedCommit is empty), the merge is forced
// even if HEAD did not change, so that a fresh shallow clone's configs are
// applied on the first cycle rather than silently skipped.
func (s *ConfigSyncer) pullOnce(ctx context.Context) error {
	newCommit, changed, err := s.checkout.Pull(ctx)
	if err != nil {
		return &ConfigSyncError{Op: "pull", Path: s.cfg.RepoURL, Err: err}
	}

	s.mu.Lock()
	s.lastCommitHash = newCommit
	// firstPull is true when no commit has ever been successfully applied.
	firstPull := s.lastAppliedCommit == ""
	s.mu.Unlock()

	// Skip merge only when this isn't the first pull AND nothing changed.
	if !changed && !firstPull {
		return nil
	}

	// Merge configs from checkout dir
	result, err := s.merger.Merge(newCommit)
	if err != nil {
		return &ConfigSyncError{Op: "merge", Path: s.baseDir, Err: err}
	}

	// Record that we have now applied a commit, so subsequent no-change pulls
	// skip the merge as expected.
	s.mu.Lock()
	s.lastAppliedCommit = newCommit
	s.mu.Unlock()

	if len(result.Errors) > 0 {
		for _, e := range result.Errors {
			s.logger.Warn("config sync: merge error", "error", e)
		}
	}

	s.logger.Info("config sync: pull completed", "files_applied", len(result.FilesApplied), "commit", newCommit)

	// Trigger reload hooks for applied files
	for _, f := range result.FilesApplied {
		s.hooks.Trigger(f, newCommit)
	}

	return nil
}

// SetBaseDir sets the config base directory after creation.
func (s *ConfigSyncer) SetBaseDir(baseDir string) {
	s.mu.Lock()
	s.baseDir = baseDir
	s.mu.Unlock()
}

// NodeID returns the node identifier used for per-node overrides.
func (s *ConfigSyncer) NodeID() string {
	return s.nodeID
}

// LastCommitHash returns the latest known commit hash.
func (s *ConfigSyncer) LastCommitHash() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastCommitHash
}

// LastAppliedCommit returns the commit hash of the most recent successfully
// merged pull. Returns empty string if no merge has ever completed.
func (s *ConfigSyncer) LastAppliedCommit() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.lastAppliedCommit
}

// Status returns current sync status information.
func (s *ConfigSyncer) Status() SyncStatus {
	s.mu.Lock()
	defer s.mu.Unlock()

	status := SyncStatus{
		Enabled:         s.cfg.Enabled,
		RepoURL:         s.cfg.RepoURL,
		PullRate:        s.cfg.PullSchedule.String(),
		NodeID:          s.nodeID,
		LastCommit:      s.lastCommitHash,
		LastAppliedCommit: s.lastAppliedCommit,
		Checkout:        s.checkout.Path(),
	}

	if dirty, err := s.checkout.IsDirty(); err == nil {
		status.Dirty = dirty
	}

	return status
}

// RepoURL returns the configured repo URL.
func (s *ConfigSyncer) RepoURL() string {
	return s.cfg.RepoURL
}

// PullSchedule returns the pull interval.
func (s *ConfigSyncer) PullSchedule() time.Duration {
	return s.cfg.PullSchedule
}

// Stop halts the periodic pull loop.
func (s *ConfigSyncer) Stop() {
	select {
	case <-s.stopping:
		return // already stopped
	default:
	}
	close(s.stopping)
	close(s.tickerStop)
	s.stopped.Wait()
	s.logger.Info("config syncer: stopped")
}

// RegisterReloadHook adds a callback invoked when a specific config file path changes.
func (s *ConfigSyncer) RegisterReloadHook(path string, fn ReloadFunc) {
	s.hooks.Register(path, fn)
}

// SyncStatus carries snapshot data about sync state.
type SyncStatus struct {
	Enabled           bool   `json:"enabled"`
	RepoURL           string `json:"repo_url"`
	PullRate          string `json:"pull_rate"`
	NodeID            string `json:"node_id"`
	LastCommit        string `json:"last_commit"`
	LastAppliedCommit string `json:"last_applied_commit,omitempty"`
	Checkout          string `json:"checkout"`
	Dirty             bool   `json:"dirty"`
}

// ReloadRegistry is exported for use in daemon wiring.
func (s *ConfigSyncer) ReloadRegistry() *ReloadRegistry {
	return s.hooks
}

// sanitizeRepoName strips URL scheme and path delimiters, converting to a
// filesystem-safe directory name. E.g. "git@github.com:user/repo.git" → "github.com_user_repo.git".
func sanitizeRepoName(url string) string {
	// Remove common git URL prefixes
	s := url
	s = replace(s, "git@", "_")
	s = replace(s, "://", "_")
	s = replace(s, "/", "_")
	s = replace(s, ":", "_")
	return s
}

func replace(s, old, new string) string {
	if idx := indexOf(s, old); idx >= 0 {
		return s[:idx] + new + s[idx+len(old):]
	}
	return s
}

func indexOf(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}
