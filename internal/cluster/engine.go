package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"

	"github.com/caimlas/meept/internal/bus"

	"github.com/cespare/xxhash/v2"
	q "github.com/caimlas/meept/internal/queue"
)

// Engine is the central orchestrator for cluster operations.
// It manages gossip communication, git-based membership sync,
// node signing, and integrates with the task queue store.
type Engine struct {
	cfg        *Config
	localCfg   *Config
	logger     *slog.Logger
	msgBus     *bus.MessageBus
	queueStore *q.Store
	gitRepoPath string

	gossip    *GossipEngine
	gitSync   *GitSync
	wgMgr     *WireGuardManager
	signingPriv ed25519.PrivateKey
	signingPub  ed25519.PublicKey

	nodeID   string
	nodeName string

	enableWireGuard bool

	running bool
	mu      sync.RWMutex
}

// EngineConfig holds parameters for NewEngine.
type EngineConfig struct {
	Cfg              *Config
	LocalCfg         *Config
	Logger           *slog.Logger
	MsgBus           *bus.MessageBus
	QueueStore       *q.Store
	GitRepoPath      string
	NodeName         string
	EnableWireGuard  bool
}

// NewEngine creates a new cluster engine from the given configuration.
// It does not start any background routines; call Start() to begin.
func NewEngine(cfg EngineConfig) *Engine {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	nodeID := ""
	if cfg.Cfg != nil {
		nodeID = cfg.Cfg.NodeID
	} else if cfg.LocalCfg != nil {
		nodeID = cfg.LocalCfg.NodeID
	}

	return &Engine{
		cfg:             cfg.Cfg,
		localCfg:        cfg.LocalCfg,
		logger:          cfg.Logger,
		msgBus:          cfg.MsgBus,
		queueStore:      cfg.QueueStore,
		gitRepoPath:     cfg.GitRepoPath,
		nodeID:          nodeID,
		nodeName:        cfg.NodeName,
		enableWireGuard: cfg.EnableWireGuard,
	}
}

// Start initializes and starts all cluster components (gossip and git sync).
func (e *Engine) Start(ctx context.Context) error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if e.running {
		return fmt.Errorf("cluster engine already running")
	}

	// Load or generate signing keys
	if e.signingPriv == nil {
		if err := e.loadKeys(); err != nil {
			return fmt.Errorf("engine: load keys: %w", err)
		}
	}

	// Ensure node ID is resolved
	if e.nodeID == "" {
		e.nodeID = nodeIDFromPub(e.signingPub)
		if e.cfg != nil {
			e.cfg.NodeID = e.nodeID
		}
		if e.localCfg != nil {
			e.localCfg.NodeID = e.nodeID
		}
	}

	// Create git sync first so we can pass it as MembersProvider to gossip
	e.gitSync = NewGitSync(e.cfg, e.localCfg, e.gitRepoPath, e.logger)
	if err := e.gitSync.Start(ctx); err != nil {
		return fmt.Errorf("engine: start git sync: %w", err)
	}

	// Build gossip options
	gossipOpts := []GossipOption{
		WithSigningKey(e.signingPriv, e.signingPub),
		WithMembersProvider(e.gitSync), // Enables TCP transport for peer delivery
	}
	// Wire database if queue store has a usable db handle
	if e.queueStore != nil {
		gossipOpts = append(gossipOpts, WithDatabase(e.queueStore.DB()))
	}

	// Create and start gossip engine
	e.gossip = NewGossipEngine(e.cfg, e.nodeID, e.msgBus, e.logger, gossipOpts...)
	if err := e.gossip.Start(ctx); err != nil {
		gitErr := e.gitSync.Stop()
		if gitErr != nil {
			e.logger.Warn("engine: stop git_sync after gossip failure", "error", gitErr)
		}
		return fmt.Errorf("engine: start gossip: %w", err)
	}

	// Create and start WireGuard sync (if enabled)
	// WireGuard tools (wg, wg-quick, ip) are Linux-only; on other platforms
	// the sync loop will log warnings but will not crash the engine.
	if e.enableWireGuard {
		wgConfigPath := filepath.Join(e.gitRepoPath, "wireguard")
		wgMgr, err := NewWireGuardManager(wgConfigPath, e.cfg.Network.Interface)
		if err != nil {
			e.logger.Warn("engine: wireguard manager creation failed, continuing without it",
				"error", err,
			)
		} else {
			e.wgMgr = wgMgr
		}
	}

	e.running = true
	e.logger.Info("cluster engine started",
		"node_id", e.nodeID,
		"node_name", e.nodeName,
		"repo", e.gitRepoPath,
	)

	return nil
}

// Stop gracefully shuts down all cluster components.
func (e *Engine) Stop() error {
	e.mu.Lock()
	defer e.mu.Unlock()

	if !e.running {
		return nil
	}

	e.logger.Info("cluster engine stopping", "node_id", e.nodeID)

	// Stop git sync first so it can push final state
	if e.gitSync != nil {
		if err := e.gitSync.Stop(); err != nil {
			e.logger.Warn("engine: git sync stop error", "error", err)
		}
	}

	// Stop WireGuard manager before gossip
	if e.wgMgr != nil {
		if err := e.wgMgr.Stop(); err != nil {
			e.logger.Warn("engine: wireguard stop error", "error", err)
		}
	}

	// Then stop gossip
	if e.gossip != nil {
		if err := e.gossip.Stop(); err != nil {
			e.logger.Warn("engine: gossip stop error", "error", err)
		}
	}

	e.running = false
	e.logger.Info("cluster engine stopped", "node_id", e.nodeID)
	return nil
}

// NodeID returns this node's unique identifier.
func (e *Engine) NodeID() string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.nodeID
}

// Config returns the global cluster configuration.
func (e *Engine) Config() *Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.cfg
}

// LocalCfg returns the local node configuration.
func (e *Engine) LocalCfg() *Config {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.localCfg
}

// Gossip returns the gossip engine, or nil if not started.
func (e *Engine) Gossip() *GossipEngine {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.gossip
}

// GitSync returns the git sync instance, or nil if not started.
func (e *Engine) GitSync() *GitSync {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.gitSync
}

// WireGuardManager returns the WireGuard manager instance, or nil if not started.
func (e *Engine) WireGuardManager() *WireGuardManager {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.wgMgr
}

// SigningKey returns the node's ed25519 signing key pair.
func (e *Engine) SigningKey() (ed25519.PrivateKey, ed25519.PublicKey) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.signingPriv, e.signingPub
}

// IsRunning returns true if the engine is currently active.
func (e *Engine) IsRunning() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()
	return e.running
}

// loadKeys loads ed25519 signing keys from the local keys directory,
// or generates a new key pair if none exist.
func (e *Engine) loadKeys() error {
	keysDir := defaultKeysDir()

	if err := os.MkdirAll(keysDir, 0o700); err != nil {
		return fmt.Errorf("engine: create keys dir: %w", err)
	}

	privPath := filepath.Join(keysDir, "signing_priv.ed25519")
	pubPath := filepath.Join(keysDir, "signing_pub.ed25519")

	// Try to load existing keys
	privData, err := os.ReadFile(privPath)
	if err == nil {
		// Private key is 64 bytes (32-byte seed + 32-byte public key)
		if len(privData) == 64 {
			e.signingPriv = ed25519.NewKeyFromSeed(privData[:32])
		} else {
			e.logger.Warn("engine: corrupt existing private key (bad length), generating new")
			goto generate
		}
		pubData, err2 := os.ReadFile(pubPath)
		if err2 == nil {
			if len(pubData) == ed25519.PublicKeySize {
				e.signingPub = pubData
			}
		} else {
			e.signingPub = e.signingPriv.Public().(ed25519.PublicKey)
		}
		e.logger.Info("engine: loaded existing signing keys",
			"pubkey_prefix", fmt.Sprintf("%08x", xxhash.Sum64(e.signingPub)))
		return nil
	}

generate:
	// Generate new keys
	e.logger.Info("engine: generating new signing keys")
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return fmt.Errorf("engine: generate signing key: %w", err)
	}

	e.signingPriv = priv
	e.signingPub = pub

	// Persist keys (64-byte seed+pub format for private key)
	_ = priv // priv is already a 64-byte PrivateKey (seed + pub concatenated)
	if err := os.WriteFile(privPath, priv, 0o600); err != nil {
		return fmt.Errorf("engine: write private key: %w", err)
	}
	pubBytes := pub
	if err := os.WriteFile(pubPath, pubBytes, 0o600); err != nil {
		return fmt.Errorf("engine: write public key: %w", err)
	}

	e.logger.Info("engine: generated and persisted new signing keys",
		"pubkey_prefix", fmt.Sprintf("%08x", xxhash.Sum64(e.signingPub)))
	return nil
}

// defaultKeysDir returns the default path for cluster signing keys.
func defaultKeysDir() string {
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".meept", "cluster", "keys")
}

// nodeIDFromPub derives a deterministic node ID from an ed25519 public key.
func nodeIDFromPub(pub ed25519.PublicKey) string {
	return fmt.Sprintf("node-%08x", xxhash.Sum64(pub))
}
