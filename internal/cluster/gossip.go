package cluster

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// GossipEngine handles peer-to-peer event gossip within the cluster.
// It runs periodic heartbeats, publishes events to connected peers,
// and processes events received from other nodes via the message bus.
type GossipEngine struct {
	cfg       *Config
	localNode string
	msgBus    *bus.MessageBus
	logger    *slog.Logger

	mu        sync.RWMutex
	peers     map[string]*PeerInfo
	running   bool
	sub       *bus.Subscriber
	stopCh    chan struct{}
	doneCh    chan struct{}
	eventID   string
}

// PeerInfo describes a connected cluster peer.
type PeerInfo struct {
	NodeID   string
	Endpoint string
	JoinedAt time.Time
	LastSeen time.Time
	Status   string // "active" | "inactive" | "syncing"
}

// NewGossipEngine creates a new gossip engine.
func NewGossipEngine(cfg *Config, localNode string, msgBus *bus.MessageBus, logger *slog.Logger) *GossipEngine {
	return &GossipEngine{
		cfg:       cfg,
		localNode: localNode,
		msgBus:    msgBus,
		logger:    logger,
		peers:     make(map[string]*PeerInfo),
		stopCh:    make(chan struct{}),
		doneCh:    make(chan struct{}),
	}
}

// Start begins the gossip heartbeat and event propagation loop.
func (g *GossipEngine) Start(ctx context.Context) error {
	g.mu.Lock()
	if g.running {
		g.mu.Unlock()
		return fmt.Errorf("gossip engine already running")
	}
	g.running = true
	g.mu.Unlock()

	g.logger.Info("gossip: starting engine", "node_id", g.localNode)

	// Subscribe to cluster events on the message bus
	if g.msgBus != nil {
		g.eventID = fmt.Sprintf("gossip-%s", g.localNode)
		g.sub = g.msgBus.Subscribe(g.eventID, "cluster.event.*")
	}

	// Heartbeat and event propagation goroutine
	go g.run(ctx)

	return nil
}

// Stop gracefully shuts down the gossip engine.
func (g *GossipEngine) Stop() error {
	g.mu.Lock()
	if !g.running {
		g.mu.Unlock()
		return nil
	}
	g.running = false
	g.mu.Unlock()

	close(g.stopCh)
	<-g.doneCh

	// Unsubscribe from bus if applicable
	if g.msgBus != nil && g.sub != nil {
		g.msgBus.Unsubscribe(g.sub)
	}

	g.mu.Lock()
	g.peers = make(map[string]*PeerInfo)
	g.mu.Unlock()

	g.logger.Info("gossip: engine stopped")
	return nil
}

// addPeer records information about a newly discovered peer.
func (g *GossipEngine) addPeer(info PeerInfo) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.peers[info.NodeID] = &info
}

// removePeer removes a peer from the gossip mesh.
func (g *GossipEngine) removePeer(nodeID string) {
	g.mu.Lock()
	defer g.mu.Unlock()
	delete(g.peers, nodeID)
}

// Peers returns a snapshot of known peers.
func (g *GossipEngine) Peers() []PeerInfo {
	g.mu.RLock()
	defer g.mu.RUnlock()
	result := make([]PeerInfo, 0, len(g.peers))
	for _, p := range g.peers {
		result = append(result, *p)
	}
	return result
}

// PeerCount returns the number of known peers.
func (g *GossipEngine) PeerCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.peers)
}

// run is the main gossip loop that drives heartbeats and event propagation.
func (g *GossipEngine) run(ctx context.Context) {
	defer close(g.doneCh)

	ticker := time.NewTicker(g.cfg.Gossip.HeartbeatInterval)
	defer ticker.Stop()

	g.logger.Info("gossip: engine started",
		"node_id", g.localNode,
		"heartbeat_interval", g.cfg.Gossip.HeartbeatInterval,
		"peer_timeout", g.cfg.Gossip.PeerTimeout,
	)

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.sendHeartbeat()
			g.pruneStalePeers()
		}
	}
}

// sendHeartbeat publishes a heartbeat event to the cluster.
func (g *GossipEngine) sendHeartbeat() {
	g.logger.Debug("gossip: sending heartbeat", "node", g.localNode)
	// Broadcast to cluster bus topic for peer discovery
	body, _ := models.NewBusMessage(models.MessageTypeEvent, "cluster", map[string]any{
		"event":        "heartbeat",
		"node_id":      g.localNode,
		"peer_count":   g.PeerCount(),
	})
	if g.msgBus != nil {
		g.msgBus.Publish("cluster.event.heartbeat", body)
	}
}

// handleClusterEvent processes an incoming cluster event from the message bus.
func (g *GossipEngine) handleClusterEvent(msg *models.BusMessage) {
	if msg == nil {
		return
	}
	_ = msg.Type
	// Process event and propagate to peers
	g.logger.Debug("gossip: received cluster event", "topic", msg.Topic)
}

// pruneStalePeers removes peers that haven't been seen within the timeout.
func (g *GossipEngine) pruneStalePeers() {
	g.mu.Lock()
	defer g.mu.Unlock()

	timeout := g.cfg.Gossip.PeerTimeout
	for nodeID, peer := range g.peers {
		if time.Since(peer.LastSeen) > timeout {
			g.logger.Info("gossip: peer timeout", "node_id", nodeID)
			delete(g.peers, nodeID)
		}
	}
}
