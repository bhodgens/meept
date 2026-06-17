package rpc

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/internal/queue"
)

// ClusterHandler provides native RPC methods for cluster management.
// It exposes cluster status, peer lists, and sync operations via JSON-RPC.
type ClusterHandler struct {
	gossip       *cluster.GossipEngine
	gitSync      *cluster.GitSync
	clusterMQ    *queue.ClusterQueue
	cfg          *cluster.Config
	store        *queue.Store
}

// NewClusterHandler creates a new cluster RPC handler.
func NewClusterHandler(gossip *cluster.GossipEngine, gitSync *cluster.GitSync, cfg *cluster.Config) *ClusterHandler {
	return &ClusterHandler{
		gossip: gossip,
		gitSync: gitSync,
		cfg:     cfg,
	}
}

// SetClusterQueue attaches the cluster-aware queue to the handler.
func (h *ClusterHandler) SetClusterQueue(mq *queue.ClusterQueue) {
	h.clusterMQ = mq
}

// SetStore attaches the queue store (for event log queries) to the handler.
func (h *ClusterHandler) SetStore(s *queue.Store) {
	if s != nil {
		h.store = s
	}
}

// RegisterClusterMethods registers cluster RPC methods on the server.
func (h *ClusterHandler) RegisterClusterMethods(server *Server) {
	server.RegisterHandler("cluster.status", h.handleStatus)
	server.RegisterHandler("cluster.peers", h.handlePeers)
	server.RegisterHandler("cluster.peer_count", h.handlePeerCount)
	server.RegisterHandler("cluster.join", h.handleJoin)
	server.RegisterHandler("cluster.start", h.handleStart)
	server.RegisterHandler("cluster.leave", h.handleLeave)
	server.RegisterHandler("cluster.reset", h.handleReset)
	server.RegisterHandler("cluster.debug.events", h.handleDebugEvents)
}

// StatusResponse holds the daemon cluster status.
type StatusResponse struct {
	Enabled     bool   `json:"enabled"`
	NodeID      string `json:"node_id"`
	ClusterID   string `json:"cluster_id"`
	PeerCount   int    `json:"peer_count"`
	GossipOK    bool   `json:"gossip_ok"`
	SyncOK      bool   `json:"sync_ok"`
	Claims      int    `json:"local_claims"`
	ClusterNode string `json:"cluster_node"`
}

// handleStatus returns the current cluster status.
func (h *ClusterHandler) handleStatus(_ context.Context, params json.RawMessage) (any, error) {
	resp := StatusResponse{
		Enabled:  h.cfg != nil,
		GossipOK: h.gossip != nil,
		SyncOK:   h.gitSync != nil,
	}

	if h.cfg != nil {
		resp.NodeID = h.cfg.NodeID
		resp.ClusterID = h.cfg.ClusterID
	}

	if h.gossip != nil {
		resp.PeerCount = h.gossip.PeerCount()
	}

	if h.clusterMQ != nil {
		stats, err := h.clusterMQ.Stats(context.Background())
		if err == nil {
			resp.Claims = stats.LocalClaims
			resp.ClusterNode = stats.LocalNode
		}
	}

	return resp, nil
}

// handlePeers returns a list of known cluster peers.
func (h *ClusterHandler) handlePeers(_ context.Context, params json.RawMessage) (any, error) {
	if h.gossip == nil {
		return []cluster.PeerInfo{}, nil
	}

	peers := h.gossip.Peers()
	return peers, nil
}

// handlePeerCount returns the number of known peers.
func (h *ClusterHandler) handlePeerCount(_ context.Context, params json.RawMessage) (any, error) {
	if h.gossip == nil {
		return 0, nil
	}

	return h.gossip.PeerCount(), nil
}

// handleReset resets cluster state (for development/testing).
func (h *ClusterHandler) handleReset(_ context.Context, params json.RawMessage) (any, error) {
	if h.gossip != nil {
		if err := h.gossip.Stop(); err != nil {
			return nil, fmt.Errorf("failed to stop gossip: %w", err)
		}
	}

	if h.gitSync != nil {
		if err := h.gitSync.Stop(); err != nil {
			return nil, fmt.Errorf("failed to stop git sync: %w", err)
		}
	}

	return map[string]any{
		RPCKeyStatus: "reset",
	}, nil
}

// JoinResponse is the structured response returned by cluster.join.
type JoinResponse struct {
	ClusterID   string                `json:"cluster_id"`
	ClusterName string                `json:"cluster_name"`
	Network     cluster.NetworkConfig `json:"network"`
	Gossip      cluster.GossipConfig  `json:"gossip"`
	Queue       cluster.QueueConfig   `json:"queue"`
	Security    cluster.SecurityConfig `json:"security"`
}

// handleJoin handles joining an existing cluster via a join key.
// It validates the join key against the cluster's stored key and returns
// the cluster configuration so the joining CLI can save it locally.
func (h *ClusterHandler) handleJoin(_ context.Context, params json.RawMessage) (any, error) {
	var req struct {
		JoinKey string `json:"join_key"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		return nil, fmt.Errorf("invalid join request: %w", err)
	}

	if req.JoinKey == "" {
		return nil, fmt.Errorf("join_key is required")
	}

	if h.cfg == nil {
		return nil, fmt.Errorf("cluster not configured on this node")
	}

	// Validate join key: if the cluster has a join key set, the request key
	// must match. If the cluster join key is empty, any key is accepted (open mode).
	// Use constant-time comparison to prevent timing side-channels on the join key.
	if h.cfg.JoinKey != "" {
		if subtle.ConstantTimeCompare([]byte(h.cfg.JoinKey), []byte(req.JoinKey)) != 1 {
			return nil, fmt.Errorf("invalid join key")
		}
	}

	// Build the config payload that the joining node needs to save locally.
	joinCfg := JoinResponse{
		ClusterID:   h.cfg.ClusterID,
		ClusterName: h.cfg.ClusterName,
		Network:     h.cfg.Network,
		Gossip:      h.cfg.Gossip,
		Queue:       h.cfg.Queue,
		Security:    h.cfg.Security,
	}

	cfgBytes, err := json.Marshal(joinCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal cluster config: %w", err)
	}

	return map[string]any{
		"cluster_name": h.cfg.ClusterName,
		"cluster_id":   h.cfg.ClusterID,
		"node_id":      h.cfg.NodeID,
		"config":       json.RawMessage(cfgBytes),
	}, nil
}

// handleStart starts the cluster coordination engine.
func (h *ClusterHandler) handleStart(_ context.Context, params json.RawMessage) (any, error) {
	if h.cfg == nil {
		return nil, fmt.Errorf("cluster not configured")
	}

	// The engine and git sync are started by the daemon on startup
	// This handler just confirms the cluster is running
	if h.gossip != nil && h.gitSync != nil {
		return map[string]any{
			"status":       "running",
			"node_id":      h.cfg.NodeID,
			"cluster_name": h.cfg.ClusterName,
			"cluster_id":   h.cfg.ClusterID,
		}, nil
	}

	// If not already started, we can't start them here (need context with proper lifecycle)
	// This would be handled by the daemon's Start() method
	return map[string]any{
		"status":       "configured_not_running",
		"node_id":      h.cfg.NodeID,
		"cluster_name": h.cfg.ClusterName,
		"cluster_id":   h.cfg.ClusterID,
	}, nil
}

// handleLeave gracefully leaves the cluster.
func (h *ClusterHandler) handleLeave(_ context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Force bool `json:"force"`
	}
	_ = json.Unmarshal(params, &req)

	if h.gitSync != nil && h.cfg != nil {
		// Stop git sync which will push a leave event
		if err := h.gitSync.Leave(); err != nil {
			return nil, fmt.Errorf("failed to leave cluster: %w", err)
		}
	}

	// Stop gossip engine
	if h.gossip != nil {
		if err := h.gossip.Stop(); err != nil {
			return nil, fmt.Errorf("failed to stop gossip: %w", err)
		}
	}

	return map[string]any{
		"message": "left cluster successfully",
	}, nil
}

// DebugEventRow represents a single cluster event row for the debug events RPC.
type DebugEventRow struct {
	EventID   string `json:"event_id"`
	NodeID    string `json:"node_id"`
	EventType string `json:"event_type"`
	Timestamp string `json:"timestamp"`
	Payload   string `json:"payload_summary"`
}

// handleDebugEvents returns recent cluster events from the event log.
func (h *ClusterHandler) handleDebugEvents(_ context.Context, params json.RawMessage) (any, error) {
	var req struct {
		Limit int `json:"limit"`
	}
	if err := json.Unmarshal(params, &req); err != nil {
		req.Limit = 50
	}
	if req.Limit <= 0 {
		req.Limit = 50
	}
	if req.Limit > 1000 {
		req.Limit = 1000
	}

	if h.store == nil {
		return []DebugEventRow{}, nil
	}

	db := h.store.DB()
	rows, err := db.Query(`
		SELECT event_id, node_id, event_type, timestamp, payload
		FROM cluster_events
		ORDER BY received_at DESC
		LIMIT ?`,
		req.Limit,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return []DebugEventRow{}, nil
		}
		return nil, fmt.Errorf("failed to query cluster events: %w", err)
	}
	defer rows.Close()

	var events []DebugEventRow
	for rows.Next() {
		var ev DebugEventRow
		var tsNano int64
		var payload string

		if err := rows.Scan(&ev.EventID, &ev.NodeID, &ev.EventType, &tsNano, &payload); err != nil {
			return nil, fmt.Errorf("failed to scan cluster event: %w", err)
		}

		ev.Timestamp = time.Unix(0, tsNano).UTC().Format(time.RFC3339)

		// Truncate payload for readability
		if len(payload) > 120 {
			ev.Payload = payload[:117] + "..."
		} else {
			ev.Payload = payload
		}

		events = append(events, ev)
	}

	if events == nil {
		events = []DebugEventRow{}
	}

	return events, nil
}
