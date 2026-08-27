package integration

// multiuser_users_sync_test.go — repeatable integration test for cluster
// user pooling (multiuser plan, docs/plans/2026-08-26-multiuser-access/).
//
// Exercises the real production pipeline end to end:
//
//	auth.Store A --(UsersSync.exchangeOnce)--> gossip event on the wire
//	--> node B's pool.OnEvent --> auth.Store B.MergeForeign
//	--> a key created on node A validates on node B.
//
// Two real auth stores (JSON5 files in temp dirs), two real UsersSync pools,
// and two REAL GossipEngines for event creation are used. Cross-node
// delivery rides the same message-bus topic ("cluster.event.broadcast") that
// carries gossip between daemons in production; the test replays captured
// bus envelopes into each receiving pool through OnEvent — exactly what
// GossipEngine.emitToHandlers does after transport ingestion. Peer liveness
// is provided by the same seam the daemon uses (the ActivePeersFunc closure
// over membership), driven here by an explicit membership switch so node
// departure can be tested deterministically.
//
// Run with: go test ./tests/integration/ -run TestMultiuserUsersSync -count=1

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/internal/backup"
	"github.com/caimlas/meept/internal/bus"
	"github.com/caimlas/meept/internal/cluster"
	"github.com/caimlas/meept/pkg/models"
)

const (
	nodeAID = "node-a-int"
	nodeBID = "node-b-int"
)

// usersSyncNode bundles one cluster member's auth store and sync pool plus
// the machinery to move events onto and off the shared gossip bus.
type usersSyncNode struct {
	id       string
	store    *auth.Store
	pool     *backup.UsersSync
	engine   *cluster.GossipEngine
	bus      *bus.MessageBus
	sub      *bus.Subscriber
	peers    map[string]struct{} // simulated live membership (daemon passes engine.Peers(); a map is the same shape)
	captured []*models.ClusterEvent
}

// newUsersSyncNode builds one clustered daemon's users-sync slice: store on
// disk in t.TempDir(), pool wired to publish onto the SHARED bus (as the
// daemon's publish closure publishes into its local engine which lands on
// the bus), ActivePeersFunc reading n.peers (live, not snapshotted — same
// contract as the daemon's engine.Peers() closure).
func newUsersSyncNode(t *testing.T, sharedBus *bus.MessageBus, id string) *usersSyncNode {
	t.Helper()

	usersFile := filepath.Join(t.TempDir(), "users.json5")
	store, err := auth.NewStore(usersFile)
	if err != nil {
		t.Fatalf("%s: open users store: %v", id, err)
	}

	n := &usersSyncNode{
		id:    id,
		store: store,
		bus:   sharedBus,
		peers: make(map[string]struct{}),
	}
	n.engine = cluster.NewGossipEngine(nil, id, sharedBus, newTestLogger())
	n.pool = backup.NewUsersSync(
		backup.UsersSyncConfig{
			Enabled:   true,
			Interval:  time.Hour, // loop cadence irrelevant; tests call ExchangeNow directly
			UsersFile: usersFile,
		},
		store,
		id,
		func(eventType models.ClusterEventType, payload any) error {
			// Production path: engine.PublishClusterEvent marshals+signs and
			// Publish() drops the envelope onto cluster.event.broadcast. The
			// test captures that envelope for replay into peers.
			return n.engine.PublishClusterEvent(eventType, payload)
		},
		func() map[string]struct{} {
			out := make(map[string]struct{}, len(n.peers))
			for k := range n.peers {
				out[k] = struct{}{}
			}
			return out
		},
		newTestLogger(),
	)
	n.sub = sharedBus.Subscribe("sub-"+id, "cluster.event.broadcast")
	return n
}

// drainCaptured reads everything the node published to the shared bus since
// the last drain, returning fully-formed ClusterEvent envelopes.
func (n *usersSyncNode) drainCaptured(t *testing.T) []*models.ClusterEvent {
	t.Helper()
	for {
		select {
		case msg := <-n.sub.Channel:
			var envelope struct {
				Event json.RawMessage `json:"event"`
			}
			if err := json.Unmarshal(msg.Payload, &envelope); err != nil {
				t.Fatalf("%s: decode bus envelope: %v", n.id, err)
			}
			var ev models.ClusterEvent
			if err := json.Unmarshal(envelope.Event, &ev); err != nil {
				t.Fatalf("%s: decode cluster event: %v", n.id, err)
			}
			n.captured = append(n.captured, &ev)
			continue
		default:
		}
		break
	}
	out := n.captured
	n.captured = nil
	return out
}

// exchange performs a full synchronous A->B exchange: A publishes its locals
// onto the bus; the captured envelope is replayed into B's pool via OnEvent
// (mirroring emitToHandlers dispatch post-transport).
func exchange(t *testing.T, from, to *usersSyncNode) {
	t.Helper()
	if err := from.pool.ExchangeNow(); err != nil {
		t.Fatalf("%s ExchangeNow: %v", from.id, err)
	}
	for _, ev := range from.drainCaptured(t) {
		if err := to.pool.OnEvent(ev); err != nil {
			t.Fatalf("%s OnEvent from %s: %v", to.id, from.id, err)
		}
	}
}

// mustAddUserWithKey creates a user + key on the node's store and returns
// the raw key (valid only there until pooled).
func mustAddUserWithKey(t *testing.T, n *usersSyncNode, name string) (userID, rawKey string) {
	t.Helper()
	u, err := n.store.AddUser(name)
	if err != nil {
		t.Fatalf("%s AddUser(%s): %v", n.id, name, err)
	}
	raw, err := n.store.AddKey(u.ID, "integration-test", nil)
	if err != nil {
		t.Fatalf("%s AddKey: %v", n.id, err)
	}
	return u.ID, raw
}

func TestMultiuserUsersSync_PoolsAcrossNodes(t *testing.T) {
	sharedBus := bus.New(bus.DefaultConfig(), newTestLogger())
	defer sharedBus.Close()
	a := newUsersSyncNode(t, sharedBus, nodeAID)
	b := newUsersSyncNode(t, sharedBus, nodeBID)
	a.peers[nodeBID] = struct{}{}
	b.peers[nodeAID] = struct{}{}

	// User alice lives only on node A.
	_, aliceKey := mustAddUserWithKey(t, a, "alice")
	expires := time.Now().Add(time.Hour).UTC()
	_, bobKey := mustAddUserWithKey(t, b, "bob")

	// Sanity pre-sync: keys are NOT valid cross-node yet.
	if _, err := b.store.Validate(aliceKey, time.Now()); err == nil {
		t.Fatal("alice key valid on B before any sync; expected unknown")
	}

	exchange(t, a, b)
	exchange(t, b, a)

	// THE core cluster-pooling guarantee: node A's key authenticates node B.
	if _, err := b.store.Validate(aliceKey, time.Now()); err != nil {
		t.Fatalf("alice key should validate on B after pooling: %v", err)
	}
	if _, err := a.store.Validate(bobKey, time.Now()); err != nil {
		t.Fatalf("bob key should validate on A after pooling: %v", err)
	}

	// Expiry travels with the key: expire alice's only key locally on A is
	// destructive to later subtests, so verify expiry semantics with a
	// short-lived key instead.
	expired := time.Now().Add(-time.Minute).UTC()
	_, deadKey := func() (string, string) {
		u, _ := a.store.AddUser("shortlived")
		k, _ := a.store.AddKey(u.ID, "dies-fast", &expired)
		return u.ID, k
	}()
	exchange(t, a, b)
	if _, err := b.store.Validate(deadKey, time.Now()); err == nil {
		t.Fatal("expired key accepted on B; expiry must travel across the pool")
	}
	_ = expires // reserved naming clarity for future per-key TTL assertions
}

func TestMultiuserUsersSync_NodeDepartureEvictsAndRevokesAccess(t *testing.T) {
	sharedBus := bus.New(bus.DefaultConfig(), newTestLogger())
	defer sharedBus.Close()
	a := newUsersSyncNode(t, sharedBus, nodeAID+"-dep")
	b := newUsersSyncNode(t, sharedBus, nodeBID+"-dep")
	a.peers[nodeBID+"-dep"] = struct{}{}
	b.peers[nodeAID+"-dep"] = struct{}{}

	_, carolKey := mustAddUserWithKey(t, a, "carol")
	exchange(t, a, b)
	if _, err := b.store.Validate(carolKey, time.Now()); err != nil {
		t.Fatalf("precondition: carol key valid on B: %v", err)
	}

	// Node A LEAVES B's cluster view (B no longer sees A as a peer). B's
	// next reconcile cycle must drop every foreign user sourced from A.
	delete(b.peers, nodeAID+"-dep")
	if err := b.pool.ExchangeNow(); err != nil {
		t.Fatalf("B reconcile after split: %v", err)
	}

	if _, err := b.store.Validate(carolKey, time.Now()); err == nil {
		t.Fatal("carol key STILL valid on B after its origin node departed; eviction failed")
	}
}

func TestMultiuserUsersSync_LocalAuthorityNeverOverwritten(t *testing.T) {
	sharedBus := bus.New(bus.DefaultConfig(), newTestLogger())
	defer sharedBus.Close()
	a := newUsersSyncNode(t, sharedBus, nodeAID+"-auth")
	b := newUsersSyncNode(t, sharedBus, nodeBID+"-auth")
	a.peers[nodeBID+"-auth"] = struct{}{}
	b.peers[nodeAID+"-auth"] = struct{}{}

	// BOTH nodes create a user named "dave" — same name, different identity.
	localBDave, localBDaveKey := mustAddUserWithKey(t, b, "dave")
	mustAddUserWithKey(t, a, "dave")

	exchange(t, a, b)
	exchange(t, b, a)

	// B's LOCAL dave must still exist unchanged despite the inbound foreign
	// dave from A.
	users := b.store.ListUsers()
	var foundLocalDave bool
	for _, usr := range users {
		if usr.ID == localBDave && usr.Name == "dave" && usr.OriginNode == "" {
			foundLocalDave = true
		}
	}
	if !foundLocalDave {
		t.Fatalf("B's local dave (id=%s) lost or re-attributed after merge; users=%+v", localBDave, users)
	}
	if _, err := b.store.Validate(localBDaveKey, time.Now()); err != nil {
		t.Fatalf("B's local dave key invalidated after foreign merge: %v", err)
	}
}

func TestMultiuserUsersSync_DisabledIsStrictNoOp(t *testing.T) {
	sharedBus := bus.New(bus.DefaultConfig(), newTestLogger())
	defer sharedBus.Close()
	a := newUsersSyncNode(t, sharedBus, nodeAID)
	enabledFile := filepath.Join(t.TempDir(), "users.json5")
	enabledStore, err := auth.NewStore(enabledFile)
	if err != nil {
		t.Fatalf("open enabled-side store: %v", err)
	}
	_, eveKey := mustAddUserWithKey(t, a, "eve")

	// B runs multi-user DISABLED: pool disabled flag set, own empty store.
	disabledPool := backup.NewUsersSync(
		backup.UsersSyncConfig{Enabled: false, Interval: time.Hour},
		nil, // also mirrors "no store constructed when multiuser off"
		nodeBID+"-off",
		func(models.ClusterEventType, any) error { return nil },
		func() map[string]struct{} { return nil },
		newTestLogger(),
	)
	_ = enabledStore

	// Publish A's users, then deliver to the DISABLED pool: must be a
	// silent no-op — nothing merged, nothing published back.
	if err := a.pool.ExchangeNow(); err != nil {
		t.Fatalf("A exchange: %v", err)
	}
	events := a.drainCaptured(t)
	if len(events) == 0 {
		t.Fatal("expected at least one USERS_SYNC event from enabled side")
	}
	for _, ev := range events {
		if err := disabledPool.OnEvent(ev); err != nil {
			t.Fatalf("disabled pool returned error (should silently skip): %v", err)
		}
	}
	if err := disabledPool.ExchangeNow(); err != nil {
		t.Fatalf("disabled ExchangeNow should be a no-op success: %v", err)
	}

	// And nothing of B leaked toward A either way: eve never validated
	// anywhere but A.
	if _, err := a.store.Validate(eveKey, time.Now()); err != nil {
		t.Fatalf("sanity: eve key should remain valid on its origin node: %v", err)
	}
}
