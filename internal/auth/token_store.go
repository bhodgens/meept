package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// StoredTokenInfo holds metadata about a stored token without exposing the
// actual token values.
type StoredTokenInfo struct {
	Provider    string    `json:"provider"`
	Expiry      time.Time `json:"expiry"`
	Scopes      []string  `json:"scopes,omitempty"`
	HasRefresh  bool      `json:"has_refresh"`
	FileModTime time.Time `json:"file_mod_time"`
}

// TokenStore provides encrypted file-based token storage.
// Each provider gets a single file in the store directory, encrypted with
// AES-256-GCM. File permissions are set to 0600.
type TokenStore struct {
	dir string
	enc *EncryptionKey
}

// NewTokenStore creates a token store backed by the given directory.
// The directory is created with 0700 permissions if it does not exist.
func NewTokenStore(enc *EncryptionKey) *TokenStore {
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	dir := filepath.Join(home, ".meept", "oauth")
	return &TokenStore{dir: dir, enc: enc}
}

// NewTokenStoreDir creates a token store at an explicit directory path.
// This is primarily used in tests.
func NewTokenStoreDir(dir string, enc *EncryptionKey) *TokenStore {
	return &TokenStore{dir: dir, enc: enc}
}

// Init creates the token store directory if it does not exist.
func (s *TokenStore) Init() error {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return fmt.Errorf("create token store dir %s: %w", s.dir, err)
	}
	return nil
}

// Save encrypts and persists a token for the given provider.
// The file is written with 0600 permissions.
func (s *TokenStore) Save(provider string, token *TokenResult) error {
	if err := s.Init(); err != nil {
		return err
	}

	data, err := json.Marshal(token)
	if err != nil {
		return fmt.Errorf("marshal token: %w", err)
	}

	encrypted, err := s.enc.Encrypt(data)
	if err != nil {
		return fmt.Errorf("encrypt token: %w", err)
	}

	path := s.tokenPath(provider)
	if path == "" {
		return fmt.Errorf("invalid provider name: %s", provider)
	}
	// Write to a temp file then rename for atomicity.
	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, encrypted, 0600); err != nil {
		return fmt.Errorf("write token file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		os.Remove(tmpPath)
		return fmt.Errorf("rename token file: %w", err)
	}

	slog.Debug("token saved", "provider", provider, "path", path)
	return nil
}

// Load reads and decrypts the stored token for the given provider.
func (s *TokenStore) Load(provider string) (*TokenResult, error) {
	path := s.tokenPath(provider)
	if path == "" {
		return nil, fmt.Errorf("invalid provider name: %s", provider)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no token stored for %s", provider)
		}
		return nil, fmt.Errorf("read token file: %w", err)
	}

	decrypted, err := s.enc.Decrypt(data)
	if err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}

	var token TokenResult
	if err := json.Unmarshal(decrypted, &token); err != nil {
		return nil, fmt.Errorf("unmarshal token: %w", err)
	}

	return &token, nil
}

// Delete removes the stored token for the given provider.
func (s *TokenStore) Delete(provider string) error {
	path := s.tokenPath(provider)
	if path == "" {
		return fmt.Errorf("invalid provider name: %s", provider)
	}
	err := os.Remove(path)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("no token stored for %s", provider)
		}
		return fmt.Errorf("delete token file: %w", err)
	}
	slog.Debug("token deleted", "provider", provider)
	return nil
}

// List returns metadata for all stored tokens.
func (s *TokenStore) List() ([]StoredTokenInfo, error) {
	if err := os.MkdirAll(s.dir, 0700); err != nil {
		return nil, fmt.Errorf("ensure token store dir: %w", err)
	}

	entries, err := os.ReadDir(s.dir)
	if err != nil {
		return nil, fmt.Errorf("read token store dir: %w", err)
	}

	var infos []StoredTokenInfo
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		provider := strings.TrimSuffix(entry.Name(), ".json")
		info, err := s.stat(provider)
		if err != nil {
			slog.Warn("failed to stat token", "provider", provider, "error", err)
			continue
		}
		infos = append(infos, info)
	}
	return infos, nil
}

// stat loads the token metadata (without decrypting the full token) by
// reading the file modification time, then decrypting enough to get expiry.
func (s *TokenStore) stat(provider string) (StoredTokenInfo, error) {
	path := s.tokenPath(provider)
	if path == "" {
		return StoredTokenInfo{}, fmt.Errorf("invalid provider name: %s", provider)
	}

	fileInfo, err := os.Stat(path)
	if err != nil {
		return StoredTokenInfo{}, err
	}

	token, err := s.Load(provider)
	if err != nil {
		return StoredTokenInfo{}, err
	}

	return StoredTokenInfo{
		Provider:    provider,
		Expiry:      token.Expiry,
		Scopes:      token.Scopes,
		HasRefresh:  token.RefreshToken != "",
		FileModTime: fileInfo.ModTime(),
	}, nil
}

// GetValidToken loads the token for the given provider, checks if it is still
// valid, and refreshes it if necessary. Returns the access token string.
// This is the single entry point for LLM client code.
func (s *TokenStore) GetValidToken(ctx context.Context, provider string, cfg DeviceFlowConfig) (string, error) {
	token, err := s.Load(provider)
	if err != nil {
		return "", fmt.Errorf("load token for %s: %w", provider, err)
	}

	// If the token is still valid for at least 60 seconds, use it directly.
	if time.Now().Add(60 * time.Second).Before(token.Expiry) {
		return token.AccessToken, nil
	}

	// Token is expired or about to expire. Attempt refresh.
	if token.RefreshToken == "" {
		return "", fmt.Errorf("token for %s is expired and has no refresh token; run 'meept config oauth connect %s'", provider, provider)
	}

	slog.Info("refreshing expired token", "provider", provider)
	refreshed, err := RefreshTokenRequest(ctx, cfg, token.RefreshToken)
	if err != nil {
		return "", fmt.Errorf("refresh token for %s: %w", provider, err)
	}

	// If the refresh response includes a new refresh token, use it.
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = token.RefreshToken
	}

	if err := s.Save(provider, refreshed); err != nil {
		slog.Warn("failed to save refreshed token", "provider", provider, "error", err)
		// Return the token anyway; it will be refreshed again next time.
	}

	return refreshed.AccessToken, nil
}

// ResolveToken satisfies the llm.TokenResolver interface. It resolves the
// DeviceFlowConfig from the OAuthProviders registry and delegates to
// GetValidToken. This is the adapter between the 2-param interface used by
// the LLM package and the 3-param method that requires the flow config.
func (s *TokenStore) ResolveToken(ctx context.Context, provider string) (string, error) {
	cfg, err := ResolveProviderConfig(provider)
	if err != nil {
		return "", fmt.Errorf("resolve provider config for %s: %w", provider, err)
	}
	return s.GetValidToken(ctx, provider, cfg.DeviceFlowConfig())
}

// tokenPath returns the file path for a provider's token.
// Returns "" if the provider resolves to an unsafe path (e.g., directory
// traversal via "../"). Callers must check for the empty string.
func (s *TokenStore) tokenPath(provider string) string {
	safe := filepath.Base(provider)
	if safe == "." || safe == string(filepath.Separator) {
		return ""
	}
	return filepath.Join(s.dir, safe+".json")
}

// Dir returns the store directory path.
func (s *TokenStore) Dir() string {
	return s.dir
}
