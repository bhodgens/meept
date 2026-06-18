package llm

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
)

// CredentialStore manages API credentials.
type CredentialStore struct {
	filepath string
	creds    map[string]string // provider_id -> api_key
	mu       sync.RWMutex      // Protects concurrent access to creds
}

// NewCredentialStore creates a new credential store.
func NewCredentialStore(stateDir string) (*CredentialStore, error) {
	cs := &CredentialStore{
		filepath: filepath.Join(stateDir, "credentials.json"),
		creds:    make(map[string]string),
	}

	// Load existing credentials
	if err := cs.load(); err != nil && !os.IsNotExist(err) {
		return nil, err
	}

	return cs, nil
}

func (cs *CredentialStore) load() error {
	data, err := os.ReadFile(cs.filepath)
	if err != nil {
		return err
	}
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return json.Unmarshal(data, &cs.creds) //nolint:mutexio // mutex guards cs.creds mutation during Unmarshal
}

func (cs *CredentialStore) save() error {
	if err := os.MkdirAll(filepath.Dir(cs.filepath), 0o700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cs.creds, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(cs.filepath, data, 0o600)
}

// Get returns the API key for a provider.
func (cs *CredentialStore) Get(providerID string) (string, bool) {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	key, ok := cs.creds[providerID]
	return key, ok
}

// Set stores an API key for a provider.
func (cs *CredentialStore) Set(providerID, apiKey string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.creds[providerID] = apiKey
	return cs.save()
}

// Delete removes an API key.
func (cs *CredentialStore) Delete(providerID string) error {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	delete(cs.creds, providerID)
	return cs.save()
}

// List returns all stored provider IDs.
func (cs *CredentialStore) List() []string {
	cs.mu.RLock()
	defer cs.mu.RUnlock()
	ids := make([]string, 0, len(cs.creds))
	for id := range cs.creds {
		ids = append(ids, id)
	}
	return ids
}
