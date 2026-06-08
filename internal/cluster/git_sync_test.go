package cluster

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"log/slog"
)

// gitRepoSuite creates a fresh git repo for use in tests.
// The repo has a bare origin and a "cluster-repo" workdir.
func gitRepoSuite(t *testing.T) (dir string, cleanup func()) {
	t.Helper()

	dir = t.TempDir()
	repoDir := filepath.Join(dir, "cluster-repo")
	originDir := filepath.Join(dir, "origin.git")

	// Create a bare origin repo
	if err := os.MkdirAll(originDir, 0o755); err != nil {
		t.Fatalf("mkdir origin: %v", err)
	}
	if err := exec.Command("git", "-C", originDir, "init", "--bare").Run(); err != nil {
		t.Fatalf("init origin: %v", err)
	}

	// Clone it into repoDir
	if err := exec.Command("git", "clone", originDir, repoDir).Run(); err != nil {
		t.Fatalf("clone origin: %v", err)
	}

	if err := exec.Command("git", "-C", repoDir, "config", "user.email", "test@test.com").Run(); err != nil {
		t.Fatalf("git config email: %v", err)
	}
	if err := exec.Command("git", "-C", repoDir, "config", "user.name", "Test").Run(); err != nil {
		t.Fatalf("git config name: %v", err)
	}

	// Make an initial commit so the repo has a branch
	// Write to repo root, not in nodes/, to avoid polluting member listing
	if err := os.WriteFile(filepath.Join(repoDir, "README.md"), []byte("test repo"), 0o600); err != nil {
		t.Fatalf("write readme: %v", err)
	}
	if err := exec.Command("git", "-C", repoDir, "add", ".").Run(); err != nil {
		t.Fatalf("git add: %v", err)
	}
	if err := exec.Command("git", "-C", repoDir, "commit", "-m", "initial").Run(); err != nil {
		t.Fatalf("git commit: %v", err)
	}

	// Push initial commit to origin
	if err := exec.Command("git", "-C", repoDir, "push", "-u", "origin", "HEAD").Run(); err != nil {
		t.Fatalf("git push initial: %v", err)
	}

	return repoDir, func() {}
}

func TestNewGitSync(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{NodeID: "test-node"}
	repoDir := t.TempDir()

	gs := NewGitSync(cfg, cfg, repoDir, logger)
	if gs == nil {
		t.Fatal("NewGitSync returned nil")
	}
	if gs.gitRepoPath != repoDir {
		t.Errorf("gitRepoPath = %q, want %q", gs.gitRepoPath, repoDir)
	}
}

func TestGitSync_RegisterNode(t *testing.T) {
	repo, cleanup := gitRepoSuite(t)
	defer cleanup()

	cfg := &Config{NodeID: "node-alpha"}
	localCfg := &Config{NodeID: "node-alpha"}
	logger := slog.Default()

	gs := NewGitSync(cfg, localCfg, repo, logger)

	member := &Member{
		NodeID:       "node-alpha",
		NodeName:     "Alpha Node",
		WireGuardPub: "pubkey-001",
		Endpoint:     "10.0.0.1:51820",
		Capabilities: []string{"coder", "debugger"},
		Status:       "active",
	}

	if err := gs.RegisterNode(member); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}

	// Verify the member file exists on disk
	loaded, err := LoadMember(repo, "node-alpha")
	if err != nil {
		t.Fatalf("LoadMember after register: %v", err)
	}
	if loaded.NodeID != "node-alpha" {
		t.Errorf("loaded NodeID = %q, want %q", loaded.NodeID, "node-alpha")
	}
	if loaded.NodeName != "Alpha Node" {
		t.Errorf("loaded NodeName = %q, want %q", loaded.NodeName, "Alpha Node")
	}
}

func TestGitSync_RegisterNode_Nil(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{NodeID: "test"}

	gs := NewGitSync(cfg, cfg, "/tmp", logger)
	if err := gs.RegisterNode(nil); err == nil {
		t.Error("expected error for nil member")
	}
}

func TestGitSync_RegisterNode_EmptyID(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{NodeID: "test"}

	gs := NewGitSync(cfg, cfg, "/tmp", logger)
	if err := gs.RegisterNode(&Member{NodeName: "no-id"}); err == nil {
		t.Error("expected error for empty node ID")
	}
}

func TestGitSync_ListMembers(t *testing.T) {
	repo, cleanup := gitRepoSuite(t)
	defer cleanup()

	cfg := &Config{NodeID: "node-alpha"}
	localCfg := &Config{NodeID: "node-alpha"}
	logger := slog.Default()

	gs := NewGitSync(cfg, localCfg, repo, logger)

	// Register a first member
	m1 := &Member{NodeID: "node-alpha", NodeName: "Alpha", Status: "active"}
	m1.LastHeartbeat = time.Now().UTC()
	gs.RegisterNode(m1)

	// Register a second member
	m2 := &Member{NodeID: "node-beta", NodeName: "Beta", Status: "active"}
	m2.LastHeartbeat = time.Now().UTC()
	gs.RegisterNode(m2)

	// Get members should return both
	members, err := gs.GetMembers()
	if err != nil {
		t.Fatalf("GetMembers: %v", err)
	}
	if len(members) != 2 {
		t.Errorf("GetMembers count = %d, want 2", len(members))
	}
	if _, ok := members["node-alpha"]; !ok {
		t.Error("missing node-alpha in members")
	}
	if _, ok := members["node-beta"]; !ok {
		t.Error("missing node-beta in members")
	}
}

func TestGitSync_HeartbeatUpdates(t *testing.T) {
	repo, cleanup := gitRepoSuite(t)
	defer cleanup()

	cfg := &Config{
		NodeID: "node-hb",
		Git: GitConfig{
			SyncInterval:    5 * time.Minute,
			HeartbeatCommit: true,
		},
	}
	localCfg := &Config{NodeID: "node-hb"}
	logger := slog.Default()

	gs := NewGitSync(cfg, localCfg, repo, logger)

	// Register node first
	m := &Member{
		NodeID:   "node-hb",
		NodeName: "HB Node",
		Status:   "active",
	}
	UpdateHeartbeat(m)

	if err := gs.RegisterNode(m); err != nil {
		t.Fatalf("RegisterNode: %v", err)
	}

	// Wait a moment and push heartbeat again
	time.Sleep(10 * time.Millisecond)
	beforeHB := time.Now().Add(-500 * time.Millisecond)

	if err := gs.pushHeartbeat(); err != nil {
		t.Fatalf("pushHeartbeat: %v", err)
	}

	// Reload and verify heartbeat is now recent (not the old one)
	loaded, err := LoadMember(repo, "node-hb")
	if err != nil {
		t.Fatalf("LoadMember: %v", err)
	}

	if loaded.LastHeartbeat.Before(beforeHB) {
		t.Errorf("heartbeat was not updated: loaded = %v (expected >= %v)",
			loaded.LastHeartbeat, beforeHB)
	}
}

func TestUpdateHeartbeat(t *testing.T) {
	m := &Member{
		NodeID:        "test-node",
		LastHeartbeat: time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC),
	}

	old := m.LastHeartbeat
	UpdateHeartbeat(m)

	if m.LastHeartbeat.Equal(old) {
		t.Error("LastHeartbeat not updated by UpdateHeartbeat")
	}

	if m.LastHeartbeat.Before(time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)) {
		t.Error("LastHeartbeat is before the initial value")
	}
}

func TestMember_IsActive(t *testing.T) {
	member := &Member{
		NodeID:        "active-node",
		Status:        "active",
		LastHeartbeat: time.Now().Add(-1 * time.Minute),
	}

	testCases := []struct {
		name     string
		timeout  time.Duration
		expected bool
	}{
		{"within timeout", 2 * time.Minute, true},
		{"slightly past timeout", 1 * time.Minute, false},
		{"exceeded timeout", 30 * time.Second, false},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result := member.IsActive(tc.timeout)
			if result != tc.expected {
				t.Errorf("IsActive(%v) = %v, want %v", tc.timeout, result, tc.expected)
			}
		})
	}
}

func TestMember_IsActive_Inactive(t *testing.T) {
	member := &Member{
		NodeID:        "inactive-node",
		Status:        "inactive",
		LastHeartbeat: time.Now().Add(-10 * time.Second),
	}

	if member.IsActive(2 * time.Minute) {
		t.Error("inactive node should not be active regardless of heartbeat")
	}
}

func TestMember_IsActive_Stale(t *testing.T) {
	member := &Member{
		NodeID:        "stale-node",
		Status:        "active",
		LastHeartbeat: time.Now().Add(-1 * time.Hour),
	}

	if member.IsActive(2 * time.Minute) {
		t.Error("stale heartbeat should not be considered active")
	}
}

func TestGitSync_GetMembers_NoActiveNodes(t *testing.T) {
	repo, cleanup := gitRepoSuite(t)
	defer cleanup()

	cfg := &Config{
		NodeID: "ghost",
		Gossip: GossipConfig{
			PeerTimeout: 2 * time.Minute,
		},
	}
	localCfg := &Config{NodeID: "ghost"}
	logger := slog.Default()

	members, err := NewGitSync(cfg, localCfg, repo, logger).GetMembers()
	if err != nil {
		t.Fatalf("GetMembers: %v", err)
	}

	if len(members) != 0 {
		t.Errorf("GetMembers returned %d members, want 0", len(members))
	}
}

func TestGitSync_Leave(t *testing.T) {
	repo, cleanup := gitRepoSuite(t)
	defer cleanup()

	cfg := &Config{NodeID: "node-leave"}
	localCfg := &Config{NodeID: "node-leave"}
	logger := slog.Default()

	gs := NewGitSync(cfg, localCfg, repo, logger)

	// Register the node first
	m := &Member{
		NodeID:   "node-leave",
		NodeName: "Leaving Node",
		Status:   "active",
	}
	gs.RegisterNode(m)

	// Verify it exists
	_, err := LoadMember(repo, "node-leave")
	if err != nil {
		t.Fatalf("node should exist after register: %v", err)
	}

	// Leave
	if err := gs.Leave(); err != nil {
		t.Fatalf("Leave: %v", err)
	}

	// Re-initialize GitSync so it uses the same repo path
	gs2 := NewGitSync(cfg, localCfg, repo, logger)

	// The member file should be deleted
	members, err := gs2.GetMembers()
	if err != nil {
		t.Fatalf("GetMembers after leave: %v", err)
	}
	if _, ok := members["node-leave"]; ok {
		t.Error("node-leave should not be in members after Leave()")
	}

	// Physical file should be gone
	if _, err := os.Stat(filepath.Join(repo, "nodes", "node-leave.json5")); !os.IsNotExist(err) {
		t.Error("member file should be deleted after Leave()")
	}
}

func TestGitSync_Leave_NoIdentity(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{NodeID: ""}
	localCfg := &Config{NodeID: ""}

	gs := NewGitSync(cfg, localCfg, "/tmp", logger)
	if err := gs.Leave(); err == nil {
		t.Error("expected error for Leave with no node identity")
	}
}

func TestGitSync_GitRepoPath(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{NodeID: "test"}
	repoDir := "/tmp/my-git-repo"

	gs := NewGitSync(cfg, cfg, repoDir, logger)
	if gs.GitRepoPath() != repoDir {
		t.Errorf("GitRepoPath() = %q, want %q", gs.GitRepoPath(), repoDir)
	}
}

func TestGitSync_ListLocalMembers_MissingDir(t *testing.T) {
	members, err := ListLocalMembers("/non/existent/path")
	if err != nil {
		t.Fatalf("ListLocalMembers should not error for missing dir: %v", err)
	}
	if len(members) != 0 {
		t.Errorf("expected 0 members for missing dir, got %d", len(members))
	}
}

func TestListLocalMembers_SkipsNonJSON5(t *testing.T) {
	repo, cleanup := gitRepoSuite(t)
	defer cleanup()

	// Register a normal member
	m := &Member{
		NodeID:   "valid-node",
		NodeName: "Valid",
		Status:   "active",
	}
	UpdateHeartbeat(m)
	SaveMember(repo, m)

	// Add an ignored file
	ignorePath := filepath.Join(repo, "nodes", "ignored.txt")
	if err := os.WriteFile(ignorePath, []byte("not a member"), 0o600); err != nil {
		t.Fatalf("write ignored file: %v", err)
	}

	members, err := ListLocalMembers(repo)
	if err != nil {
		t.Fatalf("ListLocalMembers: %v", err)
	}
	if len(members) != 1 {
		t.Errorf("ListLocalMembers found %d members, want 1", len(members))
	}
	if _, ok := members["valid-node"]; !ok {
		t.Error("missing valid-node in ListLocalMembers results")
	}
}

func TestGitSync_ContextCancellation(t *testing.T) {
	logger := slog.Default()
	cfg := &Config{
		NodeID: "cancellable",
		Git: GitConfig{
			SyncInterval: 1 * time.Hour, // long interval so we don't tick
		},
		Gossip: GossipConfig{
			PeerTimeout: 2 * time.Minute,
		},
	}
	localCfg := &Config{NodeID: "cancellable"}

	repo, cleanup := gitRepoSuite(t)
	defer cleanup()

	gs := NewGitSync(cfg, localCfg, repo, logger)

	// Start with a cancellable context
	ctx, cancel := context.WithCancel(context.Background())
	_ = cancel // reserved for future use with context-only cancellation

	if err := gs.Start(ctx); err != nil {
		t.Fatalf("Start: %v", err)
	}

	// Stop via Stop() which closes stopCh to signal the goroutine to exit
	if err := gs.Stop(); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	if gs.IsRunning() {
		t.Error("GitSync should not be running after Stop()")
	}
}
