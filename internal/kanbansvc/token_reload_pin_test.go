package kanbansvc

import (
	"path/filepath"
	"testing"
)

// A long-running sidecar loads the token file once at boot. The daemon mints
// a NEW token later via /kanban/pair/exchange (a distinct TokenStore writes
// the same file). Grants() must notice the change and authorize the new
// token without a sidecar restart.
func TestTokenStore_GrantsPicksUpDaemonMintedToken(t *testing.T) {
	path := filepath.Join(t.TempDir(), "kanban", tokenFileName)

	// Sidecar boots with an empty token store.
	sidecar, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore(sidecar): %v", err)
	}
	if _, ok := sidecar.Grants("new-token"); ok {
		t.Fatal("test setup: empty store already knows the token")
	}

	// Daemon process mints a pairing code and exchanges it, writing the
	// shared file through its own TokenStore instance.
	daemon, err := NewTokenStore(path)
	if err != nil {
		t.Fatalf("NewTokenStore(daemon): %v", err)
	}
	code, err := daemon.GeneratePairCode(GrantRead, GrantMutate)
	if err != nil {
		t.Fatalf("GeneratePairCode: %v", err)
	}
	token, _, err := daemon.ExchangePairCode(code)
	if err != nil {
		t.Fatalf("ExchangePairCode: %v", err)
	}

	// The sidecar must see the new token on its next auth check.
	grants, ok := sidecar.Grants(token)
	if !ok {
		t.Fatal("sidecar did not pick up daemon-minted token (requires restart)")
	}
	if len(grants) != 2 {
		t.Fatalf("grants = %v, want 2 lanes", grants)
	}
}
