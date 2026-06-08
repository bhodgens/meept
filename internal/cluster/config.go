package cluster

import (
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// Config holds the global cluster configuration.
type Config struct {
	// Identity
	ClusterID   string `json:"cluster_id"`
	ClusterName string `json:"cluster_name"`

	// JoinKey is the secret key required for new nodes to join the cluster.
	// When empty, any join key is accepted (open mode).
	JoinKey string `json:"join_key"`

	// Network configuration (WireGuard)
	Network NetworkConfig `json:"network"`

	// Gossip configuration
	Gossip GossipConfig `json:"gossip"`

	// Queue configuration
	Queue QueueConfig `json:"queue"`

	// Git sync configuration
	Git GitConfig `json:"git"`

	// Security configuration
	Security SecurityConfig `json:"security"`

	// Local node identity
	NodeID string `json:"node_id"`
}

// QueueConfig holds task queue settings.
type QueueConfig struct {
	DefaultClaimTimeout     time.Duration `json:"default_claim_timeout"`
	NodeReachabilityTimeout time.Duration `json:"node_reachability_timeout"`
	FullPayloadReplication  bool          `json:"full_payload_replication"`
}

// NetworkConfig holds WireGuard network settings.
type NetworkConfig struct {
	WireGuardSubnet string `json:"wireguard_subnet"`
	WireGuardPort   int    `json:"wireguard_port"`
	Interface       string `json:"mesh_interface"`
}

// GossipConfig holds gossip protocol settings.
type GossipConfig struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval"`
	PeerTimeout       time.Duration `json:"peer_timeout"`
	EventRetention    time.Duration `json:"event_retention"`
	MaxRetryAttempts  int           `json:"max_retry_attempts"`
}

// GitConfig holds git sync settings.
type GitConfig struct {
	SyncInterval    time.Duration `json:"sync_interval"`
	HeartbeatCommit bool          `json:"heartbeat_commit"`
	RemoteURL       string        `json:"remote_url"`
}

// SecurityConfig holds security settings.
type SecurityConfig struct {
	RequireNodeSignatures   bool `json:"require_node_signatures"`
	Ed25519KeyRotationDays  int  `json:"ed25519_key_rotation_days"`
}

// LoadClusterConfig loads cluster configuration from a JSON5 file.
func LoadClusterConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := config.UnmarshalJSON5(data, &cfg); err != nil {
		return nil, err
	}

	cfg.setDefault()

	return &cfg, nil
}

// LoadClusterConfigFromDefaultPath loads cluster config from the default location.
func LoadClusterConfigFromDefaultPath() (*Config, error) {
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return nil, err
	}
	return LoadClusterConfig(filepath.Join(homeDir, ".meept", "cluster", "config.json5"))
}

// DefaultClusterConfigPath returns the default path for the cluster config.
func DefaultClusterConfigPath() string {
	homeDir, _ := os.UserHomeDir()
	return filepath.Join(homeDir, ".meept", "cluster", "config.json5")
}

// setDefault applies default values for missing fields.
func (c *Config) setDefault() {
	if c.Network.WireGuardPort == 0 {
		c.Network.WireGuardPort = 51820
	}
	if c.Network.Interface == "" {
		c.Network.Interface = "wg0"
	}
	if c.Gossip.HeartbeatInterval == 0 {
		c.Gossip.HeartbeatInterval = 30 * time.Second
	}
	if c.Gossip.PeerTimeout == 0 {
		c.Gossip.PeerTimeout = 2 * time.Minute
	}
	if c.Gossip.MaxRetryAttempts == 0 {
		c.Gossip.MaxRetryAttempts = 3
	}
	if c.Queue.DefaultClaimTimeout == 0 {
		c.Queue.DefaultClaimTimeout = 5 * time.Minute
	}
	if c.Queue.NodeReachabilityTimeout == 0 {
		c.Queue.NodeReachabilityTimeout = 2 * time.Minute
	}
	if c.Git.SyncInterval == 0 {
		c.Git.SyncInterval = 5 * time.Minute
	}
	if c.Security.Ed25519KeyRotationDays == 0 {
		c.Security.Ed25519KeyRotationDays = 90
	}
}
