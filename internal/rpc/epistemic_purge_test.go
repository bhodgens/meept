package rpc

import (
	"context"
	"encoding/json"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	"github.com/caimlas/meept/internal/config"
	"github.com/caimlas/meept/internal/memory"
)

func newPurgeTestHandler(t *testing.T) *EpistemicHandler {
	t.Helper()
	tmpDir := t.TempDir()
	mgr := memory.NewManager(memory.ManagerConfig{
		Config: config.MemoryConfig{
			Backend:  config.MemoryBackendSQLite,
			DataDir:  tmpDir,
			Episodic: config.EpisodicConfig{Enabled: true},
		},
		Logger: slog.Default(),
	})
	if err := mgr.Initialize(context.Background()); err != nil {
		t.Fatalf("manager.Initialize: %v", err)
	}
	t.Cleanup(func() { _ = mgr.Close() })
	return NewEpistemicHandler(mgr)
}

func storeAutoClaim(t *testing.T, h *EpistemicHandler, text string) string {
	t.Helper()
	mgr, err := h.managerOrErr()
	if err != nil {
		t.Fatalf("managerOrErr: %v", err)
	}
	id, err := mgr.Store(context.Background(), memory.Memory{
		Type:     memory.MemoryTypeClaim,
		Category: "claim",
		Content:  text,
		Metadata: map[string]any{"status": string(memory.ClaimStatusAuto)},
	})
	if err != nil {
		t.Fatalf("store auto claim: %v", err)
	}
	return id
}

func TestPurgeAutoClaims_PreviewRequiresConfirmation(t *testing.T) {
	h := newPurgeTestHandler(t)
	storeAutoClaim(t, h, "ambient claim one")
	storeAutoClaim(t, h, "ambient claim two")

	params, _ := json.Marshal(map[string]any{"confirmed": false})
	res, err := dispatchEpistemic(t, h, params)
	if err != nil {
		t.Fatalf("preview returned error: %v", err)
	}
	resp := res.(map[string]any)
	if resp["requires_confirmation"] != true {
		t.Errorf("requires_confirmation = %v, want true", resp["requires_confirmation"])
	}
	if resp["action"] != "purge_auto_claims" {
		t.Errorf("action = %v, want purge_auto_claims", resp["action"])
	}
	// CallMethod returns handler results directly, so numbers are int (not
	// float64 as they would be after a JSON round-trip).
	if n, ok := resp["claim_count"].(int); !ok || n != 2 {
		t.Errorf("claim_count = %v, want 2", resp["claim_count"])
	}
	if d, ok := resp["older_than_days"].(int); !ok || d != 30 {
		t.Errorf("older_than_days default = %v, want 30", resp["older_than_days"])
	}

	// Preview must not delete anything.
	claims, err := h.manager.ListAutoClaims(context.Background(), time.Now().AddDate(0, 0, -365), 100)
	if err != nil {
		t.Fatalf("ListAutoClaims after preview: %v", err)
	}
	if len(claims) != 2 {
		t.Errorf("preview deleted claims; got %d remaining, want 2", len(claims))
	}
}

func TestPurgeAutoClaims_ConfirmedDeletes(t *testing.T) {
	h := newPurgeTestHandler(t)
	storeAutoClaim(t, h, "ambient claim to delete")
	storeAutoClaim(t, h, "another ambient claim")

	params, _ := json.Marshal(map[string]any{"confirmed": true})
	res, err := dispatchEpistemic(t, h, params)
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}
	resp := res.(map[string]any)
	if resp["success"] != true {
		t.Errorf("success = %v, want true", resp["success"])
	}
	if n, ok := resp["deleted"].(int); !ok || n != 2 {
		t.Errorf("deleted = %v, want 2", resp["deleted"])
	}
	if n, ok := resp["attempted"].(int); !ok || n != 2 {
		t.Errorf("attempted = %v, want 2", resp["attempted"])
	}
	if _, hasErr := resp["first_error"]; hasErr {
		t.Errorf("unexpected first_error: %v", resp["first_error"])
	}

	claims, err := h.manager.ListAutoClaims(context.Background(), time.Time{}, 100)
	if err != nil {
		t.Fatalf("ListAutoClaims after purge: %v", err)
	}
	if len(claims) != 0 {
		t.Errorf("%d auto claims remain after confirmed purge, want 0", len(claims))
	}
}

func TestPurgeAutoClaims_NotInitialized(t *testing.T) {
	h := NewEpistemicHandler(nil)
	params, _ := json.Marshal(map[string]any{"confirmed": true})
	if _, err := dispatchEpistemic(t, h, params); err == nil {
		t.Error("expected error when manager is not initialized")
	}
}

// dispatchEpistemic routes raw JSON params through the registered handler,
// exercising the same path the RPC server uses.
func dispatchEpistemic(t *testing.T, h *EpistemicHandler, params []byte) (any, error) {
	t.Helper()
	srv := New(&Config{SocketPath: filepath.Join(t.TempDir(), "test.sock")}, nil, slog.Default())
	h.RegisterEpistemicHandlers(srv)
	ctx := context.Background()
	return srv.CallMethod(ctx, "memory.purgeAutoClaims", params)
}
