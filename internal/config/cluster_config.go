// Package config provides configuration loading and validation for meept.
package config

import "time"

// ClusterConfig holds distributed cluster settings.
type ClusterConfig struct {
	Enabled     bool                  `json:"enabled" toml:"enabled"`
	ClusterID   string                `json:"cluster_id" toml:"cluster_id"`
	ClusterName string                `json:"cluster_name" toml:"cluster_name"`
	NodeID      string                `json:"node_id" toml:"node_id"`
	NodeName    string                `json:"node_name" toml:"node_name"`
	Network     ClusterNetworkConfig  `json:"network" toml:"network"`
	Gossip      ClusterGossipConfig   `json:"gossip" toml:"gossip"`
	Queue       ClusterQueueConfig    `json:"queue" toml:"queue"`
	Git         ClusterGitConfig      `json:"git" toml:"git"`
	Security    ClusterSecurityConfig `json:"security" toml:"security"`

	// Cluster resource model (spec 2026-07-01-cluster-resource-model-design.md).
	// CAS store, ephemeral workspaces, and dispatch defaults. Nil-safe zero
	// values disable resource-model features.
	Resources  ClusterResourcesConfig  `json:"resources" toml:"resources"`
	Workspace  ClusterWorkspaceConfig  `json:"workspace" toml:"workspace"`
	Dispatch   ClusterDispatchConfig   `json:"dispatch" toml:"dispatch"`
}

// ClusterResourcesConfig configures the CAS transit cache. Files enter only
// when referenced by an in-flight dispatched task; refcount-driven eviction
// removes them once no task references them. Local-only files never enter
// CAS. See spec §4.1, §2.4, §7, §9.
type ClusterResourcesConfig struct {
	CASStoreDir            string        `json:"cas_store_dir" toml:"cas_store_dir"`
	CASCapacityBytes       int64         `json:"cas_capacity_bytes" toml:"cas_capacity_bytes"`
	EvictionSweepInterval  time.Duration `json:"eviction_sweep_interval" toml:"eviction_sweep_interval"`
	PinnedHashes           []string      `json:"pinned_hashes" toml:"pinned_hashes"`
	HashAlgorithm          string        `json:"hash_algorithm" toml:"hash_algorithm"`
}

// ClusterWorkspaceConfig configures ephemeral per-job working trees.
// See spec §4.2, §9.
type ClusterWorkspaceConfig struct {
	WorktreeRoot       string `json:"worktree_root" toml:"worktree_root"`
	GitFallbackToPeer  bool   `json:"git_fallback_to_peer" toml:"git_fallback_to_peer"`
}

// ClusterDispatchConfig configures cross-daemon dispatch behavior including
// peer fallback and scheduler capacity policies. See spec §2.5, §9.
type ClusterDispatchConfig struct {
	DefaultClaimTimeout        time.Duration `json:"default_claim_timeout" toml:"default_claim_timeout"`
	ResultDeliveryTimeout      time.Duration `json:"result_delivery_timeout" toml:"result_delivery_timeout"`
	PeerFallbackPolicy         string        `json:"peer_fallback_policy" toml:"peer_fallback_policy"`                   // always | never | if_capacity
	SchedulerNoCapacityPolicy  string        `json:"scheduler_no_capacity_policy" toml:"scheduler_no_capacity_policy"`   // queue | run_local
	PeerDropCooldown           time.Duration `json:"peer_drop_cooldown" toml:"peer_drop_cooldown"`
	QuarantinePeriod           time.Duration `json:"quarantine_period" toml:"quarantine_period"`
	QuarantineThreshold        int           `json:"quarantine_threshold" toml:"quarantine_threshold"`
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
		Resources: ClusterResourcesConfig{
			CASStoreDir:           "~/.meept/resources",
			CASCapacityBytes:      10 * 1024 * 1024 * 1024, // 10 GB default (spec §7)
			EvictionSweepInterval: 5 * time.Minute,
			PinnedHashes:          nil,
			HashAlgorithm:         "blake3",
		},
		Workspace: ClusterWorkspaceConfig{
			WorktreeRoot:      "~/.meept/worktrees",
			GitFallbackToPeer: true,
		},
		Dispatch: ClusterDispatchConfig{
			DefaultClaimTimeout:       5 * time.Minute,
			ResultDeliveryTimeout:     1 * time.Hour,
			PeerFallbackPolicy:        "if_capacity",
			SchedulerNoCapacityPolicy: "queue",
			PeerDropCooldown:          30 * time.Second,
			QuarantinePeriod:          1 * time.Hour,
			QuarantineThreshold:       3,
		},
	}
}
