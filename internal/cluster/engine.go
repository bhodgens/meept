package cluster

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
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

	// Load existing WireGuard keys from disk, or generate new ones.
	if err := engine.loadOrGenerateKeys(); err != nil {
		engine.logger.Warn("cluster: failed to load existing keys, will generate fresh ones on Init", "error", err)
	}

	return engine, nil
}

// keyDir returns the directory where cluster keys are stored.
// Defaults to ~/.meept/cluster.
func (e *Engine) keyDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".meept", "cluster")
}

// loadOrGenerateKeys loads existing WireGuard keys from disk, or generates fresh ones.
// If keys exist on disk they are reused so the node identity is preserved across restarts.
func (e *Engine) loadOrGenerateKeys() error {
	keyDir := e.keyDir()
	if keyDir == "" {
		return fmt.Errorf("cannot determine key storage directory")
	}

	privPath := filepath.Join(keyDir, "wireguard_private_key")
	pubPath := filepath.Join(keyDir, "wireguard_public_key")

	// Try to load from disk first
	privData, err := os.ReadFile(privPath)
	if err == nil {
		privKey := strings.TrimSpace(string(privData))
		if pubData, err := os.ReadFile(pubPath); err == nil {
			pubKey := strings.TrimSpace(string(pubData))
			// Validate that this is a proper base64 WireGuard key (44 chars, ends with ==)
			if len(privKey) == 44 && strings.HasSuffix(privKey, "==") &&
				len(pubKey) == 44 && strings.HasSuffix(pubKey, "==") {
				e.wgPriv = privKey
				e.wgPub = pubKey
				e.logger.Info("cluster: loaded WireGuard keys from disk", "path", keyDir)
				return nil
			}
		}
	}

	wgPrivKey, wgPubKey, err := generateWireGuardKeys()
	if err != nil {
		return fmt.Errorf("failed to generate WireGuard keys: %w", err)
	}
	e.wgPriv = wgPrivKey
	e.wgPub = wgPubKey

	// Persist keys to disk
	if err := os.MkdirAll(keyDir, 0o700); err != nil {
		return fmt.Errorf("failed to create key directory %s: %w", keyDir, err)
	}
	if err := os.WriteFile(privPath, []byte(wgPrivKey+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write private key: %w", err)
	}
	if err := os.WriteFile(pubPath, []byte(wgPubKey+"\n"), 0o600); err != nil {
		return fmt.Errorf("failed to write public key: %w", err)
	}
	e.logger.Info("cluster: generated and saved new WireGuard keys", "directory", keyDir)

	return nil
}

// Init creates a new cluster and returns the join key.
func (e *Engine) Init(clusterName, nodeName, nodeID string) (string, error) {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return "", fmt.Errorf("cluster engine already running")
	}

	// Generate signing keys
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return "", fmt.Errorf("failed to generate signing keys: %w", err)
	}
	e.signingPub = pub
	e.signingPriv = priv

	// WireGuard keys should already be loaded by NewEngine -> loadOrGenerateKeys().
	// Only generate fresh ones if they are empty (e.g. if loadOrGenerateKeys failed).
	if e.wgPriv == "" {
		wgPrivKey, wgPubKey, err := generateWireGuardKeys()
		if err != nil {
			return "", fmt.Errorf("failed to generate WireGuard keys: %w", err)
		}
		e.wgPriv = wgPrivKey
		e.wgPub = wgPubKey
	}

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
		WireGuardPub:  e.wgPub,
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

	// Generate signing keys for new node
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("failed to generate signing keys: %w", err)
	}
	e.signingPub = pub
	e.signingPriv = priv

	// WireGuard keys should already be loaded by NewEngine.
	// Only generate fresh ones if they are empty.
	if e.wgPriv == "" {
		wgPrivKey, wgPubKey, err := generateWireGuardKeys()
		if err != nil {
			return nil, fmt.Errorf("failed to generate WireGuard keys: %w", err)
		}
		e.wgPriv = wgPrivKey
		e.wgPub = wgPubKey
	}

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
		"wireguard_pubkey": e.wgPub,
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

	// WireGuard manager is started after git sync

	// Stop gossip engine if we have a message bus
	if e.msgBus != nil && e.cfg != nil {
		e.gossip = NewGossipEngine(e.cfg, e.nodeID, e.msgBus, e.logger)
		// Wire signing key and peers' public keys
		if e.signingPriv != nil {
			e.gossip.WithSigningKey(e.signingPriv)
			// Register this node's public key so events from us can be verified
			e.gossip.SetPeerSigningKey(e.nodeID, e.signingPub)
		}
		if err := e.gossip.Start(ctx); err != nil {
			e.logger.Warn("cluster: gossip engine start failed", "error", err)
		}
	}

	// Wire and start WireGuard manager if we have keys and config
	if e.cfg != nil && e.wgPriv != "" && e.wgPub != "" {
		wgIface := e.cfg.Network.Interface
		if wgIface == "" {
			wgIface = "wg0"
		}
		keyDir := e.keyDir()
		wgConfPath := filepath.Join(keyDir, "wg0.conf")

		wgMgr, err := NewWireGuardManager(wgConfPath, wgIface)
		if err != nil {
			e.logger.Warn("cluster: WireGuard manager creation failed", "error", err)
		} else {
			e.wgMgr = wgMgr

			// Build WireGuard config with current peers
			members, _ := e.gitSync.GetMembers()
			var peers []Member
			for _, m := range members {
				if m.NodeID != e.nodeID {
					peers = append(peers, *m)
				}
			}

			wgCfg := &WireGuardConfig{
				PrivateKey:            e.wgPriv,
				ClusterIP:             "10.200.0.1",
				ListenPort:            e.cfg.Network.WireGuardPort,
				DNS:                   "8.8.8.8",
				PersistentKeepalive:   "25",
				Peers:                 peers,
			}

			if err := wgMgr.WriteConfig(wgCfg); err != nil {
				e.logger.Warn("cluster: WireGuard config write failed", "error", err)
			} else {
				e.logger.Info("cluster: WireGuard config written", "path", wgConfPath)
			}
		}
	}

	e.running = true

	e.logger.Info("cluster: started",
		"node_id", e.nodeID,
		"gossip_ok", e.gossip != nil,
		"git_sync_ok", e.gitSync != nil,
		"wireguard_ok", e.wgMgr != nil,
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

	// Add WireGuard info
	if e.wgMgr != nil {
		status["wireguard_iface"] = e.wgMgr.iface
		status["wireguard_key_set"] = true
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

// WireGuardManager returns the WireGuard manager (may be nil if not wired).
func (e *Engine) WireGuardManager() *WireGuardManager {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.wgMgr
}
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

// generateRandomBytes generates n cryptographically random bytes.
func generateRandomBytes(n int) []byte {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(fmt.Sprintf("crypto/rand failed: %v", err))
	}
	return b
}

// generateWireGuardKeys generates a WireGuard key pair by shelling out to
// `wg genkey` and piping the result into `wg pubkey`. This uses the system
// WireGuard toolkit which implements correct Curve25519 derivation.
func generateWireGuardKeys() (privateKey, publicKey string, err error) {
	// Generate private key via `wg genkey`
	cmd := exec.Command("wg", "genkey")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	privPipe, err := cmd.StdoutPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stdout pipe: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", "", fmt.Errorf("failed to run wg genkey: %w", err)
	}

	privBytes, err := io.ReadAll(io.LimitReader(privPipe, 64))
	privPipe.Close()
	if err != nil {
		cmd.Wait()
		return "", "", fmt.Errorf("failed to read wg genkey output: %w", err)
	}

	if waitErr := cmd.Wait(); waitErr != nil {
		return "", "", fmt.Errorf("wg genkey failed: %w%s", waitErr, stderr.String())
	}

	privKey := strings.TrimSpace(string(privBytes))

	// Derive public key via `wg pubkey`, piping the private key on stdin
	pubCmd := exec.Command("wg", "pubkey")
	pubStdin, err := pubCmd.StdinPipe()
	if err != nil {
		return "", "", fmt.Errorf("failed to create stdin pipe for pubkey: %w", err)
	}
	pubOut, err := pubCmd.StdoutPipe()
	if err != nil {
		pubStdin.Close()
		return "", "", fmt.Errorf("failed to create stdout pipe for pubkey: %w", err)
	}
	pubCmd.Stderr = &stderr

	if err := pubCmd.Start(); err != nil {
		pubStdin.Close()
		return "", "", fmt.Errorf("failed to run wg pubkey: %w", err)
	}

	if _, err := pubStdin.Write([]byte(privKey + "\n")); err != nil {
		pubStdin.Close()
		return "", "", fmt.Errorf("failed to write private key to pubkey pipe: %w", err)
	}
	pubStdin.Close()

	pubBytes, err := io.ReadAll(io.LimitReader(pubOut, 64))
	pubOut.Close()
	if err != nil {
		pubCmd.Wait()
		return "", "", fmt.Errorf("failed to read wg pubkey output: %w", err)
	}

	if waitErr := pubCmd.Wait(); waitErr != nil {
		return "", "", fmt.Errorf("wg pubkey failed: %w%s", waitErr, stderr.String())
	}

	pubKey := strings.TrimSpace(string(pubBytes))

	return privKey, pubKey, nil
}

// LoadKeysFromDir loads signing and WireGuard keys from the given directory.
// Derives the WireGuard public key from the private key using `wg pubkey`.
func LoadKeysFromDir(dir string) (signingPriv ed25519.PrivateKey, signingPub ed25519.PublicKey, wgPriv, wgPub string, err error) {
	// Load signing key
	signingKeyPath := filepath.Join(dir, "node_private_key")
	signingData, err := os.ReadFile(signingKeyPath)
	if err != nil {
		return nil, nil, "", "", fmt.Errorf("failed to read signing key: %w", err)
	}

	signingPriv = ed25519.PrivateKey(signingData)
	signingPub = signingPriv.Public().(ed25519.PublicKey)

	// Load WireGuard private key
	wgKeyPath := filepath.Join(dir, "wireguard_private_key")
	wgData, err := os.ReadFile(wgKeyPath)
	if err != nil {
		return signingPriv, signingPub, "", "", fmt.Errorf("failed to read WireGuard key: %w", err)
	}

	wgPriv = strings.TrimSpace(string(wgData))

	// Derive public key via `wg pubkey`
	pubCmd := exec.Command("wg", "pubkey")
	pubStdin, err := pubCmd.StdinPipe()
	if err != nil {
		return signingPriv, signingPub, wgPriv, "", fmt.Errorf("failed to create stdin pipe for pubkey: %w", err)
	}
	pubOut, err := pubCmd.StdoutPipe()
	if err != nil {
		pubStdin.Close()
		return signingPriv, signingPub, wgPriv, "", fmt.Errorf("failed to create stdout pipe for pubkey: %w", err)
	}

	if _, err := pubStdin.Write([]byte(wgPriv + "\n")); err != nil {
		pubStdin.Close()
		return signingPriv, signingPub, wgPriv, "", fmt.Errorf("failed to write private key to pubkey pipe: %w", err)
	}
	pubStdin.Close()

	pubBytes, err := io.ReadAll(io.LimitReader(pubOut, 64))
	pubOut.Close()
	if err != nil {
		pubCmd.Wait()
		return signingPriv, signingPub, wgPriv, "", fmt.Errorf("failed to read wg pubkey output: %w", err)
	}

	if err := pubCmd.Wait(); err != nil {
		return signingPriv, signingPub, wgPriv, "", fmt.Errorf("wg pubkey failed: %w", err)
	}

	wgPub = strings.TrimSpace(string(pubBytes))

	return signingPriv, signingPub, wgPriv, wgPub, nil
}
