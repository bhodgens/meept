package cluster

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/pkg/models"

	"github.com/cespare/xxhash/v2"
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

	// Signing key for this node (ed25519)
	signingPriv ed25519.PrivateKey
	signingPub  map[string]ed25519.PublicKey // indexed by peer nodeID

	// Database handle for event persistence
	db *sql.DB

	// TCP transport for peer-to-peer event delivery
	transport       *GossipTransport
	membersProvider MembersProvider

	// Retry queue for failed broadcasts
	retryQueue chan *models.ClusterEvent

	// Deduplication cache (xxhash of event_id -> expiration time)
	dedupCache map[string]time.Time
	dedupMu    sync.RWMutex

	// Vector clock (local state)
	vectorClock map[string]int64
	vcMu        sync.RWMutex
}

// PeerInfo describes a connected cluster peer.
type PeerInfo struct {
	NodeID   string
	Endpoint string
	JoinedAt time.Time
	LastSeen time.Time
	Status   string // "active" | "inactive" | "syncing"
}

// GossipOption configures a GossipEngine via functional options.
type GossipOption func(*GossipEngine)

// NewGossipEngine creates a new gossip engine.
func NewGossipEngine(cfg *Config, localNode string, msgBus *bus.MessageBus, logger *slog.Logger, opts ...GossipOption) *GossipEngine {
	g := &GossipEngine{
		cfg:         cfg,
		localNode:   localNode,
		msgBus:      msgBus,
		logger:      logger,
		peers:       make(map[string]*PeerInfo),
		stopCh:      make(chan struct{}),
		doneCh:      make(chan struct{}),
		signingPub:  make(map[string]ed25519.PublicKey),
		retryQueue:  make(chan *models.ClusterEvent, 64),
		dedupCache:  make(map[string]time.Time),
		vectorClock: make(map[string]int64),
	}

	// Generate an ed25519 signing key pair if signing is required
	if g.signingPriv == nil && cfg.Security.RequireNodeSignatures {
		_, privBytes, _ := ed25519.GenerateKey(rand.Reader)
		g.signingPriv = privBytes
	}

	for _, opt := range opts {
		opt(g)
	}

	// Register our own public key under our nodeID if we have a signing key
	if g.signingPriv != nil {
		g.signingPub[localNode] = g.signingPriv.Public().(ed25519.PublicKey)
	}

	return g
}

// WithDatabase sets the database handle on the gossip engine.
func WithDatabase(db *sql.DB) GossipOption {
	return func(g *GossipEngine) {
		g.db = db
	}
}

// WithSigningKey sets the ed25519 signing key pair on the gossip engine.
func WithSigningKey(priv ed25519.PrivateKey, pub ed25519.PublicKey) GossipOption {
	return func(g *GossipEngine) {
		g.signingPriv = priv
		if pub != nil {
			g.signingPub[pubKeyID(pub)] = pub
		}
	}
}

// pubKeyID returns a short hex identifier for a public key.
func pubKeyID(key ed25519.PublicKey) string {
	return fmt.Sprintf("%08x", xxhash.Sum64(key))
}

// WithMembersProvider sets the members provider for TCP transport peer resolution.
func WithMembersProvider(mp MembersProvider) GossipOption {
	return func(g *GossipEngine) {
		g.membersProvider = mp
	}
}

// SigningPrivate returns the gossip engine's ed25519 private signing key, or nil if none was set.
func (g *GossipEngine) SigningPrivate() ed25519.PrivateKey {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return g.signingPriv
}

// PublicSigningKey returns the gossip engine's signing public key, or nil if none was set.
func (g *GossipEngine) PublicSigningKey() ed25519.PublicKey {
	if g.signingPriv == nil {
		return nil
	}
	return g.signingPriv.Public().(ed25519.PublicKey)
}

// SetPeerSigningKey records the ed25519 public key for a peer node.
// The key is indexed by nodeID for signature verification on receipt.
func (g *GossipEngine) SetPeerSigningKey(nodeID string, pubKey ed25519.PublicKey) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.signingPub[nodeID] = pubKey
	_ = pubKeyID(pubKey)
}

// PeerSigningKey retrieves the ed25519 public key for a peer node.
// Returns the key and true if found, or nil and false otherwise.
func (g *GossipEngine) PeerSigningKey(nodeID string) (ed25519.PublicKey, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	key, ok := g.signingPub[nodeID]
	return key, ok
}

// incrementVC increments the vector clock entry for the given nodeID.
func (g *GossipEngine) incrementVC(nodeID string) {
	g.vcMu.Lock()
	defer g.vcMu.Unlock()
	g.vectorClock[nodeID]++
}

// getVC returns a snapshot of the current vector clock.
func (g *GossipEngine) getVC() map[string]int64 {
	g.vcMu.RLock()
	defer g.vcMu.RUnlock()
	cp := make(map[string]int64, len(g.vectorClock))
	for k, v := range g.vectorClock {
		cp[k] = v
	}
	return cp
}

// persistEvent writes a cluster event into the cluster_events table using INSERT OR IGNORE.
func (g *GossipEngine) persistEvent(event *models.ClusterEvent) {
	if g.db == nil {
		return
	}

	sigB64 := ""
	if event.Signature != nil {
		sigB64 = string(event.Signature)
	}

	vcJSON, _ := json.Marshal(event.VectorClock)
	payloadJSON := []byte(event.Payload)
	receivedAt := time.Now().UnixNano()

	_, err := g.db.Exec(`
		INSERT OR IGNORE INTO cluster_events
			(event_id, node_id, event_type, timestamp, vector_clock, payload, signature, received_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
	`,
		event.EventID,
		event.NodeID,
		string(event.EventType),
		event.Timestamp.UnixNano(),
		string(vcJSON),
		string(payloadJSON),
		sigB64,
		receivedAt,
	)
	if err != nil {
		g.logger.Error("gossip: persist event failed", "event_id", event.EventID, "err", err)
	}
}

// RecordLocalVC increments the vector clock for the local node and returns a map
// suitable for embedding into a ClusterEvent's VectorClock field.
func (g *GossipEngine) RecordLocalVC() map[string]int64 {
	g.incrementVC(g.localNode)
	return g.getVC()
}

// Publish creates, signs, persists, and broadcasts a cluster event.
// If any fields are missing they are auto-generated. Events are persisted first,
// then published to the message bus topic "cluster.event.broadcast" for peer re-gossip.
func (g *GossipEngine) Publish(event *models.ClusterEvent) {
	if event == nil {
		return
	}

	// Auto-generate required fields if missing
	if event.EventID == "" {
		event.EventID = models.GenerateEventID()
	}
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now().UTC()
	}
	if event.NodeID == "" {
		event.NodeID = g.localNode
	}

	// Increment local vector clock and attach
	vc := g.RecordLocalVC()
	event.VectorClock = make(map[string]int64, len(vc))
	for k, v := range vc {
		event.VectorClock[k] = v
	}

	// Sign the event
	priv := g.SigningPrivate()
	if priv != nil {
		_ = event.Sign(priv)
	}

	// Persist to database before broadcasting
	if g.db != nil {
		g.persistEvent(event)
	}

	// Broadcast to bus so handleClusterEvent can re-gossip to peers
	if g.msgBus != nil {
		eventPayload, _ := json.Marshal(event)
		busPayload, _ := json.Marshal(map[string]json.RawMessage{"event": eventPayload})
		body := &models.BusMessage{
			ID:        fmt.Sprintf("gossip-pub-%d", time.Now().UnixNano()),
			Type:      models.MessageTypeEvent,
			Source:    "gossip",
			Timestamp: time.Now().UTC(),
			Payload:   busPayload,
		}
		g.msgBus.Publish("cluster.event.broadcast", body)
	}

	// Send to remote peers via TCP transport
	if g.transport != nil {
		g.transport.SendEvent(event)
	}

	g.logger.Debug("gossip: published event",
		"event_id", event.EventID,
		"event_type", event.EventType,
		"node_id", event.NodeID,
	)
}

// handleClusterEvent processes an incoming cluster event from the message bus.
// It deduplicates via xxhash, verifies ed25519 signatures, persists, re-broadcasts,
// and updates peer LastSeen timestamps.
func (g *GossipEngine) handleClusterEvent(msg *models.BusMessage) {
	if msg == nil {
		return
	}

	// Unmarshal event payload
	var rawEvent map[string]json.RawMessage
	if err := json.Unmarshal(msg.Payload, &rawEvent); err != nil {
		g.logger.Debug("gossip: handleClusterEvent bad payload", "err", err)
		return
	}

	eventBytes, ok := rawEvent["event"]
	if !ok {
		return
	}

	var event models.ClusterEvent
	if err := json.Unmarshal(eventBytes, &event); err != nil {
		g.logger.Debug("gossip: handleClusterEvent unmarshal fail", "err", err)
		return
	}

	// Deduplicate via xxhash of event_id
	checksum := fmt.Sprintf("%d", xxhash.Sum64String(event.EventID))
	g.dedupMu.RLock()
	if expiry, seen := g.dedupCache[checksum]; seen {
		if time.Now().Before(expiry) {
			g.dedupMu.RUnlock()
			return // duplicate, skip
		}
		g.dedupMu.RUnlock()
	} else {
		g.dedupMu.RUnlock()
	}
	g.dedupMu.Lock()
	g.dedupCache[checksum] = time.Now().Add(g.cfg.Gossip.EventRetention)
	g.dedupMu.Unlock()

	// Verify signature if node signature requirement is enabled
	if g.cfg.Security.RequireNodeSignatures && len(event.Signature) > 0 {
		pubKey, found := g.PeerSigningKey(event.NodeID)
		if !found {
			g.logger.Warn("gossip: no signing key for event sender", "node_id", event.NodeID)
			return
		}
		if !event.Verify(pubKey) {
			g.logger.Warn("gossip: event signature verification failed", "event_id", event.EventID, "node_id", event.NodeID)
			return
		}
	}

	// Persist (INSERT OR IGNORE prevents duplicates)
	if g.db != nil {
		g.persistEvent(&event)
	}

	// Update peer LastSeen timestamp
	g.mu.Lock()
	if peer, exists := g.peers[event.NodeID]; exists {
		peer.LastSeen = time.Now().UTC()
	}
	g.mu.Unlock()

	// Re-broadcast to peers via bus
	if g.msgBus != nil {
		eventPayload, _ := json.Marshal(event)
		busPayload, _ := json.Marshal(map[string]json.RawMessage{"event": eventPayload})
		body := &models.BusMessage{
			ID:        fmt.Sprintf("gossip-%s-%d", g.localNode, time.Now().UnixNano()),
			Type:      models.MessageTypeEvent,
			Source:    g.localNode,
			Timestamp: time.Now().UTC(),
			Payload:   busPayload,
		}
		g.msgBus.Publish("cluster.event.broadcast", body)
	}

	g.logger.Debug("gossip: processed cluster event",
		"event_id", event.EventID,
		"event_type", event.EventType,
		"node_id", event.NodeID,
	)
}

// QueueForRetry adds an event to the retry queue for later redelivery.
func (g *GossipEngine) QueueForRetry(event *models.ClusterEvent) {
	if event == nil {
		return
	}
	select {
	case g.retryQueue <- event:
		g.logger.Debug("gossip: queued event for retry", "event_id", event.EventID)
	default:
		g.logger.Warn("gossip: retry queue full, dropping event", "event_id", event.EventID)
	}
}

// cleanupDedupCache removes expired entries from the deduplication cache.
func (g *GossipEngine) cleanupDedupCache() {
	now := time.Now()
	g.dedupMu.Lock()
	defer g.dedupMu.Unlock()
	for checksum, expiry := range g.dedupCache {
		if now.After(expiry) {
			delete(g.dedupCache, checksum)
		}
	}
}

// startRetryLoop launches a background goroutine that retries failed event broadcasts
// up to maxRetryAttempts times.
func (g *GossipEngine) startRetryLoop(ctx context.Context) {
	go g.retryLoop(ctx)
}

// retryLoop processes the retry queue, re-publishing events until max attempts.
func (g *GossipEngine) retryLoop(ctx context.Context) {
	ticker := time.NewTicker(5 * time.Second)
	defer ticker.Stop()

	maxAttempts := g.cfg.Gossip.MaxRetryAttempts
	if maxAttempts <= 0 {
		maxAttempts = 3
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-g.stopCh:
			return
		case <-ticker.C:
			g.cleanupDedupCache()
			// Drain retry queue at most once per tick
			select {
			case event := <-g.retryQueue:
				g.logger.Info("gossip: retrying event broadcast",
					"event_id", event.EventID, "attempt", 1)
				// Re-broadcast via existing Publish path
				g.Publish(event)
			default:
				// nothing to retry
			}
		}
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

	// Start background retry loop
	g.startRetryLoop(ctx)

	// Start TCP transport for peer-to-peer event delivery
	if g.membersProvider != nil {
		g.transport = NewGossipTransport(g.cfg, g.localNode, g, g.membersProvider, g.logger)
		if err := g.transport.Start(ctx); err != nil {
			g.logger.Warn("gossip: TCP transport start failed (events will be local-only)", "error", err)
		}
	} else {
		g.logger.Info("gossip: no members provider, TCP transport disabled")
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

	// Stop TCP transport
	if g.transport != nil {
		if err := g.transport.Stop(); err != nil {
			g.logger.Warn("gossip: transport stop error", "error", err)
		}
	}

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
