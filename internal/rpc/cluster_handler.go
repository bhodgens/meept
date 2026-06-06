package rpc

import (
	"context"
	"encoding/json"
	"fmt"

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

// RegisterClusterMethods registers cluster RPC methods on the server.
func (h *ClusterHandler) RegisterClusterMethods(server *Server) {
	server.RegisterHandler("cluster.status", h.handleStatus)
	server.RegisterHandler("cluster.peers", h.handlePeers)
	server.RegisterHandler("cluster.peer_count", h.handlePeerCount)
	server.RegisterHandler("cluster.reset", h.handleReset)
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
		Enabled:   h.cfg != nil,
		NodeID:    h.cfg.NodeID,
		ClusterID: h.cfg.ClusterID,
		GossipOK:  h.gossip != nil,
		SyncOK:    h.gitSync != nil,
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
