package cluster

import (
	"context"
	"crypto/ed25519"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
)

// Engine is the main cluster engine that coordinates all cluster components.
// It implements the clusterEngine interface for RPC handling.
type Engine struct {
	cfg        *Config
	localCfg   *Config
	logger     *slog.Logger
	msgBus     *bus.MessageBus
	gitRepoPath string

	mu          sync.RWMutex
	running     bool
	nodeID      string
	nodeName    string
	clusterID   string
	clusterName string
	joinKey     string

	// Components
	gossip *GossipEngine
	gitSync *GitSync
	wgMgr  *WireGuardManager

	// Keys
	signingPub  ed25519.PublicKey
	signingPriv ed25519.PrivateKey
	wgPub       string
	wgPriv      string
}

// EngineConfig holds configuration for creating a cluster engine.
type EngineConfig struct {
	Config      *Config
	LocalConfig *Config
	Logger      *slog.Logger
	MessageBus  *bus.MessageBus
	GitRepoPath string
}

// NewEngine creates a new cluster engine.
func NewEngine(cfg EngineConfig) (*Engine, error) {
	engine := &Engine{
		cfg:         cfg.Config,
		localCfg:    cfg.LocalConfig,
		logger:      cfg.Logger,
		msgBus:      cfg.MessageBus,
		gitRepoPath: cfg.GitRepoPath,
	}

	return engine, nil
}

// Init creates a new cluster and returns the join key.
func (e *Engine) Init(clusterName, nodeName, nodeID string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return "", fmt.Errorf("cluster engine already running")
	}

	// Generate signing keys
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return "", fmt.Errorf("failed to generate signing keys: %w", err)
	}
	e.signingPub = pub
	e.signingPriv = priv

	// Generate WireGuard keys
	wgPrivKey, wgPubKey, err := generateWireGuardKeys()
	if err != nil {
		return "", fmt.Errorf("failed to generate WireGuard keys: %w", err)
	}
	e.wgPriv = wgPrivKey
	e.wgPub = wgPubKey

	// Set cluster identity
	e.clusterName = clusterName
	e.nodeName = nodeName
	e.nodeID = nodeID
	e.clusterID = generateClusterID()

	// Generate join key (simple hex token for now)
	e.joinKey = hex.EncodeToString(generateJoinKey())

	// Create member record for this node
	member := &Member{
		NodeID:        nodeID,
		NodeName:      nodeName,
		WireGuardPub:  wgPubKey,
		SigningPub:    pub,
		ClusterIP:     "10.200.0.1", // First node gets .1
		JoinedAt:      time.Now().UTC(),
		LastHeartbeat: time.Now().UTC(),
		Status:        "active",
	}

	// Save member to git repo
	if err := SaveMember(e.gitRepoPath, member); err != nil {
		return "", fmt.Errorf("failed to save member: %w", err)
	}

	e.logger.Info("cluster: initialized",
		"cluster_id", e.clusterID,
		"cluster_name", e.clusterName,
		"node_id", e.nodeID,
		"join_key", e.joinKey,
	)

	return e.joinKey, nil
}

// Join joins an existing cluster with the given join key.
func (e *Engine) Join(joinKey string) (map[string]any, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	// For now, accept any join key (real implementation would validate)
	if joinKey == "" {
		return nil, fmt.Errorf("join key required")
	}

	// Generate keys for new node
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to generate signing keys: %w", err)
	}
	e.signingPub = pub
	e.signingPriv = priv

	wgPrivKey, wgPubKey, err := generateWireGuardKeys()
	if err != nil {
		return nil, fmt.Errorf("failed to generate WireGuard keys: %w", err)
	}
	e.wgPriv = wgPrivKey
	e.wgPub = wgPubKey

	e.joinKey = joinKey
	e.nodeID = generateNodeID()
	e.clusterName = "joined-cluster"
	e.nodeName = "node-" + e.nodeID[:8]

	result := map[string]any{
		"status":           "joined",
		"node_id":          e.nodeID,
		"node_name":        e.nodeName,
		"cluster_name":     e.clusterName,
		"signing_pubkey":   hex.EncodeToString(pub),
		"wireguard_pubkey": wgPubKey,
		"cluster_ip":       "10.200.0.2", // Second node gets .2
	}

	e.logger.Info("cluster: joined",
		"node_id", e.nodeID,
		"join_key", joinKey,
	)

	return result, nil
}

// Start initializes the cluster coordination protocol.
func (e *Engine) Start() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("cluster engine already running")
	}

	ctx := context.Background()

	// Start git sync if configured
	if e.gitRepoPath != "" && e.cfg != nil {
		e.gitSync = NewGitSync(e.cfg, e.localCfg, e.gitRepoPath, e.logger)
		if err := e.gitSync.Start(ctx); err != nil {
			e.logger.Warn("cluster: git sync start failed", "error", err)
		}
	}

	// Start gossip engine if we have a message bus
	if e.msgBus != nil && e.cfg != nil {
		e.gossip = NewGossipEngine(e.cfg, e.nodeID, e.msgBus, e.logger)
		if err := e.gossip.Start(ctx); err != nil {
			e.logger.Warn("cluster: gossip engine start failed", "error", err)
		}
	}

	e.running = true

	e.logger.Info("cluster: started",
		"node_id", e.nodeID,
		"gossip_ok", e.gossip != nil,
		"git_sync_ok", e.gitSync != nil,
	)

	return nil
}

// Status returns the current cluster state.
func (e *Engine) Status() (map[string]any, error) {
	e.mu.RLock()
	defer e.mu.RUnlock()

	status := map[string]any{
		"cluster_id":      e.clusterID,
		"cluster_name":    e.clusterName,
		"node_id":         e.nodeID,
		"node_name":       e.nodeName,
		"running":         e.running,
		"join_key_set":    e.joinKey != "",
		"git_repo_path":   e.gitRepoPath,
	}

	// Add member info if we have git sync
	if e.gitSync != nil {
		members, err := e.gitSync.GetMembers()
		if err != nil {
			status["members_error"] = err.Error()
		} else {
			status["member_count"] = len(members)
			status["members"] = members
		}
	}

	// Add gossip info if we have gossip engine
	if e.gossip != nil {
		peers := e.gossip.Peers()
		status["peer_count"] = len(peers)
		status["peers"] = peers
	}

	return status, nil
}

// Leave gracefully departs the cluster.
func (e *Engine) Leave(force bool) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	// Stop gossip engine
	if e.gossip != nil {
		if err := e.gossip.Stop(); err != nil {
			e.logger.Warn("cluster: gossip stop failed", "error", err)
		}
		e.gossip = nil
	}

	// Stop git sync
	if e.gitSync != nil {
		if !force {
			if err := e.gitSync.Leave(); err != nil {
				e.logger.Warn("cluster: git sync leave failed", "error", err)
			}
		}
		if err := e.gitSync.Stop(); err != nil {
			e.logger.Warn("cluster: git sync stop failed", "error", err)
		}
		e.gitSync = nil
	}

	e.running = false

	e.logger.Info("cluster: stopped",
		"node_id", e.nodeID,
		"force", force,
	)

	return nil
}

// NodeID returns the local node ID.
func (e *Engine) NodeID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.nodeID
}

// Config returns the cluster configuration.
func (e *Engine) Config() *Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// LocalConfig returns the local cluster configuration.
func (e *Engine) LocalConfig() *Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.localCfg
}

// GitSync returns the git sync component.
func (e *Engine) GitSync() *GitSync {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.gitSync
}

// Gossip returns the gossip engine component.
func (e *Engine) Gossip() *GossipEngine {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.gossip
}

// WireGuardKeys returns the WireGuard key pair.
func (e *Engine) WireGuardKeys() (privateKey, publicKey string) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.wgPriv, e.wgPub
}

// SigningKeys returns the ed25519 key pair.
func (e *Engine) SigningKeys() (ed25519.PrivateKey, ed25519.PublicKey) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.signingPriv, e.signingPub
}

// generateClusterID creates a unique cluster ID.
func generateClusterID() string {
	return fmt.Sprintf("cluster-%s", hex.EncodeToString(generateRandomBytes(8)))
}

// generateNodeID creates a unique node ID.
func generateNodeID() string {
	return hex.EncodeToString(generateRandomBytes(8))
}

// generateJoinKey creates a join key for cluster invitation.
func generateJoinKey() []byte {
	return generateRandomBytes(16)
}

// generateRandomBytes generates n random bytes.
func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(i) // Simple placeholder - use crypto/rand in production
	}
	return b
}

// generateWireGuardKeys generates a WireGuard key pair.
// Returns private key, public key, error.
func generateWireGuardKeys() (privateKey, publicKey string, err error) {
	// Placeholder - real implementation uses libwgctrl or wg binary
	// For now, return dummy keys
	privateKey = "GENERATE_WITH_WG_BINARY"
	publicKey = "PLACEHOLDER_KEY"
	return privateKey, publicKey, nil
}

// LoadKeysFromDir loads signing and WireGuard keys from the given directory.
func LoadKeysFromDir(dir string) (signingPriv ed25519.PrivateKey, signingPub ed25519.PublicKey, wgPriv, wgPub string, err error) {
	// Load signing key
	signingKeyPath := filepath.Join(dir, "node_private_key")
	signingData, err := os.ReadFile(signingKeyPath)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("failed to read signing key: %w", err)
	}

	signingPriv = ed25519.PrivateKey(signingData)
	signingPub = signingPriv.Public().(ed25519.PublicKey)

	// Load WireGuard key
	wgKeyPath := filepath.Join(dir, "wireguard_private_key")
	wgData, err := os.ReadFile(wgKeyPath)
	if err != nil {
		return signingPriv, signingPub, "", "", fmt.Errorf("failed to read WireGuard key: %w", err)
	}

	wgPriv = string(wgData)

	// Derive public key from private (placeholder)
	wgPub = "derived_from_private"

	return signingPriv, signingPub, wgPriv, wgPub, nil
}
