package auth

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/tailscale/hujson"

	"github.com/caimlas/meept/pkg/id"
)

// filePerm is the on-disk permission mode for the users store: owner
// read/write only, since key hashes are authentication material.
const filePerm = 0o600

// Store holds users and their API keys, persisted to a JSON5 file.
//
// Concurrency: all access serializes on mu. File I/O always happens OUTSIDE
// the lock — state is snapshotted under the lock first, then written after
// release (collect-under-lock / operate-outside, enforced by mutexio).
type Store struct {
	path  string
	mu    sync.RWMutex
	users []User
}

// NewStore opens (or initializes) the users store at path. A missing file
// starts empty and is created on the first mutation. A malformed file yields
// an error rather than panicking.
func NewStore(path string) (*Store, error) {
	users, err := loadStore(path)
	if err != nil {
		return nil, err
	}
	return &Store{path: path, users: users}, nil
}

// Validate authenticates a presented raw API key at time now. It hashes the
// key and looks for a constant-time match across every user's keys. Keys
// with a non-nil ExpiresAt are rejected once now is after that instant.
func (s *Store) Validate(rawKey string, now time.Time) (*Identity, error) {
	if rawKey == "" {
		return nil, fmt.Errorf("validate key: empty key")
	}

	hash := hashKey(rawKey)

	s.mu.RLock()
	var found *Key
	var foundUser *User
	for i := range s.users {
		for j := range s.users[i].Keys {
			k := s.users[i].Keys[j]
			if subtle.ConstantTimeCompare([]byte(k.Hash), []byte(hash)) == 1 {
				found = &k
				foundUser = &s.users[i]
				break
			}
		}
		if found != nil {
			break
		}
	}
	var (
		userID  string
		keyID   string
		expired bool
	)
	if found != nil {
		userID = foundUser.ID
		keyID = found.ID
		expired = found.ExpiresAt != nil && now.After(*found.ExpiresAt)
	}
	s.mu.RUnlock()

	switch {
	case found == nil:
		return nil, fmt.Errorf("validate key: unknown key")
	case expired:
		return nil, fmt.Errorf("validate key %s: expired", keyID)
	}

	return &Identity{UserID: userID, UserName: foundUser.Name, KeyID: keyID}, nil
}

// AddUser creates a new local user with the given name. It errors if a user
// with that name already exists. The user is persisted before returning.
func (s *Store) AddUser(name string) (*User, error) {
	if name == "" {
		return nil, fmt.Errorf("add user: empty name")
	}

	s.mu.Lock()
	for _, u := range s.users {
		if u.Name == name {
			s.mu.Unlock()
			return nil, fmt.Errorf("add user %q: duplicate name", name)
		}
	}
	u := User{ID: id.Generate("user-"), Name: name}
	prev := s.users
	s.users = append(append([]User{}, prev...), u)
	snapshot := copyUsers(s.users)
	path := s.path
	s.mu.Unlock()

	if err := saveStore(path, snapshot); err != nil {
		s.mu.Lock()
		s.users = prev
		s.mu.Unlock()
		return nil, err
	}

	out := copyUser(u)
	return &out, nil
}

// AddKey generates a new raw API key for the user identified by userID,
// persisting only its sha256 hash. The raw key is returned exactly once.
// expiresAt may be nil for a never-expiring key.
func (s *Store) AddKey(userID string, label string, expiresAt *time.Time) (rawKey string, err error) {
	raw, hash, err := generateRawKey()
	if err != nil {
		return "", fmt.Errorf("add key: %w", err)
	}

	k := Key{ID: id.Generate("key-"), Hash: hash, Label: label, ExpiresAt: expiresAt}

	s.mu.Lock()
	idx := -1
	for i, u := range s.users {
		if u.ID == userID {
			idx = i
			break
		}
	}
	if idx < 0 {
		s.mu.Unlock()
		return "", fmt.Errorf("add key: unknown user %q", userID)
	}
	prevKeys := s.users[idx].Keys
	keys := make([]Key, 0, len(prevKeys)+1)
	keys = append(keys, prevKeys...)
	keys = append(keys, k)
	s.users[idx].Keys = keys
	snapshot := copyUsers(s.users)
	path := s.path
	s.mu.Unlock()

	if err := saveStore(path, snapshot); err != nil {
		s.mu.Lock()
		s.users[idx].Keys = prevKeys
		s.mu.Unlock()
		return "", err
	}

	return raw, nil
}

// RevokeKey removes the key identified by keyID from the user identified by
// userID. Revoking a nonexistent user or key is an error.
func (s *Store) RevokeKey(userID, keyID string) error {
	s.mu.Lock()
	idx := -1
	keyIdx := -1
	for i, u := range s.users {
		if u.ID != userID {
			continue
		}
		idx = i
		for j, k := range u.Keys {
			if k.ID == keyID {
				keyIdx = j
				break
			}
		}
		break
	}
	if idx < 0 || keyIdx < 0 {
		s.mu.Unlock()
		return fmt.Errorf("revoke key %s from user %s: not found", keyID, userID)
	}
	prevKeys := s.users[idx].Keys
	next := make([]Key, 0, len(prevKeys)-1)
	next = append(next, prevKeys[:keyIdx]...)
	next = append(next, prevKeys[keyIdx+1:]...)
	s.users[idx].Keys = next
	snapshot := copyUsers(s.users)
	path := s.path
	s.mu.Unlock()

	if err := saveStore(path, snapshot); err != nil {
		s.mu.Lock()
		s.users[idx].Keys = prevKeys
		s.mu.Unlock()
		return err
	}
	return nil
}

// MergeForeign reconciles cluster-pooled ("foreign") users into the store.
//
// Callers pass fully-attributed User values (OriginNode set by the sync
// layer) plus the set of currently clustered peer node IDs:
//
//   - Local users (empty OriginNode) are authoritative and never overwritten.
//   - A foreign user is accepted only while its OriginNode is in activePeers;
//     an incoming value whose origin is not clustered is ignored.
//   - Existing foreign users whose node dropped out of activePeers are
//     removed from the cache.
//   - A same-origin re-merge replaces the previous foreign copy wholesale.
//
// The merged state is persisted outside the lock; on a persistence failure
// the in-memory state is rolled back so memory and disk stay consistent.
func (s *Store) MergeForeign(users []User, activePeers map[string]struct{}) error {
	s.mu.Lock()
	prev := s.users

	foreignByID := make(map[string]User, len(users))
	for _, fu := range users {
		if fu.OriginNode == "" {
			continue // unattributed values are not mergeable
		}
		foreignByID[fu.ID] = fu
	}

	next := make([]User, 0, len(s.users)+len(users))
	// Retention pass: locals always survive; foreign entries survive only
	// while their origin node remains clustered and unreplaced this merge.
	for _, lu := range s.users {
		switch {
		case lu.OriginNode == "":
			next = append(next, lu)
		case hasForeign(foreignByID, lu.ID):
			// replaced below by the incoming wholesale copy
		default:
			if _, active := activePeers[lu.OriginNode]; active {
				next = append(next, lu)
			}
		}
	}
	// Acceptance pass: admit incoming foreign users whose origin is live.
	for _, fu := range users {
		if fu.OriginNode == "" {
			continue
		}
		if _, active := activePeers[fu.OriginNode]; !active {
			continue
		}
		next = append(next, copyUser(fu))
	}

	s.users = next
	snapshot := copyUsers(s.users)
	path := s.path
	s.mu.Unlock()

	if err := saveStore(path, snapshot); err != nil {
		s.mu.Lock()
		s.users = prev
		s.mu.Unlock()
		return err
	}
	return nil
}

// hasForeign reports whether a user id has an incoming foreign replacement.
func hasForeign(byID map[string]User, userID string) bool {
	_, ok := byID[userID]
	return ok
}

// loadStore reads and parses a JSON5 users file into a user slice. A missing
// file yields an empty slice; a malformed file yields an error.
func loadStore(path string) ([]User, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("load users store %s: %w", path, err)
	}

	stdJSON, err := hujson.Standardize(data)
	if err != nil {
		return nil, fmt.Errorf("parse users store %s: %w", path, err)
	}

	var wrapper struct {
		Users []User `json:"users"`
	}
	if err := json.Unmarshal(stdJSON, &wrapper); err != nil {
		return nil, fmt.Errorf("decode users store %s: %w", path, err)
	}
	return wrapper.Users, nil
}

// saveStore persists users to path atomically: indented JSON (a valid JSON5
// subset) is written to a temp file at 0600 and renamed over the target.
func saveStore(path string, users []User) error {
	wrapper := struct {
		Users []User `json:"users"`
	}{Users: users}
	data, err := json.MarshalIndent(wrapper, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal users store: %w", err)
	}
	data = append(data, '\n')

	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, filePerm); err != nil {
		return fmt.Errorf("write users store temp file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return fmt.Errorf("rename users store temp file to %s: %w", path, err)
	}
	return nil
}
