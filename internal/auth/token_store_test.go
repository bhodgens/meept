package auth

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTokenStore_SaveLoadDelete(t *testing.T) {
	dir := t.TempDir()
	enc, err := NewEncryptionKey("test-key-store")
	if err != nil {
		t.Fatalf("NewEncryptionKey: %v", err)
	}
	store := NewTokenStoreDir(dir, enc)

	token := &TokenResult{
		AccessToken:  "access-123",
		TokenType:    "Bearer",
		RefreshToken: "refresh-456",
		Expiry:       time.Now().Add(1 * time.Hour),
		Scopes:       []string{"models:read", "read:user"},
	}

	if err := store.Save("github-models", token); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Verify file exists with correct permissions.
	path := filepath.Join(dir, "github-models.json")
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("Stat token file: %v", err)
	}
	// Check file permissions are 0600 (owner read/write only).
	perm := info.Mode().Perm()
	if perm != 0600 {
		t.Errorf("file permissions = %o, want 0600", perm)
	}

	// Load and verify.
	loaded, err := store.Load("github-models")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if loaded.AccessToken != "access-123" {
		t.Errorf("AccessToken = %q, want %q", loaded.AccessToken, "access-123")
	}
	if loaded.RefreshToken != "refresh-456" {
		t.Errorf("RefreshToken = %q, want %q", loaded.RefreshToken, "refresh-456")
	}
	if len(loaded.Scopes) != 2 {
		t.Errorf("Scopes = %v, want 2 elements", loaded.Scopes)
	}

	// Delete.
	if err := store.Delete("github-models"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("expected file to be deleted")
	}
}

func TestTokenStore_Load_Nonexistent(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	_, err := store.Load("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}

func TestTokenStore_Delete_Nonexistent(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	err := store.Delete("nonexistent")
	if err == nil {
		t.Fatal("expected error for nonexistent token")
	}
}

func TestTokenStore_Save_DifferentKeysCantRead(t *testing.T) {
	dir := t.TempDir()
	enc1, _ := NewEncryptionKey("key-alpha")
	store1 := NewTokenStoreDir(dir, enc1)

	token := &TokenResult{
		AccessToken:  "at-secret",
		TokenType:    "Bearer",
		RefreshToken: "rt-secret",
		Expiry:       time.Now().Add(1 * time.Hour),
	}
	if err := store1.Save("prov", token); err != nil {
		t.Fatalf("Save: %v", err)
	}

	enc2, _ := NewEncryptionKey("key-beta")
	store2 := NewTokenStoreDir(dir, enc2)
	_, err := store2.Load("prov")
	if err == nil {
		t.Fatal("expected error decrypting with wrong key")
	}
}

func TestTokenStore_List(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	now := time.Now()

	// Save two tokens.
	store.Save("github-models", &TokenResult{
		AccessToken:  "at1",
		TokenType:    "Bearer",
		RefreshToken: "rt1",
		Expiry:       now.Add(1 * time.Hour),
		Scopes:       []string{"models:read"},
	})
	store.Save("google-oauth", &TokenResult{
		AccessToken:  "at2",
		TokenType:    "Bearer",
		RefreshToken: "",
		Expiry:       now.Add(30 * time.Minute),
		Scopes:       []string{"generativelanguage.retriever"},
	})

	infos, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 2 {
		t.Fatalf("List returned %d entries, want 2", len(infos))
	}

	// Build a map for easier assertions.
	byProvider := map[string]StoredTokenInfo{}
	for _, info := range infos {
		byProvider[info.Provider] = info
	}

	if gh, ok := byProvider["github-models"]; !ok {
		t.Error("missing github-models entry")
	} else {
		if !gh.HasRefresh {
			t.Error("github-models should have refresh token")
		}
		if len(gh.Scopes) != 1 {
			t.Errorf("github-models scopes = %v, want 1 element", gh.Scopes)
		}
	}

	if goauth, ok := byProvider["google-oauth"]; !ok {
		t.Error("missing google-oauth entry")
	} else {
		if goauth.HasRefresh {
			t.Error("google-oauth should not have refresh token")
		}
	}
}

func TestTokenStore_List_Empty(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	infos, err := store.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(infos) != 0 {
		t.Errorf("List returned %d entries, want 0", len(infos))
	}
}

func TestTokenStore_GetValidToken_Fresh(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	store.Save("provider-a", &TokenResult{
		AccessToken:  "fresh-access-token",
		TokenType:    "Bearer",
		RefreshToken: "refresh-tok",
		Expiry:       time.Now().Add(2 * time.Hour),
	})

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  "https://example.com/token", // not used for fresh tokens
	}

	token, err := store.GetValidToken(context.Background(), "provider-a", cfg)
	if err != nil {
		t.Fatalf("GetValidToken: %v", err)
	}
	if token != "fresh-access-token" {
		t.Errorf("token = %q, want %q", token, "fresh-access-token")
	}
}

func TestTokenStore_GetValidToken_ExpiredNoRefresh(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	store.Save("provider-b", &TokenResult{
		AccessToken:  "expired-token",
		TokenType:    "Bearer",
		RefreshToken: "",
		Expiry:       time.Now().Add(-5 * time.Minute),
	})

	cfg := DeviceFlowConfig{
		ClientID: "test-client",
		TokenEP:  "https://example.com/token",
	}

	_, err := store.GetValidToken(context.Background(), "provider-b", cfg)
	if err == nil {
		t.Fatal("expected error for expired token without refresh")
	}
}

func TestTokenStore_Init_CreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "oauth-subdir")
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)

	if err := store.Init(); err != nil {
		t.Fatalf("Init: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("Stat dir: %v", err)
	}
	perm := info.Mode().Perm()
	if perm != 0700 {
		t.Errorf("dir permissions = %o, want 0700", perm)
	}
}

func TestTokenStore_Dir(t *testing.T) {
	dir := t.TempDir()
	enc, _ := NewEncryptionKey("test-key")
	store := NewTokenStoreDir(dir, enc)
	if store.Dir() != dir {
		t.Errorf("Dir() = %q, want %q", store.Dir(), dir)
	}
}
