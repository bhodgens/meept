package backup

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/pkg/models"

	"github.com/tailscale/hujson"
)

// EventTypeUsersSync is the gossip event type carrying cluster user-pooling
// payloads. It is declared locally instead of appended to pkg/models so this
// leaf keeps its footprint out of shared model files; gossip recipients that
// predate multi-user pooling hit their existing default branch (silently
// ignore unknown event types), making the addition backward compatible on
// mixed-version clusters.
const EventTypeUsersSync = models.ClusterEventType("USERS_SYNC")

// DefaultUsersSyncInterval is the exchange cadence used when
// UsersSyncConfig.Interval does not specify one.
const DefaultUsersSyncInterval = 30 * time.Second

// UsersSyncPayload is the wire format for cluster user pooling. Each
// exchange carries the sender's LOCAL users only (cached foreign users are
// never relayed); the receiving pool re-stamps every entry with the
// transporting event's NodeID before merging, satisfying the leaf-03
// contract "OriginNode set to the SENDING node's id".
//
// When multi-user is disabled the pool neither publishes nor accepts this
// payload, so the users field stays omitted on the wire.
type UsersSyncPayload struct {
	SenderNodeID string      `json:"sender_node_id"`
	Users        []auth.User `json:"users,omitempty"`
}

// PublishFunc publishes a signed cluster event with a typed payload. It is
// satisfied by (*cluster.GossipEngine).PublishClusterEvent without importing
// internal/cluster here (structural dependency injection keeps internal/backup
// free of a hard cluster dependency).
type PublishFunc func(eventType models.ClusterEventType, payload any) error

// ActivePeersFunc returns the set of node IDs currently considered active
// cluster peers. Supplied by the daemon from cluster.GossipEngine.Peers(),
// it provides the LIVE peer set required by auth.Store.MergeForeign —
// evaluated fresh at each merge rather than snapshotted at construction.
type ActivePeersFunc func() map[string]struct{}

// UsersSourceFunc snapshots the local users eligible for export. The default
// implementation reads the local users store file.
type UsersSourceFunc func() ([]auth.User, error)

// UsersSyncConfig configures a UsersSync pool.
type UsersSyncConfig struct {
	// Enabled gates the entire exchange: when false nothing is published,
	// accepted, or reconciled (multi-user off must be a strict no-op).
	Enabled bool
	// Interval is the exchange cadence; <=0 falls back to
	// DefaultUsersSyncInterval.
	Interval time.Duration
	// UsersFile is the path of the local users store (the same file
	// auth.Store persists to). Empty disables publishing.
	UsersFile string
}

// UsersSync pools cluster users over the gossip event channel. Senders
// periodically broadcast their local users stamped for transit; receivers
// merge inbound batches through auth.Store.MergeForeign using the LIVE peer
// set, so foreign users vanish automatically once their origin node drops
// out of the cluster. Local users are always authoritative because
// MergeForeign never overwrites entries with an empty OriginNode.
//
// Concurrency: all dependencies are externally synchronized or immutable;
// UsersSync holds no locks of its own.
type UsersSync struct {
	cfg       UsersSyncConfig
	store     *auth.Store
	localNode string
	publish   PublishFunc
	active    ActivePeersFunc
	source    UsersSourceFunc
	logger    *slog.Logger
}

// NewUsersSync creates a cluster user-pooling exchange. Any of store,
// publish, or active being nil disables the corresponding direction; a nil
// logger falls back to a stderr text logger. Reading cfg.UsersFile backs the
// default local-users snapshot.
func NewUsersSync(cfg UsersSyncConfig, store *auth.Store, localNode string, publish PublishFunc, active ActivePeersFunc, logger *slog.Logger) *UsersSync {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stderr, nil))
	}
	return &UsersSync{
		cfg:       cfg,
		store:     store,
		localNode: localNode,
		publish:   publish,
		active:    active,
		source:    usersFileSource(cfg.UsersFile),
		logger:    logger.With("component", "users-sync"),
	}
}

// OnEvent processes an inbound USERS_SYNC gossip event, making UsersSync a
// structural match for cluster.GossipHandler (register with
// GossipEngine.RegisterHandler). Events of other types, self-originated
// events, and unattributed nodes are skipped silently; malformed payloads
// surface an error that the gossip pipeline logs without stopping. Merge
// failures are likewise reported, never fatal: the next exchange cycle
// retries.
func (u *UsersSync) OnEvent(event *models.ClusterEvent) error {
	if !u.cfg.Enabled || u.store == nil || event == nil || event.EventType != EventTypeUsersSync {
		return nil
	}
	if event.NodeID == "" || event.NodeID == u.localNode {
		return nil
	}

	var payload UsersSyncPayload
	if err := json.Unmarshal(event.Payload, &payload); err != nil {
		return fmt.Errorf("decode users sync payload from %s: %w", event.NodeID, err)
	}
	if payload.SenderNodeID != "" && payload.SenderNodeID != event.NodeID {
		return fmt.Errorf("users sync payload claims sender %q but arrived via node %q", payload.SenderNodeID, event.NodeID)
	}

	users := make([]auth.User, len(payload.Users))
	for i, user := range payload.Users {
		user.OriginNode = event.NodeID
		users[i] = user
	}

	// Pass the LIVE peer set untouched: auth.Store.MergeForeign owns the
	// admission rule (accept only while OriginNode is clustered), so the
	// pool never widens liveness on a sender's behalf.
	if err := u.store.MergeForeign(users, u.activePeers()); err != nil {
		return fmt.Errorf("merge foreign users from %s: %w", event.NodeID, err)
	}
	u.logger.Debug("merged cluster users", "node", event.NodeID, "count", len(users))
	return nil
}

// Run executes the exchange loop until ctx is cancelled: one immediate
// exchange, then one per interval. Pull-side eviction (foreign users whose
// origin left the cluster) rides the same cadence via reconcile.
func (u *UsersSync) Run(ctx context.Context) {
	interval := u.cfg.Interval
	if interval <= 0 {
		interval = DefaultUsersSyncInterval
	}
	u.logger.Info("cluster users sync starting",
		"enabled", u.cfg.Enabled,
		"interval", interval.String())

	u.exchangeOnce()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			u.logger.Info("cluster users sync stopping")
			return
		case <-ticker.C:
			u.exchangeOnce()
		}
	}
}

// ExchangeNow performs one full exchange synchronously — publish local
// users plus reconcile of the foreign cache against the live peer set — for
// the RPC control plane (sync.pull). Disabled pools are silent no-ops.
func (u *UsersSync) ExchangeNow() error {
	if !u.cfg.Enabled {
		return nil
	}
	u.exchangeOnce()
	return nil
}

// exchangeOnce guards the cycle against partially wired pools so Run stays
// safe whenever the pool exists.
func (u *UsersSync) exchangeOnce() {
	if !u.cfg.Enabled {
		return
	}
	u.publishLocal()
	u.reconcile()
}

// publishLocal broadcasts this node's local users stamped with the sending
// node id.
func (u *UsersSync) publishLocal() {
	if u.publish == nil || u.store == nil || u.source == nil {
		return
	}
	users, err := u.localUsers()
	if err != nil {
		u.logger.Warn("failed to read users for exchange", "error", err)
		return
	}
	if len(users) == 0 {
		return // nothing local to share; publish nothing
	}
	payload := UsersSyncPayload{
		SenderNodeID: u.localNode,
		Users:        users,
	}
	if err := u.publish(EventTypeUsersSync, payload); err != nil {
		u.logger.Warn("failed to publish cluster users", "error", err)
	}
}

// localUsers snapshots the local (empty-OriginNode) portion of the users
// store, stamping the outbound OriginNode with this node's id per the leaf
// contract that sent users carry the SENDING node's id.
func (u *UsersSync) localUsers() ([]auth.User, error) {
	all, err := u.source()
	if err != nil {
		return nil, err
	}
	out := make([]auth.User, 0, len(all))
	for _, user := range all {
		if user.OriginNode == "" {
			user.OriginNode = u.localNode
			out = append(out, user)
		}
	}
	return out, nil
}

// reconcile prunes foreign users whose origin node dropped out of the live
// peer set while leaving local users and users of still-active peers alone.
// Passing no replacement users makes auth.Store.MergeForeign act purely
// subtractively toward stale origins; merge errors are logged, never fatal,
// and the next cycle retries.
func (u *UsersSync) reconcile() {
	if u.store == nil || u.active == nil {
		return
	}
	if err := u.store.MergeForeign(nil, u.active()); err != nil {
		u.logger.Warn("failed to reconcile cluster users", "error", err)
	}
}

// activePeers returns a copy of the live peer set. MergeForeign mutates
// nothing it is given, but copying keeps pool boundaries clean regardless
// of store internals.
func (u *UsersSync) activePeers() map[string]struct{} {
	set := map[string]struct{}{}
	if u.active != nil {
		for id := range u.active() {
			set[id] = struct{}{}
		}
	}
	return set
}

// usersFileSource returns a UsersSourceFunc that parses the local users
// store file on each call. The format mirrors auth.Store's persisted shape
// exactly (JSON5 allowed, {"users": [...]} wrapper): saveStore replaces the
// file atomically via rename and every mutation persists before returning,
// so reads observe either the previous or the new complete state — a
// deliberate duplication of auth's ~15-line decode path, forced by the
// no-changes-to-internal/auth constraint (it exposes no bulk reader).
func usersFileSource(path string) UsersSourceFunc {
	return func() ([]auth.User, error) {
		if path == "" {
			return nil, nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			if os.IsNotExist(err) {
				return nil, nil // fresh install / disabled: nothing to share
			}
			return nil, fmt.Errorf("read users store %s: %w", path, err)
		}
		stdJSON, err := hujson.Standardize(data)
		if err != nil {
			return nil, fmt.Errorf("parse users store %s: %w", path, err)
		}
		var wrapper struct {
			Users []auth.User `json:"users"`
		}
		if err := json.Unmarshal(stdJSON, &wrapper); err != nil {
			return nil, fmt.Errorf("decode users store %s: %w", path, err)
		}
		return wrapper.Users, nil
	}
}
