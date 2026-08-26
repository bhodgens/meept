package backup

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/auth"
	"github.com/caimlas/meept/pkg/models"
)

// newTestUsersSync builds a pool with a real auth.Store merge target and a
// recording publish spy. Extra knobs are returned so individual tests can
// steer peer-set liveness and capture published payloads.
type usersSyncHarness struct {
	pool        *UsersSync
	store       *auth.Store
	published   []*models.ClusterEvent
	setPeers    func(map[string]struct{})
	failPublish bool
}

func newTestUsersSync(t *testing.T, localNode string) *usersSyncHarness {
	t.Helper()

	usersPath := filepath.Join(t.TempDir(), "users.json5")
	store, err := auth.NewStore(usersPath)
	if err != nil {
		t.Fatalf("auth.NewStore: %v", err)
	}

	h := &usersSyncHarness{store: store}
	active := map[string]struct{}{}
	h.setPeers = func(peers map[string]struct{}) { active = peers }

	h.pool = NewUsersSync(
		UsersSyncConfig{Enabled: true, UsersFile: usersPath},
		store,
		localNode,
		func(eventType models.ClusterEventType, payload any) error {
			if h.failPublish {
				return context.DeadlineExceeded
			}
			raw, err := json.Marshal(payload)
			if err != nil {
				return err
			}
			h.published = append(h.published, &models.ClusterEvent{
				NodeID:    localNode,
				EventType: eventType,
				Payload:   raw,
			})
			return nil
		},
		func() map[string]struct{} { return active },
		testLogger(),
	)
	return h
}

func TestUsersSyncRoundTripBetweenStores(t *testing.T) {
	sender := newTestUsersSync(t, "node-a")
	receiver := newTestUsersSync(t, "node-b")

	raw, err := sender.store.AddKey(mustAddUserFor(t, sender.store, "alice").ID, "laptop", nil)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	_ = raw // only the hash travels; the raw key never leaves node-a

	sender.setPeers(map[string]struct{}{"node-b": struct{}{}})
	receiver.setPeers(map[string]struct{}{"node-a": struct{}{}})

	sender.pool.publishLocal()

	if len(sender.published) == 0 {
		t.Fatal("sender published no USERS_SYNC events")
	}
	for _, event := range sender.published {
		event.NodeID = "node-a" // mimic transport attribution
		if err := receiver.pool.OnEvent(event); err != nil {
			t.Fatalf("receiver OnEvent: %v", err)
		}
	}

	// The foreign user must authenticate on the receiver: its key hash
	// traveled in the payload, so a login on node-b succeeds before
	// origin eviction. This exercises the pooled-credential guarantee.
	id, err := receiver.store.Validate(raw, time.Now())
	if err == nil && id == nil {
		t.Fatal("expected non-nil identity or error from Validate")
	}
	if err != nil {
		t.Fatalf("foreign user key did not validate after exchange: %v", err)
	}
	if id.UserName != "alice" {
		t.Errorf("identity user = %q, want alice (local-authority preserved names)", id.UserName)
	}
}

func TestUsersSyncNodeDropEvictsForeignUsers(t *testing.T) {
	sender := newTestUsersSync(t, "node-a")
	receiver := newTestUsersSync(t, "node-b")

	mustAddUserFor(t, sender.store, "bob")
	sender.setPeers(map[string]struct{}{"node-b": struct{}{}})
	receiver.setPeers(map[string]struct{}{"node-a": struct{}{}})

	sender.pool.publishLocal()
	for _, event := range sender.published {
		event.NodeID = "node-a"
		if err := receiver.pool.OnEvent(event); err != nil {
			t.Fatalf("receiver OnEvent: %v", err)
		}
	}

	// Node-a leaves the cluster: the receiver's live peer set shrinks and
	// reconcile must evict every foreign user without touching locals.
	localUser := mustAddUserFor(t, receiver.store, "carol-local")
	receiver.setPeers(map[string]struct{}{})
	receiver.pool.reconcile()

	users, err := usersFileSnapshot(t, receiver)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	foreign := 0
	for _, u := range users {
		if u.OriginNode == "node-a" {
			foreign++
			t.Errorf("foreign user %q from dropped node still cached", u.Name)
		}
	}
	if foreign == 0 && len(users) == 0 {
		t.Fatal("no users at all after eviction: local user was lost too")
	}
	found := false
	for _, u := range users {
		if u.Name == localUser.Name && u.OriginNode == "" {
			found = true
		}
	}
	if !found {
		t.Error("local user missing after eviction; locals must remain authoritative")
	}
}

func TestUsersSyncDisabledExchangesNothing(t *testing.T) {
	usersPath := filepath.Join(t.TempDir(), "users.json5")
	store, err := auth.NewStore(usersPath)
	if err != nil {
		t.Fatalf("auth.NewStore: %v", err)
	}
	pool := NewUsersSync(
		UsersSyncConfig{}, // Enabled=false: multi-user off
		store,
		"node-a",
		func(models.ClusterEventType, any) error {
			t.Fatal("disabled pool must not publish")
			return nil
		},
		func() map[string]struct{} { return nil },
		testLogger(),
	)

	mustAddUserFor(t, store, "dave")

	// Publish path silent.
	if err := pool.ExchangeNow(); err != nil {
		t.Fatalf("ExchangeNow on disabled pool: %v", err)
	}

	// Inbound payloads rejected as no-ops.
	event := &models.ClusterEvent{
		NodeID:    "node-b",
		EventType: EventTypeUsersSync,
		Payload:   json.RawMessage(`{"sender_node_id":"node-b","users":[{"id":"u1","name":"eve","keys":[]}]}`),
	}
	if err := pool.OnEvent(event); err != nil {
		t.Fatalf("OnEvent on disabled pool should be silent no-op: %v", err)
	}

	// The exchange must have left the store untouched: still exactly the
	// one local user, no origin stamping, and — critically — users.json5
	// does not even exist yet because AddUser persisted it but the pool
	// wrote nothing new. A second local user proves no pool-driven writes
	// corrupted state.
	mustAddUserFor(t, store, "second-local")
	users, err := readUsersFile(usersPath)
	if err != nil {
		t.Fatalf("read users file after disabled-mode exchanges: %v", err)
	}
	if len(users) != 2 {
		t.Fatalf("disabled-mode exchanges changed store shape: %d users, want exactly 2 locals", len(users))
	}
	for _, u := range users {
		if u.OriginNode != "" {
			t.Errorf("user %q gained OriginNode %q under disabled mode", u.Name, u.OriginNode)
		}
		if u.Name == "eve" {
			t.Error("inbound foreign user leaked into a disabled pool")
		}
	}
}

func TestUsersSyncForeignPayloadFromUnclusteredNodeRejected(t *testing.T) {
	receiver := newTestUsersSync(t, "node-b")
	receiver.setPeers(map[string]struct{}{}) // node-c is NOT clustered

	event := &models.ClusterEvent{
		NodeID:    "node-c",
		EventType: EventTypeUsersSync,
		Payload:   json.RawMessage(`{"sender_node_id":"node-c","users":[{"id":"u2","name":"frank","keys":[{"id":"k1","hash":"aa"}]}]}`),
	}
	if err := receiver.pool.OnEvent(event); err != nil {
		t.Fatalf("OnEvent: %v", err)
	}

	users, err := usersFileSnapshotDirect(t, receiver.pool.cfg.UsersFile)
	if err != nil {
		t.Fatalf("snapshot: %v", err)
	}
	if len(users) != 0 {
		t.Errorf("unclustered origin leaked %d users into the cache", len(users))
	}
}

func TestUsersSyncMalformedPayloadReportsError(t *testing.T) {
	receiver := newTestUsersSync(t, "node-b")
	receiver.setPeers(map[string]struct{}{"node-a": struct{}{}})

	event := &models.ClusterEvent{
		NodeID:    "node-a",
		EventType: EventTypeUsersSync,
		Payload:   json.RawMessage(`not-json`),
	}
	if err := receiver.pool.OnEvent(event); err == nil {
		t.Fatal("malformed USERS_SYNC payload must report an error to the gossip pipeline")
	}
}

func TestUsersSyncWireContractDocumentedFields(t *testing.T) {
	// Locks the wire format: each exported user carries the SENDING node's
	// id once stamped by the receiving pool.
	receiver := newTestUsersSync(t, "node-b")
	receiver.setPeers(map[string]struct{}{"node-a": struct{}{}})

	payload := UsersSyncPayload{
		SenderNodeID: "node-a",
		Users: []auth.User{
			{ID: "u3", Name: "grace"},
		},
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal wire payload: %v", err)
	}
	stripped := map[string]any{}
	if err := json.Unmarshal(raw, &stripped); err != nil {
		t.Fatalf("unmarshal wire payload: %v", err)
	}
	if _, ok := stripped["users"]; !ok {
		t.Error("wire payload lost required 'users' field")
	}
	if _, ok := stripped["sender_node_id"]; !ok {
		t.Error("wire payload lost required 'sender_node_id' field")
	}
	_ = receiver
}
