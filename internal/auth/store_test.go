package auth

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := NewStore(filepath.Join(t.TempDir(), "users.json5"))
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

func mustAddUser(t *testing.T, s *Store, name string) *User {
	t.Helper()
	u, err := s.AddUser(name)
	if err != nil {
		t.Fatalf("AddUser(%q): %v", name, err)
	}
	return u
}

func TestStoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json5")

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}

	alice := mustAddUser(t, s, "alice")
	expiry := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	rawKey, err := s.AddKey(alice.ID, "laptop", &expiry)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	if _, err := s.AddKey(alice.ID, "", nil); err != nil {
		t.Fatalf("AddKey (nil expiry): %v", err)
	}

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload NewStore: %v", err)
	}

	if len(reloaded.users) != 1 || reloaded.users[0].Name != "alice" {
		t.Fatalf("reloaded users = %+v, want single user alice", reloaded.users)
	}
	rt := reloaded.users[0]
	if rt.ID != alice.ID {
		t.Fatalf("reloaded user ID = %q, want %q", rt.ID, alice.ID)
	}
	if len(rt.Keys) != 2 {
		t.Fatalf("reloaded key count = %d, want 2", len(rt.Keys))
	}

	var expKey, noExpKey *Key
	for i := range rt.Keys {
		if rt.Keys[i].Label == "laptop" {
			expKey = &rt.Keys[i]
		} else if rt.Keys[i].ExpiresAt == nil {
			noExpKey = &rt.Keys[i]
		}
	}
	if expKey == nil || expKey.ExpiresAt == nil || !expKey.ExpiresAt.Equal(expiry) {
		t.Fatalf("expiring key not preserved: %+v", expKey)
	}
	if noExpKey == nil {
		t.Fatal("nil-expiry key not preserved")
	}

	id, err := reloaded.Validate(rawKey, time.Now())
	if err != nil {
		t.Fatalf("Validate after reload: %v", err)
	}
	if id.UserID != alice.ID {
		t.Fatalf("identity userID = %q, want %q", id.UserID, alice.ID)
	}
}

func TestNewStoreCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "users.json5")
	if err := os.WriteFile(path, []byte("{not valid json5 !!!"), 0o600); err != nil {
		t.Fatalf("write corrupt file: %v", err)
	}

	s, err := NewStore(path)
	if err == nil {
		t.Fatal("NewStore on corrupt file succeeded, want error")
	}
	if s != nil {
		t.Fatalf("NewStore returned non-nil store on error: %+v", s)
	}
}

func TestNewStoreMissingFileIsEmpty(t *testing.T) {
	newTestStore(t) // no assertion needed beyond not panicking/erroring
}

func TestStoreFilePerm0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json5")

	s := newTestStorePath(t, path)
	mustAddUser(t, s, "alice")

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat users file: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("users file mode = %o, want 600", perm)
	}
}

func TestSaveIsAtomicNoTmpLeftover(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json5")

	s := newTestStorePath(t, path)
	mustAddUser(t, s, "alice")

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Fatalf("temp file left behind after save: %s", e.Name())
		}
	}
}

func TestFileSurvivesJSON5FeaturesOnReload(t *testing.T) {
	// The persisted file is plain JSON (a JSON5 subset), and loading accepts
	// real JSON5 (comments, trailing commas) — proving the hujson path works.
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json5")

	content := `{
		// meept users store
		"users": [
			{
				"id": "user-manual",
				"name": "bob",
				"keys": [
					{"id": "key-manual", "hash": "abc123"},
				],
			},
		],
	}`
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write json5 fixture: %v", err)
	}

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore with JSON5 features: %v", err)
	}
	if len(s.users) != 1 || s.users[0].Name != "bob" {
		t.Fatalf("parsed users = %+v, want [bob]", s.users)
	}
}

// newTestStorePath builds a store at an explicit path.
func newTestStorePath(t *testing.T, path string) *Store {
	t.Helper()
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore(%q): %v", path, err)
	}
	return s
}

func TestValidateValidKey(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	raw, err := s.AddKey(alice.ID, "laptop", nil)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	id, err := s.Validate(raw, time.Now())
	if err != nil {
		t.Fatalf("Validate: %v", err)
	}
	if id.UserID != alice.ID || id.UserName != "alice" || id.KeyID == "" {
		t.Fatalf("identity = %+v, want userID %q with non-empty key id", id, alice.ID)
	}
}

func TestValidateUnknownKey(t *testing.T) {
	s := newTestStore(t)

	if _, err := s.Validate(strings.Repeat("ab", 32), time.Now()); err == nil {
		t.Fatal("Validate of unknown key succeeded, want error")
	}
	if _, err := s.Validate("", time.Now()); err == nil {
		t.Fatal("Validate of empty key succeeded, want error")
	}
}

func TestValidateExpiredKey(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	past := time.Now().UTC().Add(-time.Hour)
	raw, err := s.AddKey(alice.ID, "expired", &past)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	// Contract: reject when now.After(*ExpiresAt) — the key is still valid
	// AT the expiry instant and dead strictly after it.
	if _, err := s.Validate(raw, past); err != nil {
		t.Fatalf("Validate at exact expiry instant: %v", err)
	}
	if _, err := s.Validate(raw, past.Add(time.Nanosecond)); err == nil {
		t.Fatal("Validate one nanosecond past expiry succeeded, want error")
	}
	if _, err := s.Validate(raw, past.Add(time.Minute)); err == nil {
		t.Fatal("Validate of expired key succeeded, want error")
	}
}

func TestValidateExpiryBoundary(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	future := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	raw, err := s.AddKey(alice.ID, "soon", &future)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	before := future.Add(-time.Second)
	id, err := s.Validate(raw, before)
	if err != nil {
		t.Fatalf("Validate one second before expiry: %v", err)
	}
	if id.KeyID == "" {
		t.Fatal("identity missing key id before expiry")
	}
}

func TestValidateNilExpiryNeverExpires(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	raw, err := s.AddKey(alice.ID, "forever", nil)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	farFuture := time.Now().UTC().AddDate(100, 0, 0)
	if _, err := s.Validate(raw, farFuture); err != nil {
		t.Fatalf("nil-expiry key failed after 100 years: %v", err)
	}
}

func TestValidateHashLookupAcrossUsers(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")
	bob := mustAddUser(t, s, "bob")

	bobRaw, err := s.AddKey(bob.ID, "phone", nil)
	if err != nil {
		t.Fatalf("AddKey bob: %v", err)
	}
	if _, err := s.AddKey(alice.ID, "laptop", nil); err != nil {
		t.Fatalf("AddKey alice: %v", err)
	}

	id, err := s.Validate(bobRaw, time.Now())
	if err != nil {
		t.Fatalf("Validate bob's key: %v", err)
	}
	if id.UserID != bob.ID || id.UserName != "bob" {
		t.Fatalf("identity = %+v, want bob", id)
	}
}

func TestAddUserDuplicateName(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "alice")

	if _, err := s.AddUser("alice"); err == nil {
		t.Fatal("AddUser with duplicate name succeeded, want error")
	} else if !strings.Contains(err.Error(), "duplicate") {
		t.Fatalf("duplicate-name error = %v, want it to mention \"duplicate\"", err)
	}
}

func TestAddUserEmptyName(t *testing.T) {
	s := newTestStore(t)

	u, err := s.AddUser("")
	if err == nil || u != nil {
		t.Fatalf("AddUser(\"\") = (%+v, %v), want error", u, err)
	}
}

func TestAddKeyUnknownUser(t *testing.T) {
	s := newTestStore(t)

	_, err := s.AddKey("user-does-not-exist", "label", nil)
	if err == nil {
		t.Fatal("AddKey for unknown user succeeded, want error")
	}
}

func TestAddKeyRawReturnedOnceNotStored(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	raw, err := s.AddKey(alice.ID, "laptop", nil)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	for _, k := range s.users[0].Keys {
		if strings.Contains(k.Hash, raw) || k.Hash == raw {
			t.Fatal("raw key material stored in place of hash")
		}
	}
	if len(raw) != 64 {
		t.Fatalf("raw key length = %d, want 64", len(raw))
	}
}

func TestRevokeKeyRemovesAndInvalidates(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	raw, err := s.AddKey(alice.ID, "laptop", nil)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	keys := s.users[0].Keys
	if len(keys) != 1 {
		t.Fatalf("key count = %d, want 1", len(keys))
	}
	keyID := keys[0].ID

	if err := s.RevokeKey(alice.ID, keyID); err != nil {
		t.Fatalf("RevokeKey: %v", err)
	}
	if len(s.users[0].Keys) != 0 {
		t.Fatalf("keys after revoke = %d, want 0", len(s.users[0].Keys))
	}
	if _, err := s.Validate(raw, time.Now()); err == nil {
		t.Fatal("Validate of revoked key succeeded, want error")
	}
}

func TestRevokeKeyMissingKeyOrUser(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	if err := s.RevokeKey(alice.ID, "key-missing"); err == nil {
		t.Fatal("RevokeKey of missing key succeeded, want error")
	}
	if err := s.RevokeKey("user-missing", "key-anything"); err == nil {
		t.Fatal("RevokeKey of unknown user succeeded, want error")
	}
}

func TestMutationRollbackOnSaveFailure(t *testing.T) {
	s := newTestStorePath(t, t.TempDir()+"/users.json5")
	alice := mustAddUser(t, s, "alice")

	// Point the store at a path whose parent directory is missing so the
	// post-lock persistence fails and the in-memory mutation must roll back
	// (test is in-package).
	prevPath := s.path
	s.path = t.TempDir() + "/missing-dir/users.json5"

	if _, err := s.AddUser("bob"); err == nil {
		t.Fatal("AddUser expected persistence failure on bad path")
	}
	for _, u := range s.users {
		if u.Name == "bob" {
			t.Fatal("in-memory user persisted after failed save; rollback missing")
		}
	}

	if _, err := s.AddKey(alice.ID, "label", nil); err == nil {
		t.Fatal("AddKey expected persistence failure on bad path")
	}
	for _, k := range s.users[0].Keys {
		if k.Label == "label" {
			t.Fatal("in-memory key persisted after failed save; rollback missing")
		}
	}

	s.path = prevPath
}

func TestConcurrentMutationsRaceFree(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")

	done := make(chan struct{})
	go func() {
		defer close(done)
		if _, err := s.AddKey(alice.ID, "concurrent", nil); err != nil {
			t.Errorf("AddKey: %v", err)
		}
	}()
	if _, err := s.Validate(strings.Repeat("zz", 32), time.Now()); err != nil && !strings.Contains(err.Error(), "unknown") {
		t.Errorf("unexpected validate error: %v", err)
	}
	<-done

	if got := len(s.users[0].Keys); got != 1 {
		t.Fatalf("key count after concurrent add = %d, want 1", got)
	}
}

func TestMergeForeignAcceptsClusteredUsers(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "local-admin")

	foreign := User{
		ID:         "user-remote-1",
		Name:       "pooled",
		OriginNode: "node-a",
		Keys: []Key{{
			ID:   "key-remote-1",
			Hash: hashKey("raw-pooled"),
		}},
	}

	if err := s.MergeForeign([]User{foreign}, map[string]struct{}{"node-a": {}}); err != nil {
		t.Fatalf("MergeForeign: %v", err)
	}

	if len(s.users) != 2 {
		t.Fatalf("user count = %d, want 2 (%+v)", len(s.users), s.users)
	}
	got := s.users[1]
	if got.OriginNode != "node-a" || got.ID != foreign.ID || got.Name != foreign.Name || len(got.Keys) != 1 {
		t.Fatalf("merged foreign user = %+v, want %+v", got, foreign)
	}

	id, err := s.Validate("raw-pooled", farFuture())
	if err != nil {
		t.Fatalf("Validate pooled key: %v", err)
	}
	if id.UserID != foreign.ID {
		t.Fatalf("identity userID = %q, want %q", id.UserID, foreign.ID)
	}
}

func TestMergeForeignRejectsInactivePeer(t *testing.T) {
	s := newTestStore(t)

	foreign := User{ID: "user-remote-2", Name: "drifter", OriginNode: "node-gone"}
	if err := s.MergeForeign([]User{foreign}, map[string]struct{}{"node-live": {}}); err != nil {
		t.Fatalf("MergeForeign: %v", err)
	}
	if len(s.users) != 0 {
		t.Fatalf("inactive-peer user accepted: users = %+v", s.users)
	}
}

func TestMergeForeignLocalAuthoritative(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "admin")

	incoming := []User{
		{ID: "user-local-any", Name: "admin", OriginNode: "node-b"}, // same name, different id
	}
	if err := s.MergeForeign(incoming, map[string]struct{}{"node-b": {}}); err != nil {
		t.Fatalf("MergeForeign: %v", err)
	}

	if len(s.users) != 2 {
		t.Fatalf("user count = %d, want 2 (local admin + foreign admin copy)", len(s.users))
	}
	if s.users[0].Name != "admin" || s.users[0].OriginNode != "" {
		t.Fatalf("local user clobbered or reordered: %+v", s.users[0])
	}
	if s.users[1].ID == s.users[0].ID {
		t.Fatal("foreign user overwrote local identity")
	}
}

func TestMergeForeignDropsWhenNodeLeaves(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "admin")

	nodeA := User{ID: "user-a", Name: "alice-remote", OriginNode: "node-a"}
	nodeB := User{ID: "user-b", Name: "bob-remote", OriginNode: "node-b"}
	err := s.MergeForeign([]User{nodeA, nodeB}, map[string]struct{}{"node-a": {}, "node-b": {}})
	if err != nil {
		t.Fatalf("MergeForeign initial: %v", err)
	}
	if len(s.users) != 3 {
		t.Fatalf("user count = %d, want 3 after first merge", len(s.users))
	}

	// node-a leaves the cluster.
	if err := s.MergeForeign(nil, map[string]struct{}{"node-b": {}}); err != nil {
		t.Fatalf("MergeForeign shrink: %v", err)
	}

	var names []string
	for _, u := range s.users {
		names = append(names, u.Name)
	}
	if len(s.users) != 2 {
		t.Fatalf("users after node leave = %+v, want 2 (admin + node-b)", names)
	}
	for _, u := range s.users {
		if u.OriginNode == "node-a" {
			t.Fatalf("foreign user from departed node still present: %+v", u)
		}
	}
}

func TestMergeForeignSameOriginReplacesWholesale(t *testing.T) {
	s := newTestStore(t)

	v1 := User{
		ID:         "user-r",
		Name:       "pooled",
		OriginNode: "node-x",
		Keys:       []Key{{ID: "key-old", Hash: "hash-old"}},
	}
	if err := s.MergeForeign([]User{v1}, map[string]struct{}{"node-x": {}}); err != nil {
		t.Fatalf("MergeForeign v1: %v", err)
	}

	v2 := User{
		ID:         "user-r",
		Name:       "pooled-renamed",
		OriginNode: "node-x",
		Keys:       []Key{{ID: "key-new", Hash: "hash-new"}, {ID: "key-newer", Hash: "hash-newer"}},
	}
	if err := s.MergeForeign([]User{v2}, map[string]struct{}{"node-x": {}}); err != nil {
		t.Fatalf("MergeForeign v2: %v", err)
	}

	if len(s.users) != 1 {
		t.Fatalf("user count = %d, want 1 (no duplicate merge)", len(s.users))
	}
	got := s.users[0]
	if got.Name != "pooled-renamed" || len(got.Keys) != 2 {
		t.Fatalf("same-origin re-merge did not replace wholesale: %+v", got)
	}
	for _, k := range got.Keys {
		if k.ID == "key-old" {
			t.Fatal("stale key from previous foreign copy survived")
		}
	}
}

func TestMergeForeignIdempotentReMerge(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "admin")

	foreign := User{
		ID:         "user-idem",
		Name:       "stable",
		OriginNode: "node-i",
		Keys:       []Key{{ID: "key-idem", Hash: "hash-idem"}},
	}
	peers := map[string]struct{}{"node-i": {}}

	for i := 0; i < 3; i++ {
		if err := s.MergeForeign([]User{foreign}, peers); err != nil {
			t.Fatalf("MergeForeign iteration %d: %v", i, err)
		}
		if len(s.users) != 2 {
			t.Fatalf("iteration %d: user count = %d, want 2", i, len(s.users))
		}
	}
	if s.users[1].ID != foreign.ID || len(s.users[1].Keys) != 1 {
		t.Fatalf("repeated merge mutated state: %+v", s.users[1])
	}
}

func TestMergeForeignIgnoresUnattributedIncoming(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "admin")

	// An incoming "foreign" value with empty OriginNode is a sync-layer bug;
	// it must not be admitted as local.
	incoming := User{ID: "user-bad", Name: "mystery", OriginNode: ""}
	if err := s.MergeForeign([]User{incoming}, nil); err != nil {
		t.Fatalf("MergeForeign: %v", err)
	}
	if len(s.users) != 1 {
		t.Fatalf("unattributed incoming user accepted: %+v", s.users)
	}
}

func TestRemoveUserRemovesAndRejectsKey(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")
	if _, err := s.AddKey(alice.ID, "laptop", nil); err != nil {
		t.Fatalf("AddKey: %v", err)
	}
	raw, err := s.AddKey(alice.ID, "phone", nil)
	if err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	if err := s.RemoveUser(alice.ID); err != nil {
		t.Fatalf("RemoveUser: %v", err)
	}
	for _, u := range s.users {
		if u.ID == alice.ID {
			t.Fatal("user still present after RemoveUser")
		}
	}
	// All of the user's keys must stop authenticating too.
	if _, err := s.Validate(raw, time.Now()); err == nil {
		t.Fatal("Validate of removed user's key succeeded, want error")
	}
}

func TestRemoveUserUnknownID(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "alice")

	if err := s.RemoveUser("user-missing"); err == nil {
		t.Fatal("RemoveUser of unknown id succeeded, want error")
	} else if !strings.Contains(err.Error(), "not found") {
		t.Fatalf("error = %v, want it to mention \"not found\"", err)
	}
}

func TestRemoveUserIsLocalOnly(t *testing.T) {
	s := newTestStore(t)

	foreign := User{ID: "user-f", Name: "pooled", OriginNode: "node-x"}
	if err := s.MergeForeign([]User{foreign}, map[string]struct{}{"node-x": {}}); err != nil {
		t.Fatalf("MergeForeign: %v", err)
	}

	if err := s.RemoveUser("user-f"); err == nil {
		t.Fatal("RemoveUser of foreign user succeeded, want error (local users only)")
	}
}

func TestRemoveUserRollbackOnSaveFailure(t *testing.T) {
	s := newTestStorePath(t, t.TempDir()+"/users.json5")
	alice := mustAddUser(t, s, "alice")

	// Point the store at a path whose parent directory is missing so the
	// post-lock persistence fails and the in-memory removal must roll back.
	s.path = t.TempDir() + "/missing-dir/users.json5"
	if err := s.RemoveUser(alice.ID); err == nil {
		t.Fatal("RemoveUser expected persistence failure on bad path")
	}
	s.path = t.TempDir() + "/users.json5"

	found := false
	for _, u := range s.users {
		if u.ID == alice.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("in-memory removal persisted after failed save; rollback missing")
	}

	// With a writable path restored, a real removal round-trips cleanly.
	if err := s.RemoveUser(alice.ID); err != nil {
		t.Fatalf("RemoveUser after recovered path: %v", err)
	}
}

func TestListUsersReturnsDeepCopies(t *testing.T) {
	s := newTestStore(t)
	alice := mustAddUser(t, s, "alice")
	expiry := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	if _, err := s.AddKey(alice.ID, "laptop", &expiry); err != nil {
		t.Fatalf("AddKey: %v", err)
	}

	users := s.ListUsers()
	if len(users) != 1 || users[0].Name != "alice" || len(users[0].Keys) != 1 {
		t.Fatalf("ListUsers = %+v, want single alice with 1 key", users)
	}

	// Mutating the returned slice and key expiry pointers must not touch
	// store-owned state.
	users[0].Name = "hacked"
	users[0].Keys[0].ExpiresAt = nil

	if got := s.ListUsers(); len(got) != 1 || got[0].Name != "alice" || got[0].Keys[0].ExpiresAt == nil {
		t.Fatalf("mutation leaked into store state: %+v", got)
	}
}

func TestListUsersIncludesForeign(t *testing.T) {
	s := newTestStore(t)
	mustAddUser(t, s, "admin")

	foreign := User{ID: "user-f", Name: "pooled", OriginNode: "node-x"}
	if err := s.MergeForeign([]User{foreign}, map[string]struct{}{"node-x": {}}); err != nil {
		t.Fatalf("MergeForeign: %v", err)
	}

	users := s.ListUsers()
	if len(users) != 2 {
		t.Fatalf("user count = %d, want 2", len(users))
	}
	var hasOrigin bool
	for _, u := range users {
		if u.OriginNode == "node-x" {
			hasOrigin = true
		}
	}
	if !hasOrigin {
		t.Fatalf("foreign user missing from listing: %+v", users)
	}
}

// farFuture returns a time long after any test expiry.
func farFuture() time.Time {
	return time.Now().UTC().AddDate(100, 0, 0)
}
