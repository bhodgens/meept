package backup

import (
	"context"
	"database/sql"
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

// SyncPuller performs periodic git pulls and merges peer backups into the local gossip DB.
type SyncPuller struct {
	cfg        config.PeerSyncConfig
	localDB    *sql.DB
	gossipDB   *sql.DB
	peers      []string
	repoPath   string
	nodeID     string
	logger     *slog.Logger
	tempMgr    *TempManager
	metaStore  *SyncMetadataStore
	lastSyncMu sync.Mutex
	lastSync   map[string]time.Time // peer -> last successful sync
}

// NewSyncPuller creates a new sync puller from config and database connections.
func NewSyncPuller(cfg config.PeerSyncConfig, localDB, gossipDB *sql.DB) (*SyncPuller, error) {
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("backup: invalid sync config: %w", err)
	}

	if gossipDB == nil {
		return nil, fmt.Errorf("backup: gossipDB is required for sync puller")
	}

	home, _ := os.UserHomeDir()
	backupDir := filepath.Join(home, ".meept", "backups-git")
	if cfg.RepoURL != "" {
		// Use the backup checkout dir from the backup config
		// (we need to retrieve it from the backup config separately)
	}

	_ = localDB // currently unused (gossipDB is the target for merge)

	nodeID, _ := os.Hostname()
	if len(nodeID) > 20 {
		nodeID = nodeID[:20]
	}
	nodeID = cleanHostname(nodeID)

	tempMgr, err := NewTempManager(filepath.Join(home, ".meept"))
	if err != nil {
		return nil, fmt.Errorf("backup: create temp manager: %w", err)
	}

	metaStore := NewSyncMetadataStore(gossipDB)
	if err := metaStore.EnsureTable(); err != nil {
		tempMgr.Cleanup()
		return nil, fmt.Errorf("backup: ensure sync_metadata table: %w", err)
	}

	return &SyncPuller{
		cfg:       cfg,
		localDB:   localDB,
		gossipDB:  gossipDB,
		peers:     cfg.Peers,
		repoPath:  backupDir,
		nodeID:    nodeID,
		logger:    slog.New(slog.NewTextHandler(os.Stderr, nil)).With("component", "sync-puller"),
		tempMgr:   tempMgr,
		metaStore: metaStore,
		lastSync:  make(map[string]time.Time),
	}, nil
}

// Start begins the scheduled pull loop. It runs until ctx is cancelled.
func (p *SyncPuller) Start(ctx context.Context) {
	p.logger.Info("sync puller starting",
		"peers", p.peers,
		"pull_schedule", p.cfg.PullSchedule)

	// Initial pull
	go func() {
		if err := p.pullOnce(context.Background()); err != nil {
			p.logger.Error("sync: initial pull failed", "error", err)
		}
	}()

	if p.cfg.PullSchedule <= 0 {
		p.logger.Info("sync: scheduled pulls disabled (pull_schedule=0)")
		return
	}

	ticker := time.NewTicker(p.cfg.PullSchedule)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			p.logger.Info("sync puller stopping")
			return
		case <-ticker.C:
			if err := p.pullOnce(context.Background()); err != nil {
				p.logger.Error("sync: scheduled pull failed", "error", err)
			}
		}
	}
}

// Stop gracefully stops the puller.
func (p *SyncPuller) Stop() {
	p.tempMgr.Cleanup()
}

// PullNow triggers an immediate sync.
func (p *SyncPuller) PullNow() error {
	return p.pullOnce(context.Background())
}

// pullOnce performs a single sync cycle: git pull + merge each peer.
func (p *SyncPuller) pullOnce(ctx context.Context) error {
	p.logger.Info("sync: starting pull cycle")

	// Step 1: open git repo and pull
	repo, err := GitCloneOrOpen(p.repoPath, p.cfg.RepoURL)
	if err != nil {
		return &SyncError{
			Op:    "pull",
			Err:   fmt.Errorf("git pull: %w", err),
			Message: "failed to open or clone backup repo",
		}
	}

	if err := GitPullRebase(repo); err != nil && err != git.NoErrAlreadyUpToDate {
		p.logger.Warn("sync: git pull had conflicts", "error", err)
		// Continue anyway; we may still have useful peer data
	}

	// Step 2: find and merge each peer's backup
	for _, peerID := range p.peers {
		if err := p.mergePeer(ctx, peerID); err != nil {
			p.logger.Error("sync: merge failed for peer",
				"peer_id", peerID,
				"error", err)

			// Record the error in metadata
			_ = p.metaStore.SetLastError(peerID, err.Error())

			// Don't fail the whole cycle for one peer
			continue
		}
	}

	p.logger.Info("sync: pull cycle complete")
	return nil
}

// mergePeer finds and merges a specific peer's latest backup.
func (p *SyncPuller) mergePeer(ctx context.Context, peerID string) error {
	// Search for the peer's backup in the repo checkout
	backupDBPath, err := findPeerBackup(p.repoPath, peerID)
	if err != nil {
		return SyncWrap("find_backup", err)
	}

	// Decompress to temp
	peerDBPath, err := p.tempMgr.ReservePeerDB(backupDBPath)
	if err != nil {
		return SyncWrap("reserve_peer_db", fmt.Errorf("decompress: %w", err))
	}
	defer p.tempMgr.Remove(peerDBPath)

	// Merge with timeout
	timeout := time.Duration(p.cfg.MaxMergeMinutes) * time.Minute
	if timeout <= 0 {
		timeout = 10 * time.Minute
	}

	stats, err := MergePeerDBWithContext(ctx, p.gossipDB, peerDBPath, peerID, timeout)
	if err != nil {
		return SyncWrap("merge", fmt.Errorf("merge: %w", err))
	}

	// Update metadata
	now := time.Now().UTC()
	p.lastSyncMu.Lock()
	p.lastSync[peerID] = now
	p.lastSyncMu.Unlock()

	if err := p.metaStore.SetLastSync(peerID, now); err != nil {
		p.logger.Warn("sync: failed to record last sync time", "peer_id", peerID, "error", err)
	}
	if err := p.metaStore.SetLastMergeStats(peerID, stats); err != nil {
		p.logger.Warn("sync: failed to record merge stats", "peer_id", peerID, "error", err)
	}
	// Clear error if we succeeded
	_ = p.metaStore.SetLastError(peerID, "")

	p.logger.Info("sync: merge complete",
		"peer_id", peerID,
		"sessions", stats.SessionsMerged,
		"turns", stats.TurnsMerged,
		"memories", stats.MemoriesMerged)

	return nil
}

// PeerStatus returns the current peer sync status.
func (p *SyncPuller) PeerStatus() (map[string]SyncStatus, error) {
	return p.metaStore.GetAllSyncStatus()
}

// findPeerBackup searches the repo checkout for the latest backup for a peer.
func findPeerBackup(repoPath, peerID string) (string, error) {
	backupsDir := filepath.Join(repoPath, "backups")
	entries, err := os.ReadDir(backupsDir)
	if err != nil {
		if os.IsNotExist(err) {
			return "", SyncWrap("find", ErrPeerNotFound)
		}
		return "", SyncWrap("find_read_dir", err)
	}

	// Find date directories that contain this peer's backup
	var latestPath string
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		// Only process date-formatted directories
		dateDir := filepath.Join(backupsDir, e.Name())
		peerDir := filepath.Join(dateDir, peerID)

		peerEntries, err := os.ReadDir(peerDir)
		if err != nil {
			continue
		}

		for _, pe := range peerEntries {
			if pe.IsDir() {
				continue
			}
			name := pe.Name()
			// Look for .db.zst files
			if len(name) > 4 && name[len(name)-4:] == ".zst" && strings.HasSuffix(name, ".db.zst") {
				// We want the latest by date, so just take the last one found
				latestPath = filepath.Join(peerDir, name)
			}
		}
	}

	if latestPath == "" {
		return "", SyncWrap("find", ErrPeerNotFound)
	}

	return latestPath, nil
}

// GitCloneOrOpen clones or opens a git repo at the given path.
func GitCloneOrOpen(path, url string) (*git.Repository, error) {
	repo, err := git.PlainOpen(path)
	if err == nil {
		return repo, nil
	}

	if !os.IsNotExist(err) {
		return nil, Wrap("git_open", err)
	}

	if url == "" {
		return nil, Wrap("git_clone", fmt.Errorf("no repo URL and no existing repo at %s", path))
	}

	repo, err = git.PlainClone(path, false, &git.CloneOptions{
		URL: url,
	})
	if err != nil {
		return nil, Wrap("git_clone", err)
	}

	return repo, nil
}

// cleanHostname removes dots from hostname for git safety.
func cleanHostname(h string) string {
	return strings.ReplaceAll(h, ".", "-")
}
