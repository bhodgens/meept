package cluster

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/caimlas/meept/internal/config"
)

// Member represents a cluster member (stored in git nodes/*.json5).
type Member struct {
	NodeID              string    `json:"node_id"`
	NodeName            string    `json:"node_name"`
	WireGuardPub        string    `json:"wireguard_pubkey"`
	SigningPub          []byte    `json:"signing_pubkey"`
	Endpoint            string    `json:"endpoint"`
	Capabilities        []string  `json:"capabilities"`
	ClusterIP           string    `json:"cluster_ip"`
	PersistentKeepalive string    `json:"persistent_keepalive,omitempty"`
	JoinedAt            time.Time `json:"joined_at"`
	LastHeartbeat       time.Time `json:"last_heartbeat"`
	Status              string    `json:"status"` // "active" | "inactive" | "leaving"
}

// MemberPath returns the path to a node's registry file.
func MemberPath(baseDir, nodeID string) string {
	return filepath.Join(baseDir, "nodes", nodeID+".json5")
}

// LoadMember loads a member record from a local directory (git-synced).
func LoadMember(baseDir, nodeID string) (*Member, error) {
	path := MemberPath(baseDir, nodeID)
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read member %s: %w", nodeID, err)
	}

	var member Member
	if err := config.UnmarshalJSON5(data, &member); err != nil {
		return nil, fmt.Errorf("failed to parse member %s: %w", nodeID, err)
	}

	return &member, nil
}

// SaveMember saves a member record to the local directory (will be committed to git).
func SaveMember(baseDir string, member *Member) error {
	path := MemberPath(baseDir, member.NodeID)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create member directory: %w", err)
	}
	data, err := json.MarshalIndent(member, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal member: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// DeleteMember removes a member record from the local directory.
func DeleteMember(baseDir, nodeID string) error {
	path := MemberPath(baseDir, nodeID)
	return os.Remove(path)
}

// UpdateHeartbeat updates the last_heartbeat timestamp on a member.
func UpdateHeartbeat(member *Member) {
	member.LastHeartbeat = time.Now().UTC()
}

// IsActive checks if a member's heartbeat is within the peer timeout window.
func (m *Member) IsActive(timeout time.Duration) bool {
	if m.Status != "active" {
		return false
	}
	return time.Since(m.LastHeartbeat) <= timeout
}

// SaveClusterConfig saves cluster configuration to a JSON5 file.
func SaveClusterConfig(path string, cfg *Config) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	return os.WriteFile(path, data, 0o600)
}

// ListLocalMembers reads all nodes/*.json5 files from baseDir.
// Returns a map of nodeID -> Member.
func ListLocalMembers(baseDir string) (map[string]*Member, error) {
	nodesDir := filepath.Join(baseDir, "nodes")
	entries, err := os.ReadDir(nodesDir)
	if err != nil {
		if os.IsNotExist(err) {
			return make(map[string]*Member), nil
		}
		return nil, fmt.Errorf("failed to read nodes directory: %w", err)
	}

	members := make(map[string]*Member)
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		if ext := filepath.Ext(entry.Name()); ext != ".json5" {
			continue
		}
		nodeID := entry.Name()[:len(entry.Name())-len(".json5")]
		member, err := LoadMember(baseDir, nodeID)
		if err != nil {
			// Skip broken entries but log
			continue
		}
		members[nodeID] = member
	}

	return members, nil
}
