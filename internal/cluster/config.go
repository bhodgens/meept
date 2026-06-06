package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// Duration is a time.Duration wrapper that supports JSON unmarshaling from strings.
type Duration time.Duration

// UnmarshalJSON implements json.Unmarshaler for Duration.
func (d *Duration) UnmarshalJSON(data []byte) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	switch value := v.(type) {
	case float64:
		*d = Duration(time.Duration(value))
		return nil
	case string:
		tmp, err := time.ParseDuration(value)
		if err != nil {
			return err
		}
		*d = Duration(tmp)
		return nil
	default:
		return fmt.Errorf("cannot unmarshal %T into Duration", value)
	}
}

// MarshalJSON implements json.Marshaler for Duration.
func (d *Duration) MarshalJSON() ([]byte, error) {
	return json.Marshal(d.String())
}

// String returns the duration as a string.
func (d *Duration) String() string {
	return time.Duration(*d).String()
}

// Config holds the global cluster configuration.
type Config struct {
	ClusterID   string        `json:"cluster_id"`
	ClusterName string        `json:"cluster_name"`
	NodeID      string        `json:"node_id"`
	Network     NetworkConfig `json:"network"`
	Gossip      GossipConfig  `json:"gossip"`
	Queue       QueueConfig   `json:"queue"`
	Git         GitConfig     `json:"git"`
	Security    SecurityConfig `json:"security"`
}

// NetworkConfig holds WireGuard network settings.
type NetworkConfig struct {
	WireGuardSubnet string `json:"wireguard_subnet"`
	WireGuardPort   int    `json:"wireguard_port"`
	Interface       string `json:"mesh_interface"`
}

// GossipConfig holds gossip protocol settings.
type GossipConfig struct {
	HeartbeatInterval Duration `json:"heartbeat_interval"`
	PeerTimeout       Duration `json:"peer_timeout"`
	EventRetention    Duration `json:"event_retention"`
	MaxRetryAttempts  int      `json:"max_retry_attempts"`
}

// QueueConfig holds task queue settings.
type QueueConfig struct {
	DefaultClaimTimeout     Duration `json:"default_claim_timeout"`
	NodeReachabilityTimeout Duration `json:"node_reachability_timeout"`
	FullPayloadReplication  bool     `json:"full_payload_replication"`
}

// GitConfig holds git sync settings.
type GitConfig struct {
	SyncInterval    Duration `json:"sync_interval"`
	HeartbeatCommit bool     `json:"heartbeat_commit"`
	RemoteURL       string   `json:"remote_url"`
}

// SecurityConfig holds security settings.
type SecurityConfig struct {
	RequireNodeSignatures  bool `json:"require_node_signatures"`
	Ed25519KeyRotationDays int  `json:"ed25519_key_rotation_days"`
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
		c.Gossip.HeartbeatInterval = Duration(30 * time.Second)
	}
	if c.Gossip.PeerTimeout == 0 {
		c.Gossip.PeerTimeout = Duration(2 * time.Minute)
	}
	if c.Gossip.MaxRetryAttempts == 0 {
		c.Gossip.MaxRetryAttempts = 3
	}
	if c.Queue.DefaultClaimTimeout == 0 {
		c.Queue.DefaultClaimTimeout = Duration(5 * time.Minute)
	}
	if c.Queue.NodeReachabilityTimeout == 0 {
		c.Queue.NodeReachabilityTimeout = Duration(2 * time.Minute)
	}
	if c.Git.SyncInterval == 0 {
		c.Git.SyncInterval = Duration(5 * time.Minute)
	}
	if c.Security.Ed25519KeyRotationDays == 0 {
		c.Security.Ed25519KeyRotationDays = 90
	}
}

// ToTimeDuration converts Duration to time.Duration.
func (d Duration) ToTimeDuration() time.Duration {
	return time.Duration(d)
}
