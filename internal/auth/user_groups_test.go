package auth

// RED tests for leaf 03-membership/02-user-groups T1 (contract C4, human
// half): the `groups:` field on User and its users.json5 round-trip.
//
// Written BEFORE the implementation: User.Groups does not exist yet, so
// every failure below is a missing-symbol compile error ("field absent"),
// never a syntax error. GREEN state: leaf 02 T1 makes these pass without
// modification.
//
// Pinned contract (C4):
//   - User.Groups []string `json:"groups,omitempty"` — additive JSON, old
//     stores load unchanged (the pattern Key.Role established).
//   - Store save/load round-trips groups.
//   - A user with no groups persists NO "groups" key (omitempty contract).

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// findUser returns the store's live copy of the named user.
func findUser(s *Store, id string) *User {
	for i := range s.users {
		if s.users[i].ID == id {
			return &s.users[i]
		}
	}
	return nil
}

// persistStore snapshots the store's users through its save path. Test-only
// accessor mirroring what every mutating method does internally.
func persistStore(t *testing.T, s *Store) {
	t.Helper()
	s.mu.RLock()
	snapshot := copyUsers(s.users)
	path := s.path
	s.mu.RUnlock()
	if err := saveStore(path, snapshot); err != nil {
		t.Fatalf("saveStore: %v", err)
	}
}

// T2a: a user can carry groups, set and read back in-memory.
func TestUserGroups_SetAndRead(t *testing.T) {
	s := newTestStore(t)
	u := mustAddUser(t, s, "alice")
	u.Groups = []string{"admins", "oncall"}
	if !slices.Equal(u.Groups, []string{"admins", "oncall"}) {
		t.Fatalf("User.Groups = %v, want [admins oncall] (User.Groups field absent)", u.Groups)
	}
}

// T2b: groups survive a save/load round-trip through the JSON5 store.
func TestUserGroups_StoreRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json5")

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	u := mustAddUser(t, s, "bob")
	u.Groups = []string{"developers", "oncall"}
	persistStore(t, s)

	reloaded, err := NewStore(path)
	if err != nil {
		t.Fatalf("reload: %v", err)
	}
	got := findUser(reloaded, u.ID)
	if got == nil {
		t.Fatalf("user %s missing after reload", u.ID)
	}
	if !slices.Equal(got.Groups, []string{"developers", "oncall"}) {
		t.Errorf("Groups after round-trip = %v, want [developers oncall]", got.Groups)
	}
}

// T2c: a user with no groups persists no "groups" key (omitempty, old files
// load unchanged).
func TestUserGroups_OmittedWhenEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json5")

	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	mustAddUser(t, s, "carol")
	persistStore(t, s)

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read store: %v", err)
	}
	if bytes.Contains(data, []byte(`"groups"`)) {
		t.Errorf("store file contains a groups key for a user with no groups (omitempty violated):\n%s", data)
	}
}

// T2d: a legacy store file WITHOUT groups keys loads cleanly (backward
// compatibility — the additive-JSON rule).
func TestUserGroups_LegacyFileLoads(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "users.json5")
	legacy := `{
  "users": [
    {"id": "u-legacy", "name": "old", "keys": []}
  ]
}`
	if err := os.WriteFile(path, []byte(legacy), 0o600); err != nil {
		t.Fatalf("write legacy store: %v", err)
	}
	s, err := NewStore(path)
	if err != nil {
		t.Fatalf("NewStore over legacy file: %v", err)
	}
	u := findUser(s, "u-legacy")
	if u == nil {
		t.Fatalf("legacy user not loaded")
	}
	if len(u.Groups) != 0 {
		t.Errorf("legacy user Groups = %v, want empty", u.Groups)
	}
}
