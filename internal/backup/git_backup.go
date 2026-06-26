package backup

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/go-git/go-git/v5"
)

// GitBackupScheduler performs periodic git-backed backups of local SQLite databases.
type GitBackupScheduler struct {
	cfg           config.BackupConfig
	dataDir       string
	logger        *slog.Logger
	mu            sync.Mutex
	backupDir     string // local checkout directory
	repo          *git.Repository
	nodeID        string
	running       bool
	stopCh        chan struct{}
	onBackupDone  func(*BackupManifest, error)
}

// NewGitBackupScheduler creates a new scheduler from config.
func NewGitBackupScheduler(cfg config.BackupConfig, logger *slog.Logger) (*GitBackupScheduler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, Wrap("scheduler_new", err)
	}

	nodeID := cfg.NodeID
	if nodeID == "" {
		nodeID = defaultNodeID()
	}

	backupDir := cfg.CheckoutDir
	if backupDir == "" {
		home, _ := os.UserHomeDir()
		backupDir = filepath.Join(home, ".meept", "backups-git")
	}

	s := &GitBackupScheduler{
		cfg:       cfg,
		dataDir:   backupDir,
		logger:    logger.With("component", "backup-scheduler"),
		nodeID:    nodeID,
		backupDir: backupDir,
		stopCh:    make(chan struct{}),
	}

	return s, nil
}

// SetOnBackupDone sets a callback invoked after each backup run.
func (s *GitBackupScheduler) SetOnBackupDone(fn func(*BackupManifest, error)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.onBackupDone = fn
}

// Start begins the scheduled backup loop. It runs until ctx is cancelled.
func (s *GitBackupScheduler) Start(ctx context.Context) {
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		s.logger.Warn("backup scheduler already running")
		return
	}
	s.running = true
	s.mu.Unlock()

	s.logger.Info("backup scheduler starting",
		"schedule", s.cfg.Schedule,
		"retention_days", s.cfg.RetentionDays,
		"node_id", s.nodeID)

	// Initialize git repo
	if err := s.initRepo(); err != nil {
		s.logger.Error("backup: failed to initialize git repo", "error", err)
		return
	}

	// Run immediate first backup
	if err := s.runBackup(ctx); err != nil {
		s.logger.Error("backup: first backup failed", "error", err)
	}

	// Schedule ticker
	ticker := time.NewTicker(s.cfg.Schedule)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			s.logger.Info("backup scheduler stopping")
			return
		case <-s.stopCh:
			s.logger.Info("backup scheduler stopped")
			return
		case <-ticker.C:
			if err := s.runBackup(ctx); err != nil {
				s.logger.Error("backup: scheduled backup failed", "error", err)
			}
		}
	}
}

// Stop gracefully stops the scheduler.
func (s *GitBackupScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopCh != nil {
		close(s.stopCh)
	}
	s.running = false
}

// RunNow triggers an immediate backup and push.
func (s *GitBackupScheduler) RunNow() error {
	s.mu.Lock()
	repo := s.repo
	running := s.running
	s.mu.Unlock()

	if !running || repo == nil {
		if err := s.initRepo(); err != nil {
			return err
		}
	}

	return s.runBackup(context.TODO())
}

func (s *GitBackupScheduler) initRepo() error {
	repoPath := s.backupDir
	repo, _, err := GitInit(repoPath)
	if err != nil {
		// If the repo URL is set, try to clone
		if s.cfg.RepoURL != "" && repo != nil {
			// Use EnsureRemote helper
			if createErr := EnsureRemote(repo, "origin", s.cfg.RepoURL); createErr != nil {
				// Already exists error is fine; try pull anyway
			}
			// Try to fetch
			if fetchErr := repo.Fetch(&git.FetchOptions{}); fetchErr != nil && fetchErr != git.NoErrAlreadyUpToDate {
				s.logger.Warn("backup: fetch failed (remote may not exist yet)", "error", fetchErr)
			}
		} else if repo == nil {
			return Wrap("init_repo", err)
		}
	}

	s.mu.Lock()
	s.repo = repo
	s.mu.Unlock()

	return nil
}

// runBackup performs a single backup cycle: checkpoint DBs → compress → manifest → git commit → push.
func (s *GitBackupScheduler) runBackup(ctx context.Context) error {
	s.logger.Info("backup: starting backup cycle")

	// Prune old backups before beginning
	if err := s.pruneOldBackups(); err != nil {
		s.logger.Warn("backup: prune failed (will continue anyway)", "error", err)
	}

	// Get database paths
	dbPaths, err := GetLocalDBPaths(s.dataDir)
	if err != nil {
		s.logger.Error("backup: failed to resolve DB paths", "error", err)
		s.invokeCallback(nil, err)
		return err
	}

	// Create backup directory for this run
	date := time.Now().UTC().Format("2006-01-02")
	backupSubdir := filepath.Join(s.dataDir, "backups", date, s.nodeID)
	if err := os.MkdirAll(backupSubdir, 0o700); err != nil {
		err = Wrap("backup_create_dir", err)
		s.logger.Error("backup: failed to create backup directory", "error", err)
		s.invokeCallback(nil, err)
		return err
	}

	// Compress each DB
	var dbInfos []DatabaseInfo
	for _, dbPath := range dbPaths {
		name := filepath.Base(dbPath)
		compressedPath := filepath.Join(backupSubdir, name+".zst")

		compressedSize, err := CompressFile(dbPath, compressedPath)
		if err != nil {
			s.logger.Error("backup: compression failed", "file", name, "error", err)
			err = Wrap("backup_compress", err)
			s.invokeCallback(nil, err)
			return err
		}

		sha, err := ComputeSHA256(compressedPath)
		if err != nil {
			s.logger.Error("backup: sha256 failed", "file", name, "error", err)
			err = Wrap("backup_sha256", err)
			s.invokeCallback(nil, err)
			return err
		}

		info, _ := os.Stat(dbPath)

		dbInfos = append(dbInfos, DatabaseInfo{
			Name:             name,
			CompressedSize:   compressedSize,
			UncompressedSize: info.Size(),
			SHA256:           sha,
			CompressedPath:   compressedPath,
		})
	}

	// Generate manifest
	manifest := &BackupManifest{
		NodeID:    s.nodeID,
		Timestamp: time.Now().UTC(),
		Databases: dbInfos,
		SyncMetadata: SyncMetadata{
			PeersSynced: []string{},
		},
	}

	manifestPath := filepath.Join(backupSubdir, "manifest.json")
	if err := manifest.Save(manifestPath); err != nil {
		s.logger.Error("backup: failed to save manifest", "error", err)
		err = Wrap("backup_manifest_save", err)
		s.invokeCallback(nil, err)
		return err
	}

	// Git add, commit, push
	start := time.Now()
	// Collect all file paths for git tracking
	var gitFiles []string
	for _, db := range dbInfos {
		gitFiles = append(gitFiles, db.CompressedPath)
	}
	gitFiles = append(gitFiles, manifestPath)

	message := fmt.Sprintf("backup: %s %d databases (%.1f MB compressed)",
		manifest.Timestamp.Format("15:04:05"),
		len(dbInfos),
		float64(manifest.TotalCompressedSize())/(1024*1024))

	if err := s.gitCommitAndPush(gitFiles, message); err != nil {
		s.logger.Error("backup: git push failed", "error", err, "retryable", IsRetryable(err))
		s.invokeCallback(manifest, err)
		return err
	}

	duration := time.Since(start)
	s.logger.Info("backup: push completed",
		"node_id", s.nodeID,
		"backup_dir", backupSubdir,
		"compressed_size", manifest.TotalCompressedSize(),
		"db_count", len(dbInfos),
		"duration_ms", duration.Milliseconds())

	s.invokeCallback(manifest, nil)
	return nil
}

func (s *GitBackupScheduler) gitCommitAndPush(files []string, message string) error {
	s.mu.Lock()
	repo := s.repo
	s.mu.Unlock()

	if repo == nil {
		if err := s.initRepo(); err != nil {
			return err
		}
		s.mu.Lock()
		repo = s.repo
		s.mu.Unlock()
	}

	return GitAddCommitPush(repo, files, message)
}

func (s *GitBackupScheduler) invokeCallback(manifest *BackupManifest, err error) {
	s.mu.Lock()
	fn := s.onBackupDone
	s.mu.Unlock()
	if fn != nil {
		fn(manifest, err)
	}
}

// pruneOldBackups removes backup directories older than retention_days.
func (s *GitBackupScheduler) pruneOldBackups() error {
	s.mu.Lock()
	repo := s.repo
	s.mu.Unlock()

	if repo == nil {
		return nil
	}

	// List date directories
	backups, err := GitListBackups(repo, s.nodeID)
	if err != nil {
		// Non-fatal: if backups listing fails, just skip pruning
		s.logger.Debug("backup: failed to list backups for pruning", "error", err)
		return nil
	}

	cutoff := time.Now().UTC().AddDate(0, 0, -s.cfg.RetentionDays)

	for _, dateStr := range backups {
		t, parseErr := time.Parse("2006-01-02", dateStr)
		if parseErr != nil {
			continue
		}
		if t.Before(cutoff) {
			dir := filepath.Join(s.dataDir, "backups", dateStr, s.nodeID)
			s.logger.Info("backup: pruning old backup",
				"date", dateStr,
				"path", dir)
			if err := os.RemoveAll(dir); err != nil {
				s.logger.Warn("backup: failed to prune old backup", "date", dateStr, "error", err)
			}
		}
	}

	return nil
}

// defaultNodeID returns a unique-ish node identifier from hostname.
func defaultNodeID() string {
	hostname, err := os.Hostname()
	if err != nil {
		return "unknown"
	}
	// Truncate to 20 chars for git safety
	if len(hostname) > 20 {
		hostname = hostname[:20]
	}
	return strings.ReplaceAll(hostname, ".", "-")
}
