package cluster

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"sort"
	"sync"
	"time"

	"github.com/caimlas/meept/pkg/models"
)

// MembersProvider resolves cluster members for peer address lookup.
// Implemented by GitSync to provide the member registry.
type MembersProvider interface {
	// GetActiveMembers returns currently active cluster members.
	GetActiveMembers() (map[string]*Member, error)
}

// GossipTransport handles peer-to-peer TCP communication for cluster events.
// It listens for incoming events and sends outgoing events to known peers
// over the WireGuard mesh network.
type GossipTransport struct {
	cfg       *Config
	localNode string
	logger    *slog.Logger
	gossip    *GossipEngine
	members   MembersProvider

	mu      sync.RWMutex
	running bool
	stopCh  chan struct{}
	doneCh  chan struct{}

	listener net.Listener

	// Track which events we've already sent to each peer to avoid
	// sending duplicates back to the originator.
	sentEvents map[string]map[string]time.Time // peerAddr -> eventID -> sent time
	sentMu     sync.RWMutex
}

// NewGossipTransport creates a new TCP-based gossip transport.
func NewGossipTransport(cfg *Config, localNode string, gossip *GossipEngine, members MembersProvider, logger *slog.Logger) *GossipTransport {
	return &GossipTransport{
		cfg:        cfg,
		localNode:  localNode,
		logger:     logger,
		gossip:     gossip,
		members:    members,
		stopCh:     make(chan struct{}),
		doneCh:     make(chan struct{}),
		sentEvents: make(map[string]map[string]time.Time),
	}
}

// gossipListenPort returns the TCP port for gossip communication.
// Defaults to WireGuard port + 1 (51821).
func gossipListenPort(cfg *Config) int {
	if cfg == nil {
		return 51821
	}
	// Use WG port + 1 as gossip port
	return cfg.Network.WireGuardPort + 1
}

// gossipListenAddr returns the address to listen on for gossip TCP connections.
func gossipListenAddr(cfg *Config) string {
	return fmt.Sprintf(":%d", gossipListenPort(cfg))
}

// Start begins listening for incoming gossip connections and starts the
// peer event forwarding loop.
func (t *GossipTransport) Start(ctx context.Context) error {
	t.mu.Lock()
	if t.running {
		t.mu.Unlock()
		return fmt.Errorf("gossip transport already running")
	}
	t.running = true
	t.mu.Unlock()

	// Start TCP listener
	addr := gossipListenAddr(t.cfg)
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.mu.Lock()
		t.running = false
		t.mu.Unlock()
		return fmt.Errorf("gossip transport: listen on %s: %w", addr, err)
	}
	t.listener = listener

	t.logger.Info("gossip_transport: listening for peer events",
		"address", addr,
		"node_id", t.localNode,
	)

	go t.acceptLoop(ctx)

	return nil
}

// Stop gracefully shuts down the gossip transport.
func (t *GossipTransport) Stop() error {
	t.mu.Lock()
	if !t.running {
		t.mu.Unlock()
		return nil
	}
	t.running = false
	t.mu.Unlock()

	if t.listener != nil {
		t.listener.Close()
	}
	close(t.stopCh)
	<-t.doneCh

	t.logger.Info("gossip_transport: stopped")
	return nil
}

// SendEvent sends a cluster event to all known active peers via TCP.
// This is called by the GossipEngine after publishing an event locally.
func (t *GossipTransport) SendEvent(event *models.ClusterEvent) {
	if event == nil {
		return
	}

	members, err := t.getActivePeers()
	if err != nil {
		t.logger.Warn("gossip_transport: failed to get peers for send", "error", err)
		return
	}

	data, err := json.Marshal(event)
	if err != nil {
		t.logger.Error("gossip_transport: failed to marshal event", "error", err)
		return
	}

	for nodeID, member := range members {
		if nodeID == t.localNode {
			continue // Don't send to self
		}

		// Skip if we've already sent this event to this peer
		if t.hasSentToPeer(nodeID, event.EventID) {
			continue
		}

		peerAddr := t.peerGossipAddr(member)
		go t.sendToPeer(peerAddr, nodeID, data, event.EventID)
	}
}

// acceptLoop accepts incoming TCP connections and processes events.
func (t *GossipTransport) acceptLoop(ctx context.Context) {
	defer close(t.doneCh)

	for {
		select {
		case <-ctx.Done():
			return
		case <-t.stopCh:
			return
		default:
		}

		conn, err := t.listener.Accept()
		if err != nil {
			t.mu.RLock()
			running := t.running
			t.mu.RUnlock()
			if !running {
				return // Shutdown
			}
			t.logger.Warn("gossip_transport: accept error", "error", err)
			continue
		}

		go t.handleConnection(ctx, conn)
	}
}

// handleConnection reads a cluster event from an incoming TCP connection,
// processes it through the gossip engine, and sends an ACK.
func (t *GossipTransport) handleConnection(ctx context.Context, conn net.Conn) {
	defer conn.Close()

	// Set read deadline to prevent hanging connections
	conn.SetReadDeadline(time.Now().Add(10 * time.Second))

	// Read the event from the connection
	reader := bufio.NewReaderSize(conn, 1024*1024) // 1MB buffer for large payloads
	data, err := reader.ReadBytes('\n')
	if err != nil {
		if err != io.EOF {
			t.logger.Debug("gossip_transport: read error", "remote", conn.RemoteAddr(), "error", err)
		}
		return
	}

	// Unmarshal the event
	var event models.ClusterEvent
	if err := json.Unmarshal(data, &event); err != nil {
		t.logger.Debug("gossip_transport: unmarshal error", "error", err)
		return
	}

	t.logger.Debug("gossip_transport: received event from peer",
		"event_id", event.EventID,
		"event_type", event.EventType,
		"node_id", event.NodeID,
		"remote", conn.RemoteAddr(),
	)

	// Send ACK back to sender
	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	ack := []byte("ACK\n")
	conn.Write(ack)

	// Process through the gossip engine's existing handler pipeline.
	// Call handleClusterEvent directly since the bus subscriber channel
	// is not actively consumed by the gossip engine.
	if t.gossip != nil {
		// Build a BusMessage in the same format the gossip engine expects
		payloadMap := map[string]json.RawMessage{"event": data}
		payload, _ := json.Marshal(payloadMap)
		body := &models.BusMessage{
			ID:        fmt.Sprintf("transport-recv-%d", time.Now().UnixNano()),
			Type:      models.MessageTypeEvent,
			Source:    "gossip_transport",
			Timestamp: time.Now().UTC(),
			Payload:   payload,
		}

		// Also publish to bus for any external subscribers
		if t.gossip.msgBus != nil {
			t.gossip.msgBus.Publish("cluster.event.received", body)
		}

		// Process through gossip handler (dedup, verify, persist, re-broadcast)
		t.gossip.handleClusterEvent(body)
	}
}

// sendToPeer sends event data to a single peer via TCP.
func (t *GossipTransport) sendToPeer(addr, nodeID string, data []byte, eventID string) {
	conn, err := net.DialTimeout("tcp", addr, 5*time.Second)
	if err != nil {
		t.logger.Debug("gossip_transport: failed to connect to peer",
			"peer", nodeID, "addr", addr, "error", err)
		// Queue for retry via the gossip engine
		if t.gossip != nil {
			var event models.ClusterEvent
			if json.Unmarshal(data, &event) == nil {
				t.gossip.QueueForRetry(&event)
			}
		}
		return
	}
	defer conn.Close()

	conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	_, err = conn.Write(append(data, '\n'))
	if err != nil {
		t.logger.Debug("gossip_transport: failed to send to peer",
			"peer", nodeID, "error", err)
		return
	}

	// Wait for ACK
	conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	reader := bufio.NewReader(conn)
	ack, err := reader.ReadString('\n')
	if err != nil {
		t.logger.Debug("gossip_transport: no ACK from peer",
			"peer", nodeID, "error", err)
		return
	}

	if ack == "ACK\n" {
		t.markSentToPeer(nodeID, eventID)
		t.logger.Debug("gossip_transport: event delivered",
			"peer", nodeID, "event_id", eventID)
	}
}

// getActivePeers returns currently active cluster members from the members provider.
func (t *GossipTransport) getActivePeers() (map[string]*Member, error) {
	if t.members == nil {
		return make(map[string]*Member), nil
	}
	return t.members.GetActiveMembers()
}

// peerGossipAddr constructs the gossip TCP address for a peer member.
// The gossip port is WireGuard port + 1 on the member's ClusterIP or Endpoint.
func (t *GossipTransport) peerGossipAddr(member *Member) string {
	port := gossipListenPort(t.cfg)

	// Prefer cluster IP (WireGuard mesh address)
	host := member.ClusterIP
	if host == "" {
		// Fall back to endpoint, strip port if present
		host = member.Endpoint
		if idx := lastColon(host); idx > 0 {
			host = host[:idx]
		}
	}

	return fmt.Sprintf("%s:%d", host, port)
}

// lastColon finds the last colon in a string (for splitting host:port).
func lastColon(s string) int {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == ':' {
			return i
		}
	}
	return -1
}

// hasSentToPeer checks if an event has already been sent to a peer.
func (t *GossipTransport) hasSentToPeer(nodeID, eventID string) bool {
	t.sentMu.RLock()
	defer t.sentMu.RUnlock()
	peerMap, ok := t.sentEvents[nodeID]
	if !ok {
		return false
	}
	_, exists := peerMap[eventID]
	return exists
}

// markSentToPeer records that an event was successfully sent to a peer.
func (t *GossipTransport) markSentToPeer(nodeID, eventID string) {
	t.sentMu.Lock()
	defer t.sentMu.Unlock()

	if _, ok := t.sentEvents[nodeID]; !ok {
		t.sentEvents[nodeID] = make(map[string]time.Time)
	}
	t.sentEvents[nodeID][eventID] = time.Now()

	// Prune oldest entries to prevent unbounded growth (keep last 1000 per peer).
	// Uses timestamps for LRU eviction instead of random deletion.
	if len(t.sentEvents[nodeID]) > 1000 {
		// Find and remove the 500 oldest entries.
		type entry struct {
			id string
			ts time.Time
		}
		entries := make([]entry, 0, len(t.sentEvents[nodeID]))
		for k, v := range t.sentEvents[nodeID] {
			entries = append(entries, entry{id: k, ts: v})
		}
		// Partial sort: find the 500 oldest.
		sort.Slice(entries, func(i, j int) bool {
			return entries[i].ts.Before(entries[j].ts)
		})
		toRemove := 500
		if toRemove > len(entries) {
			toRemove = len(entries)
		}
		for i := range toRemove {
			delete(t.sentEvents[nodeID], entries[i].id)
		}
	}
}

// IsRunning returns whether the transport is currently active.
func (t *GossipTransport) IsRunning() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}
