package cluster

import (
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"time"
)

// GitSync manages synchronization of the cluster membership registry via git.
// It pulls remote state on start, periodically polls for changes, and commits
// local heartbeats or membership updates back to the remote repository.
type GitSync struct {
	cfg      *Config
	logger   *slog.Logger
	localCfg *Config

	mu       sync.Mutex
	running  bool
	stopCh   chan struct{}
	doneCh   chan struct{}

	// runCtx is stored from Start so that git/hasStagedChanges/cloneRepo
	// can pass a cancellable context to exec.CommandContext, enabling
	// shutdown-time process termination (S6-8). Guarded by mu.
	runCtx    context.Context
	runCancel context.CancelFunc

	// Git repository path (local clone of the membership registry)
	gitRepoPath string
}

// NewGitSync creates a new git syncer.
func NewGitSync(cfg *Config, localCfg *Config, repoPath string, logger *slog.Logger) *GitSync {
	return &GitSync{
		cfg:         cfg,
		localCfg:    localCfg,
		logger:      logger,
		gitRepoPath: repoPath,
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
	}
}

// Start begins the git sync loop: initial pull, then periodic sync.
func (g *GitSync) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return fmt.Errorf("git sync already running")
	}
	g.running = true
	// Store a derived context so git/hasStagedChanges/cloneRepo can be
	// cancelled on parent cancellation (S6-8). We derive rather than
	// storing ctx directly to avoid lifetime issues.
	g.runCtx, g.runCancel = context.WithCancel(ctx)
	g.mu.Unlock()

	g.logger.Info("git_sync: starting", "repo", g.gitRepoPath)

	// Initial pull
	if err := g.pullRemote(); err != nil {
		g.logger.Warn("git_sync: initial pull failed", "error", err)
	}

	// Start sync loop
	go g.run(ctx)

	return nil
}

// Stop gracefully shuts down the git sync loop.
func (g *GitSync) Stop() error {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = false
	// Cancel any in-flight git subprocesses (S6-8).
	if g.runCancel != nil {
		g.runCancel()
	}
	g.mu.Unlock()

	close(g.stopCh)
	<-g.doneCh

	g.logger.Info("git_sync: stopped")
	return nil
}

// run is the main git sync loop.
func (g *GitSync) run(ctx context.Context) {
	defer close(g.doneCh)

	ticker := time.NewTicker(g.cfg.Git.SyncInterval)
	defer ticker.Stop()

	// Periodic heartbeat commit
	var hbTicker *time.Ticker
	if g.cfg.Git.HeartbeatCommit {
		hbTicker = time.NewTicker(30 * time.Second)
		defer hbTicker.Stop()
	}

	g.logger.Info("git_sync: loop started",
		"sync_interval", g.cfg.Git.SyncInterval,
		"heartbeat_commit", g.cfg.Git.HeartbeatCommit,
	)

	if hbTicker != nil {
		// Both sync and heartbeat tickers active
		for {
			select {
			case <-ctx.Done():
				return
			case <-g.stopCh:
				return
			case <-ticker.C:
				if err := g.pullRemote(); err != nil {
					g.logger.Warn("git_sync: sync failed", "error", err)
				}
			case <-hbTicker.C:
				if err := g.pushHeartbeat(); err != nil {
					g.logger.Warn("git_sync: heartbeat push failed", "error", err)
				}
			}
		}
	} else {
		// Only sync ticker active
		for {
			select {
			case <-ctx.Done():
				return
			case <-g.stopCh:
				return
			case <-ticker.C:
				if err := g.pullRemote(); err != nil {
					g.logger.Warn("git_sync: sync failed", "error", err)
				}
			}
		}
	}
}

// RegisterNode registers a new node in the cluster by saving its member
// record to the git directory and committing+pushing it.
func (g *GitSync) RegisterNode(member *Member) error {
	if member == nil {
		return fmt.Errorf("git_sync: register: member is nil")
	}
	if member.NodeID == "" {
		return fmt.Errorf("git_sync: register: node_id is required")
	}
	if member.Status == "" {
		member.Status = "active"
	}
	if member.LastHeartbeat.IsZero() {
		UpdateHeartbeat(member)
	}

	if err := SaveMember(g.gitRepoPath, member); err != nil {
		return fmt.Errorf("git_sync: register: save member: %w", err)
	}

	// Stage and commit
	if err := g.git("add", "."); err != nil {
		return fmt.Errorf("git_sync: register: git add: %w", err)
	}

	if err := g.git("commit", "-m", fmt.Sprintf("cluster: register node %s", member.NodeID)); err != nil {
		return fmt.Errorf("git_sync: register: git commit: %w", err)
	}

	if err := g.push(); err != nil {
		return fmt.Errorf("git_sync: register: git push: %w", err)
	}

	g.logger.Info("git_sync: node registered", "node_id", member.NodeID)
	return nil
}

// Leave removes the local node's member record from the cluster.
func (g *GitSync) Leave() error {
	if g.localCfg == nil || g.localCfg.NodeID == "" {
		return fmt.Errorf("git_sync: leave: no node identity configured")
	}

	if err := DeleteMember(g.gitRepoPath, g.localCfg.NodeID); err != nil {
		return fmt.Errorf("git_sync: leave: delete member: %w", err)
	}

	if err := g.git("add", "."); err != nil {
		return fmt.Errorf("git_sync: leave: git add: %w", err)
	}

	if err := g.git("commit", "-m", fmt.Sprintf("cluster: node %s leaving", g.localCfg.NodeID)); err != nil {
		return fmt.Errorf("git_sync: leave: git commit: %w", err)
	}

	if err := g.push(); err != nil {
		return fmt.Errorf("git_sync: leave: git push: %w", err)
	}

	g.logger.Info("git_sync: node left cluster", "node_id", g.localCfg.NodeID)
	return nil
}

// GetMembers returns all active members found in the local git repo.
// It does a pull first to ensure fresh state, then parses nodes/*.json5.
func (g *GitSync) GetMembers() (map[string]*Member, error) {
	if err := g.pullRemote(); err != nil {
		g.logger.Warn("git_sync: get_members: pull failed, returning cached", "error", err)
	}

	members, err := ListLocalMembers(g.gitRepoPath)
	if err != nil {
		return nil, fmt.Errorf("git_sync: get_members: list: %w", err)
	}

	// Filter to active members only
	active := make(map[string]*Member)
	for id, m := range members {
		if g.cfg.Gossip.PeerTimeout > 0 && m.IsActive(g.cfg.Gossip.PeerTimeout) {
			active[id] = m
		} else if g.cfg.Gossip.PeerTimeout == 0 && m.Status == "active" {
			active[id] = m
		}
	}

	return active, nil
}

// GetActiveMembers returns currently active cluster members.
// It implements the MembersProvider interface for the gossip transport.
func (g *GitSync) GetActiveMembers() (map[string]*Member, error) {
	return g.GetMembers()
}

// IsRunning returns whether the git sync loop is currently active.
func (g *GitSync) IsRunning() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.running
}

// GitRepoPath returns the local git repository path.
func (g *GitSync) GitRepoPath() string {
	return g.gitRepoPath
}

// pullRemote fetches the latest cluster state from the remote git repository
// using pull --rebase for linear history.
func (g *GitSync) pullRemote() error {
	g.logger.Debug("git_sync: pulling remote state")

	// Ensure the repo directory exists
	if _, err := os.Stat(g.gitRepoPath); os.IsNotExist(err) {
		if err := os.MkdirAll(g.gitRepoPath, 0o700); err != nil {
			return fmt.Errorf("pullRemote: mkdir %s: %w", g.gitRepoPath, err)
		}
	}

	// Check if it's a valid git repo
	if !g.isGitRepo() {
		// Try to clone if it fails and we haven't cloned yet
		g.logger.Debug("git_sync: not a git repo, will init locally")
		if err := g.initGit(); err != nil {
			return fmt.Errorf("pullRemote: init: %w", err)
		}
	}

	// git pull --rebase origin main
	if err := g.git("pull", "--rebase", "origin", "main"); err != nil {
		// If there's a rebase conflict, try to resolve it
		if err2 := g.handleRebaseConflict(); err2 != nil {
			return fmt.Errorf("pullRemote: pull --rebase failed: %w (rebase resolve: %v)", err, err2)
		}
	}

	return nil
}

// handleRebaseConflict attempts to resolve a rebase conflict by
// aborting the rebase and re-trying with merge instead.
func (g *GitSync) handleRebaseConflict() error {
	// Abort current rebase
	if abortErr := g.git("rebase", "--abort"); abortErr != nil {
		g.logger.Debug("git_sync: rebase --abort returned error", "error", abortErr)
	}
	g.logger.Debug("git_sync: aborted rebase due to conflict")

	// Fetch latest
	if err := g.git("fetch", "origin"); err != nil {
		return fmt.Errorf("handleRebaseConflict: fetch: %w", err)
	}

	// Try merge instead
	if err := g.git("merge", "origin/main"); err != nil {
		// Merge also conflicts -- abort and accept worst case
		if abortErr := g.git("merge", "--abort"); abortErr != nil {
			g.logger.Debug("git_sync: merge --abort returned error", "error", abortErr)
		}
		g.logger.Warn("git_sync: merge also conflicted, keeping local state")
		return fmt.Errorf("handleRebaseConflict: merge conflicted, keeping local state")
	}

	g.logger.Debug("git_sync: resolved rebase conflict via merge")
	return nil
}

// pushHeartbeat updates the heartbeat timestamp for the current node
// and pushes the change to the remote.
func (g *GitSync) pushHeartbeat() error {
	if g.localCfg == nil || g.localCfg.NodeID == "" {
		g.logger.Debug("git_sync: heartbeat: no node identity, skipping")
		return nil
	}

	members, err := ListLocalMembers(g.gitRepoPath)
	if err != nil {
		return fmt.Errorf("pushHeartbeat: list members: %w", err)
	}

	member, ok := members[g.localCfg.NodeID]
	if !ok {
		g.logger.Debug("git_sync: heartbeat: node not registered, skipping")
		return nil
	}

	UpdateHeartbeat(member)

	if err := SaveMember(g.gitRepoPath, member); err != nil {
		return fmt.Errorf("pushHeartbeat: save member: %w", err)
	}

	// Stage, check for changes, commit if any
	if err := g.git("add", "."); err != nil {
		return fmt.Errorf("pushHeartbeat: git add: %w", err)
	}

	hasChanges, err := g.hasStagedChanges()
	if err != nil {
		return fmt.Errorf("pushHeartbeat: check changes: %w", err)
	}
	if !hasChanges {
		return nil
	}

	if err := g.git("commit", "-m", fmt.Sprintf("cluster: heartbeat %s", g.localCfg.NodeID)); err != nil {
		return fmt.Errorf("pushHeartbeat: git commit: %w", err)
	}

	if err := g.push(); err != nil {
		return fmt.Errorf("pushHeartbeat: push: %w", err)
	}

	g.logger.Debug("git_sync: heartbeat committed", "node_id", g.localCfg.NodeID)
	return nil
}

// push commits any remaining staged changes and pushes to origin.
func (g *GitSync) push() error {
	hasChanges, err := g.hasStagedChanges()
	if err != nil || !hasChanges {
		return nil
	}

	// Commit remaining changes
	if err := g.git("commit", "-m", "cluster: git sync"); err != nil {
		return fmt.Errorf("push: commit: %w", err)
	}

	if err := g.git("push", "origin", "main"); err != nil {
		return fmt.Errorf("push: %w", err)
	}

	return nil
}

// isGitRepo checks whether the repo path is a valid git repository.
func (g *GitSync) isGitRepo() bool {
	err := g.git("rev-parse", "--is-inside-work-tree")
	return err == nil
}

// initGit initializes a new git repository in the local directory
// and wires up the origin remote from config.
func (g *GitSync) initGit() error {
	if err := g.git("init"); err != nil {
		return fmt.Errorf("initGit: %w", err)
	}

	// Add origin remote
	remoteURL := ""
	if g.cfg != nil {
		remoteURL = g.cfg.Git.RemoteURL
	}
	if g.localCfg != nil && remoteURL == "" {
		remoteURL = g.localCfg.Git.RemoteURL
	}
	if remoteURL != "" {
		if err := g.git("remote", "add", "origin", remoteURL); err != nil {
			return fmt.Errorf("initGit: add remote: %w", err)
		}
		g.logger.Debug("git_sync: added origin remote", "url", remoteURL)
	}

	return nil
}

// gitCtx returns the run context stored from Start, or
// context.Background() if Start hasn't been called yet (S6-8).
func (g *GitSync) gitCtx() context.Context {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.runCtx != nil {
		return g.runCtx
	}
	return context.Background()
}

// hasStagedChanges checks if there are staged (or uncommitted) changes.
func (g *GitSync) hasStagedChanges() (bool, error) {
	cmd := exec.CommandContext(g.gitCtx(), "git", "status", "--porcelain")
	cmd.Dir = g.gitRepoPath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("hasStagedChanges: git status: %w", err)
	}
	return len(output) > 0, nil
}

// git runs a git command in the repository directory.
func (g *GitSync) git(args ...string) error {
	cmd := exec.CommandContext(g.gitCtx(), "git", args...)
	cmd.Dir = g.gitRepoPath
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		// Exit code 1 from "git commit" with no changes is non-fatal
		if len(args) >= 2 && args[0] == "commit" &&
			(bytes.Contains(output, []byte("nothing to commit")) ||
				bytes.Contains(output, []byte("no changes committed"))) {
			return nil
		}
		return fmt.Errorf("git %s: %w: %s", args, err, string(output))
	}
	return nil
}

// cloneRepo clones the remote repository to the local path.
func (g *GitSync) cloneRepo(remoteURL string) error {
	parentDir := filepath.Dir(g.gitRepoPath)
	if err := os.MkdirAll(parentDir, 0o755); err != nil {
		return fmt.Errorf("cloneRepo: mkdir: %w", err)
	}

	// Remove existing directory if present
	if _, err := os.Stat(g.gitRepoPath); err == nil {
		if err := os.RemoveAll(g.gitRepoPath); err != nil {
			return fmt.Errorf("cloneRepo: remove existing: %w", err)
		}
	}

	cmd := exec.CommandContext(g.gitCtx(), "git", "clone", remoteURL, g.gitRepoPath)
	cmd.Dir = parentDir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("cloneRepo: %w: %s", err, string(output))
	}

	return nil
}
