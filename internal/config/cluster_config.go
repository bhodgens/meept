// Package config provides configuration loading and validation for meept.
package config

import "time"

// ClusterConfig holds distributed cluster settings.
type ClusterConfig struct {
	Enabled              bool                     `json:"enabled" toml:"enabled"`
	ClusterID            string                   `json:"cluster_id" toml:"cluster_id"`
	ClusterName          string                   `json:"cluster_name" toml:"cluster_name"`
	NodeID               string                   `json:"node_id" toml:"node_id"`
	NodeName             string                   `json:"node_name" toml:"node_name"`
	Network              ClusterNetworkConfig     `json:"network" toml:"network"`
	Gossip               ClusterGossipConfig      `json:"gossip" toml:"gossip"`
	Queue                ClusterQueueConfig       `json:"queue" toml:"queue"`
	Git                  ClusterGitConfig         `json:"git" toml:"git"`
	Security             ClusterSecurityConfig    `json:"security" toml:"security"`
}

// ClusterNetworkConfig holds WireGuard network settings.
type ClusterNetworkConfig struct {
	WireGuardSubnet string `json:"wireguard_subnet" toml:"wireguard_subnet"`
	WireGuardPort   int    `json:"wireguard_port" toml:"wireguard_port"`
	Interface       string `json:"interface" toml:"interface"`
}

// ClusterGossipConfig holds gossip protocol settings.
type ClusterGossipConfig struct {
	HeartbeatInterval time.Duration `json:"heartbeat_interval" toml:"heartbeat_interval"`
	PeerTimeout       time.Duration `json:"peer_timeout" toml:"peer_timeout"`
	EventRetention    time.Duration `json:"event_retention" toml:"event_retention"`
	MaxRetryAttempts  int           `json:"max_retry_attempts" toml:"max_retry_attempts"`
}

// ClusterQueueConfig holds cluster queue settings.
type ClusterQueueConfig struct {
	DefaultClaimTimeout     time.Duration `json:"default_claim_timeout" toml:"default_claim_timeout"`
	NodeReachabilityTimeout time.Duration `json:"node_reachability_timeout" toml:"node_reachability_timeout"`
	FullPayloadReplication  bool          `json:"full_payload_replication" toml:"full_payload_replication"`
}

// ClusterGitConfig holds git sync settings.
type ClusterGitConfig struct {
	SyncInterval    time.Duration `json:"sync_interval" toml:"sync_interval"`
	HeartbeatCommit bool          `json:"heartbeat_commit" toml:"heartbeat_commit"`
	RemoteURL       string        `json:"remote_url" toml:"remote_url"`
}

// ClusterSecurityConfig holds cluster security settings.
type ClusterSecurityConfig struct {
	RequireNodeSignatures  bool `json:"require_node_signatures" toml:"require_node_signatures"`
	Ed25519KeyRotationDays int  `json:"ed25519_key_rotation_days" toml:"ed25519_key_rotation_days"`
}

// DefaultClusterConfig returns default cluster configuration.
func DefaultClusterConfig() ClusterConfig {
	return ClusterConfig{
		Enabled:     false,
		ClusterID:   "",
		ClusterName: "",
		NodeID:      "",
		NodeName:    "",
		Network: ClusterNetworkConfig{
			WireGuardSubnet: "10.200.0.0/24",
			WireGuardPort:   51820,
			Interface:       "wg0",
		},
		Gossip: ClusterGossipConfig{
			HeartbeatInterval: 30 * time.Second,
			PeerTimeout:       2 * time.Minute,
			EventRetention:    1 * time.Hour,
			MaxRetryAttempts:  3,
		},
		Queue: ClusterQueueConfig{
			DefaultClaimTimeout:     5 * time.Minute,
			NodeReachabilityTimeout: 2 * time.Minute,
			FullPayloadReplication:  false,
		},
		Git: ClusterGitConfig{
			SyncInterval:    5 * time.Minute,
			HeartbeatCommit: true,
			RemoteURL:       "",
		},
		Security: ClusterSecurityConfig{
			RequireNodeSignatures:  true,
			Ed25519KeyRotationDays: 90,
		},
	}
}
