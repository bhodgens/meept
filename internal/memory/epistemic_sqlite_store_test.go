package memory

import (
	"context"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
)

func TestStoreClaimSQLiteRoundTrip(t *testing.T) {
	mgr := NewManager(ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  t.TempDir(),
			Episodic: config.EpisodicConfig{Enabled: true},
		},
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("init: %v", err)
	}
	defer mgr.Close()
	id, err := mgr.StoreClaim(context.Background(), Claim{Text: "hello claim", Status: ClaimStatusAuto})
	if err != nil {
		t.Fatalf("StoreClaim: %v", err)
	}
	claims, err := mgr.ListAutoClaims(context.Background(), time.Time{}, 100)
	if err != nil {
		t.Fatalf("ListAutoClaims: %v", err)
	}
	if len(claims) != 1 {
		t.Fatalf("ListAutoClaims got %d, want 1", len(claims))
	}
	t.Logf("ok id=%s type=%s", id, claims[0].Memory.Type)
}
