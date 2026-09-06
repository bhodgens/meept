package rpc

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"log/slog"

	"github.com/caimlas/meept/internal/memory"
)

// dispatchListExpired calls memory.listExpired through the registration map
// (same CallMethod path as dispatchEpistemic's purge test).
func dispatchListExpired(t *testing.T, h *EpistemicHandler, params []byte) (any, error) {
	t.Helper()
	srv := New(&Config{SocketPath: filepath.Join(t.TempDir(), "test.sock")}, nil, slog.Default())
	h.RegisterEpistemicHandlers(srv)
	return srv.CallMethod(context.Background(), "memory.listExpired", params)
}

// TestListExpiredClaims_RPC covers memory.listExpired end to end: the expired
// claim comes back, the unbounded claim does not, and absent params fall back
// to the default limit without erroring.
func TestListExpiredClaims_RPC(t *testing.T) {
	h := newPurgeTestHandler(t)
	mgr, err := h.managerOrErr()
	if err != nil {
		t.Fatalf("managerOrErr: %v", err)
	}
	ctx := context.Background()

	vt := time.Now().UTC().Add(-time.Hour)
	if _, err := mgr.StoreClaim(ctx, memory.Claim{
		Text:    "expired claim",
		Status:  memory.ClaimStatusConfirmed,
		ValidTo: &vt,
	}); err != nil {
		t.Fatalf("StoreClaim expired: %v", err)
	}
	if _, err := mgr.StoreClaim(ctx, memory.Claim{
		Text:   "unbounded claim",
		Status: memory.ClaimStatusConfirmed,
	}); err != nil {
		t.Fatalf("StoreClaim unbounded: %v", err)
	}

	params, _ := json.Marshal(map[string]any{"limit": 20})
	res, err := dispatchListExpired(t, h, params)
	if err != nil {
		t.Fatalf("memory.listExpired: %v", err)
	}
	raw, err := json.Marshal(res)
	if err != nil {
		t.Fatalf("marshal response: %v", err)
	}
	var out struct {
		Memories []struct {
			Memory struct {
				ID       string         `json:"id"`
				Content  string         `json:"content"`
				Metadata map[string]any `json:"metadata"`
			} `json:"memory"`
		} `json:"memories"`
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(out.Memories) != 1 {
		t.Fatalf("expired claims = %d, want 1 (only the expired claim; unbounded excluded)", len(out.Memories))
	}
	if got := out.Memories[0].Memory.Content; got != "expired claim" {
		t.Errorf("expired claim content = %q, want %q", got, "expired claim")
	}
	if vtStr, ok := out.Memories[0].Memory.Metadata["valid_to"].(string); !ok || vtStr == "" {
		t.Errorf("valid_to metadata missing or non-string: %v", out.Memories[0].Memory.Metadata)
	}

	// Absent params must not error (default limit 20 applies).
	if _, err := dispatchListExpired(t, h, nil); err != nil {
		t.Errorf("listExpired with nil params: %v", err)
	}
}
