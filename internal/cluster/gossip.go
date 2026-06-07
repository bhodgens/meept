package cluster

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"crypto/ed25519"
	"github.com/cespare/xxhash/v2"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"
)

// gossipEventTopic is the bus topic used for relaying cluster events between peers.
const gossipEventTopic = "cluster.event.broadcast"

// GossipEngine handles peer-to-peer event gossip within the cluster.
// It runs periodic heartbeats, publishes events to connected peers,
// and processes events received from other nodes via the message bus.
type GossipEngine struct {
	cfg       *Config
	localNode string
	msgBus    *bus.MessageBus
	logger    *slog.Logger
	signPriv  ed25519.PrivateKey

	mu        sync.RWMutex
	peers     map[string]*PeerInfo
	peerKeys  map[string]ed25519.PublicKey // nodeID -> signing public key
	running   bool
	sub       *bus.Subscriber
	stopCh    chan struct{}
	doneCh    chan struct{}
	eventID   string

	// Event persistence
	db *sql.DB

	// Deduplication cache: event hash -> event ID (approx max 10k entries)
	seenEvents    map[uint64]string
	seenEventsMu  sync.RWMutex
	maxSeenEvents int

	// Retry queue: event ID -> retry metadata
	retryQueue    map[string]*retryEntry
	retryQueueMu  sync.Mutex
}

type retryEntry struct {
	attempts  int
	lastRetry time.Time
	payload   []byte
	eventType models.ClusterEventType
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
		cfg:           cfg,
		localNode:     localNode,
		msgBus:        msgBus,
		logger:        logger,
		peers:         make(map[string]*PeerInfo),
		peerKeys:      make(map[string]ed25519.PublicKey),
		stopCh:        make(chan struct{}),
		doneCh:        make(chan struct{}),
		seenEvents:    make(map[uint64]string),
		retryQueue:    make(map[string]*retryEntry),
		maxSeenEvents: 10000,
	}
}

// WithSigningKey sets the ed25519 private key used for signing outgoing events.
func (g *GossipEngine) WithSigningKey(privKey ed25519.PrivateKey) *GossipEngine {
	g.signPriv = privKey
	return g
}

// WithDatabase sets the SQLite DB handle used for event persistence.
func (g *GossipEngine) WithDatabase(db *sql.DB) *GossipEngine {
	g.db = db
	return g
}

// SetPeerSigningKey records the signing public key for a known peer.
func (g *GossipEngine) SetPeerSigningKey(nodeID string, pubKey ed25519.PublicKey) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.peerKeys[nodeID] = pubKey
}

// PeerSigningKey returns the signing public key for a given node.
func (g *GossipEngine) PeerSigningKey(nodeID string) (ed25519.PublicKey, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	key, ok := g.peerKeys[nodeID]
	return key, ok
}

// engineDB returns the database handle if available.
func (g *GossipEngine) engineDB() *sql.DB {
	return g.db
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

	// Retry ticker for failed sends
	retryInterval := g.cfg.Gossip.HeartbeatInterval.ToTimeDuration() / 2
	if retryInterval < 2*time.Second {
		retryInterval = 2 * time.Second
	}
	retryTicker := time.NewTicker(retryInterval)

	// Heartbeat and event propagation goroutine
	go g.run(ctx, retryTicker)

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

// run is the main gossip loop that drives heartbeats, event processing, and retries.
func (g *GossipEngine) run(ctx context.Context, retryTicker *time.Ticker) {
	defer close(g.doneCh)
	defer retryTicker.Stop()

	heartbeatTicker := time.NewTicker(g.cfg.Gossip.HeartbeatInterval.ToTimeDuration())
	defer heartbeatTicker.Stop()

	g.logger.Info("gossip: engine started",
		"node_id", g.localNode,
		"heartbeat_interval", g.cfg.Gossip.HeartbeatInterval.ToTimeDuration(),
		"peer_timeout", g.cfg.Gossip.PeerTimeout.ToTimeDuration(),
	)

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-heartbeatTicker.C:
			g.sendHeartbeat()
			g.pruneStalePeers()
		case <-retryTicker.C:
			g.retryFailedEvents()
		case msg, ok := <-g.sub.Channel:
			if !ok {
				return
			}
			g.handleClusterEvent(msg)
		}
	}
}

// sendHeartbeat publishes a heartbeat event to the cluster.
func (g *GossipEngine) sendHeartbeat() {
	payload, _ := json.Marshal(map[string]any{
		"node_id":      g.localNode,
		"peer_count":   g.PeerCount(),
	})

	g.mu.RLock()
	key := g.signPriv
	g.mu.RUnlock()

	if key == nil {
		g.logger.Warn("gossip: cannot send heartbeat, no signing key")
		return
	}

	event := &models.ClusterEvent{
		EventID:     models.GenerateEventID(),
		NodeID:      g.localNode,
		EventType:   models.EventNodeHeartbeat,
		Timestamp:   time.Now().UTC(),
		VectorClock: incrementVC(g.localNode, nil),
		Payload:     payload,
	}

	if err := event.Sign(key); err != nil {
		g.logger.Error("gossip: heartbeat sign failed", "error", err)
		return
	}

	g.publishEvent(event)
}

// Publish creates, signs, and publishes a cluster event to the bus.
// This is the primary entry point for initiating cluster-wide events.
// It persists the event locally and attempts to forward to all known peers.
func (g *GossipEngine) Publish(event *models.ClusterEvent) error {
	if g.signPriv == nil {
		return fmt.Errorf("gossip: cannot publish, no signing key")
	}

	// Generate an event ID if not already set
	if event.EventID == "" {
		event.EventID = models.GenerateEventID()
	}

	// Set timestamp if not already set
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}

	// Set node ID if not already set
	if event.NodeID == "" {
		event.NodeID = g.localNode
	}

	// Increment the local vector clock
	if event.VectorClock == nil {
		event.VectorClock = make(map[string]int64)
	}
	event.VectorClock[g.localNode]++

	// Sign the event
	if err := event.Sign(g.signPriv); err != nil {
		return fmt.Errorf("gossip: event signing failed: %w", err)
	}

	// Persist locally before publishing
	if g.db != nil {
		if err := g.persistEvent(event); err != nil {
			// Non-fatal: continue publishing even if persistence fails
			g.logger.Warn("gossip: event persistence skipped (continuing)",
				"event_id", event.EventID, "error", err)
		}
	}

	// Publish to the bus (broadcasts on gossipEventTopic for peer delivery).
	g.publishEvent(event)

	g.logger.Info("gossip: published event",
		"event_id", event.EventID,
		"event_type", event.EventType,
		"node_id", event.NodeID,
	)

	return nil
}

// publishEvent serializes the cluster event and publishes it to the gossip broadcast topic.
func (g *GossipEngine) publishEvent(event *models.ClusterEvent) {
	payload, err := json.Marshal(event)
	if err != nil {
		g.logger.Error("gossip: failed to marshal cluster event",
			"event_id", event.EventID, "error", err)
		return
	}

	msg, err := models.NewBusMessage(
		models.MessageTypeEvent,
		"gossip",
		payload,
	)
	if err != nil {
		g.logger.Error("gossip: failed to create bus message", "error", err)
		return
	}

	if g.msgBus != nil {
		delivered := g.msgBus.Publish(gossipEventTopic, msg)
		g.logger.Debug("gossip: event broadcast",
			"event_id", event.EventID,
			"delivered", delivered,
			"peers", g.PeerCount(),
		)
	}
}

// persistEvent stores the event in the cluster_events SQLite table.
// Uses INSERT OR IGNORE on the event_id primary key for idempotency.
func (g *GossipEngine) persistEvent(event *models.ClusterEvent) error {
	db := g.engineDB()
	if db == nil {
		return fmt.Errorf("no database configured")
	}

	// Marshal vector clock as JSON
	vcJSON, err := json.Marshal(event.VectorClock)
	if err != nil {
		return fmt.Errorf("failed to marshal vector clock: %w", err)
	}

	query := `
		INSERT OR IGNORE INTO cluster_events
			(event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`
	_, err = db.Exec(query,
		event.EventID,
		event.NodeID,
		string(event.EventType),
		event.Timestamp.UnixNano(),
		string(vcJSON),
		event.Payload,
		event.Signature,
		time.Now().UnixNano(),
	)
	return err
}

// handleClusterEvent processes an incoming cluster event from the message bus.
// It verifies the signature, deduplicates, persists, and forwards to peers.
func (g *GossipEngine) handleClusterEvent(msg *models.BusMessage) {
	if msg == nil {
		return
	}

	// Extract the cluster event JSON from the bus message payload
	var rawEvent struct {
		EventID     string                        `json:"event_id"`
		NodeID      string                        `json:"node_id"`
		EventType   models.ClusterEventType       `json:"event_type"`
		Timestamp   int64                         `json:"timestamp"`
		VectorClock map[string]int64              `json:"vector_clock"`
		Payload     json.RawMessage               `json:"payload"`
		Signature   []byte                        `json:"signature"`
	}

	if err := json.Unmarshal(msg.Payload, &rawEvent); err != nil {
		g.logger.Warn("gossip: failed to unmarshal cluster event payload",
			"source", msg.Source, "error", err)
		return
	}

	// Build the ClusterEvent struct
	event := &models.ClusterEvent{
		EventID:     rawEvent.EventID,
		NodeID:      rawEvent.NodeID,
		EventType:   rawEvent.EventType,
		Timestamp:   time.Unix(0, rawEvent.Timestamp),
		VectorClock: rawEvent.VectorClock,
		Payload:     rawEvent.Payload,
		Signature:   rawEvent.Signature,
	}

	// Deduplication check
	if g.isDuplicate(event.EventID) {
		g.logger.Debug("gossip: duplicate event dropped", "event_id", event.EventID)
		return
	}
	g.markSeen(event.EventID)

	// Verify signature
	pubKey, known := g.PeerSigningKey(event.NodeID)
	if known {
		if !event.Verify(pubKey) {
			g.logger.Warn("gossip: event signature verification failed",
				"event_id", event.EventID,
				"node_id", event.NodeID,
				"event_type", event.EventType,
			)
			return
		}
	} else {
		// Unknown node - log but still process (the node's key may arrive later via NODE_JOIN event)
		g.logger.Info("gossip: event from unknown node (key not yet known), accepting",
			"event_id", event.EventID,
			"node_id", event.NodeID,
		)
	}

	// Persist to the cluster events table
	if g.db != nil {
		if err := g.persistEvent(event); err != nil {
			g.logger.Warn("gossip: event persistence failed (non-fatal)",
				"event_id", event.EventID, "error", err)
		}
	}

	// Forward to all peers via the gossip bus
	g.msgBroadcast(event)

	// Update peer heartbeat
	if event.NodeID != g.localNode {
		g.updatePeerLastSeen(event.NodeID)
	}

	g.logger.Debug("gossip: processed cluster event",
		"event_id", event.EventID,
		"event_type", event.EventType,
		"node_id", event.NodeID,
	)
}

// msgBroadcast forwards an event to the gossip broadcast topic.
// This is called after handleClusterEvent has already accepted the event,
// so other nodes in the mesh receive a copy.
func (g *GossipEngine) msgBroadcast(event *models.ClusterEvent) {
	payload, _ := json.Marshal(event)
	msg, err := models.NewBusMessage(models.MessageTypeEvent, "gossip-forward", payload)
	if err != nil {
		return
	}
	if g.msgBus != nil {
		g.msgBus.Publish(gossipEventTopic, msg)
	}
}

// isDuplicate checks if an event ID has already been seen, using a hash-based cache.
func (g *GossipEngine) isDuplicate(eventID string) bool {
	g.seenEventsMu.RLock()
	defer g.seenEventsMu.RUnlock()
	_, ok := g.seenEvents[eventHash(eventID)]
	return ok
}

// markSeen adds an event ID to the deduplication cache, trimming if over capacity.
func (g *GossipEngine) markSeen(eventID string) {
	hash := eventHash(eventID)

	g.seenEventsMu.Lock()
	defer g.seenEventsMu.Unlock()

	if len(g.seenEvents) >= g.maxSeenEvents {
		g.seenEvents = trimSeenMap(g.seenEvents)
	}
	g.seenEvents[hash] = eventID
}

// trimSeenMap keeps approximately the most recent 2/3 of entries from the cache.
func trimSeenMap(m map[uint64]string) map[uint64]string {
	keep := len(m) * 2 / 3
	if keep < 100 {
		keep = 100
	}
	result := make(map[uint64]string, keep)
	for h, id := range m {
		if len(result) < keep {
			result[h] = id
		}
	}
	return result
}

// eventHash computes a fast hash of an event ID string for the dedup cache.
func eventHash(eventID string) uint64 {
	return xxhash.Sum64String(eventID)
}

// updatePeerLastSeen updates the LastSeen timestamp for a peer node.
func (g *GossipEngine) updatePeerLastSeen(nodeID string) {
	g.mu.Lock()
	defer g.mu.Unlock()

	if peer, ok := g.peers[nodeID]; ok {
		peer.LastSeen = time.Now().UTC()
	}
}

// pruneStalePeers removes peers that haven't been seen within the timeout.
func (g *GossipEngine) pruneStalePeers() {
	g.mu.Lock()
	defer g.mu.Unlock()

	timeout := g.cfg.Gossip.PeerTimeout.ToTimeDuration()
	for nodeID, peer := range g.peers {
		if time.Since(peer.LastSeen) > timeout {
			g.logger.Info("gossip: peer timeout", "node_id", nodeID)
			delete(g.peers, nodeID)
		}
	}
}

// retryFailedEvents checks the retry queue and re-publishes events that have not
// been confirmed via the gossip mesh.
func (g *GossipEngine) retryFailedEvents() {
	maxRetries := g.cfg.Gossip.MaxRetryAttempts
	cooldown := time.Second // Minimum time between retries

	g.retryQueueMu.Lock()

	for eventID, entry := range g.retryQueue {
		if entry.attempts >= maxRetries {
			g.logger.Warn("gossip: max retries exceeded for event, dropping",
				"event_id", eventID, "attempts", entry.attempts)
			delete(g.retryQueue, eventID)
			continue
		}

		if time.Since(entry.lastRetry) < cooldown {
			continue
		}

		g.logger.Info("gossip: retrying event",
			"event_id", eventID,
			"attempt", entry.attempts+1,
			"max", maxRetries,
		)

		// Re-broadcast the raw payload to trigger handleClusterEvent on peers
		msg, err := models.NewBusMessage(
			models.MessageTypeEvent,
			"gossip-retry",
			entry.payload,
		)
		if err != nil {
			g.logger.Error("gossip: retry failed to create message",
				"event_id", eventID, "error", err)
			delete(g.retryQueue, eventID)
			continue
		}
		if g.msgBus != nil {
			g.msgBus.Publish(gossipEventTopic, msg)
		}

		entry.attempts++
		entry.lastRetry = time.Now()
		g.retryQueue[eventID] = entry
	}

	g.retryQueueMu.Unlock()
}

// QueueForRetry adds an event to the retry queue in case it was not properly delivered.
func (g *GossipEngine) QueueForRetry(event *models.ClusterEvent) {
	payload, _ := json.Marshal(event)

	g.retryQueueMu.Lock()
	defer g.retryQueueMu.Unlock()

	if _, exists := g.retryQueue[event.EventID]; !exists {
		g.retryQueue[event.EventID] = &retryEntry{
			attempts:  0,
			lastRetry: time.Now(),
			payload:   payload,
			eventType: event.EventType,
		}
	}
}

// incrementVC increments the vector clock for the given node and returns a copy.
// Accepts an optional existing clock to increment; returns nil-protected result.
func incrementVC(nodeID string, vc map[string]int64) map[string]int64 {
	if vc == nil {
		vc = make(map[string]int64)
	}
	vc[nodeID]++
	// Return a copy
	result := make(map[string]int64, len(vc))
	for k, v := range vc {
		result[k] = v
	}
	return result
}
