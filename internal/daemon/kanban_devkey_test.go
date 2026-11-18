package daemon

// Tests for plans/tui-gui-consistency-fixes 01-backend/03-sidecar-devkey:
// the default-on sidecar grants the existing per-installation dev key the
// three C5 board grants in the shared token store BEFORE the child spawns.
// The KEY VALUE is never printed — only its presence as a token-store key.

import (
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/kanbansvc"
	"github.com/caimlas/meept/pkg/constants"
)

func TestKanbanDevKeySeed(t *testing.T) {
	home := t.TempDir()
	t.Setenv(config.EnvMeeptHome, home)

	// A known dev_key fixture at the documented location. DevAPIKey() is
	// process-cached (sync.Once), so read the key through it — the seed uses
	// the same cached value, keeping the assertion coherent even if an
	// earlier test primed the cache under a different home.
	keyPath := filepath.Join(home, "dev_key")
	if err := os.WriteFile(keyPath, []byte("kanban-devkey-fixture-32-bytes-long-0001"), 0o600); err != nil { //nolint:gosec // test fixture, owner-only
		t.Fatalf("write dev_key fixture: %v", err)
	}
	key := constants.DevAPIKey()
	if key == "" {
		t.Fatal("DevAPIKey() returned empty")
	}

	if err := seedKanbanDevKeyToken(slog.New(slog.DiscardHandler)); err != nil {
		t.Fatalf("seedKanbanDevKeyToken: %v", err)
	}

	// The shared store (same default path the sidecar loads) now holds the
	// dev key with all three board grants.
	store, err := kanbansvc.NewTokenStore("")
	if err != nil {
		t.Fatalf("open token store: %v", err)
	}
	grants, ok := store.Grants(key)
	if !ok {
		t.Fatal("dev key not granted in the shared token store")
	}
	want := map[string]bool{
		kanbansvc.GrantRead:     true,
		kanbansvc.GrantMutate:   true,
		kanbansvc.GrantTakeover: true,
	}
	for _, g := range grants {
		delete(want, g)
	}
	if len(want) != 0 {
		t.Fatalf("dev-key grants missing %v (got %d grants)", want, len(grants))
	}

	// Owner-only file: no group/other access bits.
	tokensPath := filepath.Join(home, "kanban", "tokens.json")
	info, err := os.Stat(tokensPath)
	if err != nil {
		t.Fatalf("stat token store: %v", err)
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		t.Fatalf("token store mode %o has group/other access bits", perm)
	}
}
